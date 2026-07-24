package reconciler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	telemetryModels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// confirmationFakeStore wraps fakeStore with the confirmation fast path's
// store surface: a state-aware eligibility read (rows drop out once the
// underlying target leaves 'dispatched', so the pulse parks after
// confirming) and enforcement of the ExpectedState /
// ExpectedDispatchBatchUUID single-winner guards that the base fake ignores.
//
// All overridden methods and test accessors share one mutex so loop tests
// (pulse goroutine vs. asserting test goroutine) are race-clean. The
// embedded fakeStore's un-overridden methods stay unsynchronized; pulse
// tests must not exercise them concurrently from multiple goroutines.
type confirmationFakeStore struct {
	*fakeStore

	mu sync.Mutex
	// items is the authored eligibility fixture; the read filters it by the
	// in-memory target's current state.
	items              []models.ConfirmationTarget
	listEligibleErr    error
	listEligibleCalls  int
	confirmWriteCalls  int
	lastConfirmWrite   interfaces.UpdateCurtailmentTargetStateParams
	lastConfirmDevice  string
	lastConfirmEventID int64
}

func newConfirmationFakeStore() *confirmationFakeStore {
	return &confirmationFakeStore{fakeStore: newFakeStore()}
}

func (f *confirmationFakeStore) setItems(items ...models.ConfirmationTarget) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = items
}

func (f *confirmationFakeStore) setListEligibleErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listEligibleErr = err
}

func (f *confirmationFakeStore) eligibleCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listEligibleCalls
}

func (f *confirmationFakeStore) confirmCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.confirmWriteCalls
}

func (f *confirmationFakeStore) targetState(eventID int64, device string) models.TargetState {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.findTarget(eventID, device)
	if row == nil {
		return ""
	}
	return row.State
}

func (f *confirmationFakeStore) lastWrite() (int64, string, interfaces.UpdateCurtailmentTargetStateParams) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastConfirmEventID, f.lastConfirmDevice, f.lastConfirmWrite
}

func (f *confirmationFakeStore) findTarget(eventID int64, device string) *models.Target {
	for _, row := range f.targetsByEventID[eventID] {
		if row.DeviceIdentifier == device {
			return row
		}
	}
	return nil
}

