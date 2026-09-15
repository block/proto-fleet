package files

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirmwareReuse_RejectsUnhealthyPayloadWithoutPriorAvailabilityCheck(t *testing.T) {
	for _, lookup := range []string{"check", "streamed upload", "staged upload"} {
		for _, failure := range []string{"changed", "missing", "unreadable"} {
			t.Run(lookup+"/"+failure, func(t *testing.T) {
				if failure == "unreadable" && os.Geteuid() == 0 {
					t.Skip("root can read files without read permission")
				}
				svc := setupService(t)
				const content = "original firmware upload to recover"
				checksum := checksumOf(content)
				originalID, err := svc.SaveFirmwareFile("original.swu", strings.NewReader(content), testFirmwareMetadata())
				require.NoError(t, err)
				payloadPath, err := svc.GetFirmwareFilePath(originalID)
				require.NoError(t, err)
				switch failure {
				case "changed":
					require.NoError(t, os.WriteFile(payloadPath, []byte(strings.Repeat("x", len(content))), 0600))
				case "missing":
					require.NoError(t, os.Remove(payloadPath))
				case "unreadable":
					require.NoError(t, os.Chmod(payloadPath, 0000))
					t.Cleanup(func() { _ = os.Chmod(payloadPath, 0600) })
				}

				var saved FirmwareUploadSaveResult
				switch lookup {
				case "check":
					foundID, reusable := svc.FindFirmwareFileByChecksum(checksum, testFirmwareMetadata())
					assert.False(t, reusable, "the pre-upload check must not let the caller skip a needed upload")
					assert.Empty(t, foundID)
				case "streamed upload":
					saved, err = svc.SaveFirmwareUpload("replacement.swu", strings.NewReader(content), testFirmwareMetadata(), false)
				case "staged upload":
					staged, stageErr := svc.StageFirmwareUpload(strings.NewReader(content))
					require.NoError(t, stageErr)
					defer staged.Discard()
					saved, err = svc.SaveFirmwareUploadFromPath("replacement.swu", staged.Path, testFirmwareMetadata(), false, staged.Checksum)
				}
				require.NoError(t, err)
				assert.NotContains(t, svc.checksumIndex[checksum], originalID)
				assert.Equal(t, checksum, svc.firmwareChecksumByID[originalID], "unhealthy bytes must retain their original upload identity")
				if lookup != "check" {
					assert.False(t, saved.Reused)
					require.NotEqual(t, originalID, saved.FirmwareFileID)
					reader, info, openErr := svc.OpenFirmwareArtifactByChecksum(checksum)
					require.NoError(t, openErr)
					payload, readErr := io.ReadAll(reader)
					closeErr := reader.Close()
					require.NoError(t, readErr)
					require.NoError(t, closeErr)
					assert.Equal(t, saved.FirmwareFileID, info.ID)
					assert.Equal(t, content, string(payload), "the healthy replacement must remain available to existing assignments")
				}

				// Retaining by-ID identity lets exact grants and checksum lookup
				// recover the original file after its bytes are restored.
				if failure == "unreadable" {
					require.NoError(t, os.Chmod(payloadPath, 0600))
				}
				require.NoError(t, os.WriteFile(payloadPath, []byte(content), 0600))
				reader, info, err := svc.OpenFirmwareArtifact(originalID, checksum)
				require.NoError(t, err)
				require.NoError(t, reader.Close())
				assert.Equal(t, originalID, info.ID)
				assert.Contains(t, svc.FirmwareFileIDsByChecksum(checksum), originalID)
			})
		}
	}
}

func TestFirmwareReuse_SkipsCorruptCandidateAndReusesHealthyDuplicate(t *testing.T) {
	svc := setupService(t)
	const content = "duplicate firmware payload"
	corruptID, err := svc.SaveFirmwareFile("first.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	healthyID, err := svc.SaveFirmwareFile("second.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(getFirmwareDirPath(corruptID), "first.swu"), []byte("corrupt"), 0600))

	saved, err := svc.SaveFirmwareUpload("duplicate.swu", strings.NewReader(content), testFirmwareMetadata(), false)
	require.NoError(t, err)
	assert.True(t, saved.Reused)
	assert.Equal(t, healthyID, saved.FirmwareFileID)
	assert.Len(t, firmwareFileEntries(t), 2, "reuse should not publish a third copy")
	assert.NotContains(t, svc.checksumIndex[checksumOf(content)], corruptID)
}
