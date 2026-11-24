package pairing

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestIsSameDevice_UnconfirmedDiscoveryNeverReadsCredentials(t *testing.T) {
	for _, tc := range []struct{ name, mac, serial string }{
		{name: "missing identity"},
		{name: "invalid MAC", mac: "invalid-mac"},
		{name: "conflicting MAC", mac: "AA:BB:CC:DD:EE:00", serial: "serial-1"},
		{name: "conflicting serial", mac: "AA:BB:CC:DD:EE:FF", serial: "another-miner"},
		{name: "unrelated identity", mac: "AA:BB:CC:DD:EE:00", serial: "another-miner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			deviceStore := mocks.NewMockDeviceStore(ctrl)
			pairer := pairingmocks.NewMockPairer(ctrl)
			service := &Service{deviceStore: deviceStore, pairer: pairer}
			paired := &pb.Device{DeviceIdentifier: "miner-1", MacAddress: "AA:BB:CC:DD:EE:FF", SerialNumber: "serial-1"}
			deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), "miner-1", int64(7)).Return(paired, nil)
			// No credential-store or driver calls are permitted for an unconfirmed candidate.
			matched := service.IsSameDevice(t.Context(), &discoverymodels.DiscoveredDevice{Device: pb.Device{
				IpAddress: "192.168.1.20", Port: "80", DriverName: "antminer", MacAddress: tc.mac, SerialNumber: tc.serial,
			}}, "miner-1", 7)
			require.False(t, matched)
		})
	}
}

func TestIsSameDevice_ConfirmedIdentityAuthenticationFailureReconcilesStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	pairer := pairingmocks.NewMockPairer(ctrl)
	encryptService, credentials, plainCredentials := recoveryTestCredentials(t)
	service := &Service{deviceStore: deviceStore, transactor: transactor, pairer: pairer, encryptService: encryptService}

	paired := &pb.Device{
		DeviceIdentifier: "miner-1",
		MacAddress:       "AA:BB:CC:DD:EE:FF",
		SerialNumber:     "serial-1",
	}
	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), "miner-1", int64(7)).Return(paired, nil)
	deviceStore.EXPECT().GetMinerCredentials(gomock.Any(), paired, int64(7)).Return(credentials, nil)
	pairer.EXPECT().GetDeviceInfo(gomock.Any(), gomock.Any(), plainCredentials).
		Return(nil, fleeterror.NewUnauthenticatedError("credentials rejected"))
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	)
	deviceStore.EXPECT().LockDeviceForCloudRecoveryByIdentifier(gomock.Any(), "miner-1", int64(7)).
		Return(true, nil)
	deviceStore.EXPECT().ReconcileCloudAuthenticationNeededPairingStatusByIdentifier(gomock.Any(), "miner-1", int64(7)).
		Return(true, true, nil)

	matched := service.IsSameDevice(t.Context(), &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:  "192.168.1.20",
			Port:       "80",
			DriverName: "antminer",
			MacAddress: "aa-bb-cc-dd-ee-ff",
		},
	}, "miner-1", 7)

	require.False(t, matched)
}

func TestAnnotateAlreadyPairedClearsUntrustedFlagWithoutIdentifier(t *testing.T) {
	service := &Service{deviceStore: mocks.NewMockDeviceStore(gomock.NewController(t))}
	device := &pb.Device{AlreadyPaired: true}

	require.NoError(t, service.AnnotateAlreadyPaired(t.Context(), 7, []*pb.Device{device}))
	require.False(t, device.GetAlreadyPaired())
}

func TestAnnotateAlreadyPairedRequiresPairingStateStoreForIdentifiedDevices(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service *Service
	}{
		{name: "nil service"},
		{name: "nil store", service: &Service{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := &pb.Device{DeviceIdentifier: "miner-1", AlreadyPaired: true}

			err := tc.service.AnnotateAlreadyPaired(t.Context(), 7, []*pb.Device{device})

			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
			assert.Contains(t, err.Error(), "pairing state store is not configured")
			assert.False(t, device.GetAlreadyPaired())
		})
	}
}

