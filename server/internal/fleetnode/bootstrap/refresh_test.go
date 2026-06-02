package bootstrap

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefresh_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := GenerateKeypair()
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
	state := &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "abcdef0123456789",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 fake.expectedAPIKey,
	}

	// Act
	err = Refresh(t.Context(), state)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "session-after-refresh", state.SessionToken)
	assert.WithinDuration(t, expiresAt, state.SessionExpiresAt, time.Second)
}

func TestRefresh_RequiresAPIKey(t *testing.T) {
	t.Parallel()

	// Arrange
	state := &State{
		ServerURL:             "https://fleet.example.com",
		FleetNodeID:           1,
		IdentityFingerprint:   "0000000000000000",
		IdentityPrivateKeyHex: hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:  hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
	}

	// Act
	err := Refresh(t.Context(), state)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no api_key")
}

func TestRefresh_PreservesStateOnAPIKeyRejected(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "right-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x55}, 32),
	}
	srv := newFakeServer(t, fake)
	staleExpiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	state := &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            7,
		IdentityFingerprint:    "abc0000000000000",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "wrong-key",
		SessionToken:           "stale-session",
		SessionExpiresAt:       staleExpiry,
	}

	// Act
	err = Refresh(t.Context(), state)

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBeginAuthRejected)
	// Refresh must not mutate the in-memory state on failure.
	assert.Equal(t, "wrong-key", state.APIKey)
	assert.Equal(t, "stale-session", state.SessionToken)
	assert.Equal(t, staleExpiry, state.SessionExpiresAt)
}

func TestRefresh_PreservesStateOnSignatureFailure(t *testing.T) {
	t.Parallel()

	// Arrange: state's public and private keys belong to different keypairs,
	// so CompleteAuthHandshake's ed25519.Verify will fail.
	pub, _, err := GenerateKeypair()
	require.NoError(t, err)
	_, otherPriv, err := GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "good-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x66}, 32),
	}
	srv := newFakeServer(t, fake)
	staleExpiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	state := &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            9,
		IdentityFingerprint:    "def0000000000000",
		IdentityPrivateKeyHex:  hex.EncodeToString(otherPriv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "good-key",
		SessionToken:           "still-valid-session",
		SessionExpiresAt:       staleExpiry,
	}

	// Act
	err = Refresh(t.Context(), state)

	// Assert
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrBeginAuthRejected, "BeginAuth accepted the api_key; CompleteAuth signature failure must not surface as api_key rejection")
	assert.Equal(t, "good-key", state.APIKey)
	assert.Equal(t, "still-valid-session", state.SessionToken)
	assert.Equal(t, staleExpiry, state.SessionExpiresAt)
}

func TestRefresh_RejectsNonHTTPS(t *testing.T) {
	t.Parallel()

	// Arrange
	state := &State{
		ServerURL:              "http://fleet.example.com",
		AllowInsecureTransport: false,
		FleetNodeID:            3,
		IdentityFingerprint:    "1111111111111111",
		IdentityPrivateKeyHex:  hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:   hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		APIKey:                 "k",
	}

	// Act
	err := Refresh(t.Context(), state)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}
