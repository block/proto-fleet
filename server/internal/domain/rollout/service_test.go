package rollout

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

var (
	pilotOf1   = Behavior{Method: MethodPilotThenContinue, PilotSize: 1}
	batchesOf2 = Behavior{Method: MethodBatched, BatchSize: 2, ReviewAfterEachBatch: true}
)

func TestPilotRolloutGatesRestBehindReview(t *testing.T) {
	f := newFixture(t, 3)
	ctx := t.Context()
	f.channel(t, pilotOf1, f.allMiners()...)

	started := f.apply(t, "fw-2")
	assert.Equal(t, MethodPilotThenContinue, started.Behavior.Method)
	assert.Equal(t, StageBatch, started.Stage)
	assert.Equal(t, StateInProgress, started.State)
	assert.Equal(t, int32(1), started.Behavior.PilotSize)
	assert.Equal(t, int32(1), started.BatchCount)
	assert.Equal(t, int32(1), batchOf(started, "miner-0"), "equal efficiency: identifier order picks the pilot")
	assert.Equal(t, int32(0), batchOf(started, "miner-1"))
	assert.Equal(t, []string{EventRolloutStarted}, f.activity.types())

	// Batch stage: only the pilot is dispatched to, even across ticks.
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
	assert.Equal(t, PhaseInProgress, phaseOf(f.rollout(t, started.ID), "miner-0"))
	assert.Equal(t, PhaseQueued, phaseOf(f.rollout(t, started.ID), "miner-1"))

	// Reporting the version is not enough: the miner has to come back online.
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.setStatus(t, "miner-0", "OFFLINE")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageBatch, f.rollout(t, started.ID).Stage)
	assert.Equal(t, PhaseInProgress, phaseOf(f.rollout(t, started.ID), "miner-0"))

	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Equal(t, StatePausedAtPilotGate, gated.State)
	assert.Equal(t, PhaseDone, phaseOf(gated, "miner-0"))
	assert.Contains(t, f.activity.types(), EventRolloutReviewReady)
	require.NotNil(t, gated.Evidence)
	assert.Equal(t, int32(1), gated.Evidence.Verified)
	assert.Equal(t, "Manual review", gated.Evidence.HoldReason)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "gate holds: no dispatch to the rest")

	// Continue releases the gate; the rest is dispatched on the next tick.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, gated.ID)
	require.NoError(t, err)
	assert.Equal(t, StageRest, continued.Stage)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, f.dispatcher.sentIdentifiers()[1:])

	f.finishUpdate(t, "miner-1", "2.0.0")
	f.finishUpdate(t, "miner-2", "2.0.0")
	f.svc.EnforceTick(ctx)
	done := f.rollout(t, started.ID)
	assert.Equal(t, StatusCompleted, done.Status)
	assert.Equal(t, StateCompleted, done.State)
	assert.Contains(t, f.activity.types(), EventRolloutCompleted)
}

func TestBatchesGateAfterEveryBatch(t *testing.T) {
	f := newFixture(t, 4)
	ctx := t.Context()
	f.channel(t, batchesOf2, f.allMiners()...)

	started := f.apply(t, "fw-2")
	assert.Equal(t, int32(2), started.BatchCount)
	assert.Equal(t, int32(1), batchOf(started, "miner-1"))
	assert.Equal(t, int32(2), batchOf(started, "miner-2"))

	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.finishUpdate(t, "miner-1", "2.0.0")
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Equal(t, StatePausedAtBatchReview, gated.State)

	// Continuing after the first batch starts the second, not the rest.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, StageBatch, continued.Stage)
	assert.Equal(t, int32(1), continued.CurrentBatch)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-2", "miner-3"}, f.dispatcher.sentIdentifiers()[2:])
	f.finishUpdate(t, "miner-2", "2.0.0")
	f.finishUpdate(t, "miner-3", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	// Continuing after the last batch enters the rest stage, which has
	// nothing left to do and completes.
	continued, err = f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, StageRest, continued.Stage)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
}

