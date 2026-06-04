package mqttingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// newTestWorker wires a sourceWorker for direct-method exercise. The
// fake store and service let tests inspect persistence + dispatch.
func newTestWorker(t *testing.T, store *fakeStore, svc *fakeService, src SourceConfig) *sourceWorker {
	t.Helper()
	cfg := Config{
		Store:             store,
		Driver:            NewDriver(svc, nil),
		NewClient:         func() MQTTClient { return newFakeMQTTClient() },
		Decryptor:         passthroughDecryptor{},
		Logger:            slog.New(slog.DiscardHandler),
		Clock:             time.Now,
		WatchdogTickEvery: time.Second,
		BrokerFreshness:   60 * time.Second,
		ShutdownDeadline:  time.Second,
	}
	return &sourceWorker{
		cfg:           cfg,
		source:        src,
		decoder:       targetTimestampDecoder{},
		primaryHost:   src.BrokerPrimaryHost,
		secondaryHost: src.BrokerSecondaryHost,
		lastObs:       map[BrokerRole]*Observation{},
	}
}

func workerSource() SourceConfig {
	return SourceConfig{
		ID:                      1,
		OrganizationID:          7,
		ServiceUserID:           99,
		SourceName:              "site-a",
		BrokerPrimaryHost:       "10.0.0.1",
		BrokerSecondaryHost:     "10.0.0.2",
		BrokerPort:              1883,
		ContractedCurtailmentKw: 12500,
		StalenessThreshold:      240 * time.Second,
		MinCurtailedDuration:    600 * time.Second,
		Enabled:                 true,
	}
}

// Regression: handleWatchdog dispatched WATCHDOG_OFF but never advanced
// LastTarget, so EvaluateWatchdog kept firing every tick.
func TestWorker_HandleWatchdog_PersistsTargetOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	stale := time.Now().Add(-5 * time.Minute) // older than 240 s threshold
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastReceivedAt: stale,
	}

	next := w.handleWatchdog(context.Background(), prior)

	require.Equal(t, 1, svc.startCallsLen(), "watchdog must dispatch one Start")
	assert.Equal(t, TargetOff, next.LastTarget, "in-memory state must record OFF after watchdog dispatch")

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOff, persisted.LastTarget, "persisted state must record OFF so next tick is idle")
}

// A failed watchdog Start leaves state untouched so the next tick retries.
func TestWorker_HandleWatchdog_DispatchFailure_DoesNotAdvance(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	stale := time.Now().Add(-5 * time.Minute)
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastReceivedAt: stale,
	}

	next := w.handleWatchdog(context.Background(), prior)

	assert.Equal(t, TargetOn, next.LastTarget, "failed dispatch must leave LastTarget unchanged")
	_, err := store.GetSourceState(context.Background(), w.source.ID)
	assert.ErrorIs(t, err, ErrSourceStateNotFound, "failed dispatch must not persist state")
}

// A failed Start must not advance LastTarget, or the next identical
// observation reads as a no-op repeat and the site silently uncurtails.
func TestWorker_HandleMessage_DispatchFailure_KeepsLastTarget(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	body, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
	}
	obs := observation{broker: w.primaryHost, payload: body, receivedAt: now}

	next := w.handleMessage(context.Background(), prior, obs)

	assert.Equal(t, TargetOn, next.LastTarget,
		"failed dispatch must not advance LastTarget — the implied edge did not actually run")
	assert.Equal(t, now, next.LastReceivedAt,
		"freshness must still advance — we heard a message, the dispatch is what failed")
	assert.Equal(t, w.primaryHost, next.LastReceivedBroker)
}

