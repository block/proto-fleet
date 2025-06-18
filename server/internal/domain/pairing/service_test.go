package pairing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pairing"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

type MockDiscoverer struct {
	mock.Mock
}

func (m *MockDiscoverer) Discover(ctx context.Context, ipAddress string, port string) (*pb.Device, error) {
	args := m.Called(ctx, ipAddress, port)
	if args.Get(0) == nil {
		return nil, fmt.Errorf("discover error: %w", args.Error(1))
	}
	device, ok := args.Get(0).(*pb.Device)
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

func setupTestService(t *testing.T, testContext *testutil.TestContext, adminUser *testutil.TestUser, mockDiscoverers ...*MockDiscoverer) (*pairing.Service, context.Context) {
	discoverers := make([]minerdiscovery.Discoverer, len(mockDiscoverers))
	for i, m := range mockDiscoverers {
		discoverers[i] = m
	}

	tokenService := testContext.ServiceProvider.TokenService
	ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, adminUser.OrganizationID)

	discoveryService, _ := minerdiscovery.NewService(discoverers...)

	pairingService := pairing.NewService(
		testContext.ServiceProvider.DB,
		pairing.Config{SecretKey: "test-secret"},
		tokenService,
		discoveryService,
	)

	return pairingService, ctx
}

func TestDiscoverWithIPList(t *testing.T) {
	t.Run("discovers devices from IP list", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice1 := &pb.Device{
			IpAddress:    "192.168.1.10",
			Port:         "8080",
			SerialNumber: "SERIAL1",
		}
		mockDevice2 := &pb.Device{
			IpAddress:    "192.168.1.11",
			Port:         "8080",
			SerialNumber: "SERIAL2",
		}

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice1, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.11", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice2, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockDiscoverer)

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

		require.Len(t, devices, 2)

		expectedSerialNumbers := []string{mockDevice1.SerialNumber, mockDevice2.SerialNumber}

		foundSerialNumbers := []string{}
		for _, device := range devices {
			foundSerialNumbers = append(foundSerialNumbers, device.SerialNumber)
		}

		assert.ElementsMatch(t, expectedSerialNumbers, foundSerialNumbers)
	})
}

func TestDiscoverWithIPRange(t *testing.T) {
	t.Run("discovers devices in IP range", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(3)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice1 := &pb.Device{
			IpAddress:    "192.168.1.10",
			Port:         "8080",
			SerialNumber: "RANGE1",
		}
		mockDevice2 := &pb.Device{
			IpAddress:    "192.168.1.11",
			Port:         "8080",
			SerialNumber: "RANGE2",
		}
		mockDevice3 := &pb.Device{
			IpAddress:    "192.168.1.12",
			Port:         "8080",
			SerialNumber: "RANGE3",
		}

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

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockDiscoverer)

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

		require.Len(t, devices, 3)

		expectedSerialNumbers := []string{mockDevice1.SerialNumber, mockDevice2.SerialNumber, mockDevice3.SerialNumber}

		foundSerialNumbers := []string{}
		for _, device := range devices {
			foundSerialNumbers = append(foundSerialNumbers, device.SerialNumber)
		}

		assert.ElementsMatch(t, expectedSerialNumbers, foundSerialNumbers)
	})

	t.Run("supports updates to existing devices", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(6)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice1 := &pb.Device{
			IpAddress:    "192.168.1.10",
			Port:         "8080",
			SerialNumber: "RANGE1",
		}
		mockDevice2 := &pb.Device{
			IpAddress:    "192.168.1.11",
			Port:         "8080",
			SerialNumber: "RANGE2",
		}
		mockDevice3 := &pb.Device{
			IpAddress:    "192.168.1.12",
			Port:         "8080",
			SerialNumber: "RANGE3",
		}

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

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockDiscoverer)

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

		require.Len(t, devices, 3, devices)

		devicesBySerialNumber := make(map[string]*pb.Device)
		for _, device := range devices {
			devicesBySerialNumber[device.SerialNumber] = device
		}

		assert.Equal(t, mockDevice1.IpAddress, devicesBySerialNumber[mockDevice1.SerialNumber].IpAddress)
		assert.Equal(t, mockDevice2.IpAddress, devicesBySerialNumber[mockDevice2.SerialNumber].IpAddress)
		assert.Equal(t, mockDevice3.IpAddress, devicesBySerialNumber[mockDevice3.SerialNumber].IpAddress)
	})

	t.Run("does not lead to duplicate device pairings", func(t *testing.T) {
		// Arrange
		var discoverWg sync.WaitGroup
		discoverWg.Add(2)

		mockDiscoverer := &MockDiscoverer{}
		mockDiscoverer.On("GetMinerType").Return(miner.TypeProto)

		mockDevice := &pb.Device{
			IpAddress:    "192.168.1.10",
			Port:         "8080",
			SerialNumber: "RANGE1",
		}

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.10", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()

		pairingService, ctx := setupTestService(t, testContext, adminUser, mockDiscoverer)

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

		require.Len(t, devices, 1)

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

		mockDevice := &pb.Device{
			IpAddress:    "192.168.1.20",
			Port:         "8080",
			SerialNumber: "SUCCESS1",
		}

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.20", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(mockDevice, nil)

		mockDiscoverer.On("Discover", mock.Anything, "192.168.1.21", "8080").Run(func(_ mock.Arguments) {
			defer discoverWg.Done()
		}).Return(nil, assert.AnError)

		testContext := testutil.InitializeDBServiceInfrastructure(t)
		adminUser := testContext.DatabaseService.CreateSuperAdminUser()
		pairingService, ctx := setupTestService(t, testContext, adminUser, mockDiscoverer)

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

		require.Len(t, devices, 1)

		expectedSerialNumbers := []string{mockDevice.SerialNumber}

		foundSerialNumbers := []string{}
		for _, device := range devices {
			foundSerialNumbers = append(foundSerialNumbers, device.SerialNumber)
		}

		assert.ElementsMatch(t, expectedSerialNumbers, foundSerialNumbers)
	})
}
