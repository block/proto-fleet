package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireCachedFirmwareAvailability(t *testing.T, svc *Service, checksum, wantID string) {
	t.Helper()
	id, available := svc.FindCachedFirmwareFileIDByChecksum(checksum)
	require.Equal(t, wantID != "", available)
	require.Equal(t, wantID, id)
}

func TestCachedFirmwareAvailability_DoesNotVerifyPayloadButHonorsStrictFailures(t *testing.T) {
	for _, verifier := range []string{"resolve", "find", "lease", "open", "reuse"} {
		t.Run(verifier, func(t *testing.T) {
			svc := setupService(t)
			content := "uploaded firmware payload"
			checksum := checksumOf(content)
			id, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
			require.NoError(t, err)
			requireCachedFirmwareAvailability(t, svc, checksum, id)
			path, err := getFirmwareFilePathForCanonicalID(id)
			require.NoError(t, err)
			original, err := os.Stat(path)
			require.NoError(t, err)

			// Same-size corruption with unchanged timestamps cannot be detected by
			// stat. Advisory reads do not hash; all strict paths must still do so.
			require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", len(content))), 0600))
			require.NoError(t, os.Chtimes(path, original.ModTime(), original.ModTime()))
			requireCachedFirmwareAvailability(t, svc, checksum, id)
			switch verifier {
			case "resolve":
				_, err = svc.ResolveFirmwareArtifact(id)
				requireFleetCode(t, err, connect.CodeFailedPrecondition)
			case "find":
				_, available := svc.FindFirmwareFileIDByChecksum(checksum)
				require.False(t, available)
			case "lease":
				_, release, err := svc.LeaseFirmwareArtifact(checksum)
				requireFleetCode(t, err, connect.CodeFailedPrecondition)
				require.Nil(t, release)
			case "open":
				reader, _, err := svc.OpenFirmwareArtifact(id, checksum)
				requireFleetCode(t, err, connect.CodeFailedPrecondition)
				require.Nil(t, reader)
			case "reuse":
				_, reusable := svc.FindFirmwareFileByChecksum(checksum, testFirmwareMetadata())
				require.False(t, reusable)
			}
			requireCachedFirmwareAvailability(t, svc, checksum, "")
			requireCachedFirmwareAvailability(t, svc, checksum, "")

			// Metadata repair must not erase a known payload-verification failure.
			_, err = svc.UpdateFirmwareMetadata(id, testFirmwareMetadata())
			require.NoError(t, err)
			requireCachedFirmwareAvailability(t, svc, checksum, "")

			// A strict success clears the failure even when restoration preserves
			// the failed snapshot's identity, size, timestamp and mode.
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			require.NoError(t, os.Chtimes(path, original.ModTime(), original.ModTime()))
			requireCachedFirmwareAvailability(t, svc, checksum, "")
			_, err = svc.ResolveFirmwareArtifact(id)
			require.NoError(t, err)
			requireCachedFirmwareAvailability(t, svc, checksum, id)
			assert.NotContains(t, svc.firmwarePayloadFailures, id)
		})
	}
}

func TestCachedFirmwareAvailability_UsesUploadedAndStartupIdentityWithoutMetadata(t *testing.T) {
	for _, sidecarState := range []string{"missing", "malformed", "edited"} {
		t.Run(sidecarState, func(t *testing.T) {
			svc := setupService(t)
			content := "metadata-independent firmware"
			checksum := checksumOf(content)
			id, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
			require.NoError(t, err)
			sidecar := filepath.Join(getFirmwareDirPath(id), firmwareMetadataFilename)
			switch sidecarState {
			case "missing":
				require.NoError(t, os.Remove(sidecar))
			case "malformed":
				require.NoError(t, os.WriteFile(sidecar, []byte("not json"), 0600))
			case "edited":
				metadata := testFirmwareMetadata()
				metadata.TargetModel = "Another model"
				_, err = svc.UpdateFirmwareMetadata(id, metadata)
				require.NoError(t, err)
			}
			if sidecarState != "edited" {
				_, err = svc.ResolveFirmwareArtifact(id)
				require.Error(t, err, "metadata errors do not establish payload corruption")
				assert.NotContains(t, svc.checksumIndex[checksum], id)
			}
			requireCachedFirmwareAvailability(t, svc, checksum, id)

			restarted, err := NewService(Config{})
			require.NoError(t, err)
			requireCachedFirmwareAvailability(t, restarted, checksum, id)
			for _, invalid := range []string{"", "not a checksum", strings.ToUpper(checksum), checksumOf("unknown")} {
				requireCachedFirmwareAvailability(t, restarted, invalid, "")
			}
		})
	}
}

