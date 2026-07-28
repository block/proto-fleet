package reconciler

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	items               []models.ConfirmationTarget
	listEligibleErr     error
	listEligibleCalls   int
	listEligibleCursors []interfaces.ConfirmationPageCursor
	confirmWriteCalls   int
	bulkConfirmCalls    int
	lastBulkUpdateSize  int
	lastConfirmWrite    interfaces.UpdateCurtailmentTargetStateParams
	lastConfirmDevice   string
	lastConfirmEventID  int64
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

func (f *confirmationFakeStore) eligibleCursors() []interfaces.ConfirmationPageCursor {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]interfaces.ConfirmationPageCursor(nil), f.listEligibleCursors...)
}

func (f *confirmationFakeStore) confirmCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.confirmWriteCalls
}

func (f *confirmationFakeStore) bulkCalls() (calls, lastSize int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bulkConfirmCalls, f.lastBulkUpdateSize
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
func (f *confirmationFakeStore) ListEligibleConfirmationTargets(
	_ context.Context,
	cursor interfaces.ConfirmationPageCursor,
) ([]models.ConfirmationTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listEligibleCalls++
	f.listEligibleCursors = append(f.listEligibleCursors, cursor)
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].EventID != out[j].EventID {
			return out[i].EventID < out[j].EventID
		}
		return out[i].DeviceIdentifier < out[j].DeviceIdentifier
	})
	start := sort.Search(len(out), func(i int) bool {
		return out[i].EventID > cursor.AfterEventID ||
			(out[i].EventID == cursor.AfterEventID &&
				out[i].DeviceIdentifier > cursor.AfterDeviceIdentifier)
	})
	out = out[start:]
	if len(out) > interfaces.ConfirmationBatchSize {
		out = out[:interfaces.ConfirmationBatchSize]
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

func (f *confirmationFakeStore) BulkConfirmTargets(
	ctx context.Context,
	eventID int64,
	expectedEventState models.EventState,
	updates []interfaces.ConfirmationUpdate,
) (interfaces.ConfirmationBulkResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bulkConfirmCalls++
	f.lastBulkUpdateSize = len(updates)
	var applied []string
	for _, update := range updates {
		f.confirmWriteCalls++
		desiredState := models.DesiredStateCurtailed
		targetState := models.TargetStateConfirmed
		if update.Phase == models.TargetPhaseRestore {
			desiredState = models.DesiredStateActive
			targetState = models.TargetStateResolved
		}
		row := f.findTarget(eventID, update.DeviceIdentifier)
		if row == nil || row.State != models.TargetStateDispatched ||
			row.DesiredState != desiredState {
			continue
		}
		batch := phaseBatchUUIDForTest(row)
		if batch == nil || *batch != update.BatchUUID {
			continue
		}
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:                     targetState,
			ObservedPowerW:            update.ObservedPowerW,
			ObservedAt:                &update.ObservedAt,
			ConfirmedAt:               &update.ConfirmedAt,
			ExpectedEventState:        &expectedEventState,
			ExpectedDesiredState:      &desiredState,
			ExpectedDispatchBatchUUID: &update.BatchUUID,
		}
		expectedState := models.TargetStateDispatched
		params.ExpectedState = &expectedState
		if update.Phase == models.TargetPhaseCurtail {
			zero := int32(0)
			params.RetryCount = &zero
		}
		f.lastConfirmEventID = eventID
		f.lastConfirmDevice = update.DeviceIdentifier
		f.lastConfirmWrite = params
		if err := f.fakeStore.UpdateTargetState(ctx, eventID, update.DeviceIdentifier, params); err != nil {
			if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				continue
			}
			return interfaces.ConfirmationBulkResult{}, err
		}
		applied = append(applied, update.DeviceIdentifier)
	}
	sampleSize := min(len(applied), confirmationLogSampleSize)
	return interfaces.ConfirmationBulkResult{
		AppliedCount:            len(applied),
		SampleDeviceIdentifiers: applied[:sampleSize],
	}, nil
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
	omitted map[string]bool
	calls   [][]telemetry.SampleRequest
	// panics makes the next N calls panic, exercising pass panic recovery.
	panics int
	// blockUntilCtxDone makes SampleDeviceMetrics return only after its
	// (pass) context is done, simulating sampling that consumes the whole
	// pass budget so confirmationPass observes an expired passCtx.
	blockUntilCtxDone bool
}

func newFakeSampler() *fakeSampler {
	return &fakeSampler{
		results: map[string]telemetry.SampleResult{},
		omitted: map[string]bool{},
	}
}

func (s *fakeSampler) setResult(device string, res telemetry.SampleResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[device] = res
}

