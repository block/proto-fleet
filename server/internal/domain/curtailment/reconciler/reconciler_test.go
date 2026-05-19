package reconciler

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// fakeStore is an in-memory CurtailmentStore for reconciler tests. Methods
// the reconciler does not exercise panic so an unintended call is loud.
type fakeStore struct {
	events           []*models.Event
	targetsByEventID map[int64][]*models.Target
	candidates       []*models.Candidate

	listEventsErr      error
	listEventsCalls    int
	listEventsPanicErr string

	updateEventCalls   int
	updateEventLast    map[int64]models.EventState
	updateTargetCalls  int
	updateTargetParams map[string]interfaces.UpdateCurtailmentTargetStateParams

	listTargetsByEventCalls int

	heartbeatCalls        int
	lastHeartbeatActive   int32
	lastHeartbeatTickUUID uuid.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		targetsByEventID:   map[int64][]*models.Target{},
		updateEventLast:    map[int64]models.EventState{},
		updateTargetParams: map[string]interfaces.UpdateCurtailmentTargetStateParams{},
	}
}

func (f *fakeStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	panic("GetOrgConfig not exercised")
}
func (f *fakeStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised")
}
func (f *fakeStore) ListRecentlyResolvedCurtailedDevices(context.Context, int64, int32) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised")
}
func (f *fakeStore) GetEventByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventByUUID not exercised")
}
func (f *fakeStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised")
}
func (f *fakeStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised")
}

func (f *fakeStore) ListTargetsByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	f.listTargetsByEventCalls++
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			// Return shared pointers so the reconciler's per-tick mutations
			// are visible across phases (mirrors how the SQL store flow
			// works once dispatchPending returns the in-memory slice).
			return append([]*models.Target(nil), f.targetsByEventID[ev.ID]...), nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListCandidates(_ context.Context, _ int64, deviceIdentifiers []string) ([]*models.Candidate, error) {
	if len(deviceIdentifiers) == 0 {
		return f.candidates, nil
	}
	want := map[string]struct{}{}
	for _, id := range deviceIdentifiers {
		want[id] = struct{}{}
	}
	out := make([]*models.Candidate, 0, len(f.candidates))
	for _, c := range f.candidates {
		if _, ok := want[c.DeviceIdentifier]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	f.listEventsCalls++
	if f.listEventsPanicErr != "" {
		panic(f.listEventsPanicErr)
	}
	if f.listEventsErr != nil {
		return nil, f.listEventsErr
	}
	out := make([]*models.Event, 0, len(f.events))
	for _, ev := range f.events {
		out = append(out, ev)
	}
	return out, nil
}

func (f *fakeStore) UpdateEventState(_ context.Context, eventID int64, state models.EventState, _ *time.Time, _ *time.Time) error {
	f.updateEventCalls++
	f.updateEventLast[eventID] = state
	for _, ev := range f.events {
		if ev.ID == eventID {
			ev.State = state
		}
	}
	return nil
}

func (f *fakeStore) UpdateTargetState(_ context.Context, eventID int64, deviceIdentifier string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	f.updateTargetCalls++
	f.updateTargetParams[deviceIdentifier] = params
	for _, t := range f.targetsByEventID[eventID] {
		if t.DeviceIdentifier == deviceIdentifier {
			t.State = params.State
			if params.LastDispatchedAt != nil {
				t.LastDispatchedAt = params.LastDispatchedAt
			}
			if params.LastBatchUUID != nil {
				t.LastBatchUUID = params.LastBatchUUID
			}
			if params.ObservedPowerW != nil {
				t.ObservedPowerW = params.ObservedPowerW
			}
			if params.ObservedAt != nil {
				t.ObservedAt = params.ObservedAt
			}
			if params.ConfirmedAt != nil {
				t.ConfirmedAt = params.ConfirmedAt
			}
			if params.RetryCount != nil {
				t.RetryCount = *params.RetryCount
			}
			if params.LastError != nil {
				// Empty-string maps to "clear the error" so callers can
				// signal a successful redispatch without resorting to NULL
				// over the wire.
				if *params.LastError == "" {
					t.LastError = nil
				} else {
					t.LastError = params.LastError
				}
			}
		}
	}
	return nil
}

func (f *fakeStore) UpsertHeartbeat(_ context.Context, params interfaces.UpsertCurtailmentHeartbeatParams) error {
	f.heartbeatCalls++
	f.lastHeartbeatActive = params.ActiveEventCount
	f.lastHeartbeatTickUUID = params.LastTickUUID
	return nil
}

// fakeDispatcher records Curtail / Uncurtail calls and returns the
// configured outcome.
type fakeDispatcher struct {
	curtailErr            error
	curtailResultOverride *command.CommandResult
	uncurtailErr          error
	curtailCalls          int
	curtailLastIDs        []string
	curtailLastActor      session.Actor
	uncurtailCalls        int
	uncurtailLastIDs      []string
}

func (f *fakeDispatcher) Curtail(ctx context.Context, selector *pb.DeviceSelector, _ sdk.CurtailLevel) (*command.CommandResult, error) {
	f.curtailCalls++
	f.curtailLastIDs = identifiersFromSelector(selector)
	if info, err := session.GetInfo(ctx); err == nil {
		f.curtailLastActor = info.Actor
	}
	if f.curtailErr != nil {
		return nil, f.curtailErr
	}
	if f.curtailResultOverride != nil {
		return f.curtailResultOverride, nil
	}
	return &command.CommandResult{BatchIdentifier: "batch-curtail", DispatchedCount: len(f.curtailLastIDs), DispatchedDeviceIdentifiers: f.curtailLastIDs}, nil
}

func (f *fakeDispatcher) Uncurtail(_ context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error) {
	f.uncurtailCalls++
	f.uncurtailLastIDs = identifiersFromSelector(selector)
	if f.uncurtailErr != nil {
		return nil, f.uncurtailErr
	}
	return &command.CommandResult{BatchIdentifier: "batch-uncurtail", DispatchedCount: len(f.uncurtailLastIDs)}, nil
}

func identifiersFromSelector(selector *pb.DeviceSelector) []string {
	if selector == nil {
		return nil
	}
	if inc, ok := selector.SelectionType.(*pb.DeviceSelector_IncludeDevices); ok && inc.IncludeDevices != nil {
		return append([]string(nil), inc.IncludeDevices.DeviceIdentifiers...)
	}
	return nil
}

// --- helpers ---

func newReconcilerForTest(store *fakeStore, disp *fakeDispatcher) *Reconciler {
	r := New(Config{
		TickInterval:         time.Hour, // tests drive runTick directly
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp)
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }
	return r
}

func ptrFloat64(v float64) *float64 { return &v }

// --- tests ---

func TestReconciler_PendingDispatchesCurtail(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// One Curtail call per target.
	assert.Equal(t, 2, disp.curtailCalls)
	assert.Equal(t, session.ActorCurtailment, disp.curtailLastActor)

	// Both targets transitioned to dispatched.
	require.Len(t, store.targetsByEventID[eventID], 2)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][1].State)

	// Heartbeat upserted once.
	assert.Equal(t, 1, store.heartbeatCalls)
	assert.Equal(t, int32(1), store.lastHeartbeatActive)
}

