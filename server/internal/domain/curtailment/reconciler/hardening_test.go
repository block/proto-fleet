package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func hardeningFixture(state models.EventState) (*fakeStore, *fakeDispatcher, *models.Event, *models.Target) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	event := &models.Event{ID: 10, OrgID: 1, EventUUID: uuid.New(), State: state, ScopeType: models.ScopeTypeWholeOrg, AuthorizationEnvelopeJSON: []byte(defaultTestAuthorizationEnvelope)}
	target := &models.Target{CurtailmentEventID: event.ID, DeviceIdentifier: "miner-1", DesiredState: models.DesiredStateCurtailed, State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}
	return store, dispatcher, event, target
}

func TestDispatchAuthorizationRechecksEveryScope(t *testing.T) {
	for _, scope := range []models.ScopeType{models.ScopeTypeWholeOrg, models.ScopeTypeSite, models.ScopeTypeDeviceList, models.ScopeTypeMixed} {
		t.Run(string(scope), func(t *testing.T) {
			store, dispatcher, event, _ := hardeningFixture(models.EventStatePending)
			event.ScopeType = scope
			switch scope {
			case models.ScopeTypeWholeOrg, models.ScopeTypeDeviceList:
			case models.ScopeTypeSite:
				event.ScopeJSON = []byte(`{"site_id":7}`)
			case models.ScopeTypeMixed:
				event.ScopeJSON = []byte(`{"scope_schema_version":1,"site_ids":[7]}`)
			}
			reconciler := newReconcilerForTest(store, dispatcher, WithDispatchPermissionResolver(staticDispatchPermissionResolver{effective: authz.NewEffectivePermissions(nil)}))
			reconciler.runTick(context.Background())
			assert.Zero(t, dispatcher.curtailCalls)
			assert.Equal(t, models.EventStateRestoring, event.State)
			require.NotEmpty(t, store.dispatchRejections[event.ID])
		})
	}
}

func TestDispatchAuthorizationInfrastructureFailureDefersWithoutRestore(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	reconciler := newReconcilerForTest(store, dispatcher, WithDispatchPermissionResolver(staticDispatchPermissionResolver{err: errors.New("database unavailable")}))
	reconciler.runTick(context.Background())
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Zero(t, store.beginRestoreCalls)
	assert.Empty(t, store.dispatchRejections)
	assert.Equal(t, models.EventStatePending, event.State)
	assert.Zero(t, target.RetryCount, "infrastructure failures do not consume device retries")
	reconciler.permissions = allowDispatchForTest()
	reconciler.runTick(context.Background())
	assert.Equal(t, 1, dispatcher.curtailCalls)
}

func TestDispatchAuthorizationRejectionSurvivesNewReconciler(t *testing.T) {
	store, dispatcher, event, _ := hardeningFixture(models.EventStatePending)
	store.beginRestoreErr = errors.New("restore temporarily unavailable")
	rejected := newReconcilerForTest(store, dispatcher, WithDispatchPermissionResolver(staticDispatchPermissionResolver{effective: authz.NewEffectivePermissions(nil)}))
	rejected.runTick(context.Background())
	require.NotEmpty(t, store.dispatchRejections[event.ID])
	store.beginRestoreErr = nil
	restarted := newReconcilerForTest(store, dispatcher)
	restarted.runTick(context.Background())
	assert.Zero(t, dispatcher.curtailCalls, "restoring permissions cannot erase a durable denial")
	assert.Equal(t, models.EventStateRestoring, event.State)
}

func TestCurtailNeverConfirmsUsesFiniteBudgetAndCompensatingRestore(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	store.candidates = []*models.Candidate{{DeviceIdentifier: target.DeviceIdentifier, PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)}}
	reconciler := newReconcilerForTest(store, dispatcher)
	now := reconciler.now()
	reconciler.now = func() time.Time { return now }
	for range 8 {
		reconciler.runTick(context.Background())
		now = now.Add(10 * time.Second)
		if event.State == models.EventStateRestoring {
			break
		}
	}
	assert.Equal(t, 3, dispatcher.curtailCalls)
	assert.Equal(t, models.EventStateRestoring, event.State, "a command may have curtailed hardware despite missing confirmation")
	assert.Equal(t, models.TargetStateRestoreFailed, target.CurtailPhase.State)
	assert.Equal(t, models.DesiredStateActive, target.DesiredState)
	assert.Equal(t, models.TargetStatePending, target.State)
}

