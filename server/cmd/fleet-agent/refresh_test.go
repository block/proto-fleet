package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
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
	var stdout bytes.Buffer

	// Act
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &stdout, &bytes.Buffer{})

	// Assert
	require.NoError(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "session-after-refresh", loaded.SessionToken)
	assert.Equal(t, fake.expectedAPIKey, loaded.APIKey)
}

func TestRefreshCmd_CompletesPartialEnrollViaPrompt(t *testing.T) {
	t.Parallel()

	// Arrange: state has keys + agent_id but no api_key (simulating an
	// interrupted enroll). Refresh prompts for the api_key on stdin.
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
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader(pastedAPIKey+"\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.NoError(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, pastedAPIKey, loaded.APIKey)
	assert.Equal(t, "session-recovered", loaded.SessionToken)
}

func TestRefreshCmd_RejectsEmptyApiKeyAtPrompt(t *testing.T) {
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
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty api_key")
}

func TestRefreshCmd_PreservesStateOnAPIKeyRejected(t *testing.T) {
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
	staleExpiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		AgentID:                7,
		IdentityFingerprint:    "abc0000000000000",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "wrong-key",
		SessionToken:           "stale-session",
		SessionExpiresAt:       staleExpiry,
	}))
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	// Unauthenticated is ambiguous (revocation vs identity mismatch vs
	// proxy/transient); refresh must preserve local state and surface
	// the error rather than self-destruct on a single failure.
	assert.Equal(t, "wrong-key", loaded.APIKey)
	assert.Equal(t, "stale-session", loaded.SessionToken)
	assert.WithinDuration(t, staleExpiry, loaded.SessionExpiresAt, time.Second)
}

func TestRefreshCmd_PreservesStateOnSignatureFailure(t *testing.T) {
	t.Parallel()

	// Arrange: state's public and private keys belong to different keypairs,
	// so CompleteAuthHandshake's ed25519.Verify will fail.
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
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "good-key", loaded.APIKey, "BeginAuth accepted the api_key; refresh must not wipe it on a CompleteAuth signature failure")
	assert.Equal(t, "still-valid-session", loaded.SessionToken)
	assert.WithinDuration(t, staleExpiry, loaded.SessionExpiresAt, time.Second)
}

func TestRefreshCmd_ConcurrentRefreshesSerialize(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, priv, err := generateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey:   "k",
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x88}, 32),
		sessionToken:     "session-X",
		sessionExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, saveState(filepath.Join(dir, "state.yaml"), &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		AgentID:                22,
		IdentityFingerprint:    "aaaaaaaaaaaaaaaa",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "k",
	}))

	const N = 5
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := range N {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := RefreshCmd{}
			errs[i] = cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
		}()
	}

	// Act
	wg.Wait()

	// Assert (collected after all goroutines join so a require.NoError
	// FailNow cannot leak still-running goroutines).
	for _, e := range errs {
		require.NoError(t, e)
	}
	loaded, _, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "session-X", loaded.SessionToken)
	assert.Equal(t, "k", loaded.APIKey)
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
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}
