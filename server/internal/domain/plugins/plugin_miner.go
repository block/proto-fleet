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

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	diagnosticsModels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
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
	SaveLogs(batchLogUUID string, macAddress string, logLines []string) (string, error)
}

type PluginMiner struct {
	orgID          int64
	deviceID       models.DeviceIdentifier
	driverName     string
	caps           sdk.Capabilities
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
	driverName string,
	caps sdk.Capabilities,
	serialNumber string,
	connectionInfo networking.ConnectionInfo,
	sdkDevice sdk.Device,
	deviceInfo sdk.DeviceInfo,
	filesService logSaver,
) *PluginMiner {
	return &PluginMiner{
		orgID:          orgID,
		deviceID:       deviceID,
		driverName:     driverName,
		caps:           caps,
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

// GetDriverName implements interfaces.MinerInfo
func (p *PluginMiner) GetDriverName() string {
	return p.driverName
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
		if isDefaultPasswordActiveError(err) {
			return modelsV2.DeviceMetrics{}, fleeterror.NewForbiddenErrorf(
				"device %s default password must be changed during metrics fetch: %v",
				p.deviceID,
				err,
			)
		}
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
		if isDefaultPasswordActiveError(err) {
			return models.MinerStatusUnknown, fleeterror.NewForbiddenErrorf(
				"device %s default password must be changed during status check: %v",
				p.deviceID,
				err,
			)
		}
		if isAuthError(err) {
			return models.MinerStatusUnknown, fleeterror.NewUnauthenticatedErrorf("device %s authentication failed during status check: %v", p.deviceID, err)
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
		return wrapPluginError(err, "failed to reboot device")
	}
	return nil
}

// StartMining implements interfaces.Miner
func (p *PluginMiner) StartMining(ctx context.Context) error {
	if err := p.sdkDevice.StartMining(ctx); err != nil {
		return wrapPluginError(err, "failed to start mining")
	}
	return nil
}

// StopMining implements interfaces.Miner
func (p *PluginMiner) StopMining(ctx context.Context) error {
	if err := p.sdkDevice.StopMining(ctx); err != nil {
		return wrapPluginError(err, "failed to stop mining")
	}
	return nil
}

// Curtail dispatches through optional SDK curtailment support.
func (p *PluginMiner) Curtail(ctx context.Context, req sdk.CurtailRequest) error {
	if !supportsCurtailLevel(p.caps, req.Level) {
		return fleeterror.NewUnimplementedError("device does not support curtailment")
	}
	curtailer, ok := p.sdkDevice.(sdk.DeviceCurtailment)
	if !ok {
		return fleeterror.NewUnimplementedError("device does not support curtailment")
	}
	if err := curtailer.Curtail(ctx, req); err != nil {
		return wrapCurtailmentPluginError(err, "failed to curtail device")
	}
	return nil
}

// Uncurtail dispatches through optional SDK curtailment support.
func (p *PluginMiner) Uncurtail(ctx context.Context, req sdk.UncurtailRequest) error {
	if !supportsAnyCurtailLevel(p.caps) {
		return fleeterror.NewUnimplementedError("device does not support curtailment")
	}
	curtailer, ok := p.sdkDevice.(sdk.DeviceCurtailment)
	if !ok {
		return fleeterror.NewUnimplementedError("device does not support curtailment")
	}
	if err := curtailer.Uncurtail(ctx, req); err != nil {
		return wrapCurtailmentPluginError(err, "failed to uncurtail device")
	}
	return nil
}

func supportsCurtailLevel(caps sdk.Capabilities, level sdk.CurtailLevel) bool {
	switch level {
	case sdk.CurtailLevelFull:
		return caps[sdk.CapabilityCurtailFull]
	case sdk.CurtailLevelEfficiency:
		return caps[sdk.CapabilityCurtailEfficiency]
	case sdk.CurtailLevelPartialPercent:
		return caps[sdk.CapabilityCurtailPartial]
	case sdk.CurtailLevelUnspecified:
		return false
	default:
		return false
	}
}

func supportsAnyCurtailLevel(caps sdk.Capabilities) bool {
	return caps[sdk.CapabilityCurtailFull] ||
		caps[sdk.CapabilityCurtailEfficiency] ||
		caps[sdk.CapabilityCurtailPartial]
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
		return wrapPluginError(err, "failed to set cooling mode")
	}
	return nil
}

// GetCoolingMode implements interfaces.Miner
func (p *PluginMiner) GetCoolingMode(ctx context.Context) (commonpb.CoolingMode, error) {
	sdkMode, err := p.sdkDevice.GetCoolingMode(ctx)
	if err != nil {
		return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, wrapPluginError(err, "failed to get cooling mode")
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
		return wrapPluginError(err, "failed to set power target")
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
		return wrapPluginError(err, "failed to update mining pools")
	}
	return nil
}

// BlinkLED implements interfaces.Miner
func (p *PluginMiner) BlinkLED(ctx context.Context) error {
	if err := p.sdkDevice.BlinkLED(ctx); err != nil {
		return wrapPluginError(err, "failed to blink LED")
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

	csvRows := formatLogsToCSV(logLines, p.caps[sdk.CapabilityAsymmetricAuth])
	if _, err := p.filesService.SaveLogs(batchLogUUID, p.deviceInfo.MacAddress, csvRows); err != nil {
		return fleeterror.NewInternalErrorf("failed to save logs: %v", err)
	}
	return nil
}

const csvLogHeaderWithType = "Time,Type,Message"
const csvLogHeaderNoType = "Time,Message"

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

// formatLogsToCSV converts raw log lines into CSV rows.
// When includeType is true, the header is "Time,Type,Message" (used for Proto miners that emit log levels).
// When false, the header is "Time,Message" (used for Antminer logs that have no log level field).
func formatLogsToCSV(logLines []string, includeType bool) []string {
	header := csvLogHeaderWithType
	if !includeType {
		header = csvLogHeaderNoType
	}
	rows := make([]string, 0, len(logLines)+1)
	rows = append(rows, header)
	for _, line := range logLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, formatLogLineToCSVRow(line, includeType))
	}
	return rows
}

