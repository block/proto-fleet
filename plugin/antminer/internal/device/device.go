// Package device implements the Fleet SDK Device interface for individual Antminer devices.
package device

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/internal/types"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

const (
	statusCacheTTL   = 5 * time.Second  // Cache status for 5 seconds
	rpcPort          = 4028             // Default RPC port for Antminers
	webPort          = 80               // Default web API port for Antminers
	newDeviceTimeout = 10 * time.Second // Timeout for new device creation
	blinkLEDDuration = 30 * time.Second // Duration to blink LED for identification

	// Sensor metric constants
	sensorTypeUptime = "uptime"
	unitSeconds      = "seconds"
)

var _ sdk.Device = (*Device)(nil)

// toMetricValue wraps a numeric value in a MetricValue struct with Gauge kind.
func toMetricValue(value float64) *sdk.MetricValue {
	return &sdk.MetricValue{
		Value: value,
		Kind:  sdk.MetricKindGauge,
	}
}

// toMetricValueWithKind wraps a numeric value in a MetricValue struct with the specified kind.
func toMetricValueWithKind(value float64, kind sdk.MetricKind) *sdk.MetricValue {
	return &sdk.MetricValue{
		Value: value,
		Kind:  kind,
	}
}

// setMetricIfNotNil sets a gauge metric value if the source pointer is not nil.
func setMetricIfNotNil(source *float64) *sdk.MetricValue {
	if source != nil {
		return toMetricValue(*source)
	}
	return nil
}

// ptrString returns a pointer to a string value.
func ptrString(s string) *string {
	return &s
}

// Device implements the SDK Device interface for a single Antminer.
type Device struct {
	// Identity and connection information
	id         string
	deviceInfo sdk.DeviceInfo

	// Authentication - store the SDK type for security and type safety
	credentials sdk.UsernamePassword

	// Communication and authentication
	client antminer.AntminerClient

	// Status caching to reduce RPC calls
	lastStatus   *sdk.DeviceMetrics
	lastStatusAt time.Time
	statusMutex  sync.RWMutex
	statusTTL    time.Duration
}

// New creates a new Antminer device instance.
func New(deviceID string, deviceInfo sdk.DeviceInfo, credentials sdk.UsernamePassword, clientFactory types.ClientFactory) (*Device, error) {
	device := &Device{
		id:          deviceID,
		deviceInfo:  deviceInfo,
		credentials: credentials,
		statusTTL:   statusCacheTTL,
	}

	client, err := clientFactory(deviceInfo.Host, rpcPort, webPort, deviceInfo.URLScheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	device.client = client

	if credentials.Username != "" && credentials.Password != "" {
		if err := client.SetCredentials(credentials); err != nil {
			slog.Warn("Failed to set credentials", "deviceID", deviceID, "username", credentials.Username, "error", err)
		}
	}

	slog.Debug("Antminer device instance created successfully", "deviceID", deviceID, "username", credentials.Username)
	return device, nil
}

func (d *Device) Connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, newDeviceTimeout)
	defer cancel()

	if _, err := d.Status(ctx); err != nil {
		d.client.Close()
		return fmt.Errorf("failed to verify device communication: %w", err)
	}
	return nil
}

// ID implements the SDK Device interface.
func (d *Device) ID() string {
	return d.id
}

// DescribeDevice implements the SDK Device interface.
func (d *Device) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	capabilities := sdk.Capabilities{
		sdk.CapabilityPollingHost: true, // This device supports RPC status polling
		// Other capabilities would require web API implementation
		sdk.CapabilityReboot:     false,
		sdk.CapabilityFirmware:   false,
		sdk.CapabilityPoolConfig: false,

		// Optional capabilities currently unused
		"factoryResetSupported": false,
		"coolingModeSupported":  false,
		"logsDownloadSupported": false,
		"poolStatsReported":     true,
		"perChipStatsReported":  true,
		"perBoardStatsReported": true,
		"psuStatsReported":      false,
	}

	return d.deviceInfo, capabilities, nil
}

