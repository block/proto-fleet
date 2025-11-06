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
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

var _ sdk.Device = (*Device)(nil) // Ensure Device implements sdk.Device

const (
	defaultStatusTTL          = 30 * time.Second // Default time-to-live for cached status
	maxLogLines               = 10000            // Maximum number of log lines to retrieve
	deviceVerificationTimeout = 10 * time.Second // Timeout for device verification during initialization

	// Unit conversion factors for telemetry
	teraHashToHashConversion                   = 1e12  // Converts TH/s to H/s
	joulesPerTeraHashToJoulesPerHashConversion = 1e-12 // Converts J/TH to J/H
)

// Device implements the SDK Device interface for a single Proto miner.
//
// Each device instance maintains its own connection and state,
// allowing for concurrent operations across multiple miners.
type Device struct {
	// Identity and connection information
	id         string
	deviceInfo sdk.DeviceInfo

	// Communication and authentication
	client *proto.Client

	// Status caching to reduce API calls
	lastStatus   *sdk.DeviceMetrics
	lastStatusAt time.Time
	statusTTL    time.Duration

	// Mutex for synchronizing access to cached status
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
	// Create client for communication with the miner
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

	for _, opt := range opts {
		opt(device)
	}

	// Verify we can communicate with the device
	ctx, cancel := context.WithTimeout(context.Background(), deviceVerificationTimeout)
	defer cancel()

	if _, err := device.Status(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to verify device communication: %w", err)
	}

	slog.Debug("Device instance created successfully", "deviceID", deviceID)
	return device, nil
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
	// Device capabilities may differ from driver capabilities
	// For example, some devices might not support certain features
	capabilities := sdk.Capabilities{
		sdk.CapabilityPollingHost: true, // This device supports status polling
		sdk.CapabilityReboot:      true, // This device supports reboot
		sdk.CapabilityFirmware:    true, // This device supports firmware updates
		sdk.CapabilityPoolConfig:  true, // This device supports pool configuration
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
	// Check if we have a cached status that's still valid
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.lastStatus != nil && time.Since(d.lastStatusAt) < d.statusTTL {
		slog.Debug("Returning cached status", "deviceID", d.id)
		return *d.lastStatus, nil
	}

	slog.Debug("Fetching fresh status", "deviceID", d.id)

	// Get current status from the miner
	minerStatus, err := d.client.GetStatus(ctx)
	if err != nil {
		return sdk.DeviceMetrics{}, fmt.Errorf("failed to get miner status: %w", err)
	}

	// Get telemetry data
	telemetryResp, err := d.client.GetTelemetryValues(ctx)
	if err != nil {
		slog.Warn("Failed to get telemetry values", "deviceID", d.id, "error", err)
		// Continue without telemetry - status is more important
	}

	// Convert miner status to SDK format
	metrics := d.convertStatus(minerStatus, telemetryResp)

	// Cache the status
	d.lastStatus = &metrics
	d.lastStatusAt = time.Now()

	return metrics, nil
}

// convertStatus converts miner-specific status to SDK format.
//
// This helper method demonstrates:
//   - Status mapping between different formats
//   - Health determination logic
//   - Hierarchical telemetry data integration
func (d *Device) convertStatus(minerStatus *proto.Status, telemetryResp *proto.TelemetryValues) sdk.DeviceMetrics {
	now := time.Now()

	// Determine health status from miner state
	health := minerStatus.State
	var healthReason *string
	if minerStatus.ErrorMessage != "" {
		healthReason = &minerStatus.ErrorMessage
	}

	metrics := sdk.DeviceMetrics{
		DeviceID:     d.id,
		Timestamp:    now,
		Health:       health,
		HealthReason: healthReason,
	}

	// Add telemetry data if available
	if telemetryResp != nil {
		// Device-level aggregates from miner telemetry
		if telemetryResp.Miner != nil {
			miner := telemetryResp.Miner
			metrics.HashrateHS = convertHashrateToHS(miner.HashrateThS)
			metrics.TempC = toMetricValue(miner.TemperatureC)
			metrics.PowerW = toMetricValue(miner.PowerW)
			metrics.EfficiencyJH = convertEfficiencyToJH(miner.EfficiencyJTh)
		}

		// Hashboards (with nested ASICs)
		if len(telemetryResp.Hashboards) > 0 {
			metrics.HashBoards = d.convertHashboards(telemetryResp.Hashboards, minerStatus.State)
		}

		// PSUs
		if len(telemetryResp.PSUs) > 0 {
			metrics.PSUMetrics = d.convertPSUs(telemetryResp.PSUs, minerStatus.State)
		}
	}

	return metrics
}

// convertHashboards converts hashboard telemetry to SDK format
func (d *Device) convertHashboards(hashboards []*proto.HashboardTelemetry, deviceHealth sdk.HealthStatus) []sdk.HashBoardMetrics {
	result := make([]sdk.HashBoardMetrics, len(hashboards))

	for i, hb := range hashboards {
		// Determine component status from device health
		componentStatus := deriveComponentStatus(deviceHealth)

		hbMetrics := sdk.HashBoardMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  safeUint32ToInt32(hb.Index),
				Name:   fmt.Sprintf("Hashboard %d", hb.Index),
				Status: componentStatus,
			},
			HashRateHS:  convertHashrateToHS(hb.HashrateThS),
			TempC:       toMetricValue(hb.AverageTemperatureC),
			InletTempC:  toMetricValue(hb.InletTemperatureC),
			OutletTempC: toMetricValue(hb.OutletTemperatureC),
		}

		// Serial number
		if hb.SerialNumber != "" {
			hbMetrics.SerialNumber = &hb.SerialNumber
		}

		// Optional voltage and current
		if hb.VoltageV != nil {
			hbMetrics.VoltageV = toMetricValue(*hb.VoltageV)
		}
		if hb.CurrentA != nil {
			hbMetrics.CurrentA = toMetricValue(*hb.CurrentA)
		}

		// Convert nested ASICs if present
		if hb.ASICs != nil {
			hbMetrics.ASICs = d.convertASICs(hb.ASICs, int(safeUint32ToInt32(hb.Index)), componentStatus)
		}

		result[i] = hbMetrics
	}

	return result
}