// ListEligibleConfirmationTargets mirrors the real query's state filter:
// only rows whose target is still 'dispatched' are returned, so a target
// the pass just promoted disappears from the next read.
func (f *confirmationFakeStore) ListEligibleConfirmationTargets(context.Context) ([]models.ConfirmationTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listEligibleCalls++
	if f.listEligibleErr != nil {
		return nil, f.listEligibleErr
	}
	out := make([]models.ConfirmationTarget, 0, len(f.items))
	for _, item := range f.items {
		row := f.findTarget(item.EventID, item.DeviceIdentifier)
		if row == nil || row.State != models.TargetStateDispatched {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// UpdateTargetState enforces the fast-path guards (target state + phase
// batch UUID) the way the guarded SQL does, then delegates to the base
// fake's in-memory mirror.
func (f *confirmationFakeStore) UpdateTargetState(ctx context.Context, eventID int64, device string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmWriteCalls++
	f.lastConfirmEventID = eventID
	f.lastConfirmDevice = device
	f.lastConfirmWrite = params
	if params.ExpectedState != nil || params.ExpectedDispatchBatchUUID != nil {
		row := f.findTarget(eventID, device)
		if row == nil {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		if params.ExpectedState != nil && row.State != *params.ExpectedState {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		if params.ExpectedDispatchBatchUUID != nil {
			batch := phaseBatchUUIDForTest(row)
			if batch == nil || *batch != *params.ExpectedDispatchBatchUUID {
				return interfaces.ErrCurtailmentEventStateRaceLoss
			}
		}
	}
	return f.fakeStore.UpdateTargetState(ctx, eventID, device, params)
}

// phaseBatchUUIDForTest resolves the batch UUID column the real guard
// compares: restore_batch_uuid on restore-phase rows, curtail_batch_uuid
// otherwise.
func phaseBatchUUIDForTest(row *models.Target) *string {
	if row.DesiredState == models.DesiredStateActive && row.RestorePhase != nil {
		return row.RestorePhase.BatchUUID
	}
	return row.CurtailPhase.BatchUUID
}

// fakeSampler is an in-memory ConfirmationSampler. Results are keyed by
// device; requests are recorded for dedup/request assertions. A device
// without a fixture result errors, mirroring a failed read.
type fakeSampler struct {
	mu      sync.Mutex
	results map[string]telemetry.SampleResult
	calls   [][]telemetry.SampleRequest
	// panics makes the next N calls panic, exercising pass panic recovery.
	panics int
	// blockUntilCtxDone makes SampleDeviceMetrics return only after its
	// (pass) context is done, simulating sampling that consumes the whole
	// pass budget so confirmationPass observes an expired passCtx.
	blockUntilCtxDone bool
}

func newFakeSampler() *fakeSampler {
	return &fakeSampler{results: map[string]telemetry.SampleResult{}}
}

func (s *fakeSampler) setResult(device string, res telemetry.SampleResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[device] = res
}

func (s *fakeSampler) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *fakeSampler) SampleDeviceMetrics(ctx context.Context, requests []telemetry.SampleRequest) []telemetry.SampleResult {
	out, block := s.buildSampleResults(requests)
	if block {
		// Model sampling that burns the whole pass budget: return only once
		// the pass context expires, so the caller sees passCtx.Err() != nil
		// while its freshly derived write budget is still live.
		<-ctx.Done()
	}
	return out
}

func (s *fakeSampler) buildSampleResults(requests []telemetry.SampleRequest) ([]telemetry.SampleResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]telemetry.SampleRequest(nil), requests...))
	if s.panics > 0 {
		s.panics--
		panic("injected sampler panic")
	}
	seen := map[string]bool{}
	out := make([]telemetry.SampleResult, 0, len(requests))
	for _, req := range requests {
		device := string(req.DeviceID)
		if seen[device] {
			continue
		}
		seen[device] = true
		if res, ok := s.results[device]; ok {
			out = append(out, res)
			continue
		}
		out = append(out, telemetry.SampleResult{
			DeviceID: req.DeviceID,
			Source:   telemetry.SampleSourceDirect,
			Err:      errors.New("no fixture sample"),
		})
	}
	return out, s.blockUntilCtxDone
}

func confirmationSample(device string, powerW float64, flightStart time.Time) telemetry.SampleResult {
	return telemetry.SampleResult{
		DeviceID:    telemetryModels.DeviceIdentifier(device),
		FlightStart: flightStart,
		Source:      telemetry.SampleSourceDirect,
		Metrics: modelsV2.DeviceMetrics{
			DeviceIdentifier: device,
			Timestamp:        flightStart,
			Health:           modelsV2.HealthHealthyActive,
			PowerW:           &modelsV2.MetricValue{Value: powerW},
		},
	}
}

// fastPathTestNow is the fixed reconciler clock shared by these tests.
var fastPathTestNow = time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

func newFastPathReconcilerForTest(store *confirmationFakeStore, sampler ConfirmationSampler, metrics *recordingMetrics) *Reconciler {
	opts := []Option{WithConfirmationSampler(sampler)}
	if metrics != nil {
		opts = append(opts, WithMetrics(metrics))
	}
	r := New(Config{
		TickInterval:                time.Hour, // pass/loop driven directly by tests
		MaxRetries:                  3,
		CurtailMaxRetries:           3,
		DriftThresholdFactor:        0.5,
		ConfirmationFastPathEnabled: true,
	}, store, &fakeDispatcher{}, opts...)
	r.now = func() time.Time { return fastPathTestNow }
	r.confirmationPulse = 5 * time.Millisecond
	return r
}

// seedDispatchedWork installs an event plus one dispatched target and
// returns the matching eligibility item. desired selects the phase:
// DesiredStateCurtailed seeds curtail-phase work under an active event,
// DesiredStateActive seeds restore-phase work under a restoring event.
func seedDispatchedWork(store *confirmationFakeStore, eventID int64, device, desired, batch string, dispatchedAt time.Time) models.ConfirmationTarget {
	eventState := models.EventStateActive
	if desired == models.DesiredStateActive {
		eventState = models.EventStateRestoring
	}
	eventUUID := uuid.New()
	found := false
	for _, ev := range store.events {
		if ev.ID == eventID {
			eventUUID = ev.EventUUID
			found = true
			break
		}
	}
	if !found {
		store.events = append(store.events, &models.Event{
			ID: eventID, EventUUID: eventUUID, OrgID: 1, State: eventState,
		})
	}

	ts := dispatchedAt
	batchCopy := batch
	row := &models.Target{
		CurtailmentEventID: eventID,
		DeviceIdentifier:   device,
		State:              models.TargetStateDispatched,
		DesiredState:       desired,
		BaselinePowerW:     ptrFloat64(3000),
		LastDispatchedAt:   &ts,
		LastBatchUUID:      &batchCopy,
	}
	phase := models.TargetPhaseSummary{
		State:        models.TargetStateDispatched,
		DispatchedAt: &ts,
		BatchUUID:    &batchCopy,
	}
	if desired == models.DesiredStateActive {
		phase.Phase = models.TargetPhaseRestore
		row.RestorePhase = &phase
	} else {
		phase.Phase = models.TargetPhaseCurtail
		row.CurtailPhase = phase
	}
	store.targetsByEventID[eventID] = append(store.targetsByEventID[eventID], row)

	return models.ConfirmationTarget{
		EventID:          eventID,
		EventUUID:        eventUUID,
		OrgID:            1,
		EventState:       eventState,
		DeviceIdentifier: device,
		DesiredState:     desired,
		BaselinePowerW:   ptrFloat64(3000),
		DispatchedAt:     dispatchedAt,
		BatchUUID:        batch,
		PairingStatus:    "PAIRED",
	}
}

// --- confirmationPass: promotions ---

func TestConfirmationPass_ConfirmsCurtailTargetFromFreshSample(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	// 100W against a 3000W baseline (factor 0.5) is curtailed; flight
	// started after dispatch, so the evidence is fresh.
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-1"))

	eventID, device, params := store.lastWrite()
	assert.Equal(t, int64(10), eventID)
	assert.Equal(t, "miner-1", device)
	assert.Equal(t, models.TargetStateConfirmed, params.State)
	require.NotNil(t, params.ConfirmedAt)
	assert.Equal(t, fastPathTestNow, *params.ConfirmedAt)
	require.NotNil(t, params.RetryCount)
	assert.Equal(t, int32(0), *params.RetryCount, "confirmation must reset retry budget like the full tick")
	require.NotNil(t, params.ObservedPowerW)
	assert.Equal(t, float64(100), *params.ObservedPowerW)
	// Full single-winner guard set.
	require.NotNil(t, params.ExpectedEventState)
	assert.Equal(t, models.EventStateActive, *params.ExpectedEventState)
	require.NotNil(t, params.ExpectedDesiredState)
	assert.Equal(t, models.DesiredStateCurtailed, *params.ExpectedDesiredState)
	require.NotNil(t, params.ExpectedState)
	assert.Equal(t, models.TargetStateDispatched, *params.ExpectedState)
	require.NotNil(t, params.ExpectedDispatchBatchUUID)
	assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)

	// The promoted row leaves the eligibility read: the next pass parks.
	parked, failed = r.confirmationPass(context.Background())
	assert.True(t, parked)
	assert.False(t, failed)
}

