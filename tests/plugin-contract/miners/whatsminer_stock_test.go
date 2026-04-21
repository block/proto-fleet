package miners_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/tests/plugin-contract/harness"
	"github.com/block/proto-fleet/tests/plugin-contract/miners"
	"github.com/block/proto-fleet/tests/plugin-contract/mockapi/whatsminer"
)

const whatsminerTestdataDir = "../testdata/whatsminer-stock"

func TestWhatsMinerStock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test in short mode")
	}

	// Arrange
	mock := whatsminer.NewServer(t, whatsminerTestdataDir)
	manifest := miners.LoadManifest(t, whatsminerTestdataDir+"/manifest.json")

	// Use cache_ttl=0 so mock overrides take effect immediately in edge-case tests.
	driver := harness.StartAsicrsWithConfig(t, `plugin:
  log_level: debug
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 0

miners:
  whatsminer:
    stock:
      enabled: true
`)

	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		id, err := driver.Handshake(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, id.DriverName)
	}()

	tc := miners.TestContext{
		Driver:    driver,
		Manifest:  manifest,
		MinerIP:   mock.Host(),
		MinerPort: fmt.Sprintf("%d", manifest.Ports["rpc"]),
	}

	t.Run("Discovery", func(t *testing.T) { miners.AssertDiscovery(t, tc) })
	t.Run("Pairing", func(t *testing.T) { miners.AssertPairing(t, tc) })

	t.Run("DeviceLifecycle", func(t *testing.T) {
		device, caps := miners.SetupDevice(t, tc, "test-whatsminer-001")

		// --- Happy-path contract assertions ---
		t.Run("Telemetry", func(t *testing.T) { miners.AssertTelemetry(t, tc, device) })
		t.Run("Control", func(t *testing.T) { miners.AssertControl(t, tc, device, caps) })
		t.Run("Configuration", func(t *testing.T) { miners.AssertConfiguration(t, tc, device, caps) })
		t.Run("Errors", func(t *testing.T) { miners.AssertErrors(t, tc, device) })
		t.Run("Capabilities", func(t *testing.T) { miners.AssertCapabilities(t, tc, device) })

		// --- Edge-case assertions using mock overrides ---
		t.Run("EdgeCases", func(t *testing.T) {
			miners.AssertEdgeCases(t, device, mock, []byte(`{
				"STATUS": "S", "When": 1741500000, "Code": 134,
				"Msg": {
					"HS RT": 0, "HS av": 0, "Elapsed": 100,
					"MHS av": 0, "MHS 1m": 0, "MHS 5m": 0, "MHS 15m": 0,
					"Temperature": 65.0, "Fan Speed In": 4200, "Fan Speed Out": 4100,
					"Power": 0, "Power_RT": 0,
					"Power Mode": "Normal", "Factory GHS": 110000, "Power Limit": 3500,
					"Chip Temp Min": 55.0, "Chip Temp Max": 78.0, "Chip Temp Avg": 65.0,
					"Uptime": 100, "MAC": "C4:11:04:01:02:03",
					"Firmware Version": "20251209.16.Rel2"
				},
				"Description": "btminer"
			}`))

			// WhatsMiner-specific: error codes edge case
			// Skipped: asic-rs WhatsMiner V2 GetMessages returns empty message strings.
			// See https://github.com/256foundation/asic-rs/issues/201
			t.Run("error_codes_appear_in_get_errors", func(t *testing.T) {
				t.Skip("asic-rs #201: WhatsMiner V2 GetMessages returns empty error strings")
				// Arrange
				mock.SetResponse("get_error_code", []byte(`{
					"STATUS": "S", "When": 1741500000, "Code": 136,
					"Msg": {"error_code": [110]},
					"Description": "btminer"
				}`))
				t.Cleanup(mock.ResetOverrides)

				// Act
				errs, err := device.GetErrors(miners.TestCtx(t))

				// Assert
				require.NoError(t, err)
				assert.NotEmpty(t, errs.DeviceID)
				assert.GreaterOrEqual(t, len(errs.Errors), 1,
					"should report at least one error")
			})
		})
	})
}
