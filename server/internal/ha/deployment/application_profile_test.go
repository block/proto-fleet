package deployment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFleetApplicationComposeArgsRespectPersistedFeatureFlags(t *testing.T) {
	for _, test := range []struct {
		name         string
		environment  string
		composeFiles []string
		services     []string
	}{
		{
			name: "optional features disabled",
			environment: "ENABLE_BETA_ALERTS=false\n" +
				"ENABLE_SYSTEM_MONITORING=false\n" +
				"ENABLE_TRACING=false\n",
			composeFiles: []string{"ha/fleet-compose.yaml"},
			services:     []string{"fleet-api", "fleet-client"},
		},
		{
			name: "optional features enabled",
			environment: "DD_API_KEY=test-key\n" +
				"ENABLE_BETA_ALERTS=true\n" +
				"ENABLE_SYSTEM_MONITORING=true\n" +
				"ENABLE_TRACING=true\n",
			composeFiles: []string{
				"docker-compose.alerts.yaml",
				"ha/fleet-compose.yaml",
				"ha/fleet-compose.alerts.yaml",
				"docker-compose.system-monitoring.yaml",
				"ha/fleet-compose.system-monitoring.yaml",
				"docker-compose.tracing.yaml",
				"ha/fleet-compose.tracing.yaml",
			},
			services: []string{"fleet-api", "fleet-client", "grafana", "otel-collector"},
		},
		{
			name:         "legacy deployment defaults to alerts",
			composeFiles: []string{"docker-compose.alerts.yaml", "ha/fleet-compose.yaml", "ha/fleet-compose.alerts.yaml"},
			services:     []string{"fleet-api", "fleet-client", "grafana"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			environmentPath := filepath.Join(root, fleetEnvironmentFile)
			require.NoError(t, os.WriteFile(environmentPath, []byte(test.environment), 0o600))

			profile, err := loadFleetApplicationProfileFile(environmentPath, true)
			require.NoError(t, err)
			args := fleetApplicationComposeArgsAtProfile(root, environmentPath, profile, "config", "--quiet")

			expected := []string{
				"--project-name", fleetComposeProject,
				"--env-file", filepath.Join(configRoot, "base.env"),
				"--env-file", environmentPath,
				"--env-file", filepath.Join(configRoot, "node.env"),
				"--file", filepath.Join(root, "docker-compose.yaml"),
			}
			for _, composeFile := range test.composeFiles {
				expected = append(expected, "--file", filepath.Join(root, composeFile))
			}
			expected = append(expected, "config", "--quiet")
			expected = append(expected, test.services...)
			require.Equal(t, expected, args)
		})
	}
}

func TestFleetApplicationProfileRejectsInvalidFeatureCombinations(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment string
		error       string
	}{
		{name: "invalid boolean", environment: "ENABLE_TRACING=maybe\n", error: "must be true or false"},
		{name: "monitoring without alerts", environment: "ENABLE_BETA_ALERTS=false\nENABLE_SYSTEM_MONITORING=true\n", error: "requires ENABLE_BETA_ALERTS"},
		{name: "tracing without API key", environment: "ENABLE_TRACING=true\n", error: "requires DD_API_KEY"},
		{name: "unapproved Datadog site", environment: "DD_SITE=example.com\n", error: "official Datadog site"},
		{name: "invalid tracing sample rate", environment: "ENABLE_TRACING=true\nDD_API_KEY=test-key\nFLEET_TELEMETRY_SAMPLE_RATE=abc\n", error: "must be a number from 0.0 to 1.0"},
		{name: "out-of-range tracing sample rate", environment: "ENABLE_TRACING=true\nDD_API_KEY=test-key\nFLEET_TELEMETRY_SAMPLE_RATE=1.1\n", error: "must be a number from 0.0 to 1.0"},
		{name: "NaN tracing sample rate", environment: "ENABLE_TRACING=true\nDD_API_KEY=test-key\nFLEET_TELEMETRY_SAMPLE_RATE=NaN\n", error: "must be a number from 0.0 to 1.0"},
		{name: "invalid incoming trace trust", environment: "ENABLE_TRACING=true\nDD_API_KEY=test-key\nFLEET_TELEMETRY_TRUST_INCOMING_TRACES=maybe\n", error: "must be true or false"},
		{name: "unknown key", environment: "DB_PASSWORD=unexpected\n", error: "unknown key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseFleetDeploymentEnvironment([]byte(test.environment))
			require.ErrorContains(t, err, test.error)
		})
	}
}

func TestRenderedFleetDeploymentEnvironmentNormalizesFeatureFlags(t *testing.T) {
	values, err := fleetApplicationEnvironment(func(key string) (string, bool) {
		value, ok := map[string]string{
			"DD_API_KEY":                            "test-key",
			"ENABLE_TRACING":                        "TRUE",
			"ENABLE_BETA_ALERTS":                    "true",
			"ENABLE_SYSTEM_MONITORING":              "true",
			"FLEET_TELEMETRY_SAMPLE_RATE":           "0.25",
			"FLEET_TELEMETRY_TRUST_INCOMING_TRACES": "TRUE",
		}[key]
		return value, ok
	})
	require.NoError(t, err)

	environment := renderFleetDeploymentEnvironment(values)
	require.Equal(t, "DD_API_KEY=test-key\nENABLE_BETA_ALERTS=true\nENABLE_SYSTEM_MONITORING=true\nENABLE_TRACING=true\nFLEET_TELEMETRY_SAMPLE_RATE=0.25\nFLEET_TELEMETRY_TRUST_INCOMING_TRACES=true\n", string(environment))
}

func TestFleetDeploymentEnvironmentOmitsDatadogAPIKeyWhenTracingIsDisabled(t *testing.T) {
	profile, err := fleetApplicationEnvironment(func(key string) (string, bool) {
		value, ok := map[string]string{
			"DD_API_KEY":     "ambient-secret",
			"ENABLE_TRACING": "false",
		}[key]
		return value, ok
	})
	require.NoError(t, err)
	require.NotContains(t, profile, "DD_API_KEY")
	require.NotContains(t, string(renderFleetDeploymentEnvironment(profile)), "DD_API_KEY")
}

func TestCaptureFleetApplicationEnvironmentClearsDatadogAPIKey(t *testing.T) {
	t.Setenv("ENABLE_TRACING", "true")
	t.Setenv("DD_API_KEY", "captured-secret")

	profile, err := captureFleetApplicationEnvironment()

	require.NoError(t, err)
	require.Equal(t, "captured-secret", profile["DD_API_KEY"])
	_, stillExported := os.LookupEnv("DD_API_KEY")
	require.False(t, stillExported)
}
