package pairing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingMocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	pluginsdomain "github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockDiscoverer struct {
	mock.Mock
}

func (m *MockDiscoverer) Discover(ctx context.Context, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error) {
	args := m.Called(ctx, ipAddress, port)
	if args.Get(0) == nil {
		return nil, fmt.Errorf("discover error: %w", args.Error(1))
	}
	device, ok := args.Get(0).(*discoverymodels.DiscoveredDevice)
	if !ok {
		return nil, fmt.Errorf("unexpected type for device: %T", args.Get(0))
	}

	if err := args.Error(1); err != nil {
		return device, fmt.Errorf("discover error: %w", err)
	}

	return device, nil
}

var _ minerdiscovery.Discoverer = (*MockDiscoverer)(nil)

func setupTestService(t *testing.T, testContext *testutil.TestContext, adminUser *testutil.TestUser, pairer pairing.Pairer, mockDiscoverer *MockDiscoverer) (*pairing.Service, context.Context) {
	tokenService := testContext.ServiceProvider.TokenService
	ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	pluginService := testContext.ServiceProvider.PluginService

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockListener := pairingMocks.NewMockListener(ctrl)

	mockListener.EXPECT().AddDevices(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	if pairer == nil {
		pairer = testutil.NewMockProtoPairer(ctrl)
	}

	pairingService := pairing.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenService,
		mockDiscoverer,
		pluginService,
		mockListener,
		pairer,
	)

	return pairingService, ctx
}

func createMockDevice(ipAddress, port, deviceType string) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:  ipAddress,
			Port:       port,
			UrlScheme:  "http",
			DriverName: deviceType,
		},
	}
}

func registerDiscoveryPortsPlugin(t *testing.T, testContext *testutil.TestContext, driverName string, ports []string) {
	t.Helper()

	err := testContext.ServiceProvider.PluginService.GetManager().RegisterPluginForTest(&pluginsdomain.LoadedPlugin{
		Name: fmt.Sprintf("%s-plugin", driverName),
		Identifier: sdk.DriverIdentifier{
			DriverName: driverName,
			APIVersion: "v1",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: ports,
	})
	require.NoError(t, err)
}

// createPairRequest creates a PairRequest with the given device identifiers using DeviceSelector.
func createPairRequest(deviceIdentifiers []string) *pb.PairRequest {
	return &pb.PairRequest{
		DeviceSelector: &commandpb.DeviceSelector{
			SelectionType: &commandpb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIdentifiers,
				},
			},
		},
	}
}

// createPairRequestWithAllDevicesFilter creates a PairRequest with AllDevices selector and pairing status filter.
func createPairRequestWithAllDevicesFilter(pairingStatuses []fm.PairingStatus) *pb.PairRequest {
	return &pb.PairRequest{
		DeviceSelector: &commandpb.DeviceSelector{
			SelectionType: &commandpb.DeviceSelector_AllDevices{
				AllDevices: &commandpb.DeviceFilter{
					PairingStatus: pairingStatuses,
				},
			},
		},
	}
}

// TODO: setUpMockMinerServer should be reimplemented using plugin-based test infrastructure
// This functionality should be reimplemented using the proto plugin's integration test
// helpers (see plugin/proto/tests/integration) when needed for server integration testing.
func setUpMockMinerServer(t *testing.T) (string, string) {
	t.Skip("Disabled pending plugin-based test infrastructure")
	return "", ""
}

func TestDiscoverWithIPList(t *testing.T) {
	t.Run("discovers devices from IP list", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice1 := createMockDevice("192.168.1.10", "8080", "proto")
		mockDevice2 := createMockDevice("192.168.1.11", "8080", "antminer")

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice1, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.11", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice2, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPListModeRequest{
			IpAddresses: []string{"192.168.1.10", "192.168.1.11"},
			Ports:       []string{"8080"},
		}

		// Act
		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device

		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}

		discoverWg.Wait()

		// Assert
		mockDiscoverer.AssertExpectations(t)

		assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice1, mockDevice2})
	})
}

func TestDiscoverWithIPList_DerivesPortsFromPluginMetadata(t *testing.T) {
	mockDiscoverer := &MockDiscoverer{}
	mockDevice := createMockDevice("192.168.1.10", "443", "proto")
	mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "443").Return(mockDevice, nil).Once()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	registerDiscoveryPortsPlugin(t, testContext, "proto", []string{"443"})

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}

	mockDiscoverer.AssertExpectations(t)
	assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice})
}

func TestDiscoverWithIPList_UsesAllAdvertisedPluginPortsByDefault(t *testing.T) {
	// Arrange
	mockDiscoverer := &MockDiscoverer{}
	mockDevice := createMockDevice("192.168.1.10", "443", "proto")
	mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "443").Return(mockDevice, nil).Once()
	// Both ports are probed concurrently; "8080" may be called before "443"'s success cancels it.
	mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Return(nil, context.Canceled).Maybe()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	registerDiscoveryPortsPlugin(t, testContext, "proto", []string{"443", "8080"})

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	// Act
	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}

	// Assert
	mockDiscoverer.AssertExpectations(t)
	assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice})
}

func TestDiscoverWithIPList_ExplicitPortsOverridePluginMetadata(t *testing.T) {
	mockDiscoverer := &MockDiscoverer{}
	mockDevice := createMockDevice("192.168.1.10", "8080", "proto")
	mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Return(mockDevice, nil).Once()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	registerDiscoveryPortsPlugin(t, testContext, "proto", []string{"443"})

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"},
		Ports:       []string{"8080"},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}

	mockDiscoverer.AssertExpectations(t)
	assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice})
}

func TestDiscoverWithIPList_CancelsRemainingPortsAfterFirstSuccessForSameIP(t *testing.T) {
	// Arrange
	mockDiscoverer := &MockDiscoverer{}
	scannedIP := "192.168.1.10"
	firstPort := "443"
	secondPort := "4028"

	firstDiscoveryDevice := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    scannedIP,
			Port:         firstPort,
			UrlScheme:    "https",
			DriverName:   "asicrs",
			Model:        "M60S",
			Manufacturer: "WhatsMiner",
		},
	}

	// Both ports are probed concurrently; the second may or may not be called depending on
	// whether the cancellation propagates before it starts.
	mockDiscoverer.On("Discover", mock.Anything, scannedIP, firstPort).Return(firstDiscoveryDevice, nil).Once()
	mockDiscoverer.On("Discover", mock.Anything, scannedIP, secondPort).Return(nil, context.Canceled).Maybe()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	registerDiscoveryPortsPlugin(t, testContext, "asicrs", []string{firstPort, secondPort})

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	// Act
	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{scannedIP},
		Ports:       []string{firstPort, secondPort},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}

	// Assert
	mockDiscoverer.AssertExpectations(t)
	require.Len(t, devices, 1)
	assert.NotEmpty(t, devices[0].DeviceIdentifier)
}

