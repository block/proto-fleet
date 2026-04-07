package pairing

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func mockSessionContext(ctx context.Context, userID, orgID int64) context.Context {
	return authn.SetInfo(ctx, &session.Info{
		SessionID:      "test-session",
		UserID:         userID,
		OrganizationID: orgID,
	})
}

func TestHandleAuthenticationRequiredPairing_PreservesExistingWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)

	service := &Service{
		deviceStore: mockDeviceStore,
		transactor:  mockTransactor,
	}

	discoveredDevice := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "device-123",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
			MacAddress:       "AA:BB:CC:DD:EE:FF",
		},
		OrgID: 1,
	}

	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(t.Context())
		},
	)

	mockDeviceStore.EXPECT().
		GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1)).
		Return(nil, fleeterror.NewNotFoundError("no paired device"))
	mockDeviceStore.EXPECT().
		GetDeviceByDeviceIdentifier(gomock.Any(), "device-123", int64(1)).
		Return(&pb.Device{DeviceIdentifier: "device-123"}, nil)
	mockDeviceStore.EXPECT().
		UpdateDeviceInfo(gomock.Any(), gomock.Any(), int64(1)).
		Return(nil)
	mockDeviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(1), []string{"device-123"}, false).
		Return([]stores.DeviceRenameProperties{
			{
				DeviceIdentifier: "device-123",
				WorkerName:       "rig-01",
			},
		}, nil)
	mockDeviceStore.EXPECT().
		UpsertDevicePairing(gomock.Any(), gomock.Any(), int64(1), StatusAuthenticationNeeded).
		Return(nil)

	err := service.handleAuthenticationRequiredPairing(t.Context(), discoveredDevice)
	require.NoError(t, err)
}

func TestCanonicalCIDR(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantCIDR     string
		wantMaskBits int
		wantIsIPv4   bool
		wantOK       bool
	}{
		{
			name:         "valid IPv4 /24",
			input:        "192.168.1.0/24",
			wantCIDR:     "192.168.1.0/24",
			wantMaskBits: 24,
			wantIsIPv4:   true,
			wantOK:       true,
		},
		{
			name:         "valid IPv4 /16",
			input:        "10.0.0.0/16",
			wantCIDR:     "10.0.0.0/16",
			wantMaskBits: 16,
			wantIsIPv4:   true,
			wantOK:       true,
		},
		{
			name:         "IPv4 with host bits strips them",
			input:        "192.168.1.100/24",
			wantCIDR:     "192.168.1.0/24",
			wantMaskBits: 24,
			wantIsIPv4:   true,
			wantOK:       true,
		},
		{
			name:         "valid IPv6",
			input:        "fd00::/64",
			wantCIDR:     "fd00::/64",
			wantMaskBits: 64,
			wantIsIPv4:   false,
			wantOK:       true,
		},
		{
			name:   "malformed input",
			input:  "not-a-cidr",
			wantOK: false,
		},
		{
			name:   "empty string",
			input:  "",
			wantOK: false,
		},
		{
			name:   "bare IP without mask",
			input:  "192.168.1.1",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonical, maskBits, isIPv4, ok := canonicalCIDR(tt.input)
			require.Equal(t, tt.wantOK, ok)
			if ok {
				require.Equal(t, tt.wantCIDR, canonical)
				require.Equal(t, tt.wantMaskBits, maskBits)
				require.Equal(t, tt.wantIsIPv4, isIPv4)
			}
		})
	}
}

