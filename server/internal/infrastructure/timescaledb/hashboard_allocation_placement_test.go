package timescaledb

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestControllerPlacementAfterClockAheadAutomaticReplacement(t *testing.T) {
	for _, action := range []string{"move site", "change identifier", "delete"} {
		t.Run(action, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			site, err := sqlc.New(r.db).CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: r.orgID, Name: "Moved", Slug: "moved"})
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old-a", "old-b", "unchanged")))
			client := replacementAccount(t, r, 0)
			child := replacementAccount(t, r, client)
			for _, serial := range []string{"old-a", "old-b", "unchanged"} {
				assignReplacementBoard(t, r, r.boardID(t, serial), client, child, r.base.Add(time.Millisecond))
			}
			at := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
			require.NoError(t, r.store(t, replacementSnapshot(at, "new-a", "old-b", "unchanged")))
			latest := at.Add(time.Millisecond)
			require.NoError(t, r.store(t, replacementSnapshot(latest, "new-a", "new-b", "unchanged")))
			require.Equal(t, 2, replacementEventCount(t, r))
			require.True(t, time.Now().Before(at), "placement must change before the accepted inventory timestamp")
			switch action {
			case "move site":
				_, err = r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$2 WHERE id=$1`, r.deviceID, site.ID)
			case "change identifier":
				_, err = r.db.ExecContext(t.Context(), `UPDATE device SET device_identifier='moved-controller' WHERE id=$1`, r.deviceID)
			case "delete":
				_, err = r.db.ExecContext(t.Context(), `UPDATE device SET deleted_at=clock_timestamp() WHERE id=$1`, r.deviceID)
			}
			require.NoError(t, err)
			for _, serial := range []string{"new-a", "new-b", "unchanged"} {
				board := r.boardID(t, serial)
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, board))
				var end time.Time
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT max(ended_at) FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2`, r.orgID, board).Scan(&end))
				require.Equal(t, latest, end.UTC(), "all placements must close at the latest board start")
				var open int
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7 AND asset_id=$2 AND ended_at IS NULL`, r.orgID, board).Scan(&open))
				if action == "delete" {
					require.Zero(t, open)
					continue
				}
				require.Equal(t, 1, open)
				var start time.Time
				var matchesPlacement bool
				var owner, subowner int64
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT i.started_at,i.client_id,i.subclient_id,
				 i.site_id IS NOT DISTINCT FROM d.site_id AND i.device_identifier=d.device_identifier
				 FROM hosted_account_asset_interval i JOIN device d ON d.id=i.device_id
				 WHERE i.organization_id=$1 AND i.kind=7 AND i.asset_id=$2 AND i.ended_at IS NULL`, r.orgID, board).Scan(&start, &owner, &subowner, &matchesPlacement))
				require.Equal(t, latest, start.UTC(), "new placement must start at the shared close boundary")
				require.Equal(t, [2]int64{client, child}, [2]int64{owner, subowner})
				require.True(t, matchesPlacement)
			}
		})
	}
}
