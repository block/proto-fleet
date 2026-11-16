package plugins

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Helper function to create test pairer with all required services
func createTestPairer(ctrl *gomock.Controller, manager *Manager) *Pairer {
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService := &encrypt.Service{} // Simple instance for testing

	return NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)
}

// These unit fixtures model a stable discovery snapshot. Database race tests
// verify that the real store refuses changed snapshots before pairing writes.
func stablePairingSnapshotStore(ctrl *gomock.Controller) *mocks.MockDiscoveredDeviceStore {
	store := mocks.NewMockDiscoveredDeviceStore(ctrl)
	store.EXPECT().LockPairingSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	return store
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

func TestPairer_PairDevice_InitializesWorkerNameWithoutReadingPools(t *testing.T) {
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
			sdk.CapabilityPairing:    true,
			sdk.CapabilityPoolConfig: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Create pairer with mocked dependencies
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "TEST123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			MacAddress:       "00:11:22:33:44:55",
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
	deviceStore.EXPECT().GetPairedDeviceByMACAddress(gomock.Any(), gomock.Any(), device.OrgID, "").Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().GetPairedDeviceBySerialNumber(gomock.Any(), gomock.Any(), device.OrgID).Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().InitializeWorkerName(
		gomock.Any(), int64(1),
		models.DeviceIdentifier(device.DeviceIdentifier),
		"00:11:22:33:44:55",
	).Return(true, nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err)
}

func TestPairer_CreateSecretBundle_AllowsBlankPassword(t *testing.T) {
	pairer := &Pairer{}
	password := ""

	bundle, err := pairer.createSecretBundle(&pb.Credentials{
		Username: "admin",
		Password: &password,
	})

	require.NoError(t, err)
	assert.Equal(t, sdk.UsernamePassword{Username: "admin", Password: ""}, bundle.Kind)
}

func TestPairer_CreateSecretBundle_RequiresPasswordPresence(t *testing.T) {
	pairer := &Pairer{}

	_, err := pairer.createSecretBundle(&pb.Credentials{
		Username: "admin",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "password is required")
}

// TestPairer_PairDevice_DefaultPasswordActive_PersistsRemediationState verifies a
// device that pairs while still on its factory password is recorded immediately in
// the DEFAULT_PASSWORD pairing state without changing its successful initial status.
func TestPairer_PairDevice_DefaultPasswordActive_PersistsRemediationState(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	expectedDeviceInfo := sdk.DeviceInfo{
		Host:         "192.168.1.100",
		Port:         80,
		URLScheme:    "http",
		SerialNumber: "TEST123",
		Model:        "S19",
		Manufacturer: "Bitmain",
		MacAddress:   "00:11:22:33:44:55",
	}
	// The plugin reports the rig is still on its factory password.
	active := true
	pairResult := expectedDeviceInfo
	pairResult.DefaultPasswordActive = &active

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Eq(expectedDeviceInfo), gomock.Any()).
		Return(pairResult, nil)

	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "proto"},
		Driver:     mockDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	manager.pluginsByDriverName["proto"] = mockPlugin

	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "TEST123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			MacAddress:       "00:11:22:33:44:55",
			DriverName:       "proto",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{Username: "admin", Password: stringPtr("proto")}

	ctx := t.Context()
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	)

	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), device.DeviceIdentifier, device.OrgID).Return(nil, fleeterror.NewNotFoundError("device not found"))
	deviceStore.EXPECT().GetPairedDeviceByMACAddress(gomock.Any(), gomock.Any(), device.OrgID, "").Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().GetPairedDeviceBySerialNumber(gomock.Any(), gomock.Any(), device.OrgID).Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().InitializeWorkerName(gomock.Any(), int64(1), models.DeviceIdentifier(device.DeviceIdentifier), "00:11:22:33:44:55").Return(true, nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	// Key assertions: DEFAULT_PASSWORD pairing state and normal successful initial status.
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "DEFAULT_PASSWORD").Return(nil)
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
	mockDevice.EXPECT().Close(gomock.Any()).Return(nil)
	mockDevice.EXPECT().
		DescribeDevice(gomock.Any()).
		Return(deviceInfo, sdk.Capabilities{}, nil)

	// Create mock driver
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq(expectedSecretBundle)).
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
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

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
	assert.Equal(t, "00:11:22:33:44:55", result.MacAddress)
}

func TestPairer_GetDeviceInfo_DefaultPasswordActiveFromNewDevice_ReturnsForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{}, status.Error(codes.PermissionDenied, "default password must be changed"))

	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password123"),
	}

	result, err := pairer.GetDeviceInfo(t.Context(), device, credentials)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
}

