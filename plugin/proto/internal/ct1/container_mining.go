package ct1

import (
	"context"
	"fmt"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/status"
)

func (d *Device) ContainerMining(ctx context.Context, req sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	var result sdk.ContainerMiningResult
	if err := req.Validate(); err != nil {
		return result, sdk.InvalidWriteRequest(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !req.ReadOnly() {
		d.writeMu.Lock()
		defer d.writeMu.Unlock()
	}
	c, err := d.selectClient(ctx, !req.ReadOnly())
	if err != nil {
		return result, sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	// HA selection verifies the group and controller. Standalone reads and writes
	// also verify live identity before sending an operation to a saved address.
	if len(d.info.Endpoints) == 0 {
		if strings.TrimSpace(d.info.SerialNumber) == "" {
			return result, sdk.WriteRefused(fmt.Errorf("container identity unavailable"), sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
		}
		if _, err := c.Discover(ctx, d.info); err != nil {
			reason := sdk.WriteReasonStaleBinding
			if _, rpcError := status.FromError(err); rpcError {
				reason = sdk.WriteReasonReadFailed
			}
			return result, sdk.WriteRefused(fmt.Errorf("container identity unavailable: %w", err), sdk.WriteStageConfigurationRead, reason)
		}
	}
	switch req.Action {
	case "status":
		value, err := c.mining.GetMiningStatus(ctx, &mdk.GetMiningStatusRequest{})
		if err != nil {
			return result, err
		}
		result.Status = &sdk.ContainerMiningStatus{Mining: new(value.Mining), OperatingState: value.OperatingState.String()}
	case "power_target":
		value, err := c.mining.GetTargetPower(ctx, &mdk.GetTargetPowerRequest{})
		if err != nil {
			return result, err
		}
		result.Power = &sdk.ContainerPowerState{Watts: new(value.Watts), PerformanceMode: value.PerformanceMode.String(), BalanceBays: value.BalanceBays, HashOnDisconnect: value.HashOnDisconnect, MinimumWatts: value.MinimumWatts, MaximumWatts: value.MaximumWatts}
	case "set_mining":
		// These global MDK methods preserve default-group and isolated-board behavior.
		if *req.Mining {
			_, err = c.mining.StartMining(ctx, &mdk.StartMiningRequest{})
		} else {
			_, err = c.mining.StopMining(ctx, &mdk.StopMiningRequest{})
		}
		if err != nil {
			return result, err
		}
	case "set_power_target":
		p := req.Power
		_, err = c.mining.SetTargetPower(ctx, &mdk.SetTargetPowerRequest{Watts: p.Watts, PerformanceMode: mdk.PerformanceMode(mdk.PerformanceMode_value[p.PerformanceMode]), BalanceBays: p.BalanceBays, HashOnDisconnect: p.HashOnDisconnect})
		if err != nil {
			return result, err
		}
	}
	// No mutation retries or controller changes after dispatch, even on timeout.
	return result, result.Validate(req)
}
