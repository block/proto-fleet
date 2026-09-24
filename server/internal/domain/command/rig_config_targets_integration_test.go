package command

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	commandmocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func newRigConfigCommandFixture(t *testing.T) (*Service, *sql.DB, int64, string) {
	t.Helper()
	execution, conn, encryptSvc, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO "user" (id, user_id, username, password_hash)
		VALUES (42, 'rig-config-user', 'rig-config-user', 'unused')`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET manufacturer = 'Proto'`)
	require.NoError(t, err)
	execution.run = newExecutionRun(t.Context())
	t.Cleanup(execution.run.cancelAdmission)
	t.Cleanup(execution.run.cancelWork)
	execution.messageQueue = queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 1}, conn)
	svc := &Service{
		config: &Config{}, conn: conn, executionService: execution,
		messageQueue: execution.messageQueue, encryptService: encryptSvc,
		startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
	}
	return svc, conn, deviceID, identifier
}

func seedRigConfigDevice(t *testing.T, conn *sql.DB, orgID int64, identifier, manufacturer string, status sqlc.PairingStatusEnum) int64 {
	t.Helper()
	q := sqlc.New(conn)
	discoveredID, err := q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID: orgID, DeviceIdentifier: identifier,
		Model: sql.NullString{String: "Rig", Valid: true}, Manufacturer: sql.NullString{String: manufacturer, Valid: true},
		DriverName: "proto", IpAddress: "192.0.2.20", Port: "443", UrlScheme: "https", IsActive: true,
	})
	require.NoError(t, err)
	deviceID, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID: orgID, DiscoveredDeviceID: discoveredID, DeviceIdentifier: identifier,
		MacAddress:   fmt.Sprintf("02:00:00:00:%02x:%02x", (discoveredID>>8)&0xff, discoveredID&0xff),
		SerialNumber: sql.NullString{String: "serial-" + identifier, Valid: true},
	})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: status})
	require.NoError(t, err)
	return deviceID
}

func TestApplyCurtailmentConfigToDevicesOnlyEnqueuesRequestedEligibleRigs(t *testing.T) {
	svc, conn, requestedID, requested := newRigConfigCommandFixture(t)
	seedRigConfigDevice(t, conn, 1, "existing-rig", "Proto", sqlc.PairingStatusEnumPAIRED)
	seedRigConfigDevice(t, conn, 1, "bitmain", "Bitmain", sqlc.PairingStatusEnumPAIRED)
	seedRigConfigDevice(t, conn, 1, "needs-auth", "Proto", sqlc.PairingStatusEnumAUTHENTICATIONNEEDED)
	seedRigConfigDevice(t, conn, 1, "default-password", "Proto", sqlc.PairingStatusEnumDEFAULTPASSWORD)
	seedRigConfigDevice(t, conn, 1, "unpaired", "Proto", sqlc.PairingStatusEnumUNPAIRED)
	deletedID := seedRigConfigDevice(t, conn, 1, "deleted", "Proto", sqlc.PairingStatusEnumPAIRED)
	_, err := conn.ExecContext(t.Context(), `UPDATE device SET deleted_at = NOW() WHERE id = $1`, deletedID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO organization (id, org_id, name) VALUES (2, 'other-org', 'Other')`)
	require.NoError(t, err)
	seedRigConfigDevice(t, conn, 2, "foreign-rig", "Proto", sqlc.PairingStatusEnumPAIRED)

	ctx := manualSessionCtx(1)
	config := sdk.CurtailmentConfig{Enabled: true}
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, config, []string{
		requested, requested, "bitmain", "needs-auth", "default-password", "unpaired", "deleted", "foreign-rig", "missing",
	}))
	var queuedID int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_id FROM queue_message`).Scan(&queuedID))
	assert.Equal(t, requestedID, queuedID)
	assertCommandDispatchRows(t, conn, 1)

	// A target that became ineligible, or an empty target list, must not widen
	// delivery to an existing paired rig or create an empty command batch.
	_, err = conn.ExecContext(ctx, `UPDATE device_pairing SET pairing_status = 'UNPAIRED' WHERE device_id = $1`, requestedID)
	require.NoError(t, err)
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, config, []string{requested}))
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, config, nil))
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, config, []string{}))
	assertCommandDispatchRows(t, conn, 1)
}

func TestRigConfigTerminalFailureRequeuesOnlyFailedDevice(t *testing.T) {
	for _, path := range []string{"worker", "reaper"} {
		t.Run(path, func(t *testing.T) {
			svc, conn, failedID, failedIdentifier := newRigConfigCommandFixture(t)
			otherID := seedRigConfigDevice(t, conn, 1, "other-rig", "Proto", sqlc.PairingStatusEnumPAIRED)
			ctx := manualSessionCtx(1)
			_, err := conn.ExecContext(ctx, `INSERT INTO curtailment_rig_config_reconciliation
				(organization_id, requested_by, desired_generation, enqueued_generation)
				VALUES (1, 42, 1, 1)`)
			require.NoError(t, err)
			require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{failedIdentifier, "other-rig"}))
			message := queue.Message{OrgID: 1, DeviceID: failedID, CommandType: commandtype.ApplyCurtailmentConfig}
			require.NoError(t, conn.QueryRowContext(ctx, `UPDATE queue_message SET status = 'PROCESSING'
				WHERE device_id = $1 RETURNING id, command_batch_log_uuid`, failedID).Scan(&message.ID, &message.BatchLogUUID))

			execution := svc.executionService
			if path == "worker" {
				getter := commandmocks.NewMockCachedMinerGetter(gomock.NewController(t))
				getter.EXPECT().GetMiner(gomock.Any(), failedID).Return(nil, errors.New("device unavailable"))
				execution.minerService = getter
				execution.workerProcessCommand(ctx, message)
			} else {
				execution.config.StuckMessageTimeout = time.Hour
				reaped, reapErr := execution.reapMessages(ctx, reapModeRestart)
				require.NoError(t, reapErr)
				require.Len(t, reaped, 1)
			}

			var desired, enqueued, fullReconcileGeneration, targetID int64
			require.NoError(t, conn.QueryRowContext(ctx, `
				SELECT desired_generation, enqueued_generation, full_reconcile_generation
				FROM curtailment_rig_config_reconciliation WHERE organization_id = 1
			`).Scan(&desired, &enqueued, &fullReconcileGeneration))
			assert.Equal(t, int64(2), desired)
			assert.Equal(t, int64(1), enqueued)
			assert.LessOrEqual(t, fullReconcileGeneration, enqueued, "terminal retry must not request another full reconciliation")
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_id FROM curtailment_rig_config_target`).Scan(&targetID))
			assert.Equal(t, failedID, targetID)
			var targetCount int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM curtailment_rig_config_target`).Scan(&targetCount))
			assert.Equal(t, 1, targetCount)
			var failedStatus, otherStatus string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM queue_message WHERE device_id = $1`, failedID).Scan(&failedStatus))
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM queue_message WHERE device_id = $1`, otherID).Scan(&otherStatus))
			assert.Equal(t, "FAILED", failedStatus)
			assert.Equal(t, "PENDING", otherStatus)
		})
	}
}
