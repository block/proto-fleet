package ct1

import (
	"context"
	"errors"
	"fmt"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const controllerProbeTimeout = 5 * time.Second

type controllerSelection struct {
	client *Client
	info   sdk.DeviceInfo
}

// Probe both open connections within one budget. Never send a mutation here.
// A selected connection owns the entire subsequent command, including readback.
func (d *Device) selectClient(ctx context.Context, write bool) (*Client, error) {
	selected, _, err := d.probeControllers(ctx, write)
	if err != nil {
		return nil, err
	}
	return selected.client, nil
}
func (d *Device) probeControllers(ctx context.Context, write bool) (*controllerSelection, *sdk.ContainerHA, error) {
	if len(d.info.Endpoints) == 0 {
		// Older or unconfigured servers cannot establish a healthy HA pair.
		return &controllerSelection{client: d.clients[0], info: d.info}, &sdk.ContainerHA{ObservedAt: time.Now().UTC(), Degraded: true}, nil
	}
	clients := d.clients
	type probeResult struct {
		selected *controllerSelection
		state    *mdk.SystemStatus
		report   *mdk.HAStatus
		err      error
	}
	results := make(chan probeResult, len(clients))
	probeTimeout := controllerProbeTimeout
	if deadline, ok := ctx.Deadline(); ok {
		// Reserve time for collection or command execution after selection.
		probeTimeout = min(probeTimeout, time.Until(deadline)/2)
	}
	probe, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	for _, c := range clients {
		go func(c *Client) {
			id := c.endpointID(d.info.Endpoints)
			report, _, err := c.readHA(probe, id, d.info.Endpoints[0].DeviceKey)
			if err != nil {
				results <- probeResult{err: err}
				return
			}
			response, err := c.system.GetSystemStatus(probe, &mdk.GetSystemStatusRequest{})
			state := response.GetStatus()
			if err == nil && state.GetServerId() != id {
				err = fmt.Errorf("CT1 status identity mismatch")
			}
			info := d.info
			info.FirmwareVersion = state.GetVersion()
			if info.SerialNumber == "" {
				info.SerialNumber = state.GetServerId()
			}
			results <- probeResult{selected: &controllerSelection{client: c, info: info}, state: state, report: report, err: err}
		}(c)
	}
	var primary, standby, readable, writable *controllerSelection
	var errs []error
	primaryCount := 0
	standbyCount := 0
	healthy := len(clients) == 2
	for range clients {
		var result probeResult
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("probe CT1 controllers: %w", ctx.Err())
		case result = <-results:
		}
		if result.err != nil {
			errs = append(errs, result.err)
			continue
		}
		readable = result.selected
		state := result.state
		if !healthyHAReport(result.report, state, d.info.Endpoints, time.Now()) {
			healthy = false
		}
		writePrimary := state.GetContainerWritePrimary()
		if writePrimary {
			primaryCount++
		}
		isPrimary := state.GetHaRole() == mdk.HARole_HA_ROLE_PRIMARY && writePrimary
		isStandby := state.GetHaRole() == mdk.HARole_HA_ROLE_STANDBY && !writePrimary
		// Frigate authorizes writes independently of recovery health. Keep the
		// strict health table for HA reporting and read preference only.
		if isPrimary {
			writable = result.selected
		}
		if !settledController(state) {
			healthy = false
			continue
		}
		if isPrimary {
			primary = result.selected
		} else if isStandby {
			standby = result.selected
			standbyCount++
		} else {
			healthy = false
		}
	}
	ha := &sdk.ContainerHA{ObservedAt: time.Now().UTC(), Degraded: !healthy || len(errs) != 0 || primaryCount != 1 || standbyCount != 1}
	if write {
		if primaryCount > 1 {
			return nil, ha, fmt.Errorf("both CT1 controllers report write authority")
		}
		if writable != nil {
			return writable, ha, nil
		}
	} else {
		if primary != nil {
			return primary, ha, nil
		}
		if standby != nil {
			return standby, ha, nil
		}
		if readable != nil {
			return readable, ha, nil
		}
	}
	errs = append(errs, errors.New("no eligible controller"))
	return nil, ha, fmt.Errorf("no CT1 controller available for this operation: %w", errors.Join(errs...))
}

func (c *Client) endpointID(controllers []sdk.DeviceEndpoint) string {
	if c.memberID != "" {
		return c.memberID
	}
	for _, controller := range controllers {
		if controller.Host == c.endpoint.Host && effectiveRPCPort(controller.RPCPort) == effectiveRPCPort(c.endpoint.RPCPort) {
			return controller.ID
		}
	}
	return ""
}

// Missing or unknown recovery values do not establish a settled pair.
func settledController(state *mdk.SystemStatus) bool {
	if !state.GetDatabaseConnected() {
		return false
	}
	if state.GetHaRecovery().GetState() != mdk.HARecoveryState_HA_RECOVERY_STATE_STEADY {
		return false
	}
	return state.DcsHealthy == nil || state.GetDcsHealthy()
}
