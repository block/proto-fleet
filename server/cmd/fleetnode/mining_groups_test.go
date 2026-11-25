package main

import (
	"context"
	"encoding/json"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/passwordupdate"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/poolconfig"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestMiningGroupsNodeRejectsPlaintextAndMismatchedCredentials(t *testing.T) {
	public, private, err := passwordupdate.GenerateKeypair()
	require.NoError(t, err)
	encrypted, err := poolconfig.Encrypt(public, "ct1", &pb.MiningPoolWriteConfiguration{Pools: []*pb.MiningPoolWriteConfig{{Url: "stratum+tcp://pool.example:3333", Username: "worker", Password: proto.String("private")}}})
	require.NoError(t, err)
	request := sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "g", Key: "key", Revision: 1, PoolIDs: []int32{42}, PoolNames: []string{"Pool 2"}, Pools: []sdk.MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}}
	payload, err := json.Marshal(request)
	require.NoError(t, err)
	action := &pb.MiningGroupsAction{Request: payload, EncryptedPools: encrypted}
	command := &pb.MinerCommand{Action: &pb.MinerCommand_MiningGroups{MiningGroups: action}}
	target := &pb.MinerConnectionDescriptor{DeviceIdentifier: "ct1"}
	prepared, err := newMiningGroupsCommand(private, target, command)
	require.NoError(t, err)
	require.Equal(t, "private", prepared.request.Pools[0].Password)
	require.Equal(t, request.PoolIDs, prepared.request.PoolIDs)
	require.Equal(t, request.PoolNames, prepared.request.PoolNames)
	request.Pools[0].Username = "different"
	action.Request, err = json.Marshal(request)
	require.NoError(t, err)
	_, err = newMiningGroupsCommand(private, target, command)
	require.Error(t, err)
	request.Pools[0].Username = "worker"
	request.Pools[0].Password = "private"
	action.Request, err = json.Marshal(request)
	require.NoError(t, err)
	_, err = newMiningGroupsCommand(private, target, command)
	require.Error(t, err)
}

type miningGroupResultDevice struct {
	sdk.Device
	result sdk.MiningGroupResult
}

func (d miningGroupResultDevice) MiningGroups(context.Context, sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	return d.result, nil
}

func TestMiningGroupsNodeValidatesOutgoingReceipts(t *testing.T) {
	command := &miningGroupsCommand{request: sdk.MiningGroupRequest{Action: "start", Key: "k", GroupID: "g", Revision: 1}}
	data, err := command.run(t.Context(), miningGroupResultDevice{result: sdk.MiningGroupResult{Group: &sdk.MiningGroup{ID: "g", Revision: 0, Mining: true}}})
	require.Error(t, err)
	require.Empty(t, data)
	for _, result := range []sdk.MiningGroupResult{
		{Group: &sdk.MiningGroup{ID: "g", Revision: 2, Mining: true}},
		{RevisionConflict: true},
	} {
		data, err := command.run(t.Context(), miningGroupResultDevice{result: result})
		require.NoError(t, err)
		decoded, err := sdk.DecodeMiningGroupResult(data, command.request)
		require.NoError(t, err)
		require.Equal(t, result, decoded)
	}
}
