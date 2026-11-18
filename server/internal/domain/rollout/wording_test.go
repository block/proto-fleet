package rollout

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestNounForNamesContainersByTheirPair(t *testing.T) {
	t.Parallel()
	assert.Equal(t, containerNoun, nounFor("Proto", "CT1"))
	assert.Equal(t, containerNoun, nounFor(" proto ", "ct1"), "the pair folds like firmware targets")
	assert.Equal(t, minerNoun, nounFor("Proto", "Rig"))
	assert.Equal(t, minerNoun, nounFor("", "CT1"), "an unknown manufacturer is not a container")

	assert.Equal(t, "1 miner", minerNoun.count(1))
	assert.Equal(t, "0 miners", minerNoun.count(0))
	assert.Equal(t, "3 containers", containerNoun.count(3))
	assert.Equal(t, "1 device", anyDeviceNoun.count(1))
}

// A review gate's hold reason names the rollout's targets by what they are,
// with a singular noun for exactly one.
func TestReviewHoldReasonsNameTheRolloutsDevices(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := &Service{now: func() time.Time { return now }}
	at := sql.NullTime{Time: now, Valid: true}
	failed := target{ListFirmwareRolloutDevicesRow: sqlc.ListFirmwareRolloutDevicesRow{
		HaltedAt: at, HaltReason: HaltReasonFailed, Status: deviceStatusActive,
	}}
	offline := target{ListFirmwareRolloutDevicesRow: sqlc.ListFirmwareRolloutDevicesRow{VerifiedAt: at, Status: "OFFLINE"}}
	queued := target{ListFirmwareRolloutDevicesRow: sqlc.ListFirmwareRolloutDevicesRow{Status: deviceStatusActive}}
	rollout := func(manufacturer, model string) sqlc.FirmwareRollout {
		r := sqlc.FirmwareRollout{Status: StatusActive, Stage: StageAwaitingReview, Manufacturer: manufacturer, Model: model, StageChangedAt: now}
		r.BehaviorSnapshot.AutoContinue = true
		return r
	}
	rig, container := rollout("Proto", "Rig"), rollout("Proto", "CT1")

	for _, tc := range []struct {
		name            string
		scope           []target
		miners, devices string
	}{
		{"one failed", []target{failed}, "1 miner failed to update", "1 container failed to update"},
		{"several failed", []target{failed, failed}, "2 miners failed to update", "2 containers failed to update"},
		{"unverified", []target{queued, offline}, "1 of 2 miners not yet verified", "1 of 2 containers not yet verified"},
		{"offline", []target{offline}, "Waiting for 1 updated miner to come back online", "Waiting for 1 updated container to come back online"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.miners, s.evaluate(rig, tc.scope).HoldReason)
			assert.Equal(t, tc.devices, s.evaluate(container, tc.scope).HoldReason)
		})
	}

	assert.Equal(t, "Recent hashrate sample covers 1 of 3 verified containers",
		coverageHold("hashrate", Aggregate{SampledDevices: 1}, 3, containerNoun))
}
