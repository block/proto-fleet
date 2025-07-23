package pairing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web/mocks"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	pairingAntminer "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/antminer"
	pairingMocks "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/mocks"
	pairingProto "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/proto"
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

func (m *MockDiscoverer) Discover(ctx context.Context, ipAddress string, port string) (*minerdiscovery.DiscoveredDevice, error) {
	args := m.Called(ctx, ipAddress, port)
	if args.Get(0) == nil {
		return nil, fmt.Errorf("discover error: %w", args.Error(1))
	}
	device, ok := args.Get(0).(*minerdiscovery.DiscoveredDevice)
	if !ok {
		return nil, fmt.Errorf("unexpected type for device: %T", args.Get(0))
	}

	if err := args.Error(1); err != nil {
		return device, fmt.Errorf("discover error: %w", err)
	}

	return device, nil
}

func (m *MockDiscoverer) GetMinerType() miner.Type {
	args := m.Called()
	minerType, ok := args.Get(0).(miner.Type)
	if !ok {
		panic(fmt.Sprintf("unexpected type for miner type: %T", args.Get(0)))
	}
	return minerType
}

var _ minerdiscovery.Discoverer = (*MockDiscoverer)(nil)

func setupTestService(t *testing.T, testContext *testutil.TestContext, adminUser *testutil.TestUser, webClient *mocks.MockWebAPIClient, mockDiscoverers ...*MockDiscoverer) (*pairing.Service, context.Context) {
	discoverers := make([]minerdiscovery.Discoverer, len(mockDiscoverers))
	for i, m := range mockDiscoverers {
		discoverers[i] = m
	}

	tokenService := testContext.ServiceProvider.TokenService
	ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

	discoveryService, _ := minerdiscovery.NewService(discoverers...)
	discoveredDeviceStore := minerdiscovery.NewInMemoryDiscoveredDeviceStore()
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)

	protoPairer := pairingProto.NewService(transactor, deviceStore, pairing.Config{SecretKey: "test-secret"})

	antminerPairer := pairingAntminer.NewService(transactor, deviceStore, testContext.ServiceProvider.EncryptService, webClient)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockListener := pairingMocks.NewMockListener(ctrl)
	mockListener.EXPECT().AddDevices(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	pairingService := pairing.NewService(
		discoveredDeviceStore,
		deviceStore,
		transactor,
		tokenService,
		discoveryService,
		mockListener,
		protoPairer,
		antminerPairer,
	)

	return pairingService, ctx
}

func createMockDevice(ipAddress, port, serialNumber, deviceType string) *minerdiscovery.DiscoveredDevice {
	return &minerdiscovery.DiscoveredDevice{
		Device: pb.Device{
			IpAddress:    ipAddress,
			Port:         port,
			SerialNumber: serialNumber,
			UrlScheme:    "http",
		},
		Type: deviceType,
	}
}

func TestDiscoverWithIPList(t *testing.T) {
	t.Run("discovers devices from IP list", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice1 := createMockDevice("192.168.1.10", "8080", "SERIAL1", miner.TypeProto.String())
		mockDevice2 := createMockDevice("192.168.1.11", "8080", "SERIAL2", miner.TypeAntminer.String())

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice1, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.11", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice2, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

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

		assertDevicesEqual(t, devices, []*minerdiscovery.DiscoveredDevice{mockDevice1, mockDevice2})
	})
}

func TestDiscoverWithIPRange(t *testing.T) {
	t.Run("discovers devices in IP range", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(3)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice1 := createMockDevice("192.168.1.10", "8080", "RANGE1", miner.TypeProto.String())
		mockDevice2 := createMockDevice("192.168.1.11", "8080", "RANGE2", miner.TypeProto.String())
		mockDevice3 := createMockDevice("192.168.1.12", "8080", "RANGE3", miner.TypeAntminer.String())

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

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

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

		assertDevicesEqual(t, devices, []*minerdiscovery.DiscoveredDevice{mockDevice1, mockDevice2, mockDevice3})
	})

	t.Run("supports updates to existing devices", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(6)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice1 := createMockDevice("192.168.1.10", "8080", "RANGE1", miner.TypeProto.String())
		mockDevice2 := createMockDevice("192.168.1.11", "8080", "RANGE2", miner.TypeProto.String())
		mockDevice3 := createMockDevice("192.168.1.12", "8080", "RANGE3", miner.TypeAntminer.String())

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

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

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

		assertDevicesEqual(t, devices, []*minerdiscovery.DiscoveredDevice{mockDevice1, mockDevice2, mockDevice3})
	})

	t.Run("does not lead to duplicate device pairings", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice := createMockDevice("192.168.1.10", "8080", "RANGE1", miner.TypeProto.String())

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

		request := &pb.IPRangeModeRequest{
			StartIp: "192.168.1.10",
			EndIp:   "192.168.1.10",
			Ports:   []string{"8080"},
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

		assertDevicesEqual(t, devices, []*minerdiscovery.DiscoveredDevice{mockDevice})

		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 100)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})

	t.Run("handles discovery failures in IP range", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice := createMockDevice("192.168.1.20", "80", "SUCCESS1", miner.TypeProto.String())

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.20", "80").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.21", "80").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(nil, assert.AnError)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

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

		assertDevicesEqual(t, devices, []*minerdiscovery.DiscoveredDevice{mockDevice})
	})
}

