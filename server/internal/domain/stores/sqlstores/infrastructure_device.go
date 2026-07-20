package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.InfrastructureDeviceStore = &SQLInfrastructureDeviceStore{}

// SQLInfrastructureDeviceStore is the Postgres-backed implementation
// of InfrastructureDeviceStore.
type SQLInfrastructureDeviceStore struct {
	SQLConnectionManager
}

// NewSQLInfrastructureDeviceStore returns a store bound to the
// supplied connection.
func NewSQLInfrastructureDeviceStore(conn *sql.DB) *SQLInfrastructureDeviceStore {
	return &SQLInfrastructureDeviceStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

// deviceFromRow maps the shared Get/List projection (device columns +
// site_label) to the domain model. The two queries produce
// structurally identical sqlc row types, so List converts its row to
// the Get shape; if the projections ever diverge the conversion stops
// compiling rather than drifting silently.
func deviceFromRow(row sqlc.GetInfrastructureDeviceRow) models.Device {
	return models.Device{
		ID:           row.ID,
		OrgID:        row.OrgID,
		SiteID:       row.SiteID,
		SiteLabel:    row.SiteLabel,
		BuildingName: row.BuildingName,
		Name:         row.Name,
		DeviceKind:   row.DeviceKind,
		FanCount:     row.FanCount,
		Enabled:      row.Enabled,
		DriverType:   row.DriverType,
		DriverConfig: row.DriverConfig,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// normalizeDriverConfig maps an empty blob to the empty JSON object so
// the jsonb column never receives zero-length bytes (invalid JSON at
// the wire). Adapters that require config still reject empty blobs in
// ValidateConfig before this runs; this guards future adapters that
// legitimately need no config.
func normalizeDriverConfig(cfg json.RawMessage) json.RawMessage {
	if len(cfg) == 0 {
		return json.RawMessage(`{}`)
	}
	return cfg
}

func (s *SQLInfrastructureDeviceStore) CreateInfrastructureDevice(ctx context.Context, params models.CreateParams) (*models.Device, error) {
	row, err := s.GetQueries(ctx).CreateInfrastructureDevice(ctx, sqlc.CreateInfrastructureDeviceParams{
		OrgID:        params.OrgID,
		SiteID:       params.SiteID,
		BuildingName: params.BuildingName,
		Name:         params.Name,
		DeviceKind:   params.DeviceKind,
		FanCount:     params.FanCount,
		Enabled:      params.Enabled,
		DriverType:   params.DriverType,
		DriverConfig: normalizeDriverConfig(params.DriverConfig),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fleeterror.NewPlainError("an infrastructure device with this name already exists in the site", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return nil, fleeterror.NewInternalErrorf("failed to create infrastructure device: %v", err)
	}
	return s.GetInfrastructureDevice(ctx, params.OrgID, row.ID)
}

func (s *SQLInfrastructureDeviceStore) GetInfrastructureDevice(ctx context.Context, orgID, id int64) (*models.Device, error) {
	row, err := s.GetQueries(ctx).GetInfrastructureDevice(ctx, sqlc.GetInfrastructureDeviceParams{ID: id, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("infrastructure device %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get infrastructure device: %v", err)
	}
	out := deviceFromRow(row)
	return &out, nil
}

func (s *SQLInfrastructureDeviceStore) ListInfrastructureDevices(ctx context.Context, filter models.ListFilter) ([]models.Device, error) {
	siteIDs := filter.SiteIDs
	if siteIDs == nil {
		siteIDs = []int64{}
	}
	excludedSiteIDs := filter.ExcludedSiteIDs
	if excludedSiteIDs == nil {
		excludedSiteIDs = []int64{}
	}
	rows, err := s.GetQueries(ctx).ListInfrastructureDevicesByOrg(ctx, sqlc.ListInfrastructureDevicesByOrgParams{
		OrgID:           filter.OrgID,
		SiteIds:         siteIDs,
		ExcludedSiteIds: excludedSiteIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list infrastructure devices: %v", err)
	}
	out := make([]models.Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, deviceFromRow(sqlc.GetInfrastructureDeviceRow(row)))
	}
	return out, nil
}

func (s *SQLInfrastructureDeviceStore) LockInfrastructureDeviceForWrite(ctx context.Context, orgID, id, expectedSiteID int64) error {
	_, err := s.GetQueries(ctx).LockInfrastructureDeviceForWrite(ctx, sqlc.LockInfrastructureDeviceForWriteParams{
		ID:             id,
		OrgID:          orgID,
		ExpectedSiteID: expectedSiteID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundErrorf("infrastructure device %d not found", id)
	}
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to lock infrastructure device: %v", err)
	}
	return nil
}

func (s *SQLInfrastructureDeviceStore) CountResponseProfilesByInfrastructureDevice(ctx context.Context, orgID, id int64) (int64, error) {
	count, err := s.GetQueries(ctx).CountResponseProfilesByInfrastructureDevice(
		ctx,
		sqlc.CountResponseProfilesByInfrastructureDeviceParams{
			OrgID:                   orgID,
			InfrastructureDeviceIds: []int64{id},
		},
	)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count response profiles by infrastructure device: %v", err)
	}
	return count, nil
}

func (s *SQLInfrastructureDeviceStore) CountActiveCurtailmentEventsByInfrastructureDevice(
	ctx context.Context,
	orgID, id int64,
) (int64, error) {
	count, err := s.GetQueries(ctx).CountActiveCurtailmentEventsByInfrastructureDevices(
		ctx,
		sqlc.CountActiveCurtailmentEventsByInfrastructureDevicesParams{
			OrgID:                   orgID,
			InfrastructureDeviceIds: []int64{id},
		},
	)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count active curtailment events by infrastructure device: %v", err)
	}
	return count, nil
}

func (s *SQLInfrastructureDeviceStore) CountNonTerminalCurtailmentEventsByInfrastructureDevice(
	ctx context.Context,
	orgID, id int64,
) (int64, error) {
	count, err := s.GetQueries(ctx).CountNonTerminalCurtailmentEventsByInfrastructureDevices(
		ctx,
		sqlc.CountNonTerminalCurtailmentEventsByInfrastructureDevicesParams{
			OrgID:                   orgID,
			InfrastructureDeviceIds: []int64{id},
		},
	)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count non-terminal curtailment events by infrastructure device: %v", err)
	}
	return count, nil
}

func (s *SQLInfrastructureDeviceStore) UpdateInfrastructureDevice(ctx context.Context, params models.UpdateParams) (*models.Device, error) {
	// Nil Enabled maps to SQL NULL: the query's COALESCE preserves the
	// row's current value atomically instead of writing back a value
	// read before the transaction.
	enabled := sql.NullBool{}
	if params.Enabled != nil {
		enabled = sql.NullBool{Bool: *params.Enabled, Valid: true}
	}
	affected, err := s.GetQueries(ctx).UpdateInfrastructureDevice(ctx, sqlc.UpdateInfrastructureDeviceParams{
		SiteID:         params.SiteID,
		BuildingName:   params.BuildingName,
		Name:           params.Name,
		DeviceKind:     params.DeviceKind,
		FanCount:       params.FanCount,
		Enabled:        enabled,
		DriverType:     params.DriverType,
		DriverConfig:   normalizeDriverConfig(params.DriverConfig),
		ID:             params.ID,
		OrgID:          params.OrgID,
		ExpectedSiteID: params.ExpectedSiteID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fleeterror.NewPlainError("an infrastructure device with this name already exists in the site", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return nil, fleeterror.NewInternalErrorf("failed to update infrastructure device: %v", err)
	}
	if affected == 0 {
		return nil, fleeterror.NewNotFoundErrorf("infrastructure device %d not found", params.ID)
	}
	return s.GetInfrastructureDevice(ctx, params.OrgID, params.ID)
}

func (s *SQLInfrastructureDeviceStore) SoftDeleteInfrastructureDevice(ctx context.Context, orgID, id, expectedSiteID int64) (*models.Device, bool, error) {
	row, err := s.GetQueries(ctx).SoftDeleteInfrastructureDevice(ctx, sqlc.SoftDeleteInfrastructureDeviceParams{ID: id, OrgID: orgID, ExpectedSiteID: expectedSiteID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fleeterror.NewInternalErrorf("failed to delete infrastructure device: %v", err)
	}
	out := models.Device{
		ID:           row.ID,
		OrgID:        row.OrgID,
		SiteID:       row.SiteID,
		BuildingName: row.BuildingName,
		Name:         row.Name,
		DeviceKind:   row.DeviceKind,
		FanCount:     row.FanCount,
		Enabled:      row.Enabled,
		DriverType:   row.DriverType,
		DriverConfig: row.DriverConfig,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	return &out, true, nil
}
