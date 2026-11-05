package pools

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/infrastructure/pooldial"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConnection_SV2URLRejectsMissingPubkey(t *testing.T) {
	// Arrange — without /PUBKEY in the path the URL would have slipped past
	// the CEL rule (defensive: the service still must refuse rather than
	// silently fall back to a meaningless TCP dial).
	url := "stratum2+tcp://pool.example.com:3336"
	svc := NewService(Config{Timeout: time.Second})

	// Act
	ok, err := svc.ValidateConnection(t.Context(), url, "anything", nil, nil)

	// Assert
	require.Error(t, err)
	assert.False(t, ok)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestValidateConnection_SV2URLHandshakeFailureSurfaces(t *testing.T) {
	// Arrange — TEST-NET-1 (RFC 5737) address that won't respond. A short
	// timeout keeps the test fast; pubkey is well-formed so the failure has
	// to come from the handshake/dial, not validation.
	pubkey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	url := "stratum2+tcp://192.0.2.1:34254/" + pubkey
	svc := NewService(Config{Timeout: 100 * time.Millisecond})

	// Act
	ok, err := svc.ValidateConnection(t.Context(), url, "anything", nil, nil)

	// Assert
	assert.False(t, ok)
	assert.Error(t, err)
}

func TestValidateConnectionCustomerPolicyCoversBothProtocols(t *testing.T) {
	svc := NewService(Config{Timeout: time.Second})
	// X coordinate of secp256k1's generator: a valid Noise authority public key.
	const authority = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	for _, url := range []string{
		"stratum+tcp://127.0.0.1:3333",
		"stratum+tcp://[::1]:3333",
		"stratum+tcp://[::ffff:127.0.0.1]:3333",
		"stratum2+tcp://127.0.0.1:3336/" + authority,
		"stratum2+tcp://[::1]:3336/" + authority,
		"stratum2+tcp://[::ffff:127.0.0.1]:3336/" + authority,
	} {
		t.Run(url, func(t *testing.T) {
			connected, err := svc.ValidateConnection(pooldial.PublicOnly(t.Context()), url, "payout", nil, nil)
			require.ErrorContains(t, err, pooldial.ErrPrivateDestination.Error(), "the real protocol path must enforce destination policy before connecting")
			require.False(t, connected)
		})
	}
}
