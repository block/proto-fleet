package rollout

import (
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

func TestReturnedBehaviorWithoutThresholdsCanBeSaved(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		behavior rollout.Behavior
	}{
		{"unspecified defaults", rollout.Behavior{}},
		{"all at once", rollout.Behavior{Method: rollout.MethodAllAtOnce}},
		{"batched without review", rollout.Behavior{Method: rollout.MethodBatched, BatchSize: 5, WaitBetweenBatchesSeconds: 30}},
		{"batched with manual review", rollout.Behavior{Method: rollout.MethodBatched, BatchSize: 5, ReviewAfterEachBatch: true}},
		{"pilot with manual review", rollout.Behavior{Method: rollout.MethodPilotThenContinue, PilotSize: 1}},
		{"auto continue without metric limits", rollout.Behavior{Method: rollout.MethodPilotThenContinue, PilotSize: 1, AutoContinue: true, StabilizationSeconds: 30}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			channel := channelToProto(&rollout.Channel{ID: 1, Name: "Canary", Behavior: tc.behavior})
			require.Nil(t, channel.Behavior.Thresholds)
			// Clients reuse the behavior returned by reads when saving unrelated
			// channel edits. An empty thresholds message would reject this save.
			require.NoError(t, protovalidate.Validate(&pb.UpdateReleaseChannelRequest{
				ChannelId: channel.Id, Name: "Renamed channel", Behavior: channel.Behavior,
			}))
			assert.Equal(t, tc.behavior, behaviorFromProto(channel.Behavior))
		})
	}
}

func TestReturnedBehaviorPreservesConfiguredThresholdPresence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		thresholds rollout.Thresholds
	}{
		{"zero hashrate drop", rollout.Thresholds{MaxHashrateDropPercent: ptr(0.0)}},
		{"zero efficiency increase", rollout.Thresholds{MaxEfficiencyIncreasePercent: ptr(0.0)}},
		{"zero temperature increase", rollout.Thresholds{MaxTempIncreaseC: ptr(0.0)}},
		{"zero new errors", rollout.Thresholds{MaxNewErrors: ptr(int32(0))}},
		{"all limits and coverage", rollout.Thresholds{
			MaxHashrateDropPercent: ptr(10.0), MaxEfficiencyIncreasePercent: ptr(5.0),
			MaxTempIncreaseC: ptr(3.0), MaxNewErrors: ptr(int32(2)), MinSampleCoveragePercent: ptr(80.0),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			behavior := rollout.Behavior{
				Method: rollout.MethodPilotThenContinue, PilotSize: 1,
				AutoContinue: true, Thresholds: tc.thresholds,
			}
			channel := channelToProto(&rollout.Channel{ID: 1, Name: "Canary", Behavior: behavior})
			require.NotNil(t, channel.Behavior.Thresholds)
			require.NoError(t, protovalidate.Validate(&pb.UpdateReleaseChannelRequest{
				ChannelId: channel.Id, Name: channel.Name, Behavior: channel.Behavior,
			}))
			assert.Equal(t, behavior, behaviorFromProto(channel.Behavior), "explicit zero limits remain set; omitted limits remain nil")
		})
	}
}
