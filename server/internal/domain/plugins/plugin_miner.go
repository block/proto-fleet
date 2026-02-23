package plugins

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	diagnosticsModels "github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/plugins/mappers"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

var _ interfaces.Miner = &PluginMiner{}
var _ interfaces.MinerInfo = &PluginMiner{}

// PluginMiner wraps an SDK Device to implement the interfaces.Miner interface.
//
// Lifecycle Management:
// SDK Devices have a Close() method that should be called to release resources, but the
// interfaces.Miner interface does not include a Close() method. Currently, SDK devices are
// cleaned up implicitly when the plugin process is killed during plugin manager shutdown.
//
// TODO: Consider adding explicit device lifecycle management:
//   - Option 1: Add Close() to interfaces.Miner interface (breaking change)
//   - Option 2: Track SDK devices in plugin manager and close them during shutdown
//   - Option 3: Document that plugin processes handle cleanup on exit
//
// logSaver is the subset of files.Service used by PluginMiner.
type logSaver interface {
	SaveLogs(batchLogUUID string, deviceIdentifier *models.DeviceIdentifier, logLines []string) (string, error)
}

type PluginMiner struct {
	orgID          int64
	deviceID       models.DeviceIdentifier
	deviceType     models.Type
	serialNumber   string
	connectionInfo networking.ConnectionInfo
	sdkDevice      sdk.Device
	deviceInfo     sdk.DeviceInfo
	filesService   logSaver
}

// NewPluginMiner creates a new PluginMiner wrapper around an SDK Device
func NewPluginMiner(
	orgID int64,
	deviceID models.DeviceIdentifier,
	deviceType models.Type,
	serialNumber string,
	connectionInfo networking.ConnectionInfo,
	sdkDevice sdk.Device,
	deviceInfo sdk.DeviceInfo,
	filesService logSaver,
) *PluginMiner {
	return &PluginMiner{
		orgID:          orgID,
		deviceID:       deviceID,
		deviceType:     deviceType,
		serialNumber:   serialNumber,
		connectionInfo: connectionInfo,
		sdkDevice:      sdkDevice,
		deviceInfo:     deviceInfo,
		filesService:   filesService,
	}
}

// GetID implements interfaces.MinerInfo
func (p *PluginMiner) GetID() models.DeviceIdentifier {
	return p.deviceID
}

// GetOrgID implements interfaces.MinerInfo
func (p *PluginMiner) GetOrgID() int64 {
	return p.orgID
}

// GetType implements interfaces.MinerInfo
func (p *PluginMiner) GetType() models.Type {
	return p.deviceType
}

// GetSerialNumber implements interfaces.MinerInfo
func (p *PluginMiner) GetSerialNumber() string {
	return p.serialNumber
}

// GetConnectionInfo implements interfaces.MinerInfo
func (p *PluginMiner) GetConnectionInfo() networking.ConnectionInfo {
	return p.connectionInfo
}

// GetWebViewURL implements interfaces.MinerInfo
func (p *PluginMiner) GetWebViewURL() *url.URL {
	webViewURL, supported, err := p.sdkDevice.TryGetWebViewURL(context.Background())
	if err != nil || !supported || webViewURL == "" {
		return p.connectionInfo.GetURL()
	}

	parsedURL, err := url.Parse(webViewURL)
	if err != nil {
		return nil
	}
	return parsedURL
}

// GetDeviceMetrics implements interfaces.Miner
// This is the critical method that bridges SDK metrics to Fleet's V2 format
func (p *PluginMiner) GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error) {
	sdkMetrics, err := p.sdkDevice.Status(ctx)
	if err != nil {
		return modelsV2.DeviceMetrics{}, fleeterror.NewInternalErrorf("failed to get SDK device metrics: %v", err)
	}

	v2Metrics := mappers.SDKDeviceMetricsToV2(sdkMetrics)

	return v2Metrics, nil
}

