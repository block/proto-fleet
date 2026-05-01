// Package miners contains shared contract assertions for plugin contract tests.
//
// Each Assert* function validates a specific domain of the plugin contract
// (discovery, pairing, telemetry, etc.) using the Go SDK client. These
// functions are plugin-agnostic — they work identically for any miner/firmware
// combo, driven by the manifest and capabilities.
package miners

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/tests/plugin-contract/mockapi"
)

type TestContext struct {
	Driver    sdk.Driver
	Manifest  Manifest
	MinerIP   string
	MinerPort string
}

// TestCtx returns a 30-second context tied to the test's cleanup.
func TestCtx(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestSecret returns the default admin credentials used in contract tests.
func TestSecret() sdk.SecretBundle {
	return sdk.SecretBundle{
		Version: "1",
		Kind:    sdk.UsernamePassword{Username: "admin", Password: "admin"},
	}
}

func AssertDiscovery(t *testing.T, tc TestContext) {
	t.Helper()

	t.Run("discovers_device_with_correct_info", func(t *testing.T) {
		// Act
		info, err := tc.Driver.DiscoverDevice(TestCtx(t), tc.MinerIP, tc.MinerPort)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, tc.MinerIP, info.Host)
		assert.Equal(t, tc.Manifest.Model, info.Model)
		assert.Equal(t, tc.Manifest.Manufacturer, info.Manufacturer)
	})

	t.Run("unsupported_port_returns_not_found", func(t *testing.T) {
		// Act
		_, err := tc.Driver.DiscoverDevice(TestCtx(t), tc.MinerIP, "9999")

		// Assert
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok, "expected gRPC status error")
		assert.Equal(t, codes.NotFound, s.Code())
	})
}

func AssertPairing(t *testing.T, tc TestContext) {
	t.Helper()

	t.Run("pair_returns_mac_and_firmware", func(t *testing.T) {
		ctx := TestCtx(t)

		// Arrange
		info, err := tc.Driver.DiscoverDevice(ctx, tc.MinerIP, tc.MinerPort)
		require.NoError(t, err)

		secret := TestSecret()

		// Act
		paired, err := tc.Driver.PairDevice(ctx, info, secret)

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, paired.MacAddress, "pairing should return MAC address")
		assert.Equal(t, tc.Manifest.FirmwareVersion, paired.FirmwareVersion)
	})

	t.Run("default_credentials_returns_entries", func(t *testing.T) {
		// Act
		provider, ok := tc.Driver.(sdk.DefaultCredentialsProvider)
		if !ok {
			t.Skip("driver does not implement DefaultCredentialsProvider")
		}
		creds := provider.GetDefaultCredentials(TestCtx(t), "", "")

		// Assert
		assert.NotEmpty(t, creds, "should return at least one default credential")
	})
}

