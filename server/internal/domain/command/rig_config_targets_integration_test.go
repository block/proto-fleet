package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	commandmocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
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
	seedRigConfigDevice(t, conn, 1, "ct1", "Proto", sqlc.PairingStatusEnumPAIRED)
	_, err := conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class = $1, model = 'CT1' WHERE device_identifier = 'ct1'`, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	require.NoError(t, err)
	deletedID := seedRigConfigDevice(t, conn, 1, "deleted", "Proto", sqlc.PairingStatusEnumPAIRED)
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET deleted_at = NOW() WHERE id = $1`, deletedID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO organization (id, org_id, name) VALUES (2, 'other-org', 'Other')`)
	require.NoError(t, err)
	seedRigConfigDevice(t, conn, 2, "foreign-rig", "Proto", sqlc.PairingStatusEnumPAIRED)

	ctx := manualSessionCtx(1)
	config := sdk.CurtailmentConfig{Enabled: true}
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, config, []string{
		requested, requested, "bitmain", "needs-auth", "default-password", "unpaired", "ct1", "deleted", "foreign-rig", "missing",
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

func TestApplyCurtailmentConfigToProtoRigsExcludesContainers(t *testing.T) {
	svc, conn, legacyRigID, _ := newRigConfigCommandFixture(t)
	rigID := seedRigConfigDevice(t, conn, 1, "rig", "Proto", sqlc.PairingStatusEnumPAIRED)
	seedRigConfigDevice(t, conn, 1, "ct1", "Proto", sqlc.PairingStatusEnumPAIRED)
	_, err := conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class = CASE device_identifier WHEN 'ct1' THEN $1::integer ELSE $2::integer END WHERE device_identifier IN ('rig', 'ct1')`, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, err)

	// No sources still sends a disabled config to clear stale rig settings.
	require.NoError(t, svc.ApplyCurtailmentConfigToProtoRigs(manualSessionCtx(1), sdk.CurtailmentConfig{}))
	rows, err := conn.QueryContext(t.Context(), `SELECT device_id FROM queue_message ORDER BY device_id`)
	require.NoError(t, err)
	defer rows.Close()
	var queued []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		queued = append(queued, id)
	}
	require.NoError(t, rows.Err())
	require.ElementsMatch(t, []int64{legacyRigID, rigID}, queued)
}

func TestApplyCurtailmentConfigToDevicesHoldsEligibilityThroughEnqueue(t *testing.T) {
	changes := []struct {
		name  string
		query string
	}{
		{"unpair", "UPDATE device_pairing SET pairing_status = 'UNPAIRED' WHERE device_id = $1"},
		{"manufacturer", "UPDATE discovered_device SET manufacturer = 'Bitmain' WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)"},
		{"delete", "UPDATE device SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1"},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			svc, conn, deviceID, identifier := newRigConfigCommandFixture(t)
			ctx := manualSessionCtx(1)
			reachedResolver := make(chan struct{})
			releaseResolver := make(chan struct{})
			defer func() {
				select {
				case <-releaseResolver:
				default:
					close(releaseResolver)
				}
			}()
			svc.resolveDevicesOverride = func(context.Context, []string) ([]resolvedDevice, error) {
				close(reachedResolver)
				<-releaseResolver
				return []resolvedDevice{{id: deviceID, identifier: identifier}}, nil
			}
			dispatched := make(chan error, 1)
			go func() {
				dispatched <- svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{identifier})
			}()
			select {
			case <-reachedResolver:
			case <-time.After(5 * time.Second):
				t.Fatal("dispatch did not reach device resolution")
			}

			writer, err := conn.Conn(ctx)
			require.NoError(t, err)
			defer func() {
				select {
				case <-releaseResolver:
				default:
					close(releaseResolver)
				}
				_ = writer.Close()
			}()
			var writerPID int
			require.NoError(t, writer.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&writerPID))
			changed := make(chan error, 1)
			go func() {
				_, err := writer.ExecContext(ctx, change.query, deviceID)
				changed <- err
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := conn.QueryRowContext(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", writerPID).Scan(&blocked)
				return err == nil && blocked
			}, 5*time.Second, 10*time.Millisecond, "eligibility change must wait for dispatch")
			select {
			case err := <-changed:
				t.Fatalf("eligibility changed before enqueue: %v", err)
			default:
			}
			close(releaseResolver)
			select {
			case err := <-dispatched:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("dispatch did not finish")
			}
			select {
			case err := <-changed:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("eligibility change did not finish after dispatch")
			}
			var queuedID int64
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT device_id FROM queue_message").Scan(&queuedID))
			require.Equal(t, deviceID, queuedID)
			assertCommandDispatchRows(t, conn, 1)
		})
	}
}

