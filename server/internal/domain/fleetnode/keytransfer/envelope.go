// Package keytransfer seals a Fleet Node credential key to an independently
// enrolled recipient. All provisioning context is authenticated; cloud forwards
// this envelope without learning the shared credential key.
package keytransfer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"golang.org/x/crypto/hkdf"
)

const Algorithm = "x25519-hkdf-sha256-aes-256-gcm-v1"
const context = "proto-fleet/fleet-node/ha-credential-key/v1"

type Metadata struct {
	OperationID        string `json:"operation_id"`
	OrgID              int64  `json:"org_id"`
	OwnerNodeID        int64  `json:"owner_node_id"`
	SourceNodeID       int64  `json:"source_node_id"`
	RecipientNodeID    int64  `json:"recipient_node_id"`
	RecipientPublicKey []byte `json:"recipient_public_key"`
	Fingerprint        []byte `json:"credential_key_fingerprint"`
	ExpiresAt          int64  `json:"expires_at_unix_nano"`
}

func (m Metadata) Validate(now time.Time) error {
	if m.OperationID == "" || len(m.OperationID) > 128 || m.OrgID <= 0 || m.OwnerNodeID <= 0 || m.SourceNodeID <= 0 || m.RecipientNodeID <= 0 || m.SourceNodeID == m.RecipientNodeID || len(m.RecipientPublicKey) != 32 || len(m.Fingerprint) != 32 || m.ExpiresAt <= now.UnixNano() {
		return fmt.Errorf("invalid or expired key transfer context")
	}
	return nil
}

func Encrypt(m Metadata, key []byte, now time.Time) (*pb.NodeEncryptedPayload, error) {
	if err := m.Validate(now); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(key)
	if len(key) != 32 || !bytes.Equal(sum[:], m.Fingerprint) {
		return nil, fmt.Errorf("credential key fingerprint mismatch")
	}
	recipient, err := ecdh.X25519().NewPublicKey(m.RecipientPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse recipient key: %w", err)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate transfer key: %w", err)
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return nil, fmt.Errorf("derive transfer secret: %w", err)
	}
	aead, err := derive(shared)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate transfer nonce: %w", err)
	}
	return &pb.NodeEncryptedPayload{Algorithm: Algorithm, EphemeralPubkey: ephemeral.PublicKey().Bytes(), Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, key, m.aad())}, nil
}

func Decrypt(privateKey []byte, payload *pb.NodeEncryptedPayload, m Metadata, now time.Time) ([]byte, error) {
	if err := m.Validate(now); err != nil {
		return nil, err
	}
	recipient, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("parse recipient private key: %w", err)
	}
	if !bytes.Equal(recipient.PublicKey().Bytes(), m.RecipientPublicKey) {
		return nil, fmt.Errorf("recipient public key mismatch")
	}
	if payload == nil || payload.GetAlgorithm() != Algorithm || len(payload.GetNonce()) != 12 || len(payload.GetCiphertext()) != 48 {
		return nil, fmt.Errorf("invalid encrypted key transfer")
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(payload.GetEphemeralPubkey())
	if err != nil {
		return nil, fmt.Errorf("parse ephemeral key: %w", err)
	}
	shared, err := recipient.ECDH(ephemeral)
	if err != nil {
		return nil, fmt.Errorf("derive transfer secret: %w", err)
	}
	aead, err := derive(shared)
	if err != nil {
		return nil, err
	}
	key, err := aead.Open(nil, payload.GetNonce(), payload.GetCiphertext(), m.aad())
	if err != nil {
		return nil, fmt.Errorf("authenticate transferred key: %w", err)
	}
	sum := sha256.Sum256(key)
	if len(key) != 32 || !bytes.Equal(sum[:], m.Fingerprint) {
		return nil, fmt.Errorf("credential key fingerprint mismatch")
	}
	return key, nil
}

func (m Metadata) aad() []byte {
	serialized := m.serialized()
	return append([]byte(context+"/"), serialized...)
}
func derive(shared []byte) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, []byte(context+"/salt"), []byte(context+"/aes-256-gcm")), key); err != nil {
		return nil, fmt.Errorf("derive transfer key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create transfer cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create transfer AEAD: %w", err)
	}
	return aead, nil
}

// Metadata contains only JSON primitives. Fail closed if future fields break
// that invariant rather than authenticating an empty context.
func (m Metadata) serialized() []byte {
	serialized, err := json.Marshal(m)
	if err != nil {
		panic("key transfer metadata cannot be serialized")
	}
	return serialized
}
