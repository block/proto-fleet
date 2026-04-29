// Package device implements the SDK Device interface for virtual miners.
package device

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/plugin/virtual/internal/config"
	"github.com/block/proto-fleet/plugin/virtual/pkg/virtual"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Compile-time check that *Device implements sdk.Device interface.
var _ sdk.Device = (*Device)(nil)

const statusCacheTTL = 5 * time.Second

// Device implements sdk.Device for a virtual miner.
type Device struct {
	id         string
	deviceInfo sdk.DeviceInfo
	config     *config.VirtualMinerConfig
	simulator  *virtual.Simulator

	// Simulated state
	isMining        bool
	coolingMode     sdk.CoolingMode
	performanceMode sdk.PerformanceMode
	pools           []sdk.MiningPoolConfig

	// curtailLevel is nonzero while telemetry should report inactive mining.
	curtailLevel           sdk.CurtailLevel
	preCurtailMiningActive *bool

	// Status caching
	lastStatus   *sdk.DeviceMetrics
	lastStatusAt time.Time

	mutex sync.Mutex
}

// New creates a new virtual device instance.
func New(id string, deviceInfo sdk.DeviceInfo, cfg *config.VirtualMinerConfig) *Device {
	return &Device{
		id:              id,
		deviceInfo:      deviceInfo,
		config:          cfg,
		simulator:       virtual.NewSimulator(cfg),
		isMining:        true, // Start in mining state by default
		coolingMode:     sdk.CoolingModeAirCooled,
		performanceMode: sdk.PerformanceModeMaximumHashrate,
		pools:           []sdk.MiningPoolConfig{},
	}
}

// ID implements sdk.DeviceCore.
func (d *Device) ID() string {
	return d.id
}

// DescribeDevice implements sdk.DeviceCore.
func (d *Device) DescribeDevice(_ context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	return d.deviceInfo, sdk.Capabilities{
		sdk.CapabilityPollingHost:       true,
		sdk.CapabilityReboot:            true,
		sdk.CapabilityMiningStart:       true,
		sdk.CapabilityMiningStop:        true,
		sdk.CapabilityLEDBlink:          true,
		sdk.CapabilityCoolingModeAir:    true,
		sdk.CapabilityPoolConfig:        true,
		sdk.CapabilityHashrateReported:  true,
		sdk.CapabilityPowerUsage:        true,
		sdk.CapabilityTemperature:       true,
		sdk.CapabilityFanSpeed:          true,
		sdk.CapabilityEfficiency:        true,
		sdk.CapabilityPerBoardStats:     true,
		sdk.CapabilityPSUStats:          true,
		sdk.CapabilityRealtimeTelemetry: true,
		// v1 advertises FULL curtailment only.
		sdk.CapabilityCurtailFull: true,
	}, nil
}

// Status implements sdk.DeviceCore.
func (d *Device) Status(_ context.Context) (sdk.DeviceMetrics, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	// Return cached status if still valid
	if d.lastStatus != nil && time.Since(d.lastStatusAt) < statusCacheTTL {
		return *d.lastStatus, nil
	}

	// Generate new metrics
	metrics := d.simulator.GenerateMetrics(d.id, d.isMining)
	d.lastStatus = &metrics
	d.lastStatusAt = time.Now()

	return metrics, nil
}

// Close implements sdk.DeviceCore.
func (d *Device) Close(_ context.Context) error {
	slog.Info("Closing virtual device", "device_id", d.id)
	return nil
}

// StartMining implements sdk.DeviceControl.
func (d *Device) StartMining(_ context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.isMining = true
	d.clearCurtailmentStateLocked()
	d.lastStatus = nil // Invalidate cache
	slog.Info("Virtual miner started mining", "device_id", d.id)
	return nil
}

// StopMining implements sdk.DeviceControl.
func (d *Device) StopMining(_ context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.isMining = false
	d.clearCurtailmentStateLocked()
	d.lastStatus = nil
	slog.Info("Virtual miner stopped mining", "device_id", d.id)
	return nil
}

// BlinkLED implements sdk.DeviceControl.
func (d *Device) BlinkLED(_ context.Context) error {
	slog.Info("Virtual miner LED blink triggered", "device_id", d.id)
	return nil
}

// Reboot implements sdk.DeviceControl.
func (d *Device) Reboot(_ context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	slog.Info("Virtual miner rebooting", "device_id", d.id)

	// Simulate brief downtime
	d.isMining = false
	d.lastStatus = nil

	// Immediately come back up (in a real scenario you might add a delay)
	d.isMining = true

	return nil
}