func TestRigConfigTerminalFailureRequeuesOnlyFailedDevice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		requeue bool
	}{
		{"worker", errors.New("device unavailable"), true},
		{"reaper", nil, true},
		{"unsupported", fmt.Errorf("device: %w", fleeterror.NewUnimplementedError("curtailment configuration")), false},
		{"failed precondition", fleeterror.NewFailedPreconditionError("invalid config"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
				WHERE device_id = $1 RETURNING id, command_batch_log_uuid, payload`, failedID).Scan(&message.ID, &message.BatchLogUUID, &message.Payload))

			execution := svc.executionService
			if tc.name != "reaper" {
				ctrl := gomock.NewController(t)
				getter := commandmocks.NewMockCachedMinerGetter(ctrl)
				miner := minerIfaceMocks.NewMockMiner(ctrl)
				miner.EXPECT().GetOrgID().Return(int64(1))
				miner.EXPECT().GetSiteID().Return(int64(0))
				handle := &fencedCurtailmentConfigMiner{MockMiner: miner, configure: func(context.Context, dto.ApplyCurtailmentConfigPayload) error { return tc.err }}
				getter.EXPECT().GetMinerForCommand(gomock.Any(), failedID).Return(handle, nil)
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
			if tc.requeue {
				assert.Equal(t, int64(2), desired)
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_id FROM curtailment_rig_config_target`).Scan(&targetID))
				assert.Equal(t, failedID, targetID)
			} else {
				assert.Equal(t, int64(1), desired)
			}
			assert.Equal(t, int64(1), enqueued)
			assert.LessOrEqual(t, fullReconcileGeneration, enqueued, "terminal retry must not request another full reconciliation")
			var targetCount int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM curtailment_rig_config_target`).Scan(&targetCount))
			if tc.requeue {
				assert.Equal(t, 1, targetCount)
			} else {
				assert.Zero(t, targetCount)
			}
			var failedStatus, otherStatus string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM queue_message WHERE device_id = $1`, failedID).Scan(&failedStatus))
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM queue_message WHERE device_id = $1`, otherID).Scan(&otherStatus))
			assert.Equal(t, "FAILED", failedStatus)
			assert.Equal(t, "PENDING", otherStatus)
		})
	}
}

func TestRigConfigBindingChangeRetriesWithFreshHandle(t *testing.T) {
	for _, change := range []string{"endpoint", "site", "credentials", "backend generation"} {
		t.Run(change, func(t *testing.T) {
			t.Chdir(t.TempDir())
			svc, conn, deviceID, identifier := newRigConfigCommandFixture(t)
			ctx := manualSessionCtx(1)
			q := sqlc.New(conn)
			_, err := conn.ExecContext(ctx, `INSERT INTO curtailment_rig_config_reconciliation
				(organization_id, requested_by, desired_generation, enqueued_generation) VALUES (1, 42, 1, 1)`)
			require.NoError(t, err)
			require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{identifier}))
			ctrl := gomock.NewController(t)
			device := &struct {
				*sdkMocks.MockDevice
				*sdkMocks.MockDeviceCurtailmentConfigurator
			}{sdkMocks.NewMockDevice(ctrl), sdkMocks.NewMockDeviceCurtailmentConfigurator(ctrl)}
			writes, resolved := 0, 0
			device.MockDeviceCurtailmentConfigurator.EXPECT().ApplyCurtailmentConfig(gomock.Any(), sdk.CurtailmentConfig{}).
				DoAndReturn(func(context.Context, sdk.CurtailmentConfig) error {
					if change == "backend generation" && resolved == 1 {
						rejected, err := status.New(codes.FailedPrecondition, "SDK backend registration changed").WithDetails(
							&errdetails.ErrorInfo{Reason: "SDK_BACKEND_GENERATION_MISMATCH", Domain: "protofleet.sdk"})
						require.NoError(t, err)
						return rejected.Err()
					}
					writes++
					return nil
				}).AnyTimes()
			driver := sdkMocks.NewMockDriver(ctrl)
			driver.EXPECT().NewDevice(gomock.Any(), identifier, gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).AnyTimes()
			fileService, err := files.NewService(files.Config{})
			require.NoError(t, err)
			miners := minerDomain.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), svc.encryptService, fileService,
				&commandTestPluginManager{driver: driver, driverName: "proto"})
			getter := commandmocks.NewMockCachedMinerGetter(ctrl)
			getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).DoAndReturn(func(ctx context.Context, id int64) (interfaces.Miner, error) {
				handle, err := miners.GetMinerForCommand(ctx, id)
				require.NoError(t, err)
				resolved++
				if resolved == 1 {
					switch change {
					case "endpoint":
						err = q.UpdateDeviceIPAssignment(ctx, sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.44", Port: "443", UrlScheme: "https"})
					case "site":
						_, err = conn.ExecContext(ctx, `WITH new_site AS (INSERT INTO site (org_id, name, slug, timezone) VALUES (1, 'new-site', 'new-site', 'UTC') RETURNING id) UPDATE device SET site_id = new_site.id FROM new_site WHERE device.id = $1`, deviceID)
					case "credentials":
						username, encryptErr := svc.encryptService.Encrypt([]byte("new-user"))
						require.NoError(t, encryptErr)
						password, encryptErr := svc.encryptService.Encrypt([]byte("new-password"))
						require.NoError(t, encryptErr)
						err = q.UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: username, PasswordEnc: password})
					}
					require.NoError(t, err)
				}
				return handle, nil
			}).Times(2)
			svc.executionService.minerService = getter
			processNext := func() {
				message := queue.Message{OrgID: 1, DeviceID: deviceID, CommandType: commandtype.ApplyCurtailmentConfig}
				require.NoError(t, conn.QueryRowContext(ctx, `UPDATE queue_message SET status = 'PROCESSING' WHERE device_id = $1 AND status = 'PENDING' RETURNING id, command_batch_log_uuid, payload`, deviceID).
					Scan(&message.ID, &message.BatchLogUUID, &message.Payload))
				svc.executionService.workerProcessCommand(ctx, message)
			}
			processNext()
			require.Zero(t, writes, "stale binding must not dispatch")
			var generation int64
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT desired_generation FROM curtailment_rig_config_reconciliation WHERE organization_id = 1`).Scan(&generation))
			require.Equal(t, int64(2), generation, "binding change must preserve reconciliation")
			_, err = conn.ExecContext(ctx, `UPDATE curtailment_rig_config_reconciliation SET retry_at = CURRENT_TIMESTAMP WHERE organization_id = 1`)
			require.NoError(t, err)
			claim, err := q.ClaimRigConfigReconciliation(ctx)
			require.NoError(t, err)
			targets, err := q.ListRigConfigReconciliationTargets(ctx, sqlc.ListRigConfigReconciliationTargetsParams{
				OrganizationID: 1, EnqueuedGeneration: claim.EnqueuedGeneration, DesiredGeneration: claim.DesiredGeneration,
				ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
			})
			require.NoError(t, err)
			require.Equal(t, []string{identifier}, targets)
			require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, targets))
			require.NoError(t, q.CompleteRigConfigReconciliation(ctx, sqlc.CompleteRigConfigReconciliationParams{OrganizationID: 1, EnqueuedGeneration: claim.DesiredGeneration}))
			processNext()
			require.Equal(t, 1, writes, "fresh handle must deliver exactly once")
			var successes int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message WHERE status = 'SUCCESS'`).Scan(&successes))
			require.Equal(t, 1, successes)
		})
	}
}

func TestRigConfigRecoveryDoesNotReplayUnresolvedWrites(t *testing.T) {
	for _, tc := range []struct {
		name    string
		finish  bool
		outcome error
		requeue bool
	}{
		{name: "running", outcome: devicewrite.UncertainDelivery(context.DeadlineExceeded)},
		{name: "unknown after completion", finish: true, outcome: devicewrite.UncertainDelivery(context.DeadlineExceeded)},
		{name: "definite refusal", finish: true, outcome: devicewrite.NotDispatched(errors.New("preparation failed")), requeue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, conn, deviceID, identifier := newRigConfigCommandFixture(t)
			ctx := manualSessionCtx(1)
			_, err := conn.ExecContext(ctx, `INSERT INTO curtailment_rig_config_reconciliation
				(organization_id, requested_by, desired_generation, enqueued_generation) VALUES (1, 42, 1, 1)`)
			require.NoError(t, err)
			require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{identifier}))
			message := queue.Message{OrgID: 1, DeviceID: deviceID, CommandType: commandtype.ApplyCurtailmentConfig}
			require.NoError(t, conn.QueryRowContext(ctx, `UPDATE queue_message SET status = 'PROCESSING'
				WHERE device_id = $1 RETURNING id, command_batch_log_uuid, payload, write_epoch`, deviceID).
				Scan(&message.ID, &message.BatchLogUUID, &message.Payload, &message.WriteEpoch))
			attempt, err := devicewrite.NewService(conn).BeginQueuedHost(ctx, message)
			require.NoError(t, err)
			outcome := devicewrite.Dispatch(attempt.Context(ctx), func(context.Context) error { return tc.outcome })
			require.Error(t, outcome)
			if tc.finish {
				require.NoError(t, attempt.Finish(ctx, outcome))
			}
			// The attempt has committed, but the worker never records the queue
			// result. Restart recovery must use that durable physical evidence.
			before, err := sqlc.New(conn).ListUnresolvedDeviceWriteAttempts(ctx, sqlc.ListUnresolvedDeviceWriteAttemptsParams{OrganizationID: 1, DeviceID: deviceID})
			require.NoError(t, err)
			if tc.requeue {
				require.Empty(t, before)
			} else {
				require.Len(t, before, 1)
				if tc.finish {
					require.Equal(t, "unknown", before[0].Status)
				} else {
					require.Equal(t, "running", before[0].Status)
				}
			}
			reaped, err := svc.executionService.reapMessages(ctx, reapModeRestart)
			require.NoError(t, err)
			require.Len(t, reaped, 1)
			var queuedStatus string
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT status FROM queue_message WHERE id=$1", message.ID).Scan(&queuedStatus))
			require.Equal(t, "FAILED", queuedStatus)
			var targets int
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM curtailment_rig_config_target WHERE organization_id=1 AND device_id=$1", deviceID).Scan(&targets))
			if tc.requeue {
				require.Equal(t, 1, targets, "a known preparation refusal can retry")
			} else {
				require.Zero(t, targets, "recovery must not replay possibly accepted work")
			}
			after, err := sqlc.New(conn).ListUnresolvedDeviceWriteAttempts(ctx, sqlc.ListUnresolvedDeviceWriteAttemptsParams{OrganizationID: 1, DeviceID: deviceID})
			require.NoError(t, err)
			require.Equal(t, before, after, "queue recovery must retain physical attempt evidence")
		})
	}
}

func TestRigConfigRecoverySeesAdmissionCommittedWhileWaiting(t *testing.T) {
	svc, conn, deviceID, identifier := newRigConfigCommandFixture(t)
	ctx := manualSessionCtx(1)
	_, err := conn.ExecContext(ctx, `INSERT INTO curtailment_rig_config_reconciliation
		(organization_id, requested_by, desired_generation, enqueued_generation) VALUES (1, 42, 1, 1)`)
	require.NoError(t, err)
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{identifier}))
	var messageID, epoch int64
	require.NoError(t, conn.QueryRowContext(ctx, `UPDATE queue_message SET status='PROCESSING'
		WHERE device_id=$1 RETURNING id,write_epoch`, deviceID).Scan(&messageID, &epoch))
	keys, err := sqlc.New(conn).GetDeviceWriteIdentityKeys(ctx, deviceID)
	require.NoError(t, err)

	// Match admission's queue lock and attempt insert, then delay its commit
	// until the reaper has taken its first statement snapshot and is waiting.
	admission, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = admission.Rollback() }()
	var ownerPID, lockedID int64
	require.NoError(t, admission.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&ownerPID))
	require.NoError(t, admission.QueryRowContext(ctx, "SELECT id FROM queue_message WHERE id=$1 FOR UPDATE", messageID).Scan(&lockedID))
	require.Equal(t, messageID, lockedID)
	kind := commandtype.ApplyCurtailmentConfig
	require.NoError(t, sqlc.New(admission).CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{
		AttemptID: uuid.New(), DeviceID: deviceID, OrganizationID: 1, Epoch: epoch,
		QueueMessageID: sql.NullInt64{Int64: messageID, Valid: true}, OwnerInstanceID: uuid.New(),
		OperationKind: "mutation", IdentityKeys: keys, CommandType: kind.String(),
		CurtailmentCommand: sql.NullString{String: kind.String(), Valid: true},
	}))
	done := make(chan error, 1)
	go func() {
		_, err := svc.executionService.reapMessages(ctx, reapModeRestart)
		done <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", ownerPID).Scan(&blocked)
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond, "recovery must wait for admission's queue lock")
	require.NoError(t, admission.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not finish")
	}
	var targets int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM curtailment_rig_config_target WHERE organization_id=1 AND device_id=$1", deviceID).Scan(&targets))
	require.Zero(t, targets, "recovery must see the attempt committed while it waited")
}

func TestRigConfigLateWorkerRequeuesOnlySettledFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome error
		requeue bool
	}{
		{name: "safe refusal", outcome: devicewrite.NotDispatched(fleeterror.NewUnavailableErrorf("preparation failed")), requeue: true},
		{name: "unknown", outcome: devicewrite.UncertainDelivery(context.DeadlineExceeded)},
		{name: "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, conn, deviceID, identifier := newRigConfigCommandFixture(t)
			ctx := manualSessionCtx(1)
			_, err := conn.ExecContext(ctx, `INSERT INTO curtailment_rig_config_reconciliation
				(organization_id, requested_by, desired_generation, enqueued_generation) VALUES (1, 42, 1, 1)`)
			require.NoError(t, err)
			require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{identifier}))
			messages, err := svc.messageQueue.Dequeue(ctx, 1)
			require.NoError(t, err)
			require.Len(t, messages, 1)
			message := messages[0]
			entered, release := make(chan struct{}), make(chan struct{})
			resume := sync.OnceFunc(func() { close(release) })
			t.Cleanup(resume)
			ctrl := gomock.NewController(t)
			getter := commandmocks.NewMockCachedMinerGetter(ctrl)
			miner := minerIfaceMocks.NewMockMiner(ctrl)
			miner.EXPECT().GetOrgID().Return(int64(1))
			miner.EXPECT().GetSiteID().Return(int64(0))
			handle := &fencedCurtailmentConfigMiner{MockMiner: miner, configure: func(ctx context.Context, _ dto.ApplyCurtailmentConfigPayload) error {
				return devicewrite.Dispatch(ctx, func(context.Context) error {
					close(entered)
					<-release
					return tc.outcome
				})
			}}
			getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(handle, nil)
			svc.executionService.minerService = getter
			done := make(chan struct{})
			go func() {
				svc.executionService.workerProcessCommand(ctx, message)
				close(done)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not enter dispatch")
			}
			reaped, err := svc.executionService.reapMessages(ctx, reapModeRestart)
			require.NoError(t, err)
			require.Len(t, reaped, 1)
			before, err := sqlc.New(conn).GetQueueMessagesByBatch(ctx, message.BatchLogUUID)
			require.NoError(t, err)
			readAudit := func() string {
				var audit string
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT cdl.error_info FROM command_on_device_log cdl
					JOIN command_batch_log cbl ON cbl.id=cdl.command_batch_log_id
					WHERE cbl.uuid=$1 AND cdl.device_id=$2`, message.BatchLogUUID, deviceID).Scan(&audit))
				return audit
			}
			auditBefore := readAudit()
			resume()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("late worker did not finish")
			}
			after, err := sqlc.New(conn).GetQueueMessagesByBatch(ctx, message.BatchLogUUID)
			require.NoError(t, err)
			require.Equal(t, before, after, "late completion must preserve the reaper's queue result")
			require.Equal(t, auditBefore, readAudit(), "late completion must preserve the reaper's audit result")
			var generation, targets int64
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT desired_generation,
				(SELECT count(*) FROM curtailment_rig_config_target WHERE organization_id=1 AND device_id=$1)
				FROM curtailment_rig_config_reconciliation WHERE organization_id=1`, deviceID).Scan(&generation, &targets))
			if tc.requeue {
				require.Equal(t, int64(2), generation, "late safe failure must request exactly one retry")
				require.Equal(t, int64(1), targets)
				// A competing recovery still has the original dequeued retry count.
				// Its claim must not request another generation for this failure.
				require.NoError(t, db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
					return requeueSettledRigConfigMessages(ctx, q, []queue.Message{message})
				}))
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT desired_generation FROM curtailment_rig_config_reconciliation WHERE organization_id=1`).Scan(&generation))
				require.Equal(t, int64(2), generation)
			} else {
				require.Equal(t, int64(1), generation)
				require.Zero(t, targets)
			}
		})
	}
}

