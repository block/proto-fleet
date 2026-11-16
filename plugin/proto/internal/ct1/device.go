package ct1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type Device struct {
	id           string
	info         sdk.DeviceInfo
	clients      []*Client
	writeMu      sync.Mutex
	firmwareOnce sync.Once
	firmware     *firmwareState
	errorsOnce   sync.Once
	errorState   *errorState
	// updateOrder is a routing hint for Frigate update status calls.
	updateOrder updateOrder
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
			return nil, authFailure(id, err)
		}
		d.info = verified
		d.clients[0].memberID = verified.SerialNumber
	}
	return d, nil
}
func Capabilities() sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityWorkerNameDefaultNone: true, sdk.CapabilityFirmware: true, sdk.CapabilityManualUpload: true, sdk.CapabilityFirmwareSelfManaged: true, sdk.CapabilityContainerMining: true, sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true, sdk.CapabilityPoolPriority: true, sdk.CapabilityMiningGroups: true, sdk.CapabilityMiningGroupPoolClear: true, sdk.CapabilityTankMining: true, sdk.CapabilityModuleMining: true, sdk.CapabilityBoardIdentify: true, sdk.CapabilityBoardASICStream: true, sdk.CapabilityControllerRecords: true, sdk.CapabilityContainerPowerFeed: true, sdk.CapabilityContainerControl: true, sdk.CapabilityContainerAlarm: true, sdk.CapabilityFrigateServerUpdate: true, sdk.CapabilityPollingHost: true, sdk.CapabilityDiscovery: true, sdk.CapabilityPairing: true, sdk.CapabilityRealtimeTelemetry: true, sdk.CapabilityHashrateReported: true, sdk.CapabilityPowerUsage: true, sdk.CapabilityGetErrors: true}
}
func (d *Device) ID() string { return d.id }
func (d *Device) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	info := d.info
	if len(info.Endpoints) > 0 {
		// Pairing verifies live identity here; reads and writes probe when used.
		selected, _, err := d.probeControllers(ctx, false)
		if err != nil {
			return sdk.DeviceInfo{}, nil, authFailure(d.id, err)
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
		d.pollFailed(ctx, err)
		return sdk.DeviceMetrics{}, authFailure(d.id, err)
	}
	firmwareJobs := observeFirmwareJobs(ctx, selected)
	metrics, evidence, err := selected.client.collect(ctx, d.id)
	if err != nil && selected.standby != nil && ctx.Err() == nil {
		// Reuse this poll's verified standby. Only telemetry and error polling
		// (readErrorEvidence) can fall back; commands and their readback stay
		// on one selected connection.
		selected = selected.standby
		firstErr := err
		metrics, evidence, err = selected.client.collect(ctx, d.id)
		if err != nil {
			err = errors.Join(firstErr, err)
		}
		ha.Degraded = true
	}
	if err != nil {
		d.pollFailed(ctx, err)
	} else {
		d.errorsShared().observe(evidence.readFrom(selected))
	}
	if metrics.Container != nil {
		metrics.Container.HA = ha
		enrichFirmwareBoards(metrics.Container, <-firmwareJobs, time.Now().UTC())
	}
	metrics.FirmwareVersion = d.info.FirmwareVersion
	if selected.state != nil {
		metrics.FirmwareVersion = selected.state.GetVersion()
	}
	return metrics, authFailure(d.id, err)
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

// GetErrors reports active board, PSU, and plant errors. Fleet closes an error
// that polls stop reporting, so an error is still reported, marked as
// retained, until its source confirms that it cleared. Fleet polls errors
// right after telemetry, so the outcome of a telemetry poll that just ended is
// reused, whether it read the container or failed to.
//
// A poll that cannot read the container confirms and clears nothing. Fleet
// records an unreachable container as offline and still closes the errors
// that polls stop refreshing, so the errors already held are reported, all
// marked as retained. With none held, which includes the first poll after a
// plugin restart, the poll fails: a failed read never returns an empty report.
func (d *Device) GetErrors(ctx context.Context) (sdk.DeviceErrors, error) {
	state := d.errorsShared()
	observed, err := state.lastPoll(time.Now(), errorEvidenceMaxAge)
	if err == nil && !observed {
		var evidence *errorEvidence
		if evidence, err = d.readErrorEvidence(ctx); err == nil {
			state.observe(evidence)
		}
	}
	if err == nil {
		return state.report(d.id), nil
	}
	// A poll the caller abandoned says nothing about the container.
	if ctx.Err() == nil {
		if report, held := state.carried(d.id); held {
			slog.WarnContext(ctx, "CT1 container could not be read; reporting the errors already held", "device_id", d.id, "errors", len(report.Errors), "error", err)
			return report, nil
		}
	}
	return sdk.DeviceErrors{}, err
}

// pollFailed records a telemetry poll that could not read the container, so
// that the error poll after it does not repeat the failing reads. A poll the
// caller abandoned says nothing about the container.
func (d *Device) pollFailed(ctx context.Context, err error) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	d.errorsShared().failed(time.Now(), err)
}

// errorsShared returns the container's error state, shared across handles
// like its firmware state.
func (d *Device) errorsShared() *errorState {
	d.errorsOnce.Do(func() {
		if d.errorState == nil {
			d.errorState = errorStateFor(firmwareContainerIdentity(d.info))
		}
	})
	return d.errorState
}

// readErrorEvidence follows the telemetry read policy: prefer the settled
// primary and retry once on this poll's verified standby.
func (d *Device) readErrorEvidence(ctx context.Context) (*errorEvidence, error) {
	selected, _, err := d.probeControllers(ctx, false)
	if err != nil {
		return nil, err
	}
	evidence, err := selected.client.collectErrorEvidence(ctx)
	if err != nil && selected.standby != nil && ctx.Err() == nil {
		selected = selected.standby
		firstErr := err
		if evidence, err = selected.client.collectErrorEvidence(ctx); err != nil {
			err = errors.Join(firstErr, err)
		}
	}
	return evidence.readFrom(selected), err
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
