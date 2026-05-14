package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
)

func TestRefreshCmd_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, priv, err := fleetnodebootstrap.GenerateKeypair()
	require.NoError(t, err)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:   "fleet_known_key",
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x33}, 32),
		sessionToken:     "session-after-refresh",
		sessionExpiresAt: expiresAt,
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), &fleetnodebootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
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
	loaded, _, err := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	require.NoError(t, err)
	assert.Equal(t, "session-after-refresh", loaded.SessionToken)
	assert.Equal(t, fake.expectedAPIKey, loaded.APIKey)
	assert.Contains(t, stdout.String(), "refreshed session_expires_at=")
}

func TestRefreshCmd_PromptsForApiKeyAndPersistsOnSuccess(t *testing.T) {
	t.Parallel()

	// Arrange: state has keys + fleet_node_id but no api_key (simulating an
	// interrupted enroll). Refresh prompts for the api_key on stdin and
	// persists it only after a successful handshake.
	dir := t.TempDir()
	pub, priv, err := fleetnodebootstrap.GenerateKeypair()
	require.NoError(t, err)
	const pastedAPIKey = "fleet_pasted_after_recovery" //nolint:gosec // test fixture, not a real credential
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:   pastedAPIKey,
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x44}, 32),
		sessionToken:     "session-recovered",
		sessionExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), &fleetnodebootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            100,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
	}))
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader(pastedAPIKey+"\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.NoError(t, err)
	loaded, _, err := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	require.NoError(t, err)
	assert.Equal(t, pastedAPIKey, loaded.APIKey)
	assert.Equal(t, "session-recovered", loaded.SessionToken)
}

func TestRefreshCmd_RejectsEmptyApiKeyAtPrompt(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), &fleetnodebootstrap.State{
		ServerURL:             "https://fleet.example.com",
		FleetNodeID:           1,
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

func TestRefreshCmd_PreservesStateOnBeginAuthRejection(t *testing.T) {
	t.Parallel()

	// Arrange: handshake rejects the stored api_key. Local credentials
	// (api_key + keys + fleet_node_id) must be preserved so the operator
	// can re-run after the server-side cause is resolved without
	// re-enrolling. (PR #187 commit 3184f04 #3.)
	dir := t.TempDir()
	pub, priv, err := fleetnodebootstrap.GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey: "the-only-key-the-server-will-accept",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x77}, 32),
	}
	srv := newFakeServer(t, fake)
	initial := &fleetnodebootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            7,
		IdentityFingerprint:    "deadbeefcafebabe",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "stored-but-revoked-server-side",
	}
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), initial))
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, fleetnodebootstrap.ErrBeginAuthRejected)
	assert.Contains(t, err.Error(), "Local credentials are preserved")
	assert.NotContains(t, err.Error(), "invalid api_key", "the server-side message must not leak through; the CLI gives generic guidance for all Unauthenticated causes")
	loaded, _, _ := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	assert.Equal(t, initial.APIKey, loaded.APIKey)
	assert.Equal(t, initial.IdentityPrivateKeyHex, loaded.IdentityPrivateKeyHex)
	assert.Equal(t, int64(7), loaded.FleetNodeID)
}

func TestRefreshCmd_DoesNotPersistWrongPastedApiKey(t *testing.T) {
	t.Parallel()

	// Arrange: state has no api_key (partial enroll). Operator pastes the
	// wrong key at the prompt; BeginAuth rejects it. The wrong key must NOT
	// land on disk — otherwise the next refresh would skip the prompt and
	// silently retry the bad key, leaving the operator with no CLI path to
	// replace it. (Codex PR #221 finding: refresh.go:48.)
	dir := t.TempDir()
	pub, priv, err := fleetnodebootstrap.GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey: "the-correct-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x88}, 32),
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), &fleetnodebootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            123,
		IdentityFingerprint:    "feedface00112233",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
	}))
	cmd := RefreshCmd{}

	// Act
	err = cmd.run(&Context{StateDir: dir}, strings.NewReader("the-wrong-key\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, fleetnodebootstrap.ErrBeginAuthRejected)
	loaded, _, _ := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	assert.Empty(t, loaded.APIKey, "wrong api_key must not be persisted; the next refresh must prompt again")
	assert.Equal(t, int64(123), loaded.FleetNodeID, "keys + fleet_node_id are preserved so the operator can retry")
}

func TestRefreshCmd_FailsWhenNoState(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	cmd := RefreshCmd{}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fleetnode enroll")
}

func TestRefreshCmd_DoesNotTouchStateDirWhenNeverEnrolled(t *testing.T) {
	t.Parallel()

	// Arrange: point at a state dir that does not exist. The "no state"
	// bailout must happen before any side-effecting filesystem operation
	// (no state.lock, no MkdirAll, no chmod). (Copilot PR #221 finding:
	// refresh.go:24.)
	parent := t.TempDir()
	stateDir := filepath.Join(parent, "never-existed")
	cmd := RefreshCmd{}

	// Act
	err := cmd.run(&Context{StateDir: stateDir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fleetnode enroll")
	_, statErr := os.Stat(stateDir)
	assert.True(t, os.IsNotExist(statErr), "state dir must not be created when refresh bails out on missing state")
}
