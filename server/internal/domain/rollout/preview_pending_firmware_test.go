package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirmwarePreviewDoesNotCountForeignPendingCommandsAsOnTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fileID     string
		checksum   string
		status     string
		alsoLegacy bool
		foreign    bool
	}{
		{name: "pending foreign checksum overrides matching file ID", fileID: "fw-2", checksum: checksum1, status: "PENDING", foreign: true},
		{name: "processing foreign checksum", fileID: "old-upload", checksum: checksum1, status: "PROCESSING", foreign: true},
		{name: "matching checksum overrides foreign file ID", fileID: "fw-1", checksum: checksum2, status: "PENDING"},
		{name: "foreign legacy file", fileID: "fw-1", status: "PENDING", foreign: true},
		{name: "matching legacy file", fileID: "fw-2", status: "PROCESSING"},
		{name: "missing legacy file ID", status: "PENDING", foreign: true},
		{name: "finished foreign command", fileID: "fw-1", checksum: checksum1, status: "SUCCESS"},
		{name: "failed foreign command", fileID: "fw-1", checksum: checksum1, status: "FAILED"},
		{name: "own checksum plus foreign legacy command", fileID: "old-upload", checksum: checksum2, status: "PENDING", alsoLegacy: true, foreign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 3)
			f.channel(t, allAtOnce, f.allMiners()...)
			f.apply(t, "fw-2")
			setPreviewMemberOnTarget(t, f, "miner-0")
			setPreviewMemberOnTarget(t, f, "miner-1")
			// miner-0 has the queued command, miner-1 remains on target, and
			// miner-2 still needs the assignment's firmware.
			f.queueFirmwareArtifactCommand(t, "miner-0", tc.fileID, tc.checksum, tc.status)
			if tc.alsoLegacy {
				f.queueFirmwareCommand(t, "miner-0", "fw-1", "PROCESSING")
			}
			plans, err := f.svc.PreviewFirmware(t.Context(), f.orgID, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
			require.NoError(t, err)
			require.Len(t, plans, 1)
			wantTarget, wantOnTarget := int32(1), int32(2)
			if tc.foreign {
				wantTarget, wantOnTarget = 2, 1
			}
			assert.Equal(t, wantTarget, plans[0].TargetCount)
			assert.Equal(t, wantOnTarget, plans[0].OnTargetCount)
			assert.Equal(t, int32(3), plans[0].TargetCount+plans[0].OnTargetCount, "each unsuppressed member belongs to exactly one preview count")
		})
	}
}

func TestFirmwarePreviewDoesNotTreatSuppressionAsMatchingFirmware(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, allAtOnce, f.allMiners()...)
	started := f.apply(t, "fw-2")
	_, _, err := f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	// Both miners stay suppressed for this assignment, but can later report
	// its firmware. Only the one without a foreign command is on target.
	setPreviewMemberOnTarget(t, f, "miner-0")
	setPreviewMemberOnTarget(t, f, "miner-1")
	f.queueFirmwareArtifactCommand(t, "miner-0", "fw-2", checksum1, "PENDING")
	plans, err := f.svc.PreviewFirmware(t.Context(), f.orgID, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.True(t, plans[0].Unchanged)
	assert.Zero(t, plans[0].TargetCount, "suppression still excludes both miners from dispatch")
	assert.Equal(t, int32(1), plans[0].OnTargetCount, "suppression cannot make a foreign pending command count as matching")
}

func setPreviewMemberOnTarget(t *testing.T, f *fixture, identifier string) {
	t.Helper()
	f.setReportedVersion(t, identifier, "2.0.0")
	_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version)
		VALUES ($1, $2, '2.0.0')`, f.deviceIDs[identifier], checksum2)
	require.NoError(t, err)
}