// Status implements the SDK Device interface.
func (d *Device) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	d.statusMutex.RLock()
	if d.lastStatus != nil && time.Since(d.lastStatusAt) < d.statusTTL {
		defer d.statusMutex.RUnlock()
		slog.Debug("Returning cached status", "deviceID", d.id)
		return *d.lastStatus, nil
	}
	d.statusMutex.RUnlock()

	slog.Debug("Fetching fresh status from Antminer", "deviceID", d.id)

	minerStatus, err := d.client.GetStatus(ctx)
	if err != nil {
		return sdk.DeviceMetrics{}, fmt.Errorf("failed to get miner status: %w", err)
	}

	telemetry, err := d.client.GetTelemetry(ctx)
	if err != nil {
		slog.Warn("Failed to get telemetry data", "deviceID", d.id, "error", err)
	}

	status := d.convertStatus(minerStatus, telemetry)

	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()
	d.lastStatus = &status
	d.lastStatusAt = time.Now()

	return status, nil
}

// GetErrors returns all active and historical errors for the device.
// Since CGMiner RPC provides point-in-time metrics (not historical errors),
// errors are detected heuristically from current metric values.
func (d *Device) GetErrors(ctx context.Context) (sdk.DeviceErrors, error) {
	// Fetch data from CGMiner RPC in parallel - collect all available data even if some calls fail
	var summaryResp *rpc.SummaryResponse
	var devsResp *rpc.DevsResponse
	var poolsResp *rpc.PoolsResponse

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		var err error
		if summaryResp, err = d.client.GetSummary(ctx); err != nil {
			slog.Warn("Failed to get summary for error detection", "deviceID", d.id, "error", err)
		}
	}()

	go func() {
		defer wg.Done()
		var err error
		if devsResp, err = d.client.GetDevs(ctx); err != nil {
			slog.Warn("Failed to get devs for error detection", "deviceID", d.id, "error", err)
		}
	}()

	go func() {
		defer wg.Done()
		var err error
		if poolsResp, err = d.client.GetPools(ctx); err != nil {
			slog.Warn("Failed to get pools for error detection", "deviceID", d.id, "error", err)
		}
	}()

	wg.Wait()

	// Detect errors from the collected data
	errors := detectErrors(summaryResp, devsResp, poolsResp, d.id)

	return sdk.DeviceErrors{
		DeviceID: d.id,
		Errors:   errors,
	}, nil
}

// convertStatus converts Antminer-specific status to SDK format.
func (d *Device) convertStatus(minerStatus *antminer.Status, telemetry *antminer.Telemetry) sdk.DeviceMetrics {
	now := time.Now()

	// Determine health status based on miner state and performance
	health := minerStatus.State
	var healthReason *string

	// Refine health status based on telemetry
	// Health status hierarchy: Critical > Warning > Active/Inactive > Unknown
	// We may upgrade healthy states to warning/critical based on telemetry
	switch minerStatus.State {
	case sdk.HealthHealthyActive:
		// Detect if active miner has no hash rate, which may indicate an issue
		if telemetry != nil && telemetry.HashrateHS != nil && *telemetry.HashrateHS == 0 {
			health = sdk.HealthWarning
			healthReason = ptrString("Mining but no hashrate detected")
		}
	case sdk.HealthHealthyInactive:
		// Idle state is normal
	case sdk.HealthWarning, sdk.HealthCritical:
		// Use error message as health reason
		if minerStatus.ErrorMessage != "" {
			healthReason = &minerStatus.ErrorMessage
		}
	case sdk.HealthUnknown:
		healthReason = ptrString("Status unknown")
	case sdk.HealthStatusUnspecified:
		healthReason = ptrString("Status unspecified")
	}

	metrics := sdk.DeviceMetrics{
		DeviceID:     d.id,
		Timestamp:    now,
		Health:       health,
		HealthReason: healthReason,
	}

	// Add telemetry data if available
	if telemetry != nil {
		metrics.HashrateHS = setMetricIfNotNil(telemetry.HashrateHS)
		metrics.TempC = setMetricIfNotNil(telemetry.TemperatureCelsius)
		metrics.PowerW = setMetricIfNotNil(telemetry.PowerWatts)
		metrics.EfficiencyJH = setMetricIfNotNil(telemetry.EfficiencyJPerHash)
		metrics.FanRPM = setMetricIfNotNil(telemetry.FanRPM)

		// Add uptime as a sensor metric if available
		// Uptime is a counter (monotonically increasing) rather than a gauge
		if telemetry.UptimeSeconds != nil {
			metrics.SensorMetrics = []sdk.SensorMetrics{
				{
					ComponentInfo: sdk.ComponentInfo{
						Name:   sensorTypeUptime,
						Status: sdk.ComponentStatusHealthy,
					},
					Type:  sensorTypeUptime,
					Unit:  unitSeconds,
					Value: toMetricValueWithKind(float64(*telemetry.UptimeSeconds), sdk.MetricKindCounter),
				},
			}
		}
	}

	return metrics
}

