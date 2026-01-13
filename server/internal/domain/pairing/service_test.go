package pairing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	pairingMocks "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/golang/mock/gomock"
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

func setupTestService(t *testing.T, testContext *testutil.TestContext, adminUser *testutil.TestUser, additionalPairers []pairing.Pairer, mockDiscoverer *MockDiscoverer) (*pairing.Service, context.Context) {
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

	protoPairer := testutil.NewMockProtoPairer(ctrl)
	pairers := []pairing.Pairer{protoPairer}
	pairers = append(pairers, additionalPairers...)

	pairingService := pairing.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenService,
		mockDiscoverer,
		pluginService,
		mockListener,
		pairers...,
	)

	return pairingService, ctx
}

func createMockDevice(ipAddress, port, deviceType string) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			IpAddress: ipAddress,
			Port:      port,
			UrlScheme: "http",
			Type:      deviceType,
		},
	}
}

// TODO(DASH-887): setUpMockMinerServer should be reimplemented using plugin-based test infrastructure
// This functionality should be reimplemented using the proto plugin's integration test
// helpers (see plugin/proto/tests/integration) when needed for server integration testing.
func setUpMockMinerServer(t *testing.T) (string, string) {
	t.Skip("Disabled pending DASH-887")
	return "", ""
}

func TestDiscoverWithIPList(t *testing.T) {
	t.Run("discovers devices from IP list", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice1 := createMockDevice("192.168.1.10", "8080", miner.TypeProto.String())
		mockDevice2 := createMockDevice("192.168.1.11", "8080", miner.TypeAntminer.String())

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

func TestDiscoverWithIPRange(t *testing.T) {
	t.Run("discovers devices in IP range", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(3)

		mockDiscoverer := &MockDiscoverer{}
		mockDevice1 := createMockDevice("192.168.1.10", "8080", miner.TypeProto.String())
		mockDevice2 := createMockDevice("192.168.1.11", "8080", miner.TypeProto.String())
		mockDevice3 := createMockDevice("192.168.1.12", "8080", miner.TypeAntminer.String())

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
		mockDevice1 := createMockDevice("192.168.1.10", "8080", miner.TypeProto.String())
		mockDevice2 := createMockDevice("192.168.1.11", "8080", miner.TypeProto.String())
		mockDevice3 := createMockDevice("192.168.1.12", "8080", miner.TypeAntminer.String())

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
		mockDevice := createMockDevice(host, portStr, miner.TypeProto.String())

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
		_, err = pairingService.PairDevices(ctx, &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
		})
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
		mockDevice := createMockDevice("192.168.1.20", "80", miner.TypeProto.String())

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.20", "80").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.21", "80").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(nil, assert.AnError)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, nil, mockDiscoverer)

		request := &pb.IPRangeModeRequest{
			StartIp: "192.168.1.20",
			EndIp:   "192.168.1.21",
			Ports:   []string{"80"},
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
		mockDevice := createMockDevice(host, portStr, miner.TypeAntminer.String())
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns credentials required error
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockAntminerPairer := pairingMocks.NewMockPairer(ctrl)
		mockAntminerPairer.EXPECT().GetMinerType().Return(miner.TypeAntminer).AnyTimes()
		mockAntminerPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(fmt.Errorf("invalid_argument: credentials are required but were not provided"))

		pairingService, ctx := setupTestService(t, testContext, adminUser, []pairing.Pairer{mockAntminerPairer}, mockDiscoverer)

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
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
		}

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
		mockDevice := createMockDevice(host, portStr, miner.TypeProto.String())
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
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
		}

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
			// No pairers registered
		)

		// Try to pair a non-existent device (this will fail at device lookup)
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{"unsupported-device-001"},
		}

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
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{"non-existent-device"},
		}

		_, err := pairingService.PairDevices(ctx, pairRequest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to pair any devices")
	})

	t.Run("pairs miners even if one of them fails", func(t *testing.T) {
		host, portStr := setUpMockMinerServer(t)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, miner.TypeProto.String())
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
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier, "test-invalid-device"},
		}

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify pairing was successful for the valid device
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})
}

func assertDevicesEqual(t *testing.T, actual []*pb.Device, expected []*discoverymodels.DiscoveredDevice) {
	require.Len(t, actual, len(expected))

	expectedDevicesMap := make(map[string]*pb.Device)
	for _, device := range expected {
		key := fmt.Sprintf("%s-%s", device.Type, device.IpAddress)
		expectedDevicesMap[key] = &device.Device
	}

	actualDevicesMap := make(map[string]*pb.Device)
	for _, device := range actual {
		key := fmt.Sprintf("%s-%s", device.Type, device.IpAddress)
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
		out[k] = c
	}
	return out
}

func TestPairDevices_SavesFirmwareVersion(t *testing.T) {
	t.Run("saves firmware version after successful pairing", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		host := "192.168.1.100"
		portStr := "80"
		expectedFirmwareVersion := "1.2.3"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, miner.TypeProto.String())
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns successful pairing
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockProtoPairer := pairingMocks.NewMockPairer(ctrl)
		mockProtoPairer.EXPECT().GetMinerType().Return(miner.TypeProto).AnyTimes()

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

		pairingService, ctx := setupTestService(t, testContext, adminUser, []pairing.Pairer{mockProtoPairer}, mockDiscoverer)

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
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
		}

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
		portStr := "80"

		mockDiscoverer := &MockDiscoverer{}
		mockDevice := createMockDevice(host, portStr, miner.TypeProto.String())
		mockDiscoverer.On("Discover", mock.Anything, host, portStr).Return(mockDevice, nil)

		// Create mock pairer that returns successful pairing but no firmware
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockProtoPairer := pairingMocks.NewMockPairer(ctrl)
		mockProtoPairer.EXPECT().GetMinerType().Return(miner.TypeProto).AnyTimes()

		mockProtoPairer.EXPECT().
			PairDevice(gomock.Any(), gomock.Any(), nil).
			Return(nil)

		// Mock GetDeviceInfo that returns error (firmware unavailable)
		mockProtoPairer.EXPECT().
			GetDeviceInfo(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("failed to get device info"))

		pairingService, ctx := setupTestService(t, testContext, adminUser, []pairing.Pairer{mockProtoPairer}, mockDiscoverer)

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
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
		}

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
}
