package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/stretchr/testify/require"
)

func TestFleetdLoadsConfigFromYAML(t *testing.T) {
	t.Parallel()

	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
db:
  address: "db.internal:5432"
encrypt:
  service-master-key: "test-master-key"
http:
  address: "0.0.0.0:9090"
  suppress-cors: true
logging:
  json: true
`)

	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	_, err = parser.Parse(nil)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9090", config.HTTP.Address)
	require.True(t, config.HTTP.SuppressCors)
	require.Equal(t, "db.internal:5432", config.DB.Address)
	require.True(t, config.Log.JSON)
	require.Equal(t, "test-client-secret", config.Auth.ClientToken.SecretKey)
	require.Equal(t, time.Hour, config.Auth.ClientToken.ExpirationPeriod)
	require.Equal(t, 30*time.Minute, config.Auth.MinerTokenExpirationPeriod)
	require.Equal(t, "test-master-key", config.Encrypt.ServiceMasterKey)
}

func TestFleetdFlagsOverrideYAMLConfig(t *testing.T) {
	t.Parallel()

	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
http:
  address: "0.0.0.0:9090"
logging:
  json: true
`)

	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	_, err = parser.Parse([]string{
		"--http-address=127.0.0.1:8081",
		"--logging-json=false",
	})
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8081", config.HTTP.Address)
	require.False(t, config.Log.JSON)
}

func writeFleetdConfigFile(t *testing.T, contents string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "fleetd.yaml")
	err := os.WriteFile(configPath, []byte(contents), 0o600)
	require.NoError(t, err)

	return configPath
}
