// Package device implements the Fleet SDK Device interface for individual Proto miners.
//
// The Device represents a single miner instance and is responsible for:
//   - Device status monitoring and reporting
//   - Mining control operations (start/stop)
//   - Configuration management (pools, cooling)
//   - Maintenance operations (reboot, firmware update)
//   - Telemetry data collection
//
// This implementation demonstrates best practices for:
//   - Efficient status polling and caching
//   - Robust error handling and recovery
//   - Secure communication with miners
//   - Comprehensive telemetry collection
package device

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/plugin/proto/internal/device/types"
	"github.com/block/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

var _ sdk.Device = (*Device)(nil)

const (
	defaultStatusTTL          = 30 * time.Second
	maxLogLines               = 10000
	deviceVerificationTimeout = 10 * time.Second
	firmwareRefreshInterval   = 5 * time.Minute

	teraHashToHashConversion                   = 1e12
	joulesPerTeraHashToJoulesPerHashConversion = 1e-12
)

// Device implements the SDK Device interface for a single Proto miner.
//
// Each device instance maintains its own connection and state,
// allowing for concurrent operations across multiple miners.
type Device struct {
	id         string
	deviceInfo sdk.DeviceInfo

	client *proto.Client

	lastStatus   *sdk.DeviceMetrics
	lastStatusAt time.Time
	statusTTL    time.Duration

	lastFirmwareCheckAt time.Time

	mutex sync.Mutex
}

type DeviceOption func(*Device)

func SetStatusTTL(ttl time.Duration) func(*Device) {
	return func(d *Device) {
		d.statusTTL = ttl
	}
}

// New creates a new Proto device instance.
//
// This function demonstrates proper device initialization:
//   - Connection establishment and validation
//   - Authentication setup
//   - Status caching configuration
func New(deviceID string, deviceInfo sdk.DeviceInfo, bearerToken sdk.BearerToken, opts ...DeviceOption) (*Device, error) {
	client, err := proto.NewClient(deviceInfo.Host, deviceInfo.Port, deviceInfo.URLScheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	if err := client.SetCredentials(bearerToken); err != nil {
		return nil, fmt.Errorf("failed to set credentials: %w", err)
	}

	device := &Device{
		id:         deviceID,
		deviceInfo: deviceInfo,
		client:     client,
		statusTTL:  defaultStatusTTL,
		mutex:      sync.Mutex{},
	}

	// If firmware version is already known from pairing, start the refresh
	// throttle from now so we don't immediately re-fetch what we already have.
	if deviceInfo.FirmwareVersion != "" {
		device.lastFirmwareCheckAt = time.Now()
	}

	for _, opt := range opts {
		opt(device)
	}

	ctx, cancel := context.WithTimeout(context.Background(), deviceVerificationTimeout)
	defer cancel()

	if _, err := device.Status(ctx); err != nil {
		// Auth succeeded; only the data gate is blocked. Return the handle so
		// remediation ops (Unpair, UpdateMinerPassword) remain reachable — they
		// route through firmware endpoints exempt from the gate.
		if isDefaultPasswordError(err) {
			slog.Info("Plugin device created with default password active",
				"device_id", deviceID, "host", deviceInfo.Host)
			return device, nil
		}

		client.Close()

		if isAuthenticationError(err) {
			return nil, sdk.NewErrorAuthenticationFailed(deviceID, err)
		}

		return nil, fmt.Errorf("failed to verify device communication: %w", err)
	}

	slog.Debug("Device instance created successfully", "deviceID", deviceID)
	return device, nil
}

// isAuthenticationError checks if the error is an authentication failure from the miner.
// It checks for HTTP 401 status codes in error messages and common auth error strings.
func isAuthenticationError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthenticated") ||
		strings.Contains(msg, "missing api key") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "invalid credentials") ||
		strings.Contains(msg, fmt.Sprintf("status %d", http.StatusUnauthorized))
}

// Proto-firmware-specific — deliberately not in the shared SDK so other
// drivers don't carry this contract.
func isDefaultPasswordError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "default password must be changed") ||
		strings.Contains(msg, "default_password_active")
}