func TestPairer_GetDeviceInfo_DefaultPasswordActiveFromDescribeDevice_ReturnsForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDevice := sdkMocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Close(gomock.Any()).Return(nil)
	mockDevice.EXPECT().
		DescribeDevice(gomock.Any()).
		Return(sdk.DeviceInfo{}, sdk.Capabilities{}, status.Error(codes.PermissionDenied, "default password must be changed"))

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{Device: mockDevice}, nil)

	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password123"),
	}

	result, err := pairer.GetDeviceInfo(t.Context(), device, credentials)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
}

func TestPairer_GetDeviceInfo_GRPCUnauthenticatedFromNewDevice_ReturnsUnauthenticated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{}, status.Error(codes.Unauthenticated, "authentication failed"))

	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{Username: "admin", Password: stringPtr("password123")}

	result, err := pairer.GetDeviceInfo(t.Context(), device, credentials)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, fleeterror.IsAuthenticationError(err), "expected unauthenticated error, got: %v", err)
}

func TestPairer_GetDeviceInfo_GRPCUnauthenticatedFromDescribeDevice_ReturnsUnauthenticated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDevice := sdkMocks.NewMockDevice(ctrl)
	mockDevice.EXPECT().Close(gomock.Any()).Return(nil)
	mockDevice.EXPECT().
		DescribeDevice(gomock.Any()).
		Return(sdk.DeviceInfo{}, sdk.Capabilities{}, status.Error(codes.Unauthenticated, "authentication failed"))

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDeviceResult{Device: mockDevice}, nil)

	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "test-device",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{Username: "admin", Password: stringPtr("password123")}

	result, err := pairer.GetDeviceInfo(t.Context(), device, credentials)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, fleeterror.IsAuthenticationError(err), "expected unauthenticated error, got: %v", err)
}

func TestPairer_GetDeviceInfo_ProtoWithoutCredentialsRequiresCredentials(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDriver := sdkMocks.NewMockDriver(ctrl)

	mockPlugin := &LoadedPlugin{
		Name:       "proto-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "proto"},
		Driver:     mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["proto"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)

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

	ctx := t.Context()
	result, err := pairer.GetDeviceInfo(ctx, device, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "credentials required for secret bundle")
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

func (m *mockDriverWithDefaultCredentials) GetDefaultCredentials(_ context.Context, _, _ string) []sdk.UsernamePassword {
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
		MacAddress:      "00:11:22:33:44:55",
		FirmwareVersion: "1.0.0",
	}

	expectedSecretBundle := sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.UsernamePassword{
			Username: "root",
			Password: "root",
		},
	}

	// Create mock driver expecting default credentials
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Eq(expectedSecretBundle)).
		Return(expectedDeviceInfo, nil)
	// GetDeviceInfo is not called because PairDevice already returned a firmware version.

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
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "ANTMINER123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			MacAddress:       "00:11:22:33:44:55",
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
	deviceStore.EXPECT().GetPairedDeviceByMACAddress(gomock.Any(), gomock.Any(), device.OrgID, "").Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().GetPairedDeviceBySerialNumber(gomock.Any(), gomock.Any(), device.OrgID).Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().InitializeWorkerName(
		gomock.Any(), int64(1),
		models.DeviceIdentifier(device.DeviceIdentifier),
		"00:11:22:33:44:55",
	).Return(true, nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err, "Antminer should be paired successfully with default credentials")
	assert.Equal(t, "1.0.0", device.FirmwareVersion, "Firmware version should be populated from GetDeviceInfo")
}

