package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
)

// A cancel stops a command that only reads and is ignored for a physical
// write, which runs to its end and acks what it did.
func TestControlLoop_CancelStopsAReadAndLeavesAPhysicalWriteRunning(t *testing.T) {
	prev := commandTimeout
	commandTimeout = 30 * time.Second
	t.Cleanup(func() { commandTimeout = prev })

	ctrl := gomock.NewController(t)
	rebootStarted := make(chan struct{})
	rebootRelease := make(chan struct{})
	rebootCancelled := make(chan struct{})
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().Reboot(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		close(rebootStarted)
		select {
		case <-rebootRelease:
			// A cancelled write context fails the ack check below, even when
			// this branch wins the select.
			return ctx.Err()
		case <-ctx.Done():
			close(rebootCancelled)
			return ctx.Err()
		}
	})
	dev.EXPECT().Close(gomock.Any()).Return(nil).AnyTimes()
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil).AnyTimes()
	disc := newBlockingDiscoverer("10.0.0.42")
	cmd := &RunCmd{DiscoveryRanges: []string{"10.0.0.0/8"}, discoverer: disc, driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}

	cancels := make(chan string)
	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{cancelOnSignal: cancels})
	fake.queueWithID("write", mustMarshal(t, &pb.AgentCommand{Command: &pb.AgentCommand_MinerCommand{MinerCommand: withTarget(
		&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}},
	)}}))
	fake.queueWithID("read", discoverPayload(t, &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpList{
		IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.42"}, Ports: []string{"4028"}},
	}}))
	client := newControlClient(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, &bootstrap.State{FleetNodeID: 7}, discardLogger(t)) }()
	<-rebootStarted
	disc.waitStarted(t, "10.0.0.42")

	// The node handles cancels in order, so once the read has stopped the
	// write's cancel has already been seen and ignored.
	cancels <- "write"
	cancels <- "read"
	disc.waitCancelled(t, "10.0.0.42")
	select {
	case <-rebootCancelled:
		t.Fatal("a cancel interrupted a physical write")
	default:
	}

	close(rebootRelease)
	require.Eventually(t, func() bool {
		for _, ack := range fake.acksCopy() {
			if ack.GetCommandId() == "write" {
				return ack.GetSucceeded()
			}
		}
		return false
	}, 3*time.Second, 20*time.Millisecond, "the write runs to its end and acks success")
	cancel()
	<-done
	cmd.waitForControlWorkers(discardLogger(t))
}
