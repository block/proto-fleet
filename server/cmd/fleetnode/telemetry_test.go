package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func telemetryCmd(t *testing.T, req *telemetrypb.FleetNodeTelemetryRequest) []byte {
	t.Helper()
	return mustMarshal(t, &pb.AgentCommand{Command: &pb.AgentCommand_Telemetry{Telemetry: req}})
}

type stubTelemetryFetcher struct {
	result *telemetrypb.FleetNodeTelemetryResult
	err    error
	seen   *telemetrypb.FleetNodeTelemetryRequest
}

func (s *stubTelemetryFetcher) Fetch(_ context.Context, req *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	s.seen = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

type waitingTelemetryFetcher struct{}

func (waitingTelemetryFetcher) Fetch(ctx context.Context, _ *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("wait for telemetry timeout: %w", ctx.Err())
}

type stuckTelemetryFetcher struct {
	started chan struct{}
	release chan struct{}
}

func (f *stuckTelemetryFetcher) Fetch(_ context.Context, _ *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	close(f.started)
	<-f.release
	return nil, errors.New("released stuck telemetry fetch")
}

type delayedTelemetryFetcher struct {
	delay  time.Duration
	result *telemetrypb.FleetNodeTelemetryResult
}

func (f delayedTelemetryFetcher) Fetch(ctx context.Context, _ *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	timer := time.NewTimer(f.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return f.result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for delayed telemetry: %w", ctx.Err())
	}
}

func TestControlLoop_TelemetryAckCarriesPayload(t *testing.T) {
	fetcher := &stubTelemetryFetcher{result: &telemetrypb.FleetNodeTelemetryResult{
		DeviceIdentifier: "node-device",
		Timestamp:        timestamppb.New(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)),
		FirmwareVersion:  "fw-1",
		DeviceStatus:     telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE,
		HashrateHs:       ptrFloat64(42),
	}}
	cmd := &RunCmd{telemetry: fetcher}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.True(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, acks[0].GetCode())
	require.NotEmpty(t, acks[0].GetPayload())
	got := &telemetrypb.FleetNodeTelemetryResult{}
	require.NoError(t, proto.Unmarshal(acks[0].GetPayload(), got))
	assert.Equal(t, "node-device", got.GetDeviceIdentifier())
	assert.Equal(t, 42.0, got.GetHashrateHs())
	assert.Equal(t, "node-device", fetcher.seen.GetDeviceIdentifier())
}

func TestControlLoop_TelemetryAgentIncapableWithoutFetcher(t *testing.T) {
	cmd := &RunCmd{}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, acks[0].GetCode())
}

func TestControlLoop_TelemetryUsesShortCommandTimeout(t *testing.T) {
	oldTelemetryTimeout := telemetryCommandTimeout
	oldCommandTimeout := commandTimeout
	telemetryCommandTimeout = 10 * time.Millisecond
	commandTimeout = time.Second
	t.Cleanup(func() {
		telemetryCommandTimeout = oldTelemetryTimeout
		commandTimeout = oldCommandTimeout
	})

	cmd := &RunCmd{telemetry: waitingTelemetryFetcher{}}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	start := time.Now()
	runControlLoopOnce(t, cmd, fake)

	require.Less(t, time.Since(start), 500*time.Millisecond)
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, acks[0].GetCode())
}

func TestControlLoop_TelemetrySupervisorReturnsAckForStuckFetcher(t *testing.T) {
	oldTelemetryTimeout := telemetryCommandTimeout
	oldSupervisorGrace := telemetrySupervisorGrace
	telemetryCommandTimeout = 20 * time.Millisecond
	telemetrySupervisorGrace = 10 * time.Millisecond
	t.Cleanup(func() {
		telemetryCommandTimeout = oldTelemetryTimeout
		telemetrySupervisorGrace = oldSupervisorGrace
	})

	fetcher := &stuckTelemetryFetcher{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(func() { close(fetcher.release) })
	cmd := &RunCmd{telemetry: fetcher}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	start := time.Now()
	runControlLoopOnce(t, cmd, fake)

	require.Less(t, time.Since(start), 500*time.Millisecond)
	select {
	case <-fetcher.started:
	default:
		t.Fatal("telemetry fetch did not start")
	}
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_SCAN_FAILED, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "supervisor budget exceeded")
}

