package miner

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

func TestRemoteFleetNodeMinerGetDeviceMetricsHappyPath(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	assertTelemetryRequest(t, cmd, "node-device")
	publishTelemetryAck(t, stream, cmd.GetCommandId(), telemetryResult("node-device"))

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	assert.Equal(t, "node-device", got.metrics.DeviceIdentifier)
	assert.Equal(t, 100.0, got.metrics.HashrateHS.Value)
	assert.Equal(t, "fw-1", got.metrics.FirmwareVersion)
}

func TestRemoteFleetNodeMinerGetDeviceMetricsNoActiveStream(t *testing.T) {
	miner := newTestRemoteFleetNodeMiner(t, control.NewRegistry())

	_, err := miner.GetDeviceMetrics(context.Background())

	require.Error(t, err)
	assert.True(t, fleeterror.IsConnectionError(err))
}

func TestRemoteFleetNodeMinerGetDeviceMetricsRejectsNonOKAck(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	stream.PublishAck(&gatewaypb.ControlAck{
		CommandId:    cmd.GetCommandId(),
		Code:         gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE,
		ErrorMessage: "driver missing",
	})

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.True(t, fleeterror.IsUnimplementedError(got.err))
}

func TestRemoteFleetNodeMinerGetDeviceMetricsMapsUnauthenticatedAck(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	stream.PublishAck(&gatewaypb.ControlAck{
		CommandId:    cmd.GetCommandId(),
		Code:         gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED,
		ErrorMessage: "bad credentials",
	})

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.True(t, fleeterror.IsAuthenticationError(got.err))
}

func TestRemoteFleetNodeMinerGetDeviceMetricsRejectsMalformedPayload(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	stream.PublishAck(&gatewaypb.ControlAck{
		CommandId: cmd.GetCommandId(),
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   []byte{0xff},
	})

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "unmarshal fleet node telemetry payload")
}

func TestRemoteFleetNodeMinerGetDeviceMetricsRejectsInvalidPayload(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	result := telemetryResult("node-device")
	result.Timestamp = nil
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "invalid fleet node telemetry payload")
}

func TestRemoteFleetNodeMinerGetDeviceMetricsRejectsNonFiniteMetric(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	result := telemetryResult("node-device")
	result.HashrateHs = ptrFloat64(math.NaN())
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "non-finite")
}

func TestRemoteFleetNodeMinerGetDeviceMetricsRejectsMismatchedIdentifier(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	publishTelemetryAck(t, stream, cmd.GetCommandId(), telemetryResult("other-device"))

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "device_identifier mismatch")
}

func TestRemoteFleetNodeMinerGetDeviceStatusReusesFreshTelemetryResult(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	publishTelemetryAck(t, stream, cmd.GetCommandId(), telemetryResult("node-device"))
	require.NoError(t, receiveMetricsResult(t, results).err)

	status, err := miner.GetDeviceStatus(context.Background())

	require.NoError(t, err)
	assert.Equal(t, models.MinerStatusActive, status)
	select {
	case cmd := <-stream.Outgoing:
		t.Fatalf("status should have reused cached telemetry result, got extra command %q", cmd.GetCommandId())
	default:
	}
}

type metricsResult struct {
	metrics modelsV2.DeviceMetrics
	err     error
}

func newTestRemoteFleetNodeMiner(t *testing.T, registry *control.Registry) *RemoteFleetNodeMiner {
	t.Helper()
	miner, err := newRemoteFleetNodeMiner(remoteTelemetryRoute{
		fleetNodeID:      12,
		orgID:            7,
		deviceIdentifier: "node-device",
		driverName:       "antminer",
		manufacturer:     "Bitmain",
		model:            "S19",
		firmwareVersion:  "fw-0",
		serialNumber:     "SN123",
		macAddress:       "aa:bb:cc:dd:ee:ff",
		ipAddress:        "10.0.0.5",
		port:             "80",
		urlScheme:        "http",
		username:         "root",
		password:         "pw",
	}, registry, nil)
	require.NoError(t, err)
	return miner
}

func receiveRemoteCommand(t *testing.T, stream *control.Stream) *gatewaypb.ControlCommand {
	t.Helper()
	select {
	case cmd := <-stream.Outgoing:
		return cmd
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remote command")
		return nil
	}
}

func receiveMetricsResult(t *testing.T, ch <-chan metricsResult) metricsResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for metrics result")
		return metricsResult{}
	}
}

func assertTelemetryRequest(t *testing.T, cmd *gatewaypb.ControlCommand, wantDeviceID string) {
	t.Helper()
	env := &gatewaypb.AgentCommand{}
	require.NoError(t, proto.Unmarshal(cmd.GetPayload(), env))
	req := env.GetTelemetry()
	require.NotNil(t, req)
	assert.Equal(t, wantDeviceID, req.GetDeviceIdentifier())
	assert.Equal(t, "10.0.0.5", req.GetIpAddress())
	assert.Equal(t, "root", req.GetUsername())
	assert.Equal(t, "pw", req.GetPassword())
}

func publishTelemetryAck(t *testing.T, stream *control.Stream, commandID string, result *telemetrypb.FleetNodeTelemetryResult) {
	t.Helper()
	payload, err := proto.Marshal(result)
	require.NoError(t, err)
	stream.PublishAck(&gatewaypb.ControlAck{
		CommandId: commandID,
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	})
}

func telemetryResult(deviceID string) *telemetrypb.FleetNodeTelemetryResult {
	return &telemetrypb.FleetNodeTelemetryResult{
		DeviceIdentifier: deviceID,
		Timestamp:        timestamppb.New(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)),
		FirmwareVersion:  "fw-1",
		DeviceStatus:     telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE,
		HashrateHs:       ptrFloat64(100),
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}