// GetDeviceStatus implements interfaces.Miner
func (p *PluginMiner) GetDeviceStatus(ctx context.Context) (models.MinerStatus, error) {
	metrics, err := p.sdkDevice.Status(ctx)
	if err != nil {
		if isNetworkError(err) {
			return models.MinerStatusOffline, fleeterror.NewConnectionError(string(p.deviceID), err)
		}
		return models.MinerStatusOffline, fleeterror.NewInternalErrorf("failed to get device status: %v", err)
	}

	var status models.MinerStatus
	switch metrics.Health {
	case sdk.HealthHealthyActive:
		status = models.MinerStatusActive
	case sdk.HealthHealthyInactive:
		status = models.MinerStatusInactive
	case sdk.HealthWarning:
		status = models.MinerStatusActive // Still operational despite warning
	case sdk.HealthCritical:
		status = models.MinerStatusError
	case sdk.HealthNeedsMiningPool:
		status = models.MinerStatusNeedsMiningPool
	case sdk.HealthUnknown:
		status = models.MinerStatusOffline
	case sdk.HealthStatusUnspecified:
		status = models.MinerStatusOffline
	default:
		status = models.MinerStatusOffline
	}

	return status, nil
}

// Reboot implements interfaces.Miner
func (p *PluginMiner) Reboot(ctx context.Context) error {
	if err := p.sdkDevice.Reboot(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to reboot device: %v", err)
	}
	return nil
}

// StartMining implements interfaces.Miner
func (p *PluginMiner) StartMining(ctx context.Context) error {
	if err := p.sdkDevice.StartMining(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to start mining: %v", err)
	}
	return nil
}

// StopMining implements interfaces.Miner
func (p *PluginMiner) StopMining(ctx context.Context) error {
	if err := p.sdkDevice.StopMining(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to stop mining: %v", err)
	}
	return nil
}

// SetCoolingMode implements interfaces.Miner
func (p *PluginMiner) SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error {
	var sdkMode sdk.CoolingMode
	switch payload.Mode {
	case commonpb.CoolingMode_COOLING_MODE_AIR_COOLED:
		sdkMode = sdk.CoolingModeAirCooled
	case commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED:
		sdkMode = sdk.CoolingModeImmersionCooled
	case commonpb.CoolingMode_COOLING_MODE_MANUAL:
		sdkMode = sdk.CoolingModeManual
	case commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED:
		sdkMode = sdk.CoolingModeUnspecified
	default:
		sdkMode = sdk.CoolingModeUnspecified
	}

	if err := p.sdkDevice.SetCoolingMode(ctx, sdkMode); err != nil {
		return fleeterror.NewInternalErrorf("failed to set cooling mode: %v", err)
	}
	return nil
}

// GetCoolingMode implements interfaces.Miner
func (p *PluginMiner) GetCoolingMode(ctx context.Context) (commonpb.CoolingMode, error) {
	sdkMode, err := p.sdkDevice.GetCoolingMode(ctx)
	if err != nil {
		return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, fleeterror.NewInternalErrorf("failed to get cooling mode: %v", err)
	}

	switch sdkMode {
	case sdk.CoolingModeAirCooled:
		return commonpb.CoolingMode_COOLING_MODE_AIR_COOLED, nil
	case sdk.CoolingModeImmersionCooled:
		return commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED, nil
	case sdk.CoolingModeManual:
		return commonpb.CoolingMode_COOLING_MODE_MANUAL, nil
	case sdk.CoolingModeUnspecified:
		return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, nil
	default:
		return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, nil
	}
}

// SetPowerTarget implements interfaces.Miner
func (p *PluginMiner) SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error {
	var sdkMode sdk.PerformanceMode
	switch payload.PerformanceMode {
	case pb.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE:
		sdkMode = sdk.PerformanceModeMaximumHashrate
	case pb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY:
		sdkMode = sdk.PerformanceModeEfficiency
	case pb.PerformanceMode_PERFORMANCE_MODE_UNSPECIFIED:
		sdkMode = sdk.PerformanceModeUnspecified
	default:
		sdkMode = sdk.PerformanceModeUnspecified
	}

	if err := p.sdkDevice.SetPowerTarget(ctx, sdkMode); err != nil {
		return fleeterror.NewInternalErrorf("failed to set power target: %v", err)
	}
	return nil
}