func TestPairDevices(t *testing.T) {
	t.Run("pairs proto device successfully without credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice := createMockDevice("192.168.1.100", "8080", "PROTO-PAIR-001", miner.TypeProto.String())
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.100", "8080").Return(mockDevice, nil)

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

		// First discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{"192.168.1.100"},
			Ports:       []string{"8080"},
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

		// Verify pairing was successful
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})

	t.Run("pairs antminer device successfully with credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeAntminer)

		mockDevice := createMockDevice("192.168.1.101", "4028", "ANTMINER-PAIR-001", miner.TypeAntminer.String())
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.101", "4028").Return(mockDevice, nil)

		ctrl := gomock.NewController(t)
		webClient := mocks.NewMockWebAPIClient(ctrl)
		webClient.EXPECT().GetSystemInfo(gomock.Any(), gomock.Any()).Return(&web.SystemInfo{
			SerialNumber: "1234567890",
			MacAddr:      "00:11:22:33:44:55",
		}, nil)

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

		// First discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{"192.168.1.101"},
			Ports:       []string{"4028"},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Now pair the device with credentials
		password := "password123"
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
			Credentials: &pb.Credentials{
				Username: "admin",
				Password: &password,
			},
		}

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify pairing was successful
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, totalPairedDevices)
	})

	t.Run("fails to pair antminer device without credentials", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeAntminer)

		mockDevice := createMockDevice("192.168.1.102", "4028", "ANTMINER-PAIR-002", miner.TypeAntminer.String())
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.102", "4028").Return(mockDevice, nil)

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

		// First discover the device
		request := &pb.IPListModeRequest{
			IpAddresses: []string{"192.168.1.102"},
			Ports:       []string{"4028"},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 1)

		// Try to pair the device without credentials
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{devices[0].DeviceIdentifier},
		}

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required for Antminer pairing")

		// Verify no pairing was created
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 0, totalPairedDevices)
	})

	t.Run("pairs multiple devices of different types", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		protoDevice := createMockDevice("192.168.1.110", "8080", "PROTO-MULTI-001", miner.TypeProto.String())
		antminerDevice := createMockDevice("192.168.1.111", "4028", "ANTMINER-MULTI-001", miner.TypeAntminer.String())

		// Set up mocks for both devices
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.110", "8080").Return(protoDevice, nil)
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.111", "8080").Return(nil, minerdiscovery.MinerNotFoundFleetError)
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.110", "4028").Return(nil, minerdiscovery.MinerNotFoundFleetError)
		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.111", "4028").Return(antminerDevice, nil)

		ctrl := gomock.NewController(t)
		webClient := mocks.NewMockWebAPIClient(ctrl)
		webClient.EXPECT().GetSystemInfo(gomock.Any(), gomock.Any()).Return(&web.SystemInfo{
			SerialNumber: "1234567890",
			MacAddr:      "00:11:22:33:44:55",
		}, nil)

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

		// Discover both devices
		request := &pb.IPListModeRequest{
			IpAddresses: []string{"192.168.1.110", "192.168.1.111"},
			Ports:       []string{"8080", "4028"},
		}

		resultChan, err := pairingService.DiscoverWithIPList(ctx, request)
		require.NoError(t, err)

		var devices []*pb.Device
		for result := range resultChan {
			require.Empty(t, result.Error)
			devices = append(devices, result.Devices...)
		}
		require.Len(t, devices, 2)

		// Get device identifiers
		var deviceIdentifiers []string
		for _, device := range devices {
			deviceIdentifiers = append(deviceIdentifiers, device.DeviceIdentifier)
		}

		password := "password123"
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: deviceIdentifiers,
			Credentials: &pb.Credentials{
				Username: "admin",
				Password: &password,
			},
		}

		_, err = pairingService.PairDevices(ctx, pairRequest)
		require.NoError(t, err)

		// Verify both devices were paired
		totalPairedDevices, err := testContext.DatabaseService.GetTotalDevicePairings(adminUser.OrganizationID, 10)
		require.NoError(t, err)
		assert.Equal(t, 2, totalPairedDevices)
	})

	t.Run("fails to pair unsupported device type", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

		// Create a service with no pairers registered
		tokenService := testContext.ServiceProvider.TokenService
		discoveryService, _ := minerdiscovery.NewService()
		discoveredDeviceStore := minerdiscovery.NewInMemoryDiscoveredDeviceStore()
		transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
		deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)

		pairingService := pairing.NewService(
			discoveredDeviceStore,
			deviceStore,
			transactor,
			tokenService,
			discoveryService,
			nil,
			// No pairers registered
		)

		// Try to pair a non-existent device (this will fail at device lookup)
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{"unsupported-device-001"},
		}

		_, err := pairingService.PairDevices(ctx, pairRequest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get device")
	})

	t.Run("handles device not found error", func(t *testing.T) {
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		webClient := mocks.NewMockWebAPIClient(gomock.NewController(t))

		pairingService, ctx := setupTestService(t, testContext, adminUser, webClient, mockDiscoverer)

		// Try to pair a non-existent device
		pairRequest := &pb.PairRequest{
			DeviceIdentifiers: []string{"non-existent-device"},
		}

		_, err := pairingService.PairDevices(ctx, pairRequest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get device")
	})
}

func assertDevicesEqual(t *testing.T, actual []*pb.Device, expected []*minerdiscovery.DiscoveredDevice) {
	require.Len(t, actual, len(expected))

	expectedDevicesMap := make(map[string]*pb.Device)
	for _, device := range expected {
		expectedDevicesMap[device.SerialNumber] = &device.Device
	}

	actualDevicesMap := make(map[string]*pb.Device)
	for _, device := range actual {
		actualDevicesMap[device.SerialNumber] = device
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
