package reconciler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Compile-time assertion that recordingMetrics satisfies curtailment.Metrics —
// surfaces a missing method at build time rather than letting the duplicate
// definition in service_test.go drift independently.
var _ curtailment.Metrics = (*recordingMetrics)(nil)

// fakeStore is an in-memory CurtailmentStore for reconciler tests. Methods
// the reconciler does not exercise panic so an unintended call is loud.
type fakeStore struct {
	events           []*models.Event
	targetsByEventID map[int64][]*models.Target
	candidates       []*models.Candidate

	listEventsErr      error
	listEventsCalls    int
	listEventsPanicErr string
	listTargetsHook    func(context.Context, uuid.UUID)
	listTargetsCtxErr  map[uuid.UUID]error

	updateEventCalls      int
	updateEventLast       map[int64]models.EventState
	updateTargetCalls     int
	updateTargetParams    map[string]interfaces.UpdateCurtailmentTargetStateParams
	updateTargetStateErr  error
	updateTargetStateHook func(device string, params interfaces.UpdateCurtailmentTargetStateParams, call int) error

	bumpTargetRetryCalls int
	lastBumpTargetRetry  bumpRetryCall
	bumpTargetRetryErr   error

	listTargetsByEventCalls int

	heartbeatCalls        int
	lastHeartbeatActive   int32
	lastHeartbeatTickUUID uuid.UUID

	// BeginRestoreTransition captures, exercised by max_duration tests.
	beginRestoreCalls       int
	beginRestoreLastEventID uuid.UUID
	beginRestoreErr         error
}