// UpdateMiningPools implements interfaces.Miner
func (p *PluginMiner) UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
	sdkPools := []sdk.MiningPoolConfig{}

	poolConfig, err := validateAndConvertPoolConfig(payload.DefaultPool, "default")
	if err != nil {
		return err
	}
	sdkPools = append(sdkPools, poolConfig)

	if payload.Backup1Pool != nil {
		poolConfig, err := validateAndConvertPoolConfig(*payload.Backup1Pool, "backup1")
		if err != nil {
			return err
		}
		sdkPools = append(sdkPools, poolConfig)
	}
	if payload.Backup2Pool != nil {
		poolConfig, err := validateAndConvertPoolConfig(*payload.Backup2Pool, "backup2")
		if err != nil {
			return err
		}
		sdkPools = append(sdkPools, poolConfig)
	}

	if err := p.sdkDevice.UpdateMiningPools(ctx, sdkPools); err != nil {
		return fleeterror.NewInternalErrorf("failed to update mining pools: %v", err)
	}
	return nil
}

// BlinkLED implements interfaces.Miner
func (p *PluginMiner) BlinkLED(ctx context.Context) error {
	if err := p.sdkDevice.BlinkLED(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to blink LED: %v", err)
	}
	return nil
}

// DownloadLogs implements interfaces.Miner
func (p *PluginMiner) DownloadLogs(ctx context.Context, batchLogUUID string) error {
	logData, _, err := p.sdkDevice.DownloadLogs(ctx, nil, batchLogUUID)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to download logs: %v", err)
	}
	logLines := strings.Split(strings.TrimRight(logData, "\n"), "\n")
	csvRows := formatLogsToCSV(logLines)
	if _, err := p.filesService.SaveLogs(batchLogUUID, &p.deviceID, csvRows); err != nil {
		return fleeterror.NewInternalErrorf("failed to save logs: %v", err)
	}
	return nil
}

const csvLogHeader = "Time,Type,Message"

// logLevelSeparators maps the separator strings used in Proto miner log lines to their display label.
// Format: "{prefix}: {timestamp} | LEVEL | {message}"
var logLevelSeparators = []struct {
	separator string
	label     string
}{
	{" | ERROR | ", "ERROR"},
	{" | WARN  | ", "WARN"},
	{" | INFO  | ", "INFO"},
	{" | DEBUG | ", "DEBUG"},
}

// formatLogsToCSV converts raw log lines into CSV rows matching the ProtoOS log viewer format.
// Header row: Time,Type,Message
// Data rows: {timestamp},{level},"{message}"
func formatLogsToCSV(logLines []string) []string {
	rows := make([]string, 0, len(logLines)+1)
	rows = append(rows, csvLogHeader)
	for _, line := range logLines {
		rows = append(rows, formatLogLineToCSVRow(line))
	}
	return rows
}

// formatLogLineToCSVRow parses a single log line into a CSV row.
// Handles two formats:
//   - Proto miner syslog: "{prefix}: {timestamp} | LEVEL | {message}"
//   - Antminer kernel log: "[timestamp] message"
//
// Matches the parsing logic in the ProtoOS frontend utility.ts formatLog function.
func formatLogLineToCSVRow(line string) string {
	for _, level := range logLevelSeparators {
		idx := strings.Index(line, level.separator)
		if idx < 0 {
			continue
		}
		prefix := line[:idx]
		message := line[idx+len(level.separator):]

		// Matches TypeScript: info[0].split(": ")?.[1]
		timestamp := ""
		if parts := strings.SplitN(prefix, ": ", 2); len(parts) == 2 {
			ts := parts[1]
			// Matches TypeScript: timestamp?.split(".")?.[0]
			if dotIdx := strings.Index(ts, "."); dotIdx >= 0 {
				ts = ts[:dotIdx]
			}
			timestamp = ts
		}

		escapedMessage := strings.ReplaceAll(message, `"`, `""`)
		return fmt.Sprintf("%s,%s,\"%s\"", timestamp, level.label, escapedMessage)
	}

	// Try [timestamp] message format used by Antminer kernel logs,
	// e.g. "[2026-01-01T00:00:00Z] message" or "[    0.000000] message".
	// Only treat bracketed content as a timestamp when it contains at least one digit,
	// which distinguishes real timestamps from keyword tokens like "[INFO]".
	if strings.HasPrefix(line, "[") {
		if closeBracket := strings.Index(line, "]"); closeBracket > 0 {
			potentialTS := line[1:closeBracket]
			if strings.ContainsAny(potentialTS, "0123456789") {
				timestamp := strings.TrimSpace(potentialTS)
				message := strings.TrimPrefix(line[closeBracket+1:], " ")
				escapedMessage := strings.ReplaceAll(message, `"`, `""`)
				return fmt.Sprintf("%s,,\"%s\"", timestamp, escapedMessage)
			}
		}
	}

	escapedLine := strings.ReplaceAll(line, `"`, `""`)
	return fmt.Sprintf(",,\"%s\"", escapedLine)
}

