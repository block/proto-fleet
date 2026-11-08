package ct1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type Device struct {
	id              string
	info            sdk.DeviceInfo
	clients         []*Client
	writeMu         sync.Mutex
	firmwareMu      sync.Mutex
	firmwareAttempt *firmwareAttempt
}

var _ sdk.Device = (*Device)(nil)

func NewDevice(ctx context.Context, id string, info sdk.DeviceInfo, credentials sdk.UsernamePassword) (*Device, error) {
	d := &Device{id: id, info: info}
	if len(info.Endpoints) > 0 {
		if len(info.Endpoints) > 2 {
			return nil, fmt.Errorf("CT1 must have at most two endpoints")
		}
		if _, err := sdk.DeviceEndpointKey(info.Endpoints); err != nil {
			return nil, err
		}
		for _, endpoint := range info.Endpoints {
			member := info
			member.Host, member.Port, member.URLScheme = endpoint.Host, endpoint.Port, endpoint.URLScheme
			member.RPCPort, member.RPCTLS = endpoint.RPCPort, endpoint.RPCTLS
			c, err := NewClient(member, credentials)
			if err != nil {
				_ = d.Close(ctx)
				return nil, err
			}
			c.memberID = endpoint.ID
			d.clients = append(d.clients, c)
		}
	} else {
		c, err := NewClient(info, credentials)
		if err != nil {
			return nil, err
		}
		d.clients = []*Client{c}
	}
	if len(info.Endpoints) == 0 {
		verified, err := d.clients[0].Discover(ctx, info)
		if err != nil {
			_ = d.Close(ctx)
			return nil, err
		}
		d.info = verified
		d.clients[0].memberID = verified.SerialNumber
	}
	return d, nil
}
func Capabilities() sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityFirmware: true, sdk.CapabilityManualUpload: true, sdk.CapabilityFirmwareSelfManaged: true, sdk.CapabilityContainerMining: true, sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true, sdk.CapabilityMiningGroups: true, sdk.CapabilityTankMining: true, sdk.CapabilityModuleMining: true, sdk.CapabilityContainerControl: true, sdk.CapabilityPollingHost: true, sdk.CapabilityDiscovery: true, sdk.CapabilityPairing: true, sdk.CapabilityRealtimeTelemetry: true, sdk.CapabilityHashrateReported: true, sdk.CapabilityPowerUsage: true}
}
func (d *Device) ID() string { return d.id }
func (d *Device) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	info := d.info
	if len(info.Endpoints) > 0 {
		// Pairing verifies live identity here; reads and writes probe when used.
		selected, _, err := d.probeControllers(ctx, false)
		if err != nil {
			return sdk.DeviceInfo{}, nil, err
		}
		info.FirmwareVersion = selected.state.GetVersion()
		if info.SerialNumber == "" {
			info.SerialNumber = selected.state.GetServerId()
		}
	}
	return info, Capabilities(), nil
}
func (d *Device) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	selected, ha, err := d.probeControllers(ctx, false)
	if err != nil {
		return sdk.DeviceMetrics{}, err
	}
	firmwareJobs := observeFirmwareJobs(ctx, selected)
	metrics, err := selected.client.Collect(ctx, d.id)
	if err != nil && selected.standby != nil && ctx.Err() == nil {
		// Reuse this poll's verified standby. Only telemetry can fall back;
		// commands and their readback stay on one selected connection.
		selected = selected.standby
		firstErr := err
		metrics, err = selected.client.Collect(ctx, d.id)
		if err != nil {
			err = errors.Join(firstErr, err)
		}
		ha.Degraded = true
	}
	if metrics.Container != nil {
		metrics.Container.HA = ha
		enrichFirmwareBoards(metrics.Container, <-firmwareJobs, time.Now().UTC())
	}
	metrics.FirmwareVersion = d.info.FirmwareVersion
	if selected.state != nil {
		metrics.FirmwareVersion = selected.state.GetVersion()
	}
	return metrics, err
}
func (d *Device) Close(context.Context) error {
	var errs []error
	for _, c := range d.clients {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}

// Unpair is a Fleet-side removal. It does not reset the container or its peers.
func (d *Device) Unpair(context.Context) error { return nil }
func (d *Device) StartMining(context.Context) error {
	return sdk.UnsupportedWrite("mining_start")
}
func (d *Device) StopMining(context.Context) error {
	return sdk.UnsupportedWrite("mining_stop")
}
func (d *Device) BlinkLED(context.Context) error {
	return sdk.UnsupportedWrite("led_blink")
}
func (d *Device) Reboot(context.Context) error {
	return sdk.UnsupportedWrite("reboot")
}
func (d *Device) SetCoolingMode(context.Context, sdk.CoolingMode) error {
	return sdk.UnsupportedWrite("cooling_mode")
}
func (d *Device) GetCoolingMode(context.Context) (sdk.CoolingMode, error) {
	return sdk.CoolingModeUnspecified, sdk.NewErrUnsupportedCapability("cooling_mode")
}
func (d *Device) SetPowerTarget(context.Context, sdk.PerformanceMode) error {
	return sdk.UnsupportedWrite("power_target")
}
func (d *Device) UpdateMinerPassword(context.Context, string, string) error {
	return sdk.UnsupportedWrite("password")
}
func (d *Device) DownloadLogs(context.Context, *time.Time, string) (string, bool, error) {
	return "", false, sdk.NewErrUnsupportedCapability("logs")
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
	web := url.URL{Scheme: d.info.URLScheme, Host: net.JoinHostPort(d.info.Host, strconv.Itoa(int(d.info.Port)))}
	return web.String(), true, nil
}
func (d *Device) TryGetTimeSeriesData(context.Context, []string, time.Time, time.Time, *time.Duration, int32, string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}
