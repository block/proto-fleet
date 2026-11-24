package ct1

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The precondition reads and the single write have separate budgets, so slow
// reads refuse the write instead of leaving it too little time to answer.
const (
	powerFeedPreconditionTimeout = 10 * time.Second
	powerFeedWriteTimeout        = 15 * time.Second
)

// writePowerFeed turns a module's feed on or off, cycles it, or clears its
// operator override. Under the controller write lock it re-derives the
// mapping from a write-primary controller and refuses unless the confirmed
// feed is still the module's unique feed, its reading is current, no recovery
// cycle is pending and the feed is in the state the action changes. It writes
// once and never retries; an ambiguous write is reported as unknown.
func (d *Device) writePowerFeed(ctx context.Context, req sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	var result sdk.ContainerPowerFeedResult
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	readCtx, cancelReads := context.WithTimeout(ctx, powerFeedPreconditionTimeout)
	defer cancelReads()
	c, err := d.verifiedWriteClient(readCtx)
	if err != nil {
		return result, err
	}
	feed, err := powerFeedWriteTarget(readCtx, c, req)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, powerFeedWriteTimeout)
	defer cancel()
	if err := powerFeedActionApplies(req.Action, feed); err != nil {
		return result, powerFeedRefused(sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding, "%s", status.Convert(err).Message())
	}
	var applied *mdk.PowerFeed
	switch req.Action {
	case sdk.ContainerPowerFeedTurnOn, sdk.ContainerPowerFeedTurnOff:
		var response *mdk.SetPowerFeedStateResponse
		response, err = c.power.SetPowerFeedState(ctx, &mdk.SetPowerFeedStateRequest{PowerFeedId: req.FeedID, Disabled: req.Action == sdk.ContainerPowerFeedTurnOff})
		applied = response.GetPowerFeed()
	case sdk.ContainerPowerFeedCycle:
		var response *mdk.CyclePowerFeedResponse
		response, err = c.power.CyclePowerFeed(ctx, &mdk.CyclePowerFeedRequest{PowerFeedId: req.FeedID})
		applied = response.GetPowerFeed()
	case sdk.ContainerPowerFeedClearOverride:
		var response *mdk.ClearPowerFeedOverrideResponse
		response, err = c.power.ClearPowerFeedOverride(ctx, &mdk.ClearPowerFeedOverrideRequest{PowerFeedId: req.FeedID})
		applied = response.GetPowerFeed()
	}
	if err != nil {
		return result, powerFeedWriteError(req, err)
	}
	if applied == nil || applied.GetPowerFeedId() != req.FeedID {
		return result, sdk.WithWriteOutcome(fmt.Errorf("controller did not acknowledge feed %s; outcome unconfirmed", req.FeedID), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonUnknown)
	}
	result.Applied = powerFeedReading(applied, req.FeedID)
	if err := result.Validate(req); err != nil {
		return sdk.ContainerPowerFeedResult{}, sdk.WithWriteOutcome(fmt.Errorf("%w; outcome unconfirmed", err), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonUnknown)
	}
	return result, nil
}

