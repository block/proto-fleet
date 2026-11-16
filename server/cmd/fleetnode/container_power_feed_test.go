package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type containerPowerFeedDevice struct {
	sdk.Device
	received sdk.ContainerPowerFeedRequest
}

func (d *containerPowerFeedDevice) ContainerPowerFeed(_ context.Context, r sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	d.received = r
	return sdk.ContainerPowerFeedResult{Inspection: &sdk.ModulePowerFeedInspection{ObservedAt: time.Now().UTC(), Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingConfigured, FeedID: "pdu-1-outlet-01"}}}, nil
}

func TestContainerPowerFeedNodeDispatchAddressesControllerAndReturnsInspection(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	base := mocks.NewMockDevice(ctrl)
	closed := make(chan struct{})
	// Inspection is a deferrable read, so the node closes its handle after the ack.
	base.EXPECT().Close(gomock.Any()).DoAndReturn(func(context.Context) error { close(closed); return nil })
	device := &containerPowerFeedDevice{Device: base}
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
		require.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
		return sdk.NewDeviceResult{Device: device}, nil
	})
	run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	capture := &captureAcker{}
	run.handleMinerCommand(t.Context(), nil, capture, "container", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_ContainerPowerFeed{ContainerPowerFeed: &pb.ContainerPowerFeedAction{Request: data}}}), slog.Default())
	ack := capture.only(t)
	require.True(t, ack.Succeeded, ack.ErrorMessage)
	require.Equal(t, req, device.received)
	got, err := sdk.DecodeContainerPowerFeedResult(ack.Payload, req)
	require.NoError(t, err)
	require.Equal(t, "pdu-1-outlet-01", got.Inspection.Mapping.FeedID)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("the device handle was not closed")
	}
}

// A node at this protocol version only inspects; an action it does not know,
// such as a write from a newer server, is refused and never reaches the controller.
func TestContainerPowerFeedNodeRefusesActionsItDoesNotKnow(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	base := mocks.NewMockDevice(ctrl)
	base.EXPECT().Close(gomock.Any()).Return(nil)
	device := &containerPowerFeedDevice{Device: base}
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
	data := []byte(`{"action":"turn_off","module":{"tank":1,"module":1}}`)
	capture := &captureAcker{}
	run.handleMinerCommand(t.Context(), nil, capture, "container", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_ContainerPowerFeed{ContainerPowerFeed: &pb.ContainerPowerFeedAction{Request: data}}}), slog.Default())
	ack := capture.only(t)
	require.False(t, ack.Succeeded)
	require.Empty(t, ack.Payload)
	require.Zero(t, device.received, "the controller must not see the request")
}
