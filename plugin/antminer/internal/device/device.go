// Package device implements the Fleet SDK Device interface for individual Antminer devices.
package device

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/block/proto-fleet/plugin/antminer/internal/types"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	rpcPort                 = 4028             // Default RPC port for Antminers
	newDeviceTimeout        = 10 * time.Second // Timeout for new device creation
	blinkLEDDuration        = 30 * time.Second // Duration to blink LED for identification
	firmwareRefreshInterval = 5 * time.Minute

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

func isSleepMode(config *web.MinerConfig) bool {
	if config == nil {
		return false
	}

	workMode := config.BitmainWorkMode
	if config.MinerMode != "" {
		workMode = web.BitmainWorkMode(config.MinerMode)
	}

	return workMode == web.BitmainWorkModeSleep
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
	statusMutex  sync.Mutex
	statusTTL    time.Duration

	lastFirmwareCheckAt time.Time
}

// New creates a new Antminer device instance.
func New(deviceID string, deviceInfo sdk.DeviceInfo, credentials sdk.UsernamePassword, clientFactory types.ClientFactory) (*Device, error) {
	device := &Device{
		id:          deviceID,
		deviceInfo:  deviceInfo,
		credentials: credentials,
		statusTTL:   types.StatusCacheTTL(),
	}

	// If firmware version is already known from pairing, start the refresh
	// throttle from now so we don't immediately re-fetch what we already have.
	if deviceInfo.FirmwareVersion != "" {
		device.lastFirmwareCheckAt = time.Now()
	}

	client, err := clientFactory(deviceInfo.Host, rpcPort, types.WebPort(), deviceInfo.URLScheme)
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
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()

	capabilities := sdk.Capabilities{
		// Core capabilities
		sdk.CapabilityPollingHost: true, // This device supports RPC status polling

		// Command capabilities - based on Antminer capabilities
		sdk.CapabilityReboot:              true,  // We can reboot devices
		sdk.CapabilityMiningStart:         true,  // Supported via bitmain-work-mode = "0"
		sdk.CapabilityMiningStop:          true,  // Supported via bitmain-work-mode = "1" (sleep)
		sdk.CapabilityCurtail:             true,  // FULL curtailment uses mining start/stop.
		sdk.CapabilityLEDBlink:            true,  // We can blink LED for identification
		sdk.CapabilityFactoryReset:        false, // Factory reset not supported
		sdk.CapabilityCoolingModeAir:      false, // Air cooling mode not configurable
		sdk.CapabilityCoolingModeImmerse:  false, // Immersion cooling mode not supported
		sdk.CapabilityPoolConfig:          true,  // We can configure mining pools
		sdk.CapabilityPoolPriority:        true,  // We can set pool priority
		sdk.CapabilityLogsDownload:        true,  // We can download logs
		sdk.CapabilityUpdateMinerPassword: true,  // We can update web UI password

		// Telemetry capabilities
		sdk.CapabilityRealtimeTelemetry: true,  // We support real-time telemetry
		sdk.CapabilityHistoricalData:    false, // Historical data not supported
		sdk.CapabilityHashrateReported:  true,  // We report hashrate
		sdk.CapabilityPowerUsage:        true,  // We report power usage
		sdk.CapabilityTemperature:       true,  // We report temperature
		sdk.CapabilityFanSpeed:          true,  // We report fan speed
		sdk.CapabilityEfficiency:        true,  // We report efficiency
		sdk.CapabilityUptime:            true,  // We report uptime
		sdk.CapabilityErrorCount:        true,  // We report error count
		sdk.CapabilityMinerStatus:       true,  // We report miner status
		sdk.CapabilityPoolStats:         true,  // We report pool stats
		sdk.CapabilityPerChipStats:      true,  // We report per-chip stats
		sdk.CapabilityPerBoardStats:     true,  // We report per-board stats
		sdk.CapabilityPSUStats:          false, // PSU stats not supported

		// Firmware capabilities
		sdk.CapabilityFirmware:     true,  // We support firmware operations
		sdk.CapabilityOTAUpdate:    false, // OTA update not supported
		sdk.CapabilityManualUpload: true,  // We support manual firmware upload

		// Authentication capabilities
		sdk.CapabilityBasicAuth: true, // We use basic (username/password) authentication
	}

	return d.deviceInfo, capabilities, nil
}

// Status implements the SDK Device interface.
func (d *Device) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()

	if d.lastStatus != nil && time.Since(d.lastStatusAt) < d.statusTTL {
		slog.Debug("Returning cached status", "deviceID", d.id)
		return *d.lastStatus, nil
	}

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

	d.refreshFirmwareVersion(ctx, &status)

	d.lastStatus = &status
	d.lastStatusAt = time.Now()

	return status, nil
}

