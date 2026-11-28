package pairing

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func containerNetworkInfo(context.Context) (*NetworkInfo, error) {
	return &NetworkInfo{NetworkInfo: networking.NetworkInfo{
		Interface:  "eth0",
		LocalIP:    "10.0.3.56",
		Gateway:    "10.0.3.1",
		Subnet:     "10.0.3.0/24",
		LocalIPv6:  "fd00:3::1",
		IPv6Subnet: "fd00:3::/64",
	}}, nil
}

func TestWithNetworkSubnet_EmptyKeepsDetection(t *testing.T) {
	s := &Service{localNetworkInfo: containerNetworkInfo}
	require.NoError(t, s.WithNetworkSubnet(""))

	info, err := s.GetLocalNetworkInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "10.0.3.0/24", info.Subnet)
	require.Equal(t, "10.0.3.56", info.LocalIP)
}

func TestWithNetworkSubnet_RejectsInvalidSubnets(t *testing.T) {
	for _, cidr := range []string{"192.168.1.10", "not-a-cidr", "fd00::/64"} {
		s := &Service{localNetworkInfo: containerNetworkInfo}
		require.Error(t, s.WithNetworkSubnet(cidr), cidr)
	}
}

func TestWithNetworkSubnet_ReplacesSubnetAndDropsForeignAddresses(t *testing.T) {
	s := &Service{localNetworkInfo: containerNetworkInfo}
	require.NoError(t, s.WithNetworkSubnet("192.168.1.77/24"))

	info, err := s.GetLocalNetworkInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "192.168.1.0/24", info.Subnet)
	require.Empty(t, info.LocalIP)
	require.Empty(t, info.Gateway)
	require.Equal(t, "fd00:3::1", info.LocalIPv6)
	require.Equal(t, "fd00:3::/64", info.IPv6Subnet)
}

func TestWithNetworkSubnet_KeepsAddressesInsideSubnet(t *testing.T) {
	s := &Service{localNetworkInfo: containerNetworkInfo}
	require.NoError(t, s.WithNetworkSubnet("10.0.0.0/16"))

	info, err := s.GetLocalNetworkInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", info.Subnet)
	require.Equal(t, "10.0.3.56", info.LocalIP)
	require.Equal(t, "10.0.3.1", info.Gateway)
}

func TestWithNetworkSubnet_ReportsSubnetWhenDetectionFails(t *testing.T) {
	s := &Service{localNetworkInfo: func(context.Context) (*NetworkInfo, error) {
		return nil, errors.New("no suitable network interface found")
	}}
	require.NoError(t, s.WithNetworkSubnet("192.168.1.0/24"))

	info, err := s.GetLocalNetworkInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "192.168.1.0/24", info.Subnet)
}

func TestWithNetworkSubnet_ExpandsConfiguredSubnetWithKnownSubnets(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	s := &Service{deviceStore: mockDeviceStore, localNetworkInfo: containerNetworkInfo}
	require.NoError(t, s.WithNetworkSubnet("192.168.1.0/24"))

	mockDeviceStore.EXPECT().
		GetKnownSubnets(gomock.Any(), int64(42), 24, true).
		Return([]string{"192.168.25.0/24"}, nil)

	targets, err := s.resolveNetworkScanTargets(mockSessionContext(t.Context(), 1, 42), "192.168.1.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.1.0/24", "192.168.25.0/24"}, targets)
}