func TestControlLoop_TelemetryUsesRequestTimeout(t *testing.T) {
	oldTelemetryTimeout := telemetryCommandTimeout
	telemetryCommandTimeout = time.Second
	t.Cleanup(func() { telemetryCommandTimeout = oldTelemetryTimeout })

	cmd := &RunCmd{telemetry: waitingTelemetryFetcher{}}
	fake := &controlFakeGateway{}
	req := validTelemetryRequest()
	req.Timeout = durationpb.New(10 * time.Millisecond)
	fake.queue(telemetryCmd(t, req))

	start := time.Now()
	runControlLoopOnce(t, cmd, fake)

	require.Less(t, time.Since(start), 500*time.Millisecond)
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, acks[0].GetCode())
}

func TestControlLoop_TelemetryReturnsAckNearNodeTimeout(t *testing.T) {
	oldTelemetryTimeout := telemetryCommandTimeout
	telemetryCommandTimeout = 50 * time.Millisecond
	t.Cleanup(func() { telemetryCommandTimeout = oldTelemetryTimeout })

	cmd := &RunCmd{telemetry: delayedTelemetryFetcher{
		delay: 40 * time.Millisecond,
		result: &telemetrypb.FleetNodeTelemetryResult{
			DeviceIdentifier: "node-device",
			Timestamp:        timestamppb.New(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)),
			DeviceStatus:     telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE,
		},
	}}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.True(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, acks[0].GetCode())
	assert.NotEmpty(t, acks[0].GetPayload())
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func TestControlLoop_TelemetryValidationError(t *testing.T) {
	cmd := &RunCmd{telemetry: &stubTelemetryFetcher{}}
	fake := &controlFakeGateway{}
	req := validTelemetryRequest()
	req.Port = "0"
	fake.queue(telemetryCmd(t, req))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, acks[0].GetCode())
}

func TestControlLoop_TelemetryFetcherCommandError(t *testing.T) {
	cmd := &RunCmd{telemetry: &stubTelemetryFetcher{err: cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "auth failed")}}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "auth failed")
}

func TestControlLoop_TelemetryFetcherAuthError(t *testing.T) {
	cmd := &RunCmd{telemetry: &stubTelemetryFetcher{err: cmdErr(pb.AckCode_ACK_CODE_UNAUTHENTICATED, "auth failed")}}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_UNAUTHENTICATED, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "auth failed")
}

func TestControlLoop_TelemetryFetcherGenericError(t *testing.T) {
	cmd := &RunCmd{telemetry: &stubTelemetryFetcher{err: errors.New("plugin exploded")}}
	fake := &controlFakeGateway{}
	fake.queue(telemetryCmd(t, validTelemetryRequest()))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, acks[0].GetCode())
}

func TestControlLoop_TelemetryRedactsRequestPasswordFromFetcherError(t *testing.T) {
	cmd := &RunCmd{telemetry: &stubTelemetryFetcher{err: errors.New("auth failed with password secret-pw")}}
	fake := &controlFakeGateway{}
	req := validTelemetryRequest()
	username := "root"
	password := "secret-pw"
	req.Username = &username
	req.Password = &password
	fake.queue(telemetryCmd(t, req))

	runControlLoopOnce(t, cmd, fake)

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.NotContains(t, acks[0].GetErrorMessage(), "secret-pw")
	assert.Contains(t, acks[0].GetErrorMessage(), "[REDACTED]")
}

func TestTelemetryDialTargetRejectsPublicAddress(t *testing.T) {
	req := validTelemetryRequest()
	req.IpAddress = "8.8.8.8"

	err := validateDialTarget(telemetryDialTarget(req))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a private or loopback address")
}

func TestTelemetryDialTargetRejectsUnsupportedScheme(t *testing.T) {
	req := validTelemetryRequest()
	req.UrlScheme = "ftp"

	err := validateDialTarget(telemetryDialTarget(req))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported url_scheme")
}

func TestTelemetrySecretBundleForAsymmetricAuth(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	fetcher := &pluginTelemetryFetcher{minerSigningPrivateKeyHex: hex.EncodeToString(privateKey)}
	req := validTelemetryRequest()
	req.SerialNumber = "SN123"

	secret, err := fetcher.secretBundleFor(sdk.Capabilities{capabilityAsymmetricAuthKey: true}, req)

	require.NoError(t, err)
	assert.Equal(t, "v1", secret.Version)
	bearer, ok := secret.Kind.(sdk.BearerToken)
	require.True(t, ok)
	assert.NotEmpty(t, bearer.Token)
}

func TestTelemetrySecretBundleForAsymmetricAuthValidationErrors(t *testing.T) {
	t.Run("missing serial number", func(t *testing.T) {
		fetcher := &pluginTelemetryFetcher{}
		req := validTelemetryRequest()

		_, err := fetcher.secretBundleFor(sdk.Capabilities{capabilityAsymmetricAuthKey: true}, req)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "serial_number is required")
	})

	t.Run("invalid signing key", func(t *testing.T) {
		fetcher := &pluginTelemetryFetcher{minerSigningPrivateKeyHex: "not-hex"}
		req := validTelemetryRequest()
		req.SerialNumber = "SN123"

		_, err := fetcher.secretBundleFor(sdk.Capabilities{capabilityAsymmetricAuthKey: true}, req)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode miner signing private key")
	})
}