func TestDiscoverWithIPList_ContinuesScanAfterCollisionSkip(t *testing.T) {
	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	scannedIP := "192.168.1.50"
	port1 := "4028"
	port2 := "443"
	mac := "AA:BB:CC:DD:EE:FF"

	// Device Y is paired at scannedIP:port1 — the "occupying" device.
	// Device X is paired at another IP with MAC=mac.
	// When scannedIP:port1 returns a device with mac=mac, reconcileByMAC finds X,
	// but Y occupies scannedIP:port1 → collision skip → (false, nil).
	// The scan must continue to port2, which has no occupying device and succeeds.
	queries := sqlc.New(testContext.ServiceProvider.DB)

	yIdentifier := "device-y-collision-test"
	yDiscoveredID, err := queries.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            adminUser.OrganizationID,
		DeviceIdentifier: yIdentifier,
		IpAddress:        scannedIP,
		Port:             port1,
		UrlScheme:        "http",
		IsActive:         true,
		DriverName:       "asicrs",
	})
	require.NoError(t, err)
	yDbID, err := queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              adminUser.OrganizationID,
		DiscoveredDeviceID: yDiscoveredID,
		DeviceIdentifier:   yIdentifier,
	})
	require.NoError(t, err)
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      yDbID,
		PairingStatus: sqlc.PairingStatusEnumPAIRED,
	})
	require.NoError(t, err)

	xIdentifier := "device-x-collision-test"
	xDiscoveredID, err := queries.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            adminUser.OrganizationID,
		DeviceIdentifier: xIdentifier,
		IpAddress:        "192.168.1.99",
		Port:             port1,
		UrlScheme:        "http",
		IsActive:         true,
		DriverName:       "asicrs",
	})
	require.NoError(t, err)
	xDbID, err := queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              adminUser.OrganizationID,
		DiscoveredDeviceID: xDiscoveredID,
		DeviceIdentifier:   xIdentifier,
		MacAddress:         mac,
	})
	require.NoError(t, err)
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      xDbID,
		PairingStatus: sqlc.PairingStatusEnumPAIRED,
	})
	require.NoError(t, err)

	mockDiscoverer := &MockDiscoverer{}
	deviceWithMAC := func(port string) *discoverymodels.DiscoveredDevice {
		return &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				IpAddress:  scannedIP,
				Port:       port,
				UrlScheme:  "http",
				DriverName: "asicrs",
				MacAddress: mac,
			},
		}
	}
	mockDiscoverer.On("Discover", mock.Anything, scannedIP, port1).Return(deviceWithMAC(port1), nil).Once()
	mockDiscoverer.On("Discover", mock.Anything, scannedIP, port2).Return(deviceWithMAC(port2), nil).Once()

	registerDiscoveryPortsPlugin(t, testContext, "asicrs", []string{port1, port2})
	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	// Act
	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{scannedIP},
		Ports:       []string{port1, port2},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}

	// Assert: port1 collision-skipped, scan continued to port2 and succeeded
	mockDiscoverer.AssertExpectations(t)
	require.Len(t, devices, 1, "port2 should be tried after port1 collision skip")
	assert.Equal(t, xIdentifier, devices[0].DeviceIdentifier)
}

func TestDiscoverWithIPRange_ContinuesScanAfterCollisionSkip(t *testing.T) {
	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	scannedIP := "192.168.1.51"
	port1 := "4028"
	port2 := "443"
	mac := "BB:CC:DD:EE:FF:AA"

	queries := sqlc.New(testContext.ServiceProvider.DB)

	yIdentifier := "device-y-range-collision-test"
	yDiscoveredID, err := queries.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            adminUser.OrganizationID,
		DeviceIdentifier: yIdentifier,
		IpAddress:        scannedIP,
		Port:             port1,
		UrlScheme:        "http",
		IsActive:         true,
		DriverName:       "asicrs",
	})
	require.NoError(t, err)
	yDbID, err := queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              adminUser.OrganizationID,
		DiscoveredDeviceID: yDiscoveredID,
		DeviceIdentifier:   yIdentifier,
	})
	require.NoError(t, err)
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      yDbID,
		PairingStatus: sqlc.PairingStatusEnumPAIRED,
	})
	require.NoError(t, err)

	xIdentifier := "device-x-range-collision-test"
	xDiscoveredID, err := queries.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            adminUser.OrganizationID,
		DeviceIdentifier: xIdentifier,
		IpAddress:        "192.168.1.98",
		Port:             port1,
		UrlScheme:        "http",
		IsActive:         true,
		DriverName:       "asicrs",
	})
	require.NoError(t, err)
	xDbID, err := queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID:              adminUser.OrganizationID,
		DiscoveredDeviceID: xDiscoveredID,
		DeviceIdentifier:   xIdentifier,
		MacAddress:         mac,
	})
	require.NoError(t, err)
	_, err = queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID:      xDbID,
		PairingStatus: sqlc.PairingStatusEnumPAIRED,
	})
	require.NoError(t, err)

	mockDiscoverer := &MockDiscoverer{}
	deviceWithMAC := func(port string) *discoverymodels.DiscoveredDevice {
		return &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				IpAddress:  scannedIP,
				Port:       port,
				UrlScheme:  "http",
				DriverName: "asicrs",
				MacAddress: mac,
			},
		}
	}
	mockDiscoverer.On("Discover", mock.Anything, scannedIP, port1).Return(deviceWithMAC(port1), nil).Once()
	mockDiscoverer.On("Discover", mock.Anything, scannedIP, port2).Return(deviceWithMAC(port2), nil).Once()

	registerDiscoveryPortsPlugin(t, testContext, "asicrs", []string{port1, port2})
	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	// Act
	resultChan, err := pairingService.DiscoverWithIPRange(ctx, &pb.IPRangeModeRequest{
		StartIp: scannedIP,
		EndIp:   scannedIP,
		Ports:   []string{port1, port2},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}

	// Assert: port1 collision-skipped, scan continued to port2 and succeeded
	mockDiscoverer.AssertExpectations(t)
	require.Len(t, devices, 1, "port2 should be tried after port1 collision skip")
	assert.Equal(t, xIdentifier, devices[0].DeviceIdentifier)
}

func TestDiscoverWithIPList_EmptyPortsWithoutMetadataReturnsError(t *testing.T) {
	mockDiscoverer := &MockDiscoverer{}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"},
	})
	require.Error(t, err)
	require.Nil(t, resultChan)
	assert.Contains(t, err.Error(), "no discovery ports were provided")
	mockDiscoverer.AssertNotCalled(t, "Discover", mock.Anything, mock.Anything, mock.Anything)
}

func TestDiscoverWithIPList_ResolvesHostnameToIP(t *testing.T) {
	mockDiscoverer := &MockDiscoverer{}
	mockDevice := createMockDevice("127.0.0.1", "8080", "proto")
	mockDiscoverer.On("Discover", mock.Anything, "127.0.0.1", "8080").Return(mockDevice, nil).Once()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	registerDiscoveryPortsPlugin(t, testContext, "proto", []string{"8080"})

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"localhost"},
		Ports:       []string{"8080"},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		devices = append(devices, result.Devices...)
	}

	mockDiscoverer.AssertCalled(t, "Discover", mock.Anything, "127.0.0.1", "8080")
	mockDiscoverer.AssertNotCalled(t, "Discover", mock.Anything, "localhost", "8080")
}

