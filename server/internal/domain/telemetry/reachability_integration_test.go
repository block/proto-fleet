package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTelemetryReachabilityRequiresConfirmedOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	ctx := t.Context()
	conn := dbtest.GetTestDB(t)
	_, err := conn.ExecContext(ctx, `
		INSERT INTO organization (id, org_id, name) VALUES (1, 'reachability-test', 'Reachability test');
		INSERT INTO discovered_device (id, org_id, device_identifier, driver_name, ip_address, port, url_scheme, is_active)
		VALUES (1, 1, 'reachability-miner', 'proto', '192.0.2.216', '8080', 'http', TRUE);
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES (1, 1, 1, 'reachability-miner', '02:00:00:00:02:16');
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES (1, 'PAIRED', now() - interval '1 hour');
	`)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	getter := mock.NewMockCachedMinerGetter(ctrl)
	scheduler := mock.NewMockUpdateScheduler(ctrl)
	miner := minerMocks.NewMockMiner(ctrl)
	device := models.Device{ID: "reachability-miner"}
	getter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), device.ID).Return(miner, nil).Times(9)
	miner.EXPECT().GetOrgID().Return(int64(1)).Times(9)
	miner.EXPECT().GetSiteID().Return(int64(0)).Times(9)
	miner.EXPECT().GetDriverName().Return("proto").Times(9)
	scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).Times(3)
	confirmed := fleeterror.NewConnectionError(string(device.ID), errors.New("miner connection refused"))
	confirmed.DeviceProbeFailed = true
	transport := fleeterror.NewConnectionError(string(device.ID), errors.New("Fleet Node unavailable"))
	gomock.InOrder(
		miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusOffline, confirmed),
		miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{Health: modelsV2.HealthUnknown}, nil),
		miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusOffline, confirmed),
		miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusOffline, nil),
		miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusOffline, confirmed),
		miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusOffline, transport),
		miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{}, confirmed),
		miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{}, transport),
		miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusOffline, transport),
	)

	store := sqlstores.NewSQLDeviceStore(conn)
	service := NewTelemetryService(Config{ConcurrencyLimit: 1, MetricTimeout: time.Second, StatusFlushInterval: time.Hour}, nil, getter, scheduler, store, nil)
	activation := telemetryActivationForTest(service)
	check := func(unreachable, unknown int64) {
		t.Helper()
		writerCtx, cancel := context.WithCancel(ctx)
		cancel()
		service.statusWriterRoutine(writerCtx, activation)
		statuses, err := store.GetDeviceStatusForDeviceIdentifiers(ctx, []models.DeviceIdentifier{device.ID})
		require.NoError(t, err)
		require.Equal(t, mm.MinerStatusOffline, statuses[device.ID])
		var gotUnreachable, gotUnknown int64
		require.NoError(t, conn.QueryRowContext(ctx, `
			SELECT unreachable_count, unknown_count FROM device_reachability_summary WHERE org_id = 1
		`).Scan(&gotUnreachable, &gotUnknown))
		require.Equal(t, unreachable, gotUnreachable)
		require.Equal(t, unknown, gotUnknown)
	}

	service.processStatusOnly(ctx, device, activation.results.status)
	check(1, 0)
	require.NoError(t, service.processDevice(ctx, device, activation.results))
	check(0, 1)
	service.processStatusOnly(ctx, device, activation.results.status)
	check(1, 0)
	service.processStatusOnly(ctx, device, activation.results.status)
	check(0, 1)
	service.processStatusOnly(ctx, device, activation.results.status)
	check(1, 0)
	service.processStatusOnly(ctx, device, activation.results.status)
	check(0, 1)

	// A confirmed failure from the telemetry RPC needs no follow-up status RPC.
	require.NoError(t, service.processDevice(ctx, device, activation.results))
	check(1, 0)
	// A Node transport failure still needs the explicit status fallback.
	require.NoError(t, service.processDevice(ctx, device, activation.results))
	check(0, 1)
}