// Curtail honors FULL and rejects reserved levels.
func (d *Device) Curtail(_ context.Context, req sdk.CurtailRequest) error {
	if req.Level != sdk.CurtailLevelFull {
		return sdk.NewErrCurtailCapabilityNotSupported(d.id, int32(req.Level))
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.preCurtailMiningActive == nil {
		wasMining := d.isMining
		d.preCurtailMiningActive = &wasMining
	}
	d.curtailLevel = req.Level
	d.isMining = false
	d.lastStatus = nil
	slog.Info("Virtual miner curtailed", "device_id", d.id, "level", req.Level)
	return nil
}

// Uncurtail clears curtailment; duplicate calls are no-ops.
func (d *Device) Uncurtail(_ context.Context, _ sdk.UncurtailRequest) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.curtailLevel == sdk.CurtailLevelUnspecified {
		slog.Info("Virtual miner uncurtail requested while not curtailed (no-op)", "device_id", d.id)
		return nil
	}
	shouldMine := true
	if d.preCurtailMiningActive != nil {
		shouldMine = *d.preCurtailMiningActive
	}
	d.curtailLevel = sdk.CurtailLevelUnspecified
	d.preCurtailMiningActive = nil
	d.isMining = shouldMine
	d.lastStatus = nil
	slog.Info("Virtual miner uncurtailed", "device_id", d.id)
	return nil
}

func (d *Device) clearCurtailmentStateLocked() {
	d.curtailLevel = sdk.CurtailLevelUnspecified
	d.preCurtailMiningActive = nil
}

// GetCoolingMode implements sdk.DeviceConfiguration.
func (d *Device) GetCoolingMode(_ context.Context) (sdk.CoolingMode, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	return d.coolingMode, nil
}

// SetCoolingMode implements sdk.DeviceConfiguration.
func (d *Device) SetCoolingMode(_ context.Context, mode sdk.CoolingMode) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.coolingMode = mode
	slog.Info("Virtual miner cooling mode set", "device_id", d.id, "mode", mode)
	return nil
}

// SetPowerTarget implements sdk.DeviceConfiguration.
func (d *Device) SetPowerTarget(_ context.Context, mode sdk.PerformanceMode) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.performanceMode = mode
	slog.Info("Virtual miner performance mode set", "device_id", d.id, "mode", mode)
	return nil
}

// UpdateMiningPools implements sdk.DeviceConfiguration.
func (d *Device) UpdateMiningPools(_ context.Context, pools []sdk.MiningPoolConfig) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.pools = pools
	slog.Info("Virtual miner pools updated", "device_id", d.id, "pool_count", len(pools))
	return nil
}

// GetMiningPools implements sdk.DeviceConfiguration.
func (d *Device) GetMiningPools(_ context.Context) ([]sdk.ConfiguredPool, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	result := make([]sdk.ConfiguredPool, len(d.pools))
	for i, p := range d.pools {
		result[i] = sdk.ConfiguredPool{
			Priority: p.Priority,
			URL:      p.URL,
			Username: p.WorkerName,
		}
	}
	return result, nil
}

// UpdateMinerPassword implements sdk.DeviceConfiguration.
func (d *Device) UpdateMinerPassword(_ context.Context, _, _ string) error {
	slog.Info("Virtual miner password update requested (no-op)", "device_id", d.id)
	return nil
}

// DownloadLogs implements sdk.DeviceMaintenance.
func (d *Device) DownloadLogs(_ context.Context, _ *time.Time, _ string) (string, bool, error) {
	return "Virtual miner log data - no actual logs available", false, nil
}

// FirmwareUpdate implements sdk.DeviceMaintenance.
func (d *Device) FirmwareUpdate(_ context.Context, _ sdk.FirmwareFile) error {
	slog.Info("Virtual miner firmware update requested (no-op)", "device_id", d.id)
	return nil
}

// Unpair implements sdk.DeviceMaintenance.
func (d *Device) Unpair(_ context.Context) error {
	slog.Info("Virtual miner unpaired", "device_id", d.id)
	return nil
}

// GetErrors implements sdk.DeviceErrorReporting.
// Virtual miners report errors based on error injection configuration.
// Currently returns an empty list - error injection affects telemetry health status instead.
func (d *Device) GetErrors(_ context.Context) (sdk.DeviceErrors, error) {
	return sdk.DeviceErrors{
		DeviceID: d.id,
		Errors:   []sdk.DeviceError{},
	}, nil
}

// TryBatchStatus implements sdk.DeviceOptional.
func (d *Device) TryBatchStatus(_ context.Context, _ []string) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

// TrySubscribe implements sdk.DeviceOptional.
func (d *Device) TrySubscribe(_ context.Context, _ []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

// TryGetWebViewURL implements sdk.DeviceOptional.
func (d *Device) TryGetWebViewURL(_ context.Context) (string, bool, error) {
	return "", false, nil
}

// TryGetTimeSeriesData implements sdk.DeviceOptional.
func (d *Device) TryGetTimeSeriesData(_ context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}
