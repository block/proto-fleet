package sv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolNoiseKeyFromURL_DecodesHex(t *testing.T) {
	// Arrange — 32 bytes of 0x01..0x20 hex-encoded.
	hex := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	url := "stratum2+tcp://pool.example.com:3336/" + hex

	// Act
	key, err := PoolNoiseKeyFromURL(url)

	// Assert
	require.NoError(t, err)
	require.Len(t, key, 32)
	assert.Equal(t, byte(0x01), key[0])
	assert.Equal(t, byte(0x20), key[31])
}

func TestPoolNoiseKeyFromURL_DecodesBase58(t *testing.T) {
	// Arrange — Bitcoin alphabet base58 encoding of 32 zero bytes is "1"*32,
	// which would decode to 32 leading-zero bytes. Use a non-trivial 32-byte
	// payload (all 0xff) base58-encoded.
	encoded := "JEKNVnkbo3jma5nREBBJCDoXFVeKkD56V3xKrvRmWxFG"
	url := "stratum2+tcp://pool.example.com:3336/" + encoded

	// Act
	key, err := PoolNoiseKeyFromURL(url)

	// Assert
	require.NoError(t, err)
	require.Len(t, key, 32)
	for i := range 32 {
		assert.Equal(t, byte(0xff), key[i], "byte %d", i)
	}
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
