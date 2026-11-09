package sdk

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiningGroupRequestRejectsIgnoredAndUnsafeFields(t *testing.T) {
	for _, r := range []MiningGroupRequest{
		{Action: "observe", GroupID: "g"},
		{Action: "observe", Key: "write"},
		{Action: "unknown"},
		{Action: "transfer", Key: "k", GroupID: "g", Revision: 1, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:01"}},
		{Action: "transfer", Key: "k", GroupID: "g", Revision: 1, MACs: []string{"module-1"}},
		{Action: "start", Key: "k", GroupID: "g", Revision: 1, Pools: []MiningGroupPoolInput{{}}},
		{Action: "replace_pools", Key: "k", GroupID: "g", Revision: 1, Pools: []MiningGroupPoolInput{{URL: "stratum+ssl://pool.example:3333", Username: "u"}}},
	} {
		require.Error(t, r.Validate(), "%s", r.Action)
	}
	_, err := DecodeMiningGroupRequest([]byte(`{"action":"observe","path":"/other-api"}`))
	require.Error(t, err)
	_, err = DecodeMiningGroupRequest([]byte(`{"action":"observe"}{}`))
	require.Error(t, err)
	r := MiningGroupRequest{Action: "replace_pools", Key: "k", GroupID: "g", Revision: 1, Pools: []MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "u", Password: "secret"}}}
	require.NotContains(t, fmt.Sprintf("%+v %#v", r, r), "secret")
	data, err := json.Marshal(r)
	require.NoError(t, err)
	decoded, err := DecodeMiningGroupRequest(data)
	require.NoError(t, err)
	require.Equal(t, r, decoded)
}

func TestMiningGroupActivationLifecycleContract(t *testing.T) {
	status := MiningGroupRequest{Action: "mining_status"}
	stop := MiningGroupRequest{Action: "stop_legacy", Key: "activation:stop"}
	require.NoError(t, status.Validate())
	require.True(t, status.ReadOnly())
	require.NoError(t, stop.Validate())
	require.False(t, stop.ReadOnly())
	for _, request := range []MiningGroupRequest{
		{Action: "mining_status", Key: "write"},
		{Action: "mining_status", GroupID: "g"},
		{Action: "stop_legacy"},
		{Action: "stop_legacy", Key: "k", GroupID: "g"},
		{Action: "stop_legacy", Key: "k", Revision: 1},
	} {
		require.Error(t, request.Validate())
	}
	require.NoError(t, (MiningGroupResult{MiningStatus: &MiningGroupMiningStatus{}}).Validate(status))
	require.NoError(t, (MiningGroupResult{StopRequested: true}).Validate(stop))
	require.Error(t, (MiningGroupResult{}).Validate(status))
	require.Error(t, (MiningGroupResult{}).Validate(stop))
	require.Error(t, (MiningGroupResult{MiningStatus: &MiningGroupMiningStatus{ActiveBoards: -1}}).Validate(status))
	require.Error(t, (MiningGroupResult{MiningStatus: &MiningGroupMiningStatus{}, StopRequested: true}).Validate(status))
	require.Error(t, (MiningGroupResult{MiningStatus: &MiningGroupMiningStatus{}, StopRequested: true}).Validate(stop))
	require.Error(t, (MiningGroupResult{StopRequested: true, RevisionConflict: true}).Validate(stop))
	require.Error(t, (MiningGroupResult{StopRequested: true}).Validate(MiningGroupRequest{Action: "observe"}))
}

func TestExplicitGroupClearContract(t *testing.T) {
	for _, action := range []string{"clear_pools", "stop"} {
		request := MiningGroupRequest{Action: action, Key: "key", GroupID: "group", Revision: 3}
		data, err := json.Marshal(request)
		require.NoError(t, err)
		decoded, err := DecodeMiningGroupRequest(data)
		require.NoError(t, err)
		require.Equal(t, request, decoded)
		require.False(t, request.ReadOnly())
		request.Pools = []MiningGroupPoolInput{{}}
		require.Error(t, request.Validate())
	}
	require.Error(t, (MiningGroupRequest{Action: "replace_pools", Key: "key", GroupID: "group", Revision: 3}).Validate(), "empty replace must never imply clear")
	request := MiningGroupRequest{Action: "verify_cleared", GroupID: "group", MACs: []string{"02:00:00:00:00:01"}}
	require.NoError(t, request.Validate())
	require.True(t, request.ReadOnly())
	require.Error(t, (MiningGroupResult{}).Validate(request), "missing clear result is not success")
	for _, cleared := range []bool{false, true} {
		data, err := json.Marshal(MiningGroupResult{PoolsCleared: &cleared})
		require.NoError(t, err)
		result, err := DecodeMiningGroupResult(data, request)
		require.NoError(t, err)
		require.Equal(t, cleared, *result.PoolsCleared)
	}
	request.MACs = append(request.MACs, request.MACs[0])
	require.Error(t, request.Validate())
	request.MACs = nil
	require.Error(t, request.Validate())
}