// FirmwareUpdate implements interfaces.Miner
func (p *PluginMiner) FirmwareUpdate(ctx context.Context) error {
	if err := p.sdkDevice.FirmwareUpdate(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to update firmware: %v", err)
	}
	return nil
}

// Unpair implements interfaces.Miner
func (p *PluginMiner) Unpair(ctx context.Context) error {
	if err := p.sdkDevice.Unpair(ctx); err != nil {
		return fleeterror.NewInternalErrorf("failed to unpair device: %v", err)
	}
	return nil
}

// UpdateMinerPassword implements interfaces.Miner
func (p *PluginMiner) UpdateMinerPassword(ctx context.Context, payload dto.UpdateMinerPasswordPayload) error {
	if err := p.sdkDevice.UpdateMinerPassword(ctx, payload.CurrentPassword, payload.NewPassword); err != nil {
		return fleeterror.NewInternalErrorf("failed to update miner password: %v", err)
	}
	return nil
}

// GetErrors implements interfaces.Miner
func (p *PluginMiner) GetErrors(ctx context.Context) (diagnosticsModels.DeviceErrors, error) {
	sdkErrors, err := p.sdkDevice.GetErrors(ctx)
	if err != nil {
		return diagnosticsModels.DeviceErrors{}, fleeterror.NewInternalErrorf("failed to get device errors: %v", err)
	}
	return mappers.SDKDeviceErrorsToFleetDeviceErrors(sdkErrors), nil
}

// GetMiningPools implements interfaces.Miner
func (p *PluginMiner) GetMiningPools(ctx context.Context) ([]interfaces.MinerConfiguredPool, error) {
	sdkPools, err := p.sdkDevice.GetMiningPools(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get mining pools: %v", err)
	}

	pools := make([]interfaces.MinerConfiguredPool, len(sdkPools))
	for i, pool := range sdkPools {
		pools[i] = interfaces.MinerConfiguredPool{
			Priority: pool.Priority,
			URL:      pool.URL,
			Username: pool.Username,
		}
	}
	return pools, nil
}

// validateAndConvertPoolConfig validates and converts a mining pool config from Fleet format to SDK format.
// It ensures the priority value fits within int32 range before conversion.
func validateAndConvertPoolConfig(pool dto.MiningPool, poolName string) (sdk.MiningPoolConfig, error) {
	if pool.Priority > math.MaxInt32 {
		return sdk.MiningPoolConfig{}, fleeterror.NewInvalidArgumentErrorf(
			"%s pool priority %d exceeds int32 maximum", poolName, pool.Priority)
	}

	return sdk.MiningPoolConfig{
		Priority:   int32(pool.Priority), //nolint:gosec // G115: Priority validated above to fit in int32
		URL:        pool.URL,
		WorkerName: pool.Username,
	}, nil
}

// isNetworkError determines if an error represents a network connectivity failure.
// It uses a layered approach: type-based detection via standard Go error interfaces,
// then syscall errno matching, and finally string matching as a fallback for errors
// that have crossed serialization boundaries (e.g., gRPC status errors).
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
		if err == nil {
			return true
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Some network failures are wrapped in os.SyscallError - unwrap to check the errno
	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		err = syscallErr.Err
	}

	// Check for specific syscall errno values that indicate network failures
	switch {
	case errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.ETIMEDOUT),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, syscall.EHOSTDOWN),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ENOTCONN),
		errors.Is(err, syscall.ESHUTDOWN):
		return true
	}

	// Fallback: string matching for errors that crossed serialization boundaries (e.g., gRPC)
	// Keep this list narrow and high-confidence to minimize false positives
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "no route to host"),
		strings.Contains(msg, "network is unreachable"),
		strings.Contains(msg, "context deadline exceeded"):
		return true
	}

	return false
}