func TestDeletedAllPairedTopologyRestoreTerminalizes(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStateRestoring)
	event.Mode = models.ModeFullFleet
	event.LoopType = models.LoopTypeClosed
	event.ForceIncludeAllPairedMiners = true
	event.ScopeType = models.ScopeTypeMixed
	event.ScopeJSON = []byte(`{"scope_schema_version":1,"building_ids":[9]}`)
	target.DesiredState = models.DesiredStateActive
	target.State = models.TargetStateUnavailable
	reconciler := newReconcilerForTest(store, dispatcher)
	reconciler.runTick(context.Background())
	assert.Equal(t, models.TargetStateRestoreFailed, target.State)
	assert.Equal(t, models.EventStateCompletedWithFailures, event.State)
	assert.Zero(t, dispatcher.uncurtailCalls)
}

func TestRestoreBatchPrefersHealthyTargetOverRetry(t *testing.T) {
	store, dispatcher, event, failed := hardeningFixture(models.EventStateRestoring)
	event.RestoreBatchSize = 1
	event.EffectiveBatchSize = new(int32(1))
	failed.DesiredState = models.DesiredStateActive
	failed.RetryCount = 5
	healthy := &models.Target{CurtailmentEventID: event.ID, DeviceIdentifier: "healthy", DesiredState: models.DesiredStateActive, State: models.TargetStatePending}
	store.targetsByEventID[event.ID] = append(store.targetsByEventID[event.ID], healthy)
	reconciler := newReconcilerForTest(store, dispatcher)
	reconciler.maybeClaimRestoreBatch(context.Background(), event, store.targetsByEventID[event.ID])
	assert.Equal(t, []string{"healthy"}, dispatcher.uncurtailLastIDs)
	assert.Equal(t, models.TargetStatePending, failed.State)
}

func TestRestoreBatchResendsInterruptedRestoreBeforeFreshTargets(t *testing.T) {
	store, dispatcher, event, interrupted := hardeningFixture(models.EventStateRestoring)
	event.RestoreBatchSize = 1
	event.EffectiveBatchSize = new(int32(1))
	interrupted.DesiredState = models.DesiredStateActive
	interrupted.State = models.TargetStateDispatching
	interrupted.RetryCount = 1
	fresh := &models.Target{CurtailmentEventID: event.ID, DeviceIdentifier: "fresh", DesiredState: models.DesiredStateActive, State: models.TargetStatePending}
	store.targetsByEventID[event.ID] = append(store.targetsByEventID[event.ID], fresh)
	reconciler := newReconcilerForTest(store, dispatcher)
	reconciler.maybeClaimRestoreBatch(context.Background(), event, store.targetsByEventID[event.ID])
	assert.Equal(t, []string{"miner-1"}, dispatcher.uncurtailLastIDs, "an accepted Uncurtail may already be waking the interrupted miner")
	assert.Equal(t, models.TargetStatePending, fresh.State)
}

type observingStore struct {
	*fakeStore
	observationCalls int
	observationCount int
}

func (s *observingStore) ObserveTargets(ctx context.Context, event *models.Event, observations []interfaces.CurtailmentTargetObservation) ([]string, error) {
	s.observationCalls++
	s.observationCount += len(observations)
	var applied []string
	for _, observation := range observations {
		expected := models.TargetStateConfirmed
		desired := models.DesiredStateCurtailed
		if err := s.UpdateTargetState(ctx, event.ID, observation.DeviceIdentifier, interfaces.UpdateCurtailmentTargetStateParams{State: observation.State, ObservedAt: &observation.ObservedAt, ObservedPowerW: observation.ObservedPowerW, ExpectedEventState: &event.State, ExpectedState: &expected, ExpectedDesiredState: &desired}); err != nil {
			return nil, err
		}
		applied = append(applied, observation.DeviceIdentifier)
	}
	return applied, nil
}
func TestActiveObservationsAndDriftDispatchAreBatched(t *testing.T) {
	base, dispatcher, event, first := hardeningFixture(models.EventStateActive)
	first.State = models.TargetStateConfirmed
	second := *first
	second.DeviceIdentifier = "miner-2"
	base.targetsByEventID[event.ID] = append(base.targetsByEventID[event.ID], &second)
	for _, target := range base.targetsByEventID[event.ID] {
		base.candidates = append(base.candidates, &models.Candidate{DeviceIdentifier: target.DeviceIdentifier, PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)})
	}
	store := &observingStore{fakeStore: base}
	reconciler := New(Config{}, store, dispatcher, WithDispatchPermissionResolver(allowDispatchForTest()))
	reconciler.runTick(context.Background())
	assert.Equal(t, 1, store.observationCalls)
	assert.Equal(t, 2, store.observationCount)
	assert.Equal(t, 1, dispatcher.curtailCalls)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, dispatcher.curtailLastIDs)
}

