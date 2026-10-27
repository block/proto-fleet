package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/poolconfig"
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
