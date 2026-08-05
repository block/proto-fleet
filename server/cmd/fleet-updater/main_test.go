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

func TestSelfUpdateStartupFailureRestoresOnlyMarkedHandoff(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		handoff     bool
		wantCurrent string
		wantBackup  bool
	}{
		{name: "marked replacement", handoff: true, wantCurrent: "old updater", wantBackup: false},
		{name: "ordinary startup", handoff: false, wantCurrent: "new updater", wantBackup: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			destination := filepath.Join(directory, "proto-fleet-updater")
			require.NoError(t, os.WriteFile(destination, []byte("new updater"), 0o755))
			require.NoError(t, os.WriteFile(destination+".previous", []byte("old updater"), 0o755))
			handoffPath := ""
			if test.handoff {
				handoffPath = destination
			}

			startupErr := errors.New("startup failed")
			err := handleSelfUpdateStartupFailure(handoffPath, startupErr)
			require.ErrorIs(t, err, startupErr)
			assert.Equal(t, test.wantCurrent, mustReadFile(t, destination))
			if test.wantBackup {
				assert.FileExists(t, destination+".previous")
			} else {
				assert.NoFileExists(t, destination+".previous")
				assert.ErrorContains(t, err, "previous executable restored")
			}
		})
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
