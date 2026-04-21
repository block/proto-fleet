package plugins

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewMultiTypeDiscoverer(t *testing.T) {
	manager := NewManager(&Config{})

	discoverer := NewMultiTypeDiscoverer(manager)

	assert.NotNil(t, discoverer)
	assert.Equal(t, manager, discoverer.manager)
}

func TestMultiTypeDiscoverer_Discover_NoPlugins(t *testing.T) {
	manager := NewManager(&Config{})
	discoverer := NewMultiTypeDiscoverer(manager)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugins available for discovery")
}

func TestMultiTypeDiscoverer_Discover_NoDiscoveryCapablePlugins(t *testing.T) {
	manager := NewManager(&Config{})

	// Add mock plugin without discovery capability
	mockPlugin := &LoadedPlugin{
		Name:       "test-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true, // Has pairing but not discovery
		},
	}
	manager.plugins["test-plugin"] = mockPlugin

	discoverer := NewMultiTypeDiscoverer(manager)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugins with discovery capability available")
}

func TestMultiTypeDiscoverer_Discover_Success(t *testing.T) {
	// Test different device info fields to ensure discovery works correctly
	testCases := []struct {
		name       string
		deviceInfo sdk.DeviceInfo
	}{
		{
			name: "ASIC device discovered by Antminer plugin",
			deviceInfo: sdk.DeviceInfo{
				SerialNumber: "TEST123",
				Model:        "S19",
				Manufacturer: "Bitmain",
				URLScheme:    "http",
				MacAddress:   "00:11:22:33:44:55",
			},
		},
		{
			name: "Bitmain manufacturer",
			deviceInfo: sdk.DeviceInfo{
				SerialNumber: "BITMAIN123",
				Model:        "S19 Pro",
				Manufacturer: "Bitmain",
				URLScheme:    "https",
				MacAddress:   "00:11:22:33:44:66",
			},
		},
		{
			name: "Antminer model detection",
			deviceInfo: sdk.DeviceInfo{
				SerialNumber: "MODEL123",
				Model:        "Antminer S19 Pro",
				Manufacturer: "",
				URLScheme:    "http",
				MacAddress:   "00:11:22:33:44:88",
			},
		},
		{
			name: "Unknown device info uses plugin driver name",
			deviceInfo: sdk.DeviceInfo{
				SerialNumber: "UNKNOWN123",
				Model:        "Unknown Model",
				Manufacturer: "Unknown Manufacturer",
				URLScheme:    "http",
				MacAddress:   "00:11:22:33:44:99",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create fresh manager for each test
			manager := NewManager(&Config{})

			// Create mock driver with expectations
			mockDriver := mocks.NewMockDriver(ctrl)
			mockDriver.EXPECT().
				DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
				Return(tc.deviceInfo, nil)

			// Add mock plugin with discovery capability
			mockPlugin := &LoadedPlugin{
				Name:       "test-plugin",
				Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
				Driver:     mockDriver,
				Caps: sdk.Capabilities{
					sdk.CapabilityDiscovery: true,
				},
			}
			manager.plugins["test-plugin"] = mockPlugin

			discoverer := NewMultiTypeDiscoverer(manager)

			ctx := t.Context()
			device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

			require.NoError(t, err)
			require.NotNil(t, device)

			assert.Equal(t, "antminer", device.DriverName)
			assert.Equal(t, "192.168.1.100", device.IpAddress)
			assert.Equal(t, "80", device.Port)
			assert.Equal(t, tc.deviceInfo.SerialNumber, device.SerialNumber)
			assert.Equal(t, tc.deviceInfo.Model, device.Model)
			assert.Equal(t, tc.deviceInfo.Manufacturer, device.Manufacturer)
			assert.Equal(t, tc.deviceInfo.MacAddress, device.MacAddress)
			assert.Equal(t, tc.deviceInfo.URLScheme, device.UrlScheme)
			assert.Equal(t, int64(0), device.OrgID)
			assert.False(t, device.FirstDiscovered.IsZero())
			assert.False(t, device.LastSeen.IsZero())
		})
	}
}

func TestMultiTypeDiscoverer_Discover_FirstPluginFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Create successful mock device info
	successDeviceInfo := sdk.DeviceInfo{
		SerialNumber: "SUCCESS123",
		Model:        "S19",
		Manufacturer: "Bitmain",
		URLScheme:    "http",
		MacAddress:   "00:11:22:33:44:55",
	}

	// Create failing mock driver - use AnyTimes() since iteration order is not guaranteed
	failingDriver := mocks.NewMockDriver(ctrl)
	failingDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(sdk.DeviceInfo{}, fleeterror.NewInternalError("mock discovery failure")).
		AnyTimes()

	// Create successful mock driver - use AnyTimes() since iteration order is not guaranteed
	successDriver := mocks.NewMockDriver(ctrl)
	successDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(successDeviceInfo, nil).
		AnyTimes()

	// Add failing plugin
	failingPlugin := &LoadedPlugin{
		Name:       "failing-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "whatsminer"},
		Driver:     failingDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
	}
	manager.plugins["failing-plugin"] = failingPlugin

	// Add successful plugin
	successPlugin := &LoadedPlugin{
		Name:       "success-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     successDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
	}
	manager.plugins["success-plugin"] = successPlugin

	discoverer := NewMultiTypeDiscoverer(manager)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	require.NoError(t, err)
	require.NotNil(t, device)

	// Should get the successful discovery result
	assert.Equal(t, "SUCCESS123", device.SerialNumber)
}