func TestCachedFirmwareAvailability_ReconsidersChangedPayloadSnapshots(t *testing.T) {
	for _, changed := range []string{"identity", "size", "mtime", "mode"} {
		t.Run(changed, func(t *testing.T) {
			svc := setupService(t)
			content := "restorable firmware payload"
			checksum := checksumOf(content)
			id, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
			require.NoError(t, err)
			path, err := getFirmwareFilePathForCanonicalID(id)
			require.NoError(t, err)
			corrupt := strings.Repeat("x", len(content))
			if changed == "size" {
				corrupt += "extra"
			}
			require.NoError(t, os.WriteFile(path, []byte(corrupt), 0600))
			failedInfo, err := os.Stat(path)
			require.NoError(t, err)
			_, available := svc.FindFirmwareFileIDByChecksum(checksum)
			require.False(t, available)
			requireCachedFirmwareAvailability(t, svc, checksum, "")

			if changed == "identity" {
				replacement := filepath.Join(t.TempDir(), "replacement.swu")
				require.NoError(t, os.WriteFile(replacement, []byte(content), 0600))
				require.NoError(t, os.Rename(replacement, path))
			} else {
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			}
			mtime := failedInfo.ModTime()
			if changed == "mtime" {
				mtime = mtime.Add(time.Second)
			}
			require.NoError(t, os.Chtimes(path, mtime, mtime))
			if changed == "mode" {
				require.NoError(t, os.Chmod(path, 0400))
			}
			requireCachedFirmwareAvailability(t, svc, checksum, id)
			_, available = svc.FindFirmwareFileIDByChecksum(checksum)
			require.True(t, available, "strict checks also recover on the next call")
		})
	}
}

func TestCachedFirmwareAvailability_FollowsPresenceAndReadability(t *testing.T) {
	svc := setupService(t)
	content := "temporarily absent firmware"
	checksum := checksumOf(content)
	id, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	path, err := getFirmwareFilePathForCanonicalID(id)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	requireCachedFirmwareAvailability(t, svc, checksum, "")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	requireCachedFirmwareAvailability(t, svc, checksum, id)

	if os.Geteuid() != 0 {
		require.NoError(t, os.Chmod(path, 0000))
		t.Cleanup(func() { _ = os.Chmod(path, 0600) })
		requireCachedFirmwareAvailability(t, svc, checksum, "")
		require.NoError(t, os.Chmod(path, 0600))
		requireCachedFirmwareAvailability(t, svc, checksum, id)
	}

	require.NoError(t, os.WriteFile(path, []byte("corrupt"), 0600))
	_, available := svc.FindFirmwareFileIDByChecksum(checksum)
	require.False(t, available)
	require.Contains(t, svc.firmwarePayloadFailures, id)
	require.NoError(t, svc.DeleteFirmwareFile(id))
	requireCachedFirmwareAvailability(t, svc, checksum, "")
	assert.NotContains(t, svc.firmwarePayloadFailures, id, "API deletion clears verification failures")
	assert.NotContains(t, svc.firmwareChecksumByID, id)
}

func TestCachedFirmwareAvailability_SkipsKnownCorruptCopyAndIgnoresWrongExpectedChecksum(t *testing.T) {
	svc := setupService(t)
	content := "duplicate firmware payload"
	checksum := checksumOf(content)
	for range 2 {
		_, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
		require.NoError(t, err)
	}
	ids := svc.FirmwareFileIDsByChecksum(checksum)
	require.Len(t, ids, 2)
	path, err := getFirmwareFilePathForCanonicalID(ids[0])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", len(content))), 0600))
	reader, _, err := svc.OpenFirmwareArtifact(ids[0], checksum)
	requireFleetCode(t, err, connect.CodeFailedPrecondition)
	require.Nil(t, reader)
	require.NoError(t, os.Remove(filepath.Join(getFirmwareDirPath(ids[1]), firmwareMetadataFilename)))
	requireCachedFirmwareAvailability(t, svc, checksum, ids[1])

	reader, _, err = svc.OpenFirmwareArtifact(ids[1], checksumOf("wrong expected checksum"))
	requireFleetCode(t, err, connect.CodeFailedPrecondition)
	require.Nil(t, reader)
	requireCachedFirmwareAvailability(t, svc, checksum, ids[1])
	assert.NotContains(t, svc.firmwarePayloadFailures, ids[1])
}
