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

func commandArtifactEntries(t *testing.T) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(commandArtifactsDir)
	require.NoError(t, err)
	var filtered []os.DirEntry
	for _, e := range entries {
		if e.Name() != "staging" {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func commandArtifactStagingEntries(t *testing.T) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(commandArtifactsStagingDir)
	require.NoError(t, err)
	return entries
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
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
	assert.Empty(t, commandArtifactStagingEntries(t))
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
