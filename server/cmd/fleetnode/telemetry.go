package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Keep node-side plugin work below TelemetryService's default 5s MetricTimeout so
// a successful near-deadline sample still has slack to marshal and return its ack.
var telemetryCommandTimeout = 4 * time.Second

// Give context-aware plugins a small window to observe fetchCtx cancellation and
// return their own error before the supervisor abandons context-ignoring calls.
var telemetrySupervisorGrace = 100 * time.Millisecond

const maxTelemetryDeviceMetricsJSONBytes = 256 * 1024
const maxTelemetryFirmwareVersionBytes = 255

var telemetryDeviceCloseTimeout = 5 * time.Second
var telemetryDeviceCloseSupervisorGrace = 100 * time.Millisecond
var telemetryDeviceCloseTokens = make(chan struct{}, commandPoolSize)

type telemetryFetcher interface {
	Fetch(ctx context.Context, req *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error)
}

type abandonedTelemetryFetcher interface {
	AbandonTelemetryFetch(deviceIdentifier string) bool
}

type telemetryFetchOutcome struct {
	result *telemetrypb.FleetNodeTelemetryResult
	err    error
}

type pluginTelemetryFetcher struct {
	manager      *plugins.Manager
	minerSecrets secretProvider
	mu           sync.Mutex
	inflight     map[string]*telemetrySlot
	workerTokens chan struct{}
}

type telemetrySlot struct {
	fetcher          *pluginTelemetryFetcher
	deviceIdentifier string
	released         bool
}

func newPluginTelemetryFetcher(manager *plugins.Manager, minerSecrets secretProvider) (*pluginTelemetryFetcher, error) {
	if manager == nil {
		return nil, fmt.Errorf("plugin manager is required")
	}
	if minerSecrets == nil {
		return nil, fmt.Errorf("miner secret provider is required")
	}
	return &pluginTelemetryFetcher{
		manager:      manager,
		minerSecrets: minerSecrets,
	}, nil
}

func (r *RunCmd) handleTelemetryCommand(ctx context.Context, stream acker, commandID string, req *telemetrypb.FleetNodeTelemetryRequest, logger *slog.Logger) {
	if r.telemetry == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "telemetry unavailable: no plugins loaded", logger)
		return
	}
	if vErr := protovalidate.Validate(req); vErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid telemetry request: %v", vErr), logger)
		return
	}

	result, err := supervisedTelemetryFetch(ctx, r.telemetry, req, telemetryTimeout(req), logger)
	if err != nil {
		code := pb.AckCode_ACK_CODE_INTERNAL
		var ce *commandError
		if errors.As(err, &ce) {
			code = ce.code
		}
		r.sendAck(stream, commandID, code, redactTelemetrySecrets(err.Error()), logger)
		return
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, fmt.Sprintf("marshal telemetry result: %v", err), logger)
		return
	}
	r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_OK, "", payload, logger)
}

