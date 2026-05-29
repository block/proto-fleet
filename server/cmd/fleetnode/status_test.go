package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

func TestStatusCmd_FailsWhenNoState(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	cmd := StatusCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fleetnode enroll")
}

func TestStatusCmd_RedactsSecrets(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	const apiKey = "fleet_super_secret_apikey_xyz" //nolint:gosec // test fixture
	const sessionToken = "session-token-also-secret"
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:             "https://fleet.example.com",
		FleetNodeID:           1234,
		IdentityFingerprint:   "abcdef0123456789",
		IdentityPrivateKeyHex: hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:  hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		APIKey:                apiKey,
		SessionToken:          sessionToken,
		SessionExpiresAt:      time.Now().Add(1 * time.Hour),
	}))
	cmd := StatusCmd{}
	var stdout bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: dir}, &stdout)

	// Assert
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "fleet_node_id:         1234")
	assert.Contains(t, out, "identity_fingerprint:  abcdef0123456789")
	assert.Contains(t, out, "api_key_present:       true")
	assert.Contains(t, out, "session_token_present: true")
	assert.NotContains(t, out, apiKey, "status output must NOT include the api_key plaintext")
	assert.NotContains(t, out, sessionToken, "status output must NOT include the session_token plaintext")
}

func TestStatusCmd_FlagsMissingSecretsAsFalse(t *testing.T) {
	t.Parallel()

	// Arrange: partial state (post-Register, pre-CompleteEnrollment) shows
	// api_key_present=false so an operator can spot recovery candidates.
	dir := t.TempDir()
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:             "https://fleet.example.com",
		FleetNodeID:           9,
		IdentityFingerprint:   "0011223344556677",
		IdentityPrivateKeyHex: hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:  hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
	}))
	cmd := StatusCmd{}
	var stdout bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: dir}, &stdout)

	// Assert
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "api_key_present:       false")
	assert.Contains(t, out, "session_token_present: false")
}
