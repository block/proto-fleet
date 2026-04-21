package miners_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/tests/plugin-contract/harness"
	"github.com/block/proto-fleet/tests/plugin-contract/miners"
	"github.com/block/proto-fleet/tests/plugin-contract/mockapi/antminer"
)

const antminerTestdataDir = "../testdata/antminer-stock"

func TestAntminerStock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test in short mode")
	}

	// Arrange
	mock := antminer.NewServer(t, antminerTestdataDir)
	manifest := miners.LoadManifest(t, antminerTestdataDir+"/manifest.json")
	driver := harness.StartAntminer(t, mock.WebPort())

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
		device, caps := miners.SetupDevice(t, tc, "test-antminer-001")

		// --- Happy-path contract assertions ---
		t.Run("Telemetry", func(t *testing.T) { miners.AssertTelemetry(t, tc, device) })
		t.Run("Control", func(t *testing.T) { miners.AssertControl(t, tc, device, caps) })
		t.Run("Configuration", func(t *testing.T) { miners.AssertConfiguration(t, tc, device, caps) })
		t.Run("Errors", func(t *testing.T) { miners.AssertErrors(t, tc, device) })
		t.Run("Capabilities", func(t *testing.T) { miners.AssertCapabilities(t, tc, device) })

		// --- Edge-case assertions using mock overrides ---
		t.Run("EdgeCases", func(t *testing.T) {
			miners.AssertEdgeCases(t, device, mock, []byte(`{
				"STATUS": [{"STATUS": "S", "When": 1741500000, "Code": 11, "Msg": "Summary", "Description": "bmminer"}],
				"SUMMARY": [{
					"Elapsed": 100,
					"GHS 5s": 0, "GHS av": 0, "GHS 30m": 0,
					"Found Blocks": 0, "Getwork": 0,
					"Accepted": 0, "Rejected": 0,
					"Hardware Errors": 0, "Utility": 0,
					"Discarded": 0, "Stale": 0,
					"Get Failures": 0, "Local Work": 0,
					"Remote Failures": 0, "Network Blocks": 0,
					"Total MH": 0, "Work Utility": 0,
					"Difficulty Accepted": 0, "Difficulty Rejected": 0,
					"Difficulty Stale": 0, "Best Share": 0,
					"Device Hardware%": 0, "Device Rejected%": 0,
					"Pool Rejected%": 0, "Pool Stale%": 0,
					"Last getwork": 0
				}],
				"id": 1
			}`))
		})
	})
}
