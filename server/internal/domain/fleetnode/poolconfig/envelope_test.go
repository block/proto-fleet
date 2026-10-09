package poolconfig

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/curtailmentconfig"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func testConfig() *gatewaypb.MiningPoolWriteConfiguration {
	return &gatewaypb.MiningPoolWriteConfiguration{Pools: []*gatewaypb.MiningPoolWriteConfig{
		{Priority: 0, Url: "stratum+tcp://primary.example.com:3333", Username: "customer.worker", Password: proto.String("primary-credential")},
		{Priority: 1, Url: "stratum+tcp://backup.example.com:3333", Username: "customer.backup", Password: proto.String("backup-credential")},
		{Priority: 2, Url: "stratum+tcp://empty.example.com:3333", Username: "customer.empty", Password: proto.String("")},
	}}
}

func TestEncryptDecryptPreservesExactConfiguration(t *testing.T) {
	t.Parallel()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	config := testConfig()
	encrypted, err := Encrypt(key.PublicKey().Bytes(), "dev-1", config)
	require.NoError(t, err)
	wire, err := proto.Marshal(encrypted)
	require.NoError(t, err)
	for _, secret := range []string{"primary-credential", "backup-credential", "customer.worker"} {
		assert.NotContains(t, string(wire), secret)
	}
	got, err := Decrypt(key.Bytes(), encrypted, "dev-1")
	require.NoError(t, err)
	assert.True(t, proto.Equal(config, got))
	require.NotNil(t, got.Pools[2].Password)
	assert.Empty(t, *got.Pools[2].Password)
}

func TestDecryptRejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	valid, err := Encrypt(key.PublicKey().Bytes(), "dev-1", testConfig())
	require.NoError(t, err)
	tampered := proto.CloneOf(valid)
	tampered.Ciphertext[0] ^= 1
	oversized := proto.CloneOf(valid)
	oversized.Ciphertext = make([]byte, maxCiphertext+1)
	wrongAlgorithm := proto.CloneOf(valid)
	wrongAlgorithm.Algorithm = "unknown"
	for _, tc := range []struct {
		name    string
		key     []byte
		payload *gatewaypb.NodeEncryptedPayload
		device  string
	}{
		{"missing key", nil, valid, "dev-1"},
		{"wrong key", otherKey.Bytes(), valid, "dev-1"},
		{"wrong target", key.Bytes(), valid, "dev-2"},
		{"missing envelope", key.Bytes(), nil, "dev-1"},
		{"tampered", key.Bytes(), tampered, "dev-1"},
		{"oversized", key.Bytes(), oversized, "dev-1"},
		{"algorithm", key.Bytes(), wrongAlgorithm, "dev-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decrypt(tc.key, tc.payload, tc.device)
			require.Error(t, err)
		})
	}
	// Other sealed command domains cannot be interpreted as pool writes.
	otherDomain, err := curtailmentconfig.Encrypt(key.PublicKey().Bytes(), "dev-1", sdk.CurtailmentConfig{})
	require.NoError(t, err)
	_, err = Decrypt(key.Bytes(), otherDomain, "dev-1")
	require.Error(t, err)
}

func TestEncryptRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*gatewaypb.MiningPoolWriteConfiguration)
	}{
		{"unknown password", func(c *gatewaypb.MiningPoolWriteConfiguration) { c.Pools[0].Password = nil }},
		{"invalid URL", func(c *gatewaypb.MiningPoolWriteConfiguration) { c.Pools[0].Url = "bad-url" }},
		{"priority out of range", func(c *gatewaypb.MiningPoolWriteConfiguration) { c.Pools[0].Priority = 3 }},
		{"duplicate priority", func(c *gatewaypb.MiningPoolWriteConfiguration) { c.Pools[1].Priority = 0 }},
		{"no pools", func(c *gatewaypb.MiningPoolWriteConfiguration) { c.Pools = nil }},
		{"too many pools", func(c *gatewaypb.MiningPoolWriteConfiguration) {
			c.Pools = append(c.Pools, proto.CloneOf(c.Pools[0]))
		}},
		{"oversized", func(c *gatewaypb.MiningPoolWriteConfiguration) {
			c.Pools[0].Password = proto.String(strings.Repeat("p", maxCiphertext))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := testConfig()
			tc.change(c)
			_, err := Encrypt(key.PublicKey().Bytes(), "dev-1", c)
			require.Error(t, err)
		})
	}
}

// Make an authenticated envelope without going through Encrypt's validation so
// decryption must independently validate the inner payload before device use.
func rawEnvelope(t *testing.T, key *ecdh.PrivateKey, secret Secret) *gatewaypb.NodeEncryptedPayload {
	t.Helper()
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	shared, err := ephemeral.ECDH(key.PublicKey())
	require.NoError(t, err)
	aead, err := aeadFromSharedSecret(shared)
	require.NoError(t, err)
	encoded, err := json.Marshal(secret)
	require.NoError(t, err)
	nonce := make([]byte, nonceSize)
	_, err = rand.Read(nonce)
	require.NoError(t, err)
	return &gatewaypb.NodeEncryptedPayload{Algorithm: Algorithm, EphemeralPubkey: ephemeral.PublicKey().Bytes(), Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, encoded, associatedData("dev-1"))}
}

func TestDecryptValidatesInnerConfiguration(t *testing.T) {
	t.Parallel()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*Secret)
	}{
		{"wrong version", func(s *Secret) { s.Version++ }},
		{"wrong inner target", func(s *Secret) { s.DeviceIdentifier = "dev-2" }},
		{"malformed config", func(s *Secret) { s.Config = []byte{0xff} }},
		{"absent password", func(s *Secret) {
			c := testConfig()
			c.Pools[0].Password = nil
			encoded, err := proto.Marshal(c)
			require.NoError(t, err)
			s.Config = encoded
		}},
		{"empty config", func(s *Secret) { s.Config = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := proto.Marshal(testConfig())
			require.NoError(t, err)
			secret := Secret{Version: envelopeVersion, DeviceIdentifier: "dev-1", Config: encoded}
			tc.change(&secret)
			_, err = Decrypt(key.Bytes(), rawEnvelope(t, key, secret), "dev-1")
			require.Error(t, err)
		})
	}
}
