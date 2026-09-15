package deployment

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInstalledFleetComposeArgsSupportLegacyAndGrafanaProfiles(t *testing.T) {
	for _, test := range []struct {
		name        string
		ownsGrafana bool
	}{
		{name: "legacy"},
		{name: "Grafana", ownsGrafana: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			environmentPath := filepath.Join(root, fleetEnvironmentFile)
			if err := os.WriteFile(environmentPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			alertsPath := filepath.Join(root, "docker-compose.alerts.yaml")
			if err := os.WriteFile(alertsPath, []byte("services: {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ownershipMarker := filepath.Join(root, "grafana-volume-owned")
			if test.ownsGrafana {
				if err := os.WriteFile(ownershipMarker, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			args, err := fleetComposeArgsForInstalledProfileAt(root, environmentPath, ownershipMarker, "down")
			if err != nil {
				t.Fatal(err)
			}
			hasAlerts := slices.Contains(args, alertsPath)
			if hasAlerts != test.ownsGrafana {
				t.Fatalf("alerts Compose included = %t, want %t; args = %q", hasAlerts, test.ownsGrafana, args)
			}
		})
	}
}

func TestResetSuperAdminPasswordComposeArgsUseInstalledHAProfile(t *testing.T) {
	root := t.TempDir()
	environmentPath := filepath.Join(root, fleetEnvironmentFile)
	if err := os.WriteFile(environmentPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ownershipMarker := filepath.Join(root, "grafana-volume-owned")

	args, err := resetSuperAdminPasswordComposeArgsAt(root, environmentPath, ownershipMarker)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"--project-name deployment",
		"--env-file /etc/proto-fleet/ha/base.env",
		"--env-file " + environmentPath,
		"--env-file /etc/proto-fleet/ha/node.env",
		"--file " + filepath.Join(root, "docker-compose.yaml"),
		"--file " + filepath.Join(root, "ha/fleet-compose.yaml"),
		"run --rm --no-deps -T fleet-api /app/fleetd admin reset-password --password-stdin",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("reset args %q do not contain %q", args, expected)
		}
	}
	if strings.Contains(joined, "docker-compose.alerts.yaml") {
		t.Fatalf("legacy HA reset unexpectedly included alerts profile: %q", args)
	}

	if err := os.WriteFile(ownershipMarker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	args, err = resetSuperAdminPasswordComposeArgsAt(root, environmentPath, ownershipMarker)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--file "+filepath.Join(root, "docker-compose.alerts.yaml")) {
		t.Fatalf("Grafana-owning HA reset omitted alerts profile: %q", args)
	}
	if !strings.Contains(joined, "--password-stdin") {
		t.Fatalf("HA reset did not force credential-free container output: %q", args)
	}
}

func TestComposeEnvironmentDropsParentOverrides(t *testing.T) {
	for _, key := range []string{"AUTH_CLIENT_SECRET_KEY", "COMPOSE_PROJECT_NAME", "DB_DSN", "HA_NODE_IP", "GRAFANA_DB_PASSWORD", "FLEET_ALERTS_GRAFANA_URL", "ENABLE_TRACING", "DD_API_KEY", "DD_HOSTNAME", "SESSION_COOKIE_SECURE", "INFRASTRUCTURE_OT_CONTROL_SUBNETS"} {
		t.Setenv(key, "parent-value")
	}

	environment, err := composeEnvironment([]string{"config"})

	if err != nil {
		t.Fatal(err)
	}
	if len(environment) != 1 || !strings.HasPrefix(environment[0], "PATH=") {
		t.Fatalf("compose environment = %q, want only PATH", environment)
	}
}

func TestComposeEnvironmentDerivesDatadogHostnameFromNodeIdentity(t *testing.T) {
	nodeEnvironment := testNodeEnv(t, t.TempDir(), t.TempDir(), t.TempDir())

	environment, err := composeEnvironment([]string{"--env-file", nodeEnvironment})

	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(environment, "DD_HOSTNAME=ha-a") {
		t.Fatalf("compose environment does not contain the derived Datadog hostname: %q", environment)
	}
	if len(environment) != 2 {
		t.Fatalf("compose environment contains ambient values: %q", environment)
	}
}

func TestFleetApplicationStartReconcilesOrphansAndReportsCollectorHealthFailure(t *testing.T) {
	root := t.TempDir()
	environmentPath := filepath.Join(root, fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(environmentPath, []byte("DD_API_KEY=test-key\nENABLE_BETA_ALERTS=false\nENABLE_TRACING=true\n"), 0o600))
	var calls [][]string
	var warnings bytes.Buffer
	healthErr := errors.New("collector rejected its configuration")
	deps := fleetApplicationStartDependencies{
		environmentPath: environmentPath,
		runCompose: func(_ context.Context, args []string) error {
			calls = append(calls, slices.Clone(args))
			return nil
		},
		collectorReady: func(context.Context) error { return healthErr },
		warnings:       &warnings,
	}

	err := startFleetApplicationWith(t.Context(), root, deps, "-d")

	require.NoError(t, err)
	require.Len(t, calls, 2)
	criticalStart := calls[0][slices.Index(calls[0], "up"):]
	require.Equal(t, []string{"up", "-d", "--remove-orphans", "fleet-api", "fleet-client"}, criticalStart)
	collectorStart := calls[1][slices.Index(calls[1], "up"):]
	require.Equal(t, []string{"up", "-d", "--no-deps", "--no-build", "--pull", "never", "otel-collector"}, collectorStart)
	require.Contains(t, warnings.String(), healthErr.Error())
}

func TestFleetApplicationDownRemovesDisabledSidecarOrphans(t *testing.T) {
	root := t.TempDir()
	environmentPath := filepath.Join(root, fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(environmentPath, []byte("ENABLE_BETA_ALERTS=false\nENABLE_TRACING=false\n"), 0o600))
	ownershipMarker := filepath.Join(root, "grafana-volume-owned")

	args, err := fleetApplicationDownArgsAt(root, environmentPath, ownershipMarker, "--timeout", "1")

	require.NoError(t, err)
	down := args[slices.Index(args, "down"):]
	require.Equal(t, []string{"down", "--timeout", "1", "--remove-orphans"}, down)
	require.NotContains(t, args, "fleet-api")
	require.NotContains(t, args, "fleet-client")
	require.NotContains(t, args, "grafana")
	require.NotContains(t, args, "otel-collector")
}

func TestWaitForHTTPEndpointRetriesUntilHealthy(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	err := waitForHTTPEndpoint(t.Context(), server.Client(), server.URL, time.Millisecond)

	require.NoError(t, err)
	require.Equal(t, int32(2), requests.Load())
}