// A transient dispatch failure must not advance LastTargetAt: a QoS-1
// redelivery of the same payload has to retry the failed Start, not be
// suppressed as an already-processed duplicate.
func TestWorker_HandleMessage_FailedDispatch_RedeliveryRetries(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	// ON settled at an earlier stamp; the edge anchor is old enough that the OFF
	// is outside the debounce window.
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
	}

	// First OFF: Start fails, so LastTarget stays ON and LastTargetAt is unmoved.
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})
	require.Equal(t, 1, svc.startCallsLen(), "first OFF attempts a Start")
	require.Equal(t, TargetOn, next.LastTarget, "a failed Start must not settle OFF")

	// Recover, then redeliver the SAME OFF payload — it must retry the Start.
	svc.startErr = nil
	newUUID := uuid.New()
	svc.startResult = &curtailment.Plan{EventUUID: &newUUID}
	next = w.handleMessage(context.Background(), next,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now.Add(2 * time.Second)})

	assert.Equal(t, 2, svc.startCallsLen(), "a redelivery of the failed OFF must retry the Start, not be suppressed as a duplicate")
	assert.Equal(t, TargetOff, next.LastTarget, "the retry settles OFF")
}

// Regression: an OFF→ON edge with no in-flight event (ErrNoActiveEvent)
// must advance to ON, not wedge in OFF and re-attempt Stop every message.
func TestWorker_HandleMessage_OffToOn_NoActiveEvent_AdvancesToOn(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{listActiveResult: nil} // no active event → ErrNoActiveEvent
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now})

	assert.Equal(t, TargetOn, next.LastTarget,
		"OFF→ON with no active event must advance to ON, not wedge in OFF")
	assert.Empty(t, svc.stopCalls, "no Stop when there is no active event to stop")
	require.Len(t, svc.listActiveCalls, 1)

	// A follow-up ON is now a plain repeat — it must not retry the dispatch.
	next = w.handleMessage(context.Background(), next,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now.Add(time.Second)})
	assert.Equal(t, TargetOn, next.LastTarget)
	require.Len(t, svc.listActiveCalls, 1, "repeat ON must not retry the OFF→ON dispatch")
}

// A flip absorbed by the debounce window must leave LastTarget untouched
// so a later genuine opposite edge still fires.
func TestWorker_HandleMessage_DebouncedFlip_DoesNotAdvance(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)

	// Curtailed (OFF) with a very recent edge so the OFF→ON flip lands
	// inside DebounceWindow (5 s).
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOff,
		LastEdgeAt:     now.Add(-1 * time.Second),
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now})

	assert.Equal(t, TargetOff, next.LastTarget,
		"a debounced OFF→ON flip must leave LastTarget at OFF")
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
	assert.Equal(t, now, next.LastReceivedAt, "freshness still advances")
}

// Regression: a debounced OFF→ON flip advances LastTargetAt to that payload's
// publisher stamp while LastTarget stays OFF. A later QoS-1 redelivery of the
// same payload (equal stamp) arriving after the debounce window must not be
// read as a fresh edge and Stop the curtailment — the publisher sent no new ON.
func TestWorker_HandleMessage_DebouncedFlipRedelivery_DoesNotStop(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	actorID := "mqtt:site-a" // workerSource() is "site-a" — this source's own event
	svc := &fakeService{listActiveResult: []*models.Event{{EventUUID: uuid.New(), SourceActorID: &actorID}}}
	w := newTestWorker(t, store, svc, workerSource())

	published := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC) // the debounced ON's stamp
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": published.Unix()})
	require.NoError(t, err)

	// Post-debounce state: the ON flip was absorbed (LastTarget still OFF) but
	// its stamp + target landed in LastTargetAt / LastProcessedTarget; the edge
	// anchor is old, so the next arrival is well outside the 5 s debounce window.
	prior := SourceState{
		SourceConfigID:      w.source.ID,
		LastTarget:          TargetOff,
		LastTargetAt:        published,
		LastProcessedTarget: TargetOn,
		LastEdgeAt:          published.Add(-10 * time.Second),
	}

	// The same ON payload redelivered after the debounce window.
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: published.Add(30 * time.Second)})

	assert.Empty(t, svc.stopCalls, "a redelivered duplicate of a debounced flip must not Stop the curtailment")
	assert.Empty(t, svc.listActiveCalls, "no OFF→ON dispatch should be attempted for a duplicate stamp")
	assert.Equal(t, TargetOff, next.LastTarget, "state stays OFF — no new publisher ON")
}

