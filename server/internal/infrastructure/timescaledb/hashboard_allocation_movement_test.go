package timescaledb

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func replacementControllers(t *testing.T) (*modulePSUIdentityRig, *modulePSUIdentityRig) {
	t.Helper()
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	rigs := make([]*modulePSUIdentityRig, 0, 2)
	for range 2 {
		device := database.CreateDevice(user.OrganizationID, "proto")
		_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
		require.NoError(t, err)
		rigs = append(rigs, &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, device: device.ID, orgID: user.OrganizationID, base: base})
	}
	return rigs[0], rigs[1]
}

func TestAutomaticHashboardReplacementPreservesCrossControllerMove(t *testing.T) {
	for _, destinationFirst := range []bool{false, true} {
		name := "source reports first"
		if destinationFirst {
			name = "destination reports first"
		}
		t.Run(name, func(t *testing.T) {
			source, destination := replacementControllers(t)
			require.NoError(t, source.store(t, replacementSnapshot(source.base, "old", "", "")))
			destinationSnapshot := func(at time.Time, serial string) *sdk.ContainerSnapshot {
				snapshot := replacementSnapshot(at, serial, "", "")
				snapshot.Tanks[0].Modules[0].ReportedID = "destination-psu"
				return snapshot
			}
			require.NoError(t, destination.store(t, destinationSnapshot(source.base, "")))
			old := source.boardID(t, "old")
			client := replacementAccount(t, source, 0)
			child := replacementAccount(t, source, client)
			assignReplacementBoard(t, source, old, client, child, source.base.Add(time.Millisecond))
			if destinationFirst {
				// The first claim conflicts with the source's fresh observation.
				// It must leave the destination's map incomplete until a later poll.
				require.NoError(t, destination.store(t, destinationSnapshot(source.base.Add(time.Second), "old")))
			}
			require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(2*time.Second), "new", "", "")))
			incoming := source.boardID(t, "new")
			checkOwners := func() {
				t.Helper()
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, old))
				require.Equal(t, [2]int64{}, replacementOwner(t, source, incoming))
				require.Zero(t, replacementEventCount(t, source))
			}
			checkOwners()
			require.NoError(t, destination.store(t, destinationSnapshot(source.base.Add(3*time.Second), "old")))
			checkOwners()
			require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(4*time.Second), "new", "", "")))
			checkOwners()
			var controller int64
			require.NoError(t, source.db.QueryRowContext(t.Context(), `SELECT device_id FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, old).Scan(&controller))
			require.Equal(t, destination.deviceID, controller)
		})
	}
}

func TestAutomaticHashboardReplacementWaitsForCompletePeerInventory(t *testing.T) {
	for _, evidence := range []string{"missing", "older observation", "earlier clock-ahead poll", "incomplete map", "unknown slot"} {
		t.Run(evidence, func(t *testing.T) {
			source, peer := replacementControllers(t)
			require.NoError(t, source.store(t, replacementSnapshot(source.base, "old", "", "")))
			if evidence == "earlier clock-ahead poll" {
				require.NoError(t, peer.store(t, modulePSUSnapshot(source.base.Add(10*time.Second))))
			}
			old := source.boardID(t, "old")
			client := replacementAccount(t, source, 0)
			child := replacementAccount(t, source, client)
			assignReplacementBoard(t, source, old, client, child, source.base.Add(time.Millisecond))
			require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(time.Second), "new", "", "")))
			incoming := source.boardID(t, "new")
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, source, incoming))
			switch evidence {
			case "older observation":
				require.NoError(t, peer.store(t, modulePSUSnapshot(source.base.Add(500*time.Millisecond))))
			case "incomplete map":
				snapshot := modulePSUSnapshot(source.base.Add(2 * time.Second))
				snapshot.TopologyComplete = false
				require.NoError(t, peer.store(t, snapshot))
			case "unknown slot":
				snapshot := replacementSnapshot(source.base.Add(2*time.Second), "", "", "")
				snapshot.Tanks[0].Modules[0].ReportedID = "peer-psu"
				snapshot.Tanks[0].Modules[0].Hashboards[0].Occupancy = "unknown"
				require.NoError(t, peer.store(t, snapshot))
			}
			require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(3*time.Second), "new", "", "")))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, source, incoming))
			require.Zero(t, replacementEventCount(t, source))
			require.NoError(t, peer.store(t, modulePSUSnapshot(source.base.Add(11*time.Second))))
			confirmed := source.base.Add(12 * time.Second)
			require.NoError(t, source.store(t, replacementSnapshot(confirmed, "new", "", "")))
			require.Equal(t, [2]int64{}, replacementOwner(t, source, old))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, incoming))
			require.Equal(t, 1, replacementEventCount(t, source))
			var started time.Time
			require.NoError(t, source.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, incoming).Scan(&started))
			require.Equal(t, confirmed, started.UTC(), "waiting for peer evidence must not backfill replacement ownership")
		})
	}
}

func TestAutomaticHashboardReplacementWaitsForCompleteSourceInventory(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "", "")))
	old := r.boardID(t, "old")
	client := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	pending := replacementSnapshot(r.base.Add(time.Second), "new", "", "")
	pending.Tanks[0].Modules[0].Hashboards[1].Occupancy = "unknown"
	require.NoError(t, r.store(t, pending))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, r.boardID(t, "new")))
	require.Zero(t, replacementEventCount(t, r))
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "new", "old", "")))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, r.boardID(t, "new")))
	require.Zero(t, replacementEventCount(t, r))
}
