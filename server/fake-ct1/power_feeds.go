package main

import (
	"context"
	"math"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/status"
)

// ListPowerFeeds reports one three-phase PDU outlet per module, the same
// pdu-N-outlet-MM feeds named by the container map and container telemetry.
// Mapping can disable outlets; standalone feed control is not simulated.
func (a *grpcAPI) ListPowerFeeds(ctx context.Context, _ *mdk.ListPowerFeedsRequest) (*mdk.ListPowerFeedsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	feeds := a.state.containerTelemetry().PowerFeeds
	out := &mdk.ListPowerFeedsResponse{SnapshotReady: new(true), PowerFeeds: make([]*mdk.PowerFeed, 0, len(feeds))}

	for _, feed := range feeds {
		enabled := mdk.PowerFeedState_POWER_FEED_STATE_ENABLED
		if !a.state.mappingFeedEnabled(feed.PowerFeedID) {
			enabled = mdk.PowerFeedState_POWER_FEED_STATE_DISABLED
		}
		current := 0.0
		if feed.VoltageV > 0 {
			current = feed.PowerW / (math.Sqrt(3) * feed.VoltageV)
		}
		out.PowerFeeds = append(out.PowerFeeds, &mdk.PowerFeed{
			PowerFeedId: feed.PowerFeedID, State: &enabled, Active: new(feed.Active), Faulted: new(feed.Faulted),
			VoltageV: new(feed.VoltageV), CurrentA: new(current), PowerW: new(feed.PowerW),
			VoltageAbV: new(feed.VoltageV), VoltageBcV: new(feed.VoltageV), VoltageCaV: new(feed.VoltageV),
			CurrentAA: new(current), CurrentBA: new(current), CurrentCA: new(current),
		})
	}
	return out, nil
}

// GetActivePowerRecoveryCycle reports no durable recovery cycle, because
// fake-ct1 does not simulate feed cycling.
func (a *grpcAPI) GetActivePowerRecoveryCycle(ctx context.Context, _ *mdk.GetActivePowerRecoveryCycleRequest) (*mdk.GetActivePowerRecoveryCycleResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	return &mdk.GetActivePowerRecoveryCycleResponse{}, nil
}