func TestDiscoverWithIPList_SkipsUnresolvableHostnames(t *testing.T) {
	mockDiscoverer := &MockDiscoverer{}
	mockDevice := createMockDevice("192.168.1.10", "8080", "proto")
	mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Return(mockDevice, nil).Once()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	registerDiscoveryPortsPlugin(t, testContext, "proto", []string{"8080"})

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10", "this-host-definitely-does-not-exist.invalid"},
		Ports:       []string{"8080"},
	})
	require.NoError(t, err)

	var devices []*pb.Device
	for result := range resultChan {
		devices = append(devices, result.Devices...)
	}

	assert.Len(t, devices, 1)
	assert.Equal(t, "192.168.1.10", devices[0].IpAddress)
	mockDiscoverer.AssertNotCalled(t, "Discover", mock.Anything, "this-host-definitely-does-not-exist.invalid", mock.Anything)
}

func TestDiscoverWithIPList_TooManyPortsReturnsError(t *testing.T) {
	// Arrange
	mockDiscoverer := &MockDiscoverer{}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)
	ports := make([]string, pairing.MaxPortsPerIP+1)
	for i := range ports {
		ports[i] = fmt.Sprintf("%d", 4000+i)
	}

	// Act
	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"},
		Ports:       ports,
	})

	// Assert
	require.Error(t, err)
	require.Nil(t, resultChan)
	mockDiscoverer.AssertNotCalled(t, "Discover", mock.Anything, mock.Anything, mock.Anything)
}

func TestDiscoverWithIPRange_TooManyPortsReturnsError(t *testing.T) {
	// Arrange
	mockDiscoverer := &MockDiscoverer{}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)
	ports := make([]string, pairing.MaxPortsPerIP+1)
	for i := range ports {
		ports[i] = fmt.Sprintf("%d", 4000+i)
	}

	// Act
	resultChan, err := pairingService.DiscoverWithIPRange(ctx, &pb.IPRangeModeRequest{
		StartIp: "192.168.1.10",
		EndIp:   "192.168.1.10",
		Ports:   ports,
	})

	// Assert
	require.Error(t, err)
	require.Nil(t, resultChan)
	mockDiscoverer.AssertNotCalled(t, "Discover", mock.Anything, mock.Anything, mock.Anything)
}

func TestDiscoverWithIPRange_EmptyPortsWithoutMetadataReturnsError(t *testing.T) {
	mockDiscoverer := &MockDiscoverer{}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	resultChan, err := pairingService.DiscoverWithIPRange(ctx, &pb.IPRangeModeRequest{
		StartIp: "192.168.1.10",
		EndIp:   "192.168.1.10",
	})
	require.Error(t, err)
	require.Nil(t, resultChan)
	assert.Contains(t, err.Error(), "no discovery ports were provided")
	mockDiscoverer.AssertNotCalled(t, "Discover", mock.Anything, mock.Anything, mock.Anything)
}

func TestDiscoverWithIPRange(t *testing.T) {
	t.Run("discovers devices in IP range", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(3)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice1 := createMockDevice("192.168.1.10", "8080", "proto")
		mockDevice2 := createMockDevice("192.168.1.11", "8080", "proto")
		mockDevice3 := createMockDevice("192.168.1.12", "8080", "antminer")

		// Set up mock calls that signal completion through WaitGroup
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice1, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.11", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice2, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.12", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice3, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPRangeModeRequest{
			StartIp: "192.168.1.10",
			EndIp:   "192.168.1.12",
			Ports:   []string{"8080"},
		}

		// Act
		resultChan, err := pairingService.DiscoverWithIPRange(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device

		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}

		discoverWg.Wait()

		// Assert
		mockDiscoverer.AssertExpectations(t)

		assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice1, mockDevice2, mockDevice3})
	})

	t.Run("supports updates to existing devices", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(6)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice1 := createMockDevice("192.168.1.10", "8080", "proto")
		mockDevice2 := createMockDevice("192.168.1.11", "8080", "proto")
		mockDevice3 := createMockDevice("192.168.1.12", "8080", "antminer")

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice1, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.11", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice2, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.12", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice3, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPRangeModeRequest{
			StartIp: "192.168.1.10",
			EndIp:   "192.168.1.12",
			Ports:   []string{"8080"},
		}
		resultChan, err := pairingService.DiscoverWithIPRange(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 3)

		// Device IPs now change

		mockDevice1.IpAddress = "192.168.1.11"
		mockDevice2.IpAddress = "192.168.1.12"
		mockDevice3.IpAddress = "192.168.1.10"

		// Act
		resultChan, err = pairingService.DiscoverWithIPRange(ctx, request)
		require.NoError(t, err)

		devices = []*pb.Device{}
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}

		discoverWg.Wait()

		// Assert
		mockDiscoverer.AssertExpectations(t)

		assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice1, mockDevice2, mockDevice3})
	})

	t.Run("does not lead to duplicate device pairings", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host, portStr := setUpMockMinerServer(t)

		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "proto")

		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPRangeModeRequest{
			StartIp: host,
			EndIp:   host,
			Ports:   []string{portStr},
		}
		resultChan, err := pairingService.DiscoverWithIPRange(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Act
		resultChan, err = pairingService.DiscoverWithIPRange(ctx, request)
		require.NoError(t, err)
		_, err = pairingService.PairDevices(ctx, createPairRequest([]string{devices[0].DeviceIdentifier}))
		require.NoError(t, err)

		devices = []*pb.Device{}
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}

		discoverWg.Wait()

		// Assert
		mockDiscoverer.AssertExpectations(t)

		assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice})

		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 100)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})

	t.Run("handles discovery failures in IP range", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice("192.168.1.20", "8080", "proto")

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.20", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.21", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(nil, assert.AnError)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPRangeModeRequest{
			StartIp: "192.168.1.20",
			EndIp:   "192.168.1.21",
			Ports:   []string{"8080"},
		}

		// Act
		resultChan, err := pairingService.DiscoverWithIPRange(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device

		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}

		discoverWg.Wait()

		// Assert
		mockDiscoverer.AssertExpectations(t)

		assertDevicesEqual(t, devices, []*discoverymodels.DiscoveredDevice{mockDevice})
	})
}