func TestConfirmationPass_ResolvesRestoreTargetFromFreshSample(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 20, "miner-r", models.DesiredStateActive, "batch-restore", dispatchedAt)
	store.setItems(item)
	// 2800W against a 3000W baseline (restore threshold 1500W) is restored.
	sampler.setResult("miner-r", confirmationSample("miner-r", 2800, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, models.TargetStateResolved, store.targetState(20, "miner-r"))

	_, _, params := store.lastWrite()
	assert.Equal(t, models.TargetStateResolved, params.State)
	assert.Nil(t, params.RetryCount, "restore resolution does not touch retry budget")
	require.NotNil(t, params.ExpectedEventState)
	assert.Equal(t, models.EventStateRestoring, *params.ExpectedEventState)
	require.NotNil(t, params.ExpectedDesiredState)
	assert.Equal(t, models.DesiredStateActive, *params.ExpectedDesiredState)
	require.NotNil(t, params.ExpectedDispatchBatchUUID)
	assert.Equal(t, "batch-restore", *params.ExpectedDispatchBatchUUID)
}

// --- confirmationPass: evidence gates (KTD2: negative evidence is a no-op) ---

func TestConfirmationPass_NegativeEvidenceLeavesTargetUntouched(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	// Still at full power: the miner has not curtailed yet. The fast path
	// must not write anything — no retry burn, no timeout aging.
	sampler.setResult("miner-1", confirmationSample("miner-1", 2900, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked, "work remains eligible, pulse stays active")
	assert.False(t, failed)
	assert.Equal(t, 0, store.confirmCalls(), "negative evidence must not produce a write")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
}

func TestConfirmationPass_RestoreNegativeEvidenceLeavesTargetUntouched(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 20, "miner-r", models.DesiredStateActive, "batch-restore", dispatchedAt)
	store.setItems(item)
	// Restore phase, but the miner is still curtailed: a fresh sample well
	// below the restore threshold (100W against a 3000W baseline, threshold
	// 1500W) proves mining has NOT resumed. The pulse must not resolve it —
	// no promotion, no retry burn, no timeout aging (KTD2). Restore aging
	// stays with the full tick.
	sampler.setResult("miner-r", confirmationSample("miner-r", 100, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked, "work remains eligible, pulse stays active")
	assert.False(t, failed)
	assert.Equal(t, 0, store.confirmCalls(), "negative restore evidence must not produce a write")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(20, "miner-r"),
		"a below-threshold restore sample must leave the target dispatched")
}

func TestConfirmationPass_PreDispatchSampleNeverConfirms(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	// Curtailed-looking power, but the flight started exactly at dispatch
	// time — not strictly after — so it may predate the command (R3).
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, dispatchedAt))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, 0, store.confirmCalls(), "pre-dispatch evidence must never confirm")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
}

func TestConfirmationPass_UnpairedAllPairedPolicyDeviceSkipped(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	item.ForceIncludeAllPairedMiners = true
	item.PairingStatus = "UNPAIRED"
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, 0, store.confirmCalls(),
		"all-paired policy rows with a non-policy pairing status stay with the full tick")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
}

// --- confirmationPass: per-device failure isolation ---