func TestAnnotateAlreadyPairedHandlesDuplicateIdentifiersInOnePass(t *testing.T) {
	ctrl := gomock.NewController(t)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	service := &Service{deviceStore: deviceStore}
	devices := []*pb.Device{
		{DeviceIdentifier: "paired", AlreadyPaired: false},
		{DeviceIdentifier: "paired", AlreadyPaired: false},
		{DeviceIdentifier: "new", AlreadyPaired: true},
		{AlreadyPaired: true},
	}
	deviceStore.EXPECT().GetAlreadyPairedDeviceIdentifiers(gomock.Any(), int64(7), []string{"paired", "new"}).
		Return([]string{"paired"}, nil)

	require.NoError(t, service.AnnotateAlreadyPaired(t.Context(), 7, devices))
	assert.True(t, devices[0].GetAlreadyPaired())
	assert.True(t, devices[1].GetAlreadyPaired())
	assert.False(t, devices[2].GetAlreadyPaired())
	assert.False(t, devices[3].GetAlreadyPaired())
}

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
	mockDiscoveredStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	mockDiscoveredStore.EXPECT().LockPairingSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	service := &Service{
		deviceStore:           mockDeviceStore,
		transactor:            mockTransactor,
		discoveredDeviceStore: mockDiscoveredStore,
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
		GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1), "").
		Return(nil, fleeterror.NewNotFoundError("no paired device"))
	mockDeviceStore.EXPECT().
		GetDeviceByDeviceIdentifier(gomock.Any(), "device-123", int64(1)).
		Return(&pb.Device{DeviceIdentifier: "device-123"}, nil)
	mockDeviceStore.EXPECT().
		InitializeWorkerName(gomock.Any(), int64(1), minermodels.DeviceIdentifier("device-123"), "AA:BB:CC:DD:EE:FF").
		Return(false, nil)
	mockDeviceStore.EXPECT().
		SetDevicePairingAuthNeededIfNotPaired(gomock.Any(), gomock.Any(), int64(1)).
		Return(true, nil)

	err := service.handleAuthenticationRequiredPairing(t.Context(), discoveredDevice)
	require.NoError(t, err)
}

func TestAuthenticationRequiredPairingRejectsStaleSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	discovered := mocks.NewMockDiscoveredDeviceStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	device := &discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "container", DriverName: "proto", DeviceClass: commonv1.DeviceClass_DEVICE_CLASS_CONTAINER, IpAddress: "10.0.0.11", Port: "8080", RpcPort: 9095,
		Endpoints: []*commonv1.DeviceEndpoint{{Id: "a", DeviceKey: "a-b", Host: "10.0.0.11", Port: 8080, RpcPort: 9095}}}, OrgID: 7, IsActive: true}
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, action func(context.Context) error) error { return action(ctx) })
	discovered.EXPECT().LockPairingSnapshot(gomock.Any(), device.GetDeviceOrgIdentifier(), &device.Device, gomock.Nil()).Return(fleeterror.NewFailedPreconditionError("discovery changed"))
	service := &Service{deviceStore: mocks.NewMockDeviceStore(ctrl), discoveredDeviceStore: discovered, transactor: transactor}
	err := service.handleAuthenticationRequiredPairing(t.Context(), device)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "stale auth-needed state must not reach any device writes: %v", err)
}

func TestResolveDeviceIdentifiers_AllDevicesPassesFullFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	service := &Service{deviceStore: mockDeviceStore}
	ctx := mockSessionContext(t.Context(), 1, 42)
	selector := &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_AllDevices{AllDevices: &minercommandv1.DeviceFilter{
			DeviceStatus:  []fleetmanagementv1.DeviceStatus{fleetmanagementv1.DeviceStatus_DEVICE_STATUS_ONLINE},
			PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
			Models:        []string{"S19"},
			Manufacturers: []string{"Bitmain"},
		}},
	}

	mockDeviceStore.EXPECT().
		GetDeviceIdentifiersByOrgWithFilter(gomock.Any(), int64(42), gomock.AssignableToTypeOf(&stores.MinerFilter{})).
		DoAndReturn(func(_ context.Context, _ int64, filter *stores.MinerFilter) ([]string, error) {
			require.Equal(t, []minermodels.MinerStatus{minermodels.MinerStatusActive}, filter.DeviceStatusFilter)
			require.Equal(t, []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED}, filter.PairingStatuses)
			require.Equal(t, []string{"S19"}, filter.ModelNames)
			require.Equal(t, []string{"Bitmain"}, filter.ManufacturerNames)
			return []string{"mac:cloud"}, nil
		})

	ids, err := service.resolveDeviceIdentifiers(ctx, selector, 42)

	require.NoError(t, err)
	require.Equal(t, []string{"mac:cloud"}, ids)
}

func TestPairDevicesAllowAllFailedReturnsCanceledError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDiscoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	service := &Service{discoveredDeviceStore: mockDiscoveredDeviceStore}
	ctx, cancel := context.WithCancel(mockSessionContext(t.Context(), 1, 42))
	cancel()

	mockDiscoveredDeviceStore.EXPECT().
		GetDevice(gomock.Any(), discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "mac:canceled", OrgID: 42}).
		Return(nil, fleeterror.NewNotFoundError("not found"))

	resp, err := service.PairDevicesAllowAllFailed(ctx, &pb.PairRequest{
		DeviceSelector: &minercommandv1.DeviceSelector{
			SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{DeviceIdentifiers: []string{"mac:canceled"}},
			},
		},
	})

	require.Nil(t, resp)
	require.True(t, fleeterror.IsCanceledError(err))
}

