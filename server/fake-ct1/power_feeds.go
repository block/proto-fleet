package main

import (
	"context"
	"math"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListPowerFeeds reports one three-phase PDU outlet per module, the same
// pdu-N-outlet-MM feeds named by the container map and container telemetry.
// Mapping and SetPowerFeedState can each disable outlets.
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
		if feed.State == powerFeedStateDisabled || !a.state.mappingFeedEnabled(feed.PowerFeedID) {
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

// setPowerFeedDisabled switches one outlet and returns its resulting reading.
// The caller holds the state lock.
func (s *ContainerState) setPowerFeedDisabled(id string, disabled bool) (powerFeed, bool) {
	previous, had := s.DisabledPowerFeeds[id]
	s.DisabledPowerFeeds[id] = disabled
	for _, feed := range s.containerTelemetry().PowerFeeds {
		if feed.PowerFeedID == id {
			return feed, true
		}
	}
	if had {
		s.DisabledPowerFeeds[id] = previous
	} else {
		delete(s.DisabledPowerFeeds, id)
	}
	return powerFeed{}, false
}

// SetPowerFeedState switches one PDU outlet. Board mining state is unchanged.
func (a *grpcAPI) SetPowerFeedState(ctx context.Context, in *mdk.SetPowerFeedStateRequest) (*mdk.SetPowerFeedStateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	feed, ok := a.state.setPowerFeedDisabled(in.GetPowerFeedId(), in.GetDisabled())
	if !ok {
		return nil, status.Error(codes.NotFound, "unknown power feed")
	}
	out := new(mdk.SetPowerFeedStateResponse)
	if err := projectProto(setPowerFeedStateResponse{PowerFeed: feed}, out); err != nil {
		return nil, err
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