func TestBatchesWithoutReviewWaitBetweenBatches(t *testing.T) {
	f := newFixture(t, 3)
	ctx := t.Context()
	f.channel(t, Behavior{Method: MethodBatched, BatchSize: 1, WaitBetweenBatchesSeconds: 120}, f.allMiners()...)

	started := f.apply(t, "fw-2")
	assert.Equal(t, int32(3), started.BatchCount)
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	waiting := f.rollout(t, started.ID)
	assert.Equal(t, StageWaiting, waiting.Stage, "no review: the batch parks in the wait instead of a gate")
	assert.Equal(t, StateInProgress, waiting.State)
	assert.Equal(t, "Waiting before the next batch", waiting.Evidence.HoldReason)

	f.advanceClock(60 * time.Second)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageWaiting, f.rollout(t, started.ID).Stage)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

	f.advanceClock(61 * time.Second)
	f.svc.EnforceTick(ctx)
	next := f.rollout(t, started.ID)
	assert.Equal(t, StageBatch, next.Stage)
	assert.Equal(t, int32(1), next.CurrentBatch)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
	assert.NotContains(t, f.activity.types(), EventRolloutReviewReady)
}

func TestAutoContinueWaitsForStabilizationAndHoldsOnDegradedHashrate(t *testing.T) {
	f := newFixture(t, 3)
	ctx := t.Context()
	f.channel(t, Behavior{
		Method: MethodBatched, BatchSize: 1, ReviewAfterEachBatch: true,
		AutoContinue: true, StabilizationSeconds: 60,
		Thresholds: Thresholds{MaxHashrateDropPercent: ptr(10.0)},
	}, f.allMiners()...)

	started := f.apply(t, "fw-2")
	assert.Equal(t, int32(3), started.BatchCount)

	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	require.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Equal(t, holdStabilizing, gated.Evidence.HoldReason)
	assert.Equal(t, StateStabilizingTelemetry, gated.State)
	// stage_changed_at is stamped by the database clock, which runs a hair
	// ahead of the frozen service clock.
	assert.InDelta(t, 60, gated.Evidence.StabilizationRemainingSeconds, 1)

	// Still within the stabilization window: holds even though healthy.
	f.advanceClock(30 * time.Second)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	// Window elapsed, hashrate steady: advances on its own to batch 2.
	f.advanceClock(31 * time.Second)
	f.svc.EnforceTick(ctx)
	advanced := f.rollout(t, started.ID)
	assert.Equal(t, StageBatch, advanced.Stage)
	assert.Equal(t, int32(1), advanced.CurrentBatch)
	var autoContinued bool
	for _, e := range f.activity.events {
		if e.Type == EventRolloutContinued && e.ActorType == activitymodels.ActorSystem {
			autoContinued = true
		}
	}
	assert.True(t, autoContinued, "auto-continue is recorded as a system continue")

	// Batch 2 comes back hashing at half its baseline: evidence is degraded,
	// so the gate holds for a human even after the stabilization window.
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-1", "2.0.0")
	f.reportHashrate(t, "miner-1", 50)
	f.svc.EnforceTick(ctx)
	f.advanceClock(2 * time.Minute)
	f.svc.EnforceTick(ctx)
	held := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, held.Stage)
	assert.Equal(t, StatePausedAtBatchReview, held.State)
	assert.Equal(t, int32(1), held.CurrentBatch)
	require.NotNil(t, held.Evidence)
	assert.InDelta(t, -50, held.Evidence.HashrateChangePercent, 0.01)
	assert.Contains(t, held.Evidence.HoldReason, "Hashrate down 50.0%")
	assert.False(t, held.Evidence.ReadyToAdvance)

	// A human can still continue past a degraded gate.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(2), continued.CurrentBatch)
}

func TestAutoContinueHoldsOnMissingEvidence(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, Behavior{
		Method: MethodPilotThenContinue, PilotSize: 1, AutoContinue: true,
		Thresholds: Thresholds{MaxHashrateDropPercent: ptr(10.0)},
	}, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	// Drop the miner's samples: it has a baseline hashrate (snapshotted on
	// the rollout) but no current one.
	_, err := f.conn.ExecContext(ctx, `DELETE FROM device_metrics WHERE device_identifier = 'miner-0'`)
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Contains(t, gated.Evidence.HoldReason, "No recent hashrate sample")
}

