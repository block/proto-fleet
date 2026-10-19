package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountAssignmentReportsDestinationSitePolicy(t *testing.T) {
	for _, depth := range []string{"client", "subclient"} {
		t.Run(depth, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			actor, parent, destination, revision := f.host, f.root, client.AccountID, int64(0)
			if depth == "subclient" {
				child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
				require.NoError(t, err)
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
				actor, parent, destination, revision = f.user, client.AccountID, child.AccountID, 1
			}
			policyCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), parent)
			require.NoError(t, err)
			require.NoError(t, f.svc.ConfigureSites(policyCtx, actor, f.org, destination, true, nil))
			var before, after int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='account_assets_assigned'`).Scan(&before))
			err = f.allocate(t, actor, parent, destination, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, revision))
			require.True(t, fleeterror.IsInvalidArgumentError(err), "policy rejection must not be reported as a stale selection: %v", err)
			require.ErrorContains(t, err, "outside the destination account's site policy")
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='account_assets_assigned'`).Scan(&after))
			require.Equal(t, before, after)
			// The rejected assignment keeps its revision and becomes valid only when
			// the destination policy changes, without refreshing the selection.
			require.NoError(t, f.svc.ConfigureSites(policyCtx, actor, f.org, destination, false, nil))
			require.NoError(t, f.allocate(t, actor, parent, destination, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, revision)))
		})
	}
}