func TestReconciler_DispatchedConfirmedViaTelemetry_TransitionsEventActive(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	// Single target already in dispatched state; telemetry shows curtailed.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatched, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// No new dispatch (target was already dispatched).
	assert.Equal(t, 0, disp.curtailCalls)
	// Target promoted to confirmed.
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[eventID][0].State)
	// Event flipped to active.
	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID])
}

func TestReconciler_DriftDetectionRetriesDispatch(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		// power_w=2500 vs baseline=3000 * 0.5 threshold=1500 → drifted
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// Drifted target re-dispatched.
	assert.Equal(t, 1, disp.curtailCalls)
	// Target ends in dispatched state (after re-dispatch updates it from drifted).
	// RetryCount stays at 0 because the redispatch succeeded — the budget is
	// consumed on dispatch *failures*, not on drift detection.
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Equal(t, int32(0), final.RetryCount)
}

func TestReconciler_RetryExhaustionLeavesDrifted(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	// Already drifted at the cap; reconciler should leave it alone.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDrifted, BaselinePowerW: ptrFloat64(3000), RetryCount: 3},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls)
	assert.Equal(t, models.TargetStateDrifted, store.targetsByEventID[eventID][0].State)
}

func TestReconciler_PerEventErrorIsolation(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	// Two events: the first will panic mid-process via a poisoned target;
	// the second must still complete.
	store.events = []*models.Event{
		{ID: 10, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending},
		{ID: 20, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[10] = []*models.Target{
		{CurtailmentEventID: 10, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}
	store.targetsByEventID[20] = []*models.Target{
		{CurtailmentEventID: 20, DeviceIdentifier: "miner-2", State: models.TargetStatePending},
	}

	// Force a panic on the first dispatch only.
	first := true
	disp.curtailErr = nil
	r := newReconcilerForTest(store, disp)
	originalCmd := r.cmd
	r.cmd = &panickyDispatcher{wrapped: originalCmd, panicOn: func() bool {
		if first {
			first = false
			return true
		}
		return false
	}}

	r.runTick(context.Background())

	// Event 20 still saw a dispatch.
	assert.Equal(t, 1, disp.curtailCalls)
	// Heartbeat still fires.
	assert.Equal(t, 1, store.heartbeatCalls)
}

func TestReconciler_HeartbeatAdvancesOnEveryTick(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	// Empty event list still upserts heartbeat.
	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())
	r.runTick(context.Background())
	r.runTick(context.Background())
	assert.Equal(t, 3, store.heartbeatCalls)
}

func TestReconciler_HeartbeatStillFiresOnListEventsError(t *testing.T) {
	store := newFakeStore()
	store.listEventsErr = errors.New("db down")
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.heartbeatCalls)
	assert.Equal(t, int32(0), store.lastHeartbeatActive)
}

func TestReconciler_DispatchErrorMarksLastError(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State, "dispatch error keeps target pending for retry")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "queue down")
	assert.Equal(t, int32(1), final.RetryCount, "dispatch error bumps retry budget")
}