func TestMergeAutoDiscoveryTargets(t *testing.T) {
	tests := []struct {
		name         string
		baseTarget   string
		knownSubnets []string
		want         []string
	}{
		{
			name:         "merges unique subnets",
			baseTarget:   "192.168.1.0/24",
			knownSubnets: []string{"192.168.25.0/24", "10.0.0.0/24"},
			want:         []string{"192.168.1.0/24", "192.168.25.0/24", "10.0.0.0/24"},
		},
		{
			name:         "deduplicates base target from known subnets",
			baseTarget:   "192.168.1.0/24",
			knownSubnets: []string{"192.168.1.0/24", "192.168.25.0/24"},
			want:         []string{"192.168.1.0/24", "192.168.25.0/24"},
		},
		{
			name:         "skips malformed CIDRs from DB",
			baseTarget:   "192.168.1.0/24",
			knownSubnets: []string{"not-a-cidr", "192.168.25.0/24", "also-bad"},
			want:         []string{"192.168.1.0/24", "192.168.25.0/24"},
		},
		{
			name:         "rejects IPv6 subnets when base is IPv4",
			baseTarget:   "192.168.1.0/24",
			knownSubnets: []string{"fd00::/64", "192.168.25.0/24"},
			want:         []string{"192.168.1.0/24", "192.168.25.0/24"},
		},
		{
			name:         "rejects IPv4 subnets when base is IPv6",
			baseTarget:   "fd00::/64",
			knownSubnets: []string{"192.168.1.0/24", "fd01::/64"},
			want:         []string{"fd00::/64", "fd01::/64"},
		},
		{
			name:         "empty known subnets returns base only",
			baseTarget:   "192.168.1.0/24",
			knownSubnets: []string{},
			want:         []string{"192.168.1.0/24"},
		},
		{
			name:         "nil known subnets returns base only",
			baseTarget:   "192.168.1.0/24",
			knownSubnets: nil,
			want:         []string{"192.168.1.0/24"},
		},
		{
			name:         "malformed base target returned as-is",
			baseTarget:   "not-valid",
			knownSubnets: []string{"192.168.1.0/24"},
			want:         []string{"not-valid"},
		},
		{
			name:         "canonicalizes base target with host bits",
			baseTarget:   "192.168.1.100/24",
			knownSubnets: []string{"192.168.25.0/24"},
			want:         []string{"192.168.1.0/24", "192.168.25.0/24"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeAutoDiscoveryTargets(tt.baseTarget, tt.knownSubnets)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveNmapTargets_ExpandsLocalSubnetWithKnownSubnets(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	service := &Service{
		deviceStore: mockDeviceStore,
		localNetworkInfo: func(context.Context) (*NetworkInfo, error) {
			return &NetworkInfo{NetworkInfo: networking.NetworkInfo{Subnet: "192.168.1.0/24"}}, nil
		},
	}

	ctx := mockSessionContext(t.Context(), 1, 42)
	mockDeviceStore.EXPECT().
		GetKnownSubnets(gomock.Any(), int64(42), 24).
		Return([]string{"192.168.25.0/24", "192.168.1.0/24", "not-a-cidr"}, nil)

	targets, err := service.resolveNmapTargets(ctx, "192.168.1.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.1.0/24", "192.168.25.0/24"}, targets)
}

func TestResolveNmapTargets_SkipsExpansionForNonLocalTargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	service := &Service{
		deviceStore: mockDeviceStore,
		localNetworkInfo: func(context.Context) (*NetworkInfo, error) {
			return &NetworkInfo{NetworkInfo: networking.NetworkInfo{Subnet: "192.168.1.0/24"}}, nil
		},
	}

	ctx := mockSessionContext(t.Context(), 1, 42)

	targets, err := service.resolveNmapTargets(ctx, "192.168.25.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.25.0/24"}, targets)
}

func TestResolveNmapTargets_FallsBackWhenLocalNetworkInfoFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	service := &Service{
		deviceStore: mockDeviceStore,
		localNetworkInfo: func(context.Context) (*NetworkInfo, error) {
			return nil, errors.New("network lookup failed")
		},
	}

	ctx := mockSessionContext(t.Context(), 1, 42)

	targets, err := service.resolveNmapTargets(ctx, "192.168.1.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.1.0/24"}, targets)
}
