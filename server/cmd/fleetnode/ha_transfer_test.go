package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHAKeyTransferPersistsForStartupWithoutReplacingLiveCodec(t *testing.T) {
	now := time.Now()
	recipient, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	key := bytes.Repeat([]byte{4}, 32)
	fingerprint := sha256.Sum256(key)
	sourceState := &bootstrap.State{FleetNodeID: 2, CredentialKeyHex: hex.EncodeToString(key)}
	source := &RunCmd{credentials: &credentialCodec{key: key}, haState: sourceState, haStatePath: bootstrap.StatePath(t.TempDir())}
	sourceAuthority := testHAAuthority(t, now, 2, 0, false)
	transfer := &pb.HAKeyTransfer{OperationId: "op", OrgId: 1, OwnerNodeId: 2, SourceNodeId: 2, RecipientNodeId: 3, RecipientPublicKey: recipient.PublicKey().Bytes(), CredentialKeyFingerprint: fingerprint[:], ExpiresAt: timestamppb.New(now.Add(time.Minute))}
	command := &pb.ControlCommand{IntendedInstanceId: "i", ControlSessionId: "s"}
	exported, err := source.executeHAKeyTransfer(command, transfer, sourceAuthority, now)
	require.NoError(t, err)
	require.NotContains(t, string(exported.GetEncryptedKey().GetCiphertext()), string(key))
	imported := proto.CloneOf(transfer)
	imported.EncryptedKey = exported.EncryptedKey
	state := &bootstrap.State{FleetNodeID: 3, IdentityPrivateKeyHex: "independent identity", EncryptionPublicKeyHex: hex.EncodeToString(recipient.PublicKey().Bytes()), EncryptionPrivateKeyHex: hex.EncodeToString(recipient.Bytes()), CredentialKeyHex: hex.EncodeToString(bytes.Repeat([]byte{8}, 32))}
	path := bootstrap.StatePath(t.TempDir())
	codec := &credentialCodec{key: bytes.Repeat([]byte{8}, 32)}
	node := &RunCmd{credentials: codec, haState: state, haStatePath: path}
	preparing := testHAAuthority(t, now, 3, 2, true)
	result, err := node.executeHAKeyTransfer(command, imported, preparing, now)
	require.NoError(t, err)
	require.Equal(t, fingerprint[:], result.GetCredentialKeyFingerprint())
	persisted, exists, err := bootstrap.LoadState(path)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, hex.EncodeToString(key), persisted.CredentialKeyHex)
	require.Equal(t, "independent identity", persisted.IdentityPrivateKeyHex)
	require.Equal(t, state.EncryptionPrivateKeyHex, persisted.EncryptionPrivateKeyHex)

	// Only restarted initialization may use the installed key; existing codecs stay immutable.
	encrypted, err := source.credentials.Seal(sdk.SecretBundle{Kind: sdk.UsernamePassword{Username: "u", Password: "p"}})
	require.NoError(t, err)
	_, err = codec.Open(encrypted)
	require.Error(t, err)
	_, err = node.executeHAKeyTransfer(command, imported, preparing, now)
	require.NoError(t, err)
	reloadedCodec, err := ensureCredentialCodec(path, persisted)
	require.NoError(t, err)
	_, err = reloadedCodec.Open(encrypted)
	require.NoError(t, err)
	restarted := &RunCmd{credentials: reloadedCodec, haState: persisted, haStatePath: path}
	_, err = restarted.executeHAKeyTransfer(command, imported, preparing, now)
	require.NoError(t, err)
	changedExpiry := proto.CloneOf(transfer)
	changedExpiry.ExpiresAt = timestamppb.New(now.Add(2 * time.Minute))
	changedExport, err := source.executeHAKeyTransfer(command, changedExpiry, sourceAuthority, now)
	require.NoError(t, err)
	changedExpiry.EncryptedKey = changedExport.GetEncryptedKey()
	_, err = node.executeHAKeyTransfer(command, changedExpiry, preparing, now)
	require.NoError(t, err, "retrying the same installed key does not need an operation journal")
	replacementKey := bytes.Repeat([]byte{5}, credentialKeySize)
	replacementFingerprint := sha256.Sum256(replacementKey)
	source.credentials = &credentialCodec{key: replacementKey}
	replacement := proto.CloneOf(transfer)
	replacement.OperationId = "replace-installed-key"
	replacement.CredentialKeyFingerprint = replacementFingerprint[:]
	replacementExport, err := source.executeHAKeyTransfer(command, replacement, sourceAuthority, now)
	require.NoError(t, err)
	replacement.EncryptedKey = replacementExport.GetEncryptedKey()
	_, err = node.executeHAKeyTransfer(command, replacement, preparing, now)
	require.ErrorContains(t, err, "already installed")
	changed := proto.CloneOf(imported)
	changed.SourceNodeId = 9
	_, err = node.executeHAKeyTransfer(command, changed, preparing, now)
	require.Error(t, err)
	node.runtimeStarted = true
	_, err = node.executeHAKeyTransfer(command, imported, preparing, now)
	require.ErrorContains(t, err, "startup preparation")
	node.runtimeStarted = false
	active := testHAAuthority(t, now, 3, 2, false)
	_, err = node.executeHAKeyTransfer(command, imported, active, now)
	require.Error(t, err)
}
func testHAAuthority(t *testing.T, now time.Time, nodeID, ownerID int64, preparing bool) *haAuthority {
	t.Helper()
	authority := newHAAuthority(t.Context(), nodeID, "i", "s")
	t.Cleanup(authority.close)
	request := authority.request(now)
	require.True(t, authority.apply(&pb.HALeaseResponse{RequestId: request.GetRequestId(), OwnerNodeId: ownerID, Preparing: preparing, InstanceId: "i", ControlSessionId: "s", OrgId: 1, LeaseDurationMs: 30000}, now))
	return authority
}