// powerFeedWriteTarget returns the confirmed feed's current reading when it
// is still the module's unique feed and no recovery cycle is pending.
func powerFeedWriteTarget(ctx context.Context, c *Client, req sdk.ContainerPowerFeedRequest) (*sdk.PowerFeedReading, error) {
	refused := func(reason sdk.WriteReason, format string, args ...any) error {
		return powerFeedRefused(sdk.WriteStageConfigurationRead, reason, format, args...)
	}
	var (
		wg       sync.WaitGroup
		feeds    *mdk.ListPowerFeedsResponse
		feedsErr error
		cycle    *mdk.GetActivePowerRecoveryCycleResponse
		cycleErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); feeds, feedsErr = c.power.ListPowerFeeds(ctx, &mdk.ListPowerFeedsRequest{}) }()
	go func() {
		defer wg.Done()
		cycle, cycleErr = c.power.GetActivePowerRecoveryCycle(ctx, &mdk.GetActivePowerRecoveryCycleRequest{})
	}()
	mapWire, mapErr := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	wg.Wait()
	if mapErr != nil {
		return nil, refused(sdk.WriteReasonReadFailed, "read container map: %v", mapErr)
	}
	mapping := moduleFeedMapping(mapResponse(mapWire), req.Module)
	if mapping.Confidence != sdk.PowerFeedMappingWiringVerified && mapping.Confidence != sdk.PowerFeedMappingConfigured {
		return nil, refused(sdk.WriteReasonStaleBinding, "tank %d module %d has no unique power feed", req.Module.Tank, req.Module.Module)
	}
	if mapping.FeedID != req.FeedID {
		return nil, refused(sdk.WriteReasonStaleBinding, "tank %d module %d is now supplied by %s, not %s", req.Module.Tank, req.Module.Module, mapping.FeedID, req.FeedID)
	}
	if cycleErr != nil {
		return nil, refused(sdk.WriteReasonReadFailed, "power recovery state could not be read: %v", cycleErr)
	}
	if active := recoveryCycle(cycle.GetCycle()); active != nil {
		return nil, refused(sdk.WriteReasonRejected, "power recovery cycle %s is in progress", active.ID)
	}
	if feedsErr != nil {
		return nil, refused(sdk.WriteReasonReadFailed, "read power feeds: %v", feedsErr)
	}
	feed, reason := feedReading(feeds, req.FeedID)
	if feed == nil {
		return nil, refused(sdk.WriteReasonReadFailed, "power feed %s has no current reading (%s)", req.FeedID, reason)
	}
	return feed, nil
}

// powerFeedActionApplies requires the state the operator meant to change, so
// a write never acts on a feed that changed after confirmation.
func powerFeedActionApplies(action string, feed *sdk.PowerFeedReading) error {
	if slices.Contains(sdk.PowerFeedWritesFor(feed), action) {
		return nil
	}
	switch action {
	case sdk.ContainerPowerFeedTurnOn:
		return fmt.Errorf("power feed %s is not off", feed.ID)
	case sdk.ContainerPowerFeedTurnOff:
		return fmt.Errorf("power feed %s is not on", feed.ID)
	case sdk.ContainerPowerFeedCycle:
		return fmt.Errorf("power feed %s must be on and not faulted to cycle", feed.ID)
	case sdk.ContainerPowerFeedClearOverride:
		return fmt.Errorf("power feed %s has no operator override", feed.ID)
	}
	return fmt.Errorf("unsupported power feed action")
}

// powerFeedRefused reports a write that was never sent. FailedPrecondition
// carries the reason across the plugin boundary as operator-facing text.
func powerFeedRefused(stage sdk.WriteStage, reason sdk.WriteReason, format string, args ...any) error {
	return sdk.WriteRefused(status.Errorf(codes.FailedPrecondition, format, args...), stage, reason)
}

// powerFeedActionPhrases names each write in operator-facing text.
var powerFeedActionPhrases = map[string]string{
	sdk.ContainerPowerFeedTurnOn:        "turn on",
	sdk.ContainerPowerFeedTurnOff:       "turn off",
	sdk.ContainerPowerFeedCycle:         "cycle",
	sdk.ContainerPowerFeedClearOverride: "clear the override on",
}

// powerFeedWriteError keeps controller refusals distinct from writes whose
// outcome is unknown. A committed cycle stays unknown here: the controller
// still owns its durable restore.
func powerFeedWriteError(req sdk.ContainerPowerFeedRequest, err error) error {
	phrase := powerFeedActionPhrases[req.Action]
	if firmwareStartRefused(err) {
		return powerFeedRefused(sdk.WriteStageDispatch, sdk.WriteReasonRejected, "controller refused to %s power feed %s: %s", phrase, req.FeedID, status.Convert(err).Message())
	}
	if req.Action == sdk.ContainerPowerFeedCycle && status.Code(err) == codes.DeadlineExceeded {
		return sdk.WithWriteOutcome(fmt.Errorf("power feed cycle was committed and the controller owns its restore; outcome unconfirmed: %w", err), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonUnknown)
	}
	return sdk.WithWriteOutcome(fmt.Errorf("%s power feed %s: outcome unconfirmed: %w", phrase, req.FeedID, err), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
}