// ID implements the SDK Device interface.
//
// Returns the unique identifier for this device instance.
func (d *Device) ID() string {
	return d.id
}

// DescribeDevice implements the SDK Device interface.
//
// This method returns device information and capabilities.
// It demonstrates how to report device-specific capabilities.
func (d *Device) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	// Device capabilities may differ from driver capabilities
	// For example, some devices might not support certain features
	capabilities := sdk.Capabilities{
		sdk.CapabilityPollingHost:         true, // This device supports status polling
		sdk.CapabilityReboot:              true, // This device supports reboot
		sdk.CapabilityFirmware:            true, // This device supports firmware updates
		sdk.CapabilityPoolConfig:          true, // This device supports pool configuration
		sdk.CapabilityUpdateMinerPassword: true, // This device supports updating web UI password
		// FULL curtailment is implemented as StopMining/StartMining wrappers.
		// Higher levels (efficiency, partial-percent) are reserved for v4.
		sdk.CapabilityCurtail: true,
	}

	// Get firmware version if not already set (requires authentication, so we do it here)
	if d.deviceInfo.FirmwareVersion == "" {
		fwVersion, err := d.client.GetFirmwareVersion(ctx)
		if err != nil {
			slog.Debug("failed to get firmware version during DescribeDevice", "error", err)
		} else if fwVersion != "" {
			d.deviceInfo.FirmwareVersion = fwVersion
		}
	}

	return d.deviceInfo, capabilities, nil
}

// Status implements the SDK Device interface.
//
// This method returns the current status of the miner.
// It demonstrates:
//   - Efficient status caching
//   - Comprehensive telemetry collection
//   - Proper error handling and recovery
//   - Health status determination
func (d *Device) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.lastStatus != nil && time.Since(d.lastStatusAt) < d.statusTTL {
		return *d.lastStatus, nil
	}

	minerStatus, err := d.client.GetStatus(ctx)
	if err != nil {
		return sdk.DeviceMetrics{}, fmt.Errorf("failed to get miner status: %w", err)
	}

	telemetryResp, err := d.client.GetTelemetryValues(ctx)
	if err != nil {
		slog.Warn("Plugin device failed to get telemetry values",
			"device_id", d.id,
			"host", d.deviceInfo.Host,
			"error", err)
	}

	metrics := d.convertStatus(minerStatus, telemetryResp)

	d.refreshFirmwareVersion(ctx, &metrics)

	d.lastStatus = &metrics
	d.lastStatusAt = time.Now()

	return metrics, nil
}

// refreshFirmwareVersion periodically re-fetches firmware version from the device
// to detect firmware updates. Throttled to avoid excessive API calls.
func (d *Device) refreshFirmwareVersion(ctx context.Context, metrics *sdk.DeviceMetrics) {
	if time.Since(d.lastFirmwareCheckAt) < firmwareRefreshInterval {
		return
	}
	d.lastFirmwareCheckAt = time.Now()
	fwVersion, err := d.client.GetFirmwareVersion(ctx)
	if err != nil {
		slog.Debug("failed to get firmware version during Status", "error", err)
		return
	}
	if fwVersion != "" {
		d.deviceInfo.FirmwareVersion = fwVersion
		metrics.FirmwareVersion = fwVersion
	}
}

// GetErrors returns all active and historical errors for the device.
func (d *Device) GetErrors(ctx context.Context) (sdk.DeviceErrors, error) {
	resp, err := d.client.GetErrors(ctx)
	if err != nil {
		return sdk.DeviceErrors{}, fmt.Errorf("failed to fetch errors from device: %w", err)
	}

	return d.convertErrorsResponse(resp), nil
}