type bumpRetryCall struct {
	EventID          int64
	DeviceIdentifier string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		targetsByEventID:   map[int64][]*models.Target{},
		updateEventLast:    map[int64]models.EventState{},
		updateTargetParams: map[string]interfaces.UpdateCurtailmentTargetStateParams{},
		listTargetsCtxErr:  map[uuid.UUID]error{},
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
func (f *fakeStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	panic("SiteBelongsToOrg not exercised")
}
func (f *fakeStore) GetEventByUUID(_ context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	for _, ev := range f.events {
		if ev.OrgID == orgID && ev.EventUUID == eventUUID {
			return ev, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	return f.GetEventByUUID(ctx, orgID, eventUUID)
}
func (f *fakeStore) GetActiveEvent(context.Context, int64) (*models.Event, error) {
	panic("GetActiveEvent not exercised")
}
func (f *fakeStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	panic("ListActiveEvents not exercised")
}
func (f *fakeStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised")
}
func (f *fakeStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised")
}

func (f *fakeStore) ListTargetsByEvent(ctx context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	f.listTargetsByEventCalls++
	if f.listTargetsHook != nil {
		f.listTargetsHook(ctx, eventUUID)
	}
	f.listTargetsCtxErr[eventUUID] = ctx.Err()
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

func (f *fakeStore) ListTargetsByEventPage(ctx context.Context, params interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	targets, err := f.ListTargetsByEvent(ctx, params.OrgID, params.EventUUID)
	return targets, "", err
}

func (f *fakeStore) GetTargetRollupByEvent(context.Context, int64, uuid.UUID) (*models.TargetRollup, error) {
	panic("GetTargetRollupByEvent not exercised by reconciler tests")
}

func (f *fakeStore) ListCandidates(_ context.Context, params interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	if len(params.DeviceIdentifiers) == 0 {
		return f.candidates, nil
	}
	want := map[string]struct{}{}
	for _, id := range params.DeviceIdentifiers {
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

func (f *fakeStore) ListEvents(context.Context, interfaces.ListEventsParams) ([]*models.Event, string, error) {
	panic("ListEvents not exercised by reconciler tests")
}

func (f *fakeStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by reconciler tests")
}

func (f *fakeStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, bool, error) {
	panic("AdminTerminateEvent not exercised by reconciler tests")
}

func (f *fakeStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by reconciler tests")
}

func (f *fakeStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by reconciler tests")
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

func (f *fakeStore) UpdateEventState(_ context.Context, eventID int64, expectedState models.EventState, state models.EventState, _ *time.Time, _ *time.Time) error {
	f.updateEventCalls++
	f.updateEventLast[eventID] = state
	for _, ev := range f.events {
		if ev.ID == eventID {
			if ev.State != expectedState {
				return interfaces.ErrCurtailmentEventStateRaceLoss
			}
			ev.State = state
		}
	}
	return nil
}

func (f *fakeStore) UpdateTargetState(_ context.Context, eventID int64, deviceIdentifier string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	f.updateTargetCalls++
	f.updateTargetParams[deviceIdentifier] = params
	// updateTargetStateHook lets a test reject specific writes (e.g.
	// simulate the EXISTS guard's race-loss sentinel firing on the Nth
	// call) without globally poisoning the fake.
	if f.updateTargetStateHook != nil {
		if err := f.updateTargetStateHook(deviceIdentifier, params, f.updateTargetCalls); err != nil {
			return err
		}
	}
	// updateTargetStateErr lets tests inject the race-loss sentinel or other
	// errors without going through the in-memory state machine. When set,
	// the mirror is not advanced — matches the sqlstore contract.
	if f.updateTargetStateErr != nil {
		return f.updateTargetStateErr
	}
	for _, t := range f.targetsByEventID[eventID] {
		if t.DeviceIdentifier == deviceIdentifier {
			// Honor the ExpectedDesiredState predicate: the real SQL's
			// `desired_state = $11` clause makes the UPDATE no-op when the
			// caller's expected direction doesn't match the row, surfacing
			// as the race-loss sentinel. The fake mirrors that contract.
			//
			// Test-double simplification: an empty t.DesiredState means the
			// test author didn't set it (production targets are NOT NULL).
			// Treat empty as "any" so existing tests that don't care about
			// phase don't have to backfill the field. Tests that pin the
			// Curtail-vs-Stop dispatch-direction race set t.DesiredState
			// explicitly.
			if params.ExpectedEventState != nil {
				for _, ev := range f.events {
					if ev.ID == eventID && ev.State != *params.ExpectedEventState {
						return interfaces.ErrCurtailmentEventStateRaceLoss
					}
				}
			}
			if params.ExpectedDesiredState != nil && t.DesiredState != "" && t.DesiredState != *params.ExpectedDesiredState {
				return interfaces.ErrCurtailmentEventStateRaceLoss
			}
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
			updateTargetPhaseSummary(t, params)
		}
	}
	return nil
}

func (f *fakeStore) BumpTargetRetry(_ context.Context, eventID int64, deviceIdentifier string) error {
	f.bumpTargetRetryCalls++
	f.lastBumpTargetRetry = bumpRetryCall{EventID: eventID, DeviceIdentifier: deviceIdentifier}
	if f.bumpTargetRetryErr != nil {
		return f.bumpTargetRetryErr
	}
	for _, t := range f.targetsByEventID[eventID] {
		if t.DeviceIdentifier == deviceIdentifier {
			t.RetryCount++
			bumpTargetPhaseRetry(t)
			return nil
		}
	}
	return interfaces.ErrCurtailmentEventStateRaceLoss
}

func (f *fakeStore) UpsertHeartbeat(_ context.Context, params interfaces.UpsertCurtailmentHeartbeatParams) error {
	f.heartbeatCalls++
	f.lastHeartbeatActive = params.ActiveEventCount
	f.lastHeartbeatTickUUID = params.LastTickUUID
	return nil
}

// BeginRestoreTransition: real-fake behavior so enforceMaxDuration tests can
// assert the call happened and the event row flips to restoring in-place
// (mirroring SQL store semantics — the reconciler reads ev again on the next
// tick). effective_batch_size was stamped at Start; this fake does not touch it.
func (f *fakeStore) BeginRestoreTransition(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	f.beginRestoreCalls++
	f.beginRestoreLastEventID = eventUUID
	if f.beginRestoreErr != nil {
		return nil, f.beginRestoreErr
	}
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			ev.State = models.EventStateRestoring
			now := time.Now()
			for _, t := range f.targetsByEventID[ev.ID] {
				if t.State == models.TargetStateResolved ||
					t.State == models.TargetStateRestoreFailed ||
					t.State == models.TargetStateReleased {
					continue
				}
				t.DesiredState = models.DesiredStateActive
				t.State = models.TargetStatePending
				t.RetryCount = 0
				t.LastDispatchedAt = nil
				t.LastBatchUUID = nil
				t.ConfirmedAt = nil
				t.LastError = nil
				t.RestorePhase = &models.TargetPhaseSummary{
					Phase:     models.TargetPhaseRestore,
					State:     models.TargetStatePending,
					StartedAt: &now,
				}
			}
			return ev, nil
		}
	}
	return nil, nil
}

// BeginRecurtailTransition satisfies the store interface; the reconciler never
// calls it (the MQTT subscriber's watchdog drives re-curtail).
func (f *fakeStore) BeginRecurtailTransition(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			ev.State = models.EventStatePending
			for _, t := range f.targetsByEventID[ev.ID] {
				if t.RestorePhase == nil {
					continue
				}
				t.DesiredState = models.DesiredStateCurtailed
				t.State = models.TargetStatePending
				t.RetryCount = 0
				t.LastDispatchedAt = nil
				t.LastBatchUUID = nil
				t.ConfirmedAt = nil
				t.LastError = nil
				t.CurtailPhase = models.TargetPhaseSummary{
					Phase: models.TargetPhaseCurtail,
					State: models.TargetStatePending,
				}
			}
			return ev, nil
		}
	}
	return nil, nil
}

func updateTargetPhaseSummary(t *models.Target, params interfaces.UpdateCurtailmentTargetStateParams) {
	desired := t.DesiredState
	if desired == "" && params.ExpectedDesiredState != nil {
		desired = *params.ExpectedDesiredState
	}
	var phase *models.TargetPhaseSummary
	switch desired {
	case models.DesiredStateCurtailed, "":
		if t.CurtailPhase.Phase == "" {
			t.CurtailPhase.Phase = models.TargetPhaseCurtail
		}
		if t.CurtailPhase.StartedAt == nil && !t.AddedAt.IsZero() {
			started := t.AddedAt
			t.CurtailPhase.StartedAt = &started
		}
		phase = &t.CurtailPhase
	case models.DesiredStateActive:
		if t.RestorePhase == nil {
			t.RestorePhase = &models.TargetPhaseSummary{Phase: models.TargetPhaseRestore}
		}
		phase = t.RestorePhase
	default:
		return
	}

	phase.State = params.State
	if params.LastDispatchedAt != nil {
		phase.DispatchedAt = params.LastDispatchedAt
	}
	if params.LastBatchUUID != nil {
		phase.BatchUUID = params.LastBatchUUID
	}
	if params.RetryCount != nil {
		phase.RetryCount = *params.RetryCount
	} else {
		phase.RetryCount = t.RetryCount
	}
	if params.LastError != nil && *params.LastError != "" {
		phase.FailureCount++
		phase.LastError = params.LastError
	}
	if phaseCompleted(params.State, desired) {
		if params.ConfirmedAt != nil {
			phase.CompletedAt = params.ConfirmedAt
			return
		}
		now := time.Now()
		phase.CompletedAt = &now
	}
}

func bumpTargetPhaseRetry(t *models.Target) {
	switch t.DesiredState {
	case models.DesiredStateActive:
		if t.RestorePhase == nil {
			t.RestorePhase = &models.TargetPhaseSummary{Phase: models.TargetPhaseRestore}
		}
		t.RestorePhase.RetryCount++
	default:
		t.CurtailPhase.RetryCount++
	}
}

func phaseCompleted(state models.TargetState, desired string) bool {
	switch desired {
	case models.DesiredStateActive:
		return state == models.TargetStateResolved ||
			state == models.TargetStateReleased ||
			state == models.TargetStateRestoreFailed
	default:
		return state == models.TargetStateConfirmed ||
			state == models.TargetStateReleased ||
			state == models.TargetStateRestoreFailed
	}
}

// fakeDispatcher records Curtail / Uncurtail calls and returns the
// configured outcome.
type fakeDispatcher struct {
	curtailErr              error
	curtailResultOverride   *command.CommandResult
	uncurtailErr            error
	uncurtailResultOverride *command.CommandResult
	curtailCalls            int
	curtailLastIDs          []string
	curtailCallIDs          [][]string
	curtailLastActor        session.Actor
	curtailLastSuppressed   bool
	uncurtailCalls          int
	uncurtailLastIDs        []string
	uncurtailLastSuppressed bool
	// curtailHook fires synchronously inside Curtail before the result is
	// returned. Tests use it to inspect store state at the moment the
	// command-service call happens (e.g., to verify the DISPATCHING
	// pre-write committed before the command issued).
	curtailHook func(ids []string)
	// uncurtailHook mirrors curtailHook for the restore path.
	uncurtailHook func(ids []string)
}

func (f *fakeDispatcher) Curtail(ctx context.Context, selector *pb.DeviceSelector, _ sdk.CurtailLevel) (*command.CommandResult, error) {
	f.curtailCalls++
	f.curtailLastIDs = identifiersFromSelector(selector)
	f.curtailCallIDs = append(f.curtailCallIDs, append([]string(nil), f.curtailLastIDs...))
	if info, err := session.GetInfo(ctx); err == nil {
		f.curtailLastActor = info.Actor
	}
	f.curtailLastSuppressed = command.CommandActivitySuppressed(ctx)
	if f.curtailHook != nil {
		f.curtailHook(f.curtailLastIDs)
	}
	if f.curtailErr != nil {
		return nil, f.curtailErr
	}
	if f.curtailResultOverride != nil {
		return f.curtailResultOverride, nil
	}
	return &command.CommandResult{BatchIdentifier: "batch-curtail", DispatchedCount: len(f.curtailLastIDs), DispatchedDeviceIdentifiers: f.curtailLastIDs}, nil
}

func (f *fakeDispatcher) Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error) {
	f.uncurtailCalls++
	f.uncurtailLastIDs = identifiersFromSelector(selector)
	f.uncurtailLastSuppressed = command.CommandActivitySuppressed(ctx)
	if f.uncurtailHook != nil {
		f.uncurtailHook(f.uncurtailLastIDs)
	}
	if f.uncurtailErr != nil {
		return nil, f.uncurtailErr
	}
	if f.uncurtailResultOverride != nil {
		return f.uncurtailResultOverride, nil
	}
	return &command.CommandResult{BatchIdentifier: "batch-uncurtail", DispatchedCount: len(f.uncurtailLastIDs), DispatchedDeviceIdentifiers: f.uncurtailLastIDs}, nil
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

	effBatch := int32(2)
	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// One Curtail call covers the bounded initial batch.
	assert.Equal(t, 1, disp.curtailCalls)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, disp.curtailLastIDs)
	assert.Equal(t, session.ActorCurtailment, disp.curtailLastActor)
	assert.True(t, disp.curtailLastSuppressed, "curtailment-owned batches must suppress command activity")

	// Both targets transitioned to dispatched.
	require.Len(t, store.targetsByEventID[eventID], 2)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][1].State)
	require.NotNil(t, store.targetsByEventID[eventID][0].LastBatchUUID)
	require.NotNil(t, store.targetsByEventID[eventID][1].LastBatchUUID)
	assert.Equal(t, *store.targetsByEventID[eventID][0].LastBatchUUID, *store.targetsByEventID[eventID][1].LastBatchUUID,
		"batched Curtail targets must share one command batch UUID")

	// Heartbeat upserted once.
	assert.Equal(t, 1, store.heartbeatCalls)
	assert.Equal(t, int32(1), store.lastHeartbeatActive)
}

