package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_QueuedFirmwareChecksumIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, tc := range []struct {
		name          string
		payload       string
		status        string
		commandType   string
		wantMismatch  bool
		wantChecksums []string
		wantLegacyIDs []string
	}{
		{name: "pending matching checksum survives replaced file ID", payload: `{"firmware_file_id":"file-removed","firmware_checksum":"sum"}`, status: "PENDING", wantChecksums: []string{"sum"}},
		{name: "processing matching checksum survives replaced file ID", payload: `{"firmware_file_id":"file-removed","firmware_checksum":"sum"}`, status: "PROCESSING", wantChecksums: []string{"sum"}},
		{name: "foreign checksum overrides matching file ID", payload: `{"firmware_file_id":"file-current","firmware_checksum":"other"}`, wantMismatch: true, wantChecksums: []string{"other"}},
		{name: "nonempty invalid checksum cannot fall back to file ID", payload: `{"firmware_file_id":"file-current","firmware_checksum":"SUM"}`, wantMismatch: true, wantChecksums: []string{"SUM"}},
		{name: "legacy matching file ID", payload: `{"firmware_file_id":"file-current"}`, wantLegacyIDs: []string{"file-current"}},
		{name: "legacy removed file ID", payload: `{"firmware_file_id":"file-removed"}`, wantMismatch: true, wantLegacyIDs: []string{"file-removed"}},
		{name: "empty checksum uses legacy identity", payload: `{"firmware_file_id":"file-current","firmware_checksum":""}`, wantLegacyIDs: []string{"file-current"}},
		{name: "null checksum uses legacy identity", payload: `{"firmware_file_id":"file-current","firmware_checksum":null}`, wantLegacyIDs: []string{"file-current"}},
		{name: "missing identity is foreign", payload: `{}`, wantMismatch: true, wantLegacyIDs: []string{""}},
		{name: "successful command is ignored", payload: `{"firmware_file_id":"file-current","firmware_checksum":"other"}`, status: "SUCCESS"},
		{name: "failed command is ignored", payload: `{"firmware_file_id":"file-removed"}`, status: "FAILED"},
		{name: "other command type is ignored", payload: `{"firmware_checksum":"other"}`, commandType: "Reboot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			channel := f.channel("command identity")
			require.NoError(t, f.q.InsertReleaseChannelTargets(t.Context(), sqlc.InsertReleaseChannelTargetsParams{
				ChannelID: channel, TargetTypes: []string{"site"}, TargetIds: []int64{f.site},
			}))
			device := f.device("current", "Bitmain", "S19", "v2")
			rollout := f.rollout(channel, "Bitmain", "S19")
			require.NoError(t, f.q.AppendFirmwareRolloutDevices(t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{RolloutID: rollout, DeviceIds: []int64{device.id}}))
			require.NoError(t, f.q.RecordFirmwareDeployment(t.Context(), sqlc.RecordFirmwareDeploymentParams{DeviceIds: []int64{device.id}, FirmwareChecksum: "sum", FirmwareVersion: "v2"}))
			status, commandType := tc.status, tc.commandType
			if status == "" {
				status = "PENDING"
			}
			if commandType == "" {
				commandType = "FirmwareUpdate"
			}
			f.exec(`INSERT INTO queue_message (command_batch_log_uuid, device_id, command_type, status, retry_count, payload)
				VALUES ('00000000-0000-0000-0000-000000000001', $1, $2, $3::queue_status_enum, 0, $4::jsonb)`, device.id, commandType, status, tc.payload)

			members, err := f.q.ListReleaseChannelMismatchedMembers(t.Context(), sqlc.ListReleaseChannelMismatchedMembersParams{
				OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19",
				FirmwareVersion: "v2", FirmwareChecksum: "sum", AssignedFileIds: []string{"file-current"}, AssignmentGeneration: 1,
			})
			require.NoError(t, err)
			if tc.wantMismatch {
				require.Len(t, members, 1)
				require.Equal(t, device.id, members[0].DeviceID)
			} else {
				require.Empty(t, members, "matching queued checksum must survive replacement of the uploaded file ID")
			}

			devices, err := f.q.ListFirmwareRolloutDevices(t.Context(), rollout)
			require.NoError(t, err)
			require.Len(t, devices, 1)
			require.ElementsMatch(t, tc.wantChecksums, devices[0].PendingFirmwareChecksums)
			require.ElementsMatch(t, tc.wantLegacyIDs, devices[0].PendingLegacyFirmwareFileIds)
		})
	}
}
