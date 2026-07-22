// Package infrastructure is the domain layer for facility
// infrastructure devices (fans / fan groups behind a PLC or drive).
// The core validates only protocol-blind fields; driver_config
// validation is delegated to the driver adapter registry so the core
// never learns protocol details.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver/modbustcp"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for infrastructure-device activity logs.
const (
	eventDeviceCreated = "infrastructure_device.created"
	eventDeviceUpdated = "infrastructure_device.updated"
	eventDeviceDeleted = "infrastructure_device.deleted"
)

// NewDefaultDriverRegistry returns the registry with every production
// driver adapter registered. New protocols add a Register call here
// and nothing else in the core.
func NewDefaultDriverRegistry() *driver.Registry {
	return newDriverRegistry(modbustcp.New)
}

// NewConfiguredDriverRegistry returns the production driver registry with the
// deployment-global OT allowlist validated and applied to every Modbus
// controller. Empty config is accepted but remains fail closed for writes.
func NewConfiguredDriverRegistry(config Config) (*driver.Registry, error) {
	controlSubnets, err := config.controlSubnets()
	if err != nil {
		return nil, err
	}

	return newDriverRegistry(func() driver.Controller {
		return modbustcp.NewConfigured(controlSubnets)
	}), nil
}

func newDriverRegistry(modbusFactory driver.Factory) *driver.Registry {
	registry := driver.NewRegistry()
	registry.Register(modbustcp.DriverType, modbusFactory)
	return registry
}

