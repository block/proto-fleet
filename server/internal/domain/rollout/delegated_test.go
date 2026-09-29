package rollout

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"

	"github.com/stretchr/testify/require"
)

func delegated() Behavior { return Behavior{Method: MethodDelegated, Order: OrderLeastEfficientFirst} }

func requireReason(t *testing.T, err error, expected string) {
	t.Helper()
	require.Error(t, err)
	info, ok := ReasonOf(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, expected, info.Reason)
}

func TestDelegatedDispatchOnlyOnAdvanceAndSnapshotDoesNotGrow(t *testing.T) {
	f := newFixture(t, 3)
	f.channel(t, delegated(), f.allMiners()...)
	r := f.apply(t, "fw-2")
	require.Equal(t, StateWaitingForController, r.State)
	f.svc.EnforceTick(t.Context())
	require.Empty(t, f.dispatcher.sent)
	f.addMiner(t, "late-miner", "Rig")
	_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-0", "miner-1", "miner-2", "late-miner"}}, Behavior: delegated()})
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	require.Len(t, f.rollout(t, r.ID).Devices, 3)
	r = f.rollout(t, r.ID)
	first := r.Devices[0].DeviceIdentifier
	advanced, sent, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, Mutation{Actor: testActor, ExpectedRevision: r.Revision, Note: "first canary"})
	require.NoError(t, err)
	require.Equal(t, []string{first}, sent)
	require.Equal(t, r.Revision+1, advanced.Revision)
	require.Equal(t, StateInProgress, advanced.State)
	f.svc.EnforceTick(t.Context())
	require.Len(t, f.dispatcher.sent, 1)
	_, _, err = f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, Mutation{Actor: testActor, ExpectedRevision: r.Revision})
	requireReason(t, err, ReasonStaleRevision)
	_, err = f.svc.CompleteRollout(t.Context(), f.orgID, r.ID, byOperator)
	requireReason(t, err, ReasonUpdatesInFlight)
	f.finishUpdate(t, first, "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StateWaitingForController, f.rollout(t, r.ID).State)
	completed, err := f.svc.CompleteRollout(t.Context(), f.orgID, r.ID, Mutation{Actor: testActor, Note: "stop after canary"})
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, completed.Status)
	require.EqualValues(t, 1, completed.DeviceCounts.Done)
	require.EqualValues(t, 2, completed.DeviceCounts.Skipped)
	for _, d := range completed.Devices {
		if d.Phase == PhaseSkipped {
			require.Equal(t, "stop after canary", d.SkipNote)
		}
	}
	f.svc.EnforceTick(t.Context())
	require.Len(t, f.dispatcher.sent, 1)
	successor := f.latestRollout(t)
	require.NotEqual(t, r.ID, successor.ID)
	require.Equal(t, StateWaitingForController, successor.State)
	require.Len(t, successor.Devices, 1)
	require.Equal(t, "late-miner", successor.Devices[0].DeviceIdentifier)
}

func TestDelegatedAdvanceBudgetAndPauseAreAtomic(t *testing.T) {
	f := newFixture(t, 3)
	b := delegated()
	b.MaxConcurrentOffline = 1
	f.channel(t, b, f.allMiners()...)
	r := f.apply(t, "fw-2")
	_, _, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 2}, byOperator)
	requireReason(t, err, ReasonOfflineBudgetFull)
	require.Empty(t, f.dispatcher.sent)
	require.Equal(t, r.Revision, f.rollout(t, r.ID).Revision)
	_, err = f.svc.PauseRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	_, _, err = f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	requireReason(t, err, ReasonPaused)
	require.Empty(t, f.dispatcher.sent)
	_, err = f.svc.ResumeRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	_, sent, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	require.NoError(t, err)
	require.Len(t, sent, 1)
	f.setStatus(t, sent[0], "OFFLINE")
	_, _, err = f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	requireReason(t, err, ReasonOfflineBudgetFull)
	require.Len(t, f.dispatcher.sent, 1)
	_, _, err = f.svc.CancelRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	_, _, err = f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	requireReason(t, err, ReasonNotActive)
}

