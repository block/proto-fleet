// Package credentialkey seals a Fleet Node's miner-credential key to its HA
// standby. fleetd relays the sealed key between the two nodes but cannot
// decrypt it: only the standby's enrollment encryption key can.
package credentialkey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const (
	Algorithm = "x25519-hkdf-sha256-aes-256-gcm-v1"

	// KeySize is the size of the AES-256 miner-credential key.
	KeySize = 32

	x25519KeySize = 32
	nonceSize     = 12
)

var (
	hkdfSalt          = []byte("proto-fleet/fleet-node/credential-key/salt/v1")
	hkdfInfo          = []byte("proto-fleet/fleet-node/credential-key/aes-256-gcm/v1")
	fingerprintDomain = []byte("proto-fleet/fleet-node/credential-key/fingerprint/v1")
	associatedData    = []byte(Algorithm + "/credential-key")
)

// Seal encrypts key to the recipient's X25519 public key.
func Seal(recipientPublicKey, key []byte) (*gatewaypb.NodeEncryptedPayload, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("credential key must be %d bytes, got %d", KeySize, len(key))
	}
	if len(recipientPublicKey) != x25519KeySize {
		return nil, fmt.Errorf("recipient public key must be %d bytes, got %d", x25519KeySize, len(recipientPublicKey))
	}
	recipient, err := ecdh.X25519().NewPublicKey(recipientPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse recipient public key: %w", err)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}
	aead, err := aeadFromSharedSecret(shared)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return &gatewaypb.NodeEncryptedPayload{
		Algorithm:       Algorithm,
		EphemeralPubkey: ephemeral.PublicKey().Bytes(),
		Nonce:           nonce,
		Ciphertext:      aead.Seal(nil, nonce, key, associatedData),
	}, nil
}

// Open decrypts a key sealed to the X25519 public key of privateKey.
func Open(privateKey []byte, payload *gatewaypb.NodeEncryptedPayload) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("sealed credential key is required")
	}
	if payload.GetAlgorithm() != Algorithm {
		return nil, fmt.Errorf("unsupported encrypted payload algorithm %q", payload.GetAlgorithm())
	}
	if len(payload.GetNonce()) != nonceSize {
		return nil, fmt.Errorf("nonce must be %d bytes, got %d", nonceSize, len(payload.GetNonce()))
	}
	recipient, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("parse recipient private key: %w", err)
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(payload.GetEphemeralPubkey())
	if err != nil {
		return nil, fmt.Errorf("parse ephemeral public key: %w", err)
	}
	shared, err := recipient.ECDH(ephemeral)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}
	aead, err := aeadFromSharedSecret(shared)
	if err != nil {
		return nil, err
	}
	key, err := aead.Open(nil, payload.GetNonce(), payload.GetCiphertext(), associatedData)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential key: %w", err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("credential key must be %d bytes, got %d", KeySize, len(key))
	}
	return key, nil
}

// Fingerprint lets fleetd confirm both nodes hold the same key without seeing it.
func Fingerprint(key []byte) []byte {
	sum := sha256.Sum256(append(append([]byte(nil), fingerprintDomain...), key...))
	return sum[:]
}

func aeadFromSharedSecret(shared []byte) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, hkdfSalt, hkdfInfo), key); err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create credential key cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential key AEAD: %w", err)
	}
	return aead, nil
}
