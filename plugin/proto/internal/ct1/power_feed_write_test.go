package ct1

import (
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const writeFeed = "pdu-1-outlet-01"

func writableFeedServer(state mdk.PowerFeedState, overridden bool) *powerFeedServer {
	return &powerFeedServer{
		containerMap: wireFeedMap(writeFeed),
		feeds: &mdk.ListPowerFeedsResponse{SnapshotReady: new(true), PowerFeeds: []*mdk.PowerFeed{{
			PowerFeedId: writeFeed, State: &state, Faulted: new(false), Overridden: overridden,
		}}},
		primary: true,
	}
}

func feedWrite(action string) sdk.ContainerPowerFeedRequest {
	return sdk.ContainerPowerFeedRequest{Action: action, Module: feedTarget, FeedID: writeFeed}
}

func requireFeedWriteOutcome(t *testing.T, err error, outcome string, reason sdk.WriteReason) {
	t.Helper()
	require.Error(t, err)
	evidence := sdk.WriteEvidence(err)
	require.NotNil(t, evidence, "write errors carry outcome evidence: %v", err)
	require.Equal(t, outcome, evidence.GetReason(), "%v", err)
	require.Equal(t, string(reason), evidence.GetMetadata()["reason"], "%v", err)
}

func TestContainerPowerFeedWritesOnceWithAcknowledgement(t *testing.T) {
	enabled, disabled := mdk.PowerFeedState_POWER_FEED_STATE_ENABLED, mdk.PowerFeedState_POWER_FEED_STATE_DISABLED
	for name, tc := range map[string]struct {
		action     string
		state      mdk.PowerFeedState
		overridden bool
		written    string
		applied    string
	}{
		"turn on":        {sdk.ContainerPowerFeedTurnOn, disabled, false, "state pdu-1-outlet-01 disabled=false", sdk.PowerFeedStateEnabled},
		"turn off":       {sdk.ContainerPowerFeedTurnOff, enabled, false, "state pdu-1-outlet-01 disabled=true", sdk.PowerFeedStateDisabled},
		"cycle":          {sdk.ContainerPowerFeedCycle, enabled, false, "cycle pdu-1-outlet-01", sdk.PowerFeedStateEnabled},
		"clear override": {sdk.ContainerPowerFeedClearOverride, disabled, true, "clear pdu-1-outlet-01", sdk.PowerFeedStateEnabled},
	} {
		t.Run(name, func(t *testing.T) {
			s := writableFeedServer(tc.state, tc.overridden)
			got, err := newPowerFeedDevice(t, s).ContainerPowerFeed(t.Context(), feedWrite(tc.action))
			require.NoError(t, err)
			require.Equal(t, []string{tc.written}, s.written)
			require.Equal(t, writeFeed, got.Applied.ID)
			require.Equal(t, tc.applied, got.Applied.State)
			require.Nil(t, got.Inspection)
		})
	}
}

func TestContainerPowerFeedWriteRefusesBeforeWriting(t *testing.T) {
	enabled, disabled := mdk.PowerFeedState_POWER_FEED_STATE_ENABLED, mdk.PowerFeedState_POWER_FEED_STATE_DISABLED
	for name, tc := range map[string]struct {
		action string
		setup  func(*powerFeedServer)
		reason sdk.WriteReason
	}{
		"feed changed": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) { s.containerMap = wireFeedMap("pdu-1-outlet-02") }, sdk.WriteReasonStaleBinding},
		"mapping not unique": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) {
			s.containerMap.ContainerMap.Tanks[0].Modules[0].Slots[1].PowerFeedId = "pdu-1-outlet-02"
		}, sdk.WriteReasonStaleBinding},
		"module identity changed": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) {
			for _, slot := range s.containerMap.ContainerMap.Tanks[0].Modules[0].Slots {
				if slot.Hashboard != nil {
					slot.Hashboard.ModuleId = "module-b"
				}
			}
		}, sdk.WriteReasonStaleBinding},
		"map omits a module that could share the feed": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) {
			tank := s.containerMap.ContainerMap.Tanks[3]
			tank.Modules = tank.Modules[1:]
		}, sdk.WriteReasonStaleBinding},
		"map omits a tank": {sdk.ContainerPowerFeedCycle, func(s *powerFeedServer) {
			s.containerMap.ContainerMap.Tanks = s.containerMap.ContainerMap.Tanks[:2]
		}, sdk.WriteReasonStaleBinding},
		"recovery cycle pending": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) { s.cycle = &mdk.PowerRecoveryCycle{Id: "cycle-1"} }, sdk.WriteReasonRejected},
		"recovery cycle unknown": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) { s.cycleErr = status.Error(codes.Unavailable, "down") }, sdk.WriteReasonReadFailed},
		"reading unavailable":    {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) { s.feeds.SnapshotReady = new(false) }, sdk.WriteReasonReadFailed},
		"already off":            {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) { s.feeds.PowerFeeds[0].State = &disabled }, sdk.WriteReasonStaleBinding},
		"already on":             {sdk.ContainerPowerFeedTurnOn, func(s *powerFeedServer) { s.feeds.PowerFeeds[0].State = &enabled }, sdk.WriteReasonStaleBinding},
		"cycle faulted":          {sdk.ContainerPowerFeedCycle, func(s *powerFeedServer) { s.feeds.PowerFeeds[0].Faulted = new(true) }, sdk.WriteReasonStaleBinding},
		"cycle fault unknown":    {sdk.ContainerPowerFeedCycle, func(s *powerFeedServer) { s.feeds.PowerFeeds[0].Faulted = nil }, sdk.WriteReasonStaleBinding},
		"no override":            {sdk.ContainerPowerFeedClearOverride, func(*powerFeedServer) {}, sdk.WriteReasonStaleBinding},
		"controller refused": {sdk.ContainerPowerFeedTurnOff, func(s *powerFeedServer) {
			s.writeErr = status.Error(codes.FailedPrecondition, "a feed-mapping run is active")
		}, sdk.WriteReasonRejected},
	} {
		t.Run(name, func(t *testing.T) {
			s := writableFeedServer(enabled, false)
			tc.setup(s)
			_, err := newPowerFeedDevice(t, s).ContainerPowerFeed(t.Context(), feedWrite(tc.action))
			requireFeedWriteOutcome(t, err, sdk.WriteNotDispatched, tc.reason)
			require.Equal(t, codes.FailedPrecondition, status.Code(err), "refusals reach Fleet as operator-facing text: %v", err)
			if s.writeErr != nil {
				require.Equal(t, "controller refused to turn off power feed "+writeFeed+": a feed-mapping run is active", status.Convert(err).Message())
			}
			if tc.reason != sdk.WriteReasonRejected || s.writeErr == nil {
				require.Empty(t, s.written, "refusals happen before any write")
			}
		})
	}
}