// convertStatus converts miner-specific status to SDK format.
//
// This helper method demonstrates:
//   - Status mapping between different formats
//   - Health determination logic
//   - Hierarchical telemetry data integration
func (d *Device) convertStatus(minerStatus *proto.Status, telemetryResp *proto.TelemetryValues) sdk.DeviceMetrics {
	now := time.Now()

	health := minerStatus.State
	var healthReason *string
	if minerStatus.ErrorMessage != "" {
		healthReason = &minerStatus.ErrorMessage
	}

	metrics := sdk.DeviceMetrics{
		DeviceID:        d.id,
		Timestamp:       now,
		FirmwareVersion: d.deviceInfo.FirmwareVersion,
		Health:          health,
		HealthReason:    healthReason,
	}

	if telemetryResp != nil {
		if telemetryResp.Miner != nil {
			miner := telemetryResp.Miner
			metrics.HashrateHS = convertHashrateToHS(miner.HashrateThS)
			metrics.TempC = toMetricValue(miner.TemperatureC)
			metrics.PowerW = toMetricValue(miner.PowerW)
			metrics.EfficiencyJH = convertEfficiencyToJH(miner.EfficiencyJTh)
		}

		if len(telemetryResp.Hashboards) > 0 {
			metrics.HashBoards = d.convertHashboards(telemetryResp.Hashboards, minerStatus.State)
		}

		if len(telemetryResp.PSUs) > 0 {
			metrics.PSUMetrics = d.convertPSUs(telemetryResp.PSUs, minerStatus.State)
		}
	}

	return metrics
}

func (d *Device) convertHashboards(hashboards []*proto.HashboardTelemetry, deviceHealth sdk.HealthStatus) []sdk.HashBoardMetrics {
	result := make([]sdk.HashBoardMetrics, len(hashboards))

	for i, hb := range hashboards {
		componentStatus := deriveComponentStatus(deviceHealth)

		hbMetrics := sdk.HashBoardMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  safeUint32ToInt32(hb.Index),
				Name:   fmt.Sprintf("Hashboard %d", types.HumanReadableIndex(hb.Index)),
				Status: componentStatus,
			},
			HashRateHS:  convertHashrateToHS(hb.HashrateThS),
			TempC:       toMetricValue(hb.AverageTemperatureC),
			InletTempC:  toMetricValue(hb.InletTemperatureC),
			OutletTempC: toMetricValue(hb.OutletTemperatureC),
		}

		if hb.SerialNumber != "" {
			hbMetrics.SerialNumber = &hb.SerialNumber
		}

		if hb.VoltageV != nil {
			hbMetrics.VoltageV = toMetricValue(*hb.VoltageV)
		}
		if hb.CurrentA != nil {
			hbMetrics.CurrentA = toMetricValue(*hb.CurrentA)
		}

		if hb.ASICs != nil {
			hbMetrics.ASICs = d.convertASICs(hb.ASICs, int(safeUint32ToInt32(hb.Index)), componentStatus)
		}

		result[i] = hbMetrics
	}

	return result
}

func (d *Device) convertASICs(asics *proto.ASICTelemetry, hashboardIndex int, hashboardStatus sdk.ComponentStatus) []sdk.ASICMetrics {
	if asics == nil {
		return nil
	}

	numASICs := len(asics.HashrateThS)
	if numASICs == 0 {
		return nil
	}

	result := make([]sdk.ASICMetrics, numASICs)

	for i := range numASICs {
		asicMetrics := sdk.ASICMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  int32(i), // #nosec G115 -- Loop index inherently safe: bounded by slice length (max ~200)
				Name:   fmt.Sprintf("HB%d ASIC %d", types.HumanReadableIndex(hashboardIndex), types.HumanReadableIndex(i)),
				Status: hashboardStatus,
			},
		}

		if i < len(asics.HashrateThS) {
			asicMetrics.HashrateHS = convertHashrateToHS(asics.HashrateThS[i])
		}

		if i < len(asics.TemperatureC) {
			asicMetrics.TempC = toMetricValue(asics.TemperatureC[i])
		}

		result[i] = asicMetrics
	}

	return result
}

