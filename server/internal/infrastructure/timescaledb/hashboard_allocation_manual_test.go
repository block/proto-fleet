package timescaledb

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestManualAllocationAfterClockAheadAutomaticReplacement(t *testing.T) {
	for _, action := range []string{"recall and reassign module", "delegate module", "replace board"} {
		t.Run(action, func(t *testing.T) {
			database := testutil.NewDatabaseService(t, nil)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
			require.NoError(t, err)
			r := &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, orgID: user.OrganizationID, device: device.ID, base: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)}
			svc := hostedaccounts.NewService(r.db, nil)
			require.NoError(t, sqlc.New(r.db).EnsureDeviceWriteFences(t.Context(), []int64{r.deviceID}))
			_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,observed,observed_at)
			 VALUES($1,$2,'{"supported":true,"enabled":true,"suspended":false,"pending":false,"revision":1}',clock_timestamp())`, r.deviceID, r.orgID)
			require.NoError(t, err)
			var root string
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, r.orgID).Scan(&root))
			client, err := svc.CreateChild(t.Context(), user.DatabaseID, r.orgID, root, "Client", user.DatabaseID, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			child, err := svc.CreateChild(t.Context(), user.DatabaseID, r.orgID, client.AccountID, "Subclient", user.DatabaseID, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old-a", "old-b", "unchanged")))
			module := r.moduleIDAt(t, 1, 1)
			selection := func(account string, kind pb.MiningAssetKind, id int64) *pb.MiningAssetSelection {
				t.Helper()
				response, err := svc.ListAssets(t.Context(), user.DatabaseID, r.orgID, &pb.ListAccountAssetsRequest{
					AccountId: account, SelectedAssets: []*pb.MiningAssetReference{{Kind: kind, Id: id}},
				})
				require.NoError(t, err)
				require.Len(t, response.Assets, 1)
				asset := response.Assets[0]
				return &pb.MiningAssetSelection{Kind: kind, Id: id, ExpectedRevision: asset.Revision, Boards: asset.Boards}
			}
			assign := func(account, destination string, intent pb.AssetAssignmentIntent, assets ...*pb.MiningAssetSelection) {
				t.Helper()
				require.NoError(t, svc.AssignAssets(t.Context(), user.DatabaseID, r.orgID, &pb.AssignAccountAssetsRequest{
					AccountId: account, ChildAccountId: destination, Intent: intent, Assets: assets,
				}))
			}
			assign(root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selection(root, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module))
			at := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
			require.NoError(t, r.store(t, replacementSnapshot(at, "new-a", "old-b", "unchanged")))
			latest := at.Add(time.Millisecond)
			require.NoError(t, r.store(t, replacementSnapshot(latest, "new-a", "new-b", "unchanged")))
			boards := []int64{r.boardID(t, "new-a"), r.boardID(t, "new-b"), r.boardID(t, "unchanged")}
			for _, board := range boards {
				require.Equal(t, [2]int64{client.ID, 0}, replacementOwner(t, r, board))
			}
			require.Equal(t, 2, replacementEventCount(t, r))
			require.True(t, time.Now().Before(at), "manual actions must run before the accepted inventory timestamp")
			wantAssignments := 2
			switch action {
			case "recall and reassign module":
				wantAssignments = 3
				assign(root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selection(root, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module))
				for _, board := range boards {
					require.Equal(t, [2]int64{}, replacementOwner(t, r, board))
					var end time.Time
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2 ORDER BY id DESC LIMIT 1`, r.orgID, board).Scan(&end))
					require.Equal(t, latest, end.UTC(), "all boards close at the latest selected boundary")
				}
				assign(root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selection(root, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module))
				for _, board := range boards {
					require.Equal(t, [2]int64{client.ID, 0}, replacementOwner(t, r, board))
					var start time.Time
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2 AND ended_at IS NULL`, r.orgID, board).Scan(&start))
					require.Equal(t, latest, start.UTC(), "reassignment must retain the closed history boundary")
				}
			case "delegate module":
				assign(client.AccountID, child.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selection(client.AccountID, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module))
				for _, board := range boards {
					require.Equal(t, [2]int64{client.ID, child.ID}, replacementOwner(t, r, board))
					var end, start time.Time
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT max(ended_at),max(started_at) FILTER (WHERE ended_at IS NULL) FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2`, r.orgID, board).Scan(&end, &start))
					require.Equal(t, latest, start.UTC())
					require.Equal(t, start, end, "delegation must close and open all boards at one boundary")
				}
			case "replace board":
				// The incoming board has a later, closed period than the outgoing board.
				assign(root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selection(root, pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, boards[1]))
				old := selection(root, pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, boards[0])
				incoming := selection(root, pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, boards[1])
				require.NoError(t, svc.ReplaceHashboard(t.Context(), user.DatabaseID, r.orgID, &pb.ReplaceAccountHashboardRequest{
					AccountId:        root,
					OldBoard:         &pb.HashboardAllocationSelection{Id: old.Id, ExpectedRevision: old.ExpectedRevision},
					ReplacementBoard: &pb.HashboardAllocationSelection{Id: incoming.Id, ExpectedRevision: incoming.ExpectedRevision},
				}))
				require.Equal(t, [2]int64{}, replacementOwner(t, r, old.Id))
				require.Equal(t, [2]int64{client.ID, 0}, replacementOwner(t, r, incoming.Id))
				var end, start time.Time
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2 ORDER BY id DESC LIMIT 1`, r.orgID, old.Id).Scan(&end))
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2 AND ended_at IS NULL`, r.orgID, incoming.Id).Scan(&start))
				require.Equal(t, latest, start.UTC())
				require.Equal(t, start, end, "replacement must share the later boundary across both boards")
				require.Equal(t, 3, replacementEventCount(t, r))
			}
			var overlaps int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM (
			 SELECT ended_at,lead(started_at) OVER (PARTITION BY kind,asset_id ORDER BY id) AS next_start
			 FROM hosted_account_asset_interval WHERE organization_id=$1
			) periods WHERE next_start IS NOT NULL AND (ended_at IS NULL OR ended_at>next_start)`, r.orgID).Scan(&overlaps))
			require.Zero(t, overlaps)
			var assignments int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='account_assets_assigned'`, r.orgID).Scan(&assignments))
			require.Equal(t, wantAssignments, assignments)
		})
	}
}