// TestReconciler_DispatchSkippedKeepsTargetPending: result.Skipped means the
// command was filter-blocked and never enqueued; promoting to dispatched
// would silently drop the work. Stay pending and surface the skip reason.
func TestReconciler_DispatchSkippedKeepsTargetPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		curtailResultOverride: &command.CommandResult{
			BatchIdentifier: "",
			DispatchedCount: 0,
			Skipped: []command.SkippedDevice{{
				DeviceIdentifier: "miner-1",
				FilterName:       "schedule_conflict",
				Reason:           "schedule 99 holds higher priority",
			}},
		},
	}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State, "filter-skipped target stays pending")
	assert.Nil(t, final.LastDispatchedAt, "filter-skipped target must not record a dispatch timestamp")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "schedule 99")
	assert.Equal(t, int32(1), final.RetryCount, "skip counts toward retry budget")
}

// TestReconciler_MissingCandidateDuringConfirmConsumesRetryBudget pins the
// fix for the device-deleted-after-dispatch race: when ListCandidates
// returns no row for a Dispatched target, confirmOneDispatched routes the
// target through recordDispatchFailure (target stays Dispatched while the
// retry budget consumes) instead of stalling the event indefinitely.
func TestReconciler_MissingCandidateDuringConfirmConsumesRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "vanished", State: models.TargetStateDispatched, RetryCount: 0},
	}
	// store.candidates left empty: ListCandidates returns nothing for
	// "vanished" → confirmOneDispatched takes the nil-candidate path.

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State, "target stays dispatched while budget consumes")
	assert.Equal(t, int32(1), final.RetryCount, "missing candidate consumes a retry")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "candidate row missing")
}

// TestReconciler_MissingCandidateDuringDriftConsumesRetryBudget mirrors
// the confirm-side fix for the active-event drift path: if a Confirmed
// target's candidate row vanishes, checkDrift records a dispatch failure
// (target stays Drifted) so the budget consumes and the target eventually
// hits RestoreFailed rather than the event stalling forever.
func TestReconciler_MissingCandidateDuringDriftConsumesRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	now := time.Now()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive, StartedAt: &now},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "vanished", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000), RetryCount: 0},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State, "missing candidate moves confirmed→drifted via failure record")
	assert.Equal(t, int32(1), final.RetryCount, "missing candidate consumes a retry")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "candidate row missing")
}

// TestReconciler_DispatchEmptyBatchKeepsTargetPending: a nil-error result
// with an empty BatchIdentifier means processCommand resolved zero device
// IDs (e.g. miner unpaired between Start and reconcile). No batch was
// enqueued, so the target must NOT be marked dispatched.
func TestReconciler_DispatchEmptyBatchKeepsTargetPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		curtailResultOverride: &command.CommandResult{BatchIdentifier: "", DispatchedCount: 0},
	}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State, "empty-batch target stays pending")
	assert.Nil(t, final.LastDispatchedAt, "empty-batch target must not record dispatch timestamp")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "no batch")
	assert.Equal(t, int32(1), final.RetryCount, "empty batch counts toward retry budget")
}

