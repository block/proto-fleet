package minerdiscovery_test

import (
	"context"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockDiscoverer struct {
	MinerType    miner.Type
	DiscoverFunc func(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error)
}

var _ minerdiscovery.Discoverer = (*MockDiscoverer)(nil)

func (m *MockDiscoverer) Discover(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error) {
	return m.DiscoverFunc(ctx, ipAddress, port)
}

func (m *MockDiscoverer) GetMinerType() miner.Type {
	return m.MinerType
}

func TestService(t *testing.T) {

	t.Run("should discover device using first successful discoverer", func(t *testing.T) {
		failingDiscoverer := &MockDiscoverer{
			MinerType: miner.TypeAntminer,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*minerdiscovery.DiscoveredDevice, error) {
				return nil, assert.AnError
			},
		}

		successfulDiscoverer := &MockDiscoverer{
			MinerType: miner.TypeProto,
			DiscoverFunc: func(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error) {
				return &minerdiscovery.DiscoveredDevice{
					Device: pb.Device{
						IpAddress:    ipAddress,
						Port:         port,
						SerialNumber: "PROTO123",
					},
					Type: miner.TypeProto.String(),
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
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*minerdiscovery.DiscoveredDevice, error) {
				return nil, assert.AnError
			},
		}

		failingDiscoverer2 := &MockDiscoverer{
			MinerType: miner.TypeProto,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*minerdiscovery.DiscoveredDevice, error) {
				return nil, assert.AnError
			},
		}

		service, _ := minerdiscovery.NewService(failingDiscoverer1, failingDiscoverer2)

		device, err := service.Discover(t.Context(), "192.168.1.1", "8080")
		require.Error(t, err)
		assert.Nil(t, device)
	})

	t.Run("should return MinerNotFoundFleetError if all discoverers return not found", func(t *testing.T) {
		notFoundDiscoverer1 := &MockDiscoverer{
			MinerType: miner.TypeAntminer,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*minerdiscovery.DiscoveredDevice, error) {
				return nil, minerdiscovery.MinerNotFoundFleetError
			},
		}

		notFoundDiscoverer2 := &MockDiscoverer{
			MinerType: miner.TypeProto,
			DiscoverFunc: func(ctx context.Context, _ string, _ string) (*minerdiscovery.DiscoveredDevice, error) {
				return nil, minerdiscovery.MinerNotFoundFleetError
			},
		}

		service, _ := minerdiscovery.NewService(notFoundDiscoverer1, notFoundDiscoverer2)

		device, err := service.Discover(t.Context(), "192.168.1.1", "8080")
		require.Error(t, err)
		assert.Equal(t, minerdiscovery.MinerNotFoundFleetError, err)
		assert.Nil(t, device)
	})
}
