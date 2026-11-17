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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type containerPowerFeedDevice struct {
	sdk.Device
	received sdk.ContainerPowerFeedRequest
	err      error
}

func (d *containerPowerFeedDevice) ContainerPowerFeed(_ context.Context, r sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	d.received = r
	if d.err != nil {
		return sdk.ContainerPowerFeedResult{}, d.err
	}
	if !r.ReadOnly() {
		return sdk.ContainerPowerFeedResult{Applied: &sdk.PowerFeedReading{ID: r.FeedID, State: sdk.PowerFeedStateEnabled, Overridden: true}}, nil
	}
	return sdk.ContainerPowerFeedResult{Inspection: &sdk.ModulePowerFeedInspection{ObservedAt: time.Now().UTC(), Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingConfigured, FeedID: "pdu-1-outlet-01"}}}, nil
}

func runContainerPowerFeedOnNode(t *testing.T, req sdk.ContainerPowerFeedRequest) (*containerPowerFeedDevice, *pb.ControlAck) {
	t.Helper()
	return runContainerPowerFeedOnNodeWithError(t, req, nil)
}

func runContainerPowerFeedOnNodeWithError(t *testing.T, req sdk.ContainerPowerFeedRequest, deviceErr error) (*containerPowerFeedDevice, *pb.ControlAck) {
	t.Helper()
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	base := mocks.NewMockDevice(ctrl)
	closed := make(chan struct{})
	// A deferrable read, such as inspection, closes its handle after the ack.
	base.EXPECT().Close(gomock.Any()).DoAndReturn(func(context.Context) error { close(closed); return nil })
	device := &containerPowerFeedDevice{Device: base, err: deviceErr}
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
		require.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
		return sdk.NewDeviceResult{Device: device}, nil
	})
	run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	capture := &captureAcker{}
	run.handleMinerCommand(t.Context(), nil, capture, "container", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_ContainerPowerFeed{ContainerPowerFeed: &pb.ContainerPowerFeedAction{Request: data}}}), slog.Default())
	ack := capture.only(t)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("the device handle was not closed")
	}
	return device, ack
}

func TestContainerPowerFeedNodeDispatchAddressesControllerAndReturnsInspection(t *testing.T) {
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}}
	device, ack := runContainerPowerFeedOnNode(t, req)
	require.True(t, ack.Succeeded, ack.ErrorMessage)
	require.Equal(t, req, device.received)
	got, err := sdk.DecodeContainerPowerFeedResult(ack.Payload, req)
	require.NoError(t, err)
	require.Equal(t, "pdu-1-outlet-01", got.Inspection.Mapping.FeedID)
}

func TestContainerPowerFeedNodeDispatchReturnsWriteAcknowledgement(t *testing.T) {
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedTurnOn, FeedID: "pdu-1-outlet-01", Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}}
	device, ack := runContainerPowerFeedOnNode(t, req)
	require.True(t, ack.Succeeded, ack.ErrorMessage)
	require.Equal(t, req, device.received)
	got, err := sdk.DecodeContainerPowerFeedResult(ack.Payload, req)
	require.NoError(t, err)
	require.Equal(t, sdk.PowerFeedStateEnabled, got.Applied.State)
}

// The node refuses an action it does not know, such as one from a newer
// server, and the controller never sees it.
func TestContainerPowerFeedNodeRefusesActionsItDoesNotKnow(t *testing.T) {
	device, ack := runContainerPowerFeedOnNode(t, sdk.ContainerPowerFeedRequest{Action: "reboot_pdu", FeedID: "pdu-1-outlet-01", Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1}})
	require.False(t, ack.Succeeded)
	require.Empty(t, ack.Payload)
	require.Zero(t, device.received, "the controller must not see the request")
}

// A controller's refusal keeps its reason and its proof that nothing was
// sent; any other failure stays a generic node failure.
func TestContainerPowerFeedNodeDispatchReportsControllerRefusals(t *testing.T) {
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedTurnOff, FeedID: "pdu-1-outlet-01", Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}}
	refusal := sdk.WriteRefused(status.Error(codes.FailedPrecondition, "power feed pdu-1-outlet-01 is not on"), sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
	_, ack := runContainerPowerFeedOnNodeWithError(t, req, refusal)
	require.False(t, ack.Succeeded)
	require.Equal(t, pb.AckCode_ACK_CODE_FAILED_PRECONDITION, ack.Code)
	require.Equal(t, "container power feed: power feed pdu-1-outlet-01 is not on", ack.ErrorMessage)
	require.Equal(t, sdk.WriteNotDispatched, ack.WriteOutcome.GetOutcome())

	_, ack = runContainerPowerFeedOnNodeWithError(t, req, status.Error(codes.FailedPrecondition, "no proof the write was not sent"))
	require.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, ack.Code)
}