// TestReconciler_DispatchFailureExhaustionMarksRestoreFailedAndEventActive:
// after MaxRetries dispatch failures the target moves to RestoreFailed; the
// event then promotes to active because every other target has confirmed
// (here: a single failing target → completed_with_failures).
func TestReconciler_DispatchFailureExhaustionTransitionsTerminal(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)

	// Tick 1 + 2: target stays pending with retry incremented.
	r.runTick(context.Background())
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(1), store.targetsByEventID[eventID][0].RetryCount)

	r.runTick(context.Background())
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(2), store.targetsByEventID[eventID][0].RetryCount)

	// Tick 3 hits MaxRetries=3 and promotes the target to RestoreFailed; the
	// event has no confirmed target so it transitions to completed_with_failures.
	r.runTick(context.Background())
	assert.Equal(t, models.TargetStateRestoreFailed, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(3), store.targetsByEventID[eventID][0].RetryCount)
	assert.Equal(t, models.EventStateCompletedWithFailures, store.updateEventLast[eventID])
}

// TestReconciler_PendingPromotesActiveWithMixedTerminalTargets: a confirmed
// target plus a permanently failed target should let the event promote to
// active; the failure does not block lifecycle progression.
func TestReconciler_PendingPromotesActiveWithMixedTerminalTargets(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		// miner-1 already dispatched + telemetry shows curtailed → confirms.
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatched, BaselinePowerW: ptrFloat64(3000)},
		// miner-2 was already exhausted → terminal failure on this tick.
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStateRestoreFailed, RetryCount: 3},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// miner-1 confirmed; miner-2 still terminal; event promoted to active
	// despite the failed target.
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateRestoreFailed, store.targetsByEventID[eventID][1].State)
	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID])
}

// TestReconciler_PendingDoesNotPromoteWhilePartialBudgetRemains: a single
// failing target with two retries left must not push the event into active
// — the budget is still in flight.
func TestReconciler_PendingDoesNotPromoteWhilePartialBudgetRemains(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// Retry incremented, target still pending, event not promoted.
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(1), store.targetsByEventID[eventID][0].RetryCount)
	_, promoted := store.updateEventLast[eventID]
	assert.False(t, promoted, "event must not promote while retry budget remains")
}

// TestReconciler_DispatchedReConfirmsViaObserveActive: a target that drifted
// then re-dispatched (Dispatched on an active event) should re-confirm in
// the same flow once telemetry shows curtailment.
func TestReconciler_DispatchedReConfirmsViaObserveActive(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		// On an active event, a dispatched target represents a
		// drift→redispatch waiting on confirmation telemetry.
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatched, BaselinePowerW: ptrFloat64(3000), RetryCount: 1},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// No new dispatch; observeActive promotes back to confirmed and resets retry.
	assert.Equal(t, 0, disp.curtailCalls)
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateConfirmed, final.State)
	assert.Equal(t, int32(0), final.RetryCount, "confirmation resets retry budget for the next drift cycle")
}

// TestReconciler_RetryBudgetResetsOnReConfirm: drift → confirm → drift →
// confirm cycles must each get a fresh retry budget so a flapping miner is
// not artificially terminated by carry-over attempts.
func TestReconciler_RetryBudgetResetsOnReConfirm(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	// Tick 1: drift power=2500 → drifted+redispatch (success keeps RetryCount=0).
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}
	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())
	assert.Equal(t, int32(0), store.targetsByEventID[eventID][0].RetryCount)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)

	// Tick 2: telemetry shows curtailed → reconfirm. Retry stays at 0.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}
	r.runTick(context.Background())
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(0), store.targetsByEventID[eventID][0].RetryCount, "reconfirm clears retry budget")

	// Tick 3: drift again — successful redispatch leaves RetryCount=0 again.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}
	r.runTick(context.Background())
	assert.Equal(t, int32(0), store.targetsByEventID[eventID][0].RetryCount)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// TestReconciler_DriftFailedRedispatchConsumesOneBudgetPerAttempt: the retry