func TestConfirmationPass_SampleErrorSkipsDeviceButConfirmsSiblings(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	bad := seedDispatchedWork(store, 10, "miner-bad", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	good := seedDispatchedWork(store, 10, "miner-good", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(bad, good)
	// miner-bad has no fixture result → per-device error; miner-good confirms.
	sampler.setResult("miner-good", confirmationSample("miner-good", 100, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed, "per-device sample errors are not pass failures")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-bad"))
	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-good"))
}

// --- confirmationPass: single-winner guards ---

func TestConfirmationPass_StaleBatchUUIDRaceLoses(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-old", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))

	// A timeout redispatch stamped a new batch UUID between the eligibility
	// read (item carries batch-old) and the guarded write.
	newBatch := "batch-new"
	store.targetsByEventID[10][0].CurtailPhase.BatchUUID = &newBatch

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed, "race loss is benign, not a pass failure")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"),
		"stale-batch confirmation must lose to the redispatched row")
	assert.Equal(t, 1, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount(), "race loss is not a write failure")
}

func TestConfirmationPass_WriteFailureCountsAndSkips(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))
	store.updateTargetStateErr = errors.New("injected write failure")

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed, "a single failed write does not fail the pass")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
	assert.Equal(t, 1, metrics.TargetWriteFailureCount())
	assert.Equal(t, 0, metrics.EventStateRaceLossCount())
}

// --- confirmationPass: split pass/write budget (review #3/#4) ---

// A pass whose sampling exhausts the pass budget must still land the samples
// that already succeeded, promoting them under a fresh write budget derived
// from the live parent context.
func TestConfirmationPass_ExpiredPassBudgetStillPromotesSuccessfulSamples(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))
	// The sampler returns only after the pass budget expires, so the per-item
	// loop runs with passCtx already dead but the parent context still live.
	sampler.blockUntilCtxDone = true

	r := newFastPathReconcilerForTest(store, sampler, nil)
	r.confirmationPassTimeout = 20 * time.Millisecond

	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.True(t, failed,
		"a timed-out sampling still reports failed=true so the unsampled remainder backs off")
	assert.Equal(t, 1, store.confirmCalls(),
		"an early-successful sample must promote even though the pass budget expired")
	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-1"))
}

// When the write budget itself is expired (here: a cancelled parent context,
// as on Stop), the per-item loop bails before promoting anything.
func TestConfirmationPass_ExpiredWriteBudgetBailsEarly(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	a := seedDispatchedWork(store, 10, "miner-a", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	b := seedDispatchedWork(store, 10, "miner-b", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(a, b)
	sampler.setResult("miner-a", confirmationSample("miner-a", 100, fastPathTestNow.Add(-time.Second)))
	sampler.setResult("miner-b", confirmationSample("miner-b", 100, fastPathTestNow.Add(-time.Second)))

	// A cancelled parent expires both the pass budget and the write budget
	// derived from it, so no item is promoted (0 of 2 eligible items).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(ctx)

	assert.False(t, parked)
	assert.True(t, failed)
	assert.Equal(t, 0, store.confirmCalls(),
		"an expired write budget must bail before promoting any of the eligible items")
}

// --- confirmationPass: eligibility read outcomes ---

func TestConfirmationPass_EmptyEligibilityParks(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.True(t, parked)
	assert.False(t, failed)
	assert.Equal(t, 0, sampler.callCount(), "no work, no sampling")
}

func TestConfirmationPass_EligibilityErrorFailsPass(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	store.setListEligibleErr(errors.New("injected read failure"))

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked, "a failed read must not park the pulse")
	assert.True(t, failed)
	assert.Equal(t, 0, sampler.callCount())
	assert.Equal(t, 1, metrics.ConfirmationPassFailureCount(),
		"an eligibility-read failure must increment the pass-failure metric (mirrors IncTickFailure)")
}

func TestSafeConfirmationPass_RecoversPanicAsFailure(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.panics = 1

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.safeConfirmationPass(context.Background())

	assert.False(t, parked)
	assert.True(t, failed, "a recovered panic counts as a failed pass for backoff")
	assert.Equal(t, 1, metrics.ConfirmationPassFailureCount(),
		"a recovered panic must increment the pass-failure metric")
}

// --- pulse lifecycle: park, wake, re-park, failure backoff ---

// startConfirmationLoop runs the pulse goroutine in isolation (no tick
// loop) and returns a stop func that cancels it and waits for exit.
func startConfirmationLoop(r *Reconciler) (stop func()) {
	stopCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.confirmationLoop(stopCtx, context.Background())
	}()
	return func() {
		cancel()
		<-done
	}
}

