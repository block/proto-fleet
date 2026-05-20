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

func TestResolvePluginsDir_Default_BinaryAdjacent(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture

	// Act
	got, err := resolvePluginsDir(exeDir)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, candidate, got)
}

func TestResolvePluginsDir_Default_MissingReturnsEmpty(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()

	// Act
	got, err := resolvePluginsDir(exeDir)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolvePluginsDir_Default_PresentButUnsafeIsHardError(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "plugins")
	require.NoError(t, os.Mkdir(candidate, 0o755)) //nolint:gosec // test fixture
	require.NoError(t, os.Chmod(candidate, 0o777)) //nolint:gosec // test exercises the reject path

	// Act
	_, err := resolvePluginsDir(exeDir)

	// Assert
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "writable")
}

func TestResolvePluginsDir_NoExeDir(t *testing.T) {
	// Act
	got, err := resolvePluginsDir("")

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestResolvePluginsDir_Default_FileAtCandidateIgnored(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(exeDir, "plugins"), []byte("not a dir"), 0o644))

	// Act
	got, err := resolvePluginsDir(exeDir)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, got)
}
