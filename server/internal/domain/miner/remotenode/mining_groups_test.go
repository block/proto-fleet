package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/poolconfig"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestMiningGroupsRequiresV6AndEncryptsEveryPoolPassword(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(`{"group":{"id":"group-1","revision":10}}`)}}
	miner := newTestMiner(t, sender)
	r := sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "group-1", Revision: 9, Key: "stable-key", Pools: []sdk.MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "worker", Password: "private-pool-password"}, {URL: "stratum+tcp://backup.example:3333", Username: "worker", Password: ""}}}
	_, err := miner.MiningGroups(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6, sender.minimumCommandProtocolVersion)
	require.NotContains(t, string(sender.cmd.Payload), "private-pool-password")
	var command gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &command))
	action := command.GetMinerCommand().GetMiningGroups()
	require.NotNil(t, action)
	decoded, err := sdk.DecodeMiningGroupRequest(action.Request)
	require.NoError(t, err)
	require.Equal(t, r.Key, decoded.Key)
	require.Empty(t, decoded.Pools[0].Password)
	_, private := testPoolConfigKeys(t)
	config, err := poolconfig.Decrypt(private, action.EncryptedPools, "dev-1")
	require.NoError(t, err)
	require.Equal(t, r.Pools[0].Password, config.Pools[0].GetPassword())
	require.NotNil(t, config.Pools[1].Password)
	require.Empty(t, *config.Pools[1].Password)
	_, err = poolconfig.Decrypt(private, action.EncryptedPools, "another-controller")
	require.Error(t, err)
	require.Equal(t, "private-pool-password", r.Pools[0].Password, "redaction cannot mutate the caller's immutable replay request")

	r.PoolIDs = []int32{42, 0}
	r.PoolNames = []string{"Pool 2", "Pool 2 (2)"}
	_, err = miner.MiningGroups(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V18, sender.minimumCommandProtocolVersion)
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &command))
	decoded, err = sdk.DecodeMiningGroupRequest(command.GetMinerCommand().GetMiningGroups().Request)
	require.NoError(t, err)
	require.Equal(t, r.PoolIDs, decoded.PoolIDs)
	require.Equal(t, r.PoolNames, decoded.PoolNames)
	require.Empty(t, decoded.Pools[0].Password)
}

func TestMiningGroupsRejectsInvalidNodeResultsWithoutClaimingNoWrite(t *testing.T) {
	for _, payload := range []string{`null`, `{"unexpected":true}`, `{"revision_conflict":true,"group":{"id":"g","revision":2}}`, `{"group":{"id":"other","revision":2}}`, `{"group":{"id":"g","revision":0}}`} {
		t.Run(payload, func(t *testing.T) {
			sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(payload)}}
			miner := newTestMiner(t, sender)
			result, err := miner.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "start", Key: "k", GroupID: "g", Revision: 1})
			require.Error(t, err)
			require.False(t, devicewrite.IsNotDispatched(err))
			require.False(t, result.RevisionConflict)
		})
	}
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(`{"supported":true,"revision":0}`)}}
	_, err := newTestMiner(t, sender).MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "observe"})
	require.Error(t, err)
}

func TestMiningGroupCreationRequiresV18BeforeDispatchForNonemptyPools(t *testing.T) {
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "old-node", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V17)
	require.NoError(t, err)
	defer stream.Unregister()
	request := sdk.MiningGroupRequest{Action: "create", Key: "create", ConfigID: "nonempty-config"}
	_, err = newTestMiner(t, registry).MiningGroups(t.Context(), request)
	require.ErrorIs(t, err, control.ErrCommandProtocolUpgradeRequired)
	require.True(t, devicewrite.IsNotDispatched(err))

	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(`{"group":{"id":"group-1","revision":1}}`)}}
	miner := newTestMiner(t, sender)
	_, err = miner.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V18, sender.minimumCommandProtocolVersion)
	request.ConfigID = poolconfigurations.ClearedPoolIntent(1)
	_, err = miner.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6, sender.minimumCommandProtocolVersion)
}

func TestMiningGroupDeactivationCarriesExactRequestThroughNode(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(`{"deactivated":true}`)}}
	request := sdk.MiningGroupRequest{Action: "deactivate", Key: "stable-deactivation", Revision: 9}
	result, err := newTestMiner(t, sender).MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.True(t, result.Deactivated)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15, sender.minimumCommandProtocolVersion)
	var command gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &command))
	action := command.GetMinerCommand().GetMiningGroups()
	decoded, err := sdk.DecodeMiningGroupRequest(action.Request)
	require.NoError(t, err)
	require.Equal(t, request, decoded)
	require.Nil(t, action.EncryptedPools)
}

func TestDefaultMiningOperationsRejectOlderNodesBeforeDispatch(t *testing.T) {
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "old-node", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V17)
	require.NoError(t, err)
	defer stream.Unregister()
	miner := newTestMiner(t, registry)
	for _, request := range []sdk.MiningGroupRequest{
		{Action: sdk.MiningGroupReadDefault},
		{Action: "stop_legacy", Key: "stop"},
		{Action: "activate", Key: "activate", Revision: 1},
		{Action: "start", Key: "start", GroupID: "default", Revision: 1},
	} {
		_, err := miner.MiningGroups(t.Context(), request)
		require.ErrorIs(t, err, control.ErrCommandProtocolUpgradeRequired)
		require.True(t, devicewrite.IsNotDispatched(err))
	}
}

func TestMiningGroupDeactivationRejectsOlderNodeBeforeDispatch(t *testing.T) {
	for _, version := range []gatewaypb.CommandProtocolVersion{gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V9, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V14} {
		t.Run(version.String(), func(t *testing.T) {
			registry := control.NewRegistry()
			stream, err := registry.RegisterAuthenticated(7, "old-node", version)
			require.NoError(t, err)
			defer stream.Unregister()
			miner := newTestMiner(t, registry)
			require.ErrorContains(t, miner.CheckMiningGroupDeactivation(t.Context()), "Upgrade the Fleet Node")
			_, err = miner.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "deactivate", Key: "deactivate", Revision: 1})
			require.Error(t, err)
			require.True(t, devicewrite.IsNotDispatched(err))
		})
	}
}

func TestMiningGroupDeactivationRequiresLiveCompatibleNode(t *testing.T) {
	registry := control.NewRegistry()
	miner := newTestMiner(t, registry)
	require.ErrorContains(t, miner.CheckMiningGroupDeactivation(t.Context()), "reconnect")
	stream, err := registry.RegisterAuthenticated(7, "new-node", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15)
	require.NoError(t, err)
	require.NoError(t, miner.CheckMiningGroupDeactivation(t.Context()))
	stream.Unregister()
	require.ErrorContains(t, miner.CheckMiningGroupDeactivation(t.Context()), "reconnect")
}
