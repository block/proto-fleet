package rollout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSameVersionProvenanceRequiresSuccessfulDispatch(t *testing.T) {
	for _, outcome := range []string{"PENDING", "FAILED", "SUCCESS"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t, 2)
			dispatcher := useAuditedModelDispatcher(t, f)
			f.channel(t, pilotOf1, f.allMiners()...)
			f.setReportedVersion(t, "miner-0", "2.0.0")
			_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version)
				VALUES ($1, $2, '2.0.0')`, f.deviceIDs["miner-0"], checksum1)
			require.NoError(t, err)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			if outcome != "PENDING" {
				dispatcher.finish("miner-0", outcome)
			}

			// Both artifacts report the same version and pair. The existing
			// report cannot establish that the newly queued artifact installed.
			f.svc.EnforceTick(t.Context())
			current := f.rollout(t, started.ID)
			if outcome == "SUCCESS" {
				require.Equal(t, StageAwaitingReview, current.Stage)
				require.Equal(t, PhaseDone, phaseOf(current, "miner-0"))
				require.Equal(t, checksum2, deviceOf(t, current, "miner-0").LastDeployedFirmwareChecksum)
				require.Equal(t, int32(1), f.rigGroup(t).OnTargetCount)
			} else {
				require.Equal(t, StageBatch, current.Stage, "an unfinished or failed update cannot release the pilot review gate")
				require.Equal(t, PhaseInProgress, phaseOf(current, "miner-0"))
				require.Equal(t, checksum1, deviceOf(t, current, "miner-0").LastDeployedFirmwareChecksum)
				require.Zero(t, f.rigGroup(t).OnTargetCount)
			}
			require.Equal(t, PhaseQueued, phaseOf(current, "miner-1"))
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestChangedVersionCanEstablishProvenanceBeforeCommandResult(t *testing.T) {
	f := newFixture(t, 1)
	useAuditedModelDispatcher(t, f)
	f.channel(t, allAtOnce, "miner-0")
	f.setReportedVersion(t, "miner-0", "1.5.0")
	_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version)
		VALUES ($1, $2, '1.5.0')`, f.deviceIDs["miner-0"], checksum1)
	require.NoError(t, err)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	done := f.rollout(t, started.ID)
	require.Equal(t, StatusCompleted, done.Status)
	require.Equal(t, PhaseDone, phaseOf(done, "miner-0"))
	require.Equal(t, checksum2, done.Devices[0].LastDeployedFirmwareChecksum)
	require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
}
