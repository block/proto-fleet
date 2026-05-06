package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshCmd_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, priv, err := generateKeypair()
	require.NoError(t, err)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	fake := &fakeAgentGateway{
		expectedAPIKey:   "fleet_known_key",
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x33}, 32),
		sessionToken:     "session-after-refresh",
		sessionExpiresAt: expiresAt,
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		AgentID:                42,
		IdentityFingerprint:    "abcdef0123456789",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 fake.expectedAPIKey,
	}))
	cmd := RefreshCmd{}
	var w bytes.Buffer

	// Act
	err = cmd.run(&Context{StateDir: dir}, &w)

	// Assert
	require.NoError(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "session-after-refresh", loaded.SessionToken)
	assert.Equal(t, fake.expectedAPIKey, loaded.APIKey)
}

func TestRefreshCmd_CompletesPartialEnrollViaFlag(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, priv, err := generateKeypair()
	require.NoError(t, err)
	const pastedAPIKey = "fleet_pasted_after_recovery" //nolint:gosec // test fixture, not a real credential
	fake := &fakeAgentGateway{
		expectedAPIKey:   pastedAPIKey,
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x44}, 32),
		sessionToken:     "session-recovered",
		sessionExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		AgentID:                100,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
	}))
	cmd := RefreshCmd{APIKey: pastedAPIKey}

	// Act
	err = cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.NoError(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, pastedAPIKey, loaded.APIKey)
	assert.Equal(t, "session-recovered", loaded.SessionToken)
}

func TestRefreshCmd_RequiresApiKey(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:             "https://fleet.example.com",
		AgentID:               1,
		IdentityFingerprint:   "0000000000000000",
		IdentityPrivateKeyHex: hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:  hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
	}))
	cmd := RefreshCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no api_key")
}

func TestRefreshCmd_WipesStateOnAPIKeyRejected(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, priv, err := generateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "right-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x55}, 32),
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		AgentID:                7,
		IdentityFingerprint:    "abc0000000000000",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "wrong-key",
		SessionToken:           "stale-session",
		SessionExpiresAt:       time.Now().Add(time.Hour),
	}))
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Empty(t, loaded.APIKey)
	assert.Empty(t, loaded.SessionToken)
	assert.True(t, loaded.SessionExpiresAt.IsZero())
}

func TestRefreshCmd_PreservesStateOnSignatureFailure(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, _, err := generateKeypair()
	require.NoError(t, err)
	_, otherPriv, err := generateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "good-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x66}, 32),
	}
	srv := newFakeServer(t, fake)
	staleExpiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		AgentID:                9,
		IdentityFingerprint:    "def0000000000000",
		IdentityPrivateKeyHex:  hex.EncodeToString(otherPriv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "good-key",
		SessionToken:           "still-valid-session",
		SessionExpiresAt:       staleExpiry,
	}))
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "good-key", loaded.APIKey, "BeginAuth accepted the api_key; refresh must not wipe it on a CompleteAuth signature failure")
	assert.Equal(t, "still-valid-session", loaded.SessionToken)
	assert.WithinDuration(t, staleExpiry, loaded.SessionExpiresAt, time.Second)
}

func TestRefreshCmd_RejectsNonHTTPSWhenAllowInsecureUnset(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              "http://fleet.example.com",
		AllowInsecureTransport: false,
		AgentID:                3,
		IdentityFingerprint:    "1111111111111111",
		IdentityPrivateKeyHex:  hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:   hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		APIKey:                 "k",
	}))
	cmd := RefreshCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}
