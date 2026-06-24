package miner

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
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

func TestRemoteFleetNodeMinerGetDeviceMetricsPreservesComponentPayload(t *testing.T) {
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
	payloadMetrics := modelsV2.DeviceMetrics{
		DeviceIdentifier: "plugin-reported-id",
		Timestamp:        result.GetTimestamp().AsTime(),
		FirmwareVersion:  "plugin-fw",
		Health:           modelsV2.HealthHealthyActive,
		HashBoards: []modelsV2.HashBoardMetrics{{
			ComponentInfo: modelsV2.ComponentInfo{Index: 0, Name: "board-0"},
			TempC:         &modelsV2.MetricValue{Value: 72, Kind: modelsV2.MetricKindGauge},
			ASICs: []modelsV2.ASICMetrics{{
				ComponentInfo: modelsV2.ComponentInfo{Index: 2, Name: "asic-2"},
				TempC:         &modelsV2.MetricValue{Value: 83, Kind: modelsV2.MetricKindGauge},
			}},
		}},
	}
	payload, err := json.Marshal(payloadMetrics)
	require.NoError(t, err)
	result.DeviceMetricsJson = payload
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	assert.Equal(t, "node-device", got.metrics.DeviceIdentifier)
	assert.Equal(t, "fw-1", got.metrics.FirmwareVersion)
	require.Len(t, got.metrics.HashBoards, 1)
	require.Len(t, got.metrics.HashBoards[0].ASICs, 1)
	assert.Equal(t, 83.0, got.metrics.HashBoards[0].ASICs[0].TempC.Value)
}

func TestRemoteFleetNodeMinerGetDeviceMetricsUsesNodeLimiter(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	gate := &recordingTelemetryGate{}
	miner := newTestRemoteFleetNodeMinerWithGate(t, registry, gate)

	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	assert.Equal(t, []int64{12}, gate.acquired)
	assert.Empty(t, gate.released)
	publishTelemetryAck(t, stream, cmd.GetCommandId(), telemetryResult("node-device"))

	require.NoError(t, receiveMetricsResult(t, results).err)
	assert.Equal(t, []int64{12}, gate.released)
}

func TestRemoteFleetNodeMinerGetDeviceMetricsStopsWaitingForLimiterWhenCallerExpires(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	gate := newBlockingTelemetryGate()
	miner := newTestRemoteFleetNodeMinerWithGate(t, registry, gate)

	results := make(chan metricsResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() {
		metrics, err := miner.GetDeviceMetrics(ctx)
		results <- metricsResult{metrics: metrics, err: err}
	}()

	require.Equal(t, int64(12), gate.waitForAcquire(t))
	got := receiveMetricsResult(t, results)

	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "timed out waiting for a fleet node telemetry command slot")
	select {
	case cmd := <-stream.Outgoing:
		t.Fatalf("caller deadline should prevent sending command after limiter wait, got command %q", cmd.GetCommandId())
	default:
	}
}

func TestRemoteFleetNodeMinerGetDeviceMetricsSendsContextTimeoutWithSlack(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	go func() {
		metrics, err := miner.GetDeviceMetrics(ctx)
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	env := &gatewaypb.AgentCommand{}
	require.NoError(t, proto.Unmarshal(cmd.GetPayload(), env))
	req := env.GetTelemetry()
	require.NotNil(t, req)
	require.NotNil(t, req.GetTimeout())
	assert.Greater(t, req.GetTimeout().AsDuration(), 5*time.Second)
	assert.LessOrEqual(t, req.GetTimeout().AsDuration(), 7*time.Second)
	publishTelemetryAck(t, stream, cmd.GetCommandId(), telemetryResult("node-device"))

	require.NoError(t, receiveMetricsResult(t, results).err)
}

func TestRemoteFleetNodeMinerGetDeviceMetricsNoActiveStream(t *testing.T) {
	miner := newTestRemoteFleetNodeMiner(t, control.NewRegistry())

	_, err := miner.GetDeviceMetrics(context.Background())

	require.Error(t, err)
	assert.True(t, fleeterror.IsConnectionError(err))
}

func TestRemoteFleetNodeMinerGetDeviceMetricsMapsAckTimeoutToConnectionError(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)

	results := make(chan metricsResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() {
		metrics, err := miner.GetDeviceMetrics(ctx)
		results <- metricsResult{metrics: metrics, err: err}
	}()

	_ = receiveRemoteCommand(t, stream)
	got := receiveMetricsResult(t, results)

	require.Error(t, got.err)
	assert.True(t, fleeterror.IsConnectionError(got.err))
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

func TestRemoteFleetNodeMinerGetDeviceMetricsRejectsFailedOKAck(t *testing.T) {
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
		Succeeded:    false,
		Code:         gatewaypb.AckCode_ACK_CODE_OK,
		ErrorMessage: "agent rejected telemetry command",
	})

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "agent rejected telemetry command")
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

