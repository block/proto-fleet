package keytransfer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"testing"
	"time"

	"golang.org/x/crypto/hkdf"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTransferBindsEveryContextField(t *testing.T) {
	recipient, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	key := bytes.Repeat([]byte{7}, 32)
	fingerprint := sha256.Sum256(key)
	meta := Metadata{OperationID: "op", OrgID: 1, OwnerNodeID: 2, SourceNodeID: 2, RecipientNodeID: 3, RecipientPublicKey: recipient.PublicKey().Bytes(), Fingerprint: fingerprint[:], ExpiresAt: now.Add(time.Minute).UnixNano()}
	sealed, err := Encrypt(meta, key, now)
	require.NoError(t, err)
	opened, err := Decrypt(recipient.Bytes(), sealed, meta, now)
	require.NoError(t, err)
	require.Equal(t, key, opened)
	changes := map[string]func(*Metadata){
		"org": func(m *Metadata) { m.OrgID++ }, "owner": func(m *Metadata) { m.OwnerNodeID++ },
		"source": func(m *Metadata) { m.SourceNodeID++ }, "recipient": func(m *Metadata) { m.RecipientNodeID++ },
		"operation": func(m *Metadata) { m.OperationID = "other" }, "expiry": func(m *Metadata) { m.ExpiresAt++ },
		"pubkey":      func(m *Metadata) { m.RecipientPublicKey = bytes.Repeat([]byte{1}, 32) },
		"fingerprint": func(m *Metadata) { m.Fingerprint = bytes.Repeat([]byte{2}, 32) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			altered := meta
			change(&altered)
			_, err := Decrypt(recipient.Bytes(), sealed, altered, now)
			require.Error(t, err)
		})
	}
	_, err = Decrypt(recipient.Bytes(), sealed, meta, now.Add(2*time.Minute))
	require.Error(t, err)
	wrong, _ := ecdh.X25519().GenerateKey(rand.Reader)
	_, err = Decrypt(wrong.Bytes(), sealed, meta, now)
	require.Error(t, err)
	tampered := proto.CloneOf(sealed)
	tampered.GetCiphertext()[0] ^= 1
	_, err = Decrypt(recipient.Bytes(), tampered, meta, now)
	require.Error(t, err)
}

func TestTransferRejectsWrongHKDFContextAndFingerprint(t *testing.T) {
	recipient, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	shared, err := ephemeral.ECDH(recipient.PublicKey())
	require.NoError(t, err)
	key := bytes.Repeat([]byte{3}, 32)
	sum := sha256.Sum256(key)
	now := time.Now()
	metadata := Metadata{OperationID: "op", OrgID: 1, OwnerNodeID: 2, SourceNodeID: 2, RecipientNodeID: 3, RecipientPublicKey: recipient.PublicKey().Bytes(), Fingerprint: sum[:], ExpiresAt: now.Add(time.Minute).UnixNano()}
	wrongKey := make([]byte, 32)
	_, err = io.ReadFull(hkdf.New(sha256.New, shared, []byte("proto-fleet/fleet-node/password-update/salt/v1"), []byte("proto-fleet/fleet-node/password-update/aes-256-gcm/v1")), wrongKey)
	require.NoError(t, err)
	block, err := aes.NewCipher(wrongKey)
	require.NoError(t, err)
	wrongAEAD, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, 12)
	payload := &pb.NodeEncryptedPayload{Algorithm: Algorithm, EphemeralPubkey: ephemeral.PublicKey().Bytes(), Nonce: nonce, Ciphertext: wrongAEAD.Seal(nil, nonce, key, metadata.aad())}
	_, err = Decrypt(recipient.Bytes(), payload, metadata, now)
	require.Error(t, err)
	// Even validly authenticated ciphertext cannot claim a different key digest.
	correctAEAD, err := derive(shared)
	require.NoError(t, err)
	payload.Ciphertext = correctAEAD.Seal(nil, nonce, bytes.Repeat([]byte{9}, 32), metadata.aad())
	_, err = Decrypt(recipient.Bytes(), payload, metadata, now)
	require.ErrorContains(t, err, "fingerprint mismatch")
	_, err = Encrypt(metadata, bytes.Repeat([]byte{9}, 32), now)
	require.ErrorContains(t, err, "fingerprint mismatch")
}