func TestPairDevices(t *testing.T) {
	t.Run("saves devices that require credentials as AUTHENTICATION_NEEDED", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host := "192.168.1.100"
		portStr := "80"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "antminer")
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns credentials required error
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockAntminerPairer := pairingMocks.NewMockPairer(ctrl)
		mockAntminerPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(fmt.Errorf("invalid_argument: credentials are required but were not provided"))

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockAntminerPairer, mockDiscoverer)

		// First discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Now pair the device
		pairRequest := createPairRequest([]string{devices[0].DeviceIdentifier})

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify device pairing status is AUTHENTICATION_NEEDED
		queries := sqlc.New(testContext.ServiceProvider.DB)
		deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, devices[0].DeviceIdentifier)
		require.NoError(t, err)

		pairingStatus, err := queries.GetDevicePairingStatusByDeviceDatabaseID(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, pairing.StatusAuthenticationNeeded, string(pairingStatus), "device pairing status should be AUTHENTICATION_NEEDED")

	})

	t.Run("pairs proto device successfully without credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host, portStr := setUpMockMinerServer(t)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "proto")
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		// First discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Now pair the device
		pairRequest := createPairRequest([]string{devices[0].DeviceIdentifier})

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify device is active after pairing
		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
		orgDeviceID := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: devices[0].DeviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		}
		discoveredDevice, err := discoveredDeviceStore.GetDevice(ctx, orgDeviceID)
		require.NoError(t, err)
		assert.True(t, discoveredDevice.IsActive, "discovered device should be active after pairing")

		// Verify pairing was successful
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})

	t.Run("fails to pair unsupported device type", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

		// Create a service with no pairers registered
		tokenService := testContext.ServiceProvider.TokenService
		mockDiscoverer := &MockDiscoverer{}
		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
		transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
		deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
		pluginService := testContext.ServiceProvider.PluginService

		pairingService := pairing.NewService(
			discoveredDeviceStore,
			deviceStore,
			transactor,
			tokenService,
			mockDiscoverer,
			pluginService,
			nil,
			nil,
		)

		// Try to pair a non-existent device (this will fail at device lookup)
		pairRequest := createPairRequest([]string{"unsupported-device-001"})

		_, err := pairingService.PairDevices(ctx, pairRequest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to pair any devices")
	})

	t.Run("handles device not found error", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		// Try to pair a non-existent device
		pairRequest := createPairRequest([]string{"non-existent-device"})

		_, err := pairingService.PairDevices(ctx, pairRequest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to pair any devices")
	})

	t.Run("pairs miners even if one of them fails", func(t *testing.T) {
		host, portStr := setUpMockMinerServer(t)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "proto")
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// send pairing request with one valid and one invalid device identifier
		pairRequest := createPairRequest([]string{devices[0].DeviceIdentifier, "test-invalid-device"})

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify pairing was successful for the valid device
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})

	t.Run("treats already paired key rotation auth failures as idempotent without credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)
		queries := sqlc.New(testContext.ServiceProvider.DB)

		deviceIdentifier := "paired-device-001"
		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
		_, err := discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		}, &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceIdentifier,
				IpAddress:        "172.16.21.149",
				Port:             "443",
				UrlScheme:        "https",
				DriverName:       "proto",
			},
			OrgID:    adminUser.OrganizationID,
			IsActive: true,
		})
		require.NoError(t, err)

		discoveredDevice, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		})
		require.NoError(t, err)

		_, err = queries.InsertDevice(ctx, sqlc.InsertDeviceParams{
			OrgID:              adminUser.OrganizationID,
			DiscoveredDeviceID: discoveredDevice.ID,
			DeviceIdentifier:   deviceIdentifier,
			MacAddress:         "C8:98:DB:10:D1:94",
		})
		require.NoError(t, err)

		deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, deviceIdentifier)
		require.NoError(t, err)

		_, err = queries.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
			DeviceID:      deviceID,
			PairingStatus: sqlc.PairingStatusEnumPAIRED,
		})
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockPairer := pairingMocks.NewMockPairer(ctrl)
		mockPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(fmt.Errorf("rpc error: code = Unknown desc = pairing failed: unauthenticated: device is already paired and requires valid credentials for key rotation"))

		tokenService := testContext.ServiceProvider.TokenService
		transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
		deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
		pluginService := testContext.ServiceProvider.PluginService
		mockListener := pairingMocks.NewMockListener(ctrl)
		mockListener.EXPECT().AddDevices(gomock.Any(), tmodels.DeviceIdentifier(deviceIdentifier)).Return(nil)

		pairingService := pairing.NewService(
			discoveredDeviceStore,
			deviceStore,
			transactor,
			tokenService,
			&MockDiscoverer{},
			pluginService,
			mockListener,
			mockPairer,
		)

		resp, err := pairingService.PairDevices(ctx, createPairRequest([]string{deviceIdentifier}))
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.FailedDeviceIds)
	})

	t.Run("does not schedule telemetry for already known devices that are not fully paired", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)
		queries := sqlc.New(testContext.ServiceProvider.DB)

		deviceIdentifier := "auth-needed-device-001"
		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
		_, err := discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		}, &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceIdentifier,
				IpAddress:        "172.16.21.150",
				Port:             "443",
				UrlScheme:        "https",
				DriverName:       "proto",
			},
			OrgID:    adminUser.OrganizationID,
			IsActive: true,
		})
		require.NoError(t, err)

		discoveredDevice, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		})
		require.NoError(t, err)

		_, err = queries.InsertDevice(ctx, sqlc.InsertDeviceParams{
			OrgID:              adminUser.OrganizationID,
			DiscoveredDeviceID: discoveredDevice.ID,
			DeviceIdentifier:   deviceIdentifier,
			MacAddress:         "C8:98:DB:10:D1:95",
		})
		require.NoError(t, err)

		deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, deviceIdentifier)
		require.NoError(t, err)

		_, err = queries.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
			DeviceID:      deviceID,
			PairingStatus: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED,
		})
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockPairer := pairingMocks.NewMockPairer(ctrl)
		mockPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(fmt.Errorf("rpc error: code = Unknown desc = pairing failed: unauthenticated: device is already paired and requires valid credentials for key rotation"))

		tokenService := testContext.ServiceProvider.TokenService
		transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
		deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
		pluginService := testContext.ServiceProvider.PluginService
		mockListener := pairingMocks.NewMockListener(ctrl)

		pairingService := pairing.NewService(
			discoveredDeviceStore,
			deviceStore,
			transactor,
			tokenService,
			&MockDiscoverer{},
			pluginService,
			mockListener,
			mockPairer,
		)

		resp, err := pairingService.PairDevices(ctx, createPairRequest([]string{deviceIdentifier}))
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.FailedDeviceIds)

		pairingStatus, err := queries.GetDevicePairingStatusByDeviceDatabaseID(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, pairing.StatusAuthenticationNeeded, string(pairingStatus))
	})

	t.Run("marks externally paired unpaired devices as AUTHENTICATION_NEEDED", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)
		queries := sqlc.New(testContext.ServiceProvider.DB)

		deviceIdentifier := "ext-paired-unpaired-device-001"
		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
		_, err := discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		}, &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceIdentifier,
				IpAddress:        "172.16.21.151",
				Port:             "443",
				UrlScheme:        "https",
				DriverName:       "proto",
			},
			OrgID:    adminUser.OrganizationID,
			IsActive: true,
		})
		require.NoError(t, err)

		discoveredDevice, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		})
		require.NoError(t, err)

		_, err = queries.InsertDevice(ctx, sqlc.InsertDeviceParams{
			OrgID:              adminUser.OrganizationID,
			DiscoveredDeviceID: discoveredDevice.ID,
			DeviceIdentifier:   deviceIdentifier,
			MacAddress:         "C8:98:DB:10:D1:96",
		})
		require.NoError(t, err)

		deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, deviceIdentifier)
		require.NoError(t, err)

		_, err = queries.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
			DeviceID:      deviceID,
			PairingStatus: sqlc.PairingStatusEnumUNPAIRED,
		})
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockPairer := pairingMocks.NewMockPairer(ctrl)
		mockPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(fmt.Errorf("rpc error: code = Unknown desc = pairing failed: unauthenticated: device is already paired and requires valid credentials for key rotation"))

		tokenService := testContext.ServiceProvider.TokenService
		transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
		deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
		pluginService := testContext.ServiceProvider.PluginService
		mockListener := pairingMocks.NewMockListener(ctrl)

		pairingService := pairing.NewService(
			discoveredDeviceStore,
			deviceStore,
			transactor,
			tokenService,
			&MockDiscoverer{},
			pluginService,
			mockListener,
			mockPairer,
		)

		resp, err := pairingService.PairDevices(ctx, createPairRequest([]string{deviceIdentifier}))
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.FailedDeviceIds)

		pairingStatus, err := queries.GetDevicePairingStatusByDeviceDatabaseID(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, pairing.StatusAuthenticationNeeded, string(pairingStatus))
	})
}