func TestPairer_PairDevice_AntminerAutoCredentials_PreservesPairingFirmwareWhenDescribeOmitsIt(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	pairingDeviceInfo := sdk.DeviceInfo{
		Host:            "192.168.1.100",
		Port:            80,
		URLScheme:       "http",
		SerialNumber:    "ANTMINER123",
		Model:           "S19",
		Manufacturer:    "Bitmain",
		MacAddress:      "00:11:22:33:44:55",
		FirmwareVersion: "1.0.0",
	}

	expectedSecretBundle := sdk.SecretBundle{
		Version: "v1",
		Kind: sdk.UsernamePassword{
			Username: "root",
			Password: "root",
		},
	}

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Eq(expectedSecretBundle)).
		Return(pairingDeviceInfo, nil)
	// GetDeviceInfo is not called because PairDevice already returned a firmware version.

	driverWithCreds := &mockDriverWithDefaultCredentials{
		Driver: mockDriver,
		defaultCredentials: []sdk.UsernamePassword{
			{Username: "root", Password: "root"},
		},
	}

	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     driverWithCreds,
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-001",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			SerialNumber:     "ANTMINER123",
			Model:            "S19",
			Manufacturer:     "Bitmain",
			MacAddress:       "00:11:22:33:44:55",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	var credentials *pb.Credentials
	ctx := t.Context()

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	deviceStore.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), device.DeviceIdentifier, device.OrgID).Return(nil, fleeterror.NewNotFoundError("device not found"))
	deviceStore.EXPECT().GetPairedDeviceByMACAddress(gomock.Any(), gomock.Any(), device.OrgID, "").Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().GetPairedDeviceBySerialNumber(gomock.Any(), gomock.Any(), device.OrgID).Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().InitializeWorkerName(
		gomock.Any(), int64(1),
		models.DeviceIdentifier(device.DeviceIdentifier),
		device.MacAddress,
	).Return(true, nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err)
	assert.Equal(t, pairingDeviceInfo.FirmwareVersion, device.FirmwareVersion, "DescribeDevice should not erase firmware learned during PairDevice")
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
	gomock.InOrder(
		mockDriver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "root", Password: "root"}}).Return(sdk.DeviceInfo{}, sdk.NewErrorAuthenticationFailed("antminer-device-002")),
		mockDriver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "second"}}).Return(sdk.DeviceInfo{}, sdk.NewErrorAuthenticationFailed("antminer-device-002")),
	)

	// Wrap mock driver with default credentials provider
	driverWithCreds := &mockDriverWithDefaultCredentials{
		Driver: mockDriver,
		defaultCredentials: []sdk.UsernamePassword{
			{Username: "root", Password: "root"},
			{Username: "admin", Password: "second"},
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
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "network", err: fmt.Errorf("plugin pairing failed: connection timeout")},
		{name: "SDK unavailable", err: sdk.NewErrorDeviceUnavailable("antminer-device-003", fmt.Errorf("connection timeout"))},
		{name: "RPC unavailable", err: status.Error(codes.Unavailable, "connection timeout")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			manager := NewManager(&Config{})

			// Create mock driver that returns a network error (not an auth error)
			mockDriver := sdkMocks.NewMockDriver(ctrl)
			mockDriver.EXPECT().
				PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "root", Password: "root"}}).
				Return(sdk.DeviceInfo{}, tc.err).
				Times(1) // Should only be called once, not retried

			// Wrap mock driver with default credentials provider
			driverWithCreds := &mockDriverWithDefaultCredentials{
				Driver: mockDriver,
				defaultCredentials: []sdk.UsernamePassword{
					{Username: "root", Password: "root"},
					{Username: "admin", Password: "second"},
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
			assert.Contains(t, err.Error(), tc.err.Error(), "Unavailable error should be propagated")
			assert.NotContains(t, err.Error(), "credentials are required", "Should not convert to credentials error")
		})
	}
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
		MacAddress:   "AA:BB:CC:DD:EE:FF",
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
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

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
	deviceStore.EXPECT().GetPairedDeviceByMACAddress(gomock.Any(), gomock.Any(), device.OrgID, "").Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().GetPairedDeviceBySerialNumber(gomock.Any(), gomock.Any(), device.OrgID).Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().InsertDevice(gomock.Any(), &device.Device, device.OrgID, device.DeviceIdentifier).Return(nil)
	deviceStore.EXPECT().InitializeWorkerName(
		gomock.Any(), int64(1),
		models.DeviceIdentifier(device.DeviceIdentifier),
		"AA:BB:CC:DD:EE:FF",
	).Return(true, nil)
	deviceStore.EXPECT().UpsertMinerCredentials(gomock.Any(), &device.Device, device.OrgID, gomock.Any(), gomock.Any()).Return(nil)
	deviceStore.EXPECT().UpsertDevicePairing(gomock.Any(), &device.Device, device.OrgID, "PAIRED").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier(device.DeviceIdentifier), models.MinerStatusActive, "").Return(nil)

	err = pairer.PairDevice(ctx, device, credentials)

	require.NoError(t, err, "Antminer should be paired with explicit credentials")
}

func TestPairer_PairDevice_PermissionDeniedFromPlugin_ReturnsForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.DeviceInfo{}, status.Error(codes.PermissionDenied, "default password must be changed"))

	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-locked",
			IpAddress:        "192.168.1.150",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{Username: "admin", Password: stringPtr("custompass")}

	err := pairer.PairDevice(t.Context(), device, credentials)

	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
}

func TestPairer_PairDevice_GRPCUnauthenticatedFromPlugin_ReturnsUnauthenticated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.DeviceInfo{}, status.Error(codes.Unauthenticated, "authentication failed"))

	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     mockDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-bad-creds",
			IpAddress:        "192.168.1.151",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}
	credentials := &pb.Credentials{Username: "admin", Password: stringPtr("wrongpass")}

	err := pairer.PairDevice(t.Context(), device, credentials)

	require.Error(t, err)
	assert.True(t, fleeterror.IsAuthenticationError(err), "expected unauthenticated error, got: %v", err)
}