// A genuine same-second target change must still dispatch: wire stamps are
// seconds-precision, so a real ON→OFF flip can share the prior ON's Unix-second
// (equal stamp) yet differ in target — it is not a redelivery.
func TestWorker_HandleMessage_SameSecondTargetChange_Dispatches(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	published := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC) // shared Unix-second
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": published.Unix()})
	require.NoError(t, err)

	// Settled ON at this stamp; the edge anchor is old (outside the debounce).
	prior := SourceState{
		SourceConfigID:      w.source.ID,
		LastTarget:          TargetOn,
		LastTargetAt:        published,
		LastProcessedTarget: TargetOn,
		LastEdgeAt:          published.Add(-1 * time.Minute),
	}

	// A real OFF published in the same Unix-second as the settled ON.
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: published.Add(500 * time.Millisecond)})

	require.Equal(t, 1, svc.startCallsLen(),
		"a real same-second ON->OFF flip must curtail, not be dropped as a duplicate stamp")
	assert.Equal(t, TargetOff, next.LastTarget)
}

// A dispatched edge must persist LastProcessedTarget so the redelivery dedup
// guard survives a restart; this also guards the fake store's fidelity to the
// real sqlc store, which round-trips the column.
func TestWorker_HandleMessage_PersistsProcessedTarget(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	// Settled ON with an old edge anchor: the OFF is a real, non-debounced edge.
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
	}
	w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOff, persisted.LastProcessedTarget,
		"a dispatched edge must persist LastProcessedTarget for restart-safe dedup")
}

// Regression: a future-dated publisher stamp must not pin the ordering
// watermark ahead of receive-time. Before the clamp, an OFF stamped in the
// future advanced LastTargetAt to that future stamp, so every later
// real-stamped signal read as out-of-order (isStalePayload) and was dropped
// until wall-clock caught up — the site stayed curtailed. The watermark clamps
// to receive-time; the future-dated OFF still curtails.
func TestWorker_HandleMessage_FutureDatedStamp_ClampsWatermark(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	offUUID := uuid.New()
	actorID := "mqtt:site-a" // workerSource() is "site-a" — this source's own event
	svc := &fakeService{
		startResult:      &curtailment.Plan{EventUUID: &offUUID},
		listActiveResult: []*models.Event{{EventUUID: offUUID, SourceActorID: &actorID}},
		stopResult:       &models.Event{EventUUID: offUUID},
	}
	w := newTestWorker(t, store, svc, workerSource())

	recvOff := time.Now().UTC()
	// Valid OFF (inside the decoder's ±24 h window) but stamped 12 h ahead of
	// receive-time: a clock-skewed/hostile publisher or a stale retained message.
	futureStamp := recvOff.Add(12 * time.Hour)
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": futureStamp.Unix()})
	require.NoError(t, err)

	// Settled ON; edge anchor old enough that the OFF is a real, non-debounced edge.
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   recvOff.Add(-60 * time.Second),
		LastEdgeAt:     recvOff.Add(-60 * time.Second),
	}

	afterOff := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: recvOff})

	require.Equal(t, 1, svc.startCallsLen(), "the future-dated OFF must still curtail")
	require.Equal(t, TargetOff, afterOff.LastTarget)
	assert.Equal(t, recvOff, afterOff.LastTargetAt,
		"watermark must clamp to receive-time, not the 12 h-future publisher stamp")

	// A later legitimate ON (real stamp, earlier than the future OFF stamp)
	// arriving outside the debounce window must be honored, not dropped as
	// out-of-order behind a future-pinned watermark.
	recvOn := recvOff.Add(10 * time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": recvOn.Unix()})
	require.NoError(t, err)

	afterOn := w.handleMessage(context.Background(), afterOff,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: recvOn})

	assert.Equal(t, TargetOn, afterOn.LastTarget,
		"a later real ON must not be suppressed behind a future-dated watermark")
	require.Len(t, svc.stopCalls, 1, "the ON must dispatch a Stop")
}