// formatLogLineToCSVRow parses a single log line into a CSV row.
// Handles three formats:
//   - Proto miner syslog with mcdd timestamp: "{syslog_prefix}: {mcdd_timestamp} | LEVEL | {message}"
//   - Proto miner syslog without mcdd timestamp (BX firmware): "{syslog_prefix}: | LEVEL | {message}"
//   - Proto miner bare timestamp: "{mcdd_timestamp} | LEVEL | {message}"
//   - Antminer calendar timestamp: "[2026-01-01T00:00:00Z] message"
//   - Antminer application log: "YYYY-MM-DD HH:MM:SS message"
//   - Antminer kernel boot log: "[seconds_since_boot] message" — no wall-clock time, falls through
//
// Matches the parsing logic in the ProtoOS frontend utility.ts formatLog function.
func formatLogLineToCSVRow(line string, includeType bool) string {
	csvRow := func(ts, logType, message string) string {
		esc := func(s string) string { return strings.ReplaceAll(s, `"`, `""`) }
		if includeType {
			return fmt.Sprintf(`"%s","%s","%s"`, esc(ts), esc(logType), esc(message))
		}
		return fmt.Sprintf(`"%s","%s"`, esc(ts), esc(message))
	}

	for _, level := range logLevelSeparators {
		idx := strings.Index(line, level.separator)
		if idx < 0 {
			continue
		}
		prefix := line[:idx]
		message := line[idx+len(level.separator):]

		// Extract timestamp from the prefix.
		// The level separator ` | LEVEL | ` includes a leading space, so the prefix ends just
		// before that space (e.g. "...mcdd[664]:" with no trailing space).
		//
		// Three prefix shapes:
		//   1. Syslog + mcdd timestamp: "Jun 14 16:01:58 miner mcdd[716]: 2024-06-14 16:01:58.470952"
		//      → SplitN on ": " → parts[1] = "2024-06-14 16:01:58.470952"
		//   2. Syslog only (BX firmware): "Feb 23 12:33:24 proto-miner-D202 mcdd[664]:"
		//      → no ": " match → extract first 3 space-separated fields as syslog date/time
		//   3. Bare mcdd timestamp: "2024-06-14 16:01:58.470952"
		//      → no ": " match, < 3 fields → use full prefix
		ts := prefix
		if parts := strings.SplitN(prefix, ": ", 2); len(parts) == 2 {
			ts = parts[1]
		} else if fields := strings.Fields(prefix); len(fields) >= 3 {
			ts = fields[0] + " " + fields[1] + " " + fields[2]
		}
		ts = strings.TrimSpace(ts)
		if dotIdx := strings.Index(ts, "."); dotIdx >= 0 {
			ts = ts[:dotIdx]
		}

		return csvRow(ts, level.label, message)
	}

	// Try [timestamp] message format used by Antminer kernel logs.
	// Only treat bracketed content as a real calendar timestamp when it contains date
	// separators ('T', '-', '/'). Bare numbers like "[258.894452@1]" are seconds-since-boot
	// counters with no wall-clock date, so they fall through to the raw message catch-all.
	if strings.HasPrefix(line, "[") {
		if closeBracket := strings.Index(line, "]"); closeBracket > 0 {
			potentialTS := strings.TrimSpace(line[1:closeBracket])
			if strings.ContainsAny(potentialTS, "0123456789") && strings.ContainsAny(potentialTS, "T-/") {
				message := strings.TrimPrefix(line[closeBracket+1:], " ")
				return csvRow(potentialTS, "", message)
			}
		}
	}

	// Try "YYYY-MM-DD HH:MM:SS message" format used by Antminer application logs.
	if len(line) > 19 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':' {
		timestamp := line[:19]
		message := strings.TrimPrefix(line[19:], " ")
		return csvRow(timestamp, "", message)
	}

	return csvRow("", "", line)
}

