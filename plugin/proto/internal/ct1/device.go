package ct1

import (
	"context"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type Device struct {
	id     string
	info   sdk.DeviceInfo
	client *Client
}

var _ sdk.Device = (*Device)(nil)

func NewDevice(ctx context.Context, id string, info sdk.DeviceInfo, credentials sdk.UsernamePassword) (*Device, error) {
	c, err := NewClient(info, credentials)
	if err != nil {
		return nil, err
	}
	info, err = c.Discover(ctx, info)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return &Device{id: id, info: info, client: c}, nil
}
func Capabilities() sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityMiningGroups: true, sdk.CapabilityTankMining: true, sdk.CapabilityModuleMining: true, sdk.CapabilityContainerControl: true, sdk.CapabilityPollingHost: true, sdk.CapabilityDiscovery: true, sdk.CapabilityPairing: true, sdk.CapabilityRealtimeTelemetry: true, sdk.CapabilityHashrateReported: true, sdk.CapabilityPowerUsage: true}
}
func (d *Device) ID() string { return d.id }
func (d *Device) DescribeDevice(context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	return d.info, Capabilities(), nil
}
func (d *Device) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	metrics, err := d.client.Collect(ctx, d.id)
	metrics.FirmwareVersion = d.info.FirmwareVersion
	return metrics, err
}
func (d *Device) Close(context.Context) error { return d.client.Close() }

// Unpair is a Fleet-side removal. It does not reset the container or its peers.
func (d *Device) Unpair(context.Context) error { return nil }
func (d *Device) StartMining(context.Context) error {
	return sdk.NewErrUnsupportedCapability("mining_start")
}
func (d *Device) StopMining(context.Context) error {
	return sdk.NewErrUnsupportedCapability("mining_stop")
}
func (d *Device) BlinkLED(context.Context) error { return sdk.NewErrUnsupportedCapability("led_blink") }
func (d *Device) Reboot(context.Context) error   { return sdk.NewErrUnsupportedCapability("reboot") }
func (d *Device) SetCoolingMode(context.Context, sdk.CoolingMode) error {
	return sdk.NewErrUnsupportedCapability("cooling_mode")
}
func (d *Device) GetCoolingMode(context.Context) (sdk.CoolingMode, error) {
	return sdk.CoolingModeUnspecified, sdk.NewErrUnsupportedCapability("cooling_mode")
}
func (d *Device) SetPowerTarget(context.Context, sdk.PerformanceMode) error {
	return sdk.NewErrUnsupportedCapability("power_target")
}
func (d *Device) UpdateMiningPools(context.Context, []sdk.MiningPoolConfig) error {
	return sdk.NewErrUnsupportedCapability("pool_config")
}
func (d *Device) UpdateMinerPassword(context.Context, string, string) error {
	return sdk.NewErrUnsupportedCapability("password")
}
func (d *Device) GetMiningPools(context.Context) ([]sdk.ConfiguredPool, error) {
	return nil, sdk.NewErrUnsupportedCapability("pool_config")
}
func (d *Device) DownloadLogs(context.Context, *time.Time, string) (string, bool, error) {
	return "", false, sdk.NewErrUnsupportedCapability("logs")
}
func (d *Device) FirmwareUpdate(context.Context, sdk.FirmwareFile) error {
	return sdk.NewErrUnsupportedCapability("firmware")
}
func (d *Device) GetErrors(context.Context) (sdk.DeviceErrors, error) {
	return sdk.DeviceErrors{}, sdk.NewErrUnsupportedCapability("errors")
}
func (d *Device) TryBatchStatus(context.Context, []sdk.Device) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}
func (d *Device) TrySubscribe(context.Context, []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}
func (d *Device) TryGetWebViewURL(context.Context) (string, bool, error) {
	return d.client.baseURL, true, nil
}
func (d *Device) TryGetTimeSeriesData(context.Context, []string, time.Time, time.Time, *time.Duration, int32, string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}
