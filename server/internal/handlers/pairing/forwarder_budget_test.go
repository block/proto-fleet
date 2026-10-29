package pairing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

func TestDedupForwarderPreserves50000AndCapsCrossingPrefix(t *testing.T) {
	// Arrange
	var sent []*pb.DiscoverResponse
	cancellations := 0
	fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error {
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
		require.NoError(t, fwd.forward(batch(start, start+1000)))
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
	require.NoError(t, fwd.forward(batch(0, 1000)))
	require.NoError(t, fwd.forward(crossing))

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
		require.NoError(t, fwd.forward(batch(70000, 71000)))
	}

	// Assert
	require.Len(t, sent, count)
	require.Len(t, fwd.seen, discoverylimits.MaxUniqueDevicesPerRequest)
	require.Equal(t, 1, cancellations)
}