func TestConfirmationLoop_ParksWithoutWorkAndConfirmsAfterWake(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	r := newFastPathReconcilerForTest(store, sampler, nil)

	stop := startConfirmationLoop(r)
	defer stop()

	// Wake with no eligible work: exactly one read, then parked again.
	r.wakeConfirmation()
	require.Eventually(t, func() bool { return store.eligibleCalls() == 1 },
		2*time.Second, time.Millisecond)
	time.Sleep(30 * time.Millisecond) // several pulse intervals
	assert.Equal(t, 1, store.eligibleCalls(), "parked pulse must do zero periodic work")

	// Seed dispatched work and wake: the pulse confirms it, then the row
	// leaves eligibility and the pulse parks again.
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))
	r.wakeConfirmation()

	require.Eventually(t, func() bool {
		return store.targetState(10, "miner-1") == models.TargetStateConfirmed
	}, 2*time.Second, time.Millisecond)

	// Parked again: the eligibility call count stabilizes.
	var settled int
	require.Eventually(t, func() bool {
		calls := store.eligibleCalls()
		if calls == settled {
			return true
		}
		settled = calls
		return false
	}, 2*time.Second, 20*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, settled, store.eligibleCalls(), "pulse must re-park after the last row confirms")
}

func TestConfirmationLoop_RetriesFailedPassesThenRecovers(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	store.setListEligibleErr(errors.New("injected read failure"))
	r := newFastPathReconcilerForTest(store, sampler, nil)

	stop := startConfirmationLoop(r)
	defer stop()

	// Failed passes keep the pulse active (with backoff), not parked.
	r.wakeConfirmation()
	require.Eventually(t, func() bool { return store.eligibleCalls() >= 3 },
		5*time.Second, time.Millisecond, "failed passes must retry")

	// Recovery: the read starts succeeding with work present; the pulse
	// confirms it and parks.
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))
	store.setListEligibleErr(nil)

	require.Eventually(t, func() bool {
		return store.targetState(10, "miner-1") == models.TargetStateConfirmed
	}, 5*time.Second, time.Millisecond)
}

// --- wiring: tick wakes, Start/Stop lifecycle, disabled mode ---

func TestRunTick_DispatchedWorkWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	effBatch := int32(2)
	eventID := int64(10)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &effBatch, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	assert.Len(t, r.confirmationWake, 1,
		"a tick that leaves targets dispatched must wake the confirmation pulse")
}

func TestRunTick_NoDispatchedWorkLeavesPulseParked(t *testing.T) {
	store := newFakeStore()
	r := newReconcilerForTest(store, &fakeDispatcher{})
	r.runTick(context.Background())
	assert.Empty(t, r.confirmationWake, "nothing dispatched, no wake")
}

func TestObserveActive_DispatchedWorkWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	eventID := int64(10)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) // == newReconcilerForTest clock
	store.events = []*models.Event{
		{ID: eventID, EventUUID: uuid.New(), OrgID: 1, State: models.EventStateActive},
	}
	// A drifted-then-redispatched target re-entering the active phase in
	// dispatched: freshly dispatched with not-yet-curtailed telemetry, so the
	// tick observes it without confirming or aging and leaves it dispatched.
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &now,
			CurtailPhase: models.TargetPhaseSummary{
				Phase:        models.TargetPhaseCurtail,
				State:        models.TargetStateDispatched,
				DispatchedAt: &now,
			},
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	assert.Len(t, r.confirmationWake, 1,
		"an active-phase tick that leaves a target dispatched must wake the confirmation pulse")
}

func TestObserveRestoring_DispatchedWorkWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	eventID := int64(30)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: uuid.New(), OrgID: 1, State: models.EventStateRestoring},
	}
	// A freshly dispatched restore target whose telemetry has not yet crossed
	// the restore threshold, so the tick leaves it dispatched.
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-r",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateActive,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &now,
			RestorePhase: &models.TargetPhaseSummary{
				Phase:        models.TargetPhaseRestore,
				State:        models.TargetStateDispatched,
				DispatchedAt: &now,
			},
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-r", LatestPowerW: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	assert.Len(t, r.confirmationWake, 1,
		"a restoring-phase tick that leaves a restore target dispatched must wake the pulse")
}

func TestRunTick_ClaimedClosedLoopTargetWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	eventID := int64(40)
	store.events = []*models.Event{
		{
			ID:              eventID,
			EventUUID:       uuid.New(),
			OrgID:           1,
			State:           models.EventStateActive,
			Mode:            models.ModeFullFleet,
			LoopType:        models.LoopTypeClosed,
			ScopeType:       models.ScopeTypeWholeOrg,
			CreatedByUserID: 99,
		},
	}
	// No pre-existing targets: the only dispatched work this tick is the
	// dynamically-claimed miner-new, which is a separate slice the deferred
	// wakeIfDispatchedWork(targets) never sees. The explicit claimed-work wake
	// (review #7) is what must fire here.
	driverName := "antminer"
	now := time.Now()
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-new",
			DriverName:       &driverName,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(100),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	require.Len(t, store.targetsByEventID[eventID], 1, "the closed-loop target was claimed")
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Len(t, r.confirmationWake, 1,
		"a tick that dispatches only a dynamically-claimed target must wake the pulse (review #7)")
}

