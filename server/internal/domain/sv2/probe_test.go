package sv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolNoiseKeyFromURL_DecodesHex(t *testing.T) {
	// secp256k1 generator G's X coordinate — a known valid X-only pubkey.
	hex := "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	url := "stratum2+tcp://pool.example.com:3336/" + hex

	// Act
	key, err := PoolNoiseKeyFromURL(url)

	// Assert
	require.NoError(t, err)
	require.Len(t, key, 32)
	assert.Equal(t, byte(0x79), key[0])
	assert.Equal(t, byte(0x98), key[31])
}

func TestPoolNoiseKeyFromURL_DecodesSRIFramedBase58(t *testing.T) {
	// Arrange — the Braiins-published authority pubkey for v2.stratum.braiins.com:3336.
	// This is SRI's framed format (version + 32 key bytes + 4-byte checksum).
	encoded := "9awtMD5KQgvRUh2yFbjVeT7b6hjipWcAsQHd6wEhgtDT9soosna"
	url := "stratum2+tcp://v2.stratum.braiins.com:3336/" + encoded

	// Act
	key, err := PoolNoiseKeyFromURL(url)

	// Assert
	require.NoError(t, err)
	assert.Len(t, key, 32, "framed pubkey should yield 32 raw key bytes")
}

func TestPoolNoiseKeyFromURL_RejectsInvalidPubKey(t *testing.T) {
	// 32 bytes of 0xff base58-encoded — decodes cleanly but the X
	// coordinate exceeds the secp256k1 field prime, so it must not
	// be accepted.
	encoded := "JEKNVnkbo3jma5nREBBJCDoXFVeKkD56V3xKrvRmWxFG"
	url := "stratum2+tcp://pool.example.com:3336/" + encoded

	// Act
	_, err := PoolNoiseKeyFromURL(url)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secp256k1")
}

func TestPoolNoiseKeyFromURL_RejectsMissingPath(t *testing.T) {
	// Act
	_, err := PoolNoiseKeyFromURL("stratum2+tcp://pool.example.com:3336")

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingNoiseKey)
}

func TestPoolNoiseKeyFromURL_RejectsBadEncoding(t *testing.T) {
	// Arrange — neither valid base58 nor hex.
	url := "stratum2+tcp://pool.example.com:3336/0OIl-not-a-key"

	// Act
	_, err := PoolNoiseKeyFromURL(url)

	// Assert
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "32 bytes"))
}