func TestAutoContinueChecksEveryThreshold(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.reportTelemetry(t, "miner-0", 100, 3000, 30, 60)
	f.channel(t, Behavior{
		Method: MethodPilotThenContinue, PilotSize: 1, AutoContinue: true,
		Thresholds: Thresholds{
			MaxEfficiencyIncreasePercent: ptr(5.0),
			MaxTempIncreaseC:             ptr(3.0),
			MaxNewErrors:                 ptr(int32(0)),
		},
	}, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")

	// Efficiency worse by 10%: holds.
	f.reportTelemetry(t, "miner-0", 100, 3300, 33, 60)
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	held := f.rollout(t, started.ID)
	require.Equal(t, StageAwaitingReview, held.Stage)
	assert.InDelta(t, 10, held.Evidence.EfficiencyChangePercent, 0.01)
	assert.Contains(t, held.Evidence.HoldReason, "Efficiency worse by 10.0%")

	// Efficiency fine, temperature up 5°C: holds.
	f.reportTelemetry(t, "miner-0", 100, 3000, 30, 65)
	f.svc.EnforceTick(ctx)
	held = f.rollout(t, started.ID)
	assert.InDelta(t, 5, held.Evidence.TemperatureChangeC, 0.01)
	assert.Contains(t, held.Evidence.HoldReason, "Temperature up 5.0°C")

	// Telemetry fine, but a new error opened: holds.
	f.reportTelemetry(t, "miner-0", 100, 3000, 30, 60)
	_, err := f.conn.ExecContext(ctx, `
		INSERT INTO errors (error_id, org_id, miner_error, severity, summary, first_seen_at, last_seen_at, device_id)
		VALUES ('00000000-0000-0000-0000-000000000001', $1, 1, 2, 'boom', now(), CURRENT_TIMESTAMP, $2)
	`, f.orgID, f.deviceIDs["miner-0"])
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	held = f.rollout(t, started.ID)
	assert.Equal(t, int32(1), held.Evidence.NewErrors)
	assert.Contains(t, held.Evidence.HoldReason, "1 new errors")

	// Error closed: nothing holds, auto-continue releases the gate.
	_, err = f.conn.ExecContext(ctx, `UPDATE errors SET closed_at = now() WHERE device_id = $1`, f.deviceIDs["miner-0"])
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageRest, f.rollout(t, started.ID).Stage)
}

func TestDoneIsRelativeToBaselineHealth(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	// miner-1 was not hashing before the update; miner-0 was.
	f.setStatus(t, "miner-1", "NEEDS_MINING_POOL")
	f.channel(t, allAtOnce, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.setStatus(t, "miner-0", "NEEDS_MINING_POOL")
	f.setReportedVersion(t, "miner-1", "2.0.0")
	f.svc.EnforceTick(ctx)

	r := f.rollout(t, started.ID)
	assert.Equal(t, StatusActive, r.Status, "miner-0 used to hash and does not yet: not done")
	assert.Equal(t, PhaseInProgress, phaseOf(r, "miner-0"))
	assert.Equal(t, PhaseDone, phaseOf(r, "miner-1"), "miner-1 is as healthy as before")

	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
}

func TestPauseHoldsEnforcementUntilResumed(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, allAtOnce, f.allMiners()...)

	started := f.apply(t, "fw-2")
	paused, err := f.svc.PauseRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	require.NotNil(t, paused.PausedAt)
	assert.Equal(t, StatePaused, paused.State)
	f.svc.EnforceTick(ctx)
	assert.Empty(t, f.dispatcher.sentIdentifiers(), "paused: nothing is sent")

	_, err = f.svc.PauseRollout(ctx, f.orgID, started.ID)
	assert.ErrorContains(t, err, "already paused")

	resumed, err := f.svc.ResumeRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Nil(t, resumed.PausedAt)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
	assert.Contains(t, f.activity.types(), EventRolloutPaused)
	assert.Contains(t, f.activity.types(), EventRolloutResumed)
}