func TestPairer_PairDevice_ExplicitCredentials_UnavailableReturnsInternal(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "SDK unavailable", err: sdk.NewErrorDeviceUnavailable("proto-device-unavailable", fmt.Errorf("login connection timeout"))},
		{name: "RPC unavailable", err: status.Error(codes.Unavailable, "login connection timeout")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			manager := NewManager(&Config{})
			mockDriver := sdkMocks.NewMockDriver(ctrl)
			secret := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "custompass"}}
			mockDriver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), secret).Return(sdk.DeviceInfo{}, tc.err).Times(1)
			manager.pluginsByDriverName["proto"] = &LoadedPlugin{
				Name: "proto-plugin", Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: mockDriver,
				Caps: sdk.Capabilities{sdk.CapabilityPairing: true},
			}
			pairer := createTestPairer(ctrl, manager)
			device := &discoverymodels.DiscoveredDevice{
				Device: pb.Device{DeviceIdentifier: "proto-device-unavailable", IpAddress: "192.168.1.151", Port: "80", UrlScheme: "http", DriverName: "proto"}, OrgID: 1,
			}
			credentials := &pb.Credentials{Username: "admin", Password: stringPtr("custompass")}
			err := pairer.PairDevice(t.Context(), device, credentials)
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
			assert.Contains(t, err.Error(), tc.err.Error())
			assert.False(t, fleeterror.IsAuthenticationError(err), "an uncertain login must not request replacement credentials")
		})
	}
}

func TestPairer_PairDevice_DefaultPasswordViaAutoCredentials_ReturnsForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.DeviceInfo{}, status.Error(codes.PermissionDenied, "default password must be changed"))

	driverWithCreds := &mockDriverWithDefaultCredentials{
		Driver: mockDriver,
		defaultCredentials: []sdk.UsernamePassword{
			{Username: "root", Password: "root"},
			{Username: "admin", Password: "admin"},
		},
	}

	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     driverWithCreds,
		Caps:       sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	pairer := createTestPairer(ctrl, manager)
	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "antminer-device-default-pw",
			IpAddress:        "192.168.1.152",
			Port:             "80",
			DriverName:       "antminer",
		},
		OrgID: 1,
	}

	err := pairer.PairDevice(t.Context(), device, nil)

	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
}

func TestPairer_PerformReconciliation_DeviceClass(t *testing.T) {
	for _, tt := range []struct {
		name          string
		oldClass      commonpb.DeviceClass
		newClass      commonpb.DeviceClass
		expectedClass commonpb.DeviceClass
	}{
		{
			name:          "learns previously unspecified class",
			oldClass:      commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED,
			newClass:      commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
			expectedClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		},
		{
			name:          "updates known class",
			oldClass:      commonpb.DeviceClass_DEVICE_CLASS_RIG,
			newClass:      commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
			expectedClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		},
		{
			name:          "preserves known class when new class is unspecified",
			oldClass:      commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
			newClass:      commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED,
			expectedClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := stablePairingSnapshotStore(ctrl)
			pairer := &Pairer{discoveredDeviceStore: store}
			oldDOI := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "old-discovery", OrgID: 1}
			newDOI := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: "new-discovery", OrgID: 1}
			oldDevice := &discoverymodels.DiscoveredDevice{
				Device: pb.Device{DeviceIdentifier: oldDOI.DeviceIdentifier, DeviceClass: tt.oldClass},
				OrgID:  1,
			}
			newDevice := &discoverymodels.DiscoveredDevice{
				Device: pb.Device{DeviceIdentifier: newDOI.DeviceIdentifier, DeviceClass: tt.newClass},
				OrgID:  1,
			}
			store.EXPECT().GetDevice(gomock.Any(), oldDOI).Return(oldDevice, nil)
			store.EXPECT().Save(gomock.Any(), oldDOI, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ discoverymodels.DeviceOrgIdentifier, saved *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
					assert.Equal(t, tt.expectedClass, saved.DeviceClass)
					return saved, nil
				},
			)
			store.EXPECT().SoftDelete(gomock.Any(), newDOI).Return(nil)

			_, err := pairer.performReconciliation(t.Context(), newDevice, &stores.PairedDeviceInfo{
				DeviceIdentifier:           "paired-device",
				DiscoveredDeviceIdentifier: oldDOI.DeviceIdentifier,
			})
			require.NoError(t, err)
			assert.Equal(t, "paired-device", newDevice.DeviceIdentifier)
		})
	}
}

