package sv2

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/ellswift"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
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
	// Act
	state := newHandshakeState()

	// Assert — ck = HASH(name), h = HASH(ck) (mixHash empty prologue).
	assert.Equal(t, noiseProtocolHash, state.ck)
	assert.Equal(t, sha256.Sum256(noiseProtocolHash[:]), state.h)
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

// TestV2Ecdh_MatchesBIP324Spec hand-composes the BIP324 tagged hash against
// V2Ecdh's output for the same inputs. SRI's Noise NX uses libsecp's
// secp256k1_ellswift_xdh_hash_function_bip324 (see bitcoin-core/secp256k1
// src/modules/ellswift/main_impl.h):
//
//	output = TaggedHash("bip324_ellswift_xonly_ecdh", ell_a || ell_b || x32)
//
// where ell_a is the initiator's ellswift, ell_b the responder's, and x32
// the X-coordinate of priv*lift_x(decode(ellswift_theirs)). If V2Ecdh diverges
// from that composition, real SRI responders will fail AEAD on message 2.
func TestV2Ecdh_MatchesBIP324Spec(t *testing.T) {
	// Arrange -- two ellswift keypairs; A is the initiator.
	privA, ellA, err := ellswift.EllswiftCreate()
	require.NoError(t, err)
	_, ellB, err := ellswift.EllswiftCreate()
	require.NoError(t, err)

	// Act
	got, err := ellswift.V2Ecdh(privA, ellB, ellA, true)
	require.NoError(t, err)

	x32, err := ellswift.EllswiftECDHXOnly(ellB, privA)
	require.NoError(t, err)
	var msg []byte
	msg = append(msg, ellA[:]...)
	msg = append(msg, ellB[:]...)
	msg = append(msg, x32[:]...)
	want := chainhash.TaggedHash([]byte("bip324_ellswift_xonly_ecdh"), msg)

	// Assert
	assert.Equal(t, want[:], got[:],
		"V2Ecdh(initiating=true) must equal TaggedHash(\"bip324_ellswift_xonly_ecdh\", ourEllswift || theirEllswift || x32)")
}

// TestV2Ecdh_SymmetricBetweenSides confirms initiator and responder derive
// the same shared secret from V2Ecdh, as required by the Noise NX
// MixKey(ECDH(e, re)) step on both sides of the handshake. If this fails
// our chain key would diverge from any peer.
func TestV2Ecdh_SymmetricBetweenSides(t *testing.T) {
	// Arrange
	privA, ellA, err := ellswift.EllswiftCreate()
	require.NoError(t, err)
	privB, ellB, err := ellswift.EllswiftCreate()
	require.NoError(t, err)

	// Act -- A is initiator, B is responder; both must compute the same secret.
	fromInitiator, err := ellswift.V2Ecdh(privA, ellB, ellA, true)
	require.NoError(t, err)
	fromResponder, err := ellswift.V2Ecdh(privB, ellA, ellB, false)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, fromInitiator[:], fromResponder[:],
		"initiator and responder must derive the same shared secret from V2Ecdh")
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