// budget tracks dispatch attempts, not drift events. Three drift+fail cycles
// must consume exactly MaxRetries=3 dispatch slots — not 6 — and the third
// failure transitions the target to RestoreFailed.
func TestReconciler_DriftFailedRedispatchConsumesOneBudgetPerAttempt(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	// Telemetry stays drifted for every tick.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)

	// Tick 1: confirmed → drifted, redispatch fails → RetryCount=1, state Drifted
	// (not Pending: a Pending target on an active event is orphaned).
	r.runTick(context.Background())
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State, "non-terminal failure on active-event redispatch must stay Drifted, not flip to Pending")
	assert.Equal(t, int32(1), final.RetryCount)

	// Tick 2: drifted → redispatch fails → RetryCount=2, state Drifted.
	r.runTick(context.Background())
	final = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State)
	assert.Equal(t, int32(2), final.RetryCount)

	// Tick 3: drifted → redispatch fails → RetryCount=3 hits cap → RestoreFailed.
	r.runTick(context.Background())
	final = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateRestoreFailed, final.State)
	assert.Equal(t, int32(3), final.RetryCount)

	// Exactly 3 dispatch attempts — not 6. Each cycle consumes one budget slot,
	// not two. (Old bug: checkDrift bumped retry, then recordDispatchFailure
	// bumped again, halving the effective budget.)
	assert.Equal(t, 3, disp.curtailCalls, "MaxRetries=3 should map to exactly 3 dispatch attempts")
}

// TestReconciler_DriftFailedRedispatchStaysDriftedNotPending: when an
// active-event redispatch fails with budget remaining, the target must stay
// Drifted so observeActive's Drifted arm picks it up next tick. Flipping to
// Pending would orphan the target — observeActive's Pending case is a no-op.
func TestReconciler_DriftFailedRedispatchStaysDriftedNotPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)

	// Tick 1: drift detected, redispatch fails with 2 retries left.
	r.runTick(context.Background())
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State)
	assert.Equal(t, int32(1), final.RetryCount)

	// Tick 2: observeActive's Drifted arm must pick it up and re-dispatch.
	// Switch to a successful dispatcher so the retry consumes a slot only on
	// failure (this tick succeeds → RetryCount stays 1, state goes Dispatched).
	disp.curtailErr = nil
	r.runTick(context.Background())
	final = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State, "Drifted target with budget must redispatch on the next tick")
	assert.Equal(t, int32(1), final.RetryCount, "successful redispatch does not consume the budget")
	assert.Equal(t, 2, disp.curtailCalls)
}

// TestReconciler_SuccessfulDispatchClearsLastError: a dispatch success after
// a prior failure must clear LastError so the UI does not surface a stale
// transient error after the resolution succeeded.
func TestReconciler_SuccessfulDispatchClearsLastError(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	prior := "prior queue error"
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, LastError: &prior},
	}
	// Candidate row present but with no telemetry yet — confirmOneDispatched
	// finds the row (no nil-candidate failure path) and returns early on
	// !isPositivelyCurtailed, leaving the dispatch's clear-LastError write
	// as the final state.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	if final.LastError != nil {
		assert.Empty(t, *final.LastError, "successful dispatch must clear LastError")
	}
	// The store call should have been made with a non-nil empty pointer
	// so the SQL UPDATE clears the column rather than leaving stale text.
	params := store.updateTargetParams["miner-1"]
	require.NotNil(t, params.LastError, "successful dispatch must explicitly clear LastError on the wire")
	assert.Empty(t, *params.LastError)
}

// TestReconciler_ListTargetsByEventOnce: dispatchPending+confirmDispatched+
// maybeMarkActive must share a single ListTargetsByEvent fetch per event
// per tick instead of round-tripping three times.
func TestReconciler_ListTargetsByEventOnce(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.listTargetsByEventCalls, "pending phases must share one ListTargetsByEvent per tick")
}

// TestReconciler_PanicInListEventsRecovers: ListNonTerminalEvents panicking
// must not tear down the goroutine; the next tick still runs.
func TestReconciler_PanicInListEventsRecovers(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	store.listEventsPanicErr = "synthetic db panic"
	r := newReconcilerForTest(store, disp)

	// safeTick is what the tick loop invokes; calling it here exercises the
	// top-level recover() guard. A bare runTick would re-panic the test.
	r.safeTick(context.Background())
	assert.Equal(t, 1, store.listEventsCalls, "first tick saw the panicking call")

	// Now drop the panic and run another tick — the loop should still be
	// healthy.
	store.listEventsPanicErr = ""
	r.safeTick(context.Background())
	assert.Equal(t, 2, store.listEventsCalls, "subsequent tick still runs after recover")
}

// TestReconciler_StartIdempotency: calling Start twice without an
// intervening Stop must not fork a second goroutine.
func TestReconciler_StartIdempotency(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	r := New(Config{TickInterval: time.Hour, ShutdownDeadline: time.Second}, store, disp)

	require.NoError(t, r.Start(context.Background()))
	require.NoError(t, r.Start(context.Background()), "second Start is a no-op")
	require.NoError(t, r.Stop())
	// Second Stop is a no-op too; verify no panic / goroutine deadlock.
	require.NoError(t, r.Stop())
}

