package curtailmentconfig

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
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	Algorithm = "x25519-hkdf-sha256-aes-256-gcm-v1"

	x25519KeySize   = 32
	nonceSize       = 12
	maxCiphertext   = 8192
	envelopeVersion = 1
)

var (
	// ErrInvalidRecipientPublicKey identifies a malformed FleetNode key.
	ErrInvalidRecipientPublicKey = errors.New("invalid recipient public key")

	hkdfSalt = []byte("proto-fleet/fleet-node/curtailment-config/salt/v1")
	hkdfInfo = []byte("proto-fleet/fleet-node/curtailment-config/aes-256-gcm/v1")
)

// Secret binds a complete config to one device so an encrypted command cannot
// be replayed against another rig.
type Secret struct {
	Version          int                   `json:"version"`
	DeviceIdentifier string                `json:"device_identifier"`
	Config           sdk.CurtailmentConfig `json:"config"`
}

func Encrypt(publicKey []byte, deviceIdentifier string, config sdk.CurtailmentConfig) (*gatewaypb.NodeEncryptedPayload, error) {
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
	plaintext, err := json.Marshal(Secret{
		Version:          envelopeVersion,
		DeviceIdentifier: deviceIdentifier,
		Config:           config,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal curtailment config secret: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, associatedData(deviceIdentifier))
	if len(ciphertext) > maxCiphertext {
		return nil, fmt.Errorf("encrypted curtailment config is %d bytes; maximum is %d", len(ciphertext), maxCiphertext)
	}
	return &gatewaypb.NodeEncryptedPayload{
		Algorithm:       Algorithm,
		EphemeralPubkey: append([]byte(nil), ephemeral.PublicKey().Bytes()...),
		Nonce:           nonce,
		Ciphertext:      ciphertext,
	}, nil
}

func Decrypt(privateKey []byte, payload *gatewaypb.NodeEncryptedPayload, deviceIdentifier string) (sdk.CurtailmentConfig, error) {
	if len(privateKey) != x25519KeySize {
		return sdk.CurtailmentConfig{}, fmt.Errorf("recipient private key must be %d bytes, got %d", x25519KeySize, len(privateKey))
	}
	if payload == nil {
		return sdk.CurtailmentConfig{}, errors.New("encrypted curtailment config is required")
	}
	if payload.GetAlgorithm() != Algorithm {
		return sdk.CurtailmentConfig{}, fmt.Errorf("unsupported encrypted payload algorithm %q", payload.GetAlgorithm())
	}
	if len(payload.GetEphemeralPubkey()) != x25519KeySize {
		return sdk.CurtailmentConfig{}, fmt.Errorf("ephemeral public key must be %d bytes, got %d", x25519KeySize, len(payload.GetEphemeralPubkey()))
	}
	if len(payload.GetNonce()) != nonceSize {
		return sdk.CurtailmentConfig{}, fmt.Errorf("nonce must be %d bytes, got %d", nonceSize, len(payload.GetNonce()))
	}
	recipient, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return sdk.CurtailmentConfig{}, fmt.Errorf("parse recipient private key: %w", err)
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(payload.GetEphemeralPubkey())
	if err != nil {
		return sdk.CurtailmentConfig{}, fmt.Errorf("parse ephemeral public key: %w", err)
	}
	shared, err := recipient.ECDH(ephemeral)
	if err != nil {
		return sdk.CurtailmentConfig{}, fmt.Errorf("derive shared secret: %w", err)
	}
	aead, err := aeadFromSharedSecret(shared)
	if err != nil {
		return sdk.CurtailmentConfig{}, err
	}
	plaintext, err := aead.Open(nil, payload.GetNonce(), payload.GetCiphertext(), associatedData(deviceIdentifier))
	if err != nil {
		return sdk.CurtailmentConfig{}, fmt.Errorf("decrypt curtailment config secret: %w", err)
	}
	var secret Secret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return sdk.CurtailmentConfig{}, fmt.Errorf("unmarshal curtailment config secret: %w", err)
	}
	if secret.Version != envelopeVersion {
		return sdk.CurtailmentConfig{}, fmt.Errorf("unsupported curtailment config envelope version %d", secret.Version)
	}
	if secret.DeviceIdentifier != deviceIdentifier {
		return sdk.CurtailmentConfig{}, fmt.Errorf("curtailment config target %q does not match command target %q", secret.DeviceIdentifier, deviceIdentifier)
	}
	return secret.Config, nil
}

func aeadFromSharedSecret(shared []byte) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, hkdfSalt, hkdfInfo), key); err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create curtailment config cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create curtailment config AEAD: %w", err)
	}
	return aead, nil
}

func associatedData(deviceIdentifier string) []byte {
	return []byte(Algorithm + "/curtailment-config/device/" + deviceIdentifier)
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
