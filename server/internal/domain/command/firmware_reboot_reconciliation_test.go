package command

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type firmwareStatusReportingMiner struct {
	interfaces.Miner
	interfaces.FirmwareUpdateStatusProvider
}

func firmwareBootMetrics(observedAt time.Time, uptime uint64) modelsV2.DeviceMetrics {
	return modelsV2.DeviceMetrics{UptimeSeconds: &uptime, UptimeObservedAt: observedAt}
}

func readFirmwareRebootStatus(t *testing.T, conn *sql.DB, deviceID int64) (models.MinerStatus, time.Time, sql.NullTime) {
	t.Helper()
	var rawStatus string
	var statusAt time.Time
	var requiredAfter sql.NullTime
	require.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT status, status_timestamp, reboot_required_after
		FROM device_status WHERE device_id = $1`, deviceID).Scan(&rawStatus, &statusAt, &requiredAfter))
	status, err := models.MinerStatusUnknown.Parse(strings.ToLower(rawStatus))
	require.NoError(t, err)
	return status, statusAt, requiredAfter
}

func TestFirmwarePostBootStatusRequiresBootEvidence(t *testing.T) {
	tests := []struct {
		name         string
		state        string
		metrics      func(time.Time) modelsV2.DeviceMetrics
		metricsError error
		wantReboot   bool
	}{
		{
			name: "confirming after external reboot", state: "confirming",
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(-time.Second), 5) },
		},
		{
			name: "success after external reboot", state: "success",
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(-time.Second), 5) },
		},
		{
			name: "zero uptime is a valid fresh boot", state: "confirming",
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(-time.Second), 0) },
		},
		{
			name: "API restart with unchanged host boot", state: "confirming", wantReboot: true,
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(-time.Second), 3600) },
		},
		{
			name: "success without a later host boot", state: "success", wantReboot: true,
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(-time.Second), 3600) },
		},
		{
			name: "absent uptime", state: "confirming", wantReboot: true,
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return modelsV2.DeviceMetrics{UptimeObservedAt: now} },
		},
		{
			name: "absent observation time", state: "confirming", wantReboot: true,
			metrics: func(time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(time.Time{}, 0) },
		},
		{
			name: "future observation time", state: "confirming", wantReboot: true,
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(time.Hour), 0) },
		},
		{
			name: "cached observation predates upload", state: "confirming", wantReboot: true,
			metrics: func(now time.Time) modelsV2.DeviceMetrics { return firmwareBootMetrics(now.Add(-time.Minute), 0) },
		},
		{
			name: "overflowing uptime", state: "confirming", wantReboot: true,
			metrics: func(now time.Time) modelsV2.DeviceMetrics {
				return firmwareBootMetrics(now.Add(-time.Second), math.MaxUint64)
			},
		},
		{
			name: "metrics unavailable", state: "confirming", wantReboot: true,
			metricsError: errors.New("miner metrics temporarily unavailable"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
			service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
			devID := tmodels.DeviceIdentifier(identifier)
			now := time.Now().UTC().Truncate(time.Microsecond)
			uploadCompletedAt := now.Add(-30 * time.Second)
			var metrics modelsV2.DeviceMetrics
			if test.metrics != nil {
				metrics = test.metrics(now)
			}
			ctrl := gomock.NewController(t)
			miner := minerMocks.NewMockMiner(ctrl)
			provider := minerMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
			miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(metrics, test.metricsError)
			shouldReboot, err := service.doPollFirmwareInstall(t.Context(), miner, provider, devID, deviceID,
				uploadCompletedAt, &sdk.FirmwareUpdateStatus{State: test.state}, nil)
			require.NoError(t, err)
			require.Equal(t, test.wantReboot, shouldReboot)
			status, statusAt, requiredAfter := readFirmwareRebootStatus(t, conn, deviceID)
			require.True(t, statusAt.After(uploadCompletedAt), "status age must reflect the actual transition")
			if test.wantReboot {
				require.Equal(t, models.MinerStatusRebootRequired, status)
				require.True(t, requiredAfter.Valid)
				require.True(t, uploadCompletedAt.Equal(requiredAfter.Time), "persist the upload boundary for later telemetry")
			} else {
				require.Equal(t, models.MinerStatusUnknown, status, "normal telemetry determines health after boot completion")
				require.False(t, requiredAfter.Valid)
			}
		})
	}
}

func TestFirmwareInstalledRequiresBootAfterInstallationObservation(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	devID := tmodels.DeviceIdentifier(identifier)
	now := time.Now().UTC().Truncate(time.Microsecond)
	uploadCompletedAt := now.Add(-time.Minute)
	ctrl := gomock.NewController(t)
	miner := minerMocks.NewMockMiner(ctrl)
	provider := minerMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
	shouldReboot, err := service.doPollFirmwareInstall(t.Context(), miner, provider, devID, deviceID,
		uploadCompletedAt, &sdk.FirmwareUpdateStatus{State: "installed"}, nil)
	require.NoError(t, err)
	require.True(t, shouldReboot)
	status, statusAt, requiredAfter := readFirmwareRebootStatus(t, conn, deviceID)
	require.Equal(t, models.MinerStatusRebootRequired, status)
	require.False(t, requiredAfter.Valid, "installed must use the current status boundary")
	require.False(t, statusAt.Before(now))
	updated, err := service.deviceStore.ReconcileRebootRequiredStatus(t.Context(), devID, now.Add(-10*time.Second), models.MinerStatusActive)
	require.NoError(t, err)
	require.False(t, updated, "a boot after upload but before installation cannot complete the update")
}

func TestFirmwarePostBootStatusRecoversAfterFailedMetricsAndReboot(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	devID := tmodels.DeviceIdentifier(identifier)
	now := time.Now().UTC().Truncate(time.Microsecond)
	uploadCompletedAt := now.Add(-time.Minute)
	ctrl := gomock.NewController(t)
	miner := minerMocks.NewMockMiner(ctrl)
	provider := minerMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
	miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(modelsV2.DeviceMetrics{}, errors.New("metrics unavailable during reboot"))
	shouldReboot, err := service.doPollFirmwareInstall(t.Context(), miner, provider, devID, deviceID,
		uploadCompletedAt, &sdk.FirmwareUpdateStatus{State: "confirming"}, nil)
	require.NoError(t, err)
	require.True(t, shouldReboot)
	miner.EXPECT().Reboot(gomock.Any()).Return(errors.New("reboot response lost"))
	err = service.rebootAfterFirmwareInstall(t.Context(), miner, deviceID)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "the firmware image must not be uploaded again")
	status, statusAt, requiredAfter := readFirmwareRebootStatus(t, conn, deviceID)
	require.Equal(t, models.MinerStatusRebootRequired, status)
	require.True(t, requiredAfter.Valid)
	require.True(t, uploadCompletedAt.Equal(requiredAfter.Time))
	metrics := firmwareBootMetrics(now.Add(-time.Second), 5)
	bootedAfter, valid := metrics.BootTimeLowerBound(time.Now())
	require.True(t, valid)
	require.True(t, bootedAfter.Before(statusAt), "the external boot occurred before Fleet recorded the pending status")
	updated, err := service.deviceStore.ReconcileRebootRequiredStatus(t.Context(), devID, bootedAfter, models.MinerStatusActive)
	require.NoError(t, err)
	require.True(t, updated)
	status, _, requiredAfter = readFirmwareRebootStatus(t, conn, deviceID)
	require.Equal(t, models.MinerStatusActive, status)
	require.False(t, requiredAfter.Valid)
}

func TestFirmwareFirstStatusProbePreservesUploadBoundary(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	devID := tmodels.DeviceIdentifier(identifier)
	now := time.Now().UTC().Truncate(time.Microsecond)
	uploadCompletedAt := now.Add(-30 * time.Second)
	ctrl := gomock.NewController(t)
	miner := minerMocks.NewMockMiner(ctrl)
	provider := minerMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
	gomock.InOrder(
		provider.EXPECT().GetFirmwareUpdateStatus(gomock.Any()).DoAndReturn(func(context.Context) (*sdk.FirmwareUpdateStatus, error) {
			// The first status request only returns after the external boot. No
			// earlier "installed" observation is available to anchor its time.
			require.True(t, time.Now().After(uploadCompletedAt.Add(20*time.Second)))
			return &sdk.FirmwareUpdateStatus{State: "success"}, nil
		}),
		miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(firmwareBootMetrics(now.Add(-time.Second), 5), nil),
	)
	combined := &firmwareStatusReportingMiner{Miner: miner, FirmwareUpdateStatusProvider: provider}
	shouldReboot, err := service.pollFirmwareInstallStatus(t.Context(), combined, deviceID, uploadCompletedAt)
	require.NoError(t, err)
	require.False(t, shouldReboot, "first-probe time must not replace the earlier upload boundary")
	statuses, err := service.deviceStore.GetDeviceStatusForDeviceIdentifiers(t.Context(), []tmodels.DeviceIdentifier{devID})
	require.NoError(t, err)
	require.Equal(t, models.MinerStatusUnknown, statuses[devID])
}

func TestFirmwareObservedBootSurvivesReconciliationDatabaseFailure(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	devID := tmodels.DeviceIdentifier(identifier)
	now := time.Now().UTC().Truncate(time.Microsecond)
	uploadCompletedAt := now.Add(-time.Minute)
	_, err := conn.ExecContext(t.Context(), `
		CREATE FUNCTION reject_firmware_boot_reconciliation() RETURNS trigger AS $$
		BEGIN
			IF NEW.status = 'UNKNOWN' THEN
				RAISE EXCEPTION 'injected reboot reconciliation write failure';
			END IF;
			RETURN NEW;
		END; $$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_firmware_boot_reconciliation BEFORE UPDATE ON device_status
		FOR EACH ROW EXECUTE FUNCTION reject_firmware_boot_reconciliation()`)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	miner := minerMocks.NewMockMiner(ctrl)
	provider := minerMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
	metrics := firmwareBootMetrics(now.Add(-time.Second), 5)
	miner.EXPECT().GetDeviceMetrics(gomock.Any()).Return(metrics, nil)
	shouldReboot, err := service.doPollFirmwareInstall(t.Context(), miner, provider, devID, deviceID,
		uploadCompletedAt, &sdk.FirmwareUpdateStatus{State: "success"}, nil)
	require.NoError(t, err)
	require.False(t, shouldReboot, "a database write failure must not repeat a proven physical reboot")
	status, _, requiredAfter := readFirmwareRebootStatus(t, conn, deviceID)
	require.Equal(t, models.MinerStatusRebootRequired, status)
	require.True(t, requiredAfter.Valid)
	require.True(t, uploadCompletedAt.Equal(requiredAfter.Time))
	_, err = conn.ExecContext(t.Context(), `DROP TRIGGER reject_firmware_boot_reconciliation ON device_status`)
	require.NoError(t, err)
	bootedAfter, valid := metrics.BootTimeLowerBound(time.Now())
	require.True(t, valid)
	updated, err := service.deviceStore.ReconcileRebootRequiredStatus(t.Context(), devID, bootedAfter, models.MinerStatusActive)
	require.NoError(t, err)
	require.True(t, updated, "later telemetry can retry the same evidence against the preserved boundary")
}