func AssertTelemetry(t *testing.T, tc TestContext, device sdk.Device) {
	t.Helper()

	t.Run("status_returns_valid_metrics", func(t *testing.T) {
		// Act
		metrics, err := device.Status(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, metrics.DeviceID)

		assert.True(t,
			metrics.HashrateHS != nil || metrics.PowerW != nil || metrics.TempC != nil,
			"at least one core metric (hashrate, power, temperature) should be present")
		if metrics.HashrateHS != nil {
			assert.Greater(t, metrics.HashrateHS.Value, 0.0, "hashrate should be positive")
		}
		if metrics.PowerW != nil {
			assert.Greater(t, metrics.PowerW.Value, 0.0, "power should be positive")
		}
		if metrics.TempC != nil {
			assert.Greater(t, metrics.TempC.Value, 0.0, "temperature should be positive")
		}
	})

	t.Run("status_has_hashboards", func(t *testing.T) {
		// Act
		metrics, err := device.Status(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, metrics.HashBoards, "should have at least one hashboard")

		for i, hb := range metrics.HashBoards {
			if hb.HashRateHS != nil {
				assert.Greater(t, hb.HashRateHS.Value, 0.0, "hashboard %d hashrate should be positive", i)
			}
		}
	})

	t.Run("status_has_fan_metrics", func(t *testing.T) {
		// Act
		metrics, err := device.Status(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, metrics.FanMetrics, "should have fan metrics")
	})

	t.Run("health_is_healthy", func(t *testing.T) {
		// Act
		metrics, err := device.Status(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.True(t,
			metrics.Health == sdk.HealthHealthyActive || metrics.Health == sdk.HealthHealthyInactive,
			"a mining device should report a healthy state, got %d", metrics.Health)
	})
}

func AssertControl(t *testing.T, tc TestContext, device sdk.Device, caps sdk.Capabilities) {
	t.Helper()

	controlTests := []struct {
		cap  string
		name string
		fn   func(context.Context) error
	}{
		{"reboot", "reboot_succeeds", func(ctx context.Context) error { return device.Reboot(ctx) }},
		{"mining_start", "start_mining_succeeds", func(ctx context.Context) error { return device.StartMining(ctx) }},
		{"mining_stop", "stop_mining_succeeds", func(ctx context.Context) error { return device.StopMining(ctx) }},
		{"led_blink", "blink_led_succeeds", func(ctx context.Context) error { return device.BlinkLED(ctx) }},
	}
	for _, ct := range controlTests {
		if caps[ct.cap] {
			t.Run(ct.name, func(t *testing.T) {
				// Act + Assert
				assert.NoError(t, ct.fn(TestCtx(t)))
			})
		}
	}
}

func AssertConfiguration(t *testing.T, tc TestContext, device sdk.Device, caps sdk.Capabilities) {
	t.Helper()

	t.Run("get_mining_pools_returns_pools", func(t *testing.T) {
		// Act
		pools, err := device.GetMiningPools(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, pools, "should have at least one configured pool")
		if len(pools) > 0 {
			assert.NotEmpty(t, pools[0].URL, "first pool should have a URL")
		}
	})

	t.Run("update_mining_pools_succeeds", func(t *testing.T) {
		// Arrange
		pools := []sdk.MiningPoolConfig{
			{Priority: 0, URL: "stratum+tcp://pool.test.com:3333", WorkerName: "test.worker"},
		}

		// Act
		err := device.UpdateMiningPools(TestCtx(t), pools)

		// Assert
		assert.NoError(t, err)
	})

	if caps["power_mode_efficiency"] {
		t.Run("set_power_target_succeeds", func(t *testing.T) {
			// Act
			err := device.SetPowerTarget(TestCtx(t), sdk.PerformanceModeEfficiency)

			// Assert
			assert.NoError(t, err)
		})
	}
}

func AssertErrors(t *testing.T, tc TestContext, device sdk.Device) {
	t.Helper()

	t.Run("get_errors_returns_device_errors", func(t *testing.T) {
		// Act
		errs, err := device.GetErrors(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, errs.DeviceID, "device errors should include device ID")
	})
}

// SetupDevice performs discover → pair → newDevice and registers cleanup.
func SetupDevice(t *testing.T, tc TestContext, deviceID string) (sdk.Device, sdk.Capabilities) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Act
	info, err := tc.Driver.DiscoverDevice(ctx, tc.MinerIP, tc.MinerPort)
	require.NoError(t, err)

	paired, err := tc.Driver.PairDevice(ctx, info, TestSecret())
	require.NoError(t, err)

	result, err := tc.Driver.NewDevice(ctx, deviceID, paired, TestSecret())
	require.NoError(t, err)

	device := result.Device
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		device.Close(closeCtx)
	})

	_, caps, err := device.DescribeDevice(ctx)
	require.NoError(t, err)
	return device, caps
}

// AssertEdgeCases runs shared edge-case assertions that apply to all miners.
func AssertEdgeCases(t *testing.T, device sdk.Device, mock mockapi.MockServer, zeroHashratePayload []byte) {
	t.Helper()

	t.Run("inactive_miner_not_healthy_active", func(t *testing.T) {
		// Arrange — summary with zero hashrate
		mock.SetResponse("summary", zeroHashratePayload)
		t.Cleanup(mock.ResetOverrides)

		// Act
		metrics, err := device.Status(TestCtx(t))

		// Assert
		require.NoError(t, err)
		assert.NotEqual(t, sdk.HealthHealthyActive, metrics.Health,
			"inactive miner should not report HEALTHY_ACTIVE")
	})

	t.Run("connection_drop_degrades_health", func(t *testing.T) {
		// Arrange — mock drops all connections
		mock.SetDefaultConnBehavior(mockapi.BehaviorCloseConn)
		t.Cleanup(mock.ResetOverrides)

		// Act
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		t.Cleanup(cancel)
		metrics, err := device.Status(ctx)

		// Assert — either returns degraded data or errors out.
		// The contract is that it never reports HEALTHY_ACTIVE.
		if err != nil {
			return // timeout or connection error is fine
		}
		assert.NotEqual(t, sdk.HealthHealthyActive, metrics.Health,
			"connection drop should not report HEALTHY_ACTIVE")
	})
}

func AssertCapabilities(t *testing.T, tc TestContext, device sdk.Device) {
	t.Helper()

	t.Run("describe_device_returns_capabilities", func(t *testing.T) {
		// Act
		_, caps, err := device.DescribeDevice(TestCtx(t))

		// Assert
		require.NoError(t, err)
		require.NotEmpty(t, caps, "device should report capabilities")
	})

	t.Run("capabilities_for_unknown_model_returns_base", func(t *testing.T) {
		provider, ok := tc.Driver.(sdk.ModelCapabilitiesProvider)
		if !ok {
			t.Skip("driver does not implement ModelCapabilitiesProvider")
		}

		// Act
		caps := provider.GetCapabilitiesForModel(TestCtx(t), tc.Manifest.Manufacturer, "totally-unknown-model-xyz")

		// Assert -- plugins may return nil or conservative base caps for unknown models.
		// Both are valid; the server merges with driver-level caps from DescribeDriver.
		if caps != nil {
			// If non-nil, should not advertise control capabilities for unknown models
			assert.False(t, caps["reboot"], "unknown model should not advertise reboot")
		}
	})
}