func supervisedTelemetryFetch(ctx context.Context, fetcher telemetryFetcher, req *telemetrypb.FleetNodeTelemetryRequest, timeout time.Duration, logger *slog.Logger) (*telemetrypb.FleetNodeTelemetryResult, error) {
	if timeout <= 0 {
		timeout = telemetryCommandTimeout
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	outcomeCh := make(chan telemetryFetchOutcome, 1)
	go func() {
		result, err := fetcher.Fetch(fetchCtx, req)
		outcomeCh <- telemetryFetchOutcome{result: result, err: err}
	}()

	supervisorBudget := timeout + telemetrySupervisorGrace
	timer := time.NewTimer(supervisorBudget)
	defer timer.Stop()
	select {
	case outcome := <-outcomeCh:
		return outcome.result, outcome.err
	case <-timer.C:
		cancel()
		abandoned := abandonTelemetryFetch(fetcher, req)
		logger.Warn("telemetry fetch exceeded supervisor budget; returning failed ack",
			"device_identifier", req.GetDeviceIdentifier(),
			"timeout", timeout.String(),
			"supervisor_budget", supervisorBudget.String(),
			"abandoned", abandoned)
		return nil, cmdErr(pb.AckCode_ACK_CODE_SCAN_FAILED, "telemetry supervisor budget exceeded after %s", supervisorBudget)
	case <-ctx.Done():
		cancel()
		abandonTelemetryFetch(fetcher, req)
		return nil, cmdErr(pb.AckCode_ACK_CODE_SCAN_FAILED, "telemetry command context ended: %v", ctx.Err())
	}
}

func abandonTelemetryFetch(fetcher telemetryFetcher, req *telemetrypb.FleetNodeTelemetryRequest) bool {
	abandoner, ok := fetcher.(abandonedTelemetryFetcher)
	return ok && abandoner.AbandonTelemetryFetch(req.GetDeviceIdentifier())
}

func (f *pluginTelemetryFetcher) Fetch(ctx context.Context, req *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	plugin, err := f.manager.GetPluginByDriverNameWithCapability(req.GetDriverName(), sdk.CapabilityRealtimeTelemetry)
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "no telemetry-capable driver %q: %v", req.GetDriverName(), err)
	}

	target := telemetryDialTarget(req)
	if err := validateDialTarget(target); err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%v", err)
	}
	port, err := sdk.ParsePort(req.GetPort())
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid port %q: %v", req.GetPort(), err)
	}
	deviceInfo := sdk.DeviceInfo{
		Host:            req.GetIpAddress(),
		Port:            port,
		URLScheme:       req.GetUrlScheme(),
		SerialNumber:    req.GetSerialNumber(),
		Model:           req.GetModel(),
		Manufacturer:    req.GetManufacturer(),
		MacAddress:      req.GetMacAddress(),
		FirmwareVersion: req.GetFirmwareVersion(),
	}
	secret, err := f.minerSecrets.SecretBundle(target)
	if err != nil {
		code, msg := classifyMinerCommandError("build telemetry secret bundle", err)
		return nil, cmdErr(code, "%s", msg)
	}
	slot, err := f.acquireTelemetrySlot(req.GetDeviceIdentifier())
	if err != nil {
		return nil, err
	}
	defer slot.releaseWorker()

	redactions := telemetrySecretRedactions(secret)
	created, err := plugin.Driver.NewDevice(ctx, req.GetDeviceIdentifier(), deviceInfo, secret)
	if err != nil {
		code, msg := classifyTelemetryError("create telemetry device", err, redactions...)
		return nil, cmdErr(code, "%s", msg)
	}
	defer closeTelemetryDeviceAsync(created.Device)

	sdkMetrics, err := created.Device.Status(ctx)
	if err != nil {
		code, msg := classifyTelemetryError("fetch telemetry", err, redactions...)
		return nil, cmdErr(code, "%s", msg)
	}
	v2Metrics := mappers.SDKDeviceMetricsToV2(sdkMetrics)
	if err := validateTelemetryMetricsIdentity(req.GetDeviceIdentifier(), v2Metrics); err != nil {
		return nil, err
	}
	result, err := telemetryResultFromV2(req.GetDeviceIdentifier(), v2Metrics, deviceStatusFromSDKHealth(sdkMetrics.Health))
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_INTERNAL, "marshal telemetry metrics: %v", err)
	}
	return result, nil
}

func (f *pluginTelemetryFetcher) acquireTelemetrySlot(deviceIdentifier string) (*telemetrySlot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initTelemetrySlotsLocked()

	if _, loaded := f.inflight[deviceIdentifier]; loaded {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BUSY, "telemetry already in progress for device %q", deviceIdentifier)
	}
	select {
	case f.workerTokens <- struct{}{}:
	default:
		return nil, cmdErr(pb.AckCode_ACK_CODE_BUSY, "telemetry worker capacity exhausted")
	}
	slot := &telemetrySlot{
		fetcher:          f,
		deviceIdentifier: deviceIdentifier,
	}
	f.inflight[deviceIdentifier] = slot
	return slot, nil
}

