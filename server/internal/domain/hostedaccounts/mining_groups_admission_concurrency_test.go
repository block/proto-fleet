package hostedaccounts_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/mininggroups"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type admissionMiningGroupDevice struct {
	interfaces.Miner
	calls atomic.Int64
}

func (m *admissionMiningGroupDevice) MiningGroups(context.Context, sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m.calls.Add(1)
	return sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, Groups: []sdk.MiningGroup{{ID: "default"}}}, nil
}

func miningGroupAdmissionService(t *testing.T, f fixture, controller int64) (*mininggroups.Service, context.Context, string) {
	t.Helper()
	pools, _, _, cipher, _ := poolFixture(t, f)
	miner := &admissionMiningGroupDevice{}
	t.Cleanup(func() { require.Zero(t, miner.calls.Load(), "mode admission must use its fresh cached observation") })
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
	var identifier string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, controller).Scan(&identifier))
	// Settle only the fixture's initial pairing work before either real request.
	// Ownership transitions produced by the tested assignment remain intact.
	_, err := f.db.ExecContext(ctx, `DELETE FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing'
	 AND state='pending' AND physical_attempt_id IS NULL AND operation_id IS NULL`, controller)
	require.NoError(t, err)
	return mininggroups.NewService(f.db, poolMinerGetter{miner}, pools, cipher), ctx, identifier
}

func waitForMiningGroupAdmission(t *testing.T, conn *sql.DB, ctx context.Context, blocker int, query string) int {
	t.Helper()
	var waiter int
	require.Eventually(t, func() bool {
		err := conn.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity
		 WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE $2`, blocker, "-- name: "+query+"%").Scan(&waiter)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	return waiter
}

func TestMiningGroupDeactivationAndAllocationSerializeAdmission(t *testing.T) {
	for _, first := range []string{"deactivation", "allocation"} {
		t.Run(first+" first", func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			module, controller := f.miningModule(t)
			asset := f.moduleSelection(t, module)
			service, hostCtx, identifier := miningGroupAdmissionService(t, f, controller)
			ctx, cancel := context.WithTimeout(hostCtx, 10*time.Second)
			defer cancel()
			requestID := uuid.New()
			barrier, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer barrier.Rollback()
			_, err = sqlc.New(barrier).LockPoolConfigurationImportOrganization(ctx, f.org)
			require.NoError(t, err)
			var blocker int
			require.NoError(t, barrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
			deactivation := make(chan error, 1)
			allocation := make(chan error, 1)
			deactivate := func() {
				_, err := service.Deactivate(ctx, identifier, requestID.String())
				deactivation <- err
			}
			allocate := func() {
				allocation <- f.svc.AssignAssets(ctx, f.host, f.org, &pb.AssignAccountAssetsRequest{
					AccountId: f.root, ChildAccountId: client.AccountID,
					Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN,
					Assets: []*pb.MiningAssetSelection{asset},
				})
			}
			// Observe the actual lock queue before releasing admission. The second
			// caller must wait behind the first caller, not merely overlap in time.
			if first == "deactivation" {
				go deactivate()
				firstPID := waitForMiningGroupAdmission(t, f.db, ctx, blocker, "LockPoolConfigurationImportOrganization")
				go allocate()
				waitForMiningGroupAdmission(t, f.db, ctx, firstPID, "LockHostedAccountOrganization")
			} else {
				go allocate()
				firstPID := waitForMiningGroupAdmission(t, f.db, ctx, blocker, "LockHostedAccountOrganization")
				go deactivate()
				waitForMiningGroupAdmission(t, f.db, ctx, firstPID, "LockPoolConfigurationImportOrganization")
			}
			require.NoError(t, barrier.Commit())
			deactivationErr, allocationErr := <-deactivation, <-allocation
			var owners, attempts, requests, ownershipChanges int
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_asset WHERE device_id=$1 AND client_id=$2`, controller, client.ID).Scan(&owners))
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, controller).Scan(&attempts))
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_request WHERE device_id=$1`, controller).Scan(&requests))
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership' AND state='pending'`, controller).Scan(&ownershipChanges))
			state, err := sqlc.New(f.db).GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{DeviceID: controller, OrganizationID: f.org})
			require.NoError(t, err)
			if first == "deactivation" {
				require.NoError(t, deactivationErr)
				require.True(t, fleeterror.IsFailedPreconditionError(allocationErr), "got %v", allocationErr)
				require.ErrorContains(t, allocationErr, "pending mining-group changes")
				require.Zero(t, owners, "rejected allocation must not change any board owner")
				require.Zero(t, ownershipChanges)
				require.Equal(t, 1, attempts)
				require.Equal(t, 1, requests)
				require.Equal(t, "deactivate", state.OperationKind)
				require.Equal(t, "pending", state.Status)
				require.Equal(t, requestID, state.OperationID.UUID)
			} else {
				require.NoError(t, allocationErr)
				require.True(t, fleeterror.IsFailedPreconditionError(deactivationErr), "got %v", deactivationErr)
				require.ErrorContains(t, deactivationErr, "Recall all allocated hashboards")
				require.Equal(t, len(asset.Boards), owners)
				require.Equal(t, len(asset.Boards), ownershipChanges, "allocation cleanup must not be erased to admit deactivation")
				require.Zero(t, attempts, "rejected deactivation must roll back physical admission")
				require.Zero(t, requests)
				require.False(t, state.OperationID.Valid)
			}
		})
	}
}