func assertDevicesEqual(t *testing.T, actual []*pb.Device, expected []*discoverymodels.DiscoveredDevice) {
	require.Len(t, actual, len(expected))

	expectedDevicesMap := make(map[string]*pb.Device)
	for _, device := range expected {
		key := fmt.Sprintf("%s-%s", device.DriverName, device.IpAddress)
		expectedDevicesMap[key] = &device.Device
	}

	actualDevicesMap := make(map[string]*pb.Device)
	for _, device := range actual {
		key := fmt.Sprintf("%s-%s", device.DriverName, device.IpAddress)
		actualDevicesMap[key] = device
	}

	assert.Equal(t, stripIdentifier(expectedDevicesMap), stripIdentifier(actualDevicesMap))
}

func stripIdentifier(m map[string]*pb.Device) map[string]*pb.Device {
	out := make(map[string]*pb.Device, len(m))
	for k, d := range m {
		clone := proto.Clone(d)
		c, ok := clone.(*pb.Device)
		if !ok {
			panic(fmt.Sprintf("expected *pb.Device from proto.Clone, got %T", clone))
		}
		c.DeviceIdentifier = ""
		if isEmptyMinerCapabilities(c.Capabilities) {
			c.Capabilities = nil
		}
		out[k] = c
	}
	return out
}

func isEmptyMinerCapabilities(caps *capabilitiespb.MinerCapabilities) bool {
	if caps == nil {
		return true
	}

	return caps.Manufacturer == "" &&
		isEmptyAuthenticationCapabilities(caps.Authentication) &&
		isEmptyCommandCapabilities(caps.Commands) &&
		isEmptyTelemetryCapabilities(caps.Telemetry) &&
		isEmptyFirmwareCapabilities(caps.Firmware)
}

func isEmptyAuthenticationCapabilities(caps *capabilitiespb.AuthenticationCapabilities) bool {
	return caps == nil || len(caps.SupportedMethods) == 0
}

func isEmptyCommandCapabilities(caps *capabilitiespb.CommandCapabilities) bool {
	return caps == nil ||
		(!caps.RebootSupported &&
			!caps.MiningStartSupported &&
			!caps.MiningStopSupported &&
			!caps.LedBlinkSupported &&
			!caps.FactoryResetSupported &&
			!caps.AirCoolingSupported &&
			!caps.ImmersionCoolingSupported &&
			!caps.PoolSwitchingSupported &&
			caps.PoolMaxCount == 0 &&
			!caps.PoolPrioritySupported &&
			!caps.LogsDownloadSupported &&
			!caps.PowerModeEfficiencySupported &&
			!caps.UpdateMinerPasswordSupported)
}

func isEmptyTelemetryCapabilities(caps *capabilitiespb.TelemetryCapabilities) bool {
	return caps == nil ||
		(!caps.RealtimeTelemetrySupported &&
			!caps.HistoricalDataSupported &&
			!caps.HashrateReported &&
			!caps.PowerUsageReported &&
			!caps.TemperatureReported &&
			!caps.FanSpeedReported &&
			!caps.EfficiencyReported &&
			!caps.UptimeReported &&
			!caps.ErrorCountReported &&
			!caps.MinerStatusReported &&
			!caps.PoolStatsReported &&
			!caps.PerChipStatsReported &&
			!caps.PerBoardStatsReported &&
			!caps.PsuStatsReported)
}

func isEmptyFirmwareCapabilities(caps *capabilitiespb.FirmwareCapabilities) bool {
	return caps == nil || (!caps.OtaUpdateSupported && !caps.ManualUploadSupported)
}

func TestPairDevices_SavesFirmwareVersion(t *testing.T) {
	t.Run("saves firmware version after successful pairing", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host := "192.168.1.100"
		portStr := "8080"
		expectedFirmwareVersion := "1.2.3"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "proto")
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns successful pairing
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockProtoPairer := pairingMocks.NewMockPairer(ctrl)

		// Mock successful PairDevice call
		mockProtoPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(nil)
		// Mock GetDeviceInfo call that returns device info with firmware version
		mockProtoPairer.EXPECT().
			GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, credentials any) (*pb.Device, error) {
				// Return device with firmware version set
				return &pb.Device{
					DeviceIdentifier: discoveredDevice.DeviceIdentifier,
					FirmwareVersion:  expectedFirmwareVersion,
				}, nil
			})

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockProtoPairer, mockDiscoverer)

		// First discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Now pair the device
		pairRequest := createPairRequest([]string{devices[0].DeviceIdentifier})

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify firmware version was saved to discovered_device table
		queries := sqlc.New(testContext.ServiceProvider.DB)
		discoveredDevice, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
			DeviceIdentifier: devices[0].DeviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		})
		require.NoError(t, err)
		assert.True(t, discoveredDevice.FirmwareVersion.Valid, "firmware_version should be set")
		assert.Equal(t, expectedFirmwareVersion, discoveredDevice.FirmwareVersion.String, "firmware version should match")
	})

	t.Run("handles missing firmware version gracefully", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host := "192.168.1.101"
		portStr := "8080"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "proto")
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns successful pairing but no firmware
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockProtoPairer := pairingMocks.NewMockPairer(ctrl)

		mockProtoPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(nil)

		// Mock GetDeviceInfo that returns error (firmware unavailable)
		mockProtoPairer.EXPECT().
			GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("failed to get device info"))

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockProtoPairer, mockDiscoverer)

		// Discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Pair the device
		pairRequest := createPairRequest([]string{devices[0].DeviceIdentifier})

		// Pairing should still succeed even if GetDeviceInfo fails
		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify device was paired successfully but firmware_version is NULL
		queries := sqlc.New(testContext.ServiceProvider.DB)
		discoveredDevice, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
			DeviceIdentifier: devices[0].DeviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		})
		require.NoError(t, err)
		assert.False(t, discoveredDevice.FirmwareVersion.Valid, "firmware_version should be NULL when unavailable")
	})

	t.Run("preserves firmware learned during pairing when post-pair device info omits it", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host := "192.168.1.102"
		portStr := "8080"
		discoveredFirmwareVersion := "9.9.9"
		pairedFirmwareVersion := "1.2.3"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "proto")
		mockDevice.FirmwareVersion = discoveredFirmwareVersion
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockProtoPairer := pairingMocks.NewMockPairer(ctrl)

		mockProtoPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			DoAndReturn(func(_ context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
				discoveredDevice.FirmwareVersion = pairedFirmwareVersion
				return nil
			})
		// GetDeviceInfo is not called because PairDevice already set FirmwareVersion.

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockProtoPairer, mockDiscoverer)

		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		_, err = pairingService.PairDevices(ctx, createPairRequest([]string{devices[0].DeviceIdentifier}))
		require.NoError(t, err)

		queries := sqlc.New(testContext.ServiceProvider.DB)
		discoveredDevice, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
			DeviceIdentifier: devices[0].DeviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		})
		require.NoError(t, err)
		require.True(t, discoveredDevice.FirmwareVersion.Valid, "firmware_version should be preserved when pairing already learned it")
		assert.Equal(t, pairedFirmwareVersion, discoveredDevice.FirmwareVersion.String, "firmware_version should preserve the value learned during pairing")
	})
}