func (s *fakeSampler) omitResult(device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.omitted[device] = true
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
		if s.omitted[device] {
			continue
		}
		if res, ok := s.results[device]; ok {
			res.OrgID = req.OrgID
			out = append(out, res)
			continue
		}
		out = append(out, telemetry.SampleResult{
			DeviceID: req.DeviceID,
			OrgID:    req.OrgID,
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
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))

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
	sampler.setResult("miner-r", confirmationSample("miner-r", 2800, fastPathTestNow.Add(time.Second)))

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
	sampler.setResult("miner-1", confirmationSample("miner-1", 2900, fastPathTestNow.Add(time.Second)))

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
	sampler.setResult("miner-r", confirmationSample("miner-r", 100, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked, "work remains eligible, pulse stays active")
	assert.False(t, failed)
	assert.Equal(t, 0, store.confirmCalls(), "negative restore evidence must not produce a write")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(20, "miner-r"),
		"a below-threshold restore sample must leave the target dispatched")
}

func TestConfirmationPass_PreEligibilitySampleNeverConfirms(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	// Curtailed-looking power, but the flight started before the local
	// post-eligibility boundary, so it cannot prove post-dispatch ordering.
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
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, 0, store.confirmCalls(),
		"all-paired policy rows with a non-policy pairing status stay with the full tick")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
}

func TestConfirmationPass_UnpairedAllPairedPolicyRestoreResolves(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 20, "miner-r", models.DesiredStateActive, "batch-restore", dispatchedAt)
	item.ForceIncludeAllPairedMiners = true
	item.PairingStatus = "UNPAIRED"
	store.setItems(item)
	sampler.setResult("miner-r", confirmationSample("miner-r", 2800, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, 1, store.confirmCalls(),
		"restore evidence must be evaluated even when pairing changed after dispatch")
	assert.Equal(t, models.TargetStateResolved, store.targetState(20, "miner-r"))
}

// --- confirmationPass: per-device failure isolation ---

