package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelfUpdateExecArgsReplacesPriorHandoff(t *testing.T) {
	t.Parallel()

	args := []string{
		"proto-fleet-updater",
		"--state-dir", "/state",
		"--self-update-handoff", "/stale",
		"-self-update-handoff=/also-stale",
		"--socket-path=/socket",
	}
	assert.Equal(t, []string{
		"proto-fleet-updater",
		"--self-update-handoff=/canonical/updater",
		"--state-dir", "/state",
		"--socket-path=/socket",
	}, selfUpdateExecArgs(args, "/canonical/updater"))
}

func TestSelfUpdateStartupFailureWithoutHandoffLeavesExecutable(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "proto-fleet-updater")
	require.NoError(t, os.WriteFile(destination, []byte("new updater"), 0o755))
	require.NoError(t, os.WriteFile(destination+".previous", []byte("old updater"), 0o755))

	startupErr := errors.New("startup failed")
	err := handleSelfUpdateStartupFailure(nil, startupErr)
	require.ErrorIs(t, err, startupErr)
	assert.Equal(t, "new updater", mustReadFile(t, destination))
	assert.FileExists(t, destination+".previous")
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