// convertASICs converts ASIC telemetry to SDK format
func (d *Device) convertASICs(asics *proto.ASICTelemetry, hashboardIndex int, hashboardStatus sdk.ComponentStatus) []sdk.ASICMetrics {
	if asics == nil {
		return nil
	}

	// Determine array length from hashrate array
	numASICs := len(asics.HashrateThS)
	if numASICs == 0 {
		return nil
	}

	result := make([]sdk.ASICMetrics, numASICs)

	for i := range numASICs {
		asicMetrics := sdk.ASICMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  int32(i), // #nosec G115 -- Loop index inherently safe: bounded by slice length (max ~200)
				Name:   fmt.Sprintf("HB%d ASIC %d", hashboardIndex, i),
				Status: hashboardStatus, // Inherit status from parent hashboard
			},
		}

		// Hashrate (TH/s to H/s)
		if i < len(asics.HashrateThS) {
			asicMetrics.HashrateHS = convertHashrateToHS(asics.HashrateThS[i])
		}

		// Temperature
		if i < len(asics.TemperatureC) {
			asicMetrics.TempC = toMetricValue(asics.TemperatureC[i])
		}

		result[i] = asicMetrics
	}

	return result
}

// convertPSUs converts PSU telemetry to SDK format
func (d *Device) convertPSUs(psus []*proto.PSUTelemetry, deviceHealth sdk.HealthStatus) []sdk.PSUMetrics {
	result := make([]sdk.PSUMetrics, len(psus))

	for i, psu := range psus {
		// Determine component status from device health
		componentStatus := deriveComponentStatus(deviceHealth)

		psuMetrics := sdk.PSUMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  safeUint32ToInt32(psu.Index),
				Name:   fmt.Sprintf("PSU %d", psu.Index),
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

// toMetricValue converts a float64 to a MetricValue pointer
func toMetricValue(value float64) *sdk.MetricValue {
	return &sdk.MetricValue{
		Value: value,
		Kind:  sdk.MetricKindGauge,
	}
}

// convertHashrateToHS converts terahash/s to hash/s with MetricValue wrapper
func convertHashrateToHS(teraHashPerSec float64) *sdk.MetricValue {
	return toMetricValue(teraHashPerSec * teraHashToHashConversion)
}

// convertEfficiencyToJH converts J/TH to J/H with MetricValue wrapper
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

// deriveComponentStatus derives component status from device health status
func deriveComponentStatus(deviceHealth sdk.HealthStatus) sdk.ComponentStatus {
	switch deviceHealth {
	case sdk.HealthHealthyActive, sdk.HealthHealthyInactive:
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

	// Clear cached data
	d.lastStatus = nil
	d.lastStatusAt = time.Time{}

	return nil
}

// StartMining implements the SDK Device interface.
//
// This method starts mining operations on the device.
func (d *Device) StartMining(ctx context.Context) error {
	slog.Info("Starting mining", "deviceID", d.id)

	if err := d.client.StartMining(ctx); err != nil {
		return fmt.Errorf("failed to start mining: %w", err)
	}

	// Invalidate cached status
	d.lastStatus = nil

	return nil
}

// StopMining implements the SDK Device interface.
//
// This method stops mining operations on the device.
func (d *Device) StopMining(ctx context.Context) error {
	slog.Info("Stopping mining", "deviceID", d.id)

	if err := d.client.StopMining(ctx); err != nil {
		return fmt.Errorf("failed to stop mining: %w", err)
	}

	// Invalidate cached status
	d.lastStatus = nil

	return nil
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

// UpdateMiningPools implements the SDK Device interface.
//
// This method configures mining pool settings.
func (d *Device) UpdateMiningPools(ctx context.Context, pools []sdk.MiningPoolConfig) error {
	slog.Info("Updating mining pools", "deviceID", d.id, "poolCount", len(pools))

	// Convert SDK pools to miner-specific format
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
	slog.Info("Rebooting device", "deviceID", d.id)

	if err := d.client.Reboot(ctx); err != nil {
		return fmt.Errorf("failed to reboot device: %w", err)
	}

	// Invalidate cached status
	d.lastStatus = nil

	return nil
}

// FirmwareUpdate implements the SDK Device interface.
//
// This method initiates a firmware update on the device.
func (d *Device) FirmwareUpdate(ctx context.Context) error {
	slog.Info("Starting firmware update", "deviceID", d.id)

	if err := d.client.UpdateFirmware(ctx); err != nil {
		return fmt.Errorf("failed to start firmware update: %w", err)
	}

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
	// We can provide a web view URL
	url := fmt.Sprintf("%s://%s", d.deviceInfo.URLScheme, d.deviceInfo.Host)
	return url, true, nil
}

func (d *Device) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil // Time series not supported
}
