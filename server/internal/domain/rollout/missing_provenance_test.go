package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingProvenanceRequiresSuccessfulDispatch(t *testing.T) {
	for _, beforeVersion := range []string{"2.0.0", "1.0.0"} {
		for _, outcome := range []string{"PENDING", "PROCESSING", "FAILED", "SUCCESS"} {
			t.Run(beforeVersion+"/"+outcome, func(t *testing.T) {
				f := newFixture(t, 2)
				dispatcher := useAuditedModelDispatcher(t, f)
				f.channel(t, pilotOf1, f.allMiners()...)
				f.setReportedVersion(t, "miner-0", beforeVersion)
				started := f.apply(t, "fw-2")
				f.svc.EnforceTick(t.Context())
				require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
				if outcome == "FAILED" || outcome == "SUCCESS" {
					dispatcher.finish("miner-0", outcome)
				}
				_, err := f.conn.ExecContext(t.Context(), `INSERT INTO queue_message
					(command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
					SELECT uuid, $1, 'FirmwareUpdate', $2::queue_status_enum, 0, payload
					FROM command_batch_log WHERE id = $3`, f.deviceIDs["miner-0"], outcome, dispatcher.batchID)
				require.NoError(t, err)
				f.finishUpdate(t, "miner-0", "2.0.0")

				// Without prior managed provenance, an existing matching report
				// or a changed version cannot identify which payload was installed.
				f.svc.EnforceTick(t.Context())
				current := f.rollout(t, started.ID)
				if outcome == "SUCCESS" {
					assert.Equal(t, StageAwaitingReview, current.Stage)
					assert.Equal(t, PhaseDone, phaseOf(current, "miner-0"))
					assert.Equal(t, checksum2, deviceOf(t, current, "miner-0").LastDeployedFirmwareChecksum)
				} else {
					assert.Equal(t, StageBatch, current.Stage, "unknown provenance cannot release the pilot gate without a successful command")
					assert.Equal(t, PhaseInProgress, phaseOf(current, "miner-0"))
					var deployments int
					require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM device_firmware_deployment
						WHERE device_id = $1 AND firmware_checksum <> ''`, f.deviceIDs["miner-0"]).Scan(&deployments))
					assert.Zero(t, deployments, "pending or failed commands must not create artifact provenance")
				}
				assert.Equal(t, PhaseQueued, phaseOf(current, "miner-1"))
				assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			})
		}
	}
}
