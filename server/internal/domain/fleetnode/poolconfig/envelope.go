package poolconfig

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

	"buf.build/go/protovalidate"
	"golang.org/x/crypto/hkdf"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const (
	Algorithm = "x25519-hkdf-sha256-aes-256-gcm-v1"

	x25519KeySize   = 32
	nonceSize       = 12
	maxCiphertext   = 8192
	maxDeviceIDSize = 256
	envelopeVersion = 1
)

var (
	// ErrInvalidRecipientPublicKey identifies a malformed FleetNode key.
	ErrInvalidRecipientPublicKey = errors.New("invalid recipient public key")

	hkdfSalt = []byte("proto-fleet/fleet-node/pool-config/salt/v1")
	hkdfInfo = []byte("proto-fleet/fleet-node/pool-config/aes-256-gcm/v1")
)

// Secret binds a complete config to one device so an encrypted command cannot
// be replayed against another rig.
type Secret struct {
	Version          int    `json:"version"`
	DeviceIdentifier string `json:"device_identifier"`
	Config           []byte `json:"config"`
}

func Encrypt(publicKey []byte, deviceIdentifier string, config *gatewaypb.MiningPoolWriteConfiguration) (*gatewaypb.NodeEncryptedPayload, error) {
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
	plaintext, err := marshalSecret(deviceIdentifier, config)
	if err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, associatedData(deviceIdentifier))
	if len(ciphertext) > maxCiphertext {
		return nil, fmt.Errorf("encrypted pool config is %d bytes; maximum is %d", len(ciphertext), maxCiphertext)
	}
	return &gatewaypb.NodeEncryptedPayload{
		Algorithm:       Algorithm,
		EphemeralPubkey: append([]byte(nil), ephemeral.PublicKey().Bytes()...),
		Nonce:           nonce,
		Ciphertext:      ciphertext,
	}, nil
}

func Decrypt(privateKey []byte, payload *gatewaypb.NodeEncryptedPayload, deviceIdentifier string) (*gatewaypb.MiningPoolWriteConfiguration, error) {
	if len(privateKey) != x25519KeySize {
		return nil, fmt.Errorf("recipient private key must be %d bytes, got %d", x25519KeySize, len(privateKey))
	}
	if payload == nil {
		return nil, errors.New("encrypted pool config is required")
	}
	if payload.GetAlgorithm() != Algorithm {
		return nil, fmt.Errorf("unsupported encrypted payload algorithm %q", payload.GetAlgorithm())
	}
	if len(payload.GetEphemeralPubkey()) != x25519KeySize {
		return nil, fmt.Errorf("ephemeral public key must be %d bytes, got %d", x25519KeySize, len(payload.GetEphemeralPubkey()))
	}
	if len(payload.GetNonce()) != nonceSize {
		return nil, fmt.Errorf("nonce must be %d bytes, got %d", nonceSize, len(payload.GetNonce()))
	}
	if len(payload.GetCiphertext()) > maxCiphertext {
		return nil, fmt.Errorf("encrypted pool config is %d bytes; maximum is %d", len(payload.GetCiphertext()), maxCiphertext)
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
	plaintext, err := aead.Open(nil, payload.GetNonce(), payload.GetCiphertext(), associatedData(deviceIdentifier))
	if err != nil {
		return nil, fmt.Errorf("decrypt pool config secret: %w", err)
	}
	var secret Secret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return nil, fmt.Errorf("unmarshal pool config secret: %w", err)
	}
	if secret.Version != envelopeVersion {
		return nil, fmt.Errorf("unsupported pool config envelope version %d", secret.Version)
	}
	if secret.DeviceIdentifier != deviceIdentifier {
		return nil, fmt.Errorf("pool config target %q does not match command target %q", secret.DeviceIdentifier, deviceIdentifier)
	}
	config := new(gatewaypb.MiningPoolWriteConfiguration)
	if err := proto.Unmarshal(secret.Config, config); err != nil {
		return nil, errors.New("invalid pool config encoding")
	}
	if err := validateConfig(deviceIdentifier, config); err != nil {
		return nil, err
	}
	return config, nil
}

func marshalSecret(deviceIdentifier string, config *gatewaypb.MiningPoolWriteConfiguration) ([]byte, error) {
	if err := validateConfig(deviceIdentifier, config); err != nil {
		return nil, err
	}
	encoded, err := proto.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal pool config: %w", err)
	}
	plaintext, err := json.Marshal(Secret{Version: envelopeVersion, DeviceIdentifier: deviceIdentifier, Config: encoded})
	if err != nil {
		return nil, fmt.Errorf("marshal pool config envelope: %w", err)
	}
	return plaintext, nil
}

func validateConfig(deviceIdentifier string, config *gatewaypb.MiningPoolWriteConfiguration) error {
	if len(deviceIdentifier) == 0 || len(deviceIdentifier) > maxDeviceIDSize {
		return errors.New("invalid pool config device identifier")
	}
	if config == nil {
		return errors.New("pool config is required")
	}
	if err := protovalidate.Validate(config); err != nil {
		return errors.New("invalid pool config")
	}
	priorities := make(map[int32]bool)
	for _, pool := range config.GetPools() {
		if pool == nil || pool.Password == nil {
			return errors.New("explicit pool password is required")
		}
		if priorities[pool.GetPriority()] {
			return errors.New("duplicate pool priority")
		}
		priorities[pool.GetPriority()] = true
	}
	return nil
}

func aeadFromSharedSecret(shared []byte) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, hkdfSalt, hkdfInfo), key); err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create pool config cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create pool config AEAD: %w", err)
	}
	return aead, nil
}

func associatedData(deviceIdentifier string) []byte {
	return []byte(Algorithm + "/pool-config/device/" + deviceIdentifier)
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