func TestFailedMinersCompleteWithFailuresAndCanBeRetried(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, allAtOnce, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.finishUpdate(t, "miner-1", "2.0.0")

	// miner-0 never takes the update: three attempts, then failed.
	for attempt := int32(1); attempt <= MaxAttempts; attempt++ {
		f.svc.EnforceTick(ctx)
		assert.Equal(t, attempt, deviceOf(t, f.rollout(t, started.ID), "miner-0").Attempts)
		f.backdateSends(t)
	}
	assert.Equal(t, PhaseRetrying, phaseOf(f.rollout(t, started.ID), "miner-0"))
	f.svc.EnforceTick(ctx)
	finished := f.rollout(t, started.ID)
	assert.Equal(t, StatusCompletedWithFailures, finished.Status)
	assert.Equal(t, StateCompletedWithFailures, finished.State)
	assert.Equal(t, PhaseFailed, phaseOf(finished, "miner-0"))
	assert.Contains(t, deviceOf(t, finished, "miner-0").LastError, "after 3 update attempts")
	assert.Equal(t, PhaseDone, phaseOf(finished, "miner-1"))
	assert.Contains(t, f.activity.types(), EventRolloutCompletedWithFailures)
	sends := len(f.dispatcher.sentIdentifiers())

	// Drift correction leaves the failed miner alone.
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, finished.ID, f.latestRollout(t).ID, "no new rollout for a halted miner")
	assert.Equal(t, sends, len(f.dispatcher.sentIdentifiers()))

	// Retrying a finished rollout starts a fresh all-at-once rollout for it.
	retried, err := f.svc.RetryFailedDevices(ctx, f.orgID, 1, started.ID)
	require.NoError(t, err)
	assert.NotEqual(t, started.ID, retried.ID)
	assert.Equal(t, StatusActive, retried.Status)
	assert.Equal(t, MethodAllAtOnce, retried.Behavior.Method)
	require.Len(t, retried.Devices, 1)
	assert.Equal(t, "miner-0", retried.Devices[0].DeviceIdentifier)
	assert.Equal(t, PhaseFailed, phaseOf(f.rollout(t, started.ID), "miner-0"), "history keeps the failure")
	assert.Contains(t, f.activity.types(), EventRolloutRetried)

	f.svc.EnforceTick(ctx)
	assert.Equal(t, sends+1, len(f.dispatcher.sentIdentifiers()))
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, retried.ID).Status)
}

func TestRetryFailedInActiveRolloutRequeuesInPlace(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, pilotOf1, f.allMiners()...)

	started := f.apply(t, "fw-2")
	for range MaxAttempts {
		f.svc.EnforceTick(ctx)
		f.backdateSends(t)
	}
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	assert.Equal(t, PhaseFailed, phaseOf(gated, "miner-0"))
	assert.Equal(t, StageAwaitingReview, gated.Stage, "a failed pilot still settles the batch")
	assert.Equal(t, int32(1), gated.Evidence.Failed)

	retried, err := f.svc.RetryFailedDevices(ctx, f.orgID, 1, started.ID)
	require.NoError(t, err)
	assert.Equal(t, started.ID, retried.ID)
	assert.Equal(t, PhaseQueued, phaseOf(*retried, "miner-0"))
	assert.Equal(t, int32(0), deviceOf(t, *retried, "miner-0").Attempts)

	_, err = f.svc.RetryFailedDevices(ctx, f.orgID, 1, started.ID)
	assert.ErrorContains(t, err, "no failed miners")
}

