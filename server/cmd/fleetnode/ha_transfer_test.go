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

func TestHAKeyTransferPersistsAndReloadsSharedCodec(t *testing.T) {
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
	require.Equal(t, "op", persisted.HAKeyTransferOperationID)
	// A caller retaining the original codec pointer immediately uses the shared key.
	encrypted, err := source.credentials.Seal(sdk.SecretBundle{Kind: sdk.UsernamePassword{Username: "u", Password: "p"}})
	require.NoError(t, err)
	_, err = codec.Open(encrypted)
	require.NoError(t, err)
	_, err = node.executeHAKeyTransfer(command, imported, preparing, now)
	require.NoError(t, err)
	reloadedCodec, err := ensureCredentialCodec(path, persisted)
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
	require.ErrorContains(t, err, "different metadata")
	changed := proto.CloneOf(imported)
	changed.SourceNodeId = 9
	_, err = node.executeHAKeyTransfer(command, changed, preparing, now)
	require.Error(t, err)
	for _, busy := range []string{"ordinary", "probe", "pair", "handle"} {
		t.Run(busy, func(t *testing.T) {
			node.initControlConcurrency()
			switch busy {
			case "ordinary":
				node.controlCommandSlots <- struct{}{}
				defer func() { <-node.controlCommandSlots }()
			case "probe":
				node.haDeviceWork.Add(1)
				defer node.haDeviceWork.Add(-1)
			case "pair":
				node.pairMu.Lock()
				defer node.pairMu.Unlock()
			case "handle":
				node.deviceHandles = newDeviceHandlePool(t.Context(), 1)
				defer node.deviceHandles.shutdown()
				node.deviceHandles.slots <- struct{}{}
				defer func() { node.deviceHandles = nil }()
			}
			_, err := node.executeHAKeyTransfer(command, imported, preparing, now)
			require.Error(t, err)
		})
	}
	active := testHAAuthority(t, now, 3, 2, false)
	_, err = node.executeHAKeyTransfer(command, imported, active, now)
	require.Error(t, err)
}
func testHAAuthority(t *testing.T, now time.Time, nodeID, ownerID int64, preparing bool) *haAuthority {
	t.Helper()
	authority := newHAAuthority(nodeID, "i", "s")
	t.Cleanup(authority.close)
	request := authority.request(now)
	require.True(t, authority.apply(&pb.HALeaseResponse{RequestId: request.GetRequestId(), OwnerNodeId: ownerID, Preparing: preparing, InstanceId: "i", ControlSessionId: "s", OrgId: 1, LeaseDurationMs: 30000}, now))
	return authority
}