func TestStart_FastPathEnabledRequiresSampler(t *testing.T) {
	r := New(Config{
		TickInterval:                time.Hour,
		ConfirmationFastPathEnabled: true,
	}, newFakeStore(), &fakeDispatcher{})

	err := r.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sampler")
}

func TestStart_RunsStartupRecoveryPass(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	// Rows already dispatched before startup (e.g. crash recovery).
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(-time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	require.NoError(t, r.Start(context.Background()))
	defer func() { require.NoError(t, r.Stop(context.Background())) }()

	// No tick ran (interval 1h); the initial wake alone must confirm.
	require.Eventually(t, func() bool {
		return store.targetState(10, "miner-1") == models.TargetStateConfirmed
	}, 2*time.Second, time.Millisecond,
		"startup recovery must confirm pre-existing dispatched rows without a tick")
}

func TestStart_DisabledFastPathRunsNoPulse(t *testing.T) {
	store := newConfirmationFakeStore()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)

	r := New(Config{
		TickInterval:         time.Hour,
		MaxRetries:           3,
		CurtailMaxRetries:    3,
		DriftThresholdFactor: 0.5,
		// ConfirmationFastPathEnabled deliberately false; no sampler needed.
	}, store, &fakeDispatcher{})
	require.NoError(t, r.Start(context.Background()))
	defer func() { require.NoError(t, r.Stop(context.Background())) }()

	// Wakes are inert when disabled: nothing consumes them and no
	// eligibility read ever runs.
	r.wakeConfirmation()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, store.eligibleCalls(),
		"disabled fast path must never touch the eligibility read")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
}

// --- stale full-tick observations vs. pulse promotions ---

type dispatchedObservationFailureScenario struct {
	name           string
	desiredState   string
	pulseState     models.TargetState
	pulseRetry     int32
	staleRetry     int32
	invalidPairing bool
	failureState   models.TargetState
}

var dispatchedObservationFailureScenarios = []dispatchedObservationFailureScenario{
	{
		name:         "curtail candidate missing",
		desiredState: models.DesiredStateCurtailed,
		pulseState:   models.TargetStateConfirmed,
		staleRetry:   1,
		failureState: models.TargetStateDispatched,
	},
	{
		name:           "curtail pairing invalid",
		desiredState:   models.DesiredStateCurtailed,
		pulseState:     models.TargetStateConfirmed,
		staleRetry:     1,
		invalidPairing: true,
		failureState:   models.TargetStateDispatched,
	},
	{
		name:         "restore candidate missing",
		desiredState: models.DesiredStateActive,
		pulseState:   models.TargetStateResolved,
		pulseRetry:   2,
		staleRetry:   2,
		failureState: models.TargetStatePending,
	},
}

func runDispatchedObservationFailure(
	r *Reconciler,
	ev *models.Event,
	target *models.Target,
	scenario dispatchedObservationFailureScenario,
) {
	var candidate *models.Candidate
	if scenario.invalidPairing {
		candidate = &models.Candidate{
			DeviceIdentifier: target.DeviceIdentifier,
			PairingStatus:    "UNPAIRED",
		}
	}
	if scenario.desiredState == models.DesiredStateActive {
		r.confirmOneRestore(context.Background(), ev, target, candidate)
		return
	}
	r.confirmOneDispatched(context.Background(), ev, target, candidate, scenario.failureState)
}

func TestDispatchedObservationFailure_RaceLosesToPulseAdvance(t *testing.T) {
	for _, scenario := range dispatchedObservationFailureScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Minute)
			item := seedDispatchedWork(store, 10, "miner-1", scenario.desiredState, "batch-a", dispatchedAt)
			durable := store.targetsByEventID[10][0]
			stale := *durable
			stale.RetryCount = scenario.staleRetry

			// The pulse advanced the durable row after the tick loaded its
			// dispatched snapshot.
			durable.State = scenario.pulseState
			durable.RetryCount = scenario.pulseRetry

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			ev := &models.Event{
				ID:                          10,
				EventUUID:                   item.EventUUID,
				OrgID:                       1,
				State:                       item.EventState,
				ForceIncludeAllPairedMiners: scenario.invalidPairing,
			}
			runDispatchedObservationFailure(r, ev, &stale, scenario)

			assert.Equal(t, scenario.pulseState, store.targetState(10, "miner-1"))
			assert.Equal(t, scenario.pulseRetry, durable.RetryCount,
				"pulse-updated retry budget must survive the stale failure write")
			assert.Equal(t, models.TargetStateDispatched, stale.State,
				"stale snapshot must not mutate on race-loss")
			assert.Equal(t, scenario.staleRetry, stale.RetryCount,
				"stale snapshot retry budget must not advance on race-loss")
			assert.Equal(t, 1, metrics.EventStateRaceLossCount())
			assert.Equal(t, 0, metrics.TargetWriteFailureCount())

			_, _, params := store.lastWrite()
			require.NotNil(t, params.ExpectedState)
			assert.Equal(t, models.TargetStateDispatched, *params.ExpectedState)
			require.NotNil(t, params.ExpectedDispatchBatchUUID)
			assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
		})
	}
}

