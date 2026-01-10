package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

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
func createTestPairer(ctrl *gomock.Controller, manager *Manager, minerType models.Type) *Pairer {
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}     // Simple instance for testing
	encryptService := &encrypt.Service{} // Simple instance for testing

	return NewPairer(manager, minerType, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)
}

func TestNewPairer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	minerType := models.TypeAntminer

	pairer := createTestPairer(ctrl, manager, minerType)

	assert.NotNil(t, pairer)
	assert.Equal(t, manager, pairer.manager)
	assert.Equal(t, minerType, pairer.minerType)
	assert.NotNil(t, pairer.transactor)
	assert.NotNil(t, pairer.deviceStore)
	assert.NotNil(t, pairer.userStore)
	assert.NotNil(t, pairer.tokenService)
	assert.NotNil(t, pairer.encryptService)
}

func TestPairer_GetMinerType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	minerType := models.TypeAntminer
	pairer := createTestPairer(ctrl, manager, minerType)

	assert.Equal(t, minerType, pairer.GetMinerType())

	pairerTwo := createTestPairer(ctrl, manager, models.TypeProto)
	assert.Equal(t, models.TypeProto, pairerTwo.GetMinerType())
}

func TestPairer_PairDevice_NoPlugin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	pairer := createTestPairer(ctrl, manager, models.TypeAntminer)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
		},
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password"),
	}

	ctx := t.Context()
	err := pairer.PairDevice(ctx, device, credentials)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin available for miner type")
}

func TestPairer_PairDevice_PluginNoPairingCapability(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Add mock plugin without pairing capability
	mockPlugin := &LoadedPlugin{
		Name: "test-plugin",
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true, // Has discovery but not pairing
		},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

	pairer := createTestPairer(ctrl, manager, models.TypeAntminer)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
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
		Type:         sdk.DeviceTypeASIC,
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
		Name:   "test-plugin",
		Driver: mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

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

	pairer := NewPairer(manager, models.TypeAntminer, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "TEST123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			Type:             "asic",
			MacAddress:       "00-11-22-33-44-55",
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
		Type:         sdk.DeviceTypeASIC,
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

	// Add mock plugin with pairing capability
	mockPlugin := &LoadedPlugin{
		Name:   "test-plugin",
		Driver: mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByType[models.TypeProto] = mockPlugin

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

	pairer := NewPairer(manager, models.TypeProto, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "proto-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "grpc",
			SerialNumber:     "PROTO123",
			Model:            "ProtoMiner v1",
			Manufacturer:     "Proto",
			Type:             "asic",
			MacAddress:       "00-11-22-33-44-55",
		},
		OrgID: 1,
	}
	// No credentials provided - will use org public key
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

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err)
}

func TestPairer_GetDeviceInfo_PluginNoPairingCapability(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Add mock plugin without pairing capability
	mockPlugin := &LoadedPlugin{
		Name: "test-plugin",
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true, // Has discovery but not pairing
		},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

	pairer := createTestPairer(ctrl, manager, models.TypeAntminer)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
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
		Type:         sdk.DeviceTypeASIC,
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
		Name:   "test-plugin",
		Driver: mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

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

	pairer := NewPairer(manager, models.TypeAntminer, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			Type:             "asic",
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
	assert.Equal(t, "antminer", result.Type)
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
		Type:         sdk.DeviceTypeASIC,
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
		Name:   "proto-plugin",
		Driver: mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByType[models.TypeProto] = mockPlugin

	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	userStore := mocks.NewMockUserStore(ctrl)
	tokenService := &token.Service{}
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, models.TypeProto, transactor, discoveredDeviceStore, deviceStore, userStore, tokenService, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "proto-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "grpc",
			SerialNumber:     "PROTO123",
			Type:             "asic",
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
