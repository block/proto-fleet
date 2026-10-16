package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

func newRebootTestDevice(t *testing.T, statusAt time.Time) (*sql.DB, *sqlstores.SQLDeviceStore) {
	t.Helper()
	conn := dbtest.GetTestDB(t)
	for _, statement := range []string{
		`INSERT INTO organization (id, org_id, name) VALUES (1, 'reboot-reconciliation', 'Reboot reconciliation')`,
		`INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active, firmware_version) VALUES (1, 1, 'reboot-rig', 'proto', 'proto', 'proto', '192.0.2.216', '8080', 'http', TRUE, '1.5.0')`,
		`INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address) VALUES (1, 1, 1, 'reboot-rig', '02:00:00:00:02:16')`,
		`INSERT INTO device_pairing (device_id, pairing_status, paired_at) VALUES (1, 'PAIRED', NOW())`,
	} {
		_, err := conn.ExecContext(t.Context(), statement)
		require.NoError(t, err)
	}
	_, err := conn.ExecContext(t.Context(), `INSERT INTO device_status (device_id, status, status_timestamp) VALUES (1, 'REBOOT_REQUIRED', $1)`, statusAt)
	require.NoError(t, err)
	return conn, sqlstores.NewSQLDeviceStore(conn)
}

func TestTelemetryReconcilesExternalReboot(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	now := time.Now()
	for _, test := range []struct {
		name       string
		statusAt   time.Time
		uptime     *uint64
		observedAt time.Time
		want       mm.MinerStatus
		health     modelsV2.HealthStatus
		statusErr  error
	}{
		{name: "reboot immediately after installation", statusAt: now.Add(-10 * time.Second), uptime: new(uint64(7)), observedAt: now.Add(-time.Second), want: mm.MinerStatusActive},
		{name: "same version reboot", statusAt: now.Add(-time.Hour), uptime: new(uint64(5)), observedAt: now, want: mm.MinerStatusActive},
		{name: "zero uptime", statusAt: now.Add(-10 * time.Second), uptime: new(uint64(0)), observedAt: now, want: mm.MinerStatusActive},
		{name: "missing uptime", statusAt: now.Add(-time.Hour), observedAt: now, want: mm.MinerStatusRebootRequired},
		{name: "old boot", statusAt: now.Add(-time.Hour), uptime: new(uint64(7200)), observedAt: now, want: mm.MinerStatusRebootRequired},
		{name: "cached pre-install sample", statusAt: now.Add(-2 * time.Minute), uptime: new(uint64(1)), observedAt: now.Add(-5 * time.Minute), want: mm.MinerStatusRebootRequired},
		{name: "boot overlaps installation timestamp", statusAt: now.Add(-5 * time.Second), uptime: new(uint64(5)), observedAt: now, want: mm.MinerStatusRebootRequired},
		{name: "idle miner status is refined", statusAt: now.Add(-time.Minute), uptime: new(uint64(0)), observedAt: now, health: modelsV2.HealthHealthyInactive, want: mm.MinerStatusInactive},
		{name: "missing pool status is refined", statusAt: now.Add(-time.Minute), uptime: new(uint64(0)), observedAt: now, health: modelsV2.HealthHealthyInactive, want: mm.MinerStatusNeedsMiningPool},
		{name: "completed reboot with unresolved mining state", statusAt: now.Add(-time.Minute), uptime: new(uint64(0)), observedAt: now, health: modelsV2.HealthHealthyInactive, statusErr: errors.New("status unavailable"), want: mm.MinerStatusUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn, deviceStore := newRebootTestDevice(t, test.statusAt)
			ctrl := gomock.NewController(t)
			getter := mock.NewMockCachedMinerGetter(ctrl)
			scheduler := mock.NewMockUpdateScheduler(ctrl)
			miner := minerMocks.NewMockMiner(ctrl)
			lookups := 1
			if test.health == modelsV2.HealthHealthyInactive {
				lookups++
				miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(test.want, test.statusErr)
			} else {
				test.health = modelsV2.HealthHealthyActive
			}
			getter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), models.DeviceIdentifier("reboot-rig")).Return(miner, nil).Times(lookups)
			miner.EXPECT().GetOrgID().Return(int64(1)).Times(lookups)
			miner.EXPECT().GetSiteID().Return(int64(0)).Times(lookups)
			miner.EXPECT().GetDriverName().Return("proto").Times(lookups)
			miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{
				DeviceIdentifier: "reboot-rig", Timestamp: now, Health: test.health,
				FirmwareVersion: "1.5.0", UptimeSeconds: test.uptime, UptimeObservedAt: test.observedAt,
			}, nil)
			scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil)
			service := NewTelemetryService(Config{ConcurrencyLimit: 1, MetricTimeout: time.Second, StatusFlushInterval: time.Hour}, nil, getter, scheduler, deviceStore, nil)
			activation := telemetryActivationForTest(service)
			err := service.processDevice(t.Context(), models.Device{ID: "reboot-rig"}, activation.results)
			if test.statusErr != nil {
				require.ErrorIs(t, err, test.statusErr)
			} else {
				require.NoError(t, err)
			}
			// A canceled writer drains the queued status synchronously, exercising
			// the ordinary firmware guard after the reboot reconciliation.
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			service.statusWriterRoutine(ctx, activation)
			statuses, err := deviceStore.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
			require.NoError(t, err)
			require.Equal(t, test.want, statuses["reboot-rig"])
			var firmware string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT firmware_version FROM discovered_device WHERE id = 1`).Scan(&firmware))
			require.Equal(t, "1.5.0", firmware, "reboot evidence must work without a firmware version change")
		})
	}
}

func TestRebootReconciliationUsesBootBoundAcrossCachedObservations(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	now := time.Now()
	_, store := newRebootTestDevice(t, now.Add(-20*time.Second))
	service := &TelemetryService{deviceStore: store}
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", modelsV2.DeviceMetrics{
		UptimeSeconds: new(uint64(120)), UptimeObservedAt: now.Add(-5 * time.Second),
	}, mm.MinerStatusActive)
	statuses, err := store.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
	require.NoError(t, err)
	require.Equal(t, mm.MinerStatusRebootRequired, statuses["reboot-rig"])
	// A later Fleet Node response can have an earlier conservative observation
	// time because of network delay. Its lower uptime still proves a newer boot.
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", modelsV2.DeviceMetrics{
		UptimeSeconds: new(uint64(0)), UptimeObservedAt: now.Add(-6 * time.Second),
	}, mm.MinerStatusActive)
	statuses, err = store.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
	require.NoError(t, err)
	require.Equal(t, mm.MinerStatusActive, statuses["reboot-rig"])
}

func TestRebootReconciliationFollowsStatusWriterReads(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	now := time.Now()
	_, store := newRebootTestDevice(t, now.Add(-time.Minute))
	require.NoError(t, store.UpsertDeviceStatus(t.Context(), "reboot-rig", mm.MinerStatusActive, ""))
	service := NewTelemetryService(Config{ConcurrencyLimit: 1, MetricTimeout: time.Second, StatusFlushInterval: time.Hour}, nil, nil, nil, store, nil)
	flushStatus := func() {
		activation := telemetryActivationForTest(service)
		activation.results.status <- statusResult{deviceIdentifier: "reboot-rig", status: mm.MinerStatusActive}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		service.statusWriterRoutine(ctx, activation)
	}
	requireStatus := func(want mm.MinerStatus) {
		t.Helper()
		statuses, err := store.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
		require.NoError(t, err)
		require.Equal(t, want, statuses["reboot-rig"])
	}
	metrics := modelsV2.DeviceMetrics{UptimeSeconds: new(uint64(0)), UptimeObservedAt: now}

	flushStatus()
	// The install starts after the writer last read ACTIVE, and the rig reboots
	// before the writer reads the pending status again.
	require.NoError(t, store.MarkFirmwareRebootRequiredStatus(t.Context(), "reboot-rig", now.Add(-time.Minute)))
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", metrics, mm.MinerStatusActive)
	requireStatus(mm.MinerStatusRebootRequired)

	flushStatus()
	requireStatus(mm.MinerStatusRebootRequired)
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", metrics, mm.MinerStatusActive)
	requireStatus(mm.MinerStatusActive)
}

func TestRebootReconciliationSurvivesFailedStatusRefinement(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	for _, test := range []struct {
		name                  string
		storedStatus          mm.MinerStatus
		uptime                uint64
		readPendingBeforePoll bool
		want                  mm.MinerStatus
	}{
		{name: "reboot marker written after last active read", storedStatus: mm.MinerStatusRebootRequired, want: mm.MinerStatusUnknown},
		{name: "reboot marker already observed", storedStatus: mm.MinerStatusRebootRequired, readPendingBeforePoll: true, want: mm.MinerStatusUnknown},
		{name: "old boot preserves pending reboot", storedStatus: mm.MinerStatusRebootRequired, uptime: 7200, want: mm.MinerStatusRebootRequired},
		{name: "installation remains protected", storedStatus: mm.MinerStatusUpdating, want: mm.MinerStatusUpdating},
		{name: "ordinary status remains unchanged", storedStatus: mm.MinerStatusActive, want: mm.MinerStatusActive},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			_, store := newRebootTestDevice(t, now.Add(-time.Minute))
			require.NoError(t, store.UpsertDeviceStatus(t.Context(), "reboot-rig", mm.MinerStatusActive, ""))
			ctrl := gomock.NewController(t)
			getter := mock.NewMockCachedMinerGetter(ctrl)
			scheduler := mock.NewMockUpdateScheduler(ctrl)
			miner := minerMocks.NewMockMiner(ctrl)
			statusErr := errors.New("explicit status unavailable")
			const polls = 2
			getter.EXPECT().GetMinerFromDeviceIdentifier(gomock.Any(), models.DeviceIdentifier("reboot-rig")).Return(miner, nil).Times(polls * 2)
			miner.EXPECT().GetOrgID().Return(int64(1)).Times(polls * 2)
			miner.EXPECT().GetSiteID().Return(int64(0)).Times(polls * 2)
			miner.EXPECT().GetDriverName().Return("proto").Times(polls * 2)
			miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{
				DeviceIdentifier: "reboot-rig", Timestamp: now,
				Health:        modelsV2.HealthHealthyInactive,
				UptimeSeconds: &test.uptime, UptimeObservedAt: now,
			}, nil).Times(polls)
			miner.EXPECT().GetDeviceStatus(gomock.Any()).Return(mm.MinerStatusUnknown, statusErr).Times(polls)
			scheduler.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil).Times(polls)
			service := NewTelemetryService(Config{ConcurrencyLimit: 1, MetricTimeout: time.Second, StatusFlushInterval: time.Hour}, nil, getter, scheduler, store, nil)
			activation := telemetryActivationForTest(service)
			flushStatus := func() {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				service.statusWriterRoutine(ctx, activation)
			}
			// Populate the ordinary-status cache through the actual writer before
			// the command records its pending firmware state.
			activation.results.status <- statusResult{deviceIdentifier: "reboot-rig", status: mm.MinerStatusActive}
			flushStatus()
			if test.storedStatus == mm.MinerStatusRebootRequired {
				require.NoError(t, store.MarkFirmwareRebootRequiredStatus(t.Context(), "reboot-rig", now.Add(-time.Minute)))
			} else {
				require.NoError(t, store.UpsertDeviceStatus(t.Context(), "reboot-rig", test.storedStatus, ""))
			}
			if test.readPendingBeforePoll {
				activation.results.status <- statusResult{deviceIdentifier: "reboot-rig", status: mm.MinerStatusActive}
				flushStatus()
			}
			// The ambiguous metrics carry valid reboot evidence, but the separate
			// status RPC fails before a status result reaches the writer. A second
			// successful metrics poll must still complete a proven reboot.
			for range polls {
				err := service.processDevice(t.Context(), models.Device{ID: "reboot-rig"}, activation.results)
				require.ErrorIs(t, err, statusErr)
				flushStatus()
			}
			statuses, err := store.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
			require.NoError(t, err)
			require.Equal(t, test.want, statuses["reboot-rig"], "boot completion must not depend on successful mining-status refinement")
		})
	}
}

func TestRebootReconciliationRetriesDatabaseFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	now := time.Now()
	_, store := newRebootTestDevice(t, now.Add(-time.Minute))
	service := &TelemetryService{deviceStore: store}
	metrics := modelsV2.DeviceMetrics{UptimeSeconds: new(uint64(0)), UptimeObservedAt: now}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service.reconcileRebootRequiredState(ctx, "reboot-rig", metrics, mm.MinerStatusActive)
	_, cached := service.lastReconciledBoot.Load(models.DeviceIdentifier("reboot-rig"))
	require.False(t, cached)
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", metrics, mm.MinerStatusActive)
	statuses, err := store.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
	require.NoError(t, err)
	require.Equal(t, mm.MinerStatusActive, statuses["reboot-rig"])
}

func TestRebootReconciliationRetriesCachedEvidenceAfterUpdating(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	now := time.Now()
	_, store := newRebootTestDevice(t, now.Add(-time.Minute))
	require.NoError(t, store.UpsertDeviceStatus(t.Context(), "reboot-rig", mm.MinerStatusUpdating, ""))
	service := &TelemetryService{deviceStore: store}
	metrics := modelsV2.DeviceMetrics{UptimeSeconds: new(uint64(5)), UptimeObservedAt: now}
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", metrics, mm.MinerStatusActive)
	_, cached := service.lastReconciledBoot.Load(models.DeviceIdentifier("reboot-rig"))
	require.False(t, cached, "a no-op during installation must not consume the boot evidence")
	// The command discovers post-boot confirmation after telemetry already saw
	// this sample. The boundary predates the boot, but the status timestamp does not.
	require.NoError(t, store.MarkFirmwareRebootRequiredStatus(t.Context(), "reboot-rig", now.Add(-time.Minute)))
	service.reconcileRebootRequiredState(t.Context(), "reboot-rig", metrics, mm.MinerStatusActive)
	statuses, err := store.GetDeviceStatusForDeviceIdentifiers(t.Context(), []models.DeviceIdentifier{"reboot-rig"})
	require.NoError(t, err)
	require.Equal(t, mm.MinerStatusActive, statuses["reboot-rig"])
}
