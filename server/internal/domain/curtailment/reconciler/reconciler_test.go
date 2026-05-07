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

	listEventsErr error

	updateEventCalls   int
	updateEventLast    map[int64]models.EventState
	updateTargetCalls  int
	updateTargetParams map[string]interfaces.UpdateCurtailmentTargetStateParams

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
func (f *fakeStore) InsertEvent(context.Context, models.InsertEventParams) (*models.InsertEventResult, error) {
	panic("InsertEvent not exercised")
}
func (f *fakeStore) GetEventByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventByUUID not exercised")
}
func (f *fakeStore) InsertTarget(context.Context, models.InsertTargetParams) error {
	panic("InsertTarget not exercised")
}
func (f *fakeStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised")
}
func (f *fakeStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised")
}

func (f *fakeStore) ListTargetsByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			out := make([]*models.Target, 0, len(f.targetsByEventID[ev.ID]))
			for _, t := range f.targetsByEventID[ev.ID] {
				clone := *t
				out = append(out, &clone)
			}
			return out, nil
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
	if f.listEventsErr != nil {
		return nil, f.listEventsErr
	}
	out := make([]*models.Event, 0, len(f.events))
	for _, ev := range f.events {
		clone := *ev
		out = append(out, &clone)
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
				t.LastError = params.LastError
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
	curtailErr       error
	uncurtailErr     error
	curtailCalls     int
	curtailLastIDs   []string
	curtailLastActor session.Actor
	uncurtailCalls   int
	uncurtailLastIDs []string
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
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Equal(t, int32(1), final.RetryCount)
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
}

// --- isCurtailedByPower unit tests ---

func TestIsCurtailedByPower_BaselineRelativeThreshold(t *testing.T) {
	baseline := 3000.0
	// 1000 < baseline*0.5=1500 → curtailed
	assert.True(t, isCurtailedByPower(ptrFloat64(1000), &baseline, ptrFloat64(0), 0.5))
	// 2500 > 1500 → not curtailed
	assert.False(t, isCurtailedByPower(ptrFloat64(2500), &baseline, ptrFloat64(100), 0.5))
}

func TestIsCurtailedByPower_DualSignalFallbackWithoutBaseline(t *testing.T) {
	// No baseline; positive hash → drifted.
	assert.False(t, isCurtailedByPower(ptrFloat64(2500), nil, ptrFloat64(100), 0.5))
	// No baseline; zero hash → curtailed.
	assert.True(t, isCurtailedByPower(ptrFloat64(2500), nil, ptrFloat64(0), 0.5))
}

func TestIsCurtailedByPower_NonFinitePreservesCurtailed(t *testing.T) {
	baseline := 3000.0
	nan := math.NaN()
	inf := math.Inf(1)
	// NaN power → no signal → preserve curtailed.
	assert.True(t, isCurtailedByPower(&nan, &baseline, ptrFloat64(0), 0.5))
	// +Inf power → no signal → preserve curtailed.
	assert.True(t, isCurtailedByPower(&inf, &baseline, ptrFloat64(0), 0.5))
	// nil power, NaN hash → preserve curtailed.
	assert.True(t, isCurtailedByPower(nil, &baseline, &nan, 0.5))
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