func TestPairer_HandlePairViaStore_ReconcilesAuthRetry(t *testing.T) {
	for _, byMAC := range []bool{false, true} {
		name := "serial fallback"
		if byMAC {
			name = "MAC match"
		}
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			manager := NewManager(&Config{})
			mockDriver := sdkMocks.NewMockDriver(ctrl)
			manager.pluginsByDriverName["antminer"] = &LoadedPlugin{
				Name:       "antminer-plugin",
				Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
				Driver:     mockDriver,
				Caps: sdk.Capabilities{
					sdk.CapabilityPairing: true,
				},
			}

			transactor := mocks.NewMockTransactor(ctrl)
			discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
			deviceStore := mocks.NewMockDeviceStore(ctrl)
			encryptService, err := encrypt.NewService(&encrypt.Config{
				ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			})
			require.NoError(t, err)

			pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

			device := &discoverymodels.DiscoveredDevice{
				Device: pb.Device{
					DeviceIdentifier: "retry-id",
					IpAddress:        "192.168.1.100",
					Port:             "80",
					UrlScheme:        "http",
					DriverName:       "antminer",
					MacAddress:       "AA:BB:CC:DD:EE:FF",
					SerialNumber:     "SN-001",
				},
				OrgID: 1,
			}
			credentials := &pb.Credentials{
				Username: "admin",
				Password: stringPtr("password"),
			}

			ctx := t.Context()

			transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, fn func(context.Context) error) error {
					return fn(ctx)
				},
			)

			deviceStore.EXPECT().
				GetDeviceByDeviceIdentifier(gomock.Any(), "retry-id", int64(1)).
				Return(&pb.Device{DeviceIdentifier: "retry-id"}, nil)
			deviceStore.EXPECT().LockDeviceForCloudRecoveryByIdentifier(gomock.Any(), "retry-id", int64(1)).Return(true, nil)
			deviceStore.EXPECT().GetDevicePairingStatusByIdentifier(gomock.Any(), "retry-id", int64(1)).Return(pairing.StatusAuthenticationNeeded, nil)
			deviceStore.EXPECT().GetMinerCredentials(gomock.Any(), gomock.Any(), int64(1)).Return(nil, fleeterror.NewNotFoundError("pending credentials"))
			if byMAC {

				deviceStore.EXPECT().
					GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1), "retry-id").
					Return(&stores.PairedDeviceInfo{DeviceIdentifier: "paired-id", DiscoveredDeviceIdentifier: "paired-discovered-id",
						MacAddress: "AA:BB:CC:DD:EE:FF", SerialNumber: "SN-001"}, nil)

			} else {

				deviceStore.EXPECT().
					GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1), "retry-id").
					Return(nil, fleeterror.NewNotFoundError("no other miner with this MAC"))
				deviceStore.EXPECT().
					GetPairedDeviceBySerialNumber(gomock.Any(), "SN-001", int64(1)).
					Return(&stores.PairedDeviceInfo{
						DeviceIdentifier:           "paired-id",
						DiscoveredDeviceIdentifier: "paired-discovered-id",
						MacAddress:                 "AA:BB:CC:DD:EE:FF",
						SerialNumber:               "SN-001",
					}, nil)
			}

			discoveredDeviceStore.EXPECT().
				GetDevice(gomock.Any(), discoverymodels.DeviceOrgIdentifier{
					DeviceIdentifier: "paired-discovered-id",
					OrgID:            1,
				}).
				Return(&discoverymodels.DiscoveredDevice{
					Device: pb.Device{
						DeviceIdentifier: "paired-discovered-id",
						IpAddress:        "192.168.1.10",
						Port:             "80",
						UrlScheme:        "http",
						DriverName:       "antminer",
					},
					OrgID: 1,
				}, nil)
			discoveredDeviceStore.EXPECT().
				Save(gomock.Any(), discoverymodels.DeviceOrgIdentifier{
					DeviceIdentifier: "paired-discovered-id",
					OrgID:            1,
				}, gomock.Any()).
				DoAndReturn(func(_ context.Context, _ discoverymodels.DeviceOrgIdentifier, updated *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
					require.Equal(t, "192.168.1.100", updated.IpAddress)
					require.Equal(t, "80", updated.Port)
					require.Equal(t, "http", updated.UrlScheme)
					return updated, nil
				})
			discoveredDeviceStore.EXPECT().
				SoftDelete(gomock.Any(), discoverymodels.DeviceOrgIdentifier{
					DeviceIdentifier: "retry-id",
					OrgID:            1,
				}).
				Return(nil)

			deviceStore.EXPECT().
				UpdateDeviceInfo(gomock.Any(), gomock.Any(), int64(1)).
				DoAndReturn(func(_ context.Context, updated *pb.Device, _ int64) error {
					require.Equal(t, "paired-id", updated.DeviceIdentifier)
					require.Equal(t, "SN-001", updated.SerialNumber)
					return nil
				})
			deviceStore.EXPECT().
				InitializeWorkerName(gomock.Any(), int64(1), models.DeviceIdentifier("paired-id"), "AA:BB:CC:DD:EE:FF").Return(true, nil)
			deviceStore.EXPECT().
				UpsertMinerCredentials(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, updated *pb.Device, _ int64, _ string, _ any) error {
					require.Equal(t, "paired-id", updated.DeviceIdentifier)
					return nil
				})
			deviceStore.EXPECT().
				UpsertDevicePairing(gomock.Any(), gomock.Any(), int64(1), "PAIRED").
				DoAndReturn(func(_ context.Context, updated *pb.Device, _ int64, _ string) error {
					require.Equal(t, "paired-id", updated.DeviceIdentifier)
					return nil
				})
			deviceStore.EXPECT().
				UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier("paired-id"), models.MinerStatusActive, "").
				Return(nil)

			err = pairer.handlePairViaStore(ctx, device, &device.Device, device.DiscoveredByFleetNodeID, credentials, manager.pluginsByDriverName["antminer"])
			require.NoError(t, err)
			require.Equal(t, "paired-id", device.DeviceIdentifier)

		})
	}
}

