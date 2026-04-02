package miners_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/tests/plugin-contract/harness"
	"github.com/block/proto-fleet/tests/plugin-contract/miners"
	"github.com/block/proto-fleet/tests/plugin-contract/mockapi/vnish"
)

const antminerVNishTestdataDir = "../testdata/antminer-vnish"

func TestAntminerVNish(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test in short mode")
	}

	mock := vnish.NewServer(t, antminerVNishTestdataDir)
	manifest := miners.LoadManifest(t, antminerVNishTestdataDir+"/manifest.json")

	driver := harness.StartAsicrsWithConfig(t, `plugin:
  log_level: debug
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 0

miners:
  antminer:
    stock:
      enabled: false
    vnish:
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
		MinerPort: fmt.Sprintf("%d", manifest.Ports["web"]),
	}

	t.Run("Discovery", func(t *testing.T) { miners.AssertDiscovery(t, tc) })
	t.Run("Pairing", func(t *testing.T) { miners.AssertPairing(t, tc) })

	t.Run("DeviceLifecycle", func(t *testing.T) {
		device, caps := miners.SetupDevice(t, tc, "test-antminer-vnish-001")

		t.Run("Telemetry", func(t *testing.T) { miners.AssertTelemetry(t, tc, device) })
		t.Run("Control", func(t *testing.T) { miners.AssertControl(t, tc, device, caps) })
		t.Run("Configuration", func(t *testing.T) {
			mock.SetWebResponse("autotune/presets", []byte(`[
				{"name":"2550","pretty":"2550 watt ~ 170 TH","status":"tuned","modded_psu_required":false},
				{"name":"3160","pretty":"3160 watt ~ 212 TH","status":"tuned","modded_psu_required":false},
				{"name":"4690","pretty":"4690 watt ~ 313 TH","status":"tuned","modded_psu_required":false}
			]`))
			t.Cleanup(mock.ResetOverrides)

			miners.AssertConfiguration(t, tc, device, caps)
		})
		t.Run("Errors", func(t *testing.T) { miners.AssertErrors(t, tc, device) })
		t.Run("Capabilities", func(t *testing.T) { miners.AssertCapabilities(t, tc, device) })

		t.Run("EdgeCases", func(t *testing.T) {
			t.Run("stopped_state_not_healthy_active", func(t *testing.T) {
				mock.SetWebResponse("summary", []byte(`{
					"miner":{
						"miner_status":{"miner_state":"stopped","miner_state_time":42},
						"miner_type":"Antminer S21 Pro (Vnish 1.2.7)",
						"chains":[]
					}
				}`))
				t.Cleanup(mock.ResetOverrides)

				edgeDevice, _ := miners.SetupDevice(t, tc, "test-antminer-vnish-edge-001")
				metrics, err := edgeDevice.Status(miners.TestCtx(t))

				require.NoError(t, err)
				assert.NotEqual(t, sdk.HealthHealthyActive, metrics.Health,
					"stopped miner should not report HEALTHY_ACTIVE")
			})

			// Skipped: asic-rs VNish backend doesn't extract chain failure messages.
			// See https://github.com/256foundation/asic-rs/issues/201
			t.Run("chain_failures_appear_in_get_errors", func(t *testing.T) {
				t.Skip("asic-rs #201: VNish chain failure error parsing not implemented")
				mock.SetWebResponse("summary", []byte(`{
					"miner":{
						"chains":[
							{"id":1,"status":{"state":"mining"}},
							{"id":2,"status":{"state":"failure","description":"Hash board 2 failure"}}
						]
					}
				}`))
				t.Cleanup(mock.ResetOverrides)

				edgeDevice, _ := miners.SetupDevice(t, tc, "test-antminer-vnish-edge-002")
				errs, err := edgeDevice.GetErrors(miners.TestCtx(t))

				require.NoError(t, err)
				assert.NotEmpty(t, errs.DeviceID)
				assert.GreaterOrEqual(t, len(errs.Errors), 1, "should report at least one error")
			})
		})
	})
}