func TestDelegatedAdvanceRejectsWholeSelectionBeforeDispatch(t *testing.T) {
	f := newFixture(t, 3)
	f.channel(t, delegated(), f.allMiners()...)
	r := f.apply(t, "fw-2")
	_, _, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{DeviceIdentifiers: []string{"miner-0", "unknown"}}, byOperator)
	requireReason(t, err, ReasonDeviceNotQueued)
	f.queueFirmwareCommand(t, "miner-1", "fw-1", "PENDING")
	_, _, err = f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{DeviceIdentifiers: []string{"miner-0", "miner-1"}}, byOperator)
	requireReason(t, err, ReasonUpdatesInFlight)
	require.Empty(t, f.dispatcher.sent)
	f.files.deleted["fw-2"] = true
	_, _, err = f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{DeviceIdentifiers: []string{"miner-0"}}, byOperator)
	requireReason(t, err, ReasonArtifactMissing)
	require.Empty(t, f.dispatcher.sent)
	require.Equal(t, r.Revision, f.rollout(t, r.ID).Revision)
}

func TestSkipIsAtomicAndWorksForPausedAutonomousRollout(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, allAtOnce, f.allMiners()...)
	r := f.apply(t, "fw-2")
	_, err := f.svc.PauseRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	paused := f.rollout(t, r.ID)
	_, err = f.svc.SkipRolloutDevices(t.Context(), f.orgID, r.ID, []string{"miner-0", "missing"}, byOperator)
	requireReason(t, err, ReasonDeviceNotQueued)
	require.Equal(t, paused.Revision, f.rollout(t, r.ID).Revision)
	skipped, err := f.svc.SkipRolloutDevices(t.Context(), f.orgID, r.ID, []string{"miner-0"}, Mutation{Actor: testActor, Note: "maintenance"})
	require.NoError(t, err)
	require.Equal(t, PhaseSkipped, phaseOf(*skipped, "miner-0"))
	require.Equal(t, "maintenance", deviceOf(t, *skipped, "miner-0").SkipNote)
	require.Equal(t, paused.Revision+1, skipped.Revision)
	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	require.Equal(t, PhaseQueued, phaseOf(*retried, "miner-0"))
	require.Empty(t, deviceOf(t, *retried, "miner-0").SkipNote)
}

func TestDelegatedTimeoutResetsOnResumeAndDoesNotRunDuringUpdates(t *testing.T) {
	f := newFixture(t, 2)
	b := delegated()
	b.ControllerTimeoutSeconds = 60
	f.channel(t, b, f.allMiners()...)
	r := f.apply(t, "fw-2")
	f.advanceClock(time.Minute)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatePaused, f.rollout(t, r.ID).State)
	events, _, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{RolloutID: r.ID})
	require.NoError(t, err)
	require.Equal(t, EventRolloutControllerTimedOut, events[len(events)-1].Type)
	require.Equal(t, ActorTypeSystem, events[len(events)-1].Actor.Type)
	_, err = f.svc.ResumeRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StateWaitingForController, f.rollout(t, r.ID).State)
	_, sent, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	require.NoError(t, err)
	f.advanceClock(time.Minute)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StateInProgress, f.rollout(t, r.ID).State)
	f.finishUpdate(t, sent[0], "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StateWaitingForController, f.rollout(t, r.ID).State)
	f.advanceClock(time.Minute)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatePaused, f.rollout(t, r.ID).State)
}

