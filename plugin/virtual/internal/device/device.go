// Package device implements the SDK Device interface for virtual miners.
package device

import (
	"context"
	"log/slog"
	"math/rand/v2"
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
	latencyRNG *rand.Rand

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

	mutex     sync.Mutex
	latencyMu sync.Mutex
}

// New creates a new virtual device instance.
func New(id string, deviceInfo sdk.DeviceInfo, cfg *config.VirtualMinerConfig) *Device {
	return &Device{
		id:              id,
		deviceInfo:      deviceInfo,
		config:          cfg,
		simulator:       virtual.NewSimulator(cfg),
		latencyRNG:      rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1)),
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
func (d *Device) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	if err := d.waitForLatency(ctx, false); err != nil {
		return sdk.DeviceInfo{}, nil, err
	}
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
func (d *Device) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	if err := d.waitForLatency(ctx, true); err != nil {
		return sdk.DeviceMetrics{}, err
	}

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
func (d *Device) StartMining(ctx context.Context) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.isMining = true
	d.clearCurtailmentStateLocked()
	d.lastStatus = nil // Invalidate cache
	slog.Info("Virtual miner started mining", "device_id", d.id)
	return nil
}

// StopMining implements sdk.DeviceControl.
func (d *Device) StopMining(ctx context.Context) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.isMining = false
	d.clearCurtailmentStateLocked()
	d.lastStatus = nil
	slog.Info("Virtual miner stopped mining", "device_id", d.id)
	return nil
}

// BlinkLED implements sdk.DeviceControl.
func (d *Device) BlinkLED(ctx context.Context) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}
	slog.Info("Virtual miner LED blink triggered", "device_id", d.id)
	return nil
}

// Reboot implements sdk.DeviceControl.
func (d *Device) Reboot(ctx context.Context) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

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
func (d *Device) Curtail(ctx context.Context, req sdk.CurtailRequest) error {
	if req.Level != sdk.CurtailLevelFull {
		return sdk.NewErrCurtailCapabilityNotSupported(d.id, int32(req.Level))
	}
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
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
func (d *Device) Uncurtail(ctx context.Context, _ sdk.UncurtailRequest) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

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
func (d *Device) GetCoolingMode(ctx context.Context) (sdk.CoolingMode, error) {
	if err := d.waitForLatency(ctx, true); err != nil {
		return sdk.CoolingModeUnspecified, err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	return d.coolingMode, nil
}

// SetCoolingMode implements sdk.DeviceConfiguration.
func (d *Device) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.coolingMode = mode
	slog.Info("Virtual miner cooling mode set", "device_id", d.id, "mode", mode)
	return nil
}

// SetPowerTarget implements sdk.DeviceConfiguration.
func (d *Device) SetPowerTarget(ctx context.Context, mode sdk.PerformanceMode) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.performanceMode = mode
	slog.Info("Virtual miner performance mode set", "device_id", d.id, "mode", mode)
	return nil
}

// UpdateMiningPools implements sdk.DeviceConfiguration.
func (d *Device) UpdateMiningPools(ctx context.Context, pools []sdk.MiningPoolConfig) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.pools = pools
	slog.Info("Virtual miner pools updated", "device_id", d.id, "pool_count", len(pools))
	return nil
}

// GetMiningPools implements sdk.DeviceConfiguration.
func (d *Device) GetMiningPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	if err := d.waitForLatency(ctx, true); err != nil {
		return nil, err
	}

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
func (d *Device) UpdateMinerPassword(ctx context.Context, _, _ string) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}
	slog.Info("Virtual miner password update requested (no-op)", "device_id", d.id)
	return nil
}

// DownloadLogs implements sdk.DeviceMaintenance.
func (d *Device) DownloadLogs(ctx context.Context, _ *time.Time, _ string) (string, bool, error) {
	if err := d.waitForLatency(ctx, true); err != nil {
		return "", false, err
	}
	return "Virtual miner log data - no actual logs available", false, nil
}

// FirmwareUpdate implements sdk.DeviceMaintenance.
func (d *Device) FirmwareUpdate(ctx context.Context, _ sdk.FirmwareFile) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}
	slog.Info("Virtual miner firmware update requested (no-op)", "device_id", d.id)
	return nil
}

// Unpair implements sdk.DeviceMaintenance.
func (d *Device) Unpair(ctx context.Context) error {
	if err := d.waitForLatency(ctx, true); err != nil {
		return err
	}
	slog.Info("Virtual miner unpaired", "device_id", d.id)
	return nil
}

// GetErrors implements sdk.DeviceErrorReporting.
// Virtual miners report errors based on error injection configuration.
// Currently returns an empty list - error injection affects telemetry health status instead.
func (d *Device) GetErrors(ctx context.Context) (sdk.DeviceErrors, error) {
	if err := d.waitForLatency(ctx, true); err != nil {
		return sdk.DeviceErrors{}, err
	}
	return sdk.DeviceErrors{
		DeviceID: d.id,
		Errors:   []sdk.DeviceError{},
	}, nil
}

func (d *Device) waitForLatency(ctx context.Context, includeInternal bool) error {
	delay := d.sampleLatency(includeInternal)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Device) sampleLatency(includeInternal bool) time.Duration {
	d.latencyMu.Lock()
	defer d.latencyMu.Unlock()

	delay := d.config.Behavior.NetworkLatency.Sample(d.latencyRNG)
	if includeInternal {
		delay += d.config.Behavior.InternalLatency.Sample(d.latencyRNG)
	}
	return delay
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