func TestRemoteFleetNodeMinerGetDeviceMetricsMapsForbiddenAck(t *testing.T) {
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
		Code:         gatewaypb.AckCode_ACK_CODE_FORBIDDEN,
		ErrorMessage: "default password must be changed",
	})

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.True(t, fleeterror.IsForbiddenError(got.err))
}

func TestRemoteFleetNodeMinerGetDeviceMetricsMapsScanFailedAckToConnectionError(t *testing.T) {
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
		Code:         gatewaypb.AckCode_ACK_CODE_SCAN_FAILED,
		ErrorMessage: "miner unreachable",
	})

	got := receiveMetricsResult(t, results)
	require.Error(t, got.err)
	assert.True(t, fleeterror.IsConnectionError(got.err))
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

func TestRemoteFleetNodeMinerGetDeviceMetricsCarriesDefaultPasswordActive(t *testing.T) {
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
	active := true
	result := telemetryResult("node-device")
	result.DefaultPasswordActive = &active
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	require.NotNil(t, got.metrics.DefaultPasswordActive)
	assert.True(t, *got.metrics.DefaultPasswordActive)
}

func TestRemoteFleetNodeMinerGetDeviceMetricsPreservesTelemetryHealthWarning(t *testing.T) {
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
	result.DeviceStatus = telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE
	result.HealthStatus = telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_WARNING
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	assert.Equal(t, modelsV2.HealthWarning, got.metrics.Health)
}

func TestRemoteFleetNodeMinerGetDeviceMetricsFallsBackToStatusForLegacyHealth(t *testing.T) {
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
	result.DeviceStatus = telemetrypb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
	result.HealthStatus = telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNSPECIFIED
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	assert.Equal(t, modelsV2.HealthHealthyInactive, got.metrics.Health)
}

func TestRemoteFleetNodeMinerNeedsMiningPoolKeepsExactStatus(t *testing.T) {
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
	result.DeviceStatus = telemetrypb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
	result.HealthStatus = telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_INACTIVE
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	assert.Equal(t, modelsV2.HealthHealthyInactive, got.metrics.Health)

	status, err := miner.GetDeviceStatus(context.Background())

	require.NoError(t, err)
	assert.Equal(t, models.MinerStatusNeedsMiningPool, status)
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
	return newTestRemoteFleetNodeMinerWithGate(t, registry, nil)
}

func newTestRemoteFleetNodeMinerWithGate(t *testing.T, registry *control.Registry, gate remotenode.Gate) *RemoteFleetNodeMiner {
	t.Helper()
	miner, err := newRemoteFleetNodeMiner(remoteTelemetryRoute{
		fleetNodeID:        12,
		orgID:              7,
		deviceIdentifier:   "node-device",
		driverName:         "antminer",
		manufacturer:       "Bitmain",
		model:              "S19",
		firmwareVersion:    "fw-0",
		serialNumber:       "SN123",
		macAddress:         "aa:bb:cc:dd:ee:ff",
		ipAddress:          "10.0.0.5",
		port:               "80",
		urlScheme:          "http",
		credentialUsername: []byte("node-encrypted-user"),
		credentialPassword: []byte("node-encrypted-pass"),
	}, registry, gate, nil)
	require.NoError(t, err)
	return miner
}

type recordingTelemetryGate struct {
	acquired []int64
	released []int64
}

func (g *recordingTelemetryGate) Acquire(_ context.Context, fleetNodeID int64) (func(), error) {
	g.acquired = append(g.acquired, fleetNodeID)
	return func() { g.released = append(g.released, fleetNodeID) }, nil
}

type blockingTelemetryGate struct {
	acquired chan int64
	release  chan struct{}
}

func newBlockingTelemetryGate() *blockingTelemetryGate {
	return &blockingTelemetryGate{
		acquired: make(chan int64, 1),
		release:  make(chan struct{}),
	}
}

func (g *blockingTelemetryGate) Acquire(ctx context.Context, fleetNodeID int64) (func(), error) {
	g.acquired <- fleetNodeID
	select {
	case <-g.release:
		return func() {}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for test limiter release: %w", ctx.Err())
	}
}

func (g *blockingTelemetryGate) waitForAcquire(t *testing.T) int64 {
	t.Helper()
	select {
	case fleetNodeID := <-g.acquired:
		return fleetNodeID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for limiter acquire")
		return 0
	}
}

func (g *blockingTelemetryGate) releaseAcquire() {
	close(g.release)
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
	assert.Equal(t, []byte("node-encrypted-user"), req.GetCredentialUsername())
	assert.Equal(t, []byte("node-encrypted-pass"), req.GetCredentialPassword())
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
		HealthStatus:     telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_ACTIVE,
		HashrateHs:       ptrFloat64(100),
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}
