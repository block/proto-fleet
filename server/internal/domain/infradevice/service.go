// Package infradevice is the domain layer for the InfraDeviceService
// RPC surface. CRUD + bulk ops + network discovery stubs for
// infrastructure devices (fans, sensors, PDUs).
package infradevice

import (
	"context"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infradevice/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for infra device activity logs.
const (
	eventInfraDeviceCreated            = "infra_device.created"
	eventInfraDeviceUpdated            = "infra_device.updated"
	eventInfraDeviceDeleted            = "infra_device.deleted"
	eventInfraDeviceBulkControlMode    = "infra_device.bulk_control_mode"
	eventInfraDeviceBulkDeleted        = "infra_device.bulk_deleted"
	eventInfraDevicePaired             = "infra_device.paired"
	eventInfraDeviceConnectionTested   = "infra_device.connection_tested"
)

// TestConnectionTimeout is the configurable deadline for the mock ping
// in TestConnection. Exported so tests can override it.
var TestConnectionTimeout = 5 * time.Second

// Service is the domain entry point for infrastructure device CRUD and
// operations.
type Service struct {
	store       interfaces.InfraDeviceStore
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService wires an InfraDeviceStore, Transactor, and the activity
// Service used for fire-and-forget audit logs. activitySvc may be nil
// in tests or environments where activity logging is disabled.
func NewService(
	store interfaces.InfraDeviceStore,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:       store,
		transactor:  transactor,
		activitySvc: activitySvc,
	}
}

// CreateInfraDevice inserts a new infrastructure device.
func (s *Service) CreateInfraDevice(ctx context.Context, params models.CreateParams) (*models.InfraDevice, error) {
	if !models.DeviceType(params.DeviceType).Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid device_type")
	}
	if !models.DeviceStatus(params.Status).Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid status")
	}
	if !models.ControlMode(params.ControlMode).Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid control_mode")
	}

	device, err := s.store.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER the write succeeds.
	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDeviceCreated,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Created infra device %q (id=%d)", device.Name, device.ID),
			Metadata: map[string]any{
				"infra_device_id":   device.ID,
				"infra_device_name": device.Name,
				"device_type":       device.DeviceType,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return device, nil
}

// GetInfraDevice returns the live infra device or NotFound.
func (s *Service) GetInfraDevice(ctx context.Context, orgID, id int64) (*models.InfraDevice, error) {
	return s.store.Get(ctx, orgID, id)
}

// ListInfraDevices returns the filtered infra device list.
func (s *Service) ListInfraDevices(ctx context.Context, filter models.ListFilter) ([]models.InfraDevice, error) {
	return s.store.List(ctx, filter)
}

// UpdateInfraDevice mutates the device's mutable fields.
func (s *Service) UpdateInfraDevice(ctx context.Context, params models.UpdateParams) (*models.InfraDevice, error) {
	if params.ControlMode != nil && !models.ControlMode(*params.ControlMode).Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid control_mode")
	}

	device, err := s.store.Update(ctx, params)
	if err != nil {
		return nil, err
	}

	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDeviceUpdated,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Updated infra device %q (id=%d)", device.Name, device.ID),
			Metadata: map[string]any{
				"infra_device_id":   device.ID,
				"infra_device_name": device.Name,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return device, nil
}

