package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManualRemovalDoesNotRestoreAutomaticPoolDisplay(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear", true: "keep device settings"}[keep], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(cipher)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
			device := &poolDevice{}
			getter := poolMinerGetter{device}
			checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithChecker(checker)
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			q := sqlc.New(f.db)
			var id uuid.UUID
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership' AND state='pending'`, miner).Scan(&id))
			require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
			event, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			require.NoError(t, worker.Execute(t.Context(), f.org, event.OperationID.String, target))
			require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
			assertDisplay := func(configuration, state string) {
				t.Helper()
				got, err := service.GetAssignments(ctx, f.account.AccountID, nil, []int64{miner}, nil)
				require.NoError(t, err)
				require.Len(t, got.Assignments, 1)
				require.Equal(t, configuration, got.Assignments[0].ConfigurationID)
				require.Equal(t, state, got.Assignments[0].State)
				if configuration == "" {
					require.Empty(t, got.Assignments[0].OperationID)
				}
			}
			assertDisplay(config.ID, "verified")
			review, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
			require.NoError(t, err)
			removed, err := confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, keep, "actor-password")
			require.NoError(t, err)
			require.Equal(t, "completed", removed.State)
			require.Equal(t, keep, len(device.pools) > 0)
			assertDisplay("", "unavailable")
			// Reconciliation must retain history without making it current again.
			require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
			assertDisplay("", "unavailable")
			history, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			require.Equal(t, "verified", history.State)
			require.Equal(t, event.OperationID, history.OperationID)
			// An older retirement must not hide a later automatic policy.
			for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumUNPAIRED, sqlc.PairingStatusEnumPAIRED} {
				_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: miner, PairingStatus: status})
				require.NoError(t, err)
			}
			assertDisplay(config.ID, "pending")
		})
	}
}