func TestRigConfigReaperAndLateWorkerShareRetryClaim(t *testing.T) {
	svc, conn, deviceID, identifier := newRigConfigCommandFixture(t)
	ctx := manualSessionCtx(1)
	_, err := conn.ExecContext(ctx, `INSERT INTO curtailment_rig_config_reconciliation
		(organization_id, requested_by, desired_generation, enqueued_generation) VALUES (1, 42, 1, 1)`)
	require.NoError(t, err)
	require.NoError(t, svc.ApplyCurtailmentConfigToDevices(ctx, sdk.CurtailmentConfig{}, []string{identifier}))
	messages, err := svc.messageQueue.Dequeue(ctx, 1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	message := messages[0]
	require.Equal(t, deviceID, message.DeviceID)
	attempt, err := devicewrite.NewService(conn).BeginQueuedHost(ctx, message)
	require.NoError(t, err)
	failure := devicewrite.Dispatch(attempt.Context(ctx), func(context.Context) error {
		return devicewrite.NotDispatched(errors.New("preparation failed"))
	})
	require.Error(t, failure)
	require.NoError(t, attempt.Finish(ctx, failure))
	reaped, err := svc.executionService.reapMessages(ctx, reapModeRestart)
	require.NoError(t, err)
	require.Len(t, reaped, 1)

	// The reaper saw completion first. Late-worker recoveries must lose the
	// same claim even when they run concurrently with the original retry count.
	start := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			done <- db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
				return requeueSettledRigConfigMessages(ctx, q, []queue.Message{message})
			})
		}()
	}
	close(start)
	for range 2 {
		require.NoError(t, <-done)
	}
	var generation int64
	var retryCount int32
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT desired_generation FROM curtailment_rig_config_reconciliation WHERE organization_id=1`).Scan(&generation))
	require.Equal(t, int64(2), generation, "the reaper and late workers must request one generation")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT retry_count FROM queue_message WHERE id=$1`, message.ID).Scan(&retryCount))
	require.Equal(t, message.RetryCount+1, retryCount, "count the reaped failure once")
}