func TestPairDevices_AllDevices_WithAuthNeededFilter(t *testing.T) {
	t.Run("pairs all devices with AUTHENTICATION_NEEDED status", func(t *testing.T) {
		// Arrange
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host := "192.168.1.100"
		portStr := "80"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, "antminer")
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns credentials required error (sets AUTHENTICATION_NEEDED)
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockAntminerPairer := pairingMocks.NewMockPairer(ctrl)
		mockAntminerPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(fmt.Errorf("invalid_argument: credentials are required but were not provided"))

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockAntminerPairer, mockDiscoverer)

		// Discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{host},
			Ports:       []string{portStr},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// First pairing attempt - should set AUTHENTICATION_NEEDED
		pairRequest := createPairRequest([]string{devices[0].DeviceIdentifier})
		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify device is AUTHENTICATION_NEEDED
		queries := sqlc.New(testContext.ServiceProvider.DB)
		deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, devices[0].DeviceIdentifier)
		require.NoError(t, err)

		pairingStatus, err := queries.GetDevicePairingStatusByDeviceDatabaseID(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, pairing.StatusAuthenticationNeeded, string(pairingStatus))

		// Now set up mock to succeed with credentials
		// The mock needs to update pairing status to PAIRED (like the real plugin pairer does)
		mockAntminerPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, device *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
				// Simulate what the real plugin pairer does: set status to PAIRED
				_, err := queries.UpdateDevicePairingStatusByIdentifier(ctx, sqlc.UpdateDevicePairingStatusByIdentifierParams{
					PairingStatus:    sqlc.PairingStatusEnumPAIRED,
					DeviceIdentifier: device.DeviceIdentifier,
				})
				return err
			})
		mockAntminerPairer.EXPECT().
			GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("not implemented"))

		// Act: Use AllDevices selector with AUTHENTICATION_NEEDED filter
		allDevicesRequest := createPairRequestWithAllDevicesFilter([]fm.PairingStatus{fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED})
		allDevicesRequest.Credentials = &pb.Credentials{
			Username: "admin",
			Password: proto.String("password"),
		}

		_, err = pairingService.PairDevices(ctx, allDevicesRequest)

		// Assert
		require.NoError(t, err)

		// Verify device is now PAIRED
		pairingStatus, err = queries.GetDevicePairingStatusByDeviceDatabaseID(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, pairing.StatusPaired, string(pairingStatus))
	})
}

func TestDiscoveryReconciliation_SubnetMigration(t *testing.T) {
	t.Run("re-discovery on new subnet reconciles with existing paired device by MAC", func(t *testing.T) {
		// Scenario: A device was paired at 172.16.21.10, then the network moves it to 172.16.25.10.
		// Re-discovering should update the existing discovered_device record's IP rather than
		// creating a duplicate, allowing the device to come back online without re-pairing.
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		queries := sqlc.New(testContext.ServiceProvider.DB)

		oldIP := "172.16.21.10"
		newIP := "172.16.25.10"
		port := "8080"
		mac := "AA:BB:CC:DD:EE:01"
		normalizedMAC := "AA:BB:CC:DD:EE:01"

		// Create mock discoverer that returns device with MAC address
		mockDiscoverer := &MockDiscoverer{}
		mockDevice := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				IpAddress:       oldIP,
				Port:            port,
				UrlScheme:       "http",
				DriverName:      "proto",
				MacAddress:      mac,
				FirmwareVersion: "1.2.3",
			},
		}
		mockDiscoverer.On("Discover", mock.Anything, oldIP, port).Return(mockDevice, nil)

		// Create mock pairer that inserts device into DB (mimics real handlePairViaStore)
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockPairer := pairingMocks.NewMockPairer(ctrl)
		mockPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, device *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
				device.MacAddress = normalizedMAC
				device.SerialNumber = "SN-001"
				// Insert device into DB like the real pairer does
				dd, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
					DeviceIdentifier: device.DeviceIdentifier,
					OrgID:            adminUser.OrganizationID,
				})
				if err != nil {
					return err
				}
				_, err = queries.InsertDevice(ctx, sqlc.InsertDeviceParams{
					OrgID:              adminUser.OrganizationID,
					DiscoveredDeviceID: dd.ID,
					DeviceIdentifier:   device.DeviceIdentifier,
					MacAddress:         normalizedMAC,
				})
				if err != nil {
					return err
				}
				deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, device.DeviceIdentifier)
				if err != nil {
					return err
				}
				_, err = queries.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
					DeviceID:      deviceID,
					PairingStatus: sqlc.PairingStatusEnumPAIRED,
				})
				return err
			}).AnyTimes()
		mockPairer.EXPECT().
			GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("not implemented")).AnyTimes()

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockPairer, mockDiscoverer)

		// Step 1: Discover device at old IP
		request := &pb.IPListModeRequest{
			IpAddresses: []string{oldIP},
			Ports:       []string{port},
		}
		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)
		originalDeviceIdentifier := devices[0].DeviceIdentifier

		// Step 2: Pair the device
		_, err = pairingService.PairDevices(ctx, createPairRequest([]string{originalDeviceIdentifier}))
		require.NoError(t, err)

		totalPaired, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 100)
		require.NoError(t, err)
		require.Equal(t, 1, totalPaired, "should have 1 paired device")

		// Step 3: Now the device moves to a new subnet.
		// Re-discover it at the new IP (same MAC returned by discoverer).
		mockDeviceNewIP := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				IpAddress:  newIP,
				Port:       port,
				UrlScheme:  "http",
				DriverName: "proto",
				MacAddress: mac,
			},
		}
		mockDiscoverer.On("Discover", mock.Anything, newIP, port).Return(mockDeviceNewIP, nil)

		request2 := &pb.IPListModeRequest{
			IpAddresses: []string{newIP},
			Ports:       []string{port},
		}
		resultChan2, err := pairingService.DiscoverWithIPList(ctx, request2)
		require.NoError(t, err)

		var devicesNewIP []*pb.Device
		for result := range resultChan2 {
			devicesNewIP = append(devicesNewIP, result.Devices...)
		}
		require.Len(t, devicesNewIP, 1)

		// The discovered device should reuse the SAME device_identifier as before
		// (reconciled by MAC address), not a brand new one.
		assert.Equal(t, originalDeviceIdentifier, devicesNewIP[0].DeviceIdentifier,
			"re-discovered device should reuse the original device_identifier after MAC reconciliation")

		// Verify the discovered_device record's IP was updated to the new one
		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
		orgDeviceID := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: originalDeviceIdentifier,
			OrgID:            adminUser.OrganizationID,
		}
		dd, err := discoveredDeviceStore.GetDevice(ctx, orgDeviceID)
		require.NoError(t, err)
		assert.Equal(t, newIP, dd.IpAddress, "discovered_device IP should be updated to the new subnet IP")
		assert.Equal(t, "1.2.3", dd.FirmwareVersion, "MAC-reconciled rediscovery should preserve existing firmware when the new discovery omits it")

		// Verify no duplicate device records were created
		deviceID1, err := queries.GetDeviceIDByDeviceIdentifier(ctx, originalDeviceIdentifier)
		require.NoError(t, err)
		assert.Greater(t, deviceID1, int64(0), "device should exist in DB")

		pairingStatus, err := queries.GetDevicePairingStatusByDeviceDatabaseID(ctx, deviceID1)
		require.NoError(t, err)
		assert.Equal(t, pairing.StatusPaired, string(pairingStatus), "device should still be PAIRED")
	})
}