func TestReconciler_PendingDispatchesAllTargetsInEffectiveBatches(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(2)
	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-3", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 2, disp.curtailCalls)
	require.Len(t, disp.curtailCallIDs, 2)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, disp.curtailCallIDs[0])
	assert.ElementsMatch(t, []string{"miner-3"}, disp.curtailCallIDs[1])
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][1].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][2].State)
}

func TestReconciler_PendingDispatchesLargeEventInBoundedBatches(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(100)
	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	for i := range 205 {
		store.targetsByEventID[eventID] = append(store.targetsByEventID[eventID], &models.Target{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   fmt.Sprintf("miner-%03d", i),
			State:              models.TargetStatePending,
			BaselinePowerW:     ptrFloat64(3000),
		})
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 3, disp.curtailCalls)
	require.Len(t, disp.curtailCallIDs, 3)
	assert.Len(t, disp.curtailCallIDs[0], 100)
	assert.Len(t, disp.curtailCallIDs[1], 100)
	assert.Len(t, disp.curtailCallIDs[2], 5)
	for _, target := range store.targetsByEventID[eventID] {
		assert.Equal(t, models.TargetStateDispatched, target.State)
	}
}

func TestReconciler_DispatchingFailureDoesNotRetryAgainAsPendingInSameTick(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue unavailable")}

	eventID := int64(10)
	eventUUID := uuid.New()
	effBatch := int32(3)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-orphan",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"failed DISPATCHING recovery must not be re-claimed by the same tick's PENDING pass")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatching, final.State,
		"failed DISPATCHING recovery must remain DISPATCHING for the next tick")
	assert.Equal(t, int32(1), final.RetryCount,
		"failed DISPATCHING recovery must consume only one retry slot per tick")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "queue unavailable")
}