func TestCancelRemainingKeepsUpdatedMinersAndIsNotRestarted(t *testing.T) {
	f := newFixture(t, 3)
	ctx := t.Context()
	f.channel(t, pilotOf1, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	canceled, channel, err := f.svc.CancelRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCanceled, canceled.Status)
	assert.Equal(t, StateCanceled, canceled.State)
	assert.Equal(t, CancelReasonCanceledRemaining, canceled.CancelReason)
	assert.Equal(t, PhaseDone, phaseOf(*canceled, "miner-0"), "the updated miner keeps the new firmware")
	assert.Equal(t, PhaseQueued, phaseOf(*canceled, "miner-1"))
	assert.Equal(t, "fw-2", f.assignedFirmware(t), "the assignment stays")
	assert.Equal(t, f.channelID, channel.ID)
	assert.Contains(t, f.activity.types(), EventRolloutCanceled)

	// The assignment still mismatches miner-1/2, but they were halted for
	// this version: drift correction must not restart the change.
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, started.ID, f.latestRollout(t).ID)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

	_, _, err = f.svc.CancelRollout(ctx, f.orgID, started.ID)
	assert.ErrorContains(t, err, "not active")

	// Re-assigning the same file is a no-op, so retry is the way back in.
	retried, err := f.svc.RetryFailedDevices(ctx, f.orgID, 1, started.ID)
	require.NoError(t, err)
	assert.NotEqual(t, started.ID, retried.ID)
	assert.Len(t, retried.Devices, 2)
}

func TestRollbackRestoresPreviousVersionAllAtOnce(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	ch := f.channel(t, allAtOnce, f.allMiners()...)

	// Settle the group on fw-1 first so there is a previous version.
	first := f.apply(t, "fw-1")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "1.5.0")
	f.finishUpdate(t, "miner-1", "1.5.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, StatusCompleted, f.rollout(t, first.ID).Status)

	// Switch the channel to a pilot so the second rollout parks at a gate.
	_, err := f.svc.UpdateChannel(ctx, f.orgID, ch.ID, ChannelSpec{Name: ch.Name, Scope: ch.Scope, Behavior: pilotOf1})
	require.NoError(t, err)
	second := f.apply(t, "fw-2")
	assert.Equal(t, "fw-1", second.PreviousFirmwareFileID)
	assert.Equal(t, "1.5.0", second.PreviousFirmwareVersion)
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, StageAwaitingReview, f.rollout(t, second.ID).Stage)

	// Rolling back to the first rollout's firmware cancels the second and
	// starts an all-at-once rollout back for the miner already on 2.0.0.
	var channelID int64
	var started []Rollout
	channelID, started, err = f.svc.RollbackFirmware(ctx, f.orgID, 1, first.ID)
	require.NoError(t, err)
	assert.Equal(t, f.channelID, channelID)
	assert.Equal(t, "fw-1", f.assignedFirmware(t))
	assert.Equal(t, CancelReasonRolledBack, f.rollout(t, second.ID).CancelReason)
	require.Len(t, started, 1)
	assert.Equal(t, "1.5.0", started[0].FirmwareVersion)
	assert.Equal(t, MethodAllAtOnce, started[0].Behavior.Method)
	assert.Equal(t, "fw-2", started[0].PreviousFirmwareFileID)
	require.Len(t, started[0].Devices, 1)
	assert.Equal(t, "miner-0", started[0].Devices[0].DeviceIdentifier)

	f.svc.EnforceTick(ctx)
	sent := f.dispatcher.sentIdentifiers()
	assert.Equal(t, "miner-0", sent[len(sent)-1])
}

func TestSupersededAndClearedRolloutsRecordTheReason(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	f.channel(t, allAtOnce, f.allMiners()...)

	first := f.apply(t, "fw-1")
	f.apply(t, "fw-2")
	assert.Equal(t, CancelReasonSuperseded, f.rollout(t, first.ID).CancelReason)

	_, err := f.svc.ApplyFirmware(ctx, f.orgID, 1, f.channelID, []Assignment{{Model: "Rig", FirmwareFileID: ""}})
	require.NoError(t, err)
	assert.Equal(t, CancelReasonCleared, f.latestRollout(t).CancelReason)
	assert.Equal(t, "", f.assignedFirmware(t))
}

