package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/btc-mining/proto-fleet/server/sdk/v1/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create test pairer with all required services
func createTestPairer(ctrl *gomock.Controller, manager *Manager) *Pairer {
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}     // Simple instance for testing
	encryptService := &encrypt.Service{} // Simple instance for testing

	return NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)
}

func TestNewPairer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	pairer := createTestPairer(ctrl, manager)

	assert.NotNil(t, pairer)
	assert.Equal(t, manager, pairer.manager)
	assert.NotNil(t, pairer.transactor)
	assert.NotNil(t, pairer.deviceStore)
	assert.NotNil(t, pairer.userStore)
	assert.NotNil(t, pairer.tokenService)
	assert.NotNil(t, pairer.encryptService)
}

func TestPairer_PairDevice_NoPlugin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	pairer := createTestPairer(ctrl, manager)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			DriverName:       "antminer",
		},
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password"),
	}

	ctx := t.Context()
	err := pairer.PairDevice(ctx, device, credentials)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin available for driver name")
}

func TestPairer_PairDevice_PluginNoPairingCapability(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Add mock plugin without pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true, // Has discovery but not pairing
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			DriverName:       "antminer",
		},
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password"),
	}

	ctx := t.Context()
	err := pairer.PairDevice(ctx, device, credentials)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support capability pairing")
}

func TestPairer_PairDevice_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Expected converted DeviceInfo
	expectedDeviceInfo := sdk.DeviceInfo{
		Host:         "192.168.1.100",
		Port:         80,
		URLScheme:    "http",
		SerialNumber: "TEST123",
		Model:        "S19",
		Manufacturer: "Bitmain",
		MacAddress:   "00-11-22-33-44-55",
	}

	// Expected converted SecretBundle for username/password
	expectedSecretBundle := sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.UsernamePassword{
			Username: "admin",
			Password: "password123",
		},
	}

	// Create mock driver with specific expectations
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Eq(expectedDeviceInfo), gomock.Eq(expectedSecretBundle)).
		Return(expectedDeviceInfo, nil)

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Create pairer with mocked dependencies
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "TEST123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			MacAddress:       "00-11-22-33-44-55",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password123"),
	}

	ctx := t.Context()

	// Mock transactor to execute the function immediately
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	// Mock device store operations
	// GetDeviceByDeviceIdentifier returns nil (device doesn't exist yet)
	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), device.DeviceIdentifier, device.OrgID).Return(nil, fleeterror.NewNotFoundError("device not found"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err)
}

func TestPairer_PairDevice_Success_APIKey(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Expected converted DeviceInfo
	expectedDeviceInfo := sdk.DeviceInfo{
		Host:         "192.168.1.100",
		Port:         4028,
		URLScheme:    "grpc",
		SerialNumber: "PROTO123",
		Model:        "ProtoMiner v1",
		Manufacturer: "Proto",
		MacAddress:   "00-11-22-33-44-55",
	}

	// Create mock driver with specific expectations
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Eq(expectedDeviceInfo), gomock.Any()).
		DoAndReturn(func(_ context.Context, device sdk.DeviceInfo, bundle sdk.SecretBundle) (sdk.DeviceInfo, error) {
			// Verify bundle contains APIKey
			_, ok := bundle.Kind.(sdk.APIKey)
			require.True(t, ok, "Expected APIKey in SecretBundle")
			return device, nil
		})

	// Add mock plugin with pairing capability and asymmetric auth (like real Proto plugin)
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "proto"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing:        true,
			sdk.CapabilityAsymmetricAuth: true,
		},
	}
	manager.pluginsByDriverName["proto"] = mockPlugin

	// Create pairer with mocked dependencies
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "proto-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "grpc",
			SerialNumber:     "PROTO123",
			Model:            "ProtoMiner v1",
			Manufacturer:     "Proto",
			MacAddress:       "00-11-22-33-44-55",
			DriverName:       "proto",
		},
		OrgID: 1,
	}
	// No credentials provided - will use org public key (asymmetric auth)
	var credentials *pb.Credentials

	ctx := t.Context()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	encryptedPrivateKey, err := encryptService.Encrypt([]byte(privateKey))
	require.NoError(t, err)

	// Mock user store to return encrypted org private key
	// Called 2 times: PairDevice createSecretBundle, saveCredentials createSecretBundle
	userStore.EXPECT().GetOrganizationPrivateKey(gomock.Any(), device.OrgID).Return(encryptedPrivateKey, nil).Times(2)

	// Mock transactor to execute the function immediately
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	// Mock device store operations
	// GetDeviceByDeviceIdentifier returns nil (device doesn't exist yet)
	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), device.DeviceIdentifier, device.OrgID).Return(nil, fleeterror.NewNotFoundError("device not found"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	// No UpsertMinerCredentials call expected - org-level keys aren't stored
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err)
}

