package files

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commandArtifactEntries(t *testing.T) []os.DirEntry {
	return storageDirEntriesExcept(t, commandArtifactsDir, "staging")
}

func commandArtifactStagingEntries(t *testing.T) []os.DirEntry {
	return storageDirEntries(t, commandArtifactsStagingDir)
}

func TestSaveCommandArtifactValidatesAndOpens(t *testing.T) {
	svc := setupService(t)
	content := "miner log bundle bytes"

	info, err := svc.SaveCommandArtifact("../../miner-logs.zip", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "miner-logs.zip", info.Filename)
	assert.Equal(t, int64(len(content)), info.Size)
	assert.Equal(t, checksumOf(content), info.SHA256)

	reader, opened, err := svc.OpenCommandArtifact(info.ID)
	require.NoError(t, err)
	defer reader.Close()

	assert.Equal(t, info.ID, opened.ID)
	assert.Equal(t, info.Filename, opened.Filename)
	assert.Equal(t, info.Size, opened.Size)
	assert.Equal(t, info.SHA256, opened.SHA256)
	assert.FileExists(t, getCommandArtifactMetadataPath(info.ID))
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
	assert.Empty(t, commandArtifactStagingEntries(t))
}

func TestSaveCommandArtifactReservesMetadataFilename(t *testing.T) {
	svc := setupService(t)
	content := "miner log bundle bytes"

	info, err := svc.SaveCommandArtifact("metadata.json", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, "artifact.bin", info.Filename)

	reader, opened, err := svc.OpenCommandArtifact(info.ID)
	require.NoError(t, err)
	defer reader.Close()

	assert.Equal(t, "artifact.bin", opened.Filename)
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
	assert.FileExists(t, getCommandArtifactMetadataPath(info.ID))
	assert.FileExists(t, filepath.Join(getCommandArtifactDirPath(info.ID), "artifact.bin"))
}

func TestOpenCommandArtifactFallsBackForLegacyArtifactWithoutMetadata(t *testing.T) {
	svc := setupService(t)
	content := "legacy miner log bundle bytes"

	info, err := svc.SaveCommandArtifact("legacy.zip", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, os.Remove(getCommandArtifactMetadataPath(info.ID)))

	reader, opened, err := svc.OpenCommandArtifact(info.ID)
	require.NoError(t, err)
	defer reader.Close()

	assert.Equal(t, info.ID, opened.ID)
	assert.Equal(t, info.Filename, opened.Filename)
	assert.Equal(t, int64(len(content)), opened.Size)
	assert.Equal(t, checksumOf(content), opened.SHA256)
}

func TestOpenCommandArtifactRejectsCorruptDirectory(t *testing.T) {
	svc := setupService(t)
	content := "miner log bundle bytes"

	info, err := svc.SaveCommandArtifact("logs.zip", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(getCommandArtifactDirPath(info.ID), "extra.bin"), []byte("extra"), 0600))

	reader, _, err := svc.OpenCommandArtifact(info.ID)
	require.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "corrupt command artifact dir")
}

func TestOpenCommandArtifactRejectsMetadataSizeMismatch(t *testing.T) {
	svc := setupService(t)
	content := "miner log bundle bytes"

	info, err := svc.SaveCommandArtifact("logs.zip", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, writeCommandArtifactMetadata(info.ID, CommandArtifactInfo{
		ID:       info.ID,
		Filename: info.Filename,
		Size:     info.Size + 1,
		SHA256:   info.SHA256,
	}))

	reader, _, err := svc.OpenCommandArtifact(info.ID)
	require.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "corrupt command artifact metadata")
}