func TestDelegatedFailedAttemptRequiresControllerRetryAndCompletes(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, delegated(), "miner-0")
	r := f.apply(t, "fw-2")
	_, _, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	require.NoError(t, err)
	f.advanceClock(resendInterval + time.Second)
	f.svc.EnforceTick(t.Context())
	failed := f.rollout(t, r.ID)
	require.Equal(t, StatusCompletedWithFailures, failed.Status)
	require.Equal(t, PhaseFailed, phaseOf(failed, "miner-0"))
	require.Len(t, f.dispatcher.sent, 1)
	events, _, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{RolloutID: r.ID})
	require.NoError(t, err)
	require.Equal(t, EventRolloutDeviceFailed, events[len(events)-2].Type)
	require.Equal(t, []string{"miner-0"}, events[len(events)-2].DeviceIdentifiers)
	require.Equal(t, EventRolloutCompletedWithFailures, events[len(events)-1].Type)
}

func TestRolloutEventsPaginatePollBindScopeAndRecordCancellation(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, delegated(), f.allMiners()...)
	r := f.apply(t, "fw-2")
	events, cursor, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{RolloutID: r.ID, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, EventRolloutStarted, events[0].Type)
	require.Equal(t, r.Revision, events[0].RolloutRevision)
	require.Equal(t, testActor.Type, events[0].Actor.Type)
	empty, same, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{RolloutID: r.ID, Cursor: cursor})
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Equal(t, cursor, same)
	_, _, err = f.svc.ListRolloutEvents(t.Context(), f.orgID+1, EventFilter{RolloutID: r.ID, Cursor: cursor})
	require.Error(t, err)
	_, err = f.svc.SkipRolloutDevices(t.Context(), f.orgID, r.ID, []string{"miner-0"}, Mutation{Actor: Actor{Type: ActorTypeAPIKey, ID: 55, Name: "controller"}, Note: "canary excluded"})
	require.NoError(t, err)
	f.apply(t, "fw-1")
	events, _, err = f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{RolloutID: r.ID, Cursor: cursor})
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, EventRolloutDevicesSkipped, events[0].Type)
	require.Equal(t, "canary excluded", events[0].Note)
	require.Equal(t, ActorTypeAPIKey, events[0].Actor.Type)
	require.EqualValues(t, 55, events[0].Actor.ID)
	require.Equal(t, EventRolloutCanceled, events[1].Type)
	require.Less(t, events[0].ID, events[1].ID)
}

func TestDelegatedConcurrentAdvancesRespectRevision(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, delegated(), f.allMiners()...)
	r := f.apply(t, "fw-2")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	dispatcher, release := blockRolloutDispatch(f)
	var workers sync.WaitGroup
	t.Cleanup(func() { cancel(); release(); workers.Wait() })
	advance := func() error {
		_, _, err := f.svc.AdvanceRollout(ctx, f.orgID, r.ID, DeviceSelection{Count: 1}, Mutation{Actor: testActor, ExpectedRevision: r.Revision})
		return err
	}
	first := startDispatchWorker(&workers, advance)
	select {
	case <-dispatcher.entered:
	case <-ctx.Done():
		t.Fatal("advance did not reach dispatch")
	}
	second := startDispatchWorker(&workers, advance)
	waitForDispatchBlock(t, ctx, f, second)
	release()
	require.NoError(t, <-first)
	requireReason(t, <-second, ReasonStaleRevision)
	require.Len(t, f.dispatcher.sentIdentifiers(), 1)
}