func TestEvidenceAggregatesTelemetryAgainstBaseline(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.reportTelemetry(t, "miner-0", 100, 3000, 30, 60)
	f.reportTelemetry(t, "miner-1", 100, 3200, 32, 64)
	f.channel(t, allAtOnce, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.reportTelemetry(t, "miner-0", 110, 3300, 30, 66)

	r := f.rollout(t, started.ID)
	require.NotNil(t, r.Evidence)
	// Hashrate and power are summed, efficiency and temperature averaged,
	// over miners with both halves (miner-1 kept its samples, so both count).
	require.NotNil(t, r.Evidence.PowerW.Baseline)
	assert.InDelta(t, 200, *r.Evidence.HashRateHs.Baseline, 0.01)
	assert.InDelta(t, 210, *r.Evidence.HashRateHs.Current, 0.01)
	assert.InDelta(t, 5, r.Evidence.HashrateChangePercent, 0.01)
	assert.InDelta(t, 6200, *r.Evidence.PowerW.Baseline, 0.01)
	assert.InDelta(t, 6500, *r.Evidence.PowerW.Current, 0.01)
	assert.InDelta(t, 62, *r.Evidence.TempC.Baseline, 0.01)
	assert.InDelta(t, 65, *r.Evidence.TempC.Current, 0.01)
	assert.InDelta(t, 3, r.Evidence.TemperatureChangeC, 0.01)
	assert.InDelta(t, 31, *r.Evidence.EfficiencyJh.Current, 0.01)

	d := deviceOf(t, r, "miner-0")
	require.NotNil(t, d.PowerW.Baseline)
	assert.InDelta(t, 3000, *d.PowerW.Baseline, 0.01)
	assert.InDelta(t, 3300, *d.PowerW.Current, 0.01)
	assert.Equal(t, "10.0.0.1", d.IPAddress)
}

func TestLeastEfficientFirstAndRandomOrdering(t *testing.T) {
	f := newFixture(t, 4)
	// miner-2 is the least efficient, miner-3 has no sample and goes last.
	f.reportTelemetry(t, "miner-0", 100, 3000, 30, 60)
	f.reportTelemetry(t, "miner-1", 100, 3000, 25, 60)
	f.reportTelemetry(t, "miner-2", 100, 3000, 40, 60)
	f.channel(t, Behavior{Method: MethodBatched, BatchSize: 1, ReviewAfterEachBatch: true}, f.allMiners()...)

	started := f.apply(t, "fw-2")
	assert.Equal(t, int32(1), batchOf(started, "miner-2"))
	assert.Equal(t, int32(2), batchOf(started, "miner-0"))
	assert.Equal(t, int32(3), batchOf(started, "miner-1"))
	assert.Equal(t, int32(4), batchOf(started, "miner-3"))
	identifiers := make([]string, 0, len(started.Devices))
	for _, d := range started.Devices {
		identifiers = append(identifiers, d.DeviceIdentifier)
	}
	assert.Equal(t, []string{"miner-2", "miner-0", "miner-1", "miner-3"}, identifiers, "devices list in rollout order")

	// Random order: a reversing shuffle puts the identifier-sorted list backwards.
	f.svc.shuffle = func(n int, swap func(i, j int)) {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			swap(i, j)
		}
	}
	_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
		Name:     "Test channel",
		Scope:    Scope{DeviceIdentifiers: []string{"miner-0", "miner-1", "miner-2", "miner-3"}},
		Behavior: Behavior{Method: MethodBatched, BatchSize: 1, ReviewAfterEachBatch: true, Order: OrderRandom},
	})
	require.NoError(t, err)
	shuffled := f.apply(t, "fw-1")
	assert.Equal(t, int32(1), batchOf(shuffled, "miner-3"))
	assert.Equal(t, int32(4), batchOf(shuffled, "miner-0"))
}

