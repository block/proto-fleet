package main

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
	require.NoError(t, saveState(path, original))
	loaded, exists, err := loadState(path)

	// Assert
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, original.ServerURL, loaded.ServerURL)
	assert.Equal(t, original.AgentID, loaded.AgentID)
	assert.Equal(t, original.IdentityFingerprint, loaded.IdentityFingerprint)
	assert.Equal(t, original.IdentityPrivateKeyHex, loaded.IdentityPrivateKeyHex)
	assert.Equal(t, original.IdentityPublicKeyHex, loaded.IdentityPublicKeyHex)
	assert.Equal(t, original.MinerSigningPrivateKeyHex, loaded.MinerSigningPrivateKeyHex)
	assert.Equal(t, original.MinerSigningPublicKeyHex, loaded.MinerSigningPublicKeyHex)
	assert.Equal(t, original.APIKey, loaded.APIKey)
	assert.Equal(t, original.SessionToken, loaded.SessionToken)
	assert.True(t, loaded.SessionExpiresAt.Equal(expectedTime), "want %s, got %s", expectedTime, loaded.SessionExpiresAt)
}

func TestLoadState_MissingFile(t *testing.T) {
	t.Parallel()

	// Arrange
	path := filepath.Join(t.TempDir(), "missing", "state.yaml")

	// Act
	st, exists, err := loadState(path)

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
	require.NoError(t, saveState(path, &State{ServerURL: "x"}))

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
		dir, err := resolveStateDir("/custom/dir")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "/custom/dir", dir)
	})

	t.Run("xdg state home wins over default", func(t *testing.T) {
		// Arrange
		t.Setenv("XDG_STATE_HOME", "/tmp/xdg")

		// Act
		dir, err := resolveStateDir("")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "/tmp/xdg/fleet-agent", dir)
	})

	t.Run("default falls back to home/.local/state/fleet-agent", func(t *testing.T) {
		// Arrange
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "/tmp/home")

		// Act
		dir, err := resolveStateDir("")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "/tmp/home/.local/state/fleet-agent", dir)
	})
}
