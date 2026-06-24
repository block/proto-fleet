package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestEnsureCredentialCodecGeneratesAndPersistsKey(t *testing.T) {
	// Arrange
	path := bootstrap.StatePath(t.TempDir())
	st := &bootstrap.State{FleetNodeID: 7}

	// Act
	codec, err := ensureCredentialCodec(path, st)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, codec)
	assert.Len(t, st.CredentialKeyHex, credentialKeySize*2)
	loaded, exists, err := bootstrap.LoadState(path)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, st.CredentialKeyHex, loaded.CredentialKeyHex)
}

func TestCredentialCodecRoundTripUsernamePassword(t *testing.T) {
	// Arrange
	st := &bootstrap.State{}
	key, _, err := loadOrGenerateCredentialKey(st)
	require.NoError(t, err)
	codec := &credentialCodec{key: key}

	// Act
	encrypted, err := codec.Seal(sdk.SecretBundle{
		Version: "v1",
		Kind:    sdk.UsernamePassword{Username: "root", Password: "hunter2"},
	})
	require.NoError(t, err)
	bundle, err := codec.Open(encrypted)

	// Assert
	require.NoError(t, err)
	assert.LessOrEqual(t, len(encrypted.GetUsername()), maxCredentialBlobBytes)
	assert.LessOrEqual(t, len(encrypted.GetPassword()), maxCredentialBlobBytes)
	assert.Equal(t, credentialBlobVersion, encrypted.GetUsername()[0])
	assert.Equal(t, credentialBlobMagic, string(encrypted.GetUsername()[1:1+len(credentialBlobMagic)]))
	assert.Equal(t, sdk.UsernamePassword{Username: "root", Password: "hunter2"}, bundle.Kind)
}
