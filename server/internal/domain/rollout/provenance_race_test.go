package rollout

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaleProvenanceObservationPreservesInterveningDeployment(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "initially absent"
		if existing {
			name = "existing observation"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			f := newFixture(t, 1)
			q := f.svc.store.Queries(ctx)
			deviceID := f.deviceIDs["miner-0"]
			if existing {
				require.NoError(t, q.RecordFirmwareDeployment(ctx, sqlc.RecordFirmwareDeploymentParams{
					DeviceIds: []int64{deviceID}, FirmwareChecksum: checksum1, FirmwareVersion: "2.0.0",
					ExpectedDeploymentPresent: []bool{false}, ExpectedDeployedAts: []time.Time{{}}, ExpectedFirmwareChecksums: []string{""},
				}))
			}
			f.channel(t, pilotOf1, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			f.finishUpdate(t, "miner-0", "2.0.0")
			row, err := q.GetFirmwareRolloutWithChannel(ctx, sqlc.GetFirmwareRolloutWithChannelParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			observed, err := f.svc.listTargets(ctx, row.FirmwareRollout)
			require.NoError(t, err)
			require.Len(t, observed, 1)
			require.Equal(t, existing, observed[0].LastDeployedAt.Valid)
			require.True(t, observed[0].LastSentAt.Valid)
			require.False(t, observed[0].VerifiedAt.Valid)

			// Another rollout commits after A captured its evidence. Give B a
			// larger ID: a later corrective send by active A must still be able
			// to record its artifact, without assuming rollout IDs order writes.
			otherChannel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Other channel", Behavior: allAtOnce})
			require.NoError(t, err)
			var newerRolloutID int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `
				INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation, status)
				VALUES ($1, $2, 'Proto', 'Rig', $3, '2.0.0', 1, 'completed') RETURNING id
			`, f.orgID, otherChannel.ID, checksum1).Scan(&newerRolloutID))
			require.Greater(t, newerRolloutID, started.ID)
			require.NoError(t, q.RecordFirmwareDeployment(ctx, sqlc.RecordFirmwareDeploymentParams{
				DeviceIds: []int64{deviceID}, FirmwareChecksum: checksum1, FirmwareVersion: "2.0.0",
				RolloutID:                 sql.NullInt64{Int64: newerRolloutID, Valid: true},
				ExpectedDeploymentPresent: []bool{observed[0].LastDeployedAt.Valid},
				ExpectedDeployedAts:       []time.Time{observed[0].LastDeployedAt.Time},
				ExpectedFirmwareChecksums: []string{observed[0].LastDeployedFirmwareChecksum},
			}))
			readDeployment := func() (string, int64, time.Time) {
				t.Helper()
				var checksum string
				var rolloutID int64
				var deployedAt time.Time
				require.NoError(t, f.conn.QueryRowContext(ctx, `
					SELECT firmware_checksum, rollout_id, deployed_at FROM device_firmware_deployment WHERE device_id = $1
				`, deviceID).Scan(&checksum, &rolloutID, &deployedAt))
				return checksum, rolloutID, deployedAt
			}
			_, savedRolloutID, savedAt := readDeployment()
			require.Equal(t, newerRolloutID, savedRolloutID)
			require.True(t, savedAt.After(observed[0].LastSentAt.Time))
			assertNewerDeployment := func() {
				t.Helper()
				checksum, rolloutID, deployedAt := readDeployment()
				assert.Equal(t, checksum1, checksum)
				assert.Equal(t, newerRolloutID, rolloutID)
				assert.Equal(t, savedAt, deployedAt)
			}

			// Resume A using the earlier target read. Its timestamp eligibility
			// check passes on that stale read, but the atomic write must not.
			refreshed, err := f.svc.recordProvenance(ctx, row.FirmwareRollout, observed)
			require.NoError(t, err)
			assertNewerDeployment()
			require.Len(t, refreshed, 1)
			assert.Equal(t, checksum1, refreshed[0].LastDeployedFirmwareChecksum)
			refreshed, err = f.svc.recordConvergence(ctx, row.FirmwareRollout, refreshed)
			require.NoError(t, err)
			assert.False(t, refreshed[0].VerifiedAt.Valid)
			assert.Equal(t, PhaseInProgress, phaseOf(f.rollout(t, started.ID), "miner-0"))
			f.svc.EnforceTick(ctx)
			assertNewerDeployment()
			assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "wait for the resend interval")

			// A can claim its checksum only after a fresh corrective dispatch.
			f.backdateSends(t)
			f.svc.EnforceTick(ctx)
			assertNewerDeployment()
			assert.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
			f.svc.EnforceTick(ctx)
			checksum, rolloutID, _ := readDeployment()
			assert.Equal(t, checksum2, checksum)
			assert.Equal(t, started.ID, rolloutID, "an older rollout ID can record a genuinely later send")
			assert.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
		})
	}
}