func (f *pluginTelemetryFetcher) initTelemetrySlotsLocked() {
	if f.inflight == nil {
		f.inflight = make(map[string]*telemetrySlot)
	}
	if f.workerTokens == nil {
		f.workerTokens = make(chan struct{}, commandPoolSize)
	}
}

func (f *pluginTelemetryFetcher) AbandonTelemetryFetch(deviceIdentifier string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	slot := f.inflight[deviceIdentifier]
	return slot != nil
}

func (s *telemetrySlot) releaseWorker() {
	s.fetcher.mu.Lock()
	defer s.fetcher.mu.Unlock()
	if s.released {
		return
	}
	s.released = true
	if s.fetcher.inflight[s.deviceIdentifier] == s {
		delete(s.fetcher.inflight, s.deviceIdentifier)
	}
	<-s.fetcher.workerTokens
}

type telemetryDeviceCloser interface {
	Close(ctx context.Context) error
}

func closeTelemetryDeviceAsync(device telemetryDeviceCloser) bool {
	closeTokens := telemetryDeviceCloseTokens
	select {
	case closeTokens <- struct{}{}:
	default:
		slog.Warn("telemetry device close worker capacity is exhausted; closing synchronously")
		closeTelemetryDevice(device)
		return false
	}
	go func() {
		defer func() { <-closeTokens }()
		closeTelemetryDevice(device)
	}()
	return true
}

func closeTelemetryDevice(device telemetryDeviceCloser) {
	closeCtx, cancel := context.WithTimeout(context.Background(), telemetryDeviceCloseTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- device.Close(closeCtx)
	}()

	timer := time.NewTimer(telemetryDeviceCloseTimeout + telemetryDeviceCloseSupervisorGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			slog.Warn("telemetry device close failed", "err", err)
		}
	case <-timer.C:
		slog.Warn("telemetry device close exceeded supervisor budget")
	}
}

func validateTelemetryMetricsIdentity(requestedDeviceIdentifier string, metrics modelsV2.DeviceMetrics) error {
	if metrics.DeviceIdentifier == "" || metrics.DeviceIdentifier == requestedDeviceIdentifier {
		return nil
	}
	return cmdErr(pb.AckCode_ACK_CODE_SCAN_FAILED, "telemetry device_identifier mismatch: requested %q, plugin reported %q", requestedDeviceIdentifier, metrics.DeviceIdentifier)
}

func telemetryDialTarget(req *telemetrypb.FleetNodeTelemetryRequest) *pb.MinerConnectionDescriptor {
	return &pb.MinerConnectionDescriptor{
		DeviceIdentifier:   req.GetDeviceIdentifier(),
		DriverName:         req.GetDriverName(),
		IpAddress:          req.GetIpAddress(),
		Port:               req.GetPort(),
		UrlScheme:          req.GetUrlScheme(),
		SerialNumber:       req.GetSerialNumber(),
		MacAddress:         req.GetMacAddress(),
		CredentialUsername: req.GetCredentialUsername(),
		CredentialPassword: req.GetCredentialPassword(),
	}
}

func telemetrySecretRedactions(secret sdk.SecretBundle) []string {
	switch kind := secret.Kind.(type) {
	case sdk.UsernamePassword:
		return []string{kind.Password}
	case sdk.BearerToken:
		return []string{kind.Token}
	default:
		return nil
	}
}

func redactTelemetrySecrets(msg string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[REDACTED]")
	}
	return msg
}

func telemetryTimeout(req *telemetrypb.FleetNodeTelemetryRequest) time.Duration {
	if req.GetTimeout() != nil {
		if timeout := req.GetTimeout().AsDuration(); timeout > 0 {
			return timeout
		}
	}
	return telemetryCommandTimeout
}