// Close implements the SDK Device interface.
func (d *Device) Close(ctx context.Context) error {
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()
	slog.Debug("Closing Antminer device", "deviceID", d.id)

	if d.client != nil {
		d.client.Close()
	}

	d.lastStatus = nil

	return nil
}

// The following methods are not implemented for Antminers due to RPC API limitations
// They would require web API implementation for full functionality

// StartMining implements the SDK Device interface.
func (d *Device) StartMining(ctx context.Context) error {
	return d.client.StartMining(ctx)
}

// StopMining implements the SDK Device interface.
func (d *Device) StopMining(ctx context.Context) error {
	return d.client.StopMining(ctx)
}

// SetCoolingMode implements the SDK Device interface.
func (d *Device) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	return d.client.SetCoolingMode(ctx, web.CoolingMode(mode))
}

// SetPowerTarget implements the SDK Device interface.
// Maps performance modes to Antminer work modes:
//   - MAXIMUM_HASHRATE -> bitmain-work-mode = "0" (normal operation)
//   - EFFICIENCY -> bitmain-work-mode = "2" (low power mode)
func (d *Device) SetPowerTarget(ctx context.Context, performanceMode sdk.PerformanceMode) error {
	slog.Info("Setting power target via work mode", "deviceID", d.id, "performanceMode", performanceMode)

	// Map performance mode to work mode
	var workMode web.BitmainWorkMode
	switch performanceMode {
	case sdk.PerformanceModeMaximumHashrate:
		workMode = web.BitmainWorkModeStart // "0" - Normal operation
	case sdk.PerformanceModeEfficiency:
		workMode = web.BitmainWorkModeLowPower // "2" - Low power mode
	case sdk.PerformanceModeUnspecified:
		return fmt.Errorf("performance mode must be specified for Antminer devices")
	default:
		return fmt.Errorf("unsupported performance mode: %v", performanceMode)
	}

	// Get current configuration
	config, err := d.client.GetMinerConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current miner config: %w", err)
	}

	// Update work mode
	config.BitmainWorkMode = workMode

	// Apply configuration
	if err := d.client.SetMinerConfig(ctx, config); err != nil {
		return fmt.Errorf("failed to set work mode: %w", err)
	}

	// Clear cached status to force refresh on next Status() call
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()
	d.lastStatus = nil

	slog.Info("Successfully set work mode", "deviceID", d.id, "workMode", workMode)
	return nil
}

// UpdateMiningPools implements the SDK Device interface.
func (d *Device) UpdateMiningPools(ctx context.Context, pools []sdk.MiningPoolConfig) error {
	var antminerPools []antminer.Pool
	for _, p := range pools {
		antminerPools = append(antminerPools, antminer.Pool{
			Priority:   int(p.Priority),
			URL:        p.URL,
			WorkerName: p.WorkerName,
		})
	}
	return d.client.UpdatePools(ctx, antminerPools)
}

// BlinkLED implements the SDK Device interface.
func (d *Device) BlinkLED(ctx context.Context) error {
	return d.client.BlinkLED(ctx, blinkLEDDuration)
}

// DownloadLogs implements the SDK Device interface.
func (d *Device) DownloadLogs(ctx context.Context, _ *time.Time, _ string) (string, bool, error) {
	return "", false, fmt.Errorf("log download not supported via RPC API - requires web API implementation")
}

// Reboot implements the SDK Device interface.
func (d *Device) Reboot(ctx context.Context) error {
	return d.client.Reboot(ctx)
}

// FirmwareUpdate implements the SDK Device interface.
func (d *Device) FirmwareUpdate(ctx context.Context) error {
	return fmt.Errorf("firmware update not supported via RPC API - requires web API implementation")
}

func (d *Device) Unpair(ctx context.Context) error {
	// No specific unpair action needed for Antminer devices
	// Unpair is handled optimistically at the database level
	return nil
}

// Optional capabilities - these return false to indicate they're not supported

func (d *Device) TryBatchStatus(ctx context.Context, _ []string) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil // Not supported by individual devices
}

func (d *Device) TrySubscribe(ctx context.Context, _ []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil // Streaming not supported
}

func (d *Device) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	// We can provide a web view URL for the Antminer web interface
	url := fmt.Sprintf("%s://%s", d.deviceInfo.URLScheme, d.deviceInfo.Host)
	return url, true, nil
}

func (d *Device) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil // Time series not supported
}
