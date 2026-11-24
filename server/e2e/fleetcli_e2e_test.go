//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fleetCLIBinaryPath sits under the repo-root .cache directory so writing the
// test binary never triggers the docker compose watch on server/.
const fleetCLIBinaryPath = "../../.cache/fleet-cli/fleetcli-e2e"

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
