package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type containerMiningResponseClient struct {
	pb.DriverClient
	payload []byte
	err     error
}

func (c containerMiningResponseClient) ContainerMining(context.Context, *pb.ContainerMiningRequest, ...grpc.CallOption) (*pb.ContainerMiningResponse, error) {
	return &pb.ContainerMiningResponse{Payload: c.payload}, c.err
}

type containerMiningResultDevice struct {
	Device
	result ContainerMiningResult
	err    error
}

func (d containerMiningResultDevice) ContainerMining(context.Context, ContainerMiningRequest) (ContainerMiningResult, error) {
	return d.result, d.err
}
func TestContainerMiningPluginBoundariesValidateResults(t *testing.T) {
	for _, tc := range []struct {
		request ContainerMiningRequest
		result  ContainerMiningResult
	}{
		{ContainerMiningRequest{Action: "status"}, ContainerMiningResult{Status: &ContainerMiningStatus{Mining: new(false)}}},
		{ContainerMiningRequest{Action: "power_target"}, ContainerMiningResult{Power: &ContainerPowerState{Watts: new(2500.0), BalanceBays: new(false)}}},
		{ContainerMiningRequest{Action: "set_mining", Mining: new(false)}, ContainerMiningResult{}},
		{ContainerMiningRequest{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: 2500}}, ContainerMiningResult{}},
	} {
		t.Run(tc.request.Action, func(t *testing.T) {
			for _, valid := range []bool{true, false} {
				result := tc.result
				if !valid {
					result = ContainerMiningResult{}
					if !tc.request.ReadOnly() {
						result.Status = &ContainerMiningStatus{Mining: new(false)}
					}
				}
				payload, err := json.Marshal(result)
				require.NoError(t, err)
				client := &DeviceGRPCClient{client: containerMiningResponseClient{payload: payload}}
				got, err := client.ContainerMining(t.Context(), tc.request)
				if valid {
					require.NoError(t, err)
					require.Equal(t, result, got)
				} else {
					require.Error(t, err)
				}
				server := &DriverGRPCServer{devices: map[string]Device{"ct1": containerMiningResultDevice{result: result}}}
				data, err := json.Marshal(tc.request)
				require.NoError(t, err)
				response, err := server.ContainerMining(t.Context(), &pb.ContainerMiningRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Payload: data})
				if valid {
					require.NoError(t, err)
					require.JSONEq(t, string(payload), string(response.Payload))
				} else {
					require.Error(t, err)
					require.Nil(t, response)
				}
			}
		})
	}
}

func TestContainerMiningPluginBoundariesPreserveWriteErrors(t *testing.T) {
	for _, request := range []ContainerMiningRequest{
		{Action: "set_mining", Mining: new(false)},
		{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: 2500}},
	} {
		t.Run(request.Action, func(t *testing.T) {
			writeErr := errors.New("controller write failed")
			client := &DeviceGRPCClient{client: containerMiningResponseClient{payload: []byte(`{}`), err: writeErr}}
			_, err := client.ContainerMining(t.Context(), request)
			require.ErrorIs(t, err, writeErr)
			server := &DriverGRPCServer{devices: map[string]Device{"ct1": containerMiningResultDevice{err: writeErr}}}
			data, err := json.Marshal(request)
			require.NoError(t, err)
			response, err := server.ContainerMining(t.Context(), &pb.ContainerMiningRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Payload: data})
			require.ErrorContains(t, err, writeErr.Error())
			require.Nil(t, response)
		})
	}
}
