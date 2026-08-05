package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

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

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