func TestDiscoveryReconciliation_DeletesUnpairedStaleEndpointRecord(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	queries := sqlc.New(testContext.ServiceProvider.DB)

	oldIP := "172.16.31.10"
	newIP := "172.16.41.10"
	port := "8080"
	mac := "AA:BB:CC:DD:EE:11"

	mockDiscoverer := &MockDiscoverer{}
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockPairer := pairingMocks.NewMockPairer(ctrl)
	mockPairer.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, device *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
			device.MacAddress = mac
			device.SerialNumber = "SN-011"
			dd, err := queries.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
				DeviceIdentifier: device.DeviceIdentifier,
				OrgID:            adminUser.OrganizationID,
			})
			if err != nil {
				return err
			}
			_, err = queries.InsertDevice(ctx, sqlc.InsertDeviceParams{
				OrgID:              adminUser.OrganizationID,
				DiscoveredDeviceID: dd.ID,
				DeviceIdentifier:   device.DeviceIdentifier,
				MacAddress:         mac,
			})
			if err != nil {
				return err
			}
			deviceID, err := queries.GetDeviceIDByDeviceIdentifier(ctx, device.DeviceIdentifier)
			if err != nil {
				return err
			}
			_, err = queries.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
				DeviceID:      deviceID,
				PairingStatus: sqlc.PairingStatusEnumPAIRED,
			})
			return err
		}).AnyTimes()
	mockPairer.EXPECT().
		GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("not implemented")).AnyTimes()

	pairingService, ctx := setupTestService(t, testContext, adminUser, mockPairer, mockDiscoverer)

	mockDiscoverer.On("Discover", mock.Anything, oldIP, port).Return(&discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:  oldIP,
			Port:       port,
			UrlScheme:  "http",
			DriverName: "proto",
			MacAddress: mac,
		},
	}, nil).Once()

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{oldIP},
		Ports:       []string{port},
	})
	require.NoError(t, err)

	var firstDiscovery []*pb.Device
	for result := range resultChan {
		firstDiscovery = append(firstDiscovery, result.Devices...)
	}
	require.Len(t, firstDiscovery, 1)
	originalIdentifier := firstDiscovery[0].DeviceIdentifier

	_, err = pairingService.PairDevices(ctx, createPairRequest([]string{originalIdentifier}))
	require.NoError(t, err)

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	_, err = discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: "stale-endpoint-device",
		OrgID:            adminUser.OrganizationID,
	}, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "stale-endpoint-device",
			IpAddress:        newIP,
			Port:             port,
			UrlScheme:        "http",
			DriverName:       "proto",
		},
		OrgID:    adminUser.OrganizationID,
		IsActive: true,
	})
	require.NoError(t, err)

	mockDiscoverer.On("Discover", mock.Anything, newIP, port).Return(&discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:  newIP,
			Port:       port,
			UrlScheme:  "http",
			DriverName: "proto",
			MacAddress: mac,
		},
	}, nil).Once()

	resultChan, err = pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{newIP},
		Ports:       []string{port},
	})
	require.NoError(t, err)

	var secondDiscovery []*pb.Device
	for result := range resultChan {
		secondDiscovery = append(secondDiscovery, result.Devices...)
	}
	require.Len(t, secondDiscovery, 1)
	assert.Equal(t, originalIdentifier, secondDiscovery[0].DeviceIdentifier)

	reconciledDevice, err := discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: originalIdentifier,
		OrgID:            adminUser.OrganizationID,
	})
	require.NoError(t, err)
	assert.Equal(t, newIP, reconciledDevice.IpAddress)
}