func TestDispatchedObservationFailure_ProceedsWhenStillDispatched(t *testing.T) {
	for _, scenario := range dispatchedObservationFailureScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Minute)
			item := seedDispatchedWork(store, 10, "miner-1", scenario.desiredState, "batch-a", dispatchedAt)
			target := store.targetsByEventID[10][0]
			target.RetryCount = 1

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			ev := &models.Event{
				ID:                          10,
				EventUUID:                   item.EventUUID,
				OrgID:                       1,
				State:                       item.EventState,
				ForceIncludeAllPairedMiners: scenario.invalidPairing,
			}
			runDispatchedObservationFailure(r, ev, target, scenario)

			assert.Equal(t, scenario.failureState, store.targetState(10, "miner-1"))
			assert.Equal(t, int32(2), target.RetryCount,
				"a still-dispatched failure must consume one retry")
			assert.Equal(t, 0, metrics.EventStateRaceLossCount())
			assert.Equal(t, 0, metrics.TargetWriteFailureCount())
		})
	}
}

// --- tick timeout aging vs. a pulse-confirmed target (review #6) ---

// A dispatch-timeout aging write must not clobber a target the confirmation
// pulse already confirmed. The tick acts on a stale in-memory snapshot that
// still says dispatched; the guarded write must race-lose against the durable
// row the pulse advanced, leaving state and retry budget intact.
func TestConfirmOneDispatched_TimeoutAgingRaceLosesToPulseConfirmation(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	// Dispatched an hour ago: well past the 5s curtail dispatch timeout.
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)

	// The pulse already confirmed the target: advance the durable store row
	// out of dispatched (batch UUID unchanged).
	store.targetsByEventID[10][0].State = models.TargetStateConfirmed

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)

	// The tick still holds a stale snapshot: dispatched, retry budget spent once.
	dispatched := dispatchedAt
	batch := "batch-a"
	stale := &models.Target{
		CurtailmentEventID: 10,
		DeviceIdentifier:   "miner-1",
		State:              models.TargetStateDispatched,
		DesiredState:       models.DesiredStateCurtailed,
		BaselinePowerW:     ptrFloat64(3000),
		LastDispatchedAt:   &dispatched,
		LastBatchUUID:      &batch,
		RetryCount:         1,
		CurtailPhase: models.TargetPhaseSummary{
			Phase:        models.TargetPhaseCurtail,
			State:        models.TargetStateDispatched,
			DispatchedAt: &dispatched,
			BatchUUID:    &batch,
		},
	}
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateActive}
	// Telemetry still shows full power, so the tick enters the timeout-aging branch.
	cand := &models.Candidate{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100)}

	r.confirmOneDispatched(context.Background(), ev, stale, cand, models.TargetStateDispatching)

	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-1"),
		"pulse-confirmed target must survive the stale timeout-aging write")
	assert.Equal(t, models.TargetStateDispatched, stale.State, "stale snapshot must not mutate on race-loss")
	assert.Equal(t, int32(1), stale.RetryCount, "retry budget must not be burned on race-loss")
	assert.Equal(t, 1, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount(), "a race-loss is not a write failure")
}

// The restore variant: a pulse-resolved restore target at the retry ceiling
// must not be reverted to RESTORE_FAILED by the tick's stale timeout aging.
func TestConfirmOneRestore_TimeoutAgingRaceLosesToPulseResolution(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	// Dispatched an hour ago: past the 30s restore dispatch timeout.
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 20, "miner-r", models.DesiredStateActive, "batch-r", dispatchedAt)

	// The pulse already resolved it.
	store.targetsByEventID[20][0].State = models.TargetStateResolved

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)

	// Stale snapshot at the retry ceiling: newRetry hits MaxRetries, so
	// unguarded aging would terminalize a genuinely-restored miner.
	dispatched := dispatchedAt
	batch := "batch-r"
	stale := &models.Target{
		CurtailmentEventID: 20,
		DeviceIdentifier:   "miner-r",
		State:              models.TargetStateDispatched,
		DesiredState:       models.DesiredStateActive,
		BaselinePowerW:     ptrFloat64(3000),
		LastDispatchedAt:   &dispatched,
		LastBatchUUID:      &batch,
		RetryCount:         r.cfg.MaxRetries - 1,
		RestorePhase: &models.TargetPhaseSummary{
			Phase:        models.TargetPhaseRestore,
			State:        models.TargetStateDispatched,
			DispatchedAt: &dispatched,
			BatchUUID:    &batch,
		},
	}
	ev := &models.Event{ID: 20, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateRestoring}
	// Still below the restore threshold, so the tick enters timeout aging.
	cand := &models.Candidate{DeviceIdentifier: "miner-r", LatestPowerW: ptrFloat64(100)}

	r.confirmOneRestore(context.Background(), ev, stale, cand)

	assert.Equal(t, models.TargetStateResolved, store.targetState(20, "miner-r"),
		"pulse-resolved restore must not be reverted (esp. not to RESTORE_FAILED) by stale timeout aging")
	assert.Equal(t, models.TargetStateDispatched, stale.State)
	assert.Equal(t, r.cfg.MaxRetries-1, stale.RetryCount, "retry budget must survive the race-loss")
	assert.Equal(t, 1, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount())
}

