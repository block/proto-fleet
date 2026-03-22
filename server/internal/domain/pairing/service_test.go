package pairing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	commonv1 "github.com/proto-at-block/proto-fleet/server/generated/grpc/common/v1"
	fm "github.com/proto-at-block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	commandpb "github.com/proto-at-block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/proto-at-block/proto-fleet/server/generated/sqlc"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/proto-at-block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/pairing"
	pairingMocks "github.com/proto-at-block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/proto-at-block/proto-fleet/server/internal/testutil"
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
		out[k] = c
	}
	return out
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
				return queries.UpdateDevicePairingStatusByIdentifier(ctx, sqlc.UpdateDevicePairingStatusByIdentifierParams{
					PairingStatus:    sqlc.PairingStatusEnumPAIRED,
					DeviceIdentifier: device.DeviceIdentifier,
				})
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
				IpAddress:  oldIP,
				Port:       port,
				UrlScheme:  "http",
				DriverName: "proto",
				MacAddress: mac,
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
