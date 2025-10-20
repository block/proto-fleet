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
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

const (
	statusCacheTTL   = 5 * time.Second  // Cache status for 5 seconds
	port             = 4028             // Default RPC port for Antminers
	newDeviceTimeout = 10 * time.Second // Timeout for new device creation
	// AveragingWindow is the time window used for status sampling aggregation.
	// This is assumed based on refresh period for antminers.
	AveragingWindow  = 5 * time.Second
	blinkLEDDuration = 30 * time.Second // Duration to blink LED for identification
)

var _ sdk.Device = (*Device)(nil) // Ensure Device implements sdk.Device

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
	lastStatus   *sdk.DeviceStatusResponse
	lastStatusAt time.Time
	statusMutex  sync.RWMutex
	statusTTL    time.Duration
}

type DeviceOption func(*Device) error

func WithStatusTTL(ttl time.Duration) DeviceOption {
	return func(d *Device) error {
		if ttl < 0 {
			return fmt.Errorf("status TTL must be positive")
		}
		d.statusTTL = ttl
		return nil
	}
}

// New creates a new Antminer device instance.
func New(deviceID string, deviceInfo sdk.DeviceInfo, credentials sdk.UsernamePassword, clientFactory types.ClientFactory, opts ...DeviceOption) (*Device, error) {
	device := &Device{
		id:          deviceID,
		deviceInfo:  deviceInfo,
		credentials: credentials,
		statusTTL:   statusCacheTTL,
	}

	for _, opt := range opts {
		if err := opt(device); err != nil {
			return nil, fmt.Errorf("failed to apply device option: %w", err)
		}
	}

	client, err := clientFactory(deviceInfo.Host, port, deviceInfo.Port, deviceInfo.URLScheme)
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
func (d *Device) Status(ctx context.Context) (sdk.DeviceStatusResponse, error) {
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
		return sdk.DeviceStatusResponse{}, fmt.Errorf("failed to get miner status: %w", err)
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

// convertStatus converts Antminer-specific status to SDK format.
func (d *Device) convertStatus(minerStatus *antminer.Status, telemetry *antminer.Telemetry) sdk.DeviceStatusResponse {
	now := time.Now()

	// Determine health status and summary based on miner state and performance
	health := minerStatus.State
	var summary string

	// Determine summary based on miner state
	switch minerStatus.State {
	case sdk.HealthHealthyActive:
		// Detect if active miner has not hash rate, which may indicate an issue
		if telemetry != nil && telemetry.HashrateHS != nil && *telemetry.HashrateHS > 0 {
			hashrateTS := *telemetry.HashrateHS / 1e12
			summary = fmt.Sprintf("Mining at %.2f TH/s", hashrateTS)
			health = sdk.HealthHealthyActive
		} else {
			summary = "Mining but no hashrate detected"
			health = sdk.Warning
		}
	case sdk.HealthyInactive:
		summary = "Idle"
	case sdk.Warning:
		summary = "Warning: " + minerStatus.ErrorMessage
	case sdk.Critical:
		summary = "Error: " + minerStatus.ErrorMessage
	case sdk.Unknown:
		summary = "Status unknown"
	case sdk.HealthStatusUnspecified:
		summary = "Status unspecified"
	default:
		summary = "Unknown state"
	}

	status := sdk.DeviceStatusResponse{
		DeviceID:  d.id,
		Timestamp: now,
		Summary:   summary,
		Health:    health,
		Metadata: map[string]string{
			"ip_address":       d.deviceInfo.Host,
			"model":            d.deviceInfo.Model,
			"manufacturer":     d.deviceInfo.Manufacturer,
			"firmware_version": minerStatus.FirmwareVersion,
			"username":         d.credentials.Username, // Safe to log username
		},
	}

	// Add telemetry data if available
	if telemetry != nil {
		status.HashrateHS = telemetry.HashrateHS

		status.PowerWatts = telemetry.PowerWatts
		status.TemperatureCelsius = telemetry.TemperatureCelsius
		status.EfficiencyJPerHash = telemetry.EfficiencyJPerHash
		if telemetry.FanRPM != nil {
			fanRPM := int32(*telemetry.FanRPM)
			status.FanRPM = &fanRPM
		}

		// Add additional metrics from RPC data only if UptimeSeconds is not nil
		if telemetry.UptimeSeconds != nil {
			status.ExtraMetrics = []sdk.Metric{
				{
					Name:       "uptime_seconds",
					Value:      sdk.NewMetricValue(*telemetry.UptimeSeconds),
					Unit:       sdk.UnitUnspecified,
					Kind:       sdk.MetricKindCounter,
					ObservedAt: now,
					Labels: map[string]string{
						"device_id": d.id,
					},
				},
			}
		}
	}

	// Set sampling semantics
	status.Sample = &sdk.SampleSemantics{
		Aggregation:     sdk.AggregationGauge,
		AveragingWindow: AveragingWindow,
		StartOfWindow:   now.Truncate(AveragingWindow),
	}

	return status
}

// Close implements the SDK Device interface.
func (d *Device) Close(ctx context.Context) error {
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()
	slog.Debug("Closing Antminer device", "deviceID", d.id)

	if d.client != nil {
		d.client.Close()
	}

	// Clear cached data
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

// Optional capabilities - these return false to indicate they're not supported

func (d *Device) TryBatchStatus(ctx context.Context, _ []string) (map[string]sdk.DeviceStatusResponse, bool, error) {
	return nil, false, nil // Not supported by individual devices
}

func (d *Device) TrySubscribe(ctx context.Context, _ []string) (<-chan sdk.DeviceStatusResponse, bool, error) {
	return nil, false, nil // Streaming not supported
}

func (d *Device) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	// We can provide a web view URL for the Antminer web interface
	url := fmt.Sprintf("%s://%s", d.deviceInfo.URLScheme, d.deviceInfo.Host)
	return url, true, nil
}

func (d *Device) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceStatusResponse, string, bool, error) {
	return nil, "", false, nil // Time series not supported
}