func TestPairer_GetDeviceInfo_PluginNoPairingCapability(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Add mock plugin without pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true, // Has discovery but not pairing
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password"),
	}

	ctx := t.Context()
	result, err := pairer.GetDeviceInfo(ctx, device, credentials)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "does not support capability pairing")
}

func TestPairer_GetDeviceInfo_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	deviceInfo := sdk.DeviceInfo{
		Host:         "192.168.1.100",
		Port:         80,
		URLScheme:    "http",
		SerialNumber: "TEST123",
		Model:        "S19 Pro",
		Manufacturer: "Bitmain",
		MacAddress:   "00:11:22:33:44:55",
	}

	// Expected converted SecretBundle for username/password
	expectedSecretBundle := sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.UsernamePassword{
			Username: "admin",
			Password: "password123",
		},
	}

	// Create mock device
	mockDevice := sdkMocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().
		DescribeDevice(gomock.Any()).
		Return(deviceInfo, sdk.Capabilities{}, nil)

	// Create mock driver
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), "test-device", gomock.Any(), gomock.Eq(expectedSecretBundle)).
		Return(sdk.NewDeviceResult{Device: mockDevice}, nil)

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Create pairer with mocked dependencies
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password123"),
	}

	ctx := t.Context()

	result, err := pairer.GetDeviceInfo(ctx, device, credentials)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "192.168.1.100", result.IpAddress)
	assert.Equal(t, "80", result.Port)
	assert.Equal(t, "http", result.UrlScheme)
	assert.Equal(t, "TEST123", result.SerialNumber)
	assert.Equal(t, "S19 Pro", result.Model)
	assert.Equal(t, "Bitmain", result.Manufacturer)
	assert.Equal(t, "antminer", result.DriverName)
	assert.Equal(t, "00-11-22-33-44-55", result.MacAddress)
}

func TestPairer_GetDeviceInfo_ProtoUsesBearerToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	deviceInfo := sdk.DeviceInfo{
		Host:         "192.168.1.100",
		Port:         4028,
		URLScheme:    "grpc",
		SerialNumber: "PROTO123",
		Model:        "ProtoMiner v1",
		Manufacturer: "Proto",
		MacAddress:   "00:11:22:33:44:55",
	}

	mockSDKDevice := sdkMocks.NewMockDevice(ctrl)
	mockSDKDevice.EXPECT().
		DescribeDevice(gomock.Any()).
		Return(deviceInfo, sdk.Capabilities{}, nil)

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), "proto-device-001", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ sdk.DeviceInfo, bundle sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			bearer, ok := bundle.Kind.(sdk.BearerToken)
			require.True(t, ok, "expected bearer token in secret bundle")
			require.NotEmpty(t, bearer.Token)
			return sdk.NewDeviceResult{Device: mockSDKDevice}, nil
		})

	mockPlugin := &LoadedPlugin{
		Name:       "proto-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "proto"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing:        true,
			sdk.CapabilityAsymmetricAuth: true,
		},
	}
	manager.pluginsByDriverName["proto"] = mockPlugin

	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "proto-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "grpc",
			SerialNumber:     "PROTO123",
			DriverName:       "proto",
		},
		OrgID: 1,
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	encryptedPrivateKey, err := encryptService.Encrypt([]byte(privateKey))
	require.NoError(t, err)
	userStore.EXPECT().GetOrganizationPrivateKey(gomock.Any(), device.OrgID).Return(encryptedPrivateKey, nil)

	ctx := t.Context()
	result, err := pairer.GetDeviceInfo(ctx, device, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "PROTO123", result.SerialNumber)
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}