func TestMultiTypeDiscoverer_Discover_AllPluginsFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	manager := NewManager(&Config{})

	failingDriver1 := mocks.NewMockDriver(ctrl)
	failingDriver1.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(sdk.DeviceInfo{}, fleeterror.NewInternalError("plugin1 failure")).
		AnyTimes()

	failingDriver2 := mocks.NewMockDriver(ctrl)
	failingDriver2.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(sdk.DeviceInfo{}, fleeterror.NewInternalError("plugin2 failure")).
		AnyTimes()

	manager.plugins["failing-plugin-1"] = &LoadedPlugin{
		Name:       "failing-plugin-1",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     failingDriver1,
		Caps:       sdk.Capabilities{sdk.CapabilityDiscovery: true},
	}
	manager.plugins["failing-plugin-2"] = &LoadedPlugin{
		Name:       "failing-plugin-2",
		Identifier: sdk.DriverIdentifier{DriverName: "whatsminer"},
		Driver:     failingDriver2,
		Caps:       sdk.Capabilities{sdk.CapabilityDiscovery: true},
	}

	discoverer := NewMultiTypeDiscoverer(manager)

	// Act
	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	// Assert
	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all plugin discovery attempts failed")
}

func TestMultiTypeDiscoverer_Discover_ParallelExecution_FastPluginWins(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	manager := NewManager(&Config{})

	fastDeviceInfo := sdk.DeviceInfo{
		SerialNumber: "FAST123",
		Model:        "Fast Model",
		Manufacturer: "Fast Manufacturer",
		URLScheme:    "http",
		MacAddress:   "00:11:22:33:44:55",
	}

	slowDeviceInfo := sdk.DeviceInfo{
		SerialNumber: "SLOW123",
		Model:        "Slow Model",
		Manufacturer: "Slow Manufacturer",
		URLScheme:    "http",
		MacAddress:   "00:11:22:33:44:66",
	}

	var slowPluginCanceled atomic.Bool

	fastDriver := mocks.NewMockDriver(ctrl)
	fastDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(fastDeviceInfo, nil).
		AnyTimes()

	slowDriver := mocks.NewMockDriver(ctrl)
	slowDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		DoAndReturn(func(ctx context.Context, ip, port string) (sdk.DeviceInfo, error) {
			select {
			case <-ctx.Done():
				slowPluginCanceled.Store(true)
				return sdk.DeviceInfo{}, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return slowDeviceInfo, nil
			}
		}).
		AnyTimes()

	manager.plugins["fast-plugin"] = &LoadedPlugin{
		Name:       "fast-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     fastDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityDiscovery: true},
	}
	manager.plugins["slow-plugin"] = &LoadedPlugin{
		Name:       "slow-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "whatsminer"},
		Driver:     slowDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityDiscovery: true},
	}

	discoverer := NewMultiTypeDiscoverer(manager)

	// Act
	ctx := t.Context()
	start := time.Now()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")
	elapsed := time.Since(start)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, device)
	assert.Equal(t, "FAST123", device.SerialNumber)
	assert.Less(t, elapsed, 100*time.Millisecond, "Discovery should complete before slow plugin timeout")

	time.Sleep(50 * time.Millisecond)
	assert.True(t, slowPluginCanceled.Load(), "Slow plugin should have been canceled")
}

func TestMultiTypeDiscoverer_Discover_ContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	manager := NewManager(&Config{})

	blockingDriver := mocks.NewMockDriver(ctrl)
	blockingDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		DoAndReturn(func(ctx context.Context, ip, port string) (sdk.DeviceInfo, error) {
			<-ctx.Done()
			return sdk.DeviceInfo{}, ctx.Err()
		}).
		AnyTimes()

	manager.plugins["blocking-plugin"] = &LoadedPlugin{
		Name:       "blocking-plugin",
		Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
		Driver:     blockingDriver,
		Caps:       sdk.Capabilities{sdk.CapabilityDiscovery: true},
	}

	discoverer := NewMultiTypeDiscoverer(manager)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Act
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	// Assert
	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery canceled")
}

func TestMultiTypeDiscoverer_Discover_ParallelExecution_VerifyConcurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	manager := NewManager(&Config{})

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	deviceInfo := sdk.DeviceInfo{
		SerialNumber: "TEST123",
		Model:        "Test Model",
		Manufacturer: "Test Manufacturer",
		URLScheme:    "http",
		MacAddress:   "00:11:22:33:44:55",
	}

	createConcurrencyTrackingDriver := func() *mocks.MockDriver {
		driver := mocks.NewMockDriver(ctrl)
		driver.EXPECT().
			DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
			DoAndReturn(func(ctx context.Context, ip, port string) (sdk.DeviceInfo, error) {
				current := concurrentCount.Add(1)
				for {
					maxVal := maxConcurrent.Load()
					if current > maxVal {
						if maxConcurrent.CompareAndSwap(maxVal, current) {
							break
						}
					} else {
						break
					}
				}

				time.Sleep(20 * time.Millisecond)

				concurrentCount.Add(-1)
				return deviceInfo, nil
			}).
			AnyTimes()
		return driver
	}

	for i := range 3 {
		name := string(rune('a'+i)) + "-plugin"
		manager.plugins[name] = &LoadedPlugin{
			Name:       name,
			Identifier: sdk.DriverIdentifier{DriverName: name},
			Driver:     createConcurrencyTrackingDriver(),
			Caps:       sdk.Capabilities{sdk.CapabilityDiscovery: true},
		}
	}

	discoverer := NewMultiTypeDiscoverer(manager)

	// Act
	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	// Assert
	require.NoError(t, err)
	require.NotNil(t, device)
	assert.GreaterOrEqual(t, maxConcurrent.Load(), int32(2), "Plugins should run concurrently")
}
