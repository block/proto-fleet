package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAllocationDoesNotWaitForControllerObservation(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		name := "assign"
		if replacement {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			module, controller := f.miningModule(t)
			otherModule, _ := f.miningModule(t)
			boards := f.moduleSelection(t, module).Boards
			other := f.moduleSelection(t, otherModule).Boards[0].Id
			if replacement {
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, boards[0].Id, 0)))
			}
			change := func(ctx context.Context) error {
				if replacement {
					return f.svc.ReplaceHashboard(ctx, f.host, f.org, &pb.ReplaceAccountHashboardRequest{AccountId: f.root, OldBoard: &pb.HashboardAllocationSelection{Id: boards[0].Id, ExpectedRevision: 1}, ReplacementBoard: &pb.HashboardAllocationSelection{Id: boards[1].Id}})
				}
				return f.svc.AssignAssets(ctx, f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, Assets: []*pb.MiningAssetSelection{selected(7, boards[0].Id, 0)}})
			}
			observation, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer observation.Rollback()
			_, err = sqlc.New(observation).LockMinerBindingObservationDevice(t.Context(), controller)
			require.NoError(t, err)
			_, err = sqlc.New(observation).LockMiningGroupState(t.Context(), sqlc.LockMiningGroupStateParams{OrganizationID: f.org, DeviceID: controller})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			started := time.Now()
			err = change(ctx)
			require.True(t, fleeterror.IsUnavailableError(err), "a busy controller must return a retryable error: %v", err)
			require.Less(t, time.Since(started), time.Second)
			// The same organization remains usable while observation I/O is held.
			require.NoError(t, f.svc.AssignAssets(ctx, f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, Assets: []*pb.MiningAssetSelection{selected(7, other, 0)}}))
			require.NoError(t, observation.Commit())
			require.NoError(t, change(t.Context()), "the original reviewed selection must remain usable after the observation ends")
		})
	}
}

func TestRigAllocationCanRetryBusyBinding(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	observation, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer observation.Rollback()
	_, err = sqlc.New(observation).LockMinerBindingObservationDevice(t.Context(), miner)
	require.NoError(t, err)
	started := time.Now()
	err = f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0))
	require.True(t, fleeterror.IsUnavailableError(err))
	require.Less(t, time.Since(started), time.Second)
	require.NoError(t, observation.Commit())
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
}