func TestPairingRigConfigReapplyDetachesFromRequestCancellation(t *testing.T) {
	type reapplyCall struct {
		ctxErr      error
		orgID       int64
		userID      int64
		identifiers []string
	}
	reapplied := make(chan reapplyCall, 1)
	service := &Service{rigConfigReapplier: func(ctx context.Context, orgID, userID int64, identifiers []string) {
		reapplied <- reapplyCall{ctxErr: ctx.Err(), orgID: orgID, userID: userID, identifiers: identifiers}
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	service.reapplyRigConfigBestEffort(ctx, 42, 9, []string{"newly-paired"})

	select {
	case call := <-reapplied:
		require.Equal(t, int64(42), call.orgID)
		require.Equal(t, int64(9), call.userID)
		require.NoError(t, call.ctxErr)
		require.Equal(t, []string{"newly-paired"}, call.identifiers)
	case <-time.After(time.Second):
		t.Fatal("rig config reapply was not started")
	}
}

func TestPairingRigConfigReapplyIgnoresEmptyTargets(t *testing.T) {
	service := &Service{rigConfigReapplier: func(context.Context, int64, int64, []string) {
		t.Error("empty targets must not become an organization-wide request")
	}}
	service.reapplyRigConfigBestEffort(t.Context(), 42, 9, nil)
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

func TestResolveNetworkScanTargets_ExpandsLocalSubnetWithKnownSubnets(t *testing.T) {
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
		GetKnownSubnets(gomock.Any(), int64(42), 24, true).
		Return([]string{"192.168.25.0/24", "192.168.1.0/24", "not-a-cidr"}, nil)

	targets, err := service.resolveNetworkScanTargets(ctx, "192.168.1.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.1.0/24", "192.168.25.0/24"}, targets)
}

func TestResolveNetworkScanTargets_SkipsExpansionForNonLocalTargets(t *testing.T) {
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

	targets, err := service.resolveNetworkScanTargets(ctx, "192.168.25.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.25.0/24"}, targets)
}

func TestResolveNetworkScanTargets_FallsBackWhenLocalNetworkInfoFails(t *testing.T) {
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

	targets, err := service.resolveNetworkScanTargets(ctx, "192.168.1.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.1.0/24"}, targets)
}

func TestResolveNetworkScanTargets_DoesNotExpandIPv6Targets(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	service := &Service{
		deviceStore: mockDeviceStore,
		localNetworkInfo: func(context.Context) (*NetworkInfo, error) {
			return &NetworkInfo{NetworkInfo: networking.NetworkInfo{
				Subnet:     "192.168.1.0/24",
				IPv6Subnet: "fd00::/64",
			}}, nil
		},
	}

	ctx := mockSessionContext(t.Context(), 1, 42)

	// IPv6 targets should not be auto-expanded because IPv6 subnets are
	// too large for network sweeps.
	targets, err := service.resolveNetworkScanTargets(ctx, "fd00::/64")
	require.NoError(t, err)
	require.Equal(t, []string{"fd00::/64"}, targets)
}

func TestIsSameContainerUsesEndpointIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ip    string
		class commonv1.DeviceClass
		want  bool
	}{
		{"replacement controller at shared IP", "192.0.2.1", commonv1.DeviceClass_DEVICE_CLASS_CONTAINER, true},
		{"another container", "192.0.2.2", commonv1.DeviceClass_DEVICE_CLASS_CONTAINER, false},
		{"a miner at the old IP", "192.0.2.1", commonv1.DeviceClass_DEVICE_CLASS_RIG, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockDeviceStore(ctrl)
			store.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), "container", int64(1)).Return(&pb.Device{
				DeviceClass: commonv1.DeviceClass_DEVICE_CLASS_CONTAINER,
				DriverName:  "proto", Model: "CT1", IpAddress: "192.0.2.1", Port: "80", SerialNumber: "controller-a",
			}, nil)
			service := &Service{deviceStore: store}
			device := &discoverymodels.DiscoveredDevice{Device: pb.Device{
				DeviceClass: tc.class, DriverName: "proto", Model: "CT1",
				IpAddress: tc.ip, Port: "443", SerialNumber: "controller-b",
			}}
			require.Equal(t, tc.want, service.IsSameDevice(t.Context(), device, "container", 1))
		})
	}
}

func recoveryTestCredentials(t *testing.T) (*encrypt.Service, *pb.Credentials, *pb.Credentials) {
	t.Helper()
	service, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	username, err := service.Encrypt([]byte("admin"))
	require.NoError(t, err)
	password, err := service.Encrypt([]byte("existing-miner-password"))
	require.NoError(t, err)
	plainPassword := "existing-miner-password"
	return service, &pb.Credentials{Username: username, Password: &password}, &pb.Credentials{Username: "admin", Password: &plainPassword}
}