func (d *Device) convertPSUs(psus []*proto.PSUTelemetry, deviceHealth sdk.HealthStatus) []sdk.PSUMetrics {
	result := make([]sdk.PSUMetrics, len(psus))

	for i, psu := range psus {
		componentStatus := deriveComponentStatus(deviceHealth)

		psuMetrics := sdk.PSUMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  safeUint32ToInt32(psu.Index),
				Name:   fmt.Sprintf("PSU %d", types.HumanReadableIndex(psu.Index)),
				Status: componentStatus,
			},
			InputVoltageV:  toMetricValue(psu.InputVoltageV),
			OutputVoltageV: toMetricValue(psu.OutputVoltageV),
			InputCurrentA:  toMetricValue(psu.InputCurrentA),
			OutputCurrentA: toMetricValue(psu.OutputCurrentA),
			InputPowerW:    toMetricValue(psu.InputPowerW),
			OutputPowerW:   toMetricValue(psu.OutputPowerW),
			HotSpotTempC:   toMetricValue(psu.HotspotTemperatureC),
		}

		result[i] = psuMetrics
	}

	return result
}

func toMetricValue(value float64) *sdk.MetricValue {
	return &sdk.MetricValue{
		Value: value,
		Kind:  sdk.MetricKindGauge,
	}
}

func convertHashrateToHS(teraHashPerSec float64) *sdk.MetricValue {
	return toMetricValue(teraHashPerSec * teraHashToHashConversion)
}

func convertEfficiencyToJH(joulesPerTeraHash float64) *sdk.MetricValue {
	return toMetricValue(joulesPerTeraHash * joulesPerTeraHashToJoulesPerHashConversion)
}

// safeUint32ToInt32 safely converts uint32 to int32 for hardware indices.
// Returns the value clamped to math.MaxInt32 if overflow would occur.
// Hardware indices (hashboards, ASICs, PSUs) are bounded by physical constraints,
// so this conversion is safe in practice.
func safeUint32ToInt32(value uint32) int32 {
	if value > math.MaxInt32 {
		slog.Warn("Hardware index exceeds int32 max, clamping value",
			"original", value,
			"clamped", math.MaxInt32)
		return math.MaxInt32
	}
	return int32(value)
}

// deriveComponentStatus maps device-level health to component-level status.
// TODO: Move this mapping to fleet side so plugins don't need to handle every SDK
// health status. Fleet should map unknown statuses to sensible defaults.
func deriveComponentStatus(deviceHealth sdk.HealthStatus) sdk.ComponentStatus {
	switch deviceHealth {
	case sdk.HealthHealthyActive, sdk.HealthHealthyInactive, sdk.HealthNeedsMiningPool:
		return sdk.ComponentStatusHealthy
	case sdk.HealthWarning:
		return sdk.ComponentStatusWarning
	case sdk.HealthCritical:
		return sdk.ComponentStatusCritical
	case sdk.HealthUnknown:
		return sdk.ComponentStatusUnknown
	case sdk.HealthStatusUnspecified:
		return sdk.ComponentStatusUnknown
	default:
		return sdk.ComponentStatusUnknown
	}
}

// Close implements the SDK Device interface.
//
// This method cleans up device resources.
// It demonstrates proper resource cleanup and connection management.
func (d *Device) Close(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	slog.Debug("Closing device", "deviceID", d.id)

	if d.client != nil {
		d.client.Close()
	}

	d.lastStatus = nil
	d.lastStatusAt = time.Time{}

	return nil
}

// StartMining implements the SDK Device interface.
//
// This method starts mining operations on the device.
func (d *Device) StartMining(ctx context.Context) error {
	slog.Info("Plugin device starting mining",
		"device_id", d.id,
		"host", d.deviceInfo.Host)

	if err := d.client.StartMining(ctx); err != nil {
		return fmt.Errorf("failed to start mining: %w", err)
	}

	d.lastStatus = nil

	return nil
}

// StopMining implements the SDK Device interface.
//
// This method stops mining operations on the device.
func (d *Device) StopMining(ctx context.Context) error {
	slog.Info("Plugin device stopping mining",
		"device_id", d.id,
		"host", d.deviceInfo.Host)

	if err := d.client.StopMining(ctx); err != nil {
		return fmt.Errorf("failed to stop mining: %w", err)
	}

	d.lastStatus = nil

	return nil
}

