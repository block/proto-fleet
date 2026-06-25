package gateway

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

func TestRegisterRequestValidation_AllowsIdentityAndEncryptionKeys(t *testing.T) {
	t.Parallel()

	// Arrange
	req := &pb.RegisterRequest{
		EnrollmentToken:  strings.Repeat("e", 20),
		Name:             "node-1",
		IdentityPubkey:   make([]byte, ed25519.PublicKeySize),
		EncryptionPubkey: bytes.Repeat([]byte{1}, 32),
	}

	// Act
	err := protovalidate.Validate(req)

	// Assert
	require.NoError(t, err)
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

func TestRegisterRequestValidation_RequiresEncryptionPubkey(t *testing.T) {
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encryption_pubkey")
}

func TestUpdateMinerPasswordActionValidation_RequiresEncryptedPayload(t *testing.T) {
	t.Parallel()

	// Act
	err := protovalidate.Validate(&pb.UpdateMinerPasswordAction{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypted_password_update")
}

func TestUpdateMinerPasswordActionValidation_AllowsEncryptedPayload(t *testing.T) {
	t.Parallel()

	// Arrange
	req := &pb.UpdateMinerPasswordAction{
		EncryptedPasswordUpdate: &pb.NodeEncryptedPayload{
			Algorithm:       "x25519-hkdf-sha256-aes-256-gcm-v1",
			EphemeralPubkey: bytes.Repeat([]byte{1}, 32),
			Nonce:           bytes.Repeat([]byte{2}, 12),
			Ciphertext:      bytes.Repeat([]byte{3}, 17),
		},
	}

	// Act
	err := protovalidate.Validate(req)

	// Assert
	require.NoError(t, err)
}