func TestDiscoveryReconciliation_SkipsPairedEndpointCollision(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()

	occupantIP := "172.16.51.10"
	originalIP := "172.16.61.10"
	port := "8080"
	occupantMAC := "AA:BB:CC:DD:EE:21"
	reconciledMAC := "AA:BB:CC:DD:EE:22"
	occupantIdentifier := "occupant-device"
	reconciledIdentifier := "reconciled-device"

	conn := testContext.ServiceProvider.DB
	_, err := conn.Exec(`
		INSERT INTO discovered_device (id, org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
		VALUES
			(601, $1, $2, 'test-model', 'test-manufacturer', 'proto', $3, $4, 'http', TRUE),
			(602, $1, $5, 'test-model', 'test-manufacturer', 'proto', $6, $4, 'http', TRUE)
	`, adminUser.OrganizationID, occupantIdentifier, occupantIP, port, reconciledIdentifier, originalIP)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device (id, org_id, discovered_device_id, device_identifier, mac_address)
		VALUES
			(601, $1, 601, $2, $3),
			(602, $1, 602, $4, $5)
	`, adminUser.OrganizationID, occupantIdentifier, occupantMAC, reconciledIdentifier, reconciledMAC)
	require.NoError(t, err)

	_, err = conn.Exec(`
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES
			(601, 'PAIRED', NOW()),
			(602, 'PAIRED', NOW())
	`)
	require.NoError(t, err)

	mockDiscoverer := &MockDiscoverer{}
	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	mockDiscoverer.On("Discover", mock.Anything, occupantIP, port).Return(&discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:  occupantIP,
			Port:       port,
			UrlScheme:  "http",
			DriverName: "proto",
			MacAddress: reconciledMAC,
		},
	}, nil).Once()

	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{occupantIP},
		Ports:       []string{port},
	})
	require.NoError(t, err)

	var collisionDiscovery []*pb.Device
	for result := range resultChan {
		collisionDiscovery = append(collisionDiscovery, result.Devices...)
	}
	require.Empty(t, collisionDiscovery, "paired endpoint collision should be skipped instead of producing an ambiguous discovery row")

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)

	occupantDevice, err := discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: occupantIdentifier,
		OrgID:            adminUser.OrganizationID,
	})
	require.NoError(t, err)
	assert.Equal(t, occupantIP, occupantDevice.IpAddress)

	reconciledDevice, err := discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: reconciledIdentifier,
		OrgID:            adminUser.OrganizationID,
	})
	require.NoError(t, err)
	assert.Equal(t, originalIP, reconciledDevice.IpAddress)

	lookupByEndpoint, err := discoveredDeviceStore.GetByIPAndPort(ctx, adminUser.OrganizationID, occupantIP, port)
	require.NoError(t, err)
	assert.Equal(t, occupantIdentifier, lookupByEndpoint.DeviceIdentifier)
}

func TestDiscoveryReconciliation_ReusesUnpairedIdentifierAcrossPortsOnSameIP(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	registerDiscoveryPortsPlugin(t, testContext, "asicrs", []string{"443", "4028"})

	scannedIP := "172.16.71.10"
	firstPort := "443"
	secondPort := "4028"

	mockDiscoverer := &MockDiscoverer{}
	pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

	firstDiscoveryDevice := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:       scannedIP,
			Port:            firstPort,
			UrlScheme:       "https",
			DriverName:      "asicrs",
			Model:           "M60S",
			Manufacturer:    "WhatsMiner",
			MacAddress:      "",
			SerialNumber:    "",
			FirmwareVersion: "1.0.0",
		},
	}
	secondDiscoveryDevice := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    scannedIP,
			Port:         secondPort,
			UrlScheme:    "http",
			DriverName:   "asicrs",
			Model:        "M60S",
			Manufacturer: "WhatsMiner",
			MacAddress:   "",
			SerialNumber: "",
		},
	}

	mockDiscoverer.On("Discover", mock.Anything, scannedIP, firstPort).Return(firstDiscoveryDevice, nil).Once()
	resultChan, err := pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{scannedIP},
		Ports:       []string{firstPort},
	})
	require.NoError(t, err)

	var firstDiscovery []*pb.Device
	for result := range resultChan {
		firstDiscovery = append(firstDiscovery, result.Devices...)
	}
	require.Len(t, firstDiscovery, 1)
	originalIdentifier := firstDiscovery[0].DeviceIdentifier

	mockDiscoverer.On("Discover", mock.Anything, scannedIP, secondPort).Return(secondDiscoveryDevice, nil).Once()
	resultChan, err = pairingService.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
		IpAddresses: []string{scannedIP},
		Ports:       []string{secondPort},
	})
	require.NoError(t, err)

	var secondDiscovery []*pb.Device
	for result := range resultChan {
		secondDiscovery = append(secondDiscovery, result.Devices...)
	}
	require.Len(t, secondDiscovery, 1)
	assert.Equal(t, originalIdentifier, secondDiscovery[0].DeviceIdentifier)

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	reconciledDevice, err := discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: originalIdentifier,
		OrgID:            adminUser.OrganizationID,
	})
	require.NoError(t, err)
	assert.Equal(t, secondPort, reconciledDevice.Port)
	assert.Empty(t, reconciledDevice.FirmwareVersion, "weak same-IP cross-port reconciliation should clear stale firmware when the new discovery omits it")
}

func TestPairDevices_UsesReconciledIdentifierAfterPairing(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	tokenService := testContext.ServiceProvider.TokenService
	pluginService := testContext.ServiceProvider.PluginService

	originalIdentifier := "paired-device-001"
	orphanIdentifier := "new-subnet-device-001"

	_, err := discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: originalIdentifier,
		OrgID:            adminUser.OrganizationID,
	}, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: originalIdentifier,
			IpAddress:        "172.16.21.10",
			Port:             "8080",
			UrlScheme:        "http",
			DriverName:       "proto",
			MacAddress:       "AA:BB:CC:DD:EE:01",
		},
		OrgID:    adminUser.OrganizationID,
		IsActive: true,
	})
	require.NoError(t, err)

	_, err = discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: orphanIdentifier,
		OrgID:            adminUser.OrganizationID,
	}, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: orphanIdentifier,
			IpAddress:        "172.16.25.10",
			Port:             "8080",
			UrlScheme:        "http",
			DriverName:       "proto",
			MacAddress:       "AA:BB:CC:DD:EE:01",
		},
		OrgID:    adminUser.OrganizationID,
		IsActive: true,
	})
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockListener := pairingMocks.NewMockListener(ctrl)
	mockListener.EXPECT().AddDevices(gomock.Any(), tmodels.DeviceIdentifier(originalIdentifier)).Return(nil)

	mockPairer := pairingMocks.NewMockPairer(ctrl)
	mockPairer.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, device *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
			require.Equal(t, orphanIdentifier, device.DeviceIdentifier)
			require.NoError(t, discoveredDeviceStore.SoftDelete(ctx, discoverymodels.DeviceOrgIdentifier{
				DeviceIdentifier: orphanIdentifier,
				OrgID:            adminUser.OrganizationID,
			}))
			device.DeviceIdentifier = originalIdentifier
			return nil
		})
	mockPairer.EXPECT().
		GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("not implemented"))

	pairingService := pairing.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenService,
		&MockDiscoverer{},
		pluginService,
		mockListener,
		mockPairer,
	)

	_, err = pairingService.PairDevices(ctx, createPairRequest([]string{orphanIdentifier}))
	require.NoError(t, err)

	_, err = discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: orphanIdentifier,
		OrgID:            adminUser.OrganizationID,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), "orphan discovered device should remain soft-deleted after reconciliation")

	reconciledDevice, err := discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: originalIdentifier,
		OrgID:            adminUser.OrganizationID,
	})
	require.NoError(t, err)
	assert.Equal(t, originalIdentifier, reconciledDevice.DeviceIdentifier)
}

func TestPairDevices_DeduplicatesAliasIdentifiersByIPPort(t *testing.T) {
	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	tokenService := testContext.ServiceProvider.TokenService
	pluginService := testContext.ServiceProvider.PluginService

	// Two different identifiers pointing to the same IP:port — the alias scenario
	// where duplicate discovered_device rows exist for the same physical endpoint.
	device1Identifier := "alias-device-001"
	device2Identifier := "alias-device-002"
	sharedIP := "10.0.1.10"
	sharedPort := "8080"

	_, err := discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: device1Identifier,
		OrgID:            adminUser.OrganizationID,
	}, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: device1Identifier,
			IpAddress:        sharedIP,
			Port:             sharedPort,
			UrlScheme:        "http",
			DriverName:       "proto",
		},
		OrgID:    adminUser.OrganizationID,
		IsActive: true,
	})
	require.NoError(t, err)

	_, err = discoveredDeviceStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: device2Identifier,
		OrgID:            adminUser.OrganizationID,
	}, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: device2Identifier,
			IpAddress:        sharedIP,
			Port:             sharedPort,
			UrlScheme:        "http",
			DriverName:       "proto",
		},
		OrgID:    adminUser.OrganizationID,
		IsActive: true,
	})
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	// PairDevice must be called exactly once — the duplicate alias must be filtered.
	mockPairer := pairingMocks.NewMockPairer(ctrl)
	mockPairer.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)
	mockPairer.EXPECT().
		GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("not implemented")).
		AnyTimes()

	mockListener := pairingMocks.NewMockListener(ctrl)
	mockListener.EXPECT().AddDevices(gomock.Any(), gomock.Any()).Return(nil)

	pairingService := pairing.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenService,
		&MockDiscoverer{},
		pluginService,
		mockListener,
		mockPairer,
	)

	// Act
	resp, err := pairingService.PairDevices(ctx, createPairRequest([]string{device1Identifier, device2Identifier}))

	// Assert: the alias (same IP:port) is reported as failed; the first identifier paired successfully.
	require.NoError(t, err)
	assert.Len(t, resp.FailedDeviceIds, 1, "the alias identifier should be reported as failed")
	assert.Equal(t, device2Identifier, resp.FailedDeviceIds[0], "second (alias) identifier should be the failed one")
}

func TestPairDevices_IncludeDevices_EmptyList(t *testing.T) {
	t.Run("returns error for empty device list", func(t *testing.T) {
		// Arrange
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		// Act
		pairRequest := createPairRequest([]string{})
		_, err := pairingService.PairDevices(ctx, pairRequest)

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "include_devices selector requires at least one device identifier")
	})
}