func TestReconciler_CurtailBatchRecordsSkippedAndNotEnqueuedFailures(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		curtailResultOverride: &command.CommandResult{
			BatchIdentifier:             "batch-partial",
			DispatchedDeviceIdentifiers: []string{"miner-dispatched"},
			Skipped: []command.SkippedDevice{
				{DeviceIdentifier: "miner-skipped", Reason: "maintenance mode"},
			},
		},
	}

	eventID := int64(10)
	eventUUID := uuid.New()
	effBatch := int32(3)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-dispatched", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-skipped", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-missing", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls)
	byID := map[string]*models.Target{}
	for _, target := range store.targetsByEventID[eventID] {
		byID[target.DeviceIdentifier] = target
	}

	dispatched := byID["miner-dispatched"]
	require.NotNil(t, dispatched)
	assert.Equal(t, models.TargetStateDispatched, dispatched.State)
	require.NotNil(t, dispatched.LastBatchUUID)
	assert.Equal(t, "batch-partial", *dispatched.LastBatchUUID)

	skipped := byID["miner-skipped"]
	require.NotNil(t, skipped)
	assert.Equal(t, models.TargetStatePending, skipped.State)
	assert.Equal(t, int32(1), skipped.RetryCount)
	require.NotNil(t, skipped.LastError)
	assert.Equal(t, "maintenance mode", *skipped.LastError)

	missing := byID["miner-missing"]
	require.NotNil(t, missing)
	assert.Equal(t, models.TargetStatePending, missing.State)
	assert.Equal(t, int32(1), missing.RetryCount)
	require.NotNil(t, missing.LastError)
	assert.Equal(t, "curtail command did not enqueue device", *missing.LastError)
}