func TestSaveCommandArtifactRejectsSizeMismatchAndCleansUp(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveCommandArtifact("logs.zip", 99, checksumOf("short"), strings.NewReader("short"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size mismatch")
	assert.Empty(t, commandArtifactEntries(t))
	assert.Empty(t, commandArtifactStagingEntries(t))
}

func TestSaveCommandArtifactStopsAfterDeclaredSizeMismatch(t *testing.T) {
	svc := setupService(t)
	content := "longer-payload"
	reader := strings.NewReader(content)

	_, err := svc.SaveCommandArtifact("logs.zip", 4, checksumOf("long"), reader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "received more")
	assert.Equal(t, len(content)-5, reader.Len())
	assert.Empty(t, commandArtifactEntries(t))
	assert.Empty(t, commandArtifactStagingEntries(t))
}

func TestSaveCommandArtifactRejectsChecksumMismatchAndCleansUp(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveCommandArtifact("logs.zip", 4, checksumOf("nope"), strings.NewReader("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha256 mismatch")
	assert.Empty(t, commandArtifactEntries(t))
	assert.Empty(t, commandArtifactStagingEntries(t))
}

func TestSaveCommandArtifactRejectsInvalidMetadataAndCleansUp(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		sizeBytes int64
		sha256    string
		content   string
		want      string
	}{
		{
			name:      "empty filename",
			filename:  "",
			sizeBytes: int64(len("data")),
			sha256:    checksumOf("data"),
			content:   "data",
			want:      "filename is required",
		},
		{
			name:      "zero size",
			filename:  "logs.zip",
			sizeBytes: 0,
			sha256:    checksumOf(""),
			content:   "",
			want:      "size must be greater than zero",
		},
		{
			name:      "negative size",
			filename:  "logs.zip",
			sizeBytes: -1,
			sha256:    checksumOf("data"),
			content:   "data",
			want:      "size must be greater than zero",
		},
		{
			name:      "malformed sha",
			filename:  "logs.zip",
			sizeBytes: int64(len("data")),
			sha256:    "not-a-sha",
			content:   "data",
			want:      "sha256 must be 64 lowercase hexadecimal characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := setupService(t)

			_, err := svc.SaveCommandArtifact(tt.filename, tt.sizeBytes, tt.sha256, strings.NewReader(tt.content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, commandArtifactEntries(t))
			assert.Empty(t, commandArtifactStagingEntries(t))
		})
	}
}

func TestSaveCommandArtifactRejectsOversizeAndCleansUp(t *testing.T) {
	t.Chdir(t.TempDir())
	svc, err := NewService(Config{MaxCommandArtifactSize: 3})
	require.NoError(t, err)

	_, err = svc.SaveCommandArtifact("logs.zip", 4, checksumOf("data"), strings.NewReader("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
	assert.Empty(t, commandArtifactEntries(t))
	assert.Empty(t, commandArtifactStagingEntries(t))
}

func TestOpenCommandArtifactRejectsPathTraversal(t *testing.T) {
	svc := setupService(t)

	_, _, err := svc.OpenCommandArtifact(filepath.Join("..", "logs", "tmp"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid command artifact ID")
}

func TestDeleteCommandArtifactRemovesDirectory(t *testing.T) {
	svc := setupService(t)

	info, err := svc.SaveCommandArtifact("logs.zip", int64(len("data")), checksumOf("data"), strings.NewReader("data"))
	require.NoError(t, err)

	require.NoError(t, svc.DeleteCommandArtifact(info.ID))

	assert.NoDirExists(t, getCommandArtifactDirPath(info.ID))
	_, _, err = svc.OpenCommandArtifact(info.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command artifact not found")
}

func TestDeleteCommandArtifactRejectsInvalidID(t *testing.T) {
	svc := setupService(t)

	err := svc.DeleteCommandArtifact(filepath.Join("..", "logs", "tmp"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid command artifact ID")
}

func TestDeleteCommandArtifactReturnsNotFoundForMissingArtifact(t *testing.T) {
	svc := setupService(t)

	err := svc.DeleteCommandArtifact("00000000-0000-0000-0000-000000000000")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command artifact not found")
}

func TestSweepExpiredCommandArtifacts(t *testing.T) {
	svc := setupService(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	oldInfo, err := svc.SaveCommandArtifact("old.zip", int64(len("old")), checksumOf("old"), strings.NewReader("old"))
	require.NoError(t, err)
	freshInfo, err := svc.SaveCommandArtifact("fresh.zip", int64(len("fresh")), checksumOf("fresh"), strings.NewReader("fresh"))
	require.NoError(t, err)

	oldTime := now.Add(-8 * 24 * time.Hour)
	freshTime := now.Add(-time.Hour)
	require.NoError(t, os.Chtimes(getCommandArtifactDirPath(oldInfo.ID), oldTime, oldTime))
	require.NoError(t, os.Chtimes(getCommandArtifactDirPath(freshInfo.ID), freshTime, freshTime))
	require.NoError(t, os.Mkdir(filepath.Join(commandArtifactsDir, "not-a-uuid"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(commandArtifactsStagingDir, "orphan"), []byte("data"), 0600))

	deleted, err := svc.SweepExpiredCommandArtifacts(now, 7*24*time.Hour)

	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.NoDirExists(t, getCommandArtifactDirPath(oldInfo.ID))
	assert.DirExists(t, getCommandArtifactDirPath(freshInfo.ID))
	assert.DirExists(t, filepath.Join(commandArtifactsDir, "not-a-uuid"))
	assert.FileExists(t, filepath.Join(commandArtifactsStagingDir, "orphan"))
}
