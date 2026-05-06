package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCmd_NoState(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	cmd := StatusCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no state")
}

func TestStatusCmd_PopulatedState(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	expiresAt := time.Now().Add(24 * time.Hour).UTC()
	state := &State{
		ServerURL:                 "https://fleet.example.com",
		AgentID:                   42,
		IdentityFingerprint:       "a1b2c3d4e5f60718",
		IdentityPrivateKeyHex:     "deadbeef",
		IdentityPublicKeyHex:      "feedface",
		MinerSigningPrivateKeyHex: "cafebabe",
		MinerSigningPublicKeyHex:  "01020304",
		APIKey:                    "fleet_abcd_xyz",
		SessionToken:              "session-yyy",
		SessionExpiresAt:          expiresAt,
	}
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), state))
	var buf bytes.Buffer
	cmd := StatusCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, &buf)

	// Assert
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "agent_id:              42")
	assert.Contains(t, out, "identity_fingerprint:  a1b2c3d4e5f60718")
	assert.Contains(t, out, "api_key_present:       true")
	assert.Contains(t, out, "session_token_present: true")
	assert.Contains(t, out, "session_expires_at:")
	assert.NotContains(t, out, "fleet_abcd_xyz")
	assert.NotContains(t, out, "session-yyy")
}

func TestStatusCmd_StateWithoutSession(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	state := &State{
		ServerURL:           "https://fleet.example.com",
		AgentID:             7,
		IdentityFingerprint: "0011223344556677",
		APIKey:              "fleet_xx_yy",
	}
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), state))
	var buf bytes.Buffer
	cmd := StatusCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, &buf)

	// Assert
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "session_token_present: false")
	assert.NotContains(t, out, "session_expires_at:")
}
