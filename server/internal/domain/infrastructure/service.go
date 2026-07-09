// Package infrastructure is the domain layer for facility
// infrastructure devices (fans / fan groups behind a PLC or drive).
// The core validates only protocol-blind fields; driver_config
// validation is delegated to the driver adapter registry so the core
// never learns protocol details.
package infrastructure

import (
	"context"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver/modbustcp"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// NewDefaultDriverRegistry returns the registry with every production
// driver adapter registered. New protocols add a Register call here
// and nothing else in the core.
func NewDefaultDriverRegistry() *driver.Registry {
	registry := driver.NewRegistry()
	registry.Register(modbustcp.DriverType, modbustcp.New)
	return registry
}

// Service owns infrastructure-device CRUD and validation.
type Service struct {
	store      interfaces.InfrastructureDeviceStore
	siteStore  interfaces.SiteStore
	registry   *driver.Registry
	transactor interfaces.Transactor
}

// NewService returns a Service bound to the supplied stores and
// driver registry.
func NewService(store interfaces.InfrastructureDeviceStore, siteStore interfaces.SiteStore, registry *driver.Registry, transactor interfaces.Transactor) *Service {
	return &Service{store: store, siteStore: siteStore, registry: registry, transactor: transactor}
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
	params.Name = normalized.Name
	params.FanCount = normalized.FanCount

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
	return created, nil
}

// Update validates and mutates an existing device.
func (s *Service) Update(ctx context.Context, params models.UpdateParams) (*models.Device, error) {
	normalized, err := s.validateAndNormalize(deviceInput{
		SiteID:       params.SiteID,
		BuildingName: params.BuildingName,
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
	params.Name = normalized.Name
	params.FanCount = normalized.FanCount

	var updated *models.Device
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.siteStore.LockSiteForWrite(txCtx, params.OrgID, params.SiteID); err != nil {
			return err
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
	return updated, nil
}

// Delete soft-deletes the device.
//
// TODO(#723, PR 3): once curtailment_response_profile gains
// facility_fan_device_ids, reject deletion with FailedPrecondition
// while any response profile references the device — mirroring the
// guard that blocks response-profile deletion while automation rules
// reference it.
func (s *Service) Delete(ctx context.Context, orgID, id int64) error {
	found, err := s.store.SoftDeleteInfrastructureDevice(ctx, orgID, id)
	if err != nil {
		return err
	}
	if !found {
		return fleeterror.NewNotFoundErrorf("infrastructure device %d not found", id)
	}
	return nil
}

// deviceInput is the shared validation shape for create and update.
type deviceInput struct {
	SiteID       int64
	BuildingName string
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
	if err := s.registry.ValidateConfig(in.DriverType, in.DriverConfig); err != nil {
		return in, fleeterror.NewInvalidArgumentError(err.Error())
	}
	return in, nil
}
