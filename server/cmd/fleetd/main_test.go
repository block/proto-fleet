package main

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/block/proto-fleet/server/internal/ha"
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
  write-byte-timeout: "45s"
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
	require.Equal(t, 45*time.Second, config.HTTP.WriteByteTimeout)
	require.True(t, config.HTTP.SuppressCors)
	require.Equal(t, "db.internal:5432", config.DB.Address)
	require.True(t, config.Log.JSON)
	require.Equal(t, "test-client-secret", config.Auth.ClientToken.SecretKey)
	require.Equal(t, time.Hour, config.Auth.ClientToken.ExpirationPeriod)
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

	cli := &fleetdCLI{}
	parser, err := kong.New(
		cli,
		kong.Name("fleetd"),
		kong.Configuration(fleetdYAMLLoader, configPath),
	)
	require.NoError(t, err)

	ctx, err := parser.Parse(normalizeFleetdArgs([]string{
		"--http-address=127.0.0.1:8081",
		"--http-write-byte-timeout=1m",
		"--logging-json=false",
	}))
	require.NoError(t, err)
	require.Equal(t, "server", ctx.Command())
	require.Equal(t, "127.0.0.1:8081", cli.Server.HTTP.Address)
	require.Equal(t, time.Minute, cli.Server.HTTP.WriteByteTimeout)
	require.False(t, cli.Server.Log.JSON)
}

func TestFleetdLoadsExplicitDBDSNFromEnv(t *testing.T) {
	explicitDSN := "postgres://fleet:secret@fleet-a:5432,fleet-b:5432/fleet?sslmode=disable&target_session_attrs=read-write"
	t.Setenv("DB_DSN", explicitDSN)

	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
`)
	cli := &fleetdCLI{}
	parser, err := kong.New(
		cli,
		kong.Name("fleetd"),
		kong.Configuration(fleetdYAMLLoader, configPath),
	)
	require.NoError(t, err)

	ctx, err := parser.Parse(nil)
	require.NoError(t, err)
	require.Equal(t, "server", ctx.Command())
	require.Equal(t, explicitDSN, cli.Server.DB.ExplicitDSN)
}

func TestFleetdParsesHAEnabledFromEnv(t *testing.T) {
	t.Setenv("FLEET_HA_ENABLED", "true")
	t.Setenv("FLEET_HA_ETCD_ENDPOINTS", "https://10.0.0.1:2379,https://10.0.0.2:2379")
	t.Setenv("FLEET_HA_ENDPOINT_IP", "10.0.0.100")
	t.Setenv("FLEET_HA_ENDPOINT_INTERFACE", "eth0")

	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
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
	require.True(t, config.HA.Enabled)
	require.Equal(t, []string{"https://10.0.0.1:2379", "https://10.0.0.2:2379"}, config.HA.EtcdEndpoints)
	require.Equal(t, "10.0.0.100", config.HA.EndpointIP)
	require.Equal(t, "eth0", config.HA.EndpointInterface)
	require.NoError(t, config.HA.Validate())
}

func TestFleetdInfrastructureOTControlSubnetsFlag(t *testing.T) {
	t.Parallel()

	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
`)
	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	const allowlist = "10.20.0.0/24\nfd12:3456::7/128"
	_, err = parser.Parse([]string{"--infrastructure-ot-control-subnets=" + allowlist})
	require.NoError(t, err)
	require.Equal(t, allowlist, config.Infrastructure.OTControlSubnets)
}

func TestFleetdInfrastructureOTControlSubnetsEnvironment(t *testing.T) {
	const allowlist = "10.20.0.0/24\nfd12:3456::7/128"
	t.Setenv("INFRASTRUCTURE_OT_CONTROL_SUBNETS", allowlist)

	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
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
	require.Equal(t, allowlist, config.Infrastructure.OTControlSubnets)
}

func TestFleetdRejectsInvalidInfrastructureOTControlSubnetsBeforeStartup(t *testing.T) {
	config := &Config{}
	config.Infrastructure.OTControlSubnets = "sensitive-control-subnet"

	err := start(config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "configure infrastructure drivers")
	require.NotContains(t, err.Error(), "sensitive-control-subnet")
}