// Guards the debounce fix from over-suppressing: a cold-start ON (no prior
// target) is not a flip and must record ON.
func TestWorker_HandleMessage_ColdStartOn_AdvancesToOn(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now})

	assert.Equal(t, TargetOn, next.LastTarget, "cold-start ON must advance LastTarget to ON")
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
}

// A transient state-load error must not kill the worker: it degrades to
// cold-start and the watchdog still fires WATCHDOG_OFF (fail-safe).
func TestWorker_Run_StateLoadError_StartsColdAndWatchdogFires(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.getStateErr = errors.New("transient db error")
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond // fire quickly

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	assertEventually(t, 2*time.Second, func() bool { return svc.startCallsLen() >= 1 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

// A watchdog tick while OFF must NOT re-dispatch when the curtailment
// event still holds.
func TestWorker_HandleWatchdog_Off_ActiveEvent_Idle(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	actorID := "mqtt:site-a" // workerSource() is "site-a" — this source's own event
	svc := &fakeService{listActiveResult: []*models.Event{{EventUUID: uuid.New(), SourceActorID: &actorID}}}
	w := newTestWorker(t, store, svc, workerSource())

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}
	next := w.handleWatchdog(context.Background(), prior)

	assert.Equal(t, TargetOff, next.LastTarget)
	assert.Empty(t, svc.startCalls, "this source's event still holds — no re-curtail")
	require.Len(t, svc.listActiveCalls, 1)
}

// A watchdog tick while OFF must re-curtail when the event was terminated
// out-of-band — the source must stay curtailed.
func TestWorker_HandleWatchdog_Off_NoActiveEvent_Recurtails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{listActiveResult: nil, startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}
	next := w.handleWatchdog(context.Background(), prior)

	require.Len(t, svc.listActiveCalls, 1)
	require.Equal(t, 1, svc.startCallsLen(), "event gone while OFF — must re-curtail")
	assert.Equal(t, models.PriorityEmergency, svc.startCallAt(0).Priority)
	assert.Equal(t, TargetOff, next.LastTarget)

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOff, persisted.LastTarget)
}

// A failed active-event check while OFF must no-op (retry next tick), not
// re-curtail blindly or advance state.
func TestWorker_HandleWatchdog_Off_CheckError_NoOp(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{listActiveErr: errors.New("db down")}
	w := newTestWorker(t, store, svc, workerSource())

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}
	next := w.handleWatchdog(context.Background(), prior)

	assert.Equal(t, TargetOff, next.LastTarget)
	assert.Empty(t, svc.startCalls, "check failed — do not re-curtail blindly")
}

// On a message-driven edge the synthetic external_reference must use the
// publisher's timestamp (stable across the dual-broker duplicate), not the
// local receive time; LastEdgeAt (the debounce anchor) stays receive-time.
func TestWorker_HandleMessage_OnToOff_ReferenceUsesPublishedAt(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	published := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	received := published.Add(7 * time.Second) // fleet received it later than published
	body, err := json.Marshal(map[string]any{"target": 0, "timestamp": published.Unix()})
	require.NoError(t, err)

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOn}
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: body, receivedAt: received})

	require.Equal(t, 1, svc.startCallsLen())
	require.NotNil(t, svc.startCallAt(0).ExternalReference)
	assert.Equal(t, "site-a:"+itoa(published.Unix()), *svc.startCallAt(0).ExternalReference,
		"external_reference must use the publisher timestamp, not receive-time")
	assert.Equal(t, received, next.LastEdgeAt, "debounce anchor stays receive-time")
}