func TestPairer_HandlePairViaStore_PreservesExistingWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	transactor := mocks.NewMockTransactor(ctrl)
	discoveredDeviceStore := stablePairingSnapshotStore(ctrl)
	deviceStore := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	manager.pluginsByDriverName["antminer"] = &LoadedPlugin{
		Name:       "antminer-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
	}

	pairer := NewPairer(manager, transactor, discoveredDeviceStore, deviceStore, encryptService)

	device := &discoverymodels.DiscoveredDevice{
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
	credentials := &pb.Credentials{
		Username: "admin",
		Password: stringPtr("password"),
	}

	ctx := t.Context()

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)

	deviceStore.EXPECT().
		GetDeviceByDeviceIdentifier(gomock.Any(), "device-123", int64(1)).
		Return(&pb.Device{DeviceIdentifier: "device-123"}, nil)
	deviceStore.EXPECT().LockDeviceForCloudRecoveryByIdentifier(gomock.Any(), "device-123", int64(1)).Return(true, nil)
	deviceStore.EXPECT().GetDevicePairingStatusByIdentifier(gomock.Any(), "device-123", int64(1)).Return(pairing.StatusPaired, nil)
	deviceStore.EXPECT().
		GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1), gomock.Any()).
		Return(nil, fleeterror.NewNotFoundError("no paired device"))
	deviceStore.EXPECT().
		UpdateDeviceInfo(gomock.Any(), gomock.Any(), int64(1)).
		Return(nil)
	deviceStore.EXPECT().InitializeWorkerName(gomock.Any(), int64(1), models.DeviceIdentifier("device-123"), "AA:BB:CC:DD:EE:FF").Return(false, nil)
	deviceStore.EXPECT().
		UpsertMinerCredentials(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).
		Return(nil)
	deviceStore.EXPECT().
		UpsertDevicePairing(gomock.Any(), gomock.Any(), int64(1), "PAIRED").
		Return(nil)
	deviceStore.EXPECT().
		UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier("device-123"), models.MinerStatusActive, "").
		Return(nil)

	err = pairer.handlePairViaStore(ctx, device, &device.Device, device.DiscoveredByFleetNodeID, credentials, manager.pluginsByDriverName["antminer"])
	require.NoError(t, err)
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

func TestContainerAnonymousPairingDoesNotStoreRigCredentials(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := sdkMocks.NewMockDriver(ctrl)
	manager := NewManager(&Config{})
	manager.pluginsByDriverName["proto"] = &LoadedPlugin{Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true, sdk.CapabilityBasicAuth: true, sdk.CapabilityPoolConfig: true}}
	transactor := mocks.NewMockTransactor(ctrl)
	store := mocks.NewMockDeviceStore(ctrl)
	pairer := NewPairer(manager, transactor, stablePairingSnapshotStore(ctrl), store, nil)
	device := &discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Port: "8080", IpAddress: "192.0.2.1", UrlScheme: "http"}, OrgID: 1}
	driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1"}).Return(sdk.DeviceInfo{DeviceClass: sdk.DeviceClassContainer, SerialNumber: "controller-a", Model: "CT1"}, nil)
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	store.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), "ct1", int64(1)).Return(nil, fleeterror.NewNotFoundError("missing"))
	store.EXPECT().InsertDevice(gomock.Any(), gomock.Any(), int64(1), "ct1").Return(nil)
	store.EXPECT().UpsertDevicePairing(gomock.Any(), gomock.Any(), int64(1), "PAIRED").Return(nil)
	store.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier("ct1"), models.MinerStatusActive, "").Return(nil)
	// No controller-identity reconciliation, credential, worker-name, or pool lookups are allowed.
	require.NoError(t, pairer.PairDevice(t.Context(), device, nil))
}