func TestTelemetrySecretBundleForBasicAuthValidationErrors(t *testing.T) {
	t.Run("empty username with password is allowed when present", func(t *testing.T) {
		fetcher := &pluginTelemetryFetcher{}
		req := validTelemetryRequest()
		username := ""
		password := "pw"
		req.Username = &username
		req.Password = &password

		secret, err := fetcher.secretBundleFor(nil, req)

		require.NoError(t, err)
		basic, ok := secret.Kind.(sdk.UsernamePassword)
		require.True(t, ok)
		assert.Equal(t, "", basic.Username)
		assert.Equal(t, "pw", basic.Password)
	})

	t.Run("password without username", func(t *testing.T) {
		fetcher := &pluginTelemetryFetcher{}
		req := validTelemetryRequest()
		password := "pw"
		req.Password = &password

		_, err := fetcher.secretBundleFor(nil, req)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("username without password", func(t *testing.T) {
		fetcher := &pluginTelemetryFetcher{}
		req := validTelemetryRequest()
		username := "root"
		req.Username = &username

		_, err := fetcher.secretBundleFor(nil, req)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "password is required")
	})
}

func TestTelemetryResultFromV2CarriesWarningHealthSeparatelyFromStatus(t *testing.T) {
	result := telemetryResultFromV2("node-device", modelsV2.DeviceMetrics{
		Timestamp: time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
		Health:    modelsV2.HealthWarning,
	}, telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE)

	assert.Equal(t, telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE, result.GetDeviceStatus())
	assert.Equal(t, telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_WARNING, result.GetHealthStatus())
}

func TestValidateTelemetryMetricsIdentity(t *testing.T) {
	t.Run("allows matching identifier", func(t *testing.T) {
		err := validateTelemetryMetricsIdentity("node-device", modelsV2.DeviceMetrics{DeviceIdentifier: "node-device"})
		require.NoError(t, err)
	})

	t.Run("allows empty plugin identifier", func(t *testing.T) {
		err := validateTelemetryMetricsIdentity("node-device", modelsV2.DeviceMetrics{})
		require.NoError(t, err)
	})

	t.Run("rejects mismatched identifier", func(t *testing.T) {
		err := validateTelemetryMetricsIdentity("node-device", modelsV2.DeviceMetrics{DeviceIdentifier: "other-device"})

		require.Error(t, err)
		var ce *commandError
		require.True(t, errors.As(err, &ce))
		assert.Equal(t, pb.AckCode_ACK_CODE_SCAN_FAILED, ce.code)
		assert.Contains(t, err.Error(), "device_identifier mismatch")
	})
}

func TestTelemetryErrorClassificationRedactsSecrets(t *testing.T) {
	code, msg := classifyTelemetryError("fetch telemetry", errors.New("bearer token abc123 failed"), "abc123")

	assert.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, code)
	assert.NotContains(t, msg, "abc123")
	assert.Contains(t, msg, "[REDACTED]")
}

func TestClassifyTelemetryError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want pb.AckCode
	}{
		{
			name: "auth failure",
			err:  sdk.NewErrorAuthenticationFailed("node-device"),
			want: pb.AckCode_ACK_CODE_UNAUTHENTICATED,
		},
		{
			name: "sdk unavailable",
			err:  sdk.NewErrorDeviceUnavailable("node-device"),
			want: pb.AckCode_ACK_CODE_SCAN_FAILED,
		},
		{
			name: "grpc deadline exceeded",
			err:  grpcstatus.Error(codes.DeadlineExceeded, "timed out"),
			want: pb.AckCode_ACK_CODE_SCAN_FAILED,
		},
		{
			name: "generic failure",
			err:  errors.New("plugin exploded"),
			want: pb.AckCode_ACK_CODE_INTERNAL,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, msg := classifyTelemetryError("fetch telemetry", tc.err)

			assert.Equal(t, tc.want, code)
			assert.Contains(t, msg, "fetch telemetry")
		})
	}
}

func validTelemetryRequest() *telemetrypb.FleetNodeTelemetryRequest {
	return &telemetrypb.FleetNodeTelemetryRequest{
		DeviceIdentifier: "node-device",
		IpAddress:        "10.0.0.5",
		Port:             "80",
		UrlScheme:        "http",
		DriverName:       "antminer",
	}
}
