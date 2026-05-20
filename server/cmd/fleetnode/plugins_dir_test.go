//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePluginsDir_ExplicitFlag_RelativeRejected(t *testing.T) {
	// Act
	_, err := resolvePluginsDir("plugins", "/anything")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestResolvePluginsDir_ExplicitFlag_MissingPathRejected(t *testing.T) {
	// Act
	_, err := resolvePluginsDir(filepath.Join(t.TempDir(), "nonexistent"), "")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestResolvePluginsDir_ExplicitFlag_WorldWritableRejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o777)) //nolint:gosec // test exercises the reject path

	// Act
	_, err := resolvePluginsDir(dir, "")

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "writable")
}

func TestResolvePluginsDir_ExplicitFlag_OwnedAndTight_Returned(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755)) //nolint:gosec // test fixture

	// Act
	got, err := resolvePluginsDir(dir, "")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestResolvePluginsDir_Default_BinaryAdjacent(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture

	// Act
	got, err := resolvePluginsDir("", exeDir)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, candidate, got)
}

func TestResolvePluginsDir_Default_MissingReturnsEmpty(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()

	// Act
	got, err := resolvePluginsDir("", exeDir)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolvePluginsDir_Default_PresentButUnsafeIsHardError(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture
	// chmod after Mkdir because umask masks the requested mode.
	require.NoError(t, os.Chmod(candidate, 0o777)) //nolint:gosec // test exercises the reject path

	// Act
	_, err := resolvePluginsDir("", exeDir)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "writable")
}

func TestResolvePluginsDir_NoFlagAndNoExeDir(t *testing.T) {
	// Act
	got, err := resolvePluginsDir("", "")

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolvePluginsDir_Default_FileAtCandidateIgnored(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(exeDir, "plugins"), []byte("not a dir"), 0o644))

	// Act
	got, err := resolvePluginsDir("", exeDir)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}