func TestReconciler_TargetPhaseSummariesCaptureCurtailAndRestoreCycle(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(1)
	eventID := int64(10)
	eventUUID := uuid.New()
	addedAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	store.events = []*models.Event{
		{
			ID:                 eventID,
			EventUUID:          eventUUID,
			OrgID:              1,
			State:              models.EventStatePending,
			EffectiveBatchSize: &effBatch,
		},
	}
	baseline := float64(3000)
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     &baseline,
			AddedAt:            addedAt,
		},
	}
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-1",
			LatestPowerW:     ptrFloat64(0),
			LatestHashRateHS: ptrFloat64(0),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	target := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.EventStateActive, store.events[0].State)
	assert.Equal(t, models.TargetStateConfirmed, target.CurtailPhase.State)
	require.NotNil(t, target.CurtailPhase.DispatchedAt)
	require.NotNil(t, target.CurtailPhase.BatchUUID)
	assert.Equal(t, "batch-curtail", *target.CurtailPhase.BatchUUID)
	require.NotNil(t, target.CurtailPhase.CompletedAt)
	assert.Equal(t, int32(0), target.CurtailPhase.FailureCount)

	_, err := store.BeginRestoreTransition(context.Background(), 1, eventUUID)
	require.NoError(t, err)
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-1",
			LatestPowerW:     ptrFloat64(3100),
			LatestHashRateHS: ptrFloat64(100),
		},
	}

	r.runTick(context.Background())
	target = store.targetsByEventID[eventID][0]
	require.NotNil(t, target.RestorePhase)
	assert.Equal(t, models.TargetStateDispatched, target.RestorePhase.State)
	require.NotNil(t, target.RestorePhase.DispatchedAt)
	require.NotNil(t, target.RestorePhase.BatchUUID)
	assert.Equal(t, "batch-uncurtail", *target.RestorePhase.BatchUUID)

	r.runTick(context.Background())
	target = store.targetsByEventID[eventID][0]
	require.NotNil(t, target.RestorePhase)
	assert.Equal(t, models.EventStateCompleted, store.events[0].State)
	assert.Equal(t, models.TargetStateResolved, target.RestorePhase.State)
	require.NotNil(t, target.RestorePhase.CompletedAt)
	assert.Equal(t, int32(0), target.RestorePhase.FailureCount)
}

func TestReconciler_SkipsCurtailDispatchWhenEventTerminatesBeforeCommand(t *testing.T) {
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
	store.listTargetsHook = func(context.Context, uuid.UUID) {
		store.events[0].State = models.EventStateCancelled
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls)
	assert.Equal(t, 0, store.updateTargetCalls)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
}

