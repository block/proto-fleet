package updater

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenUpdaterSocketRefusesLiveListener(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	existing, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, existing.Close()) })

	_, err = listenUpdaterSocket(socketPath)
	require.ErrorContains(t, err, "another updater is already listening")
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	require.NoError(t, err, "the live listener path must not be unlinked")
	require.NoError(t, connection.Close())
}

func TestListenUpdaterSocketReclaimsStaleSocket(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	require.NoError(t, err)
	stale.SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())
	assert.FileExists(t, socketPath)

	listener, err := listenUpdaterSocket(socketPath)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	assert.NoFileExists(t, socketPath)
}

func TestListenUpdaterSocketRefusesNonSocketPath(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	require.NoError(t, os.WriteFile(socketPath, []byte("not a socket"), 0o600))

	_, err := listenUpdaterSocket(socketPath)
	require.ErrorContains(t, err, "refusing to replace non-socket path")
	assert.Equal(t, "not a socket", mustReadFile(t, socketPath))
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	// /tmp keeps the path below macOS's short Unix-domain socket limit.
	directory, err := os.MkdirTemp("/tmp", "proto-fleet-updater-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(directory)) })
	return filepath.Join(directory, "updater.sock")
}