func TestMiningGroupDeactivationRejectsConcurrentPoolCommandBatch(t *testing.T) {
	f := setup(t)
	_, controller := f.miningModule(t)
	other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	service, hostCtx, identifier := miningGroupAdmissionService(t, f, controller)
	ctx, cancel := context.WithTimeout(hostCtx, 10*time.Second)
	defer cancel()
	q := sqlc.New(f.db)
	require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{other}))
	batch := uuid.NewString()
	kind := commandtype.UpdateMiningPools
	_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
		Uuid: batch, Type: kind.String(), CreatedBy: f.host, CreatedAt: time.Now(),
		Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 2, OrganizationID: sql.NullInt64{Int64: f.org, Valid: true},
	})
	require.NoError(t, err)
	barrier, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer barrier.Rollback()
	_, err = sqlc.New(barrier).LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{DeviceID: controller, OrganizationID: f.org})
	require.NoError(t, err)
	var blocker int
	require.NoError(t, barrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
	deactivation := make(chan error, 1)
	requestID := uuid.New()
	go func() {
		_, err := service.Deactivate(ctx, identifier, requestID.String())
		deactivation <- err
	}()
	// Deactivate owns real physical admission while waiting to save its journal.
	deactivationPID := waitForMiningGroupAdmission(t, f.db, ctx, blocker, "LockMiningGroupState")
	enqueued := make(chan error, 1)
	go func() {
		enqueued <- queue.NewDatabaseMessageQueue(&queue.Config{}, f.db).Enqueue(ctx, batch, commandtype.UpdateMiningPools, []int64{other, controller}, map[string]string{"worker": "next"})
	}()
	waitForMiningGroupAdmission(t, f.db, ctx, deactivationPID, "LockDeviceWriteFences")
	require.NoError(t, barrier.Commit())
	require.NoError(t, <-deactivation)
	err = <-enqueued
	require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	require.ErrorContains(t, err, "being disabled")
	rows, err := q.GetQueueMessagesByBatch(ctx, batch)
	require.NoError(t, err)
	require.Empty(t, rows, "a rejected batch must not enqueue even its unrelated device")
	state, err := q.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{DeviceID: controller, OrganizationID: f.org})
	require.NoError(t, err)
	require.Equal(t, requestID, state.OperationID.UUID)
	require.Equal(t, "deactivate", state.OperationKind)
	require.Equal(t, "pending", state.Status)
}