// A stale/out-of-order payload (publisher timestamp older than the last one
// acted on) must be ignored, not allowed to Stop the active curtailment.
func TestWorker_HandleMessage_StalePayload_Ignored(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	processedAt := now.Add(-10 * time.Second) // publisher ts of the OFF we already acted on
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOff,
		LastTargetAt:   processedAt,
		LastEdgeAt:     now.Add(-1 * time.Minute), // well outside the 5s debounce window
	}

	// A stale ON published before the OFF we already acted on, delivered now.
	staleTS := processedAt.Add(-30 * time.Second)
	body, err := json.Marshal(map[string]any{"target": 100, "timestamp": staleTS.Unix()})
	require.NoError(t, err)

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: body, receivedAt: now})

	assert.Empty(t, svc.stopCalls, "a stale ON must not Stop the active curtailment")
	assert.Equal(t, TargetOff, next.LastTarget, "state stays OFF")
	assert.Equal(t, processedAt, next.LastTargetAt, "stale payload must not advance the processed timestamp")
}

// A retained/backlog payload whose publisher stamp is already older than the
// staleness threshold must not be treated as fresh on cold start: it must not
// advance LastTarget or reset the watchdog freshness clock, so the watchdog
// fails safe instead of idling a full threshold on stale data.
func TestWorker_HandleMessage_AgeStalePayload_Ignored(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource()) // StalenessThreshold = 240 s

	now := time.Now().UTC()
	// Retained ON published well past the staleness threshold (but inside the
	// 24 h decode sanity window).
	staleTS := now.Add(-10 * time.Minute)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": staleTS.Unix()})
	require.NoError(t, err)

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown} // cold

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now})

	assert.Equal(t, TargetUnknown, next.LastTarget, "age-stale ON must not advance LastTarget on cold start")
	assert.True(t, next.LastReceivedAt.IsZero(), "age-stale payload must not reset the watchdog freshness clock")
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
}

// On cold start, an age-stale winner on the precedence broker must be evicted
// and re-resolved against the other broker, so a live broker's fresh signal is
// honored instead of masked (and the cold-start watchdog doesn't curtail a
// source a live broker is reporting ON).
func TestWorker_HandleMessage_EvictsAgeStaleWinner_ThenProcessesFresh(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource()) // StalenessThreshold = 240 s

	now := time.Now().UTC()
	// Primary holds a retained ON published well past the staleness threshold,
	// received now — so it wins precedence by receive-time but is age-stale.
	w.lastObs[BrokerPrimary] = &Observation{
		Broker:     w.primaryHost,
		Role:       BrokerPrimary,
		Payload:    Payload{Target: TargetOn, PublishedAt: now.Add(-10 * time.Minute)},
		ReceivedAt: now,
	}
	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown} // cold

	// Secondary delivers a fresh ON.
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.secondaryHost, payload: onBody, receivedAt: now})

	_, primaryCached := w.lastObs[BrokerPrimary]
	assert.False(t, primaryCached, "age-stale primary must be evicted so it can't mask the fresh secondary")
	assert.Equal(t, TargetOn, next.LastTarget, "the live secondary ON must be honored, not masked")
	assert.Equal(t, now, next.LastReceivedAt, "freshness advances from the live secondary, so the watchdog stays idle")
	assert.Empty(t, svc.startCalls, "cold-start ON is not an edge — no curtail")
}

