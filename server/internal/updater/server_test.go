package updater

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
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

	var listener *net.UnixListener
	// Darwin can briefly continue accepting connects immediately after Close.
	// The production code correctly treats that ambiguity as live, so retry the
	// proof rather than weakening its fail-closed behavior.
	require.Eventually(t, func() bool {
		listener, err = listenUpdaterSocket(socketPath)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
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

func TestPrepareUpdaterSocketDirectoryRefusesSymlink(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "proto-fleet-updater-dir-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })
	realDirectory := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(realDirectory, 0o700))
	symlinkDirectory := filepath.Join(root, "socket-dir")
	require.NoError(t, os.Symlink(realDirectory, symlinkDirectory))

	_, err = prepareUpdaterSocketDirectory(filepath.Join(symlinkDirectory, "updater.sock"))
	require.ErrorContains(t, err, "must not be a symlink")
}

func TestPrepareUpdaterSocketDirectoryRefusesWritableDirectory(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "group writable", mode: 0o770},
		{name: "world writable even when sticky", mode: 0o777 | os.ModeSticky},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			socketPath := shortSocketPath(t)
			require.NoError(t, os.Chmod(filepath.Dir(socketPath), test.mode))

			_, err := prepareUpdaterSocketDirectory(socketPath)
			require.ErrorContains(t, err, "must not be group- or world-writable")
		})
	}
}

func TestPrepareUpdaterSocketDirectoryAllowsStickySharedAncestor(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "proto-fleet-updater-dir-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })
	sharedDirectory := filepath.Join(root, "shared")
	require.NoError(t, os.Mkdir(sharedDirectory, 0o700))
	require.NoError(t, os.Chmod(sharedDirectory, 0o777|os.ModeSticky))
	socketDirectory := filepath.Join(sharedDirectory, "socket-dir")
	require.NoError(t, os.Mkdir(socketDirectory, 0o700))

	_, err = prepareUpdaterSocketDirectory(filepath.Join(socketDirectory, "updater.sock"))
	require.NoError(t, err)
}

func TestValidateUpdaterSocketPathComponentRefusesUntrustedAncestorSymlink(t *testing.T) {
	t.Parallel()

	daemonUID := uint32(os.Geteuid()) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	untrustedUID := uint32(1)
	if daemonUID == untrustedUID {
		untrustedUID++
	}

	err := validateUpdaterSocketPathComponent(
		"/shared/attacker-link",
		os.ModeSymlink,
		untrustedUID,
		daemonUID,
		false,
	)
	require.ErrorContains(t, err, "owner uid")
}

func TestPrepareUpdaterSocketDirectoryReturnsCanonicalPath(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "proto-fleet-updater-dir-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })
	originalParent := filepath.Join(root, "original")
	replacementParent := filepath.Join(root, "replacement")
	for _, parent := range []string{originalParent, replacementParent} {
		require.NoError(t, os.MkdirAll(filepath.Join(parent, "socket-dir"), 0o700))
	}
	linkedParent := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(originalParent, linkedParent))
	configuredSocketPath := filepath.Join(linkedParent, "socket-dir", "updater.sock")

	canonicalSocketPath, err := prepareUpdaterSocketDirectory(configuredSocketPath)
	require.NoError(t, err)
	canonicalOriginalDirectory, err := filepath.EvalSymlinks(filepath.Join(originalParent, "socket-dir"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(canonicalOriginalDirectory, "updater.sock"), canonicalSocketPath)

	// Once validation is complete, later operations use the canonical path and
	// cannot be redirected by resolving the configured symlink again.
	require.NoError(t, os.Remove(linkedParent))
	require.NoError(t, os.Symlink(replacementParent, linkedParent))
	listener, err := listenUpdaterSocket(canonicalSocketPath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, listener.Close()) })
	require.NoError(t, secureUpdaterSocket(canonicalSocketPath))
	assert.FileExists(t, filepath.Join(canonicalOriginalDirectory, "updater.sock"))
	assert.NoFileExists(t, filepath.Join(replacementParent, "socket-dir", "updater.sock"))
}

func TestSecureUpdaterSocketOverridesSetgidDirectoryAndSetsMode(t *testing.T) {
	t.Parallel()

	socketPath := shortSocketPath(t)
	directory := filepath.Dir(socketPath)
	inheritedGID := alternateOwnedGroup(t)
	require.NoError(t, os.Chown(directory, os.Geteuid(), inheritedGID))
	require.NoError(t, os.Chmod(directory, 0o750|os.ModeSetgid))
	directoryInfo, err := os.Lstat(directory)
	require.NoError(t, err)
	require.NotZero(t, directoryInfo.Mode()&os.ModeSetgid)
	canonicalSocketPath, err := prepareUpdaterSocketDirectory(socketPath)
	require.NoError(t, err)

	listener, err := listenUpdaterSocket(canonicalSocketPath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, listener.Close()) })
	if inheritedGID != os.Getegid() {
		inheritedInfo, err := os.Lstat(canonicalSocketPath)
		require.NoError(t, err)
		inheritedStat, ok := inheritedInfo.Sys().(*syscall.Stat_t)
		require.True(t, ok)
		require.Equal(t, uint32(inheritedGID), inheritedStat.Gid) //nolint:gosec // Test GIDs are non-negative.
	}

	require.NoError(t, secureUpdaterSocket(canonicalSocketPath))
	info, err := os.Lstat(canonicalSocketPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o660), info.Mode().Perm())
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(os.Geteuid()), stat.Uid) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	assert.Equal(t, uint32(os.Getegid()), stat.Gid) //nolint:gosec // Effective GIDs are non-negative on supported Unix hosts.
}

func alternateOwnedGroup(t *testing.T) int {
	t.Helper()
	for _, gid := range mustGetgroups(t) {
		if gid != os.Getegid() {
			return gid
		}
	}
	if os.Geteuid() == 0 {
		return os.Getegid() + 1
	}
	return os.Getegid()
}

func mustGetgroups(t *testing.T) []int {
	t.Helper()
	groups, err := os.Getgroups()
	require.NoError(t, err)
	return groups
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	// /tmp keeps the path below macOS's short Unix-domain socket limit.
	directory, err := os.MkdirTemp("/tmp", "proto-fleet-updater-test-") //nolint:usetesting
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(directory)) })
	return filepath.Join(directory, "updater.sock")
}
