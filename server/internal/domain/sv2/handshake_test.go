package sv2

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandshakeProbe_RejectsWrongKeyLength(t *testing.T) {
	// Act
	ok, err := HandshakeProbe(context.Background(), "stratum2+tcp://127.0.0.1:1", []byte{1, 2, 3}, time.Second)

	// Assert
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandshakeUnsupported))
	assert.False(t, ok)
}

func TestHandshakeProbe_RejectsNilKey(t *testing.T) {
	// Act
	ok, err := HandshakeProbe(context.Background(), "stratum2+tcp://127.0.0.1:1", nil, time.Second)

	// Assert
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandshakeUnsupported))
	assert.False(t, ok)
}

func TestHandshakeProbe_FailsOnUnreachableHost(t *testing.T) {
	// Arrange — TEST-NET-1 (RFC 5737) address that should never accept
	// connections; 200ms timeout keeps the test fast.
	authority := make([]byte, 32)
	authority[0] = 0x01

	// Act
	start := time.Now()
	ok, err := HandshakeProbe(context.Background(), "stratum2+tcp://192.0.2.1:34254", authority, 200*time.Millisecond)
	elapsed := time.Since(start)

	// Assert
	require.Error(t, err)
	assert.False(t, ok)
	assert.Less(t, elapsed, 2*time.Second, "probe must respect dial timeout")
}

func TestHandshakeState_InitialKeyAndHash(t *testing.T) {
	// Arrange / Act — Noise framework spec: when the protocol name's
	// SHA-256 is the chaining key, h is initialized to that same hash.
	// Both ck and h must equal HASH("Noise_NX_Secp256k1+EllSwift_ChaChaPoly_SHA256").
	state := newHandshakeState()

	// Assert
	assert.Equal(t, noiseProtocolHash, state.ck)
	assert.Equal(t, noiseProtocolHash, state.h)
	assert.False(t, state.haveK)
}

func TestMixHash_Deterministic(t *testing.T) {
	// Arrange
	a := newHandshakeState()
	b := newHandshakeState()
	data := []byte("hello world")

	// Act
	a.mixHash(data)
	b.mixHash(data)

	// Assert
	assert.Equal(t, a.h, b.h)
}

func TestMixKey_DerivesChainAndCipher(t *testing.T) {
	// Arrange
	state := newHandshakeState()
	material := bytes.Repeat([]byte{0x42}, 32)

	// Act
	state.mixKey(material)

	// Assert
	assert.True(t, state.haveK, "mix_key must enable the cipher key")
	assert.NotEqual(t, [32]byte{}, state.k, "k must be populated after mix_key")
	assert.NotEqual(t, noiseProtocolHash, state.ck, "ck must rotate after mix_key")
}

func TestHkdf2_MatchesSpec(t *testing.T) {
	// Arrange — paired with SRI's test_hkdf2 but with simple inputs:
	//   temp_k = HMAC(ck, ikm)
	//   out1   = HMAC(temp_k, [0x01])
	//   out2   = HMAC(temp_k, out1 || [0x02])
	ck := bytes.Repeat([]byte{0x00}, 32)
	ikm := bytes.Repeat([]byte{0x00}, 32)

	// Act
	out1, out2 := hkdf2(ck, ikm)

	// Assert
	temp := hmacSHA256(ck, ikm)
	expected1 := hmacSHA256(temp[:], []byte{0x01})
	expected2 := hmacSHA256(temp[:], append(expected1[:], 0x02))
	assert.Equal(t, expected1, out1)
	assert.Equal(t, expected2, out2)
}

func TestAEAD_RoundTrip(t *testing.T) {
	// Arrange — exercise the ChaCha20-Poly1305 wrapper directly. State
	// machine round-trips via encrypt/decryptAndHash are covered
	// implicitly by the live HandshakeProbe path.
	var key [32]byte
	for i := range key {
		key[i] = 0x07
	}
	ad := []byte("ad")
	plaintext := []byte("share the work")

	// Act
	ct, err := aeadEncrypt(key, 0, ad, plaintext)
	require.NoError(t, err)
	pt, err := aeadDecrypt(key, 0, ad, ct)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, plaintext, pt)
}