func TestActivationRunsFullTickImmediately(t *testing.T) {
	store := newFakeStore()
	began := make(chan struct{}, 1)
	store.listEventsHook = func(context.Context) {
		select {
		case began <- struct{}{}:
		default:
		}
	}
	reconciler := New(Config{TickInterval: time.Hour}, store, &fakeDispatcher{})
	require.NoError(t, reconciler.Start(context.Background()))
	select {
	case <-began:
	case <-time.After(time.Second):
		t.Fatal("HA activation waited for the first periodic tick")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, reconciler.Stop(stopCtx))
	assert.Equal(t, 1, store.listEventsCalls)
}

func TestCappedPendingReadinessExpires(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	event.ForceIncludeAllPairedMiners = true
	durationCap := int32((48 * time.Hour).Seconds())
	event.MaxDurationSeconds = &durationCap
	target.State = models.TargetStateUnavailable
	reconciler := newReconcilerForTest(store, dispatcher)
	event.CreatedAt = reconciler.now()
	require.False(t, reconciler.enforcePendingDeadline(context.Background(), event, store.targetsByEventID[event.ID]), "a new event gets its full readiness budget")
	event.CreatedAt = reconciler.now().Add(-3 * time.Hour)
	reconciler.runTick(context.Background())
	assert.Equal(t, models.EventStateRestoring, event.State)
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Equal(t, models.TargetStateRestoreFailed, target.State, "deadline failure evidence is retained without waking never-dispatched hardware")
}

func TestPacedPendingPlanKeepsItsReadinessWindowWhileProgressing(t *testing.T) {
	store, dispatcher, event, confirmed := hardeningFixture(models.EventStatePending)
	durationCap := int32((48 * time.Hour).Seconds())
	event.MaxDurationSeconds = &durationCap
	event.CurtailBatchIntervalSec = 60
	reconciler := newReconcilerForTest(store, dispatcher)
	now := reconciler.now()
	event.CreatedAt = now.Add(-5 * time.Hour)
	recent := now.Add(-time.Minute)
	confirmed.State = models.TargetStateConfirmed
	confirmed.LastDispatchedAt = &recent
	confirmed.ConfirmedAt = &recent
	next := &models.Target{CurtailmentEventID: event.ID, DeviceIdentifier: "miner-2", DesiredState: models.DesiredStateCurtailed, State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)}
	store.targetsByEventID[event.ID] = append(store.targetsByEventID[event.ID], next)
	assert.False(t, reconciler.enforcePendingDeadline(context.Background(), event, store.targetsByEventID[event.ID]), "a paced plan that is still dispatching keeps waiting")
	assert.Equal(t, models.EventStatePending, event.State)

	stalled := now.Add(-4 * time.Hour)
	confirmed.LastDispatchedAt = &stalled
	confirmed.ConfirmedAt = &stalled
	assert.True(t, reconciler.enforcePendingDeadline(context.Background(), event, store.targetsByEventID[event.ID]), "the same plan expires once progress stalls")
	assert.Equal(t, models.EventStateActive, event.State, "a stalled miner never restores miners that are already curtailed")
	assert.Zero(t, store.beginRestoreCalls)
	assert.Equal(t, models.TargetStateConfirmed, confirmed.State)
	assert.Equal(t, models.TargetStateRestoreFailed, next.State, "the stalled miner records the deadline failure")
}

func TestPendingDeadlineKeepsInFlightCurtailWhenAMinerIsConfirmed(t *testing.T) {
	store, dispatcher, event, confirmed := hardeningFixture(models.EventStatePending)
	durationCap := int32((48 * time.Hour).Seconds())
	event.MaxDurationSeconds = &durationCap
	reconciler := newReconcilerForTest(store, dispatcher)
	stalled := reconciler.now().Add(-4 * time.Hour)
	event.CreatedAt = stalled
	confirmed.State = models.TargetStateConfirmed
	confirmed.ConfirmedAt = &stalled
	batch := "in-flight"
	inFlight := &models.Target{CurtailmentEventID: event.ID, DeviceIdentifier: "miner-2", DesiredState: models.DesiredStateCurtailed, State: models.TargetStateDispatched, LastDispatchedAt: &stalled, LastBatchUUID: &batch, BaselinePowerW: ptrFloat64(3000)}
	inFlight.CurtailPhase.State = models.TargetStateDispatched
	store.targetsByEventID[event.ID] = append(store.targetsByEventID[event.ID], inFlight)
	require.True(t, reconciler.enforcePendingDeadline(context.Background(), event, store.targetsByEventID[event.ID]))
	assert.Equal(t, models.EventStateActive, event.State)
	assert.Equal(t, models.TargetStateDispatched, inFlight.State, "the active phase still confirms or retries the in-flight miner")
	assert.Equal(t, models.TargetStateDispatched, inFlight.CurtailPhase.State)
}

