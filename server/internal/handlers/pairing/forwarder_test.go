package pairing

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

func dev(id, ip, port string) *pb.Device {
	return &pb.Device{DeviceIdentifier: id, IpAddress: ip, Port: port}
}

func TestDedupForwarder_DedupesAcrossSources(t *testing.T) {
	// Arrange
	var sent []*pb.DiscoverResponse
	fwd := newDedupForwarder(func(r *pb.DiscoverResponse) error { sent = append(sent, r); return nil }, nil)

	// Act: two sources report a shared device (mac:a) plus distinct ones.
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("mac:a", "10.0.0.1", "80"), dev("mac:b", "10.0.0.2", "80")}}))
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("mac:a", "10.0.0.1", "80"), dev("mac:c", "10.0.0.3", "80")}}))

	// Assert: mac:a forwarded once; the second batch is reduced to just mac:c.
	require.Len(t, sent, 2)
	assert.Len(t, sent[0].GetDevices(), 2)
	require.Len(t, sent[1].GetDevices(), 1)
	assert.Equal(t, "mac:c", sent[1].GetDevices()[0].GetDeviceIdentifier())
}

func TestDedupForwarder_IPPortFallbackKey(t *testing.T) {
	// Arrange: devices without an identifier dedupe by ip:port.
	var sent []*pb.DiscoverResponse
	fwd := newDedupForwarder(func(r *pb.DiscoverResponse) error { sent = append(sent, r); return nil }, nil)

	// Act
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("", "10.0.0.5", "4028")}}))
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("", "10.0.0.5", "4028")}}))

	// Assert: the second (all-duplicate) batch is dropped.
	require.Len(t, sent, 1)
}

func TestDedupForwarder_DropsAllDuplicateBatchButKeepsErrorResponse(t *testing.T) {
	// Arrange
	var sent []*pb.DiscoverResponse
	fwd := newDedupForwarder(func(r *pb.DiscoverResponse) error { sent = append(sent, r); return nil }, nil)
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("mac:a", "10.0.0.1", "80")}}))

	// Act: a fully-duplicate batch is dropped; an error-only response is forwarded.
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("mac:a", "10.0.0.1", "80")}}))
	require.NoError(t, fwd.forward(&pb.DiscoverResponse{Error: "scan failed"}))

	// Assert
	require.Len(t, sent, 2)
	assert.Equal(t, "scan failed", sent[1].GetError())
}

func TestDedupForwarder_SendErrorRecordedAndCancels(t *testing.T) {
	// Arrange
	sendErr := errors.New("stream gone")
	var cancelled bool
	fwd := newDedupForwarder(func(*pb.DiscoverResponse) error { return sendErr }, func() { cancelled = true })

	// Act
	err1 := fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("mac:a", "10.0.0.1", "80")}})
	err2 := fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev("mac:b", "10.0.0.2", "80")}})

	// Assert: the first failure records the error and cancels; the second short-circuits.
	require.ErrorIs(t, err1, sendErr)
	require.ErrorIs(t, err2, sendErr)
	assert.True(t, cancelled)
	require.ErrorIs(t, fwd.failure(), sendErr)
}

func TestDedupForwarder_ConcurrentForwardIsSerialized(t *testing.T) {
	// Arrange: many goroutines forward distinct devices; -race verifies safety.
	var mu sync.Mutex
	count := 0
	fwd := newDedupForwarder(func(r *pb.DiscoverResponse) error {
		mu.Lock()
		count += len(r.GetDevices())
		mu.Unlock()
		return nil
	}, nil)
	var wg sync.WaitGroup

	// Act
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = fwd.forward(&pb.DiscoverResponse{Devices: []*pb.Device{dev(fmt.Sprintf("mac:%d", i), "10.0.0.1", "80")}})
		}(i)
	}
	wg.Wait()

	// Assert: all 50 distinct devices forwarded exactly once.
	assert.Equal(t, 50, count)
}
