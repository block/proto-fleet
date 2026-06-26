package passwordupdate

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const (
	Algorithm = "x25519-hkdf-sha256-aes-256-gcm-v1"

	x25519KeySize = 32
	nonceSize     = 12
)

var (
	// ErrInvalidRecipientPublicKey identifies bad fleet-node password-update recipient keys.
	ErrInvalidRecipientPublicKey = errors.New("invalid recipient public key")

	hkdfSalt = []byte("proto-fleet/fleet-node/password-update/salt/v1")
	hkdfInfo = []byte("proto-fleet/fleet-node/password-update/aes-256-gcm/v1")
)

type Secret struct {
	DeviceIdentifier string `json:"device_identifier"`
	CurrentPassword  string `json:"current_password"`
	NewPassword      string `json:"new_password"`
}

func GenerateKeypair() (publicKey, privateKey []byte, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate x25519 keypair: %w", err)
	}
	return append([]byte(nil), key.PublicKey().Bytes()...), append([]byte(nil), key.Bytes()...), nil
}

func ValidateRecipientPublicKey(publicKey []byte) error {
	recipient, err := recipientPublicKey(publicKey)
	if err != nil {
		return err
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ephemeral key: %w", err)
	}
	if _, err := ephemeral.ECDH(recipient); err != nil {
		return fmt.Errorf("%w: validate recipient public key: %v", ErrInvalidRecipientPublicKey, err)
	}
	return nil
}

func Encrypt(publicKey []byte, secret Secret) (*gatewaypb.NodeEncryptedPayload, error) {
	recipient, err := recipientPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return nil, fmt.Errorf("%w: derive shared secret: %v", ErrInvalidRecipientPublicKey, err)
	}
	aead, err := aeadFromSharedSecret(shared)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	plaintext, err := json.Marshal(secret)
	if err != nil {
		return nil, fmt.Errorf("marshal password update secret: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, associatedData(secret.DeviceIdentifier))
	return &gatewaypb.NodeEncryptedPayload{
		Algorithm:       Algorithm,
		EphemeralPubkey: append([]byte(nil), ephemeral.PublicKey().Bytes()...),
		Nonce:           nonce,
		Ciphertext:      ciphertext,
	}, nil
}

func Decrypt(privateKey []byte, payload *gatewaypb.NodeEncryptedPayload, deviceIdentifier string) (Secret, error) {
	if len(privateKey) != x25519KeySize {
		return Secret{}, fmt.Errorf("recipient private key must be %d bytes, got %d", x25519KeySize, len(privateKey))
	}
	if payload == nil {
		return Secret{}, fmt.Errorf("encrypted password update is required")
	}
	if payload.GetAlgorithm() != Algorithm {
		return Secret{}, fmt.Errorf("unsupported encrypted payload algorithm %q", payload.GetAlgorithm())
	}
	if len(payload.GetEphemeralPubkey()) != x25519KeySize {
		return Secret{}, fmt.Errorf("ephemeral public key must be %d bytes, got %d", x25519KeySize, len(payload.GetEphemeralPubkey()))
	}
	if len(payload.GetNonce()) != nonceSize {
		return Secret{}, fmt.Errorf("nonce must be %d bytes, got %d", nonceSize, len(payload.GetNonce()))
	}
	recipient, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return Secret{}, fmt.Errorf("parse recipient private key: %w", err)
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(payload.GetEphemeralPubkey())
	if err != nil {
		return Secret{}, fmt.Errorf("parse ephemeral public key: %w", err)
	}
	shared, err := recipient.ECDH(ephemeral)
	if err != nil {
		return Secret{}, fmt.Errorf("derive shared secret: %w", err)
	}
	aead, err := aeadFromSharedSecret(shared)
	if err != nil {
		return Secret{}, err
	}
	plaintext, err := aead.Open(nil, payload.GetNonce(), payload.GetCiphertext(), associatedData(deviceIdentifier))
	if err != nil {
		return Secret{}, fmt.Errorf("decrypt password update secret: %w", err)
	}
	var secret Secret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return Secret{}, fmt.Errorf("unmarshal password update secret: %w", err)
	}
	if secret.DeviceIdentifier != deviceIdentifier {
		return Secret{}, fmt.Errorf("password update target %q does not match command target %q", secret.DeviceIdentifier, deviceIdentifier)
	}
	if secret.CurrentPassword == "" || secret.NewPassword == "" {
		return Secret{}, fmt.Errorf("password update secret requires current and new passwords")
	}
	return secret, nil
}

func aeadFromSharedSecret(shared []byte) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, hkdfSalt, hkdfInfo), key); err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create password update cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create password update AEAD: %w", err)
	}
	return aead, nil
}

func associatedData(deviceIdentifier string) []byte {
	return []byte(Algorithm + "/device/" + deviceIdentifier)
}

func recipientPublicKey(publicKey []byte) (*ecdh.PublicKey, error) {
	if len(publicKey) != x25519KeySize {
		return nil, fmt.Errorf("%w: must be %d bytes, got %d", ErrInvalidRecipientPublicKey, x25519KeySize, len(publicKey))
	}
	recipient, err := ecdh.X25519().NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: parse: %v", ErrInvalidRecipientPublicKey, err)
	}
	return recipient, nil
}