func TestKnownUnsentCurtailFailuresCompleteWithoutUncurtail(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	dispatcher.curtailResultOverride = &command.CommandResult{BatchIdentifier: "batch-skipped", Skipped: []command.SkippedDevice{{DeviceIdentifier: target.DeviceIdentifier, Reason: "device lacks curtail capability"}}}
	reconciler := newReconcilerForTest(store, dispatcher)
	for range 6 {
		reconciler.runTick(context.Background())
		if event.State.IsTerminal() || event.State == models.EventStateRestoring {
			break
		}
	}
	assert.Equal(t, models.EventStateCompletedWithFailures, event.State, "admission skips never reached the miner")
	assert.Zero(t, store.beginRestoreCalls)
	assert.Zero(t, dispatcher.uncurtailCalls)
	assert.Equal(t, models.TargetStateRestoreFailed, target.State)
	assert.False(t, target.CurtailPossiblySent)
}

func TestAmbiguousCurtailFailuresStillReceiveCompensatingRestore(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	dispatcher.curtailErr = errors.New("command queue unavailable")
	reconciler := newReconcilerForTest(store, dispatcher)
	for range 6 {
		reconciler.runTick(context.Background())
		if event.State.IsTerminal() || event.State == models.EventStateRestoring {
			break
		}
	}
	assert.Equal(t, models.EventStateRestoring, event.State, "an enqueue error may have reached the miner")
	assert.True(t, target.CurtailPossiblySent)
	assert.Equal(t, models.DesiredStateActive, target.DesiredState)
	assert.Equal(t, models.TargetStatePending, target.State)
}

func TestSkippedRecoveryOfInterruptedDispatchCountsAsPossiblySent(t *testing.T) {
	store, dispatcher, _, target := hardeningFixture(models.EventStatePending)
	target.State = models.TargetStateDispatching
	dispatcher.curtailResultOverride = &command.CommandResult{BatchIdentifier: "batch-skipped", Skipped: []command.SkippedDevice{{DeviceIdentifier: target.DeviceIdentifier, Reason: "device lacks curtail capability"}}}
	newReconcilerForTest(store, dispatcher).runTick(context.Background())
	assert.Equal(t, 1, dispatcher.curtailCalls)
	assert.True(t, target.CurtailPossiblySent, "the interrupted attempt may have sent Curtail before this skip")
}

type automationAuthorizationStore struct {
	*fakeStore
	allowed bool
	err     error
	calls   int
}

func (s *automationAuthorizationStore) AuthorizeAutomationCurtailDispatch(context.Context, *models.Event, interfaces.CurtailmentTopologyScopeCoverage) (bool, error) {
	s.calls++
	return s.allowed, s.err
}

func automationHardeningFixture() (*fakeStore, *fakeDispatcher, *models.Event, *models.Target) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	source := "curtailment_automation"
	event.ExternalSource = &source
	return store, dispatcher, event, target
}

func TestAutomationDispatchUsesRuleGrantInsteadOfCreatorPermissions(t *testing.T) {
	base, dispatcher, event, _ := automationHardeningFixture()
	store := &automationAuthorizationStore{fakeStore: base, allowed: true}
	noPermissions := WithDispatchPermissionResolver(staticDispatchPermissionResolver{effective: authz.NewEffectivePermissions(nil)})
	New(Config{CurtailMaxRetries: 3, MaxRetries: 3}, store, dispatcher, noPermissions).runTick(context.Background())
	assert.Equal(t, 1, store.calls)
	assert.Equal(t, 1, dispatcher.curtailCalls, "a demoted or removed creator does not block a granted rule")
	assert.Empty(t, base.dispatchRejections)
	assert.Equal(t, models.EventStatePending, event.State)
}