// Recovery scans probe multiple unpersisted candidates concurrently. Their SDK
// handles must be distinct even though both candidates have an empty identifier.
func TestPairer_GetDeviceInfo_ConcurrentDiscoveryProbesUseDistinctHandles(t *testing.T) {
	ctrl := gomock.NewController(t)
	manager := NewManager(&Config{})
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	handles := make(chan string, 2)
	mockDriver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, handleID string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			handles <- handleID
			mockDevice := sdkMocks.NewMockDevice(ctrl)
			mockDevice.EXPECT().DescribeDevice(gomock.Any()).Return(info, sdk.Capabilities{}, nil)
			mockDevice.EXPECT().Close(gomock.Any()).Return(nil)
			return sdk.NewDeviceResult{Device: mockDevice}, nil
		})
	manager.pluginsByDriverName["proto"] = &LoadedPlugin{
		Name: "proto", Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: mockDriver,
		Caps: sdk.Capabilities{sdk.CapabilityPairing: true},
	}
	pairer := createTestPairer(ctrl, manager)
	credentials := &pb.Credentials{Username: "admin", Password: stringPtr("proto")}
	type outcome struct {
		device *pb.Device
		err    error
	}
	results := make(chan outcome, 2)
	for _, address := range []string{"192.168.94.4", "192.168.94.15"} {
		candidate := &discoverymodels.DiscoveredDevice{Device: pb.Device{
			IpAddress: address, Port: "8080", UrlScheme: "http", DriverName: "proto", SerialNumber: address,
		}}
		go func() {
			device, err := pairer.GetDeviceInfo(t.Context(), candidate, credentials)
			results <- outcome{device, err}
		}()
	}
	seen := make(map[string]bool)
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		require.Equal(t, result.device.IpAddress, result.device.SerialNumber)
		seen[result.device.IpAddress] = true
	}
	require.Len(t, seen, 2)
	first, second := <-handles, <-handles
	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second, "concurrent probes must not overwrite a shared SDK registry entry")
}

type probeCleanupDriver struct {
	sdk.Driver
	close func(context.Context, string) error
}

func (d *probeCleanupDriver) CloseDevice(ctx context.Context, id string) error {
	return d.close(ctx, id)
}

func TestPairer_GetDeviceInfo_UncertainNewDeviceCleanup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		cancel      bool
		wantCleanup bool
	}{
		{"context cancellation", context.Canceled, true, true},
		{"context deadline", context.DeadlineExceeded, false, true},
		{"gRPC cancellation", status.Error(codes.Canceled, "reply lost"), false, true},
		{"gRPC deadline", status.Error(codes.DeadlineExceeded, "reply lost"), false, true},
		{"canceled parent with wrapped driver error", fmt.Errorf("transport closed"), true, true},
		{"authentication failure", status.Error(codes.Unauthenticated, "bad password"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockDriver := sdkMocks.NewMockDriver(ctrl)
			var probeID string
			mockDriver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				probeID = id
				return sdk.NewDeviceResult{}, tc.err
			})
			calls := 0
			driver := &probeCleanupDriver{Driver: mockDriver, close: func(ctx context.Context, id string) error {
				calls++
				require.NoError(t, ctx.Err(), "cleanup must outlive the canceled request")
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), time.Second)
				require.Equal(t, probeID, id)
				require.Contains(t, id, "pairing-info:")
				if calls == 1 {
					return status.Error(codes.NotFound, "registration still in flight")
				}
				return nil
			}}
			manager := NewManager(&Config{})
			manager.pluginsByDriverName["proto"] = &LoadedPlugin{Driver: driver, Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}
			pairer := createTestPairer(ctrl, manager)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			_, err := pairer.GetDeviceInfo(ctx, &discoverymodels.DiscoveredDevice{Device: pb.Device{DriverName: "proto"}}, &pb.Credentials{Username: "admin", Password: stringPtr("password")})
			require.Error(t, err)
			if tc.wantCleanup {
				require.Equal(t, 2, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}

func TestCloseUncertainProbeIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		closeUncertainProbe(t.Context(), &probeCleanupDriver{close: func(context.Context, string) error { calls++; return status.Error(codes.Unavailable, "plugin stopped") }}, "probe")
		require.Equal(t, time.Second, time.Since(start))
		require.Greater(t, calls, 1)
	})
}

