package agentbootstrap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadState_RoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	expectedTime := time.Date(2026, 5, 7, 12, 34, 56, 0, time.UTC)
	original := &State{
		ServerURL:                 "http://localhost:4000",
		AgentID:                   42,
		IdentityFingerprint:       "a1b2c3d4e5f60718",
		IdentityPrivateKeyHex:     "aabbccdd",
		IdentityPublicKeyHex:      "1122334455",
		MinerSigningPrivateKeyHex: "ddeeff00",
		MinerSigningPublicKeyHex:  "9988776655",
		APIKey:                    "fleet_aabbccdd_xyz",
		SessionToken:              "session-xxx",
		SessionExpiresAt:          expectedTime,
	}

	// Act
	require.NoError(t, SaveState(path, original))
	loaded, exists, err := LoadState(path)

	// Assert
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, original, loaded)
}

func TestLoadState_MalformedYAML(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not: [valid: yaml"), 0o600))

	// Act
	st, exists, err := LoadState(path)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse state")
	assert.Nil(t, st)
	assert.False(t, exists)
}

func TestSaveState_RejectsSymlinkStateDir(t *testing.T) {
	t.Parallel()

	// Arrange: an attacker-controlled symlink at the state-dir leaf
	// pointing at a different location. SaveState must refuse to follow
	// it rather than write secrets through the link.
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(realDir, 0o700))
	linkDir := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(realDir, linkDir))

	// Act
	err := SaveState(filepath.Join(linkDir, "state.yaml"), &State{ServerURL: "x"})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

func TestSaveState_TightensExistingDirPerms(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "fleet-agent")
	require.NoError(t, os.MkdirAll(stateDir, 0o755)) //nolint:gosec // the whole point of this test is to start with a too-permissive dir
	path := filepath.Join(stateDir, "state.yaml")

	// Act
	require.NoError(t, SaveState(path, &State{ServerURL: "x"}))

	// Assert
	info, err := os.Stat(stateDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestLoadState_MissingFile(t *testing.T) {
	t.Parallel()

	// Arrange
	path := filepath.Join(t.TempDir(), "missing", "state.yaml")

	// Act
	st, exists, err := LoadState(path)

	// Assert
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, &State{}, st)
}

func TestSaveState_Has0600Permissions(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	// Act
	require.NoError(t, SaveState(path, &State{ServerURL: "x"}))

	// Assert
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestResolveStateDir(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		// Arrange
		t.Setenv("XDG_STATE_HOME", "/tmp/xdg")

		// Act
		dir, err := ResolveStateDir("/custom/dir")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "/custom/dir", dir)
	})

	t.Run("xdg state home wins over default", func(t *testing.T) {
		// Arrange
		t.Setenv("XDG_STATE_HOME", "/tmp/xdg")

		// Act
		dir, err := ResolveStateDir("")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "/tmp/xdg/fleet-agent", dir)
	})

	t.Run("default falls back to home/.local/state/fleet-agent", func(t *testing.T) {
		// Arrange
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "/tmp/home")

		// Act
		dir, err := ResolveStateDir("")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "/tmp/home/.local/state/fleet-agent", dir)
	})
}
