package credentialkey

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

func newKeypair(t *testing.T) (publicKey, privateKey []byte) {
	t.Helper()
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	return private.PublicKey().Bytes(), private.Bytes()
}

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

func TestSealOpenRoundTrip(t *testing.T) {
	// Arrange
	publicKey, privateKey := newKeypair(t)
	key := newKey(t)

	// Act
	payload, err := Seal(publicKey, key)
	require.NoError(t, err)
	opened, err := Open(privateKey, payload)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, key, opened)
	assert.Equal(t, Algorithm, payload.GetAlgorithm())
	assert.False(t, bytes.Contains(payload.GetCiphertext(), key), "ciphertext must not contain the plaintext key")
}

func TestSealProducesPayloadThatPassesProtovalidate(t *testing.T) {
	// Arrange
	publicKey, _ := newKeypair(t)
	payload, err := Seal(publicKey, newKey(t))
	require.NoError(t, err)

	// Act
	err = protovalidate.Validate(payload)

	// Assert
	assert.NoError(t, err)
}

func TestSealIsRandomized(t *testing.T) {
	// Arrange
	publicKey, _ := newKeypair(t)
	key := newKey(t)

	// Act
	first, err := Seal(publicKey, key)
	require.NoError(t, err)
	second, err := Seal(publicKey, key)
	require.NoError(t, err)

	// Assert
	assert.NotEqual(t, first.GetEphemeralPubkey(), second.GetEphemeralPubkey())
	assert.NotEqual(t, first.GetNonce(), second.GetNonce())
	assert.NotEqual(t, first.GetCiphertext(), second.GetCiphertext())
}

func TestOpenRejectsDifferentRecipient(t *testing.T) {
	// Arrange
	publicKey, _ := newKeypair(t)
	_, otherPrivateKey := newKeypair(t)
	payload, err := Seal(publicKey, newKey(t))
	require.NoError(t, err)

	// Act
	opened, err := Open(otherPrivateKey, payload)

	// Assert
	require.Error(t, err)
	assert.Nil(t, opened)
}

func TestOpenRejectsTamperedPayload(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*gatewaypb.NodeEncryptedPayload)
	}{
		{"ciphertext", func(p *gatewaypb.NodeEncryptedPayload) { p.Ciphertext[0] ^= 0x01 }},
		{"nonce", func(p *gatewaypb.NodeEncryptedPayload) { p.Nonce[0] ^= 0x01 }},
		{"ephemeral public key", func(p *gatewaypb.NodeEncryptedPayload) { p.EphemeralPubkey[0] ^= 0x01 }},
		{"truncated ciphertext", func(p *gatewaypb.NodeEncryptedPayload) { p.Ciphertext = p.Ciphertext[:len(p.Ciphertext)-1] }},
		{"short nonce", func(p *gatewaypb.NodeEncryptedPayload) { p.Nonce = p.Nonce[:len(p.Nonce)-1] }},
		{"short ephemeral public key", func(p *gatewaypb.NodeEncryptedPayload) { p.EphemeralPubkey = p.EphemeralPubkey[:16] }},
		{"empty ciphertext", func(p *gatewaypb.NodeEncryptedPayload) { p.Ciphertext = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			publicKey, privateKey := newKeypair(t)
			payload, err := Seal(publicKey, newKey(t))
			require.NoError(t, err)
			tc.tamper(payload)

			// Act
			opened, err := Open(privateKey, payload)

			// Assert
			require.Error(t, err)
			assert.Nil(t, opened)
		})
	}
}

func TestOpenRejectsWrongAlgorithm(t *testing.T) {
	// Arrange
	publicKey, privateKey := newKeypair(t)
	payload, err := Seal(publicKey, newKey(t))
	require.NoError(t, err)
	payload.Algorithm = "aes-128-cbc"

	// Act
	_, err = Open(privateKey, payload)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported encrypted payload algorithm")
}

func TestOpenRejectsNilPayload(t *testing.T) {
	// Arrange
	_, privateKey := newKeypair(t)

	// Act
	_, err := Open(privateKey, nil)

	// Assert
	require.Error(t, err)
}

func TestOpenRejectsInvalidPrivateKey(t *testing.T) {
	// Arrange
	publicKey, _ := newKeypair(t)
	payload, err := Seal(publicKey, newKey(t))
	require.NoError(t, err)

	// Act
	_, err = Open([]byte("short"), payload)

	// Assert
	require.Error(t, err)
}

func TestSealRejectsWrongKeyLength(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"nil", nil},
		{"short", make([]byte, KeySize-1)},
		{"long", make([]byte, KeySize+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			publicKey, _ := newKeypair(t)

			// Act
			payload, err := Seal(publicKey, tc.key)

			// Assert
			require.Error(t, err)
			assert.Nil(t, payload)
		})
	}
}

func TestSealRejectsBadRecipientPublicKey(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"nil", nil},
		{"short", make([]byte, 31)},
		{"long", make([]byte, 33)},
		{"low order point", make([]byte, 32)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			key := newKey(t)

			// Act
			payload, err := Seal(tc.key, key)

			// Assert
			require.Error(t, err)
			assert.Nil(t, payload)
		})
	}
}

func TestFingerprint(t *testing.T) {
	// Arrange
	key := newKey(t)
	otherKey := newKey(t)

	// Act
	first := Fingerprint(key)
	second := Fingerprint(key)
	other := Fingerprint(otherKey)

	// Assert
	assert.Len(t, first, 32)
	assert.Equal(t, first, second)
	assert.NotEqual(t, first, other)
	assert.NotEqual(t, key, first)
}

func TestFingerprintIsNotPlainSHA256AndDoesNotMutateKey(t *testing.T) {
	// Arrange
	key := newKey(t)
	original := append([]byte(nil), key...)
	plain := sha256.Sum256(key)

	// Act
	fingerprint := Fingerprint(key)

	// Assert
	assert.Equal(t, original, key)
	assert.NotEqual(t, plain[:], fingerprint)
}
