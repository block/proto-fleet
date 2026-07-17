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

	logFailure := shouldLogFacilityFanFailure(event, power)
	errorsByDevice := make([]string, 0)
	for _, deviceID := range event.FacilityFanDeviceIDs {
		device, err := c.devices.GetInfrastructureDevice(ctx, event.OrgID, deviceID)
		if err != nil {
			if fleeterror.IsNotFoundError(err) {
				errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: device is missing", deviceID))
				continue
			}
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: lookup failed", deviceID))
			continue
		}
		if !device.Enabled {
			errorsByDevice = append(errorsByDevice, fmt.Sprintf("device %d: device is disabled", device.ID))
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
	if logFailure {
		c.logFailure(ctx, event, power, message)
	}
	return &message
}

func shouldLogFacilityFanFailure(event *models.Event, power driver.PowerMode) bool {
	switch power {
	case driver.PowerOff:
		return event.FanLastError == nil
	case driver.PowerOn:
		// A failed OFF phase may leave FanLastError populated. The first ON
		// failure is a distinct recovery incident and must receive its own
		// audit row; later failures in either phase remain deduplicated until
		// an intervening success clears FanLastError.
		return event.FanOnSentAt == nil || event.FanLastError == nil
	default:
		return false
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