// A state-only timeout guard is vulnerable to ABA: another fleetd can age
// batch A, redispatch batch B, and return the durable row to dispatched before
// the stale batch-A tick writes. The batch UUID guard must reject that stale
// aging write in both dispatch directions.
func TestTimeoutAging_StaleBatchRaceLosesToRedispatch(t *testing.T) {
	tests := []struct {
		name         string
		desiredState string
	}{
		{
			name:         "curtail redispatch",
			desiredState: models.DesiredStateCurtailed,
		},
		{
			name:         "restore redispatch",
			desiredState: models.DesiredStateActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Hour)
			item := seedDispatchedWork(store, 10, "miner-1", tt.desiredState, "batch-a", dispatchedAt)

			// Preserve the batch-A snapshot, including a deep copy of the
			// restore phase pointer, before advancing the durable row to batch B.
			durable := store.targetsByEventID[10][0]
			stale := *durable
			if durable.RestorePhase != nil {
				restorePhase := *durable.RestorePhase
				stale.RestorePhase = &restorePhase
			}
			batchB := "batch-b"
			durable.LastBatchUUID = &batchB
			if tt.desiredState == models.DesiredStateActive {
				durable.RestorePhase.BatchUUID = &batchB
			} else {
				durable.CurtailPhase.BatchUUID = &batchB
			}

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			if tt.desiredState == models.DesiredStateActive {
				stale.RetryCount = r.cfg.MaxRetries - 1
			}
			ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: item.EventState}
			cand := &models.Candidate{
				DeviceIdentifier: "miner-1",
				LatestPowerW:     ptrFloat64(2900),
				LatestHashRateHS: ptrFloat64(100),
			}
			if tt.desiredState == models.DesiredStateActive {
				cand.LatestPowerW = ptrFloat64(100)
				r.confirmOneRestore(context.Background(), ev, &stale, cand)
			} else {
				r.confirmOneDispatched(context.Background(), ev, &stale, cand, models.TargetStateDispatching)
			}

			assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"),
				"batch-B dispatch must survive stale batch-A timeout aging")
			assert.Equal(t, int32(0), durable.RetryCount, "batch-B retry budget must remain untouched")
			assert.Equal(t, 1, metrics.EventStateRaceLossCount())
			assert.Equal(t, 0, metrics.TargetWriteFailureCount())
			_, _, params := store.lastWrite()
			require.NotNil(t, params.ExpectedDispatchBatchUUID)
			assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
		})
	}
}

func TestTimeoutAging_MissingLoadedBatchRetainsStateGuard(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	stale := *store.targetsByEventID[10][0]
	stale.LastBatchUUID = nil
	stale.RetryCount = 1

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateActive}
	cand := &models.Candidate{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100)}

	r.confirmOneDispatched(context.Background(), ev, &stale, cand, models.TargetStateDispatching)

	assert.Equal(t, 1, store.confirmCalls())
	assert.Equal(t, models.TargetStateDispatching, store.targetState(10, "miner-1"),
		"a legacy dispatched row without a batch token must still age out")
	assert.Equal(t, int32(2), stale.RetryCount)
	assert.Equal(t, 0, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount())
	_, _, params := store.lastWrite()
	require.NotNil(t, params.ExpectedState)
	assert.Nil(t, params.ExpectedDispatchBatchUUID)
}

// Positive control: when the target is still dispatched (no pulse race), the
// dispatched-state and batch guards are transparent and normal timeout aging
// proceeds.
func TestConfirmOneDispatched_TimeoutAgingProceedsWhenStillDispatched(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
	// Use the durable row as the tick's snapshot: it is still dispatched, so
	// the guard matches and the aging write applies.
	stale := store.targetsByEventID[10][0]
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateActive}
	cand := &models.Candidate{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100)}

	r.confirmOneDispatched(context.Background(), ev, stale, cand, models.TargetStateDispatching)

	assert.Equal(t, models.TargetStateDispatching, store.targetState(10, "miner-1"),
		"a still-dispatched target ages normally through the guarded write")
	assert.Equal(t, int32(1), store.targetsByEventID[10][0].RetryCount, "normal aging burns one retry")
	assert.Equal(t, 0, metrics.EventStateRaceLossCount(), "no race when the target is still dispatched")
	_, _, params := store.lastWrite()
	require.NotNil(t, params.ExpectedDispatchBatchUUID)
	assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
}