func TestFleetdSystemMonitoringConfig(t *testing.T) {
	t.Parallel()

	// Arrange
	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
`)
	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	// Act
	_, err = parser.Parse([]string{
		"--system-monitoring-enabled",
		"--system-monitoring-interval=15s",
		"--system-monitoring-disk-path=/hostfs",
	})

	// Assert
	require.NoError(t, err)
	require.True(t, config.SystemMonitoring.Enabled)
	require.Equal(t, 15*time.Second, config.SystemMonitoring.Interval)
	require.Equal(t, "/hostfs", config.SystemMonitoring.DiskPath)
}

func TestFleetdSystemMonitoringDefaultsOff(t *testing.T) {
	t.Parallel()

	// Arrange
	configPath := writeFleetdConfigFile(t, `
auth:
  client:
    expiration-period: "1h"
    secret-key: "test-client-secret"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "test-master-key"
`)
	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	// Act
	_, err = parser.Parse(nil)

	// Assert
	require.NoError(t, err)
	require.False(t, config.SystemMonitoring.Enabled)
	require.Equal(t, 30*time.Second, config.SystemMonitoring.Interval)
	require.Equal(t, "/", config.SystemMonitoring.DiskPath)
}

func TestValidateHAHTTPAddressRequiresLocalStatusAddress(t *testing.T) {
	for _, test := range []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "loopback", address: "127.0.0.1:4000"},
		{name: "other loopback port", address: "127.0.0.1:8080", wantErr: true},
		{name: "network interface", address: "0.0.0.0:4000", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			config := Config{HTTP: HTTPConfig{Address: test.address}, HA: ha.Config{Enabled: true}}

			// Act
			err := validateHAHTTPAddress(config)

			// Assert
			if test.wantErr {
				require.ErrorContains(t, err, "HTTP listen address")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestHTTP2WriteByteTimeoutStopsNonReadingClient(t *testing.T) {
	t.Parallel()

	errMissingFlusher := errors.New("response writer does not implement http.Flusher")
	handlerDone := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 1024)
		flusher, ok := w.(http.Flusher)
		if !ok {
			handlerDone <- errMissingFlusher
			return
		}
		for {
			if _, err := w.Write(chunk); err != nil {
				handlerDone <- err
				return
			}
			flusher.Flush()
			if err := r.Context().Err(); err != nil {
				handlerDone <- err
				return
			}
		}
	})

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go newHTTP2Server(HTTPConfig{WriteByteTimeout: 50 * time.Millisecond}).ServeConn(serverConn, &http2.ServeConnOpts{
		Handler: handler,
	})

	framer := http2.NewFramer(clientConn, clientConn)
	_, err := clientConn.Write([]byte(http2.ClientPreface))
	require.NoError(t, err)
	require.NoError(t, framer.WriteSettings())
	var headers bytes.Buffer
	encoder := hpack.NewEncoder(&headers)
	require.NoError(t, encoder.WriteField(hpack.HeaderField{Name: ":method", Value: http.MethodGet}))
	require.NoError(t, encoder.WriteField(hpack.HeaderField{Name: ":scheme", Value: "http"}))
	require.NoError(t, encoder.WriteField(hpack.HeaderField{Name: ":authority", Value: "fleetd.test"}))
	require.NoError(t, encoder.WriteField(hpack.HeaderField{Name: ":path", Value: "/"}))
	require.NoError(t, framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      1,
		BlockFragment: headers.Bytes(),
		EndHeaders:    true,
		EndStream:     true,
	}))
	_, err = framer.ReadFrame()
	require.NoError(t, err)

	select {
	case err := <-handlerDone:
		require.Error(t, err)
		require.NotErrorIs(t, err, errMissingFlusher)
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not unblock after client stopped reading response body")
	}
}

func writeFleetdConfigFile(t *testing.T, contents string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "fleetd.yaml")
	err := os.WriteFile(configPath, []byte(contents), 0o600)
	require.NoError(t, err)

	return configPath
}