func TestContainerPowerFeedWriteReportsUnknownOutcomes(t *testing.T) {
	enabled := mdk.PowerFeedState_POWER_FEED_STATE_ENABLED
	for name, tc := range map[string]struct {
		action string
		err    error
	}{
		"lost response":   {sdk.ContainerPowerFeedTurnOff, status.Error(codes.Unavailable, "connection reset")},
		"backend error":   {sdk.ContainerPowerFeedTurnOff, status.Error(codes.Internal, "setting power feed failed")},
		"committed cycle": {sdk.ContainerPowerFeedCycle, status.Error(codes.DeadlineExceeded, "power cycle was committed")},
	} {
		t.Run(name, func(t *testing.T) {
			s := writableFeedServer(enabled, false)
			s.writeErr = tc.err
			_, err := newPowerFeedDevice(t, s).ContainerPowerFeed(t.Context(), feedWrite(tc.action))
			require.Equal(t, sdk.WriteUnknown, sdk.WriteEvidence(err).GetReason(), "%v", err)
			require.Len(t, s.written, 1, "an ambiguous write is never retried")
		})
	}
}

// A write the controller accepted without a usable acknowledgement may have
// run, so it is unknown rather than refused, and is never sent again.
func TestContainerPowerFeedWriteWithoutUsableAcknowledgementIsUnknown(t *testing.T) {
	for name, override := range map[string]func(*mdk.PowerFeed) *mdk.PowerFeed{
		"no acknowledgement": func(*mdk.PowerFeed) *mdk.PowerFeed { return nil },
		"another feed":       func(f *mdk.PowerFeed) *mdk.PowerFeed { f.PowerFeedId = "pdu-1-outlet-02"; return f },
		"state not changed": func(f *mdk.PowerFeed) *mdk.PowerFeed {
			f.State = new(mdk.PowerFeedState_POWER_FEED_STATE_ENABLED)
			return f
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := writableFeedServer(mdk.PowerFeedState_POWER_FEED_STATE_ENABLED, false)
			s.ackOverride = override
			got, err := newPowerFeedDevice(t, s).ContainerPowerFeed(t.Context(), feedWrite(sdk.ContainerPowerFeedTurnOff))
			requireFeedWriteOutcome(t, err, sdk.WriteUnknown, sdk.WriteReasonUnknown)
			require.ErrorContains(t, err, "outcome unconfirmed")
			require.Nil(t, got.Applied)
			require.Len(t, s.written, 1)
		})
	}
}

func TestContainerPowerFeedWriteRequiresConfirmedTarget(t *testing.T) {
	s := writableFeedServer(mdk.PowerFeedState_POWER_FEED_STATE_ENABLED, false)
	dev := newPowerFeedDevice(t, s)
	for name, req := range map[string]sdk.ContainerPowerFeedRequest{
		"no feed":           {Action: sdk.ContainerPowerFeedTurnOff, Module: feedTarget},
		"unconfirmed":       {Action: sdk.ContainerPowerFeedTurnOff, Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1}, FeedID: writeFeed},
		"inspect with feed": {Action: sdk.ContainerPowerFeedInspect, Module: feedTarget, FeedID: writeFeed},
	} {
		_, err := dev.ContainerPowerFeed(t.Context(), req)
		requireFeedWriteOutcome(t, err, sdk.WriteNotDispatched, sdk.WriteReasonInvalidRequest)
		require.Empty(t, s.written, name)
	}
}
