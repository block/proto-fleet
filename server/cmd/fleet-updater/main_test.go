package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/block/proto-fleet/server/internal/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const updaterUmaskTestHelper = "PROTO_FLEET_UPDATER_UMASK_TEST_HELPER"

func TestMainSetsSecureProcessUmask(t *testing.T) {
	if os.Getenv(updaterUmaskTestHelper) == "1" {
		// Keep the process-global umask change inside this one-shot child so it
		// cannot race the package's parallel tests.
		syscall.Umask(0)
		os.Args = []string{"proto-fleet-updater", "--version"}
		flag.CommandLine = flag.NewFlagSet("proto-fleet-updater", flag.ContinueOnError)

		main()

		actual := syscall.Umask(0)
		assert.Equal(t, 0o077, actual)
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestMainSetsSecureProcessUmask$") //nolint:gosec // The current test executable is trusted.
	command.Env = append(os.Environ(), updaterUmaskTestHelper+"=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "umask helper failed: %s", output)
}

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

func TestRollbackSelfUpdateQueuedDuringShutdown(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	require.NoError(t, os.MkdirAll(filepath.Join(installRoot, "deployment"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(installRoot, "deployment", "version.txt"),
		[]byte("version: v1.0.0\n"),
		0o600,
	))

	updaterDirectory := filepath.Join(root, "updater")
	require.NoError(t, os.Mkdir(updaterDirectory, 0o700))
	destination := filepath.Join(updaterDirectory, "proto-fleet-updater")
	require.NoError(t, os.WriteFile(destination, []byte("old updater"), 0o755))

	manager, err := updater.NewManager(updater.Config{
		InstallRoot:    installRoot,
		StateDir:       filepath.Join(root, "state"),
		SelfUpdatePath: destination,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	canonicalDestination, err := filepath.EvalSymlinks(destination)
	require.NoError(t, err)

	// Model an activation that installed and queued the replacement while
	// Manager.Shutdown was waiting for the non-cancelable activation to finish.
	require.NoError(t, os.Link(destination, destination+".previous"))
	marker, err := json.Marshal(map[string]string{"executable_path": canonicalDestination})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(destination+".handoff", marker, 0o600))
	candidate := destination + ".candidate"
	require.NoError(t, os.WriteFile(candidate, []byte("new updater"), 0o755))
	require.NoError(t, os.Rename(candidate, destination))

	ready := make(chan string, 1)
	ready <- canonicalDestination
	require.NoError(t, manager.Shutdown(context.Background()))

	rolledBackPath, err := rollbackSelfUpdateQueuedDuringShutdown(ready, manager.RollbackSelfUpdate)
	require.NoError(t, err)
	assert.Equal(t, canonicalDestination, rolledBackPath)
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.NoFileExists(t, destination+".handoff")
	assert.FileExists(t, destination+".previous")
}

func TestRollbackSelfUpdateForPendingSignal(t *testing.T) {
	t.Parallel()

	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	rolledBack := false

	pendingSignal, err := rollbackSelfUpdateForPendingSignal(signals, func() error {
		rolledBack = true
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, syscall.SIGTERM, pendingSignal)
	assert.True(t, rolledBack)
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