// A concurrent AdminTerminate that lands between target N and N+1 must
// stop further Curtail commands — the EXISTS guard on the DISPATCHING
// pre-write catches the race.
func TestReconciler_SkipsRemainingCurtailDispatchesWhenEventTerminatesMidLoop(t *testing.T) {
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
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-3", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}
	// Target 1's DISPATCHING pre-write (call 1) succeeds; the hook returns
	// the race-loss sentinel on the post-command DISPATCHED write (call 2)
	// and every subsequent DISPATCHING pre-write thereafter, simulating an
	// AdminTerminate that committed mid-loop. Only target 1's Curtail can
	// fire — its DISPATCHING write landed before the race.
	store.updateTargetStateHook = func(_ string, _ interfaces.UpdateCurtailmentTargetStateParams, call int) error {
		if call >= 2 {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"only the first target should dispatch; the rest skip after the event flipped")
}

// On a typed race-loss return, the reconciler increments
// IncEventStateRaceLoss and does NOT advance the in-memory mirror — that
// keeps the mirror consistent with persisted state on a silent SQL no-op.
func TestReconciler_TargetStateRaceLoss_LogsAndMetersWithoutMirrorAdvance(t *testing.T) {
	store := newFakeStore()
	store.updateTargetStateErr = interfaces.ErrCurtailmentEventStateRaceLoss
	disp := &fakeDispatcher{}
	metrics := &recordingMetrics{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	// The store's UpdateTargetState returned the sentinel — mirror update
	// must be skipped, so the in-memory state stays at PENDING.
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State,
		"in-memory mirror must NOT advance when the store reports race-loss")
	// Race-loss surfaces as IncEventStateRaceLoss, not IncTickFailure.
	assert.GreaterOrEqual(t, metrics.EventStateRaceLossCount(), 1,
		"race-loss sentinel must increment IncEventStateRaceLoss")
	// Race-loss is benign concurrency — IncTargetWriteFailure stays at 0
	// so the "degraded write path" alert doesn't trip on routine Stop /
	// AdminTerminate landings.
	assert.Equal(t, 0, metrics.TargetWriteFailureCount(),
		"race-loss must NOT count as a target-write failure; the two counters are operationally distinct signals")
}

// A non-race-loss target-state write failure increments
// IncTargetWriteFailure so dashboards can detect "heartbeat fresh but
// writes failing" outages.
func TestReconciler_TargetWriteFailure_IncrementsCounter(t *testing.T) {
	store := newFakeStore()
	store.updateTargetStateErr = errors.New("connection refused")
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	assert.GreaterOrEqual(t, metrics.TargetWriteFailureCount(), 1,
		"non-race-loss target-write failure must increment IncTargetWriteFailure")
	assert.Equal(t, 0, metrics.EventStateRaceLossCount(),
		"non-race-loss failure must NOT increment IncEventStateRaceLoss (different signal class)")
}

// dispatchOneCurtail must commit DISPATCHING before calling cmd.Curtail
// so AdminTerminate's in-flight gate observes it and rejects.
func TestReconciler_DispatchingPreWrite_CommitsBeforeCommand(t *testing.T) {
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

	// Capture the in-store target state at the moment cmd.Curtail is invoked.
	var stateAtCommandTime models.TargetState
	disp.curtailHook = func(_ []string) {
		for _, target := range store.targetsByEventID[eventID] {
			if target.DeviceIdentifier == "miner-1" {
				stateAtCommandTime = target.State
				return
			}
		}
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, models.TargetStateDispatching, stateAtCommandTime,
		"target must be DISPATCHING at the moment cmd.Curtail is called so a concurrent AdminTerminate's in-flight gate observes it")
	// After the command returns successfully, the target advances to
	// DISPATCHED — the DISPATCHING window only exists during the command call.
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// A row-specific persistent write failure on the DISPATCHING pre-write
// (non-race-loss) must burn a retry slot via recordDispatchFailure so a
// stuck target eventually escalates to terminal — otherwise the event
// stalls indefinitely. Mirrors the analogous restore-path coverage.
func TestReconciler_CurtailPreWriteFailureBurnsRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	// Fail only the DISPATCHING pre-write (call 1); recordDispatchFailure's
	// follow-up write at call 2 must succeed so retry_count actually lands.
	store.updateTargetStateHook = func(_ string, _ interfaces.UpdateCurtailmentTargetStateParams, call int) error {
		if call == 1 {
			return errors.New("transient row write failure")
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"pre-write failure must short-circuit before cmd.Curtail")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State,
		"target stays in the prior state after a pre-write failure under MaxRetries")
	assert.Equal(t, int32(1), final.RetryCount,
		"pre-write failure must bump retry_count so the event can't stall indefinitely")
	require.NotNil(t, final.LastError, "pre-write failure must record last_error")
}

// If Stop moves the parent event out of the active phase after the liveness
// read but before the DISPATCHING pre-write, the write must race-lose and the
// reconciler must not issue another Curtail.
func TestReconciler_CurtailPreWriteRaceLosesWhenStopFlipsEventState(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.updateTargetStateHook = func(_ string, _ interfaces.UpdateCurtailmentTargetStateParams, call int) error {
		if call == 1 {
			store.events[0] = &models.Event{
				ID:        eventID,
				EventUUID: eventUUID,
				OrgID:     1,
				State:     models.EventStateRestoring,
			}
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"stale active-phase pre-write must fail before cmd.Curtail is issued")
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
}

// If Stop runs between the pre-cmd DISPATCHING write and the post-cmd
// DISPATCHED write, the post-cmd write's ExpectedDesiredState predicate
// must race-lose so it doesn't clobber Stop's reset. curtailHook
// simulates the race by flipping desired_state to 'active' during the
// command call.
func TestReconciler_CurtailPostWriteRaceLosesWhenStopFlipsDesiredState(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}

	// Mid-command, simulate a concurrent Stop: event → RESTORING, target's
	// DesiredState flipped to 'active', state reset to 'pending'. (Per
	// ResetCurtailmentTargetsForRestore semantics.)
	disp.curtailHook = func(_ []string) {
		store.events[0].State = models.EventStateRestoring
		store.targetsByEventID[eventID][0].DesiredState = models.DesiredStateActive
		store.targetsByEventID[eventID][0].State = models.TargetStatePending
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// cmd.Curtail fired exactly once — the device received the Curtail.
	assert.Equal(t, 1, disp.curtailCalls,
		"cmd.Curtail fires before the race-lose detection (device-level effect is unavoidable mid-command)")

	// The post-cmd DISPATCHED write must NOT have landed. The target
	// must retain Stop's reset state (PENDING + DesiredStateActive) so
	// observeRestoring picks it up next tick and issues the compensating
	// Uncurtail via maybeClaimRestoreBatch.
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State,
		"post-cmd write must race-lose; target stays in Stop's reset state for restore to pick up")
	assert.Equal(t, models.DesiredStateActive, final.DesiredState,
		"Stop's desired_state flip must be preserved (not clobbered by the racing post-cmd write)")
	assert.Nil(t, final.LastBatchUUID,
		"no Curtail batch identifier should be stamped on a target that Stop has reset for restore")
}

// A target left in DISPATCHING by an interrupted prior tick must be
// redispatched on the next tick (Curtail is device-idempotent).
func TestReconciler_RecoversOrphanedDispatchingTarget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatching, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"orphaned DISPATCHING target must be redispatched on the next tick")
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// observeActive's orphan recovery: a DISPATCHING target on an ACTIVE
// event (interrupted drift-fix) must be re-issued via Curtail.
func TestReconciler_RecoversOrphanedDispatchingTargetOnActiveEvent(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"orphaned DISPATCHING target on an ACTIVE event must be redispatched")
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// TestReconciler_ObserveActive_DispatchingOrphanRespectsRetryBudget: a
// DISPATCHING orphan whose RetryCount has already hit MaxRetries must NOT
// be redispatched. Matches the symmetric Drifted-arm backstop and prevents
// budget-exhausted orphans from cycling indefinitely.
// A DISPATCHING orphan whose retry_count is already at MaxRetries must
// terminalize on the next tick rather than loop forever in DISPATCHING.
// The recordDispatchFailure fallback (BumpTargetRetry on writeTargetState
// failure) can leave a target in DISPATCHING with a bumped retry count,
// so observeActive must escalate exhausted orphans through the same
// helper to reach RESTORE_FAILED.
func TestReconciler_ObserveActive_ExhaustedDispatchingOrphanEscalates(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			RetryCount:         3, // already at MaxRetries default
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"exhausted DISPATCHING orphan must not be redispatched")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateRestoreFailed, final.State,
		"exhausted DISPATCHING orphan must escalate to RESTORE_FAILED via recordDispatchFailure")
	assert.Equal(t, int32(4), final.RetryCount,
		"recordDispatchFailure bumps retry_count once more on escalation")
	require.NotNil(t, final.LastError, "escalation records a last_error")
}

