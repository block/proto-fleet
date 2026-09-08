package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireFleetCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, code, fleetErr.GRPCCode)
}

func TestResolveFirmwareArtifact_ReturnsChecksumAndMetadata(t *testing.T) {
	svc := setupService(t)

	content := "assignable firmware content"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)

	artifact, err := svc.ResolveFirmwareArtifact(fileID)
	require.NoError(t, err)
	assert.Equal(t, fileID, artifact.FileID)
	assert.Equal(t, checksumOf(content), artifact.Checksum)
	assert.Equal(t, testFirmwareMetadata(), artifact.Metadata)
}

func TestResolveFirmwareArtifact_ErrorCodes(t *testing.T) {
	svc := setupService(t)

	_, err := svc.ResolveFirmwareArtifact("not-a-uuid")
	requireFleetCode(t, err, connect.CodeInvalidArgument)

	_, err = svc.ResolveFirmwareArtifact("00000000-0000-7000-8000-000000000000")
	requireFleetCode(t, err, connect.CodeNotFound)

	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader("firmware"), testFirmwareMetadata())
	require.NoError(t, err)
	sidecar := filepath.Join(getFirmwareDirPath(fileID), firmwareMetadataFilename)

	// A legacy sidecar without a firmware version cannot back an assignment:
	// the version is what the assignment enforces.
	legacy := testFirmwareMetadata()
	legacy.FirmwareVersion = ""
	require.NoError(t, writeFirmwareMetadata(getFirmwareDirPath(fileID), legacy, time.Now()))
	_, err = svc.ResolveFirmwareArtifact(fileID)
	requireFleetCode(t, err, connect.CodeInvalidArgument)
	assert.Contains(t, err.Error(), "firmware_version")

	require.NoError(t, os.Remove(sidecar))
	_, err = svc.ResolveFirmwareArtifact(fileID)
	requireFleetCode(t, err, connect.CodeInvalidArgument)
	assert.Contains(t, err.Error(), "has no metadata")

	// An unreadable sidecar is a storage fault, not a caller mistake.
	require.NoError(t, os.WriteFile(sidecar, []byte(`not json`), 0600))
	_, err = svc.ResolveFirmwareArtifact(fileID)
	requireFleetCode(t, err, connect.CodeInternal)
}

func TestFirmwareFileIDsByChecksum_IgnoresMetadataAndFollowsDeletes(t *testing.T) {
	svc := setupService(t)

	content := "re-uploadable firmware content"
	first, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	other := FirmwareMetadata{TargetManufacturer: "Other", TargetModel: "Model", FirmwareVersion: "v9"}
	second, err := svc.SaveFirmwareFile("firmware-copy.swu", strings.NewReader(content), other)
	require.NoError(t, err)

	// Every payload with the checksum carries the assignment, whatever its
	// metadata says; the metadata-aware lookups still distinguish them.
	assert.ElementsMatch(t, []string{first, second}, svc.FirmwareFileIDsByChecksum(checksumOf(content)))
	foundID, ok := svc.FindFirmwareFileByChecksum(checksumOf(content), other)
	assert.True(t, ok)
	assert.Equal(t, second, foundID)
	foundID, ok = svc.FindFirmwareFileIDByChecksum(checksumOf(content))
	assert.True(t, ok)
	assert.Contains(t, []string{first, second}, foundID)

	require.NoError(t, svc.DeleteFirmwareFile(first))
	assert.Equal(t, []string{second}, svc.FirmwareFileIDsByChecksum(checksumOf(content)))
	foundID, ok = svc.FindFirmwareFileIDByChecksum(checksumOf(content))
	assert.True(t, ok, "the assignment survives a different target on the remaining copy")
	assert.Equal(t, second, foundID)

	require.NoError(t, svc.DeleteFirmwareFile(second))
	assert.Empty(t, svc.FirmwareFileIDsByChecksum(checksumOf(content)))
	_, release, err := svc.LeaseFirmwareArtifact(checksumOf(content))
	requireFleetCode(t, err, connect.CodeNotFound)
	assert.Nil(t, release)
}

// After a restart every payload on disk carries the assignment, even when its
// mutable sidecar is missing or corrupt. Upload reuse still requires metadata.
func TestFirmwareFileIDsByChecksum_IndexesLegacyPayloadsOnStartup(t *testing.T) {
	svc := setupService(t)
	content := "legacy payload"
	legacy, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(getFirmwareDirPath(legacy), firmwareMetadataFilename)))
	corrupt, err := svc.SaveFirmwareFile("firmware-copy.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(getFirmwareDirPath(corrupt), firmwareMetadataFilename), []byte(`not json`), 0600))

	restarted, err := NewService(Config{})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{legacy, corrupt}, restarted.FirmwareFileIDsByChecksum(checksumOf(content)))
	foundID, ok := restarted.FindFirmwareFileIDByChecksum(checksumOf(content))
	assert.True(t, ok)
	assert.Contains(t, []string{legacy, corrupt}, foundID)
	leasedID, release, err := restarted.LeaseFirmwareArtifact(checksumOf(content))
	require.NoError(t, err)
	assert.Equal(t, foundID, leasedID)
	release()
	_, reusable := restarted.FindFirmwareFileByChecksum(checksumOf(content), testFirmwareMetadata())
	assert.False(t, reusable, "a payload without metadata is not eligible for upload reuse")
	_, err = restarted.ResolveFirmwareArtifact(legacy)
	requireFleetCode(t, err, connect.CodeInvalidArgument)

	healthy, err := restarted.SaveFirmwareFile("firmware-again.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	foundID, ok = restarted.FindFirmwareFileIDByChecksum(checksumOf(content))
	assert.True(t, ok)
	assert.Contains(t, []string{legacy, corrupt, healthy}, foundID)
}

func TestFirmwareArtifact_RejectsInvalidChecksums(t *testing.T) {
	svc := setupService(t)
	for _, checksum := range []string{"", "not-a-checksum", strings.Repeat("G", 64), strings.ToUpper(checksumOf("firmware"))} {
		_, release, err := svc.LeaseFirmwareArtifact(checksum)
		requireFleetCode(t, err, connect.CodeInvalidArgument)
		assert.Nil(t, release)
		reader, _, err := svc.OpenFirmwareArtifact("00000000-0000-7000-8000-000000000000", checksum)
		requireFleetCode(t, err, connect.CodeInvalidArgument)
		assert.Nil(t, reader)
	}
}
