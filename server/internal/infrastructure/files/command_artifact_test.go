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
}

func TestSaveCommandArtifactRejectsSizeMismatchAndCleansUp(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveCommandArtifact("logs.zip", 99, checksumOf("short"), strings.NewReader("short"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size mismatch")
	assert.Empty(t, commandArtifactEntries(t))
}

func TestSaveCommandArtifactRejectsChecksumMismatchAndCleansUp(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveCommandArtifact("logs.zip", 4, checksumOf("nope"), strings.NewReader("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha256 mismatch")
	assert.Empty(t, commandArtifactEntries(t))
}

func TestOpenCommandArtifactRejectsPathTraversal(t *testing.T) {
	svc := setupService(t)

	_, _, err := svc.OpenCommandArtifact(filepath.Join("..", "logs", "tmp"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid command artifact ID")
}