// Curtail implements the SDK Device interface. The proto driver supports
// FULL curtailment by wrapping StopMining; higher levels are reserved for v4
// and require vendor-specific underclock logic.
func (d *Device) Curtail(ctx context.Context, level sdk.CurtailLevel) error {
	if level != sdk.CurtailLevelFull {
		return sdk.NewErrCurtailCapabilityNotSupported(d.id, int32(level))
	}
	return d.StopMining(ctx)
}

// Uncurtail implements the SDK Device interface by wrapping StartMining.
func (d *Device) Uncurtail(ctx context.Context) error {
	return d.StartMining(ctx)
}

// SetCoolingMode implements the SDK Device interface.
//
// This method configures the device cooling system.
func (d *Device) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	slog.Info("Setting cooling mode", "deviceID", d.id, "mode", mode)

	if err := d.client.SetCoolingMode(ctx, mode); err != nil {
		return fmt.Errorf("failed to set cooling mode: %w", err)
	}

	return nil
}

// GetCoolingMode implements the SDK Device interface.
//
// This method retrieves the current cooling mode configuration from the device.
func (d *Device) GetCoolingMode(ctx context.Context) (sdk.CoolingMode, error) {
	return d.client.GetCoolingMode(ctx)
}

// SetPowerTarget implements the SDK Device interface.
//
// This method configures the power target based on performance mode.
// Power target values are dynamically retrieved from the miner's hardware capabilities:
//   - MAXIMUM_HASHRATE -> uses the miner's maximum power target (MaxW)
//   - EFFICIENCY -> uses the miner's default power target (DefaultW)
func (d *Device) SetPowerTarget(ctx context.Context, performanceMode sdk.PerformanceMode) error {
	// Retrieve dynamic power target bounds from the miner
	powerTargetInfo, err := d.client.GetPowerTarget(ctx)
	if err != nil {
		return fmt.Errorf("failed to get power target info: %w", err)
	}
	if powerTargetInfo == nil {
		return fmt.Errorf("power target not available yet (device returned 204)")
	}

	var powerTargetW uint32
	switch performanceMode {
	case sdk.PerformanceModeMaximumHashrate:
		powerTargetW = powerTargetInfo.MaxW
	case sdk.PerformanceModeEfficiency:
		powerTargetW = powerTargetInfo.DefaultW
	case sdk.PerformanceModeUnspecified:
		return fmt.Errorf("performance mode must be specified")
	default:
		return fmt.Errorf("unsupported performance mode: %v", performanceMode)
	}

	slog.Info("Setting power target", "deviceID", d.id, "powerTargetW", powerTargetW, "performanceMode", performanceMode,
		"maxW", powerTargetInfo.MaxW, "defaultW", powerTargetInfo.DefaultW)

	if err := d.client.SetPowerTarget(ctx, powerTargetW, performanceMode); err != nil {
		return fmt.Errorf("failed to set power target: %w", err)
	}

	return nil
}

// UpdateMiningPools implements the SDK Device interface.
//
// This method configures mining pool settings.
func (d *Device) UpdateMiningPools(ctx context.Context, pools []sdk.MiningPoolConfig) error {
	slog.Info("Updating mining pools", "deviceID", d.id, "poolCount", len(pools))

	minerPools := make([]proto.Pool, len(pools))
	for i, pool := range pools {
		minerPools[i] = proto.Pool{
			Priority:   int(pool.Priority),
			URL:        pool.URL,
			WorkerName: pool.WorkerName,
		}
	}

	if err := d.client.UpdatePools(ctx, minerPools); err != nil {
		return fmt.Errorf("failed to update mining pools: %w", err)
	}

	return nil
}

// GetMiningPools implements the SDK Device interface.
func (d *Device) GetMiningPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	slog.Debug("Getting mining pools", "deviceID", d.id)
	return d.client.GetPools(ctx)
}

// BlinkLED implements the SDK Device interface.
//
// This method triggers LED identification on the device.
func (d *Device) BlinkLED(ctx context.Context) error {
	slog.Info("Blinking LED", "deviceID", d.id)

	if err := d.client.BlinkLED(ctx); err != nil {
		return fmt.Errorf("failed to blink LED: %w", err)
	}

	return nil
}

