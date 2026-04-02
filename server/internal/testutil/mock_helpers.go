package testutil

import (
	discovererMocks "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/mocks"
	pairerMocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"go.uber.org/mock/gomock"
)

// NewMockProtoDiscoverer creates a mock discoverer for testing.
// Use this helper in tests that need a discoverer without testing actual discovery logic.
// For custom discovery behavior, create the mock directly and set specific expectations.
//
// Example usage:
//
//	ctrl := gomock.NewController(t)
//	mockDiscoverer := testutil.NewMockProtoDiscoverer(ctrl)
//	mockDiscoverer.EXPECT().Discover(ctx, "192.168.1.1", "80").Return(device, nil)
func NewMockProtoDiscoverer(ctrl *gomock.Controller) *discovererMocks.MockDiscoverer {
	return discovererMocks.NewMockDiscoverer(ctrl)
}

// NewMockProtoPairer creates a mock pairer for testing
func NewMockProtoPairer(ctrl *gomock.Controller) *pairerMocks.MockPairer {
	return pairerMocks.NewMockPairer(ctrl)
}
