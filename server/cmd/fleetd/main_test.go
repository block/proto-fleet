package main

import (
	"bytes"
	"context"
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
	"github.com/stretchr/testify/require"
)

type scriptedLifecycle struct {
	stopErrors   []error
	stopFuncs    []func(context.Context) error
	stopContexts []context.Context
}

func (s *scriptedLifecycle) Start(context.Context) error { return nil }

func (s *scriptedLifecycle) Stop(ctx context.Context) error {
	s.stopContexts = append(s.stopContexts, ctx)
	if len(s.stopFuncs) > 0 {
		stop := s.stopFuncs[0]
		s.stopFuncs = s.stopFuncs[1:]
		return stop(ctx)
	}
	if len(s.stopErrors) == 0 {
		return nil
	}
	err := s.stopErrors[0]
	s.stopErrors = s.stopErrors[1:]
	return err
}

func TestStopStandaloneJobDoesNotRetryEarlyDeadlineExceeded(t *testing.T) {
	var contextErr error
	job := &scriptedLifecycle{stopFuncs: []func(context.Context) error{
		func(ctx context.Context) error {
			contextErr = ctx.Err()
			return context.DeadlineExceeded
		},
	}}
	stopStandaloneJobWithTimeout("internal timeout", job, time.Second)

	require.Len(t, job.stopContexts, 1)
	require.NoError(t, contextErr)
}

func TestStopStandaloneJobRetriesAfterShutdownTimeout(t *testing.T) {
	job := &scriptedLifecycle{stopFuncs: []func(context.Context) error{
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func(context.Context) error { return nil },
	}}
	stopStandaloneJobWithTimeout("shutdown timeout", job, 5*time.Millisecond)

	require.Len(t, job.stopContexts, 2)
	require.ErrorIs(t, job.stopContexts[0].Err(), context.DeadlineExceeded)
	_, hasDeadline := job.stopContexts[1].Deadline()
	require.True(t, hasDeadline)
}

func TestStopStandaloneJobDoesNotRetryOrdinaryFailure(t *testing.T) {
	job := &scriptedLifecycle{stopErrors: []error{errors.New("stop failed")}}
	stopStandaloneJob("ordinary failure", job)

	require.Len(t, job.stopContexts, 1)
	_, hasDeadline := job.stopContexts[0].Deadline()
	require.True(t, hasDeadline)
}

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
cohort:
  expiry-sweep-interval: "30s"
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
	require.Equal(t, 30*time.Second, config.Cohort.ExpirySweepInterval)
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

	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	_, err = parser.Parse([]string{
		"--http-address=127.0.0.1:8081",
		"--http-write-byte-timeout=1m",
		"--cohort-expiry-sweep-interval=15s",
		"--logging-json=false",
	})
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8081", config.HTTP.Address)
	require.Equal(t, time.Minute, config.HTTP.WriteByteTimeout)
	require.Equal(t, 15*time.Second, config.Cohort.ExpirySweepInterval)
	require.False(t, config.Log.JSON)
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
	config := &Config{}
	parser, err := kong.New(
		config,
		kong.Name("fleetd"),
		kong.Configuration(kongyaml.Loader, configPath),
	)
	require.NoError(t, err)

	_, err = parser.Parse(nil)
	require.NoError(t, err)
	require.Equal(t, explicitDSN, config.DB.ExplicitDSN)
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
