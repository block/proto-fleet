package minerdiscovery_test

import (
	"context"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockDiscoverer struct {
	MinerType    miner.Type
	DiscoverFunc func(ctx context.Context, ipAddress string, port string) (*pb.Device, error)
}

var _ minerdiscovery.Discoverer = (*MockDiscoverer)(nil)

func (m *MockDiscoverer) Discover(ctx context.Context, ipAddress string, port string) (*pb.Device, error) {
	return m.DiscoverFunc(ctx, ipAddress, port)
}

func (m *MockDiscoverer) GetMinerType() miner.Type {
	return m.MinerType
}

func TestService(t *testing.T) {

	t.Run("should discover device using first successful discoverer", func(t *testing.T) {
		failingDiscoverer := &MockDiscoverer{
			MinerType: miner.TypeAntminer,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*pb.Device, error) {
				return nil, assert.AnError
			},
		}

		successfulDiscoverer := &MockDiscoverer{
			MinerType: miner.TypeProto,
			DiscoverFunc: func(ctx context.Context, ipAddress string, port string) (*pb.Device, error) {
				return &pb.Device{
					IpAddress:    ipAddress,
					Port:         port,
					SerialNumber: "PROTO123",
				}, nil
			},
		}

		service, _ := minerdiscovery.NewService(failingDiscoverer, successfulDiscoverer)

		device, err := service.Discover(t.Context(), "192.168.1.1", "8080")
		require.NoError(t, err)
		assert.NotNil(t, device)
		assert.Equal(t, "PROTO123", device.SerialNumber)
	})

	t.Run("should return error if all discoverers fail", func(t *testing.T) {
		failingDiscoverer1 := &MockDiscoverer{
			MinerType: miner.TypeAntminer,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*pb.Device, error) {
				return nil, assert.AnError
			},
		}

		failingDiscoverer2 := &MockDiscoverer{
			MinerType: miner.TypeProto,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*pb.Device, error) {
				return nil, assert.AnError
			},
		}

		service, _ := minerdiscovery.NewService(failingDiscoverer1, failingDiscoverer2)

		device, err := service.Discover(t.Context(), "192.168.1.1", "8080")
		require.Error(t, err)
		assert.Nil(t, device)
	})
}