func classifyTelemetryError(stage string, err error, redactions ...string) (pb.AckCode, string) {
	msg := redactTelemetrySecrets(fmt.Sprintf("%s: %v", stage, err), redactions...)
	if isDefaultPasswordActiveTelemetryError(err) {
		return pb.AckCode_ACK_CODE_FORBIDDEN, msg
	}
	if isNodeAuthFailure(err) {
		return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
	}
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		if sdkErr.Code == sdk.ErrCodeDeviceUnavailable {
			return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
		}
	}
	if st, ok := grpcstatus.FromError(err); ok {
		code := st.Code()
		if code == codes.Unavailable || code == codes.DeadlineExceeded || code == codes.NotFound {
			return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
	}
	if plugins.IsNetworkError(err) {
		return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
	}
	return pb.AckCode_ACK_CODE_INTERNAL, msg
}

func isDefaultPasswordActiveTelemetryError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "default password must be changed") ||
		strings.Contains(msg, "default_password_active")
}

func telemetryResultFromV2(deviceIdentifier string, metrics modelsV2.DeviceMetrics, status telemetrypb.DeviceStatus) (*telemetrypb.FleetNodeTelemetryResult, error) {
	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now().UTC()
	}
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("marshal device metrics: %w", err)
	}
	if len(metricsJSON) > maxTelemetryDeviceMetricsJSONBytes {
		return nil, fmt.Errorf("device metrics payload is %d bytes, max %d", len(metricsJSON), maxTelemetryDeviceMetricsJSONBytes)
	}
	result := &telemetrypb.FleetNodeTelemetryResult{
		DeviceIdentifier:  deviceIdentifier,
		Timestamp:         timestamppb.New(metrics.Timestamp),
		FirmwareVersion:   truncateUTF8(metrics.FirmwareVersion, maxTelemetryFirmwareVersionBytes),
		DeviceStatus:      status,
		HealthStatus:      healthStatusFromV2(metrics.Health),
		HashrateHs:        metricValue(metrics.HashrateHS),
		TempC:             metricValue(metrics.TempC),
		FanRpm:            metricValue(metrics.FanRPM),
		PowerW:            metricValue(metrics.PowerW),
		EfficiencyJh:      metricValue(metrics.EfficiencyJH),
		DeviceMetricsJson: metricsJSON,
	}
	if metrics.HealthReason != nil {
		result.HealthReason = truncateUTF8(*metrics.HealthReason, maxAckErrorMessageBytes)
	}
	if metrics.DefaultPasswordActive != nil {
		result.DefaultPasswordActive = metrics.DefaultPasswordActive
	}
	return result, nil
}

func metricValue(metric *modelsV2.MetricValue) *float64 {
	if metric == nil {
		return nil
	}
	value := metric.Value
	return &value
}

func healthStatusFromV2(health modelsV2.HealthStatus) telemetrypb.DeviceHealthStatus {
	switch health {
	case modelsV2.HealthHealthyActive:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_ACTIVE
	case modelsV2.HealthHealthyInactive:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_INACTIVE
	case modelsV2.HealthWarning:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_WARNING
	case modelsV2.HealthCritical:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_CRITICAL
	case modelsV2.HealthUnknown:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNKNOWN
	default:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNKNOWN
	}
}

func deviceStatusFromSDKHealth(health sdk.HealthStatus) telemetrypb.DeviceStatus {
	switch health {
	case sdk.HealthHealthyActive:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE
	case sdk.HealthHealthyInactive:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_INACTIVE
	case sdk.HealthWarning:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE
	case sdk.HealthCritical:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_ERROR
	case sdk.HealthNeedsMiningPool:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
	case sdk.HealthUnknown, sdk.HealthStatusUnspecified:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_OFFLINE
	default:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_OFFLINE
	}
}