func TestRolloutEventCursorCannotOvertakeUncommittedEvent(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, delegated(), "miner-0")
	first := f.apply(t, "fw-2")
	channel, err := f.svc.CreateChannel(t.Context(), f.orgID, testActor.ID, ChannelSpec{Name: "Second", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}, Behavior: delegated()})
	require.NoError(t, err)
	started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, channel.ID, []Assignment{rigAssignment("fw-2")}, nil)
	require.NoError(t, err)
	require.Len(t, started, 1)
	_, cursor, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	admission, err := f.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = admission.Rollback() }()
	q := sqlc.New(admission)
	require.NoError(t, q.LockReleaseChannelScopes(ctx, f.orgID))
	require.NoError(t, q.CreateFirmwareRolloutEvent(ctx, sqlc.CreateFirmwareRolloutEventParams{
		RolloutID: first.ID, Type: EventRolloutAdvanced, ActorType: ActorTypeSystem, DeviceIdentifiers: []string{},
	}))
	var workers sync.WaitGroup
	t.Cleanup(func() { cancel(); workers.Wait() })
	later := startDispatchWorker(&workers, func() error { _, err := f.svc.PauseRollout(ctx, f.orgID, started[0].ID, byOperator); return err })
	waitForDispatchBlock(t, ctx, f, later)
	empty, same, err := f.svc.ListRolloutEvents(ctx, f.orgID, EventFilter{Cursor: cursor})
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Equal(t, cursor, same)
	require.NoError(t, admission.Commit())
	require.NoError(t, <-later)
	events, _, err := f.svc.ListRolloutEvents(ctx, f.orgID, EventFilter{Cursor: cursor})
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, first.ID, events[0].RolloutID)
	require.Equal(t, started[0].ID, events[1].RolloutID)
	require.Less(t, events[0].ID, events[1].ID)
}

func TestRolloutEventFailureRollsBackMutation(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, delegated(), "miner-0")
	r := f.apply(t, "fw-2")
	_, err := f.conn.ExecContext(t.Context(), `CREATE FUNCTION reject_rollout_event() RETURNS trigger AS $$
        BEGIN RAISE EXCEPTION 'audit unavailable'; END; $$ LANGUAGE plpgsql;
        CREATE TRIGGER reject_rollout_event BEFORE INSERT ON firmware_rollout_event
        FOR EACH ROW EXECUTE FUNCTION reject_rollout_event();`)
	require.NoError(t, err)
	_, err = f.svc.SkipRolloutDevices(t.Context(), f.orgID, r.ID, []string{"miner-0"}, byOperator)
	require.ErrorContains(t, err, "audit unavailable")
	current := f.rollout(t, r.ID)
	require.Equal(t, r.Revision, current.Revision)
	require.Equal(t, PhaseQueued, phaseOf(current, "miner-0"))
}

func TestRollbackNotesAreRecordedOnResultingEvents(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, delegated(), "miner-0")
	f.apply(t, "fw-1")
	r := f.apply(t, "fw-2")
	_, cursor, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{})
	require.NoError(t, err)
	_, started, err := f.svc.RollbackFirmware(t.Context(), f.orgID, r.ID, Mutation{Actor: testActor, Note: "canary regressed"})
	require.NoError(t, err)
	require.Len(t, started, 1)
	events, _, err := f.svc.ListRolloutEvents(t.Context(), f.orgID, EventFilter{Cursor: cursor})
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, EventRolloutCanceled, events[0].Type)
	require.Equal(t, EventRolloutStarted, events[1].Type)
	for _, event := range events {
		require.Equal(t, "canary regressed", event.Note)
	}
}

func TestDelegatedResumeDoesNotCountInFlightTimeAsControllerWait(t *testing.T) {
	f := newFixture(t, 2)
	b := delegated()
	b.ControllerTimeoutSeconds = 60
	f.channel(t, b, f.allMiners()...)
	r := f.apply(t, "fw-2")
	_, sent, err := f.svc.AdvanceRollout(t.Context(), f.orgID, r.ID, DeviceSelection{Count: 1}, byOperator)
	require.NoError(t, err)
	_, err = f.svc.PauseRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	_, err = f.svc.ResumeRollout(t.Context(), f.orgID, r.ID, byOperator)
	require.NoError(t, err)
	// No enforcement tick observes the still-running command before it
	// settles. A process restart or delayed worker has the same effect.
	f.advanceClock(2 * time.Minute)
	f.finishUpdate(t, sent[0], "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StateWaitingForController, f.rollout(t, r.ID).State)
	f.advanceClock(time.Minute)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatePaused, f.rollout(t, r.ID).State)
}