// DeleteInfraDevice soft-deletes the infra device.
func (s *Service) DeleteInfraDevice(ctx context.Context, orgID, id int64) error {
	rowsAffected, err := s.store.SoftDelete(ctx, orgID, id)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fleeterror.NewNotFoundErrorf("infra device %d not found", id)
	}

	if s.activitySvc != nil {
		orgIDVal := orgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDeviceDeleted,
			OrganizationID: &orgIDVal,
			Description:    fmt.Sprintf("Deleted infra device %d", id),
			Metadata: map[string]any{
				"infra_device_id": id,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return nil
}

// BulkUpdateControlMode sets the control mode for every device in the
// supplied ID set. Returns the count of rows updated.
func (s *Service) BulkUpdateControlMode(ctx context.Context, orgID int64, ids []int64, controlMode int16) (int64, error) {
	if len(ids) == 0 {
		return 0, fleeterror.NewInvalidArgumentError("ids must not be empty")
	}
	if !models.ControlMode(controlMode).Valid() {
		return 0, fleeterror.NewInvalidArgumentError("invalid control_mode")
	}

	count, err := s.store.BulkUpdateControlMode(ctx, orgID, ids, controlMode)
	if err != nil {
		return 0, err
	}

	if s.activitySvc != nil {
		orgIDVal := orgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDeviceBulkControlMode,
			OrganizationID: &orgIDVal,
			Description:    fmt.Sprintf("Bulk updated control mode to %d for %d infra device(s)", controlMode, count),
			Metadata: map[string]any{
				"infra_device_ids": ids,
				"control_mode":     controlMode,
				"updated_count":    count,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return count, nil
}

// BulkSoftDelete soft-deletes every device in the supplied ID set.
// Returns the count of rows affected.
func (s *Service) BulkSoftDelete(ctx context.Context, orgID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, fleeterror.NewInvalidArgumentError("ids must not be empty")
	}

	count, err := s.store.BulkSoftDelete(ctx, orgID, ids)
	if err != nil {
		return 0, err
	}

	if s.activitySvc != nil {
		orgIDVal := orgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDeviceBulkDeleted,
			OrganizationID: &orgIDVal,
			Description:    fmt.Sprintf("Bulk deleted %d infra device(s)", count),
			Metadata: map[string]any{
				"infra_device_ids": ids,
				"deleted_count":    count,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return count, nil
}

// GetStats returns aggregate counts across all live infra devices in
// the org.
func (s *Service) GetStats(ctx context.Context, orgID int64) (*models.InfraDeviceStats, error) {
	return s.store.GetStats(ctx, orgID)
}

// TestConnection performs a mock connectivity check against the
// device's IP address. Returns success for now; will be wired to a
// real ping/SNMP probe later.
func (s *Service) TestConnection(ctx context.Context, orgID, id int64) (bool, error) {
	device, err := s.store.Get(ctx, orgID, id)
	if err != nil {
		return false, err
	}
	if device.IPAddress == nil || *device.IPAddress == "" {
		return false, fleeterror.NewInvalidArgumentError("device has no IP address configured")
	}

	// Mock ping — return success. Real implementation will use
	// net.DialTimeout or SNMP probe with TestConnectionTimeout.
	_ = TestConnectionTimeout

	if s.activitySvc != nil {
		orgIDVal := orgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDeviceConnectionTested,
			OrganizationID: &orgIDVal,
			Description:    fmt.Sprintf("Tested connection to infra device %q (id=%d)", device.Name, device.ID),
			Metadata: map[string]any{
				"infra_device_id": device.ID,
				"ip_address":      *device.IPAddress,
				"success":         true,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return true, nil
}

// ScanNetwork returns discovered devices on the network. Returns an
// empty list for now; to be wired to real network discovery (nmap /
// SNMP sweep) later.
func (s *Service) ScanNetwork(_ context.Context, _ int64) ([]models.DiscoveredDevice, error) {
	// Stub — real implementation will use nmap or SNMP broadcast
	// discovery scoped to the site's subnet configuration.
	return []models.DiscoveredDevice{}, nil
}

// PairDevices batch-creates infra devices from a list of pair entries.
// Typically called after an operator reviews ScanNetwork results and
// confirms which devices to onboard. Runs inside a single transaction
// so the batch is all-or-nothing.
func (s *Service) PairDevices(ctx context.Context, orgID int64, entries []models.PairEntry) ([]*models.InfraDevice, error) {
	if len(entries) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("entries must not be empty")
	}

	var devices []*models.InfraDevice
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		devices = make([]*models.InfraDevice, 0, len(entries))
		for _, entry := range entries {
			if !models.DeviceType(entry.DeviceType).Valid() {
				return fleeterror.NewInvalidArgumentErrorf("invalid device_type for %q", entry.Name)
			}
			if !models.ControlMode(entry.ControlMode).Valid() {
				return fleeterror.NewInvalidArgumentErrorf("invalid control_mode for %q", entry.Name)
			}
			device, err := s.store.Create(txCtx, models.CreateParams{
				OrgID:       orgID,
				Name:        entry.Name,
				DeviceType:  entry.DeviceType,
				Subtype:     entry.Subtype,
				SiteID:      entry.SiteID,
				BuildingID:  entry.BuildingID,
				IPAddress:   entry.IPAddress,
				Status:      entry.Status,
				ControlMode: entry.ControlMode,
				Protocol:    entry.Protocol,
			})
			if err != nil {
				return err
			}
			devices = append(devices, device)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER tx commits.
	if s.activitySvc != nil {
		orgIDVal := orgID
		ids := make([]int64, len(devices))
		for i, d := range devices {
			ids[i] = d.ID
		}
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventInfraDevicePaired,
			OrganizationID: &orgIDVal,
			Description:    fmt.Sprintf("Paired %d infra device(s)", len(devices)),
			Metadata: map[string]any{
				"infra_device_ids": ids,
				"count":            len(devices),
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return devices, nil
}
