package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueuedArtifactChecksumSurvivesReupload(t *testing.T) {
	for _, status := range []string{"PENDING", "PROCESSING"} {
		t.Run(status, func(t *testing.T) {
			f, started := queuedArtifactFixture(t)
			f.queueFirmwareArtifactCommand(t, "miner-0", "fw-2", checksum2, status)

			// The admitted command retains the original ID, while identical
			// bytes are now available only through a different upload.
			const replacementID = "fw-2-reuploaded"
			replacement := f.files.artifacts["fw-2"]
			replacement.FileID = replacementID
			f.files.deleted["fw-2"] = true
			f.files.artifacts[replacementID] = replacement
			assert.Equal(t, []string{replacementID}, f.files.FirmwareFileIDsByChecksum(checksum2))

			plans, err := f.svc.PreviewFirmware(t.Context(), f.orgID, f.channelID, []Assignment{rigAssignment(replacementID)}, nil)
			require.NoError(t, err)
			require.Len(t, plans, 1)
			assert.True(t, plans[0].Unchanged)
			assert.Zero(t, plans[0].TargetCount)

			f.backdateSends(t)
			before := f.rollout(t, started.ID)
			f.svc.EnforceTick(t.Context())
			current := f.rollout(t, started.ID)
			assert.Equal(t, PhaseDone, phaseOf(current, "miner-0"), "upload availability must not reopen the saved command's artifact identity")
			assert.Equal(t, before.Revision, current.Revision)
			assert.Equal(t, StageAwaitingReview, current.Stage)
			assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "no corrective command is needed")

			_, err = f.svc.ContinueRollout(t.Context(), f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			f.svc.EnforceTick(t.Context())
			require.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
			f.svc.EnforceTick(t.Context())
			rollouts, _, _, err := f.svc.ListRollouts(t.Context(), f.orgID, RolloutFilter{})
			require.NoError(t, err)
			require.Len(t, rollouts, 1, "the outstanding command must not create a reconciliation rollout")
			assert.Equal(t, started.ID, rollouts[0].ID)
			assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestQueuedArtifactChecksumOverridesMatchingFileID(t *testing.T) {
	for _, status := range []string{"PENDING", "PROCESSING"} {
		t.Run(status, func(t *testing.T) {
			f, started := queuedArtifactFixture(t)
			commandID := f.queueFirmwareArtifactCommand(t, "miner-0", "fw-2", checksum1, status)
			plans, err := f.svc.PreviewFirmware(t.Context(), f.orgID, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
			require.NoError(t, err)
			require.Len(t, plans, 1)
			assert.Equal(t, int32(1), plans[0].TargetCount, "a current file ID cannot override the queued checksum")

			f.backdateSends(t)
			f.svc.EnforceTick(t.Context())
			assert.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
			assert.Equal(t, PhaseRetrying, phaseOf(f.rollout(t, started.ID), "miner-0"))
			f.svc.EnforceTick(t.Context())
			assert.Equal(t, PhaseRetrying, phaseOf(f.rollout(t, started.ID), "miner-0"), "the foreign checksum blocks verification until its command drains")

			_, err = f.conn.ExecContext(t.Context(), `UPDATE queue_message SET status = 'SUCCESS' WHERE id = $1`, commandID)
			require.NoError(t, err)
			f.svc.EnforceTick(t.Context())
			current := f.rollout(t, started.ID)
			assert.Equal(t, PhaseDone, phaseOf(current, "miner-0"))
			assert.Equal(t, StageAwaitingReview, current.Stage)
			assert.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestQueuedArtifactAndLegacyCommandsUseTheirOwnIdentities(t *testing.T) {
	for _, tc := range []struct {
		name           string
		checksum       string
		checksumStatus string
		legacyFileID   string
		wantForeign    bool
	}{
		{name: "both own", checksum: checksum2, checksumStatus: "PENDING", legacyFileID: "fw-2"},
		{name: "foreign legacy", checksum: checksum2, checksumStatus: "PROCESSING", legacyFileID: "fw-1", wantForeign: true},
		{name: "foreign checksum", checksum: checksum1, checksumStatus: "PENDING", legacyFileID: "fw-2", wantForeign: true},
		{name: "completed foreign checksum ignored", checksum: checksum1, checksumStatus: "SUCCESS", legacyFileID: "fw-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, started := queuedArtifactFixture(t)
			// The checksum-bearing command's ID is deliberately absent from
			// the current files. Only the legacy command uses file-ID lookup.
			f.queueFirmwareArtifactCommand(t, "miner-0", "old-upload", tc.checksum, tc.checksumStatus)
			f.queueFirmwareCommand(t, "miner-0", tc.legacyFileID, "PROCESSING")
			plans, err := f.svc.PreviewFirmware(t.Context(), f.orgID, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
			require.NoError(t, err)
			require.Len(t, plans, 1)
			wantCount := int32(0)
			if tc.wantForeign {
				wantCount = 1
			}
			assert.Equal(t, wantCount, plans[0].TargetCount)

			f.backdateSends(t)
			f.svc.EnforceTick(t.Context())
			current := f.rollout(t, started.ID)
			if tc.wantForeign {
				assert.Equal(t, PhaseRetrying, phaseOf(current, "miner-0"))
				assert.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
			} else {
				assert.Equal(t, PhaseDone, phaseOf(current, "miner-0"))
				assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			}
		})
	}
}

// queuedArtifactFixture leaves a verified miner at a manual pilot gate, so
// queued-command identity can be checked against an active DONE latch.
func queuedArtifactFixture(t *testing.T) (*fixture, Rollout) {
	t.Helper()
	f := newFixture(t, 1)
	f.channel(t, pilotOf1, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	current := f.rollout(t, started.ID)
	require.Equal(t, StageAwaitingReview, current.Stage)
	require.Equal(t, PhaseDone, phaseOf(current, "miner-0"))
	return f, started
}

func (f *fixture) queueFirmwareArtifactCommand(t *testing.T, identifier, fileID, checksum, status string) int64 {
	t.Helper()
	var commandID int64
	err := f.conn.QueryRowContext(t.Context(), `
		INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
		VALUES ('00000000-0000-0000-0000-000000000000', $1, 'FirmwareUpdate', $2::queue_status_enum, 0,
		        jsonb_build_object('firmware_file_id', $3::text, 'firmware_checksum', $4::text))
		RETURNING id
	`, f.deviceIDs[identifier], status, fileID, checksum).Scan(&commandID)
	require.NoError(t, err)
	return commandID
}