// --- isCurtailed unit tests ---
//
// isCurtailed has a single shape with a requirePositiveEvidence bool:
//   - false (drift detection): missing/non-finite samples preserve "curtailed"
//     so a transient bad sensor reading does not trigger a redispatch storm.
//   - true (confirmation): missing/non-finite samples return false so a target
//     is not promoted to `confirmed` without positive evidence.

func TestIsCurtailed_DriftPath_BaselineRelativeThreshold(t *testing.T) {
	baseline := 3000.0
	// 1000 < baseline*0.5=1500 → curtailed
	assert.True(t, isCurtailed(ptrFloat64(1000), &baseline, ptrFloat64(0), 0.5, false))
	// 2500 > 1500 → not curtailed
	assert.False(t, isCurtailed(ptrFloat64(2500), &baseline, ptrFloat64(100), 0.5, false))
}

func TestIsCurtailed_DriftPath_DualSignalFallbackWithoutBaseline(t *testing.T) {
	// No baseline; positive hash → drifted.
	assert.False(t, isCurtailed(ptrFloat64(2500), nil, ptrFloat64(100), 0.5, false))
	// No baseline; zero hash → curtailed.
	assert.True(t, isCurtailed(ptrFloat64(2500), nil, ptrFloat64(0), 0.5, false))
}

func TestIsCurtailed_DriftPath_NonFinitePreservesCurtailed(t *testing.T) {
	baseline := 3000.0
	nan := math.NaN()
	inf := math.Inf(1)
	// NaN power → no signal → preserve curtailed.
	assert.True(t, isCurtailed(&nan, &baseline, ptrFloat64(0), 0.5, false))
	// +Inf power → no signal → preserve curtailed.
	assert.True(t, isCurtailed(&inf, &baseline, ptrFloat64(0), 0.5, false))
	// nil power, NaN hash → preserve curtailed.
	assert.True(t, isCurtailed(nil, &baseline, &nan, 0.5, false))
}

// TestIsCurtailed_ConfirmPath_RequiresEvidence pins the asymmetric default
// for the confirm path: missing or non-finite telemetry returns false so
// `dispatched` targets are NOT promoted to `confirmed` without evidence
// the device actually went down.
func TestIsCurtailed_ConfirmPath_RequiresEvidence(t *testing.T) {
	baseline := 3000.0
	nan := math.NaN()
	inf := math.Inf(1)

	// Below threshold → confirmed.
	assert.True(t, isCurtailed(ptrFloat64(1000), &baseline, ptrFloat64(0), 0.5, true))
	// Above threshold → not confirmed.
	assert.False(t, isCurtailed(ptrFloat64(2500), &baseline, ptrFloat64(0), 0.5, true))

	// Missing power → not confirmed (asymmetric vs. drift detection).
	assert.False(t, isCurtailed(nil, &baseline, ptrFloat64(0), 0.5, true))
	// Non-finite power → not confirmed.
	assert.False(t, isCurtailed(&nan, &baseline, ptrFloat64(0), 0.5, true))
	assert.False(t, isCurtailed(&inf, &baseline, ptrFloat64(0), 0.5, true))

	// Baseline missing: dual-signal fallback requires finite zero-or-negative hash.
	assert.True(t, isCurtailed(ptrFloat64(1000), nil, ptrFloat64(0), 0.5, true))
	assert.False(t, isCurtailed(ptrFloat64(1000), nil, nil, 0.5, true), "no baseline + missing hash → no evidence")
	assert.False(t, isCurtailed(ptrFloat64(1000), nil, &nan, 0.5, true), "no baseline + non-finite hash → no evidence")
	assert.False(t, isCurtailed(ptrFloat64(1000), nil, ptrFloat64(100), 0.5, true), "no baseline + positive hash → drifted")
}

// panickyDispatcher proxies Curtail/Uncurtail and panics when panicOn() returns
// true. Used to verify per-event error isolation.
type panickyDispatcher struct {
	wrapped CommandDispatcher
	panicOn func() bool
}

func (p *panickyDispatcher) Curtail(ctx context.Context, selector *pb.DeviceSelector, level sdk.CurtailLevel) (*command.CommandResult, error) {
	if p.panicOn() {
		panic("simulated dispatch panic")
	}
	return p.wrapped.Curtail(ctx, selector, level)
}

func (p *panickyDispatcher) Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error) {
	return p.wrapped.Uncurtail(ctx, selector)
}
