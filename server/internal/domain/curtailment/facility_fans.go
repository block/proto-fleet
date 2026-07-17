package curtailment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	ActivityTypeFacilityFanCommandFailed = "curtailment_facility_fan_command_failed"
	ActivityTypeFacilityFanSkipped       = "curtailment_facility_fan_skipped"
)

// FacilityFanController is the shared protocol-blind command boundary used by
// the reconciler and terminal recovery paths.
type FacilityFanController interface {
	SetState(ctx context.Context, event *models.Event, power driver.PowerMode) *string
}

type facilityFanController struct {
	devices  interfaces.InfrastructureDeviceStore
	sites    interfaces.SiteStore
	registry *driver.Registry
	audit    AuditLogger
}

func NewFacilityFanController(
	devices interfaces.InfrastructureDeviceStore,
	sites interfaces.SiteStore,
	registry *driver.Registry,
	audit AuditLogger,
) FacilityFanController {
	if audit == nil {
		audit = NoOpAuditLogger{}
	}
	return &facilityFanController{devices: devices, sites: sites, registry: registry, audit: audit}
}

func (c *facilityFanController) SetState(ctx context.Context, event *models.Event, power driver.PowerMode) *string {
	if event == nil || len(event.FacilityFanDeviceIDs) == 0 {
		return nil
	}
	if c == nil || c.devices == nil || c.sites == nil || c.registry == nil {
		message := "facility fan controller is not configured"
		return &message
	}

	logSkips := isFirstFacilityFanAttempt(event, power)
	errorsByDevice := make([]string, 0)
	for _, deviceID := range event.FacilityFanDeviceIDs {
		device, err := c.devices.GetInfrastructureDevice(ctx, event.OrgID, deviceID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				if logSkips {
					c.logSkip(ctx, event, deviceID, "device is missing")
				}
				continue
			}
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: lookup failed", deviceID))
			continue
		}
		if !device.Enabled {
			if logSkips {
				c.logSkip(ctx, event, device.ID, "device is disabled")
			}
			continue
		}

		canonical, err := c.sites.GetInfrastructureControlSubnets(ctx, event.OrgID, device.SiteID)
		if err != nil {
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: site commissioning lookup failed", device.ID))
			continue
		}
		parsed, err := netutil.CanonicalizeInfrastructureControlSubnets(strings.Fields(canonical))
		if err != nil {
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: site commissioning is invalid", device.ID))
			continue
		}
		controller, err := c.registry.Controller(device.DriverType)
		if err != nil {
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: driver is unavailable", device.ID))
			continue
		}
		if err := controller.SetState(ctx, driver.Device{
			ID:                           device.ID,
			OrgID:                        device.OrgID,
			SiteID:                       device.SiteID,
			DriverType:                   device.DriverType,
			DriverConfig:                 device.DriverConfig,
			InfrastructureControlSubnets: parsed.Prefixes,
		}, driver.DesiredState{Power: power}); err != nil {
			slog.Error("curtailment facility fan command failed", "event_uuid", event.EventUUID, "device_id", device.ID, "error", err)
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: command failed", device.ID))
		}
	}
	if len(errorsByDevice) == 0 {
		return nil
	}
	message := strings.Join(errorsByDevice, "; ")
	if event.FanLastError == nil {
		c.logFailure(ctx, event, power, message)
	}
	return &message
}

func isFirstFacilityFanAttempt(event *models.Event, power driver.PowerMode) bool {
	switch power {
	case driver.PowerOff:
		return event.FanOffSentAt == nil
	case driver.PowerOn:
		return event.FanOnSentAt == nil
	default:
		return false
	}
}

func (c *facilityFanController) logSkip(ctx context.Context, event *models.Event, deviceID int64, reason string) {
	orgID := event.OrgID
	row := activitymodels.Event{
		Category:       activitymodels.CategoryCurtailment,
		Type:           ActivityTypeFacilityFanSkipped,
		Description:    "Facility fan command skipped",
		Result:         activitymodels.ResultSuccess,
		ActorType:      activitymodels.ActorCurtailment,
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"event_uuid":               event.EventUUID.String(),
			"infrastructure_device_id": deviceID,
			"reason":                   reason,
		},
	}
	if err := c.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment facility fan skip audit failed", "event_uuid", event.EventUUID, "error", err)
	}
}

func (c *facilityFanController) logFailure(ctx context.Context, event *models.Event, power driver.PowerMode, message string) {
	orgID := event.OrgID
	errorMessage := message
	row := activitymodels.Event{
		Category:       activitymodels.CategoryCurtailment,
		Type:           ActivityTypeFacilityFanCommandFailed,
		Description:    "Facility fan command failed",
		Result:         activitymodels.ResultFailure,
		ErrorMessage:   &errorMessage,
		ActorType:      activitymodels.ActorCurtailment,
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"event_uuid":    event.EventUUID.String(),
			"desired_power": map[driver.PowerMode]string{driver.PowerOff: "off", driver.PowerOn: "on"}[power],
		},
	}
	if err := c.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment facility fan failure audit failed", "event_uuid", event.EventUUID, "error", err)
	}
}