// A race-loss on the orphan-redispatch pre-write must not fire Curtail
// — pins the EXISTS-guard race-closure on the observeActive path.
func TestReconciler_ObserveActive_DispatchingOrphanRaceLossDoesNotIssueCommand(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	store.updateTargetStateHook = func(string, interfaces.UpdateCurtailmentTargetStateParams, int) error {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}

	eventID := int64(11)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"race-loss on the DISPATCHING pre-write must prevent cmd.Curtail from firing")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatching, final.State,
		"in-memory mirror must not advance on race-loss")
}

// Restore-path orphan recovery: a DISPATCHING target with
// DesiredState=Active is redispatched via Uncurtail. uncurtailHook
// asserts the DISPATCHING pre-write commits before the command issues —
// guards against a regression that calls Uncurtail before stamping.
func TestReconciler_RecoversOrphanedDispatchingRestoreTarget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateRestoring},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			DesiredState:       models.DesiredStateActive,
			State:              models.TargetStateDispatching,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	var stateAtUncurtail models.TargetState
	disp.uncurtailHook = func(_ []string) {
		stateAtUncurtail = store.targetsByEventID[eventID][0].State
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.uncurtailCalls,
		"orphaned DISPATCHING restore target must be redispatched via Uncurtail")
	assert.Equal(t, models.TargetStateDispatching, stateAtUncurtail,
		"target must be re-stamped DISPATCHING before Uncurtail fires so AdminTerminate's in-flight gate sees the row")
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

func TestReconciler_SkipsRestoreDispatchWhenEventTerminatesBeforeCommand(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateRestoring},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			DesiredState:       models.DesiredStateActive,
			State:              models.TargetStatePending,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.listTargetsHook = func(context.Context, uuid.UUID) {
		store.events[0].State = models.EventStateFailed
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.uncurtailCalls)
	assert.Equal(t, 0, store.updateTargetCalls)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
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

// A Drifted target whose retry_count already sits at MaxRetries must
// escalate to the terminal state rather than loop in Drifted. Mirrors
// the DISPATCHING arm: BumpTargetRetry's fallback can bump retry_count
// past the budget without a state transition, so observeActive routes
// exhausted Drifted through recordDispatchFailure to reach RESTORE_FAILED.
func TestReconciler_RetryExhaustedDriftedEscalatesToRestoreFailed(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDrifted, BaselinePowerW: ptrFloat64(3000), RetryCount: 3},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"exhausted Drifted target must not be re-dispatched")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateRestoreFailed, final.State,
		"exhausted Drifted target must escalate to RESTORE_FAILED via recordDispatchFailure")
	assert.Equal(t, int32(4), final.RetryCount,
		"recordDispatchFailure bumps retry_count once more on escalation")
	require.NotNil(t, final.LastError, "escalation records a last_error")
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

// Heartbeat advances on tick freshness, not query health: a List failure
// still upserts the heartbeat (with active_count=0) and increments
// IncTickFailure, so the SQL staleness alert distinguishes "reconciler
// dead" (no upsert) from "DB read path degraded" (upsert advances,
// IncTickFailure rises).
func TestReconciler_ListEventsErrorAdvancesHeartbeatAndIncrementsFailure(t *testing.T) {
	store := newFakeStore()
	store.listEventsErr = errors.New("db down")
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.runTick(context.Background())

	assert.Equal(t, 1, store.heartbeatCalls,
		"heartbeat must advance on tick freshness so the SQL staleness alert distinguishes reconciler-dead from DB-read-degraded")
	assert.Equal(t, int32(0), store.lastHeartbeatActive,
		"List failure carries activeCount=0 — no events observed this tick")
	assert.Equal(t, 1, metrics.TickFailureCount())
}