// A controller that rejects anonymous pairing needs the operator's token. It is
// reported like a rig with no working defaults so the device lands in
// AUTHENTICATION_NEEDED instead of failing to pair.
func TestContainerRequiringTokenWithoutCredentialsNeedsAuthentication(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := sdkMocks.NewMockDriver(ctrl)
	manager := NewManager(&Config{})
	manager.pluginsByDriverName["proto"] = &LoadedPlugin{Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}
	pairer := NewPairer(manager, mocks.NewMockTransactor(ctrl), stablePairingSnapshotStore(ctrl), mocks.NewMockDeviceStore(ctrl), nil)
	device := &discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Port: "8080", IpAddress: "192.0.2.1", UrlScheme: "http"}, OrgID: 1}
	driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1"}).Return(sdk.DeviceInfo{}, status.Error(codes.Unauthenticated, "authentication failed"))

	err := pairer.PairDevice(t.Context(), device, nil)
	require.ErrorContains(t, err, "credentials are required for pairing")
	require.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestContainerTokenIsStoredEncryptedWithEmptyUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := sdkMocks.NewMockDriver(ctrl)
	manager := NewManager(&Config{})
	manager.pluginsByDriverName["proto"] = &LoadedPlugin{Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}
	transactor := mocks.NewMockTransactor(ctrl)
	store := mocks.NewMockDeviceStore(ctrl)
	encryptService, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="})
	require.NoError(t, err)
	pairer := NewPairer(manager, transactor, stablePairingSnapshotStore(ctrl), store, encryptService)
	device := &discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Port: "8080", IpAddress: "192.0.2.1", UrlScheme: "http"}, OrgID: 1}
	const token = "0123456789abcdef"
	driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Password: token}}).Return(sdk.DeviceInfo{DeviceClass: sdk.DeviceClassContainer, SerialNumber: "controller-a", Model: "CT1"}, nil)
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	store.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), "ct1", int64(1)).Return(nil, fleeterror.NewNotFoundError("missing"))
	store.EXPECT().InsertDevice(gomock.Any(), gomock.Any(), int64(1), "ct1").Return(nil)
	store.EXPECT().UpsertMinerCredentials(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *pb.Device, _ int64, usernameEnc string, passwordEnc *secrets.Text) error {
			require.NotEmpty(t, usernameEnc, "an empty username still stores ciphertext")
			require.NotContains(t, passwordEnc.Value(), token)
			username, err := encryptService.Decrypt(usernameEnc)
			require.NoError(t, err)
			require.Empty(t, username)
			password, err := encryptService.Decrypt(passwordEnc.Value())
			require.NoError(t, err)
			require.Equal(t, token, string(password))
			return nil
		})
	store.EXPECT().UpsertDevicePairing(gomock.Any(), gomock.Any(), int64(1), "PAIRED").Return(nil)
	store.EXPECT().UpsertDeviceStatus(gomock.Any(), models.DeviceIdentifier("ct1"), models.MinerStatusActive, "").Return(nil)

	require.NoError(t, pairer.PairDevice(t.Context(), device, &pb.Credentials{Password: stringPtr(token)}))
}

func TestProtectedHAControllerIsNotPairedYet(t *testing.T) {
	ctrl := gomock.NewController(t)
	driver := sdkMocks.NewMockDriver(ctrl)
	manager := NewManager(&Config{})
	manager.pluginsByDriverName["proto"] = &LoadedPlugin{Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}
	// No transaction or store calls: nothing is saved for an unrecorded HA pair.
	pairer := NewPairer(manager, mocks.NewMockTransactor(ctrl), stablePairingSnapshotStore(ctrl), mocks.NewMockDeviceStore(ctrl), nil)
	device := &discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Port: "8080", IpAddress: "192.0.2.1", UrlScheme: "http", RpcPort: 9095}, OrgID: 1}
	const token = "0123456789abcdef"
	members := []string{"controller-a", "controller-b"}
	driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Password: token}}).Return(sdk.DeviceInfo{
		DeviceClass: sdk.DeviceClassContainer, SerialNumber: "controller-a", Model: "CT1",
		Endpoints: []sdk.DeviceEndpoint{{ID: "controller-a", DeviceKey: "ha-1", MemberIDs: members, Host: "192.0.2.1", Port: 8080, URLScheme: "http", RPCPort: 9095}},
	}, nil)

	err := pairer.PairDevice(t.Context(), device, &pb.Credentials{Password: stringPtr(token)})
	require.ErrorContains(t, err, "Fleet cannot pair token-protected HA pairs yet")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.NotContains(t, err.Error(), token)
}