func TestAutomationDispatchDeniedGrantLatchesAndRestores(t *testing.T) {
	base, dispatcher, event, _ := automationHardeningFixture()
	store := &automationAuthorizationStore{fakeStore: base}
	New(Config{CurtailMaxRetries: 3, MaxRetries: 3}, store, dispatcher, WithDispatchPermissionResolver(allowDispatchForTest())).runTick(context.Background())
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Equal(t, "automation execution grant no longer permits dispatch", base.dispatchRejections[event.ID])
	assert.Equal(t, models.EventStateRestoring, event.State)
}

func TestAutomationDispatchGrantReadFailureDefers(t *testing.T) {
	base, dispatcher, event, target := automationHardeningFixture()
	store := &automationAuthorizationStore{fakeStore: base, err: errors.New("database unavailable")}
	New(Config{CurtailMaxRetries: 3, MaxRetries: 3}, store, dispatcher, WithDispatchPermissionResolver(allowDispatchForTest())).runTick(context.Background())
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Empty(t, base.dispatchRejections)
	assert.Zero(t, base.beginRestoreCalls)
	assert.Equal(t, models.EventStatePending, event.State)
	assert.Zero(t, target.RetryCount, "infrastructure failures do not consume device retries")
}

func TestAutomationDispatchWithoutGrantStoreDefers(t *testing.T) {
	store, dispatcher, event, _ := automationHardeningFixture()
	newReconcilerForTest(store, dispatcher).runTick(context.Background())
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Empty(t, store.dispatchRejections, "a wiring gap must not latch a policy denial")
	assert.Equal(t, models.EventStatePending, event.State)
}

func TestUnboundedPendingEventKeepsWaitingPastReadinessDeadline(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	event.ForceIncludeAllPairedMiners = true
	event.AllowUnbounded = true
	target.State = models.TargetStateUnavailable
	reconciler := newReconcilerForTest(store, dispatcher)
	event.CreatedAt = reconciler.now().Add(-24 * time.Hour)
	require.False(t, reconciler.enforcePendingDeadline(context.Background(), event, store.targetsByEventID[event.ID]))
	reconciler.runTick(context.Background())
	assert.Equal(t, models.EventStatePending, event.State, "only an operator stop or abort ends an unbounded event")
	assert.NotEqual(t, models.TargetStateRestoreFailed, target.State, "no deadline failure evidence is recorded")
}

type generalDispatchFenceStore struct {
	*fakeStore
	coverage   interfaces.CurtailmentTopologyScopeCoverage
	fenceCalls int
}

func (s *generalDispatchFenceStore) WithCurtailmentDispatchFence(_ context.Context, event *models.Event, _ interfaces.ListCandidatesParams, ids []string, command func(interfaces.CurtailmentTopologyDispatchFenceSnapshot) error) error {
	s.fenceCalls++
	return command(interfaces.CurtailmentTopologyDispatchFenceSnapshot{Event: event, Topology: interfaces.CurtailmentTopologyDispatchSnapshot{Coverage: s.coverage, DispatchMemberDeviceIdentifiers: ids}})
}
func TestNonTopologyDispatchRejectsTargetMovedOutsideAuthorizationEnvelope(t *testing.T) {
	base, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	event.ScopeType = models.ScopeTypeDeviceList
	event.AuthorizationEnvelopeJSON = []byte(`{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[7],"miner_scope_unbounded":false,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}`)
	store := &generalDispatchFenceStore{fakeStore: base, coverage: interfaces.CurtailmentTopologyScopeCoverage{CurrentMemberSiteIDs: []int64{8}}}
	reconciler := New(Config{}, store, dispatcher, WithDispatchPermissionResolver(allowDispatchForTest()))
	reconciler.runTick(context.Background())
	assert.Equal(t, 1, store.fenceCalls)
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Equal(t, models.EventStateRestoring, event.State)
	assert.Equal(t, models.TargetStateReleased, target.State)
}

func TestPendingDeadlineRestoresInterruptedCommandWithoutEnqueueTimestamp(t *testing.T) {
	store, dispatcher, event, target := hardeningFixture(models.EventStatePending)
	target.State = models.TargetStateDispatching
	reconciler := newReconcilerForTest(store, dispatcher)
	event.CreatedAt = reconciler.now().Add(-3 * time.Hour)
	reconciler.runTick(context.Background())
	assert.Equal(t, models.EventStateRestoring, event.State)
	assert.Equal(t, models.DesiredStateActive, target.DesiredState)
	assert.Equal(t, models.TargetStatePending, target.State)
	assert.Equal(t, models.TargetStateRestoreFailed, target.CurtailPhase.State)
	assert.Zero(t, dispatcher.curtailCalls)
}