// DownloadLogs implements the SDK Device interface.
//
// This method retrieves log data from the device.
func (d *Device) DownloadLogs(ctx context.Context, since *time.Time, _ string) (string, bool, error) {
	slog.Debug("Downloading logs", "deviceID", d.id, "since", since)

	logs, hasMore, err := d.client.GetLogs(ctx, since, maxLogLines)
	if err != nil {
		return "", false, fmt.Errorf("failed to download logs: %w", err)
	}

	return logs, hasMore, nil
}

// Reboot implements the SDK Device interface.
//
// This method reboots the device.
func (d *Device) Reboot(ctx context.Context) error {
	slog.Info("Plugin device rebooting",
		"device_id", d.id,
		"host", d.deviceInfo.Host)

	if err := d.client.Reboot(ctx); err != nil {
		return fmt.Errorf("failed to reboot device: %w", err)
	}

	d.lastStatus = nil

	return nil
}

// FirmwareUpdate implements the SDK Device interface.
//
// The firmware file is uploaded to the miner via the MDK REST API
// (PUT /api/v1/system/update, multipart/form-data).
func (d *Device) FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error {
	slog.Info("Plugin device starting firmware update",
		"device_id", d.id,
		"host", d.deviceInfo.Host,
		"filename", firmware.Filename,
		"size", firmware.Size)

	if firmware.Reader == nil {
		return fmt.Errorf("firmware file is required for file-based firmware update")
	}

	if err := d.client.UploadFirmware(ctx, firmware); err != nil {
		return fmt.Errorf("device firmware upload: %w", err)
	}

	slog.Info("Plugin device firmware upload completed",
		"device_id", d.id,
		"host", d.deviceInfo.Host,
		"filename", firmware.Filename)

	return nil
}

// GetFirmwareUpdateStatus implements sdk.FirmwareUpdateStatusProvider.
//
// Returns the current firmware installation status from the rig's sw_update_status.
func (d *Device) GetFirmwareUpdateStatus(ctx context.Context) (*sdk.FirmwareUpdateStatus, error) {
	status, err := d.client.GetUpdateStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get update status: %w", err)
	}
	if status == nil {
		return nil, nil
	}
	return &sdk.FirmwareUpdateStatus{
		State:    status.Status,
		Progress: status.Progress,
		Error:    status.Error,
	}, nil
}

// Unpair implements the SDK Device interface.
//
// This method clears the authentication key from the device during fleet unpairing.
func (d *Device) Unpair(ctx context.Context) error {
	slog.Info("Plugin device starting unpair",
		"device_id", d.id,
		"host", d.deviceInfo.Host)

	if err := d.client.ClearAuthKey(ctx); err != nil {
		return fmt.Errorf("failed to clear auth key: %w", err)
	}

	// Clear cached status to force fresh data on next query
	d.mutex.Lock()
	d.lastStatus = nil
	d.mutex.Unlock()

	slog.Info("Plugin device unpaired successfully",
		"device_id", d.id)

	return nil
}

// UpdateMinerPassword implements the SDK Device interface.
//
// Updates the web UI password via the /api/v1/auth/change-password REST endpoint.
// Proto uses bearer tokens for API authentication, but the web UI has a separate password.
func (d *Device) UpdateMinerPassword(ctx context.Context, currentPassword string, newPassword string) error {
	slog.Info("Plugin device updating miner password",
		"device_id", d.id,
		"host", d.deviceInfo.Host)

	d.mutex.Lock()
	defer d.mutex.Unlock()

	// Clear cached status since credentials are changing
	d.lastStatus = nil

	// Update password via REST API
	if err := d.client.ChangePassword(ctx, currentPassword, newPassword); err != nil {
		return fmt.Errorf("failed to update miner password: %w", err)
	}

	slog.Info("Plugin device password updated successfully",
		"device_id", d.id)

	return nil
}

func (d *Device) TryBatchStatus(ctx context.Context, _ []string) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

func (d *Device) TrySubscribe(ctx context.Context, _ []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

func (d *Device) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	host := d.deviceInfo.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	url := fmt.Sprintf("%s://%s", d.deviceInfo.URLScheme, host)
	return url, true, nil
}

func (d *Device) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}
