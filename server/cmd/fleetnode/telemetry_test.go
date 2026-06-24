package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
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

func validTelemetryRequest() *telemetrypb.FleetNodeTelemetryRequest {
	return &telemetrypb.FleetNodeTelemetryRequest{
		DeviceIdentifier: "node-device",
		IpAddress:        "10.0.0.5",
		Port:             "80",
		UrlScheme:        "http",
		DriverName:       "antminer",
	}
}