// refreshFirmwareVersion periodically re-fetches firmware version from the device
// to detect firmware updates. Throttled to avoid excessive API calls.
func (d *Device) refreshFirmwareVersion(ctx context.Context, metrics *sdk.DeviceMetrics) {
	if time.Since(d.lastFirmwareCheckAt) < firmwareRefreshInterval {
		return
	}
	d.lastFirmwareCheckAt = time.Now()
	versionResp, err := d.client.GetVersion(ctx)
	if err != nil {
		slog.Debug("failed to get version during Status", "error", err)
		return
	}
	if len(versionResp.Version) > 0 {
		fw := versionResp.Version[0].BMMiner
		if fw == "" {
			fw = versionResp.Version[0].Miner
		}
		d.deviceInfo.FirmwareVersion = fw
		metrics.FirmwareVersion = fw
	}
}

// GetErrors returns all active and historical errors for the device.
// Since CGMiner RPC provides point-in-time metrics (not historical errors),
// errors are detected heuristically from current metric values.
func (d *Device) GetErrors(ctx context.Context) (sdk.DeviceErrors, error) {
	// Fetch data from both RPC and Web API in parallel - collect all available data even if some calls fail
	var summaryResp *rpc.SummaryResponse
	var devsResp *rpc.DevsResponse
	var poolsResp *rpc.PoolsResponse
	var statsResp *web.StatsInfo
	var sleeping bool

	g := new(errgroup.Group)

	g.Go(func() error {
		var err error
		if summaryResp, err = d.client.GetSummary(ctx); err != nil {
			slog.Warn("Failed to get summary for error detection", "deviceID", d.id, "error", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		if devsResp, err = d.client.GetDevs(ctx); err != nil {
			slog.Warn("Failed to get devs for error detection", "deviceID", d.id, "error", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		if poolsResp, err = d.client.GetPools(ctx); err != nil {
			slog.Warn("Failed to get pools for error detection", "deviceID", d.id, "error", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		if statsResp, err = d.client.GetStatsInfo(ctx); err != nil {
			slog.Warn("Failed to get stats for error detection", "deviceID", d.id, "error", err)
		}
		return nil
	})

	g.Go(func() error {
		config, err := d.client.GetMinerConfig(ctx)
		if err != nil {
			slog.Debug("Failed to get miner config for error detection", "deviceID", d.id, "error", err)
			return nil
		}

		sleeping = isSleepMode(config)
		return nil
	})

	_ = g.Wait() // We're collecting data even if some calls fail, so we ignore the error

	// Detect errors from the collected data
	errors := detectErrors(summaryResp, devsResp, poolsResp, statsResp, d.id, sleeping)

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
	// TODO: Move this mapping to fleet side so plugins don't need to handle every SDK
	// health status. Fleet should map unknown statuses to sensible defaults.
	switch minerStatus.State {
	case sdk.HealthHealthyActive:
		// Detect if active miner has no hash rate, which may indicate an issue
		if telemetry != nil && telemetry.HashrateHS != nil && *telemetry.HashrateHS == 0 {
			health = sdk.HealthWarning
			healthReason = ptrString("Mining but no hashrate detected")
		}
	case sdk.HealthHealthyInactive, sdk.HealthNeedsMiningPool:
		// Idle state is normal, needs mining pool is handled by device status
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
		DeviceID:        d.id,
		Timestamp:       now,
		FirmwareVersion: d.deviceInfo.FirmwareVersion,
		Health:          health,
		HealthReason:    healthReason,
	}

	// Add telemetry data if available
	if telemetry != nil {
		metrics.HashrateHS = setMetricIfNotNil(telemetry.HashrateHS)
		metrics.TempC = setMetricIfNotNil(telemetry.TemperatureCelsius)
		metrics.FanRPM = setMetricIfNotNil(telemetry.FanRPM)
		metrics.PowerW = setMetricIfNotNil(telemetry.PowerWatts)
		metrics.EfficiencyJH = setMetricIfNotNil(telemetry.EfficiencyJPerHash)

		// Map per-hashboard telemetry to SDK metrics
		for _, hb := range telemetry.HashBoards {
			// #nosec G115 -- ChipCount and chain Index are small hardware constants (0-255)
			chipCount := int32(hb.ChipCount)
			sdkHB := sdk.HashBoardMetrics{
				ComponentInfo: sdk.ComponentInfo{
					// #nosec G115 -- chain Index is a small hardware constant (0-2 for typical miners)
					Index:  int32(hb.Index),
					Name:   fmt.Sprintf("Chain %d", hb.Index),
					Status: sdk.ComponentStatusHealthy,
				},
				HashRateHS:       setMetricIfNotNil(hb.HashrateHS),
				TempC:            setMetricIfNotNil(hb.Temperature),
				InletTempC:       setMetricIfNotNil(hb.InletTemp),
				OutletTempC:      setMetricIfNotNil(hb.OutletTemp),
				ChipCount:        &chipCount,
				ChipFrequencyMHz: toMetricValue(float64(hb.ChipFrequencyMHz)),
			}
			if hb.SerialNumber != "" {
				sdkHB.SerialNumber = &hb.SerialNumber
			}
			metrics.HashBoards = append(metrics.HashBoards, sdkHB)
		}

		// Map per-fan telemetry to SDK metrics
		for _, fan := range telemetry.Fans {
			metrics.FanMetrics = append(metrics.FanMetrics, sdk.FanMetrics{
				ComponentInfo: sdk.ComponentInfo{
					// #nosec G115 -- fan Index is a small hardware constant (0-7 for typical miners)
					Index:  int32(fan.Index),
					Name:   fmt.Sprintf("Fan %d", fan.Index),
					Status: sdk.ComponentStatusHealthy,
				},
				RPM: toMetricValue(float64(fan.RPM)),
			})
		}

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

// StartMining and StopMining use bitmain-work-mode via the web API.

// StartMining implements the SDK Device interface.
func (d *Device) StartMining(ctx context.Context) error {
	return d.client.StartMining(ctx)
}

// StopMining implements the SDK Device interface.
func (d *Device) StopMining(ctx context.Context) error {
	return d.client.StopMining(ctx)
}

func (d *Device) invalidateStatusCache() {
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()
	d.lastStatus = nil
	d.lastStatusAt = time.Time{}
}

// Curtail implements FULL curtailment via StopMining.
func (d *Device) Curtail(ctx context.Context, level sdk.CurtailLevel) error {
	if level != sdk.CurtailLevelFull {
		return sdk.NewErrCurtailCapabilityNotSupported(d.id, int32(level))
	}
	if err := d.client.StopMining(ctx); err != nil {
		return err
	}

	d.invalidateStatusCache()
	return nil
}

// Uncurtail restores mining via StartMining.
func (d *Device) Uncurtail(ctx context.Context) error {
	if err := d.client.StartMining(ctx); err != nil {
		return err
	}

	d.invalidateStatusCache()
	return nil
}

// SetCoolingMode implements the SDK Device interface.
func (d *Device) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	return d.client.SetCoolingMode(ctx, web.CoolingMode(mode))
}

// GetCoolingMode implements the SDK Device interface.
// Antminer doesn't support cooling mode configuration, so this returns Unspecified.
func (d *Device) GetCoolingMode(_ context.Context) (sdk.CoolingMode, error) {
	return sdk.CoolingModeUnspecified, nil
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

// GetMiningPools implements the SDK Device interface.
// Retrieves the currently configured mining pools from the Antminer via RPC.
func (d *Device) GetMiningPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	slog.Debug("Getting mining pools", "deviceID", d.id)

	poolsResp, err := d.client.GetPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get mining pools: %w", err)
	}

	pools := make([]sdk.ConfiguredPool, 0, len(poolsResp.Pools))
	for _, pool := range poolsResp.Pools {
		// Only include pools that have a URL configured
		if pool.URL != "" {
			pools = append(pools, sdk.ConfiguredPool{
				// #nosec G115 -- Pool priorities are protocol-bounded (0-2 for default/backup1/backup2)
				Priority: int32(pool.Priority),
				URL:      pool.URL,
				Username: pool.User,
			})
		}
	}

	return pools, nil
}

// BlinkLED implements the SDK Device interface.
func (d *Device) BlinkLED(ctx context.Context) error {
	return d.client.BlinkLED(ctx, blinkLEDDuration)
}

// DownloadLogs implements the SDK Device interface.
// Retrieves kernel logs from the Antminer device via the web API.
func (d *Device) DownloadLogs(ctx context.Context, since *time.Time, _ string) (string, bool, error) {
	logs, hasMore, err := d.client.GetLogs(ctx, since, 0)
	if err != nil {
		return "", false, fmt.Errorf("failed to download logs: %w", err)
	}
	return logs, hasMore, nil
}

// Reboot implements the SDK Device interface.
func (d *Device) Reboot(ctx context.Context) error {
	return d.client.Reboot(ctx)
}

// FirmwareUpdate implements the SDK Device interface.
//
// The firmware file is uploaded to the Antminer via the CGI upgrade endpoint
// (POST /cgi-bin/upgrade.cgi, multipart/form-data with digest auth).
func (d *Device) FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error {
	if firmware.Reader == nil {
		return fmt.Errorf("firmware file is required for file-based firmware update")
	}

	return d.client.UploadFirmware(ctx, firmware)
}

func (d *Device) Unpair(ctx context.Context) error {
	// No specific unpair action needed for Antminer devices
	// Unpair is handled optimistically at the database level
	return nil
}

// UpdateMinerPassword implements the SDK Device interface.
func (d *Device) UpdateMinerPassword(ctx context.Context, currentPassword string, newPassword string) error {
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()

	// Clear cached status since credentials are changing
	d.lastStatus = nil
	d.lastStatusAt = time.Time{}

	// Update password via web API using current password for verification
	if err := d.client.ChangePassword(ctx, currentPassword, newPassword); err != nil {
		return fmt.Errorf("failed to update miner password: %w", err)
	}

	// Update stored credentials for future API calls (only password changes)
	d.credentials.Password = newPassword
	if err := d.client.SetCredentials(d.credentials); err != nil {
		return fmt.Errorf("failed to update client credentials: %w", err)
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
	host := d.deviceInfo.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	url := fmt.Sprintf("%s://%s", d.deviceInfo.URLScheme, host)
	return url, true, nil
}

func (d *Device) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil // Time series not supported
}
