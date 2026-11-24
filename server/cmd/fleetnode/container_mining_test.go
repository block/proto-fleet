package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type containerMiningDevice struct {
	sdk.Device
	received sdk.ContainerMiningRequest
	err      error
}

func (d *containerMiningDevice) ContainerMining(_ context.Context, r sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	d.received = r
	if d.err != nil {
		return sdk.ContainerMiningResult{}, d.err
	}
	if r.Action == "status" {
		return sdk.ContainerMiningResult{Status: &sdk.ContainerMiningStatus{Mining: new(false), OperatingState: "MINING_OPERATING_STATE_STOPPED"}}, nil
	}
	return sdk.ContainerMiningResult{}, nil
}
func TestContainerMiningNodeDispatchPreservesTypedRequestsAndResults(t *testing.T) {
	for _, req := range []sdk.ContainerMiningRequest{{Action: "status"}, {Action: "set_mining", Mining: new(false)}, {Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 2500, BalanceBays: new(false), HashOnDisconnect: new(true)}}} {
		t.Run(req.Action, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			driver := mocks.NewMockDriver(ctrl)
			base := mocks.NewMockDevice(ctrl)
			base.EXPECT().Close(gomock.Any()).Return(nil)
			device := &containerMiningDevice{Device: base}
			driver.EXPECT().NewDevice(gomock.Any(), gomock.Not("dev-1"), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				require.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
				return sdk.NewDeviceResult{Device: device}, nil
			})
			run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
			data, err := json.Marshal(req)
			require.NoError(t, err)
			capture := &captureAcker{}
			run.handleMinerCommand(t.Context(), nil, capture, "container", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_ContainerMining{ContainerMining: &pb.ContainerMiningAction{Request: data}}}), slog.Default())
			ack := capture.only(t)
			require.True(t, ack.Succeeded, ack.ErrorMessage)
			require.Equal(t, req, device.received)
			_, err = sdk.DecodeContainerMiningResult(ack.Payload, req)
			require.NoError(t, err)
		})
	}
}

func TestContainerMiningNodeEvidenceDistinguishesRefusalFromDispatch(t *testing.T) {
	command := func(data []byte) *pb.MinerCommand {
		return &pb.MinerCommand{Action: &pb.MinerCommand_ContainerMining{ContainerMining: &pb.ContainerMiningAction{Request: data}}}
	}
	request := sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}
	data, err := json.Marshal(request)
	require.NoError(t, err)
	_, err = runMinerAction(t.Context(), nil, "container", "", nil, mocks.NewMockDevice(gomock.NewController(t)), command(data))
	require.Error(t, err)
	require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).GetReason())
	code, _ := classifyMinerCommandError("execute command", err)
	require.Equal(t, pb.AckCode_ACK_CODE_UNIMPLEMENTED, code)

	device := &containerMiningDevice{}
	_, err = runMinerAction(t.Context(), nil, "container", "", nil, device, command([]byte(`{"action":"set_mining"}`)))
	require.Error(t, err)
	require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).GetReason())
	require.Empty(t, device.received.Action)

	device.err = errors.New("controller response lost")
	_, err = runMinerAction(t.Context(), nil, "container", "", nil, device, command(data))
	require.ErrorIs(t, err, device.err)
	require.Equal(t, request, device.received)
	require.Nil(t, sdk.WriteEvidence(err))
}