// Service owns infrastructure-device CRUD and validation.
type Service struct {
	store       interfaces.InfrastructureDeviceStore
	siteStore   interfaces.SiteStore
	registry    *driver.Registry
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService returns a Service bound to the supplied stores and
// driver registry. activitySvc is the fire-and-forget audit sink for
// device mutations; it may be nil in tests or environments where
// activity logging is disabled.
func NewService(store interfaces.InfrastructureDeviceStore, siteStore interfaces.SiteStore, registry *driver.Registry, transactor interfaces.Transactor, activitySvc *activity.Service) *Service {
	return &Service{store: store, siteStore: siteStore, registry: registry, transactor: transactor, activitySvc: activitySvc}
}

// logDeviceEvent emits an audit row for a device mutation. Fires
// AFTER the mutation's tx commits — RunInTx may retry the closure on
// serialization failures, so an in-closure Log would duplicate.
//
// Metadata deliberately excludes driver_config: these records define
// OT control topology (endpoints, registers), which must not land in
// the activity feed. Only protocol-blind display fields are logged.
func (s *Service) logDeviceEvent(ctx context.Context, eventType, verb string, device *models.Device) {
	orgID := device.OrgID
	siteID := device.SiteID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventType,
		OrganizationID: &orgID,
		SiteID:         &siteID,
		Description:    fmt.Sprintf("%s infrastructure device %q (id=%d)", verb, device.Name, device.ID),
		Metadata: map[string]any{
			"infrastructure_device_id": device.ID,
			"device_name":              device.Name,
			"site_id":                  device.SiteID,
			"building_name":            device.BuildingName,
			"rack_name":                device.RackName,
			"device_kind":              device.DeviceKind,
			"fan_count":                device.FanCount,
			"enabled":                  device.Enabled,
			"driver_type":              device.DriverType,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)
}

// List returns every live device in the org, optionally narrowed to
// specific sites.
func (s *Service) List(ctx context.Context, filter models.ListFilter) ([]models.Device, error) {
	return s.store.ListInfrastructureDevices(ctx, filter)
}

// Get returns the live device or NotFound.
func (s *Service) Get(ctx context.Context, orgID, id int64) (*models.Device, error) {
	return s.store.GetInfrastructureDevice(ctx, orgID, id)
}

// Create validates and inserts a new device.
func (s *Service) Create(ctx context.Context, params models.CreateParams) (*models.Device, error) {
	normalized, err := s.validateAndNormalize(deviceInput{
		SiteID:       params.SiteID,
		BuildingName: params.BuildingName,
		RackName:     params.RackName,
		Name:         params.Name,
		DeviceKind:   params.DeviceKind,
		FanCount:     params.FanCount,
		DriverType:   params.DriverType,
		DriverConfig: params.DriverConfig,
	})
	if err != nil {
		return nil, err
	}
	params.BuildingName = normalized.BuildingName
	params.RackName = normalized.RackName
	params.Name = normalized.Name
	params.FanCount = normalized.FanCount
	params.DriverType = normalized.DriverType

	var created *models.Device
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the parent site row so a concurrent DeleteSite can't
		// soft-delete it between the live-site check and the insert
		// (same TOCTOU fix as buildings.CreateBuilding —
		// LockSiteForWrite returns NotFound when the site is
		// missing/soft-deleted/cross-org).
		if err := s.siteStore.LockSiteForWrite(txCtx, params.OrgID, params.SiteID); err != nil {
			return err
		}
		device, err := s.store.CreateInfrastructureDevice(txCtx, params)
		if err != nil {
			return err
		}
		created = device
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logDeviceEvent(ctx, eventDeviceCreated, "Created", created)
	return created, nil
}

// Update validates and mutates an existing device.
func (s *Service) Update(ctx context.Context, params models.UpdateParams) (*models.Device, error) {
	normalized, err := s.validateAndNormalize(deviceInput{
		SiteID:       params.SiteID,
		BuildingName: params.BuildingName,
		RackName:     params.RackName,
		Name:         params.Name,
		DeviceKind:   params.DeviceKind,
		FanCount:     params.FanCount,
		DriverType:   params.DriverType,
		DriverConfig: params.DriverConfig,
	})
	if err != nil {
		return nil, err
	}
	params.BuildingName = normalized.BuildingName
	params.RackName = normalized.RackName
	params.Name = normalized.Name
	params.FanCount = normalized.FanCount
	params.DriverType = normalized.DriverType

	var updated *models.Device
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the source site (ExpectedSiteID) as well as the target:
		// a move out of site A must serialize against a concurrent
		// DeleteSite(A), which locks A before cascading over devices
		// with site_id = A — without the source lock, the move could
		// commit between the delete's lock and its cascade, slipping a
		// live device out from under a confirmed deletion. Ascending
		// ID order keeps crossing moves (A→B vs B→A) deadlock-free.
		for _, siteID := range siteLockOrder(params.ExpectedSiteID, params.SiteID) {
			if err := s.siteStore.LockSiteForWrite(txCtx, params.OrgID, siteID); err != nil {
				return err
			}
		}
		if err := s.store.LockInfrastructureDeviceForWrite(
			txCtx,
			params.OrgID,
			params.ID,
			params.ExpectedSiteID,
		); err != nil {
			return err
		}
		if params.SiteID != params.ExpectedSiteID {
			profileCount, err := s.store.CountResponseProfilesByInfrastructureDevice(txCtx, params.OrgID, params.ID)
			if err != nil {
				return err
			}
			if profileCount > 0 {
				return fleeterror.NewFailedPreconditionError(
					"infrastructure device is referenced by curtailment response profiles; update those profiles before moving it",
				)
			}
			activeEventCount, err := s.store.CountActiveCurtailmentEventsByInfrastructureDevice(
				txCtx,
				params.OrgID,
				params.ID,
			)
			if err != nil {
				return err
			}
			if activeEventCount > 0 {
				return fleeterror.NewFailedPreconditionError(
					"infrastructure device is claimed by an active curtailment event or protected by unresolved terminal facility fan recovery; finish the event or resolve its facility fan recovery before moving it",
				)
			}
		} else {
			current, err := s.store.GetInfrastructureDevice(txCtx, params.OrgID, params.ID)
			if err != nil {
				return err
			}
			if infrastructureCommandBehaviorChanged(current, params) {
				activeEventCount, err := s.store.CountActiveCurtailmentEventsByInfrastructureDevice(
					txCtx,
					params.OrgID,
					params.ID,
				)
				if err != nil {
					return err
				}
				if activeEventCount > 0 {
					return fleeterror.NewFailedPreconditionError(
						"infrastructure device is claimed by an active curtailment event or protected by unresolved terminal facility fan recovery; finish the event or resolve its facility fan recovery before changing its enabled state or driver configuration",
					)
				}
			}
		}
		device, err := s.store.UpdateInfrastructureDevice(txCtx, params)
		if err != nil {
			return err
		}
		updated = device
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logDeviceEvent(ctx, eventDeviceUpdated, "Updated", updated)
	return updated, nil
}

func infrastructureCommandBehaviorChanged(current *models.Device, params models.UpdateParams) bool {
	if current == nil {
		return true
	}
	if params.Enabled != nil && *params.Enabled != current.Enabled {
		return true
	}
	return params.DriverType != current.DriverType || !bytes.Equal(params.DriverConfig, current.DriverConfig)
}

// Delete soft-deletes the device. expectedSiteID is the device's site
// as seen at authorization time; the write is predicated on it so a
// concurrent site move invalidates the delete (NotFound) rather than
// removing a device the caller no longer manages.
func (s *Service) Delete(ctx context.Context, orgID, id, expectedSiteID int64) error {
	var deleted *models.Device
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.siteStore.LockSiteForWrite(txCtx, orgID, expectedSiteID); err != nil {
			return err
		}
		if err := s.store.LockInfrastructureDeviceForWrite(txCtx, orgID, id, expectedSiteID); err != nil {
			return err
		}
		profileCount, err := s.store.CountResponseProfilesByInfrastructureDevice(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if profileCount > 0 {
			return fleeterror.NewFailedPreconditionError(
				"infrastructure device is referenced by curtailment response profiles; update those profiles first",
			)
		}
		activeEventCount, err := s.store.CountActiveCurtailmentEventsByInfrastructureDevice(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if activeEventCount > 0 {
			return fleeterror.NewFailedPreconditionError(
				"infrastructure device is claimed by an active curtailment event or protected by unresolved terminal facility fan recovery; finish the event or resolve its facility fan recovery before deleting it",
			)
		}
		device, found, err := s.store.SoftDeleteInfrastructureDevice(txCtx, orgID, id, expectedSiteID)
		if err != nil {
			return err
		}
		if !found {
			return fleeterror.NewNotFoundErrorf("infrastructure device %d not found", id)
		}
		deleted = device
		return nil
	})
	if err != nil {
		return err
	}
	// The audit stamp uses the row returned by the delete itself
	// (UPDATE … RETURNING), so it reflects the device actually
	// deleted even under a concurrent rename/move.
	s.logDeviceEvent(ctx, eventDeviceDeleted, "Deleted", deleted)
	return nil
}

// siteLockOrder returns the distinct site IDs to row-lock for a write
// touching both sites, in ascending order so concurrent transactions
// locking the same pair (e.g. crossing A→B and B→A moves) acquire
// locks in the same sequence and cannot deadlock.
func siteLockOrder(a, b int64) []int64 {
	if a == b {
		return []int64{a}
	}
	if a < b {
		return []int64{a, b}
	}
	return []int64{b, a}
}

// deviceInput is the shared validation shape for create and update.
type deviceInput struct {
	SiteID       int64
	BuildingName string
	RackName     string
	Name         string
	DeviceKind   string
	FanCount     int32
	DriverType   string
	DriverConfig []byte
}

// validateAndNormalize enforces protocol-blind invariants and
// delegates driver_config validation to the adapter registry. Site
// existence/liveness is deliberately NOT checked here — the write
// paths take a row lock on the site inside their transaction instead,
// which subsumes the check without a TOCTOU window.
func (s *Service) validateAndNormalize(in deviceInput) (deviceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BuildingName = strings.TrimSpace(in.BuildingName)
	in.RackName = strings.TrimSpace(in.RackName)
	if in.Name == "" {
		return in, fleeterror.NewInvalidArgumentError("name is required")
	}
	if !models.ValidKind(in.DeviceKind) {
		return in, fleeterror.NewInvalidArgumentErrorf("device_kind must be %q or %q, got %q", models.KindSingleFan, models.KindFanGroup, in.DeviceKind)
	}
	switch in.DeviceKind {
	case models.KindSingleFan:
		in.FanCount = 1
	case models.KindFanGroup:
		if in.FanCount < 2 {
			return in, fleeterror.NewInvalidArgumentError("fan_count must be at least 2 for a fan group")
		}
	}
	if in.SiteID <= 0 {
		return in, fleeterror.NewInvalidArgumentError("site_id is required")
	}
	in.DriverType = strings.TrimSpace(in.DriverType)
	if in.DriverType == "" {
		return in, fleeterror.NewInvalidArgumentError("driver_type is required")
	}
	if err := s.registry.ValidateConfig(in.DriverType, in.DriverConfig); err != nil {
		return in, fleeterror.NewInvalidArgumentError(err.Error())
	}
	return in, nil
}
