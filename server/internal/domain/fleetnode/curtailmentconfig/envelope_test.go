package curtailmentconfig

import (
	"crypto/ecdh"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	config := sdk.CurtailmentConfig{
		Enabled:         true,
		FailPolicy:      "closed",
		RestorePolicy:   "respect_manual_stop",
		NATSURL:         "nats://localhost:4222",
		MCDDGRPCAddress: "127.0.0.1:2122",
		Providers: []sdk.CurtailmentProviderConfig{{
			Name:     "maestro",
			Type:     "maestro_mqtt",
			Enabled:  true,
			Brokers:  []string{"10.0.0.1", "10.0.0.2"},
			Port:     1883,
			Username: "operator",
			Password: "secret",
			Topic:    "maestro/target",
		}},
	}

	payload, err := Encrypt(privateKey.PublicKey().Bytes(), "proto-rig-1", config)
	require.NoError(t, err)
	assert.NotContains(t, string(payload.Ciphertext), "secret")

	decrypted, err := Decrypt(privateKey.Bytes(), payload, "proto-rig-1")
	require.NoError(t, err)
	assert.Equal(t, config, decrypted)
}

func TestDecryptRejectsDifferentDevice(t *testing.T) {
	t.Parallel()

	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	payload, err := Encrypt(privateKey.PublicKey().Bytes(), "proto-rig-1", sdk.CurtailmentConfig{})
	require.NoError(t, err)

	_, err = Decrypt(privateKey.Bytes(), payload, "proto-rig-2")
	require.Error(t, err)
}

func TestDecryptRejectsOversizedCiphertext(t *testing.T) {
	t.Parallel()

	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	payload, err := Encrypt(privateKey.PublicKey().Bytes(), "proto-rig-1", sdk.CurtailmentConfig{})
	require.NoError(t, err)
	payload.Ciphertext = make([]byte, maxCiphertext+1)

	_, err = Decrypt(privateKey.Bytes(), payload, "proto-rig-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}

func TestValidateConfigSizeRejectsOversizedConfig(t *testing.T) {
	t.Parallel()

	config := sdk.CurtailmentConfig{Providers: []sdk.CurtailmentProviderConfig{{
		Name:     "maestro",
		Password: strings.Repeat("p", maxCiphertext),
	}}}

	err := ValidateConfigSize(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}