func TestConfirmationPass_SampleErrorSkipsDeviceButConfirmsSiblings(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	bad := seedDispatchedWork(store, 10, "miner-bad", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	good := seedDispatchedWork(store, 10, "miner-good", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(bad, good)
	// miner-bad has no fixture result → per-device error; miner-good confirms.
	sampler.setResult("miner-good", confirmationSample("miner-good", 100, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed, "one of two sample errors is not a strict majority")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-bad"))
	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-good"))
	assert.Equal(t, 0, metrics.ConfirmationPassFailureCount())
}

func TestConfirmationPass_MajoritySampleErrorsFailPassButConfirmSibling(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	badA := seedDispatchedWork(store, 10, "miner-bad-a", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	badB := seedDispatchedWork(store, 10, "miner-bad-b", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	good := seedDispatchedWork(store, 10, "miner-good", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(badA, badB, good)
	sampler.setResult("miner-good", confirmationSample("miner-good", 100, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.True(t, failed, "two of three sample errors must trigger loop backoff")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-bad-a"))
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-bad-b"))
	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-good"),
		"successful siblings must still promote during a failed pass")
	assert.Equal(t, 1, metrics.ConfirmationPassFailureCount(),
		"a failed pass increments the pass-failure metric once")
}

func TestConfirmationPass_MajorityOmittedResultsFailPassButConfirmSibling(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	omittedA := seedDispatchedWork(store, 10, "miner-omitted-a", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	omittedB := seedDispatchedWork(store, 10, "miner-omitted-b", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	good := seedDispatchedWork(store, 10, "miner-good", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(omittedA, omittedB, good)
	sampler.omitResult("miner-omitted-a")
	sampler.omitResult("miner-omitted-b")
	sampler.setResult("miner-good", confirmationSample("miner-good", 100, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.True(t, failed, "two of three omitted results must trigger loop backoff")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-omitted-a"))
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-omitted-b"))
	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-good"),
		"successful siblings must still promote during a failed pass")
	assert.Equal(t, 1, metrics.ConfirmationPassFailureCount(),
		"a failed pass increments the pass-failure metric once")
}

func TestConfirmationPass_BoundsFleetWavePageAndBulkWrite(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	const targetCount = interfaces.ConfirmationBatchSize + 1
	items := make([]models.ConfirmationTarget, 0, targetCount)
	for i := range targetCount {
		deviceID := fmt.Sprintf("miner-%04d", i)
		items = append(items, seedDispatchedWork(
			store, 10, deviceID, models.DesiredStateCurtailed, "batch-a", dispatchedAt,
		))
		sampler.setResult(deviceID, confirmationSample(deviceID, 100, fastPathTestNow.Add(time.Second)))
	}
	store.setItems(items...)

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.False(t, failed)
	calls, size := store.bulkCalls()
	assert.Equal(t, 1, calls)
	assert.Equal(t, interfaces.ConfirmationBatchSize, size)
	assert.Equal(t, models.TargetStateDispatched,
		store.targetState(10, fmt.Sprintf("miner-%04d", interfaces.ConfirmationBatchSize)),
		"one target must remain outside the bounded eligibility page")
}

func TestConfirmationPass_RotatesPastDegradedNonconfirmingPage(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	const targetCount = interfaces.ConfirmationBatchSize + 1
	items := make([]models.ConfirmationTarget, 0, targetCount)
	for i := range targetCount {
		deviceID := fmt.Sprintf("miner-%04d", i)
		items = append(items, seedDispatchedWork(
			store, 10, deviceID, models.DesiredStateCurtailed, "batch-a", dispatchedAt,
		))
		if i <= interfaces.ConfirmationBatchSize/2 {
			continue // Missing fixture simulates an offline miner/sample error.
		}
		powerW := 2900.0
		if i == interfaces.ConfirmationBatchSize {
			powerW = 100
		}
		sampler.setResult(deviceID, confirmationSample(deviceID, powerW, fastPathTestNow.Add(time.Second)))
	}
	store.setItems(items...)

	r := newFastPathReconcilerForTest(store, sampler, nil)
	parked, failed := r.confirmationPass(context.Background())
	require.False(t, parked)
	require.True(t, failed, "a strict-majority sample failure must back off without pinning the cursor")

	parked, failed = r.confirmationPass(context.Background())
	assert.False(t, parked)
	assert.False(t, failed)
	assert.Equal(t, models.TargetStateConfirmed,
		store.targetState(10, fmt.Sprintf("miner-%04d", interfaces.ConfirmationBatchSize)),
		"a degraded nonconfirming first page must not monopolize every pulse")

	parked, failed = r.confirmationPass(context.Background())
	assert.False(t, parked, "wrapping must retry the degraded first page instead of parking")
	assert.True(t, failed, "the retried degraded page must retain its strict-majority failure")
	assert.Equal(t, 3, sampler.callCount(), "the wrapped pass must sample the first page again")
	cursors := store.eligibleCursors()
	require.Len(t, cursors, 4)
	assert.Equal(t, []interfaces.ConfirmationPageCursor{
		{
			AfterEventID:          10,
			AfterDeviceIdentifier: fmt.Sprintf("miner-%04d", interfaces.ConfirmationBatchSize),
		},
		{},
	}, cursors[2:], "the wrapped pass must read the empty tail, then retry from the zero cursor")
}

// --- confirmationPass: single-winner guards ---

func TestConfirmationPass_StaleBatchUUIDRaceLoses(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-old", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))

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
	assert.Equal(t, 0, metrics.ConfirmationPassFailureCount(), "race loss must not trigger backoff")
}

func TestConfirmationPass_BulkWriteFailuresFailPassOnceForBackoff(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	first := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	second := seedDispatchedWork(store, 20, "miner-2", models.DesiredStateCurtailed, "batch-b", dispatchedAt)
	store.setItems(first, second)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))
	sampler.setResult("miner-2", confirmationSample("miner-2", 100, fastPathTestNow.Add(time.Second)))
	store.updateTargetStateErr = errors.New("injected write failure")

	r := newFastPathReconcilerForTest(store, sampler, metrics)
	parked, failed := r.confirmationPass(context.Background())

	assert.False(t, parked)
	assert.True(t, failed, "any failed bulk-write chunk must trigger loop backoff")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
	assert.Equal(t, models.TargetStateDispatched, store.targetState(20, "miner-2"))
	assert.Equal(t, 2, metrics.TargetWriteFailureCount(), "each failed chunk retains its write-failure signal")
	assert.Equal(t, 1, metrics.ConfirmationPassFailureCount(),
		"multiple failed chunks still increment the pass-failure metric once")
	assert.Equal(t, 0, metrics.EventStateRaceLossCount())
}

// --- confirmationPass: split pass/write budget ---

// A pass whose sampling exhausts the pass budget must still land the samples
// that already succeeded, promoting them under a fresh write budget derived
// from the live parent context.
func TestConfirmationPass_ExpiredPassBudgetStillPromotesSuccessfulSamples(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))
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
	sampler.setResult("miner-a", confirmationSample("miner-a", 100, fastPathTestNow.Add(time.Second)))
	sampler.setResult("miner-b", confirmationSample("miner-b", 100, fastPathTestNow.Add(time.Second)))

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