func TestReconciler_RunTickStopsWhenTickBudgetExpires(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	firstUUID := uuid.New()
	secondUUID := uuid.New()
	store.events = []*models.Event{
		{ID: 1, EventUUID: firstUUID, OrgID: 1, State: models.EventStateActive},
		{ID: 2, EventUUID: secondUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.listTargetsHook = func(ctx context.Context, eventUUID uuid.UUID) {
		if eventUUID == firstUUID {
			<-ctx.Done()
		}
	}

	r := newReconcilerForTest(store, disp)
	r.cfg.TickInterval = 5 * time.Millisecond
	r.runTick(context.Background())

	assert.ErrorIs(t, store.listTargetsCtxErr[firstUUID], context.DeadlineExceeded)
	_, processedSecond := store.listTargetsCtxErr[secondUUID]
	assert.False(t, processedSecond,
		"later events must wait for the next tick after the tick-scoped budget expires")
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

// TestSkippedDeviceReason pins the priority-order contract for the shared
// skip-reason rendering helper. Both the single-device dispatch path and the
// batch restore path route audit strings through this switch, so each branch
// must land on the right string.
func TestSkippedDeviceReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    command.SkippedDevice
		want string
	}{
		{"reason_present_wins", command.SkippedDevice{Reason: "schedule conflict", FilterName: "schedule"}, "schedule conflict"},
		{"filter_name_only", command.SkippedDevice{FilterName: "maintenance_window"}, "filtered by maintenance_window"},
		{"both_empty_fallback", command.SkippedDevice{}, "filtered by command preflight"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, skippedDeviceReason(tc.s))
		})
	}
}

// A vanished candidate during confirm routes through
// recordDispatchFailure so the event can't stall on a deleted device.
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

// Mirror of MissingCandidateDuringConfirm for the drift path: a
// vanished candidate burns retry budget toward RestoreFailed.
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

// TestReconciler_ObserveTickDurationFiresOnHappyPath: every successful
// safeTick records a duration sample.
func TestReconciler_ObserveTickDurationFiresOnHappyPath(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }

	r.safeTick(context.Background())
	r.safeTick(context.Background())

	assert.Equal(t, 2, metrics.TickCount(), "ObserveTickDuration fires once per safeTick invocation")
	assert.Equal(t, 0, metrics.TickFailureCount(), "no panic, no failure increment")
}

// TestReconciler_TickFailureFiresOnTickInfraPanic: ListNonTerminalEvents
// panicking is a tick-infra failure (heartbeat-skipping). IncTickFailure
// fires; ObserveTickDuration still fires because the deferred recorder runs
// regardless.
func TestReconciler_TickFailureFiresOnTickInfraPanic(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	store.listEventsPanicErr = "synthetic db panic"
	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }

	r.safeTick(context.Background())

	assert.Equal(t, 1, metrics.TickFailureCount(), "tick-infra panic increments TickFailures")
	assert.Equal(t, 1, metrics.TickCount(), "ObserveTickDuration still fires on a panicked tick")
}

// TestReconciler_TickFailureFiresOnPerEventPanic: a panic inside processEvent
// is recovered per-event; the tick keeps running but the failure counter
// advances so operators can spot per-event panics.
func TestReconciler_TickFailureFiresOnPerEventPanic(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

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

	first := true
	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }
	originalCmd := r.cmd
	r.cmd = &panickyDispatcher{wrapped: originalCmd, panicOn: func() bool {
		if first {
			first = false
			return true
		}
		return false
	}}

	r.safeTick(context.Background())

	assert.Equal(t, 1, metrics.TickFailureCount(), "per-event panic increments TickFailures even though tick continued")
	assert.Equal(t, 1, store.heartbeatCalls, "per-event panic does not skip the heartbeat")
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
// requirePositiveEvidence=false (drift): missing samples preserve
// curtailed; =true (confirm): missing samples return false.

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

// recordingMetrics captures Metrics calls for assertion. Counters are
// goroutine-safe via a single mutex; the reconciler emits from the tick
// goroutine but tests poke from the test goroutine.
type recordingMetrics struct {
	mu                  sync.Mutex
	tickDurations       []time.Duration
	tickFailures        int
	candidateExcluded   map[string]int
	maintenance         int
	eventStateRaces     int
	targetWriteFailures int
	auditWriteFailures  map[string]int
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{
		candidateExcluded:  map[string]int{},
		auditWriteFailures: map[string]int{},
	}
}

func (m *recordingMetrics) ObserveTickDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickDurations = append(m.tickDurations, d)
}

func (m *recordingMetrics) IncTickFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickFailures++
}

func (m *recordingMetrics) IncCandidateExcluded(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candidateExcluded[reason]++
}

func (m *recordingMetrics) IncMaintenanceOverride() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maintenance++
}

func (m *recordingMetrics) IncEventStateRaceLoss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventStateRaces++
}

func (m *recordingMetrics) IncTargetWriteFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targetWriteFailures++
}

func (m *recordingMetrics) IncAuditWriteFailure(activityType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.auditWriteFailures == nil {
		m.auditWriteFailures = map[string]int{}
	}
	m.auditWriteFailures[activityType]++
}

func (m *recordingMetrics) EventStateRaceLossCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eventStateRaces
}

func (m *recordingMetrics) TargetWriteFailureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.targetWriteFailures
}

func (m *recordingMetrics) TickCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tickDurations)
}

func (m *recordingMetrics) TickFailureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tickFailures
}