// mockDriverWithDefaultCredentials wraps a mock driver with default credentials support.
// This allows testing the DefaultCredentialsProvider interface along with the Driver interface.
type mockDriverWithDefaultCredentials struct {
	sdk.Driver
	defaultCredentials []sdk.UsernamePassword
}

func (m *mockDriverWithDefaultCredentials) GetDefaultCredentials(_ context.Context) []sdk.UsernamePassword {
	return m.defaultCredentials
}

// TestPairer_PairDevice_AntminerAutoCredentials_Success tests that Antminer devices
// are automatically paired using default credentials when no credentials are provided.
func TestPairer_PairDevice_AntminerAutoCredentials_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Expected device info and secret bundle with default credentials (root/root)
	expectedDeviceInfo := sdk.DeviceInfo{
		Host:            "192.168.1.100",
		Port:            80,
		URLScheme:       "http",
		SerialNumber:    "ANTMINER123",
		Model:           "S19",
		Manufacturer:    "Bitmain",
		MacAddress:      "00-11-22-33-44-55",
		FirmwareVersion: "1.0.0",
	}

	expectedSecretBundle := sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.UsernamePassword{
			Username: "root",
			Password: "root",
		},
	}

	// Create mock SDK device for GetDeviceInfo call
	mockSDKDevice := sdkMocks.NewMockDevice(ctrl)
	mockSDKDevice.EXPECT().
		DescribeDevice(gomock.Any()).
		Return(expectedDeviceInfo, sdk.Capabilities{}, nil)

	// Create mock driver expecting default credentials
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Eq(expectedSecretBundle)).
		Return(expectedDeviceInfo, nil)
	// After pairing, GetDeviceInfo calls NewDevice to fetch firmware version
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), "antminer-device-001", gomock.Any(), gomock.Eq(expectedSecretBundle)).
		Return(sdk.NewDeviceResult{Device: mockSDKDevice}, nil)

	// Wrap mock driver with default credentials provider
	driverWithCreds := &mockDriverWithDefaultCredentials{
		Driver: mockDriver,
		defaultCredentials: []sdk.UsernamePassword{
			{Username: "root", Password: "root"},
		},
	}

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     driverWithCreds,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Create pairer with mocked dependencies
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "ANTMINER123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			MacAddress:       "00-11-22-33-44-55",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	// NO credentials provided - should use default credentials
	var credentials *pb.Credentials

	ctx := t.Context()

	// Mock transactor to execute the function immediately
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	// Mock device store operations
	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), device.DeviceIdentifier, device.OrgID).Return(nil, fleeterror.NewNotFoundError("device not found"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err, "Antminer should be paired successfully with default credentials")
	assert.Equal(t, "1.0.0", device.FirmwareVersion, "Firmware version should be populated from GetDeviceInfo")
}

// TestPairer_PairDevice_AntminerAutoCredentials_AuthFailure tests that when default
// credentials fail with an authentication error, the pairer returns a "credentials required"
// error to trigger the AUTHENTICATION_NEEDED flow.
func TestPairer_PairDevice_AntminerAutoCredentials_AuthFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Create mock driver that returns a typed authentication error
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.DeviceInfo{}, sdk.NewErrorAuthenticationFailed("antminer-device-002"))

	// Wrap mock driver with default credentials provider
	driverWithCreds := &mockDriverWithDefaultCredentials{
		Driver: mockDriver,
		defaultCredentials: []sdk.UsernamePassword{
			{Username: "root", Password: "root"},
		},
	}

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     driverWithCreds,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-002",
			IpAddress:        "192.168.1.101",
			Port:             "80",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	// NO credentials provided
	var credentials *pb.Credentials

	ctx := t.Context()
	err := pairer.PairDevice(ctx, device, credentials)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are required", "Should return credentials required error for auth failure")
}

