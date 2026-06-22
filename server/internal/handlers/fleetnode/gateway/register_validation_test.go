package gateway

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

func TestRegisterRequestValidation_AllowsOmittedMinerSigningPubkey(t *testing.T) {
	t.Parallel()

	// Arrange
	req := &pb.RegisterRequest{
		EnrollmentToken: strings.Repeat("e", 20),
		Name:            "node-1",
		IdentityPubkey:  make([]byte, ed25519.PublicKeySize),
	}

	// Act
	err := protovalidate.Validate(req)

	// Assert
	require.NoError(t, err)
}

func TestRegisterRequestValidation_RejectsMalformedMinerSigningPubkey(t *testing.T) {
	t.Parallel()

	// Arrange
	req := &pb.RegisterRequest{
		EnrollmentToken:    strings.Repeat("e", 20),
		Name:               "node-1",
		IdentityPubkey:     make([]byte, ed25519.PublicKeySize),
		MinerSigningPubkey: make([]byte, ed25519.PublicKeySize-1),
	}

	// Act
	err := protovalidate.Validate(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "miner_signing_pubkey")
}

func TestRegisterRequestValidation_StillRequiresIdentityPubkey(t *testing.T) {
	t.Parallel()

	// Arrange
	req := &pb.RegisterRequest{
		EnrollmentToken: strings.Repeat("e", 20),
		Name:            "node-1",
	}

	// Act
	err := protovalidate.Validate(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity_pubkey")
}