// A stale observation that wins precedence by receive-time must be evicted so
// it stops masking the other broker's current data; the fresh OFF is acted on
// immediately rather than after the freshness window.
func TestWorker_HandleMessage_EvictsStaleWinner_ThenProcessesFresh(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	t0 := now.Add(-20 * time.Second) // last processed; source is ON
	// Primary has a stale ON cached with a recent receive time, so it wins
	// precedence by receive-time but is stale by publisher time.
	w.lastObs[BrokerPrimary] = &Observation{
		Broker:     w.primaryHost,
		Role:       BrokerPrimary,
		Payload:    Payload{Target: TargetOn, PublishedAt: t0.Add(-30 * time.Second)},
		ReceivedAt: now,
	}
	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOn, LastTargetAt: t0}

	// Secondary delivers the current OFF.
	body, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.secondaryHost, payload: body, receivedAt: now})

	require.Equal(t, 1, svc.startCallsLen(), "stale primary must be evicted so the current OFF curtails immediately")
	assert.Equal(t, TargetOff, next.LastTarget)
	_, primaryStillCached := w.lastObs[BrokerPrimary]
	assert.False(t, primaryStillCached, "stale primary observation must be evicted from the cache")
}

// A non-OFF loaded state is reconciled to OFF when this source already has an
// active curtailment event (recovery after a state-read or persist failure),
// so a later ON stops it instead of being a cold-start no-op. A foreign or
// absent active event leaves the loaded state untouched.
func TestWorker_LoadInitialState_ReconcilesWithActiveSourceEvent(t *testing.T) {
	t.Parallel()

	eventUUID := uuid.New()
	mine := "mqtt:site-a" // workerSource() is "site-a"
	foreign := "user:42"

	cases := []struct {
		name        string
		persisted   *SourceState // nil → cold (GetSourceState returns NotFound)
		active      *models.Event
		wantTarget  Target
		wantEventID string
	}{
		{"cold + own active event reconciles to OFF", nil, &models.Event{EventUUID: eventUUID, SourceActorID: &mine}, TargetOff, eventUUID.String()},
		{"persisted ON + own active event reconciles to OFF", &SourceState{LastTarget: TargetOn}, &models.Event{EventUUID: eventUUID, SourceActorID: &mine}, TargetOff, eventUUID.String()},
		{"cold + no active event stays cold", nil, nil, TargetUnknown, ""},
		{"cold + foreign active event stays cold", nil, &models.Event{EventUUID: uuid.New(), SourceActorID: &foreign}, TargetUnknown, ""},
		{"persisted OFF left as-is", &SourceState{LastTarget: TargetOff}, &models.Event{EventUUID: eventUUID, SourceActorID: &mine}, TargetOff, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := workerSource()
			store := newFakeStore()
			if tc.persisted != nil {
				st := *tc.persisted
				st.SourceConfigID = src.ID
				store.state[src.ID] = st
			}
			var listActive []*models.Event
			if tc.active != nil {
				listActive = []*models.Event{tc.active}
			}
			svc := &fakeService{listActiveResult: listActive}
			w := newTestWorker(t, store, svc, src)

			state := w.loadInitialState(context.Background())

			assert.Equal(t, tc.wantTarget, state.LastTarget)
			if tc.wantEventID != "" {
				assert.Equal(t, tc.wantEventID, state.LastEdgeEventUUID)
			}
		})
	}
}

// An OFF whose Start hits a device-overlap AlreadyExists (a concurrent event
// already curtails one of this scope's devices) is a retryable failure, not a
// satisfied OFF: LastTarget must not advance so the next message/tick retries.
func TestWorker_HandleMessage_OnToOff_AlreadyExists_DoesNotRecordOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: fleeterror.NewAlreadyExistsError("a selected device is already in a non-terminal curtailment")}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOn}
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})

	assert.Equal(t, TargetOn, next.LastTarget,
		"a device-overlap AlreadyExists is a retryable failure — LastTarget must not advance to OFF")
}

// A broker whose Connect blocks must not stall the other broker's
// subscription or the fail-safe watchdog — connects run concurrently.
func TestWorker_Run_BrokerConnectBlocked_WatchdogStillFires(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())
	var clientN int
	w.cfg.NewClient = func() MQTTClient {
		clientN++
		c := newFakeMQTTClient()
		if clientN == 1 {
			c.connectBlocks = true // primary hangs in Connect
		}
		return c
	}
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	assertEventually(t, 2*time.Second, func() bool { return svc.startCallsLen() >= 1 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}
