package timescaledb

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestAutomaticHashboardReplacementRequiresEveryPeerInventory(t *testing.T) {
	for _, evidence := range []string{"missing", "incomplete"} {
		t.Run(evidence, func(t *testing.T) {
			database := testutil.NewDatabaseService(t, nil)
			user := database.CreateSuperAdminUser()
			base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
			rigs := make([]*modulePSUIdentityRig, 0, 3)
			for range 3 {
				device := database.CreateDevice(user.OrganizationID, "proto")
				_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
				require.NoError(t, err)
				rigs = append(rigs, &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, device: device.ID, orgID: user.OrganizationID, base: base})
			}
			source, completePeer, blockingPeer := rigs[0], rigs[1], rigs[2]
			require.NoError(t, source.store(t, replacementSnapshot(base, "old")))
			old := source.boardID(t, "old")
			client := replacementAccount(t, source, 0)
			child := replacementAccount(t, source, client)
			assignReplacementBoard(t, source, old, client, child, base.Add(time.Millisecond))
			require.NoError(t, source.store(t, replacementSnapshot(base.Add(time.Second), "new")))
			incoming := source.boardID(t, "new")
			require.NoError(t, completePeer.store(t, modulePSUSnapshot(base.Add(2*time.Second))))
			if evidence == "incomplete" {
				snapshot := modulePSUSnapshot(base.Add(3 * time.Second))
				snapshot.TopologyComplete = false
				require.NoError(t, blockingPeer.store(t, snapshot))
			}
			require.NoError(t, source.store(t, replacementSnapshot(base.Add(4*time.Second), "new")))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, source, incoming))
			require.Zero(t, replacementEventCount(t, source))

			require.NoError(t, blockingPeer.store(t, modulePSUSnapshot(base.Add(5*time.Second))))
			require.NoError(t, source.store(t, replacementSnapshot(base.Add(6*time.Second), "new")))
			require.Equal(t, [2]int64{}, replacementOwner(t, source, old))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, incoming))
			require.Equal(t, 1, replacementEventCount(t, source))
		})
	}
}

func TestAutomaticHashboardReplacementDoesNotInferRepeatedUnconfirmedReplacement(t *testing.T) {
	for _, waitingFor := range []string{"peer inventory", "PSU confirmation"} {
		t.Run(waitingFor, func(t *testing.T) {
			var source, peer *modulePSUIdentityRig
			if waitingFor == "peer inventory" {
				source, peer = replacementControllers(t)
			} else {
				source = newModulePSUIdentityRig(t)
			}
			require.NoError(t, source.store(t, replacementSnapshot(source.base, "old")))
			old := source.boardID(t, "old")
			client := replacementAccount(t, source, 0)
			child := replacementAccount(t, source, client)
			assignReplacementBoard(t, source, old, client, child, source.base.Add(time.Millisecond))
			for i, serial := range []string{"first replacement", "second replacement"} {
				at := source.base.Add(time.Duration(i+1) * time.Second)
				snapshot := replacementSnapshot(at, serial)
				if waitingFor == "PSU confirmation" {
					snapshot.Tanks[0].Modules[0].IdentityConfirmed = false
				}
				require.NoError(t, source.store(t, snapshot))
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, old))
				require.Equal(t, [2]int64{}, replacementOwner(t, source, source.boardID(t, serial)))
				require.Zero(t, replacementEventCount(t, source))
			}
			if peer != nil {
				require.NoError(t, peer.store(t, modulePSUSnapshot(source.base.Add(3*time.Second))))
			}
			// Restored evidence cannot infer ownership across an unallocated board.
			// A host must select the original board and its intended replacement.
			for _, offset := range []time.Duration{4 * time.Second, 5 * time.Second} {
				require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(offset), "second replacement")))
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, source, old))
				for _, serial := range []string{"first replacement", "second replacement"} {
					require.Equal(t, [2]int64{}, replacementOwner(t, source, source.boardID(t, serial)))
				}
				require.Zero(t, replacementEventCount(t, source))
			}
		})
	}
}
