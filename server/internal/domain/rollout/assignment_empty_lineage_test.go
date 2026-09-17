package rollout

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func TestAssignmentWithoutTargetsPreservesRollbackLineage(t *testing.T) {
	for _, scope := range []string{"empty channel", "already matching members"} {
		for _, clear := range []bool{false, true} {
			name := scope
			if clear {
				name += "/after clearing predecessor"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t, 1)
				if scope == "empty channel" {
					f.channel(t, allAtOnce)
				} else {
					f.channel(t, allAtOnce, "miner-0")
					f.setReportedVersion(t, "miner-0", "2.0.0")
					_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version)
						VALUES ($1, $2, '2.0.0')`, f.deviceIDs["miner-0"], checksum2)
					require.NoError(t, err)
				}
				_, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-1")}, nil)
				require.NoError(t, err)
				if clear {
					_, err = f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("")}, nil)
					require.NoError(t, err)
				}
				started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
				require.NoError(t, err)
				require.Empty(t, started, "an assignment with no mismatches still creates no rollout")
				generation := f.rigGroup(t).AssignmentGeneration
				again, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
				require.NoError(t, err)
				require.Empty(t, again)
				require.Equal(t, generation, f.rigGroup(t).AssignmentGeneration, "unchanged assignment preserves its generation and predecessor")

				// Recreate the service before members join. Lineage must come
				// from persisted assignment state, not a prior rollout or memory.
				queries := sqlstores.NewSQLConnectionManager(f.conn)
				f.svc = NewService(&queries, sqlstores.NewSQLTransactor(f.conn), f.dispatcher, f.files, f.activity)
				f.svc.now = func() time.Time { return f.clock }
				if scope == "already matching members" {
					f.addMiner(t, "miner-1", "Rig")
				}
				_, err = f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
					Name: "Test channel", Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: allAtOnce,
				})
				require.NoError(t, err)
				f.svc.startNeededRollouts(t.Context())
				reconciled := f.latestRollout(t)
				require.Equal(t, checksum2, reconciled.FirmwareChecksum)
				require.Equal(t, generation, reconciled.AssignmentGeneration)
				if clear {
					require.Empty(t, reconciled.PreviousFirmwareChecksum, "an explicit clear breaks lineage to the former assignment")
					require.Empty(t, reconciled.PreviousFirmwareVersion)
				} else {
					require.Equal(t, checksum1, reconciled.PreviousFirmwareChecksum)
					require.Equal(t, "1.5.0", reconciled.PreviousFirmwareVersion)
				}
				_, restored, err := f.svc.RollbackFirmware(t.Context(), f.orgID, reconciled.ID, byOperator)
				require.NoError(t, err)
				if clear {
					require.Empty(t, restored)
					require.Empty(t, f.assignedFirmware(t))
				} else {
					require.Equal(t, "fw-1", f.assignedFirmware(t))
					require.Len(t, restored, 1)
					require.Equal(t, checksum2, restored[0].PreviousFirmwareChecksum, "rollback itself records the assignment it replaced")
				}
			})
		}
	}
}