// FirmwareUpdate implements interfaces.Miner
func (p *PluginMiner) FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error {
	if err := p.sdkDevice.FirmwareUpdate(ctx, firmware); err != nil {
		return wrapPluginError(err, "failed to update firmware")
	}
	return nil
}

// GetFirmwareUpdateStatus implements the optional FirmwareUpdateStatusProvider interface.
// Returns the firmware installation status from the device if the plugin supports it.
func (p *PluginMiner) GetFirmwareUpdateStatus(ctx context.Context) (*sdk.FirmwareUpdateStatus, error) {
	if provider, ok := p.sdkDevice.(sdk.FirmwareUpdateStatusProvider); ok {
		status, err := provider.GetFirmwareUpdateStatus(ctx)
		if err != nil {
			return nil, wrapPluginError(err, "failed to get firmware update status")
		}
		return status, nil
	}
	return nil, nil
}

// Unpair implements interfaces.Miner
func (p *PluginMiner) Unpair(ctx context.Context) error {
	if err := p.sdkDevice.Unpair(ctx); err != nil {
		return wrapPluginError(err, "failed to unpair device")
	}
	return nil
}

// UpdateMinerPassword implements interfaces.Miner
func (p *PluginMiner) UpdateMinerPassword(ctx context.Context, payload dto.UpdateMinerPasswordPayload) error {
	if err := p.sdkDevice.UpdateMinerPassword(ctx, payload.CurrentPassword, payload.NewPassword); err != nil {
		return wrapPluginError(err, "failed to update miner password")
	}
	return nil
}

// GetErrors implements interfaces.Miner
func (p *PluginMiner) GetErrors(ctx context.Context) (diagnosticsModels.DeviceErrors, error) {
	sdkErrors, err := p.sdkDevice.GetErrors(ctx)
	if err != nil {
		return diagnosticsModels.DeviceErrors{}, wrapPluginError(err, "failed to get device errors")
	}
	return mappers.SDKDeviceErrorsToFleetDeviceErrors(sdkErrors), nil
}

// GetMiningPools implements interfaces.Miner
func (p *PluginMiner) GetMiningPools(ctx context.Context) ([]interfaces.MinerConfiguredPool, error) {
	sdkPools, err := p.sdkDevice.GetMiningPools(ctx)
	if err != nil {
		return nil, wrapPluginError(err, "failed to get mining pools")
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

// isAuthError determines if an error represents an authentication failure.
// Plugin calls cross the go-plugin gRPC boundary, where auth failures arrive as
// codes.Unauthenticated status errors rather than sdk.SDKError values.
// Both shapes are recognised so neither transport path is missed.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := grpcstatus.FromError(err); ok && st.Code() == codes.Unauthenticated {
		return true
	}
	var sdkErr sdk.SDKError
	return errors.As(err, &sdkErr) && sdkErr.Code == sdk.ErrCodeAuthenticationFailed
}

