package sqlstores

import (
	"context"
	"database/sql"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

var _ interfaces.CurtailmentStore = &SQLCurtailmentStore{}

type SQLCurtailmentStore struct {
	SQLConnectionManager
}

func NewSQLCurtailmentStore(conn *sql.DB) *SQLCurtailmentStore {
	return &SQLCurtailmentStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLCurtailmentStore) ListValidDeviceSetIDs(ctx context.Context, orgID int64, deviceSetIDs []int64) ([]int64, error) {
	if len(deviceSetIDs) == 0 {
		return []int64{}, nil
	}

	rows, err := s.GetQueries(ctx).GetDeviceSetTypesBatch(ctx, sqlc.GetDeviceSetTypesBatchParams{
		OrgID:        orgID,
		DeviceSetIds: deviceSetIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list valid device sets: %v", err)
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (s *SQLCurtailmentStore) ListPreviewDevices(ctx context.Context, params interfaces.CurtailmentPreviewDeviceParams) ([]interfaces.CurtailmentPreviewDevice, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentPreviewDevices(ctx, sqlc.ListCurtailmentPreviewDevicesParams{
		OrgID:             params.OrgID,
		ScopeType:         params.ScopeType,
		DeviceSetIds:      params.DeviceSetIDs,
		DeviceIdentifiers: params.DeviceIdentifiers,
		CooldownSince:     params.CooldownSince,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment preview devices: %v", err)
	}

	devices := make([]interfaces.CurtailmentPreviewDevice, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, interfaces.CurtailmentPreviewDevice{
			DeviceID:            row.DeviceID,
			DeviceIdentifier:    row.DeviceIdentifier,
			Manufacturer:        row.Manufacturer,
			Model:               row.Model,
			FirmwareVersion:     row.FirmwareVersion,
			DriverName:          row.DriverName,
			PairingStatus:       row.PairingStatus,
			DeviceStatus:        stringPtrIfNotEmpty(row.DeviceStatus),
			LatestMetricAt:      timePtrIf(row.HasLatestMetric, row.LatestMetricAt),
			CurrentPowerW:       float64PtrIf(row.HasCurrentPowerW, row.CurrentPowerW),
			RecentPowerW:        float64PtrIf(row.HasRecentPowerW, row.RecentPowerW),
			RecentHashRateHS:    float64PtrIf(row.HasRecentHashRateHs, row.RecentHashRateHs),
			EfficiencyJH:        efficiencyJTHPtrIf(row.HasEfficiencyJh, row.EfficiencyJh),
			InActiveCurtailment: row.InActiveCurtailment,
			InCooldown:          row.InCooldown,
		})
	}

	return devices, nil
}

func stringPtrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func timePtrIf(ok bool, v time.Time) *time.Time {
	if !ok {
		return nil
	}
	return &v
}

func float64PtrIf(ok bool, v float64) *float64 {
	if !ok {
		return nil
	}
	return &v
}

func efficiencyJTHPtrIf(ok bool, v float64) *float64 {
	if !ok {
		return nil
	}
	displayValue := models.ConvertToDisplayUnits(v, models.MeasurementTypeEfficiency)
	return &displayValue
}