// TestPairer_PairDevice_AntminerAutoCredentials_NetworkError tests that network errors
// are not retried and are propagated immediately.
func TestPairer_PairDevice_AntminerAutoCredentials_NetworkError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Create mock driver that returns a network error (not an auth error)
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.DeviceInfo{}, fmt.Errorf("plugin pairing failed: connection timeout")).
		Times(1) // Should only be called once, not retried

	// Wrap mock driver with default credentials provider
	driverWithCreds := &mockDriverWithDefaultCredentials{
		Driver: mockDriver,
		defaultCredentials: []sdk.UsernamePassword{
			{Username: "root", Password: "root"},
		},
	}

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     driverWithCreds,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-003",
			IpAddress:        "192.168.1.102",
			Port:             "80",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	// NO credentials provided
	var credentials *pb.Credentials

	ctx := t.Context()
	err := pairer.PairDevice(ctx, device, credentials)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection timeout", "Network error should be propagated")
	assert.NotContains(t, err.Error(), "credentials are required", "Should not convert to credentials error")
}

// TestPairer_PairDevice_AntminerExplicitCredentials tests that when explicit credentials
// are provided for an Antminer, the auto-credential logic is bypassed.
func TestPairer_PairDevice_AntminerExplicitCredentials(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Expected secret bundle with explicit credentials (admin/custompass)
	expectedSecretBundle := sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.UsernamePassword{
			Username: "admin",
			Password: "custompass",
		},
	}

	expectedDeviceInfo := sdk.DeviceInfo{
		Host:         "192.168.1.100",
		Port:         80,
		URLScheme:    "http",
		SerialNumber: "ANTMINER456",
		Model:        "S19 Pro",
		Manufacturer: "Bitmain",
		MacAddress:   "AA-BB-CC-DD-EE-FF",
	}

	// Create mock driver expecting explicit credentials
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Eq(expectedSecretBundle)).
		Return(expectedDeviceInfo, nil)

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Create pairer with mocked dependencies
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-004",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	// Explicit credentials provided - should NOT use default credentials
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("custompass"),
	}

	ctx := t.Context()

	// Mock transactor to execute the function immediately
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	// Mock device store operations
	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), device.DeviceIdentifier, device.OrgID).Return(nil, fleeterror.NewNotFoundError("device not found"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err, "Antminer should be paired with explicit credentials")
}

// TestIsAuthenticationFailure tests the isAuthenticationFailure helper function.
func TestIsAuthenticationFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "SDK authentication failed error",
			err:      sdk.NewErrorAuthenticationFailed("device-123"),
			expected: true,
		},
		{
			name:     "SDK device not found error",
			err:      sdk.NewErrorDeviceNotFound("device-123"),
			expected: false,
		},
		{
			name:     "wrapped SDK authentication error",
			err:      fmt.Errorf("plugin pairing failed: %w", sdk.NewErrorAuthenticationFailed("device-123")),
			expected: true,
		},
		{
			name:     "gRPC Unauthenticated status error",
			err:      status.Error(codes.Unauthenticated, "authentication failed for device: http://192.168.1.1:80"),
			expected: true,
		},
		{
			name:     "network error",
			err:      fmt.Errorf("connection timeout"),
			expected: false,
		},
		{
			name:     "generic error",
			err:      fmt.Errorf("plugin pairing failed"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthenticationFailure(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestPairer_PairDevice_WithoutDefaultCredentialsProvider tests that plugins that do not
// implement the DefaultCredentialsProvider interface correctly require credentials.
func TestPairer_PairDevice_WithoutDefaultCredentialsProvider(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Create mock driver that does NOT implement DefaultCredentialsProvider
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	// No expectations set - if PairDevice is called, the test will fail

	// Plugin with a driver that does NOT implement DefaultCredentialsProvider
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver, // Plain driver without DefaultCredentialsProvider
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	// NO credentials provided
	var credentials *pb.Credentials

	ctx := t.Context()
	err := pairer.PairDevice(ctx, device, credentials)

	// Should fail with "credentials required" error because driver doesn't provide defaults
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are required", "Should require credentials when driver doesn't implement DefaultCredentialsProvider")
}
