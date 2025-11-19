package testutil

import (
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	discovererMocks "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/mocks"
	pairerMocks "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/golang/mock/gomock"
)

// NewMockProtoDiscoverer creates a mock discoverer pre-configured for Proto miner type.
// The mock automatically sets GetMinerType() to return models.TypeProto.
// Use this helper in tests that need a Proto discoverer without testing actual discovery logic.
// For custom discovery behavior, create the mock directly and set specific expectations.
//
// Example usage:
//
//	ctrl := gomock.NewController(t)
//	mockDiscoverer := testutil.NewMockProtoDiscoverer(ctrl)
//	mockDiscoverer.EXPECT().Discover(ctx, "192.168.1.1", "2121").Return(device, nil)
func NewMockProtoDiscoverer(ctrl *gomock.Controller) *discovererMocks.MockDiscoverer {
	mockDiscoverer := discovererMocks.NewMockDiscoverer(ctrl)
	mockDiscoverer.EXPECT().GetMinerType().Return(models.TypeProto).AnyTimes()
	return mockDiscoverer
}

// NewMockProtoPairer creates a mock pairer configured for Proto miner type
func NewMockProtoPairer(ctrl *gomock.Controller) *pairerMocks.MockPairer {
	mockPairer := pairerMocks.NewMockPairer(ctrl)
	mockPairer.EXPECT().GetMinerType().Return(models.TypeProto).AnyTimes()
	return mockPairer
}
