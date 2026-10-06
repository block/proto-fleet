//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fleetCLIBinaryPath sits under the repo-root .cache directory so writing the
// test binary never triggers the docker compose watch on server/.
const fleetCLIBinaryPath = "../../.cache/fleet-cli/fleetcli-e2e"

// TestFleetCLIWorkflow drives the local control-plane workflow through the
// fleetcli binary: create admin -> miners list -> performance get.
//
// Prerequisites: the docker-compose stack must be running with fleet-api on
// localhost:4000 and proto-sim reachable at 127.0.0.1:8080 (e.g. `just dev`).
func TestFleetCLIWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx := context.Background()

	t.Log("Waiting for fleet-api to be ready...")
	waitForFleetAPIHealth(t, ctx, 60*time.Second)

	t.Log("Building fleetcli test binary...")
	buildFleetCLIBinary(t)

	// Commands authenticate with session credentials from the environment;
	// the trailing slash targets fleet-api's RPC root instead of the
	// /api-proxy nginx route, and NO_COLOR keeps the JSON output parseable.
	env := []string{
		"FLEET_SERVER=" + fleetAPIURL + "/",
		"FLEET_USERNAME=" + testUsername,
		"FLEET_PASSWORD=" + testPassword,
		"NO_COLOR=1",
	}

	ensureFleetCLIAdmin(t, ctx, env)
	deviceIdentifier := ensureFleetCLIPairedMiner(t, ctx, env)

	t.Run("MinersList", func(t *testing.T) {
		output, err := runFleetCLI(ctx, env, "miners", "list")
		require.NoError(t, err, "miners list should succeed")

		var resp struct {
			Miners []struct {
				DeviceIdentifier string `json:"device_identifier"`
			} `json:"miners"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &resp), "miners list output should be JSON: %s", output)
		require.NotEmpty(t, resp.Miners, "miners list should return at least one miner")
		found := false
		for _, miner := range resp.Miners {
			if miner.DeviceIdentifier == deviceIdentifier {
				found = true
				break
			}
		}
		require.Truef(t, found, "miners list should include paired miner %q", deviceIdentifier)
		t.Logf("✓ Miners list contains %d miner(s)", len(resp.Miners))
	})

	t.Run("PerformanceGet", func(t *testing.T) {
		output, err := runFleetCLI(ctx, env, "performance", "get")
		require.NoError(t, err, "performance get should succeed")

		var resp struct {
			Source string `json:"source"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &resp), "performance output should be JSON: %s", output)
		assert.Equal(t, "telemetry.v1.TelemetryService/GetCombinedMetrics", resp.Source,
			"performance summary should report its telemetry source")
		t.Log("✓ Performance summary fetched")
	})
}

// buildFleetCLIBinary compiles fleetcli into a dedicated test binary so the
// e2e run always exercises the current sources.
func buildFleetCLIBinary(t *testing.T) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(fleetCLIBinaryPath), 0o750),
		"should create fleetcli output directory")

	cmd := exec.Command("go", "build", "-o", fleetCLIBinaryPath, "github.com/block/proto-fleet/server/cmd/fleetcli")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "fleetcli build should succeed")
}

// runFleetCLI executes the fleetcli test binary and returns its stdout.
// Failures include stderr so callers can match on the API error body.
func runFleetCLI(ctx context.Context, env []string, args ...string) (string, error) {
	return runFleetCLIWithInput(ctx, env, "", args...)
}

func runFleetCLIWithInput(ctx context.Context, env []string, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, fleetCLIBinaryPath, args...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// API error bodies are printed to stdout, so include both streams.
		return stdout.String(), fmt.Errorf("fleetcli %s failed: %w\nstderr: %s\nstdout: %s",
			strings.Join(args, " "), err, stderr.String(), stdout.String())
	}
	return stdout.String(), nil
}

// isAlreadyOnboardedError reports whether a create-admin failure is the
// expected rerun case where the fleet already has an admin user.
func isAlreadyOnboardedError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"already onboarded", "already exists", "duplicate", "failed_precondition"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
