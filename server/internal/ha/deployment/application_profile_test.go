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
		{name: "invalid market data boolean", environment: "MARKET_DATA_ENABLED=maybe\n", error: "must be true or false"},
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
			"HTTP_TRUSTED_PROXY_CIDRS":              "127.0.0.1/32,::1/128,10.0.0.0/24",
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
	require.Equal(t, "HTTP_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128,10.0.0.0/24\nDD_API_KEY=test-key\nENABLE_BETA_ALERTS=true\nENABLE_SYSTEM_MONITORING=true\nENABLE_TRACING=true\nFLEET_TELEMETRY_SAMPLE_RATE=0.25\nFLEET_TELEMETRY_TRUST_INCOMING_TRACES=true\n", string(environment))
	reloaded, err := parseFleetDeploymentEnvironment(environment)
	require.NoError(t, err)
	require.Equal(t, values["HTTP_TRUSTED_PROXY_CIDRS"], reloaded["HTTP_TRUSTED_PROXY_CIDRS"])
}

func TestFleetApplicationProfileValidatesTrustedProxyCIDRs(t *testing.T) {
	for _, test := range []struct {
		name  string
		cidrs string
		valid bool
	}{
		{name: "unset", valid: true},
		{name: "IPv4 and IPv6", cidrs: "127.0.0.1/32,::1/128,10.0.0.0/24", valid: true},
		{name: "bare IP", cidrs: "10.0.0.1"},
		{name: "invalid prefix length", cidrs: "10.0.0.0/33"},
		{name: "malformed later entry", cidrs: "127.0.0.1/32,invalid"},
		{name: "empty entry", cidrs: "127.0.0.1/32,"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const key = "HTTP_TRUSTED_PROXY_CIDRS"
			path := filepath.Join(t.TempDir(), fleetEnvironmentFile)
			var contents string
			if test.cidrs != "" {
				contents = key + "=" + test.cidrs + "\n"
			}
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			for name, load := range map[string]func() (fleetApplicationProfile, error){
				"capture": func() (fleetApplicationProfile, error) {
					return fleetApplicationEnvironment(func(name string) (string, bool) {
						return test.cidrs, name == key && test.cidrs != ""
					})
				},
				"reload": func() (fleetApplicationProfile, error) {
					return loadFleetApplicationProfileFile(path, true)
				},
			} {
				t.Run(name, func(t *testing.T) {
					profile, err := load()
					if !test.valid {
						require.ErrorContains(t, err, key)
						return
					}
					require.NoError(t, err)
					require.Equal(t, test.cidrs, profile[key])
				})
			}
		})
	}
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

func TestFleetApplicationProfilePreservesMarketDataSettings(t *testing.T) {
	settings := map[string]string{
		"MARKET_DATA_ENABLED":           "TRUE",
		"MARKET_DATA_REFRESH_INTERVAL":  "5m",
		"MARKET_DATA_PRICE_PROVIDER":    "coingecko",
		"MARKET_DATA_COINBASE_URL":      "https://coinbase.example.invalid",
		"MARKET_DATA_COINGECKO_URL":     "https://coingecko.example.invalid",
		"MARKET_DATA_COINGECKO_API_KEY": "test-coingecko-key",
		"MARKET_DATA_MEMPOOL_URL":       "https://mempool.example.invalid",
	}
	profile, err := fleetApplicationEnvironment(func(key string) (string, bool) {
		value, ok := settings[key]
		return value, ok
	})
	require.NoError(t, err)
	settings["MARKET_DATA_ENABLED"] = "true"
	for key, value := range settings {
		require.Equal(t, value, profile[key], key)
	}

	contents := renderFleetDeploymentEnvironment(profile)
	reloaded, err := parseFleetDeploymentEnvironment(contents)
	require.NoError(t, err)
	require.Equal(t, profile, reloaded)
	path := filepath.Join(t.TempDir(), fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	reloaded, err = loadFleetApplicationProfileFile(path, true)
	require.NoError(t, err)
	require.Equal(t, profile, reloaded)
}

func TestCaptureFleetApplicationEnvironmentClearsCoinGeckoAPIKey(t *testing.T) {
	for _, enabled := range []string{"true", "invalid"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv("MARKET_DATA_ENABLED", enabled)
			t.Setenv("MARKET_DATA_COINGECKO_API_KEY", "captured-coingecko-secret")
			profile, err := captureFleetApplicationEnvironment()
			if enabled == "true" {
				require.NoError(t, err)
				require.Equal(t, "captured-coingecko-secret", profile["MARKET_DATA_COINGECKO_API_KEY"])
			} else {
				require.ErrorContains(t, err, "MARKET_DATA_ENABLED")
			}
			_, stillExported := os.LookupEnv("MARKET_DATA_COINGECKO_API_KEY")
			require.False(t, stillExported)
		})
	}
}