// wrapPluginError converts an SDK/plugin error into the appropriate fleet error type.
// It preserves gRPC Unimplemented, Unauthenticated, FailedPrecondition, and
// PermissionDenied statuses so the command system can skip retries for permanent
// failures, cache eviction can fire on auth errors, device-rejected operations
// (e.g. 413) fail immediately, and default-password lockouts surface as forbidden.
func wrapPluginError(err error, format string, a ...any) error {
	if err == nil {
		return nil
	}
	msg := fmt.Sprintf(format, a...)
	if st, ok := grpcstatus.FromError(err); ok {
		switch st.Code() {
		case codes.Unimplemented:
			return fleeterror.NewUnimplementedErrorf("%s: %s", msg, st.Message())
		case codes.Unauthenticated:
			return fleeterror.NewUnauthenticatedErrorf("%s: %s", msg, st.Message())
		case codes.FailedPrecondition:
			return fleeterror.NewFailedPreconditionErrorf("%s: %s", msg, st.Message())
		case codes.PermissionDenied:
			if isDefaultPasswordActiveError(err) {
				return fleeterror.NewForbiddenErrorf("%s: %s", msg, st.Message())
			}
			return fleeterror.NewForbiddenErrorf("%s: permission denied: %s", msg, st.Message())
		case codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument,
			codes.DeadlineExceeded, codes.NotFound, codes.AlreadyExists,
			codes.ResourceExhausted,
			codes.Aborted, codes.OutOfRange, codes.Internal, codes.Unavailable,
			codes.DataLoss:
			// All other gRPC status codes are treated as internal errors below.
		}
	}
	if isDefaultPasswordActiveError(err) {
		if st, ok := grpcstatus.FromError(err); ok {
			return fleeterror.NewForbiddenErrorf("%s: %s", msg, st.Message())
		}
		return fleeterror.NewForbiddenErrorf("%s: %v", msg, err)
	}
	return fleeterror.NewInternalErrorf("%s: %v", msg, err)
}

// wrapCurtailmentPluginError preserves Unavailable for retryable dispatches.
// wrapPluginError maps Unavailable to Internal for legacy control RPCs.
func wrapCurtailmentPluginError(err error, format string, a ...any) error {
	if err == nil {
		return nil
	}
	msg := fmt.Sprintf(format, a...)
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		switch sdkErr.Code {
		case sdk.ErrCodeCurtailCapabilityNotSupported, sdk.ErrCodeUnsupportedCapability:
			return fleeterror.NewUnimplementedErrorf("%s: %s", msg, sdkErr.Message)
		case sdk.ErrCodeCurtailTransient, sdk.ErrCodeDeviceUnavailable:
			return fleeterror.NewUnavailableErrorf("%s: %s", msg, sdkErr.Message)
		case sdk.ErrCodeDeviceNotFound, sdk.ErrCodeInvalidConfig,
			sdk.ErrCodeDriverShutdown, sdk.ErrCodeAuthenticationFailed:
			// Preserve legacy non-curtailment classification for other SDK errors.
		}
	}
	if st, ok := grpcstatus.FromError(err); ok && st.Code() == codes.Unavailable {
		return fleeterror.NewUnavailableErrorf("%s: %s", msg, st.Message())
	}
	return wrapPluginError(err, format, a...)
}

// isDefaultPasswordActiveError detects the Proto firmware default-password
// lockout. Substrings match what Proto firmware emits today; the shared SDK
// deliberately doesn't encode firmware-specific text so other drivers can add
// their own gates without carrying Proto's contract.
func isDefaultPasswordActiveError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "default password must be changed") ||
		strings.Contains(msg, "default_password_active")
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

	// Check for gRPC status codes that indicate device unreachability.
	// When errors cross the plugin gRPC boundary, Go net/syscall type info is lost;
	// the SDK maps device-unreachable conditions to specific gRPC codes.
	if st, ok := grpcstatus.FromError(err); ok {
		c := st.Code()
		if c == codes.Unavailable || c == codes.NotFound || c == codes.DeadlineExceeded {
			return true
		}
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
