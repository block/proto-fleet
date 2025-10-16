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
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

var _ sdk.Device = (*Device)(nil) // Ensure Device implements sdk.Device

const (
	defaultStatusTTL = 30 * time.Second // Default time-to-live for cached status
	maxLogLines      = 10000            // Maximum number of log lines to retrieve
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
	lastStatus   *sdk.DeviceStatusResponse
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
func (d *Device) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
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
		return sdk.DeviceStatusResponse{}, fmt.Errorf("failed to get miner status: %w", err)
	}

	// Get telemetry data
	telemetry, err := d.client.GetTelemetry(ctx)
	if err != nil {
		slog.Warn("Failed to get telemetry data", "deviceID", d.id, "error", err)
		// Continue without telemetry - status is more important
	}

	// Convert miner status to SDK format
	status := d.convertStatus(minerStatus, telemetry)

	// Cache the status
	d.lastStatus = &status
	d.lastStatusAt = time.Now()

	return status, nil
}

// convertStatus converts miner-specific status to SDK format.
//
// This helper method demonstrates:
//   - Status mapping between different formats
//   - Health determination logic
//   - Telemetry data integration
func (d *Device) convertStatus(minerStatus *proto.Status, telemetry *proto.Telemetry) sdk.DeviceStatusResponse {
	now := time.Now()

	// Determine health status based on miner state
	var health sdk.HealthStatus
	var summary string

	var minerState string
	switch minerStatus.State {
	case sdk.HealthHealthyActive:
		minerState = "mining"
		summary = "Mining"
	case sdk.HealthyInactive:
		minerState = "idle"
		health = sdk.HealthyInactive
		summary = "Idle"
	case sdk.Critical:
		minerState = "error"
		health = sdk.Critical
		summary = "Error: " + minerStatus.ErrorMessage
	case sdk.Warning:
		minerState = "warning"
		health = sdk.Warning
		summary = "Warning: " + minerStatus.ErrorMessage
	case sdk.HealthStatusUnspecified, sdk.HealthUnknown:
		minerState = "unknown"
		health = sdk.Unknown
		summary = "Unknown state"
	default:
		health = sdk.Unknown
		summary = "Unknown state"
	}

	status := sdk.DeviceStatusResponse{
		DeviceID:  d.id,
		Timestamp: now,
		Summary:   summary,
		Health:    health,
		Metadata: map[string]string{
			"miner_state":      minerState,
			"ip_address":       d.deviceInfo.Host,
			"serial_number":    d.deviceInfo.SerialNumber,
			"firmware_version": minerStatus.FirmwareVersion,
		},
	}

	// Add telemetry data if available
	if telemetry != nil {
		if telemetry.HashrateHS > 0 {
			status.HashrateHS = &telemetry.HashrateHS
		}
		if telemetry.PowerWatts > 0 {
			status.PowerWatts = &telemetry.PowerWatts
		}
		if telemetry.TemperatureCelsius > 0 {
			status.TemperatureCelsius = &telemetry.TemperatureCelsius
		}
		if telemetry.EfficiencyJPerHash > 0 {
			status.EfficiencyJPerHash = &telemetry.EfficiencyJPerHash
		}
		if telemetry.FanRPM > 0 {
			status.FanRPM = &telemetry.FanRPM
		}

		// Add additional metrics
		status.ExtraMetrics = []sdk.Metric{
			{
				Name:       "uptime_seconds",
				Value:      sdk.NewMetricValue(telemetry.UptimeSeconds),
				Unit:       sdk.UnitUnspecified,
				Kind:       sdk.MetricKindCounter,
				ObservedAt: now,
				Labels: map[string]string{
					"device_id": d.id,
				},
			},
		}
	}

	// Set sampling semantics
	status.Sample = &sdk.SampleSemantics{
		Aggregation:     sdk.AggregationGauge,
		AveragingWindow: telemetry.TimeInterval,
		StartOfWindow:   now.Truncate(telemetry.TimeInterval),
	}

	return status
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

func (d *Device) TryBatchStatus(ctx context.Context, _ []string) (map[string]sdk.DeviceStatusResponse, bool, error) {
	return nil, false, nil // Not supported by individual devices
}

func (d *Device) TrySubscribe(ctx context.Context, _ []string) (<-chan sdk.DeviceStatusResponse, bool, error) {
	return nil, false, nil // Streaming not supported
}

func (d *Device) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	// We can provide a web view URL
	url := fmt.Sprintf("%s://%s", d.deviceInfo.URLScheme, d.deviceInfo.Host)
	return url, true, nil
}

func (d *Device) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceStatusResponse, string, bool, error) {
	return nil, "", false, nil // Time series not supported
}
