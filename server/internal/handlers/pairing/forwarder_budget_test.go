package pairing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

func TestDedupForwarderPreserves50000AndCapsCrossingPrefix(t *testing.T) {
	// Arrange
	var sent []*pb.DiscoverResponse
	cancellations := 0
	fwd := newDedupForwarder(nil, func(resp *pb.DiscoverResponse) error {
		require.Zero(t, cancellations, "accepted prefix and warning must flush before cancellation")
		sent = append(sent, resp)
		return nil
	}, func() { cancellations++ })
	batch := func(start, end int) *pb.DiscoverResponse {
		resp := &pb.DiscoverResponse{}
		for i := start; i < end; i++ {
			resp.Devices = append(resp.Devices, dev(fmt.Sprintf("miner-%d", i), "10.0.0.1", "4028"))
		}
		return resp
	}

	// Act
	for start := 0; start < 50000; start += 1000 {
		require.NoError(t, fwd.forward(t.Context(), batch(start, start+1000)))
	}

	// Assert
	require.Len(t, fwd.seen, 50000)
	require.Zero(t, cancellations)
	forwarded := 0
	for _, resp := range sent {
		require.Empty(t, resp.GetWarning())
		forwarded += len(resp.GetDevices())
	}
	require.Equal(t, 50000, forwarded)

	// Arrange: duplicate input does not consume the remaining slots. The crossing
	// batch retains its accepted prefix, preserves its source warning, and drops its tail.
	crossing := batch(49999, discoverylimits.MaxUniqueDevicesPerRequest+2)
	crossing.Warning = "Fleet Node: existing warning"

	// Act
	require.NoError(t, fwd.forward(t.Context(), batch(0, 1000)))
	require.NoError(t, fwd.forward(t.Context(), crossing))

	// Assert
	require.Len(t, fwd.seen, discoverylimits.MaxUniqueDevicesPerRequest)
	last := sent[len(sent)-1]
	require.Len(t, last.GetDevices(), discoverylimits.MaxUniqueDevicesPerRequest-50000)
	require.Equal(t, "miner-50000", last.GetDevices()[0].GetDeviceIdentifier())
	require.Equal(t, "miner-65535", last.GetDevices()[len(last.GetDevices())-1].GetDeviceIdentifier())
	require.Contains(t, last.GetWarning(), "Fleet Node: existing warning")
	require.Contains(t, last.GetWarning(), discoverylimits.ResultLimitWarning)
	require.Equal(t, 1, cancellations)
	require.NoError(t, fwd.failure(), "a limit is successful partial discovery, not a transport failure")

	// Act: later input is ignored once the limit is reached
	count := len(sent)
	for range 10 {
		require.NoError(t, fwd.forward(t.Context(), batch(70000, 71000)))
	}

	// Assert
	require.Len(t, sent, count)
	require.Len(t, fwd.seen, discoverylimits.MaxUniqueDevicesPerRequest)
	require.Equal(t, 1, cancellations)
}

func TestDedupForwarderLimitWaitsForAdmittedBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	cancelled := make(chan struct{})
	var cancelOnce sync.Once
	var sentMu sync.Mutex
	var sent []string
	fwd := newDedupForwarder(
		func(ctx context.Context, devices []*pb.Device) error {
			if devices[0].GetDeviceIdentifier() != "slow" {
				return nil
			}
			close(slowStarted)
			select {
			case <-releaseSlow:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		func(response *pb.DiscoverResponse) error {
			sentMu.Lock()
			defer sentMu.Unlock()
			for _, device := range response.GetDevices() {
				sent = append(sent, device.GetDeviceIdentifier())
			}
			return nil
		},
		func() {
			cancelOnce.Do(func() {
				close(cancelled)
				cancel()
			})
		},
	)
	for i := range discoverylimits.MaxUniqueDevicesPerRequest - 2 {
		fwd.seen[fmt.Sprintf("prefill-%d", i)] = struct{}{}
	}

	slowDone := make(chan error, 1)
	go func() {
		slowDone <- fwd.forward(ctx, &pb.DiscoverResponse{Devices: []*pb.Device{dev("slow", "10.0.0.1", "80")}})
	}()
	<-slowStarted
	require.NoError(t, fwd.forward(ctx, &pb.DiscoverResponse{Devices: []*pb.Device{
		dev("fast", "10.0.0.2", "80"),
		dev("over-limit", "10.0.0.3", "80"),
	}}))

	select {
	case <-cancelled:
		t.Fatal("result limit cancelled an admitted batch")
	default:
	}
	close(releaseSlow)
	require.NoError(t, <-slowDone)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("result limit did not cancel after admitted batches finished")
	}
	assert.ElementsMatch(t, []string{"slow", "fast"}, sent)
}

func TestDedupForwarderAllowsTimeoutWarningWhenLimitBatchWasNotSent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var sent []*pb.DiscoverResponse
	fwd := newDedupForwarder(
		func(context.Context, []*pb.Device) error {
			cancel()
			return ctx.Err()
		},
		func(response *pb.DiscoverResponse) error {
			sent = append(sent, response)
			return nil
		},
		nil,
	)
	for i := range discoverylimits.MaxUniqueDevicesPerRequest - 1 {
		fwd.seen[fmt.Sprintf("prefill-%d", i)] = struct{}{}
	}

	err := fwd.forward(ctx, &pb.DiscoverResponse{Devices: []*pb.Device{
		dev("at-limit", "10.0.0.1", "80"),
		dev("over-limit", "10.0.0.2", "80"),
	}})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, fwd.forwardTerminalWarning(t.Context(), discoverylimits.RequestTimeoutWarning))

	require.Len(t, sent, 1)
	assert.Equal(t, discoverylimits.RequestTimeoutWarning, sent[0].GetWarning())
}
