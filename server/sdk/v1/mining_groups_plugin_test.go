package sdk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type miningGroupResponseClient struct {
	pb.DriverClient
	payload []byte
}

func (c miningGroupResponseClient) MiningGroups(context.Context, *pb.MiningGroupsRequest, ...grpc.CallOption) (*pb.MiningGroupsResponse, error) {
	return &pb.MiningGroupsResponse{Payload: c.payload}, nil
}

type miningGroupResultDevice struct {
	Device
	result MiningGroupResult
}

func (d miningGroupResultDevice) MiningGroups(context.Context, MiningGroupRequest) (MiningGroupResult, error) {
	return d.result, nil
}

func TestMiningGroupResultValidationAtPluginBoundaries(t *testing.T) {
	observed := MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 8, ObservedAt: time.Now().UTC(), Groups: []MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}
	group := &MiningGroup{ID: "g", Revision: 2, Mining: true}
	operation := &MiningGroupOperation{ID: "op", Phase: "QUIESCING", BlockedMACs: []string{"02:00:00:00:00:01"}}
	requests := map[string]MiningGroupRequest{
		"observe":       {Action: "observe"},
		"activate":      {Action: "activate", Key: "k", Revision: 1},
		"deactivate":    {Action: "deactivate", Key: "k", Revision: 1},
		"create":        {Action: "create", Key: "k", ConfigID: "config"},
		"start":         {Action: "start", Key: "k", GroupID: "g", Revision: 1},
		"remove":        {Action: "remove", Key: "k", GroupID: "g", Revision: 1},
		"replace_pools": {Action: "replace_pools", Key: "k", GroupID: "g", Revision: 1, Pools: []MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}},
		"transfer":      {Action: "transfer", Key: "k", GroupID: "g", Revision: 1, MACs: []string{"02:00:00:00:00:01"}},
		"operation":     {Action: "operation", GroupID: "op"},
	}
	for _, tc := range []struct {
		name   string
		action string
		result MiningGroupResult
		valid  bool
	}{
		{"supported observation", "observe", observed, true},
		{"unsupported observation", "observe", MiningGroupResult{}, true},
		{"activation receipt", "activate", MiningGroupResult{Enabled: true}, true},
		{"deactivation receipt", "deactivate", MiningGroupResult{Deactivated: true}, true},
		{"missing deactivation receipt", "deactivate", MiningGroupResult{}, false},
		{"wrong deactivation receipt", "activate", MiningGroupResult{Deactivated: true}, false},
		{"mixed deactivation receipt", "deactivate", MiningGroupResult{Deactivated: true, Enabled: true}, false},
		{"creation receipt", "create", MiningGroupResult{Group: group}, true},
		{"pool receipt", "replace_pools", MiningGroupResult{Group: group}, true},
		{"start receipt", "start", MiningGroupResult{Group: group}, true},
		{"removal receipt", "remove", MiningGroupResult{Removed: true}, true},
		{"transfer receipt", "transfer", MiningGroupResult{Operation: operation}, true},
		{"operation receipt", "operation", MiningGroupResult{Operation: operation}, true},
		{"revision conflict", "start", MiningGroupResult{RevisionConflict: true}, true},
		{"zero observation revision", "observe", MiningGroupResult{Supported: true, MaxGroups: 8, ObservedAt: observed.ObservedAt}, false},
		{"missing observation time", "observe", MiningGroupResult{Supported: true, Revision: 1, MaxGroups: 8}, false},
		{"invalid capacity", "observe", MiningGroupResult{Supported: true, Revision: 1, ObservedAt: observed.ObservedAt}, false},
		{"enabled unsupported", "observe", MiningGroupResult{Enabled: true}, false},
		{"invalid group revision", "observe", MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 8, ObservedAt: observed.ObservedAt, Groups: []MiningGroup{{ID: "g"}}}, false},
		{"duplicate group IDs", "observe", MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 8, ObservedAt: observed.ObservedAt, Groups: []MiningGroup{*group, *group}}, false},
		{"duplicate membership", "observe", MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 8, ObservedAt: observed.ObservedAt, Groups: []MiningGroup{observed.Groups[0], {ID: "g", Revision: 1, MACs: observed.Groups[0].MACs}}}, false},
		{"invalid MAC", "create", MiningGroupResult{Group: &MiningGroup{ID: "g", Revision: 1, MACs: []string{"module-1"}}}, false},
		{"missing creation receipt", "create", MiningGroupResult{}, false},
		{"missing removal receipt", "remove", MiningGroupResult{}, false},
		{"missing activation receipt", "activate", MiningGroupResult{}, false},
		{"wrong pool group", "replace_pools", MiningGroupResult{Group: &MiningGroup{ID: "other", Revision: 2}}, false},
		{"start without mining", "start", MiningGroupResult{Group: &MiningGroup{ID: "g", Revision: 2}}, false},
		{"missing transfer operation", "transfer", MiningGroupResult{}, false},
		{"missing phase", "transfer", MiningGroupResult{Operation: &MiningGroupOperation{ID: "op"}}, false},
		{"wrong operation", "operation", MiningGroupResult{Operation: &MiningGroupOperation{ID: "other", Phase: "COMPLETE"}}, false},
		{"read conflict", "observe", MiningGroupResult{RevisionConflict: true}, false},
		{"contradictory conflict", "start", MiningGroupResult{RevisionConflict: true, Group: group}, false},
		{"wrong action receipt", "observe", MiningGroupResult{Removed: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := requests[tc.action]
			require.NoError(t, req.Validate())
			data, err := json.Marshal(tc.result)
			require.NoError(t, err)
			client := &DeviceGRPCClient{client: miningGroupResponseClient{payload: data}}
			got, err := client.MiningGroups(t.Context(), req)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.result, got)
			} else {
				require.Error(t, err)
			}
			server := &DriverGRPCServer{devices: map[string]Device{"ct1": miningGroupResultDevice{result: tc.result}}}
			requestData, err := json.Marshal(req)
			require.NoError(t, err)
			response, err := server.MiningGroups(t.Context(), &pb.MiningGroupsRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Payload: requestData})
			if tc.valid {
				require.NoError(t, err)
				require.JSONEq(t, string(data), string(response.Payload))
			} else {
				require.Error(t, err)
				require.Nil(t, response)
			}
		})
	}
}

func TestMiningGroupClientRejectsMalformedJSON(t *testing.T) {
	for _, payload := range []string{
		`null`, `{`, `{} {}`, `{"unexpected":"private-value"}`,
		`{"group":{"id":"g","revision":1,"pools":[{"password":"private-value"}]}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			client := &DeviceGRPCClient{client: miningGroupResponseClient{payload: []byte(payload)}}
			_, err := client.MiningGroups(t.Context(), MiningGroupRequest{Action: "observe"})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-value")
		})
	}
}
