package agentbootstrap

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
)

func TestIdentityFingerprint_MatchesServer(t *testing.T) {
	t.Parallel()

	// Arrange
	pubkey := make([]byte, 32)
	for i := range pubkey {
		pubkey[i] = byte(i)
	}

	// Act
	got := IdentityFingerprint(pubkey)

	// Assert
	assert.Equal(t, agentenrollment.IdentityFingerprint(pubkey), got)
	assert.Len(t, got, 16)
}

func TestIdentityFingerprint_FormatIsLowercaseHex(t *testing.T) {
	t.Parallel()

	// Arrange
	pubkey := []byte("any-bytes-the-function-must-still-format-cleanly")

	// Act
	got := IdentityFingerprint(pubkey)

	// Assert
	assert.Len(t, got, 16)
	_, err := hex.DecodeString(got)
	require.NoError(t, err)
}

func TestGenerateKeypair_RoundTripsThroughSignVerify(t *testing.T) {
	t.Parallel()

	// Act
	pub, priv, err := GenerateKeypair()

	// Assert
	require.NoError(t, err)
	assert.Len(t, pub, ed25519.PublicKeySize)
	assert.Len(t, priv, ed25519.PrivateKeySize)
	msg := []byte("challenge-bytes")
	sig := ed25519.Sign(priv, msg)
	assert.True(t, ed25519.Verify(pub, msg, sig))
}
