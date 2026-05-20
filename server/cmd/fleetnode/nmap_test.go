//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveNmapPath_ExplicitFlag_RelativeRejected(t *testing.T) {
	// Act
	_, err := resolveNmapPath("nmap", "/anything")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestResolveNmapPath_ExplicitFlag_NonExecutableRejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "nmap")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644))

	// Act
	_, err := resolveNmapPath(path, "")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not executable")
}

func TestResolveNmapPath_ExplicitFlag_DirectoryRejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()

	// Act
	_, err := resolveNmapPath(dir, "")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

func TestResolveNmapPath_ExplicitFlag_ExecutableReturned(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "nmap")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))

	// Act
	got, err := resolveNmapPath(path, "")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, path, got)
}

func TestResolveNmapPath_Default_BinaryAdjacent(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755))

	// Act
	got, err := resolveNmapPath("", exeDir)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, candidate, got)
}

func TestResolveNmapPath_Default_FallsBackToPATH(t *testing.T) {
	// Arrange
	exeDir := t.TempDir() // empty

	// Act
	got, err := resolveNmapPath("", exeDir)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "nmap", got)
}

func TestResolveNmapPath_Default_AdjacentNotExecutableFallsBack(t *testing.T) {
	// Arrange
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("nope"), 0o644))

	// Act
	got, err := resolveNmapPath("", exeDir)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "nmap", got)
}
