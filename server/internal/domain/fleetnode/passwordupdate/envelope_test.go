package passwordupdate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := GenerateKeypair()
	require.NoError(t, err)

	payload, err := Encrypt(publicKey, Secret{
		DeviceIdentifier: "miner-1",
		CurrentPassword:  "old-pass",
		NewPassword:      "new-pass",
	})
	require.NoError(t, err)

	secret, err := Decrypt(privateKey, payload, "miner-1")
	require.NoError(t, err)
	assert.Equal(t, Secret{
		DeviceIdentifier: "miner-1",
		CurrentPassword:  "old-pass",
		NewPassword:      "new-pass",
	}, secret)
}

func TestDecryptRejectsWrongDeviceIdentifier(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := GenerateKeypair()
	require.NoError(t, err)

	payload, err := Encrypt(publicKey, Secret{
		DeviceIdentifier: "miner-1",
		CurrentPassword:  "old-pass",
		NewPassword:      "new-pass",
	})
	require.NoError(t, err)

	_, err = Decrypt(privateKey, payload, "miner-2")
	require.Error(t, err)
}

func TestValidateRecipientPublicKeyRejectsLowOrderKey(t *testing.T) {
	t.Parallel()

	err := ValidateRecipientPublicKey(make([]byte, 32))
	require.Error(t, err)
}