func TestMaxConcurrentOfflineCapsDispatch(t *testing.T) {
	f := newFixture(t, 4)
	ctx := t.Context()
	f.channel(t, Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 2}, f.allMiners()...)

	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers(), "two in flight")
	f.svc.EnforceTick(ctx)
	assert.Len(t, f.dispatcher.sentIdentifiers(), 2, "ceiling reached: nothing more until one verifies")

	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0", "miner-1", "miner-2"}, f.dispatcher.sentIdentifiers())

	f.finishUpdate(t, "miner-1", "2.0.0")
	f.finishUpdate(t, "miner-2", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0", "miner-1", "miner-2", "miner-3"}, f.dispatcher.sentIdentifiers())
	f.finishUpdate(t, "miner-3", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
}

func TestScopesResolvePlacementAndRejectOverlap(t *testing.T) {
	f := newFixture(t, 5)
	ctx := t.Context()
	f.addMiner(t, "other-0", "Other")
	site := f.addSite(t, "Site A")
	building := f.addBuilding(t, site, "Building 1")
	rack := f.addRack(t, site, building, "Rack 1")
	group := f.addGroup(t, "Group X")
	f.placeInSet(t, rack, "rack", "miner-0")
	f.placeInSet(t, rack, "rack", "miner-1")
	f.placeInSet(t, group, "group", "miner-1")
	f.placeInSet(t, group, "group", "miner-2")
	f.placeAtSite(t, site, "miner-3")
	f.placeAtSite(t, site, "other-0")

	// Preview: a rack scope covers its two miners; a building scope covers
	// the rack's miners through rack placement; a site scope covers
	// devices placed at the site directly.
	preview, err := f.svc.PreviewScope(ctx, f.orgID, Scope{RackIDs: []int64{rack}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	assert.Equal(t, []ModelCount{{Model: "Rig", MinerCount: 2}}, preview.Models)
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{BuildingIDs: []int64{building}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{SiteIDs: []int64{site}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	assert.Equal(t, []ModelCount{{Model: "Other", MinerCount: 1}, {Model: "Rig", MinerCount: 1}}, preview.Models)

	rackChannel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Rack channel", Scope: Scope{RackIDs: []int64{rack}}})
	require.NoError(t, err)
	assert.Equal(t, int32(2), rackChannel.MinerCount)
	assert.Equal(t, []int64{rack}, rackChannel.Scope.RackIDs)

	// The group shares miner-1 with the rack: rejected, naming the channel.
	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Group channel", Scope: Scope{GroupIDs: []int64{group}}})
	require.ErrorContains(t, err, "overlaps release channel Rack channel (1 miners)")
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{GroupIDs: []int64{group}}, 0)
	require.NoError(t, err)
	assert.Equal(t, []ScopeConflict{{ChannelID: rackChannel.ID, ChannelName: "Rack channel", MinerCount: 1}}, preview.Conflicts)

	// Editing the rack channel itself does not conflict with itself.
	_, err = f.svc.UpdateChannel(ctx, f.orgID, rackChannel.ID, ChannelSpec{
		Name: "Rack channel", Scope: Scope{RackIDs: []int64{rack}, DeviceIdentifiers: []string{"miner-4"}},
	})
	require.NoError(t, err)
	_, err = f.svc.UpdateChannel(ctx, f.orgID, rackChannel.ID, ChannelSpec{
		Name: "Rack channel", Scope: Scope{DeviceIdentifiers: []string{"ghost-9"}},
	})
	assert.ErrorContains(t, err, `unknown miner "ghost-9"`)
	edited, err := f.svc.GetChannel(ctx, f.orgID, rackChannel.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-4"}, edited.Scope.DeviceIdentifiers, "identifiers round-trip through storage by id")

	// The site channel covers the miners placed at the site directly
	// (miner-3 and other-0); no overlap with the rack channel yet.
	siteChannel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Site channel", Scope: Scope{SiteIDs: []int64{site}}})
	require.NoError(t, err)
	assert.Equal(t, int32(2), siteChannel.MinerCount)

	// Membership is dynamic: a miner moved into the rack joins the rack
	// channel, and its active rollout picks it up as a late joiner. Because
	// miner-3 also sits at the site, this is a runtime overlap: the more
	// specific rack selector wins and the miner is flagged.
	f.channelID = rackChannel.ID
	started := f.apply(t, "fw-2")
	assert.Len(t, started.Devices, 3)
	f.placeInSet(t, rack, "rack", "miner-3")
	f.svc.EnforceTick(ctx)
	joined := f.rollout(t, started.ID)
	assert.Len(t, joined.Devices, 4)
	assert.Equal(t, int32(0), batchOf(joined, "miner-3"))
	assert.Equal(t, PhaseInProgress, phaseOf(joined, "miner-3"), "late joiners are dispatched to in the rest stage")

	channels, err := f.svc.ListChannels(ctx, f.orgID)
	require.NoError(t, err)
	byName := map[string]Channel{}
	for _, c := range channels {
		byName[c.Name] = c
	}
	assert.Equal(t, int32(4), byName["Rack channel"].MinerCount, "rack beats site")
	assert.Equal(t, int32(1), byName["Site channel"].MinerCount, "only other-0 remains at the site")
	var conflicted bool
	for _, g := range byName["Rack channel"].ModelGroups {
		for _, m := range g.Miners {
			if m.DeviceIdentifier == "miner-3" {
				conflicted = m.Conflicted
			}
		}
	}
	assert.True(t, conflicted)

	// A miner that leaves the rack is excluded, not counted, and does not
	// block completion.
	f.removeFromSet(t, rack, "miner-0")
	f.finishUpdate(t, "miner-1", "2.0.0")
	f.finishUpdate(t, "miner-3", "2.0.0")
	f.finishUpdate(t, "miner-4", "2.0.0")
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	done := f.rollout(t, started.ID)
	assert.Equal(t, PhaseExcluded, phaseOf(done, "miner-0"))
	assert.Equal(t, StatusCompleted, done.Status)
	ch, err := f.svc.GetChannel(ctx, f.orgID, rackChannel.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(3), ch.MinerCount)

	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Rack channel", Scope: Scope{}})
	assert.ErrorContains(t, err, "already exists")
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, siteChannel.ID))
	assert.ErrorContains(t, f.svc.DeleteChannel(ctx, f.orgID, siteChannel.ID), "not found")
}

func TestBehaviorValidation(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	create := func(b Behavior) error {
		_, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "v", Behavior: b})
		return err
	}
	assert.ErrorContains(t, create(Behavior{Method: MethodPilotThenContinue}), "pilot batch size")
	assert.ErrorContains(t, create(Behavior{Method: MethodBatched}), "batch size")
	assert.ErrorContains(t, create(Behavior{Method: "canary", BatchSize: 1}), "unknown rollout method")
	assert.ErrorContains(t, create(Behavior{Method: MethodBatched, BatchSize: 1, Order: "alphabetical"}), "unknown rollout order")
	assert.ErrorContains(t, create(Behavior{Method: MethodPilotThenContinue, PilotSize: 1, Thresholds: Thresholds{MaxHashrateDropPercent: ptr(150.0)}}), "between 0 and 100")
	assert.ErrorContains(t, create(Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: -1}), "must not be negative")

	_, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "  "})
	assert.ErrorContains(t, err, "name is required")

	// Irrelevant knobs are normalized away per method.
	ch, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "normalized", Behavior: Behavior{
		Method: MethodPilotThenContinue, PilotSize: 10, BatchSize: 3, WaitBetweenBatchesSeconds: 30,
		AutoContinue: true, StabilizationSeconds: 15, Thresholds: Thresholds{MaxNewErrors: ptr(int32(2))},
	}})
	require.NoError(t, err)
	assert.Equal(t, Behavior{
		Method: MethodPilotThenContinue, Order: OrderLeastEfficientFirst, PilotSize: 10, ReviewAfterEachBatch: true,
		AutoContinue: true, StabilizationSeconds: 15, Thresholds: Thresholds{MaxNewErrors: ptr(int32(2))},
	}, ch.Behavior)

	// A pilot larger than the mismatched set still starts.
	f.channelID = ch.ID
	_, err = f.svc.UpdateChannel(ctx, f.orgID, ch.ID, ChannelSpec{Name: "normalized", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}, Behavior: ch.Behavior})
	require.NoError(t, err)
	started := f.apply(t, "fw-2")
	assert.Equal(t, int32(1), started.BatchCount)
	assert.Equal(t, int32(1), batchOf(started, "miner-0"))
	_, err = f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	assert.ErrorContains(t, err, "not awaiting review")
}
