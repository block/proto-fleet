package mqttingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// newTestWorker wires a sourceWorker for direct method tests.
func newTestWorker(t *testing.T, store *fakeStore, svc *fakeService, src SourceConfig) *sourceWorker {
	t.Helper()
	cfg := Config{
		Store:             store,
		Driver:            NewDriver(svc),
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

func TestWorker_ConnectAndSubscribe_BackpressuresWhenQueueFull(t *testing.T) {
	t.Parallel()

	w := newTestWorker(t, newFakeStore(), &fakeService{}, workerSource())
	client := newFakeMQTTClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messages := make(chan observation, 1)
	messages <- observation{broker: "filled"}

	require.NoError(t, w.connectAndSubscribeOnce(ctx, client, w.primaryHost, messages))

	payload := []byte(`{"target":0,"timestamp":1778538975}`)
	receivedAt := time.Now().UTC()
	delivered := make(chan struct{})
	go func() {
		client.deliver(payload, receivedAt)
		close(delivered)
	}()

	select {
	case <-delivered:
		t.Fatal("callback returned while the worker queue was full")
	case <-time.After(50 * time.Millisecond):
	}

	<-messages

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not return after queue space became available")
	}

	select {
	case got := <-messages:
		assert.Equal(t, w.primaryHost, got.broker)
		assert.Equal(t, payload, got.payload)
		assert.Equal(t, receivedAt, got.receivedAt)
	default:
		t.Fatal("payload was not queued after backpressure released")
	}
}

// Watchdog OFF must settle state after dispatch.
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
	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.PendingEdge, "failed dispatch must persist pending retry state")
	assert.Equal(t, EdgeWatchdogOff, persisted.PendingEdge.Direction)
}

// Failed Start must not settle LastTarget.
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

// Failed dispatch must not suppress a redelivery retry.
func TestWorker_HandleMessage_FailedDispatch_RedeliveryRetries(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	// Old edge anchor: the OFF is outside debounce.
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

func TestWorker_HandleMessage_PendingPersistFailurePreventsSideEffect(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.upsertErr = errors.New("db down")
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})

	assert.Equal(t, 0, svc.startCallsLen(), "edge side effect must not run until pending state is durable")
	assert.Equal(t, TargetOn, next.LastTarget)
}

func TestWorker_HandleWatchdog_RetriesPendingMessageEdge(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{startErr: errors.New("svc down")}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
		LastReceivedAt: now,
	}

	pending := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})
	require.NotNil(t, pending.PendingEdge, "failed edge must remain pending for watchdog retry")
	require.Equal(t, 1, svc.startCallsLen())

	svc.startErr = nil
	newUUID := uuid.New()
	svc.startResult = &curtailment.Plan{EventUUID: &newUUID}

	settled := w.handleWatchdog(context.Background(), pending)

	assert.Equal(t, 2, svc.startCallsLen(), "watchdog must retry the pending edge without waiting for staleness")
	assert.Nil(t, settled.PendingEdge)
	assert.Equal(t, TargetOff, settled.LastTarget)
}

func TestWorker_HandleMessage_NewerOnClearsPendingOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onAt := pendingAt.Add(time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": onAt.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   pendingAt.Add(-time.Minute),
		LastReceivedAt: pendingAt,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: onAt})

	assert.Nil(t, next.PendingEdge, "newer ON must cancel the stale pending OFF")
	assert.Equal(t, TargetOn, next.LastTarget)
	assert.Empty(t, svc.startCalls, "canceling the stale pending OFF must not curtail")

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Nil(t, persisted.PendingEdge, "cleared pending edge must persist")
	assert.Equal(t, TargetOn, persisted.LastTarget)
}

func TestWorker_HandleMessage_SameSecondOnDoesNotClearPendingOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": pendingAt.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   pendingAt.Add(-time.Minute),
		LastReceivedAt: pendingAt,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: pendingAt.Add(500 * time.Millisecond)})

	require.NotNil(t, next.PendingEdge, "same-second ON must not cancel a pending OFF")
	assert.Equal(t, TargetOff, next.PendingEdge.Target)
	assert.Equal(t, TargetOn, next.LastTarget)
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
}

func TestWorker_HandleMessage_DuplicateOnDoesNotClearPendingOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onAt := pendingAt.Add(time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": onAt.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID:       w.source.ID,
		LastTarget:           TargetOn,
		LastTargetAt:         onAt,
		LastProcessedTarget:  TargetOn,
		LastProcessedTargets: []Target{TargetOn},
		LastReceivedAt:       pendingAt,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: onAt.Add(time.Second)})

	require.NotNil(t, next.PendingEdge, "known duplicate ON must not cancel a pending OFF")
	assert.Equal(t, TargetOff, next.PendingEdge.Target)
	assert.Empty(t, svc.startCalls)
	assert.Empty(t, svc.stopCalls)
}

func TestWorker_HandleMessage_NewerOnSupersedingPendingOffStopsActiveEvent(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	src := workerSource()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(src, eventUUID, models.EventStateActive),
		},
		stopResult: testSourceEvent(src, eventUUID, models.EventStateRestoring),
	}
	w := newTestWorker(t, store, svc, src)

	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onAt := pendingAt.Add(time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": onAt.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   pendingAt.Add(-time.Minute),
		LastReceivedAt: pendingAt,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: onAt})

	require.Equal(t, 1, svc.stopCallsLen(), "newer ON must restore an active event left by the superseded pending OFF")
	assert.Nil(t, next.PendingEdge)
	assert.Equal(t, TargetOn, next.LastTarget)
	assert.Equal(t, eventUUID.String(), next.LastEdgeEventUUID)

	persisted, err := store.GetSourceState(context.Background(), src.ID)
	require.NoError(t, err)
	assert.Nil(t, persisted.PendingEdge)
	assert.Equal(t, TargetOn, persisted.LastTarget)
}

func TestWorker_HandleMessage_NewerOnSupersedingPendingOffStopFailureRetries(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	src := workerSource()
	active := testSourceEvent(src, eventUUID, models.EventStateActive)
	svc := &fakeService{
		listActiveResult: []*models.Event{active},
		stopErr:          errors.New("stop failed"),
	}
	w := newTestWorker(t, store, svc, src)

	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onAt := pendingAt.Add(time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": onAt.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   pendingAt.Add(-time.Minute),
		LastReceivedAt: pendingAt,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: onAt})

	require.Equal(t, 1, svc.stopCallsLen(), "superseding ON must attempt Stop")
	require.NotNil(t, next.PendingEdge, "failed Stop must keep replacement ON pending")
	assert.Equal(t, EdgeOffToOn, next.PendingEdge.Direction)
	assert.Equal(t, TargetOn, next.PendingEdge.Target)

	persisted, err := store.GetSourceState(context.Background(), src.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.PendingEdge, "replacement pending ON must be durable")
	assert.Equal(t, EdgeOffToOn, persisted.PendingEdge.Direction)

	svc.stopErr = nil
	svc.stopResult = testSourceEvent(src, eventUUID, models.EventStateRestoring)
	settled := w.handleWatchdog(context.Background(), next)

	assert.Equal(t, 2, svc.stopCallsLen(), "watchdog must retry the pending Stop")
	assert.Nil(t, settled.PendingEdge)
	assert.Equal(t, TargetOn, settled.LastTarget)
}

func TestWorker_HandleMessage_CorrectedClockOnClearsFutureDatedPendingOff(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	pendingReceived := time.Now().UTC()
	futureTargetAt := pendingReceived.Add(time.Hour)
	onReceived := pendingReceived.Add(5 * time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": onReceived.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   pendingReceived.Add(-time.Minute),
		LastReceivedAt: pendingReceived,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       futureTargetAt,
			ReceivedAt:     pendingReceived,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: onReceived})

	assert.Nil(t, next.PendingEdge, "newer ON must cancel a pending OFF from a future-dated publisher stamp")
	assert.Equal(t, TargetOn, next.LastTarget)
	assert.Empty(t, svc.startCalls, "correcting the publisher clock must not replay the stale pending OFF")

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Nil(t, persisted.PendingEdge, "cleared future-dated pending edge must persist")
}

func TestWorker_HandleMessage_PendingClearPersistFailureKeepsPendingInMemory(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.upsertErr = errors.New("db down")
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onAt := pendingAt.Add(time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": onAt.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   pendingAt.Add(-time.Minute),
		LastReceivedAt: pendingAt,
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: w.primaryHost,
		},
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: onAt})

	require.NotNil(t, next.PendingEdge,
		"failed persistence of a pending-edge clear must not make the cancellation look settled in memory")
	assert.Equal(t, TargetOff, next.PendingEdge.Target)
	assert.Equal(t, TargetOn, next.LastTarget)
	assert.Empty(t, svc.startCalls, "the stale pending OFF must not dispatch while the clear write is failing")
}

func TestWorker_HandleMessage_RechecksAuthBeforeDispatch(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.nonIngestUsers = map[userOrgKey]bool{{userID: 99, orgID: 7}: true}
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)
	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-time.Minute),
		LastEdgeAt:     now.Add(-time.Minute),
	}

	pending := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})

	assert.Equal(t, 0, svc.startCallsLen(), "revoked ingest permission must block side effects")
	assert.Equal(t, TargetOn, pending.LastTarget)
	require.NotNil(t, pending.PendingEdge, "blocked edge remains pending so it can retry after permission is restored")
	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.PendingEdge)

	store.nonIngestUsers = nil
	settled := w.handleWatchdog(context.Background(), pending)

	assert.Equal(t, 1, svc.startCallsLen(), "pending edge retries after permission is restored")
	assert.Nil(t, settled.PendingEdge)
	assert.Equal(t, TargetOff, settled.LastTarget)
}

func TestWorker_HandleWatchdog_RechecksAuthBeforeRecurtail(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.nonIngestUsers = map[userOrgKey]bool{{userID: 99, orgID: 7}: true}
	eventUUID := uuid.New()
	src := workerSource()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(src, eventUUID, models.EventStateRestoring),
		},
		recurtailResult: &models.Event{EventUUID: eventUUID, State: models.EventStatePending},
	}
	w := newTestWorker(t, store, svc, src)

	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOff,
		LastReceivedAt: time.Now().UTC(),
	}

	next := w.handleWatchdog(context.Background(), prior)

	assert.Equal(t, prior, next)
	assert.Empty(t, svc.recurtailCalls, "revoked ingest permission must block watchdog re-curtail side effects")
}

func TestWorker_LoadInitialState_PendingOffRehydratesUntilWatchdogRetry(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.upsertErrs = []error{
		nil,
		errors.New("settle write failed"),
		nil,
	}
	eventUUID := uuid.New()
	src := workerSource()
	active := testSourceEvent(src, eventUUID, models.EventStateActive)
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &eventUUID},
		listActiveResults: [][]*models.Event{
			nil,
			{active},
		},
	}
	w := newTestWorker(t, store, svc, src)

	now := time.Now().UTC()
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)
	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
	}

	afterOff := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: now})
	require.Equal(t, TargetOff, afterOff.LastTarget)
	require.Equal(t, 1, svc.startCallsLen())

	persisted, err := store.GetSourceState(context.Background(), src.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.PendingEdge)

	recovered, ok := w.loadInitialState(context.Background())
	require.True(t, ok)

	assert.Equal(t, TargetOn, recovered.LastTarget)
	require.NotNil(t, recovered.PendingEdge)
	assert.Equal(t, 1, svc.startCallsLen(), "state load must not replay pending edges before MQTT subscription")

	settled := w.handleWatchdog(context.Background(), recovered)

	assert.Equal(t, TargetOff, settled.LastTarget)
	assert.Nil(t, settled.PendingEdge)
	assert.Equal(t, 1, svc.startCallsLen(), "watchdog retry must settle the existing event without a duplicate Start")
}

func TestWorker_LoadInitialState_ReadErrorRetriesWithoutColdStart(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.getStateErr = errors.New("db down")
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	_, ok := w.loadInitialState(context.Background())

	assert.False(t, ok, "state read errors must retry instead of cold-starting")
	assert.Equal(t, 0, svc.listActiveCallsLen(), "read failure must not continue into active-event reconciliation")
}

// ON with no source event still settles ON.
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

// MQTT ON is authoritative and bypasses min-hold.
func TestWorker_HandleMessage_OffToOn_ForcesRestore(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	src := workerSource()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(src, eventUUID, models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: eventUUID},
	}
	w := newTestWorker(t, store, svc, src)

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOff,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now})

	require.Equal(t, 1, svc.stopCallsLen(), "ON must attempt to restore the active source event")
	assert.True(t, svc.stopCallAt(0).Force, "MQTT ON must bypass source min-hold")
	assert.Equal(t, TargetOn, next.LastTarget)
}

func TestWorker_LoadInitialState_PendingOnAfterStopFailureSettlesWithoutRecurtail(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.upsertErrs = []error{
		nil,
		errors.New("settle write failed"),
		nil,
	}
	eventUUID := uuid.New()
	src := workerSource()
	active := testSourceEvent(src, eventUUID, models.EventStateActive)
	restoring := testSourceEvent(src, eventUUID, models.EventStateRestoring)
	svc := &fakeService{
		listActiveResults: [][]*models.Event{
			{active},
			{active},
			{restoring},
		},
		stopResult: &models.Event{EventUUID: eventUUID},
	}
	w := newTestWorker(t, store, svc, src)

	published := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": published.Unix()})
	require.NoError(t, err)
	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOff,
		LastTargetAt:   published.Add(-time.Minute),
		LastEdgeAt:     published.Add(-time.Minute),
	}

	afterOn := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: published})
	require.Equal(t, TargetOn, afterOn.LastTarget)
	require.Equal(t, 1, svc.stopCallsLen())

	persisted, err := store.GetSourceState(context.Background(), src.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.PendingEdge, "failed settlement leaves durable pending ON for restart recovery")
	require.Equal(t, TargetOff, persisted.LastTarget)

	recovered, ok := w.loadInitialState(context.Background())
	require.True(t, ok)

	assert.Equal(t, TargetOff, recovered.LastTarget)
	require.NotNil(t, recovered.PendingEdge)
	assert.Equal(t, 1, svc.stopCallsLen(), "state load must not replay pending edges before MQTT subscription")
	assert.Empty(t, svc.recurtailCalls, "recovery must not undo the accepted ON")

	settled := w.handleWatchdog(context.Background(), recovered)

	assert.Equal(t, TargetOn, settled.LastTarget)
	assert.Nil(t, settled.PendingEdge)
	assert.Equal(t, 1, svc.stopCallsLen(), "watchdog retry must not Stop the already-restoring event again")
	assert.Empty(t, svc.recurtailCalls, "watchdog retry must not undo the accepted ON")
	persisted, err = store.GetSourceState(context.Background(), src.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOn, persisted.LastTarget)
	assert.Nil(t, persisted.PendingEdge)
}

// A terminal stop race after lookup must still settle ON.
func TestWorker_HandleMessage_OffToOn_TerminalStopRace_AdvancesToOn(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveResults: [][]*models.Event{
			{testSourceEvent(workerSource(), eventUUID, models.EventStateActive)},
			{testSourceEvent(workerSource(), eventUUID, models.EventStateActive)},
			nil,
		},
		stopErr: fleeterror.NewFailedPreconditionErrorf(
			"cannot stop curtailment event %s in terminal state %q", eventUUID, models.EventStateCompleted),
	}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOff,
		LastTargetAt:   now.Add(-60 * time.Second),
		LastEdgeAt:     now.Add(-60 * time.Second),
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: now})

	assert.Equal(t, TargetOn, next.LastTarget,
		"a terminal stop race must still advance OFF→ON, not leave LastTarget OFF for the watchdog to re-curtail")
	require.Len(t, svc.stopCalls, 1, "Stop was attempted on the listed event")
	assert.Len(t, svc.listActiveCalls, 3, "pending ON pre-checks the event, and failed Stop re-checks the terminal race")
}

// Debounced flips do not settle LastTarget.
func TestWorker_HandleMessage_DebouncedFlip_DoesNotAdvance(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)

	// Recent edge: the ON lands inside debounce.
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

// Redelivery of a debounced flip must remain a duplicate.
func TestWorker_HandleMessage_DebouncedFlipRedelivery_DoesNotStop(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(workerSource(), uuid.New(), models.EventStateActive),
		},
	}
	w := newTestWorker(t, store, svc, workerSource())

	published := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC) // the debounced ON's stamp
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": published.Unix()})
	require.NoError(t, err)

	// The prior ON was debounced but recorded for duplicate suppression.
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

// Same-second target changes are not duplicate redeliveries.
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

// Same-second OFF->ON->OFF sequences must honor the latest OFF. This can be
// indistinguishable from an old OFF redelivery, so fail safe toward curtailment.
func TestWorker_HandleMessage_SameSecondOffAfterOn_Recurtails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	src := workerSource()
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &eventUUID},
		listActiveResults: [][]*models.Event{
			nil,
			{testSourceEvent(src, eventUUID, models.EventStateActive)},
			{testSourceEvent(src, eventUUID, models.EventStateRestoring)},
		},
		stopResult: &models.Event{EventUUID: eventUUID},
		recurtailResult: &models.Event{
			EventUUID: eventUUID,
			State:     models.EventStatePending,
		},
	}
	w := newTestWorker(t, store, svc, src)

	published := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": published.Unix()})
	require.NoError(t, err)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": published.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   published.Add(-time.Minute),
		LastEdgeAt:     published.Add(-time.Minute),
	}
	afterOff := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: published})
	require.Equal(t, TargetOff, afterOff.LastTarget)
	require.Equal(t, 1, svc.startCallsLen())

	afterOn := w.handleMessage(context.Background(), afterOff,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: published.Add(10 * time.Second)})
	require.Equal(t, TargetOn, afterOn.LastTarget)
	require.Equal(t, 1, svc.stopCallsLen())

	afterSecondOff := w.handleMessage(context.Background(), afterOn,
		observation{broker: w.primaryHost, payload: offBody, receivedAt: published.Add(20 * time.Second)})

	assert.Equal(t, TargetOff, afterSecondOff.LastTarget, "latest OFF must abort restore even with the same publisher second")
	assert.Equal(t, 1, svc.startCallsLen(), "restoring source event should be re-curtailed, not started again")
	require.Len(t, svc.recurtailCalls, 1)
	assert.Equal(t, eventUUID, svc.recurtailCalls[0].EventUUID)

	afterRedeliveredOn := w.handleMessage(context.Background(), afterSecondOff,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: published.Add(30 * time.Second)})

	assert.Equal(t, TargetOff, afterRedeliveredOn.LastTarget, "old same-second ON redelivery must not restore after the latest OFF")
	assert.Equal(t, 1, svc.stopCallsLen(), "redelivered ON must not dispatch another Stop")
	assert.Len(t, svc.recurtailCalls, 1, "redelivered ON must not undo the re-curtail")
}

// LastProcessedTarget must persist for restart-safe dedup.
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

// Future-dated payloads must not pin ordering ahead of receive time.
func TestWorker_HandleMessage_FutureDatedStamp_DoesNotSuppressLaterSignal(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	offUUID := uuid.New()
	svc := &fakeService{
		startResult: &curtailment.Plan{EventUUID: &offUUID},
		listActiveResults: [][]*models.Event{
			nil,
			{testSourceEvent(workerSource(), offUUID, models.EventStateActive)},
		},
		stopResult: &models.Event{EventUUID: offUUID},
	}
	w := newTestWorker(t, store, svc, workerSource())

	recvOff := time.Date(2026, 6, 5, 12, 0, 0, int(500*time.Millisecond), time.UTC)
	// Valid but future-dated within the decoder sanity window.
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
	assert.True(t, afterOff.LastTargetAt.After(recvOff),
		"LastTargetAt keeps the raw future stamp (the dedup guard needs it); ordering is capped at read-time")

	// A later real-stamped ON must not be hidden by the future stamp.
	recvOn := recvOff.Add(10 * time.Second)
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": recvOn.Unix()})
	require.NoError(t, err)

	afterOn := w.handleMessage(context.Background(), afterOff,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: recvOn})

	assert.Equal(t, TargetOn, afterOn.LastTarget,
		"a later real ON must not be suppressed behind a future-dated watermark")
	require.Len(t, svc.stopCalls, 1, "the ON must dispatch a Stop")
}

func TestWorker_IsStalePayload_FutureDatedCutoffAllowsSameSecondCorrection(t *testing.T) {
	t.Parallel()

	w := newTestWorker(t, newFakeStore(), &fakeService{}, workerSource())
	received := time.Date(2026, 6, 5, 12, 0, 0, int(500*time.Millisecond), time.UTC)
	prior := SourceState{
		LastTargetAt:   received.Add(time.Hour),
		LastReceivedAt: received,
	}
	corrected := CanonicalState{
		Target:      TargetOn,
		PublishedAt: received.Truncate(time.Second),
		ReceivedAt:  received.Add(300 * time.Millisecond),
	}

	assert.False(t, w.isStalePayload(prior, corrected),
		"a same-second corrected-clock payload received later must not be stale due only to receive-time subsecond precision")
}

// Future-dated duplicate suppression still uses the raw processed stamp.
func TestWorker_HandleMessage_FutureDatedDebouncedFlip_RedeliverySuppressed(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(workerSource(), uuid.New(), models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: uuid.New()},
	}
	w := newTestWorker(t, store, svc, workerSource())

	base := time.Now().UTC()
	futureStamp := base.Add(1 * time.Hour) // publisher clock ahead, still inside ±24 h
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": futureStamp.Unix()})
	require.NoError(t, err)

	// Recent edge: the future-dated ON is debounced.
	prior := SourceState{
		SourceConfigID:      w.source.ID,
		LastTarget:          TargetOff,
		LastProcessedTarget: TargetOff,
		LastTargetAt:        base.Add(-2 * time.Second),
		LastReceivedAt:      base.Add(-2 * time.Second),
		LastEdgeAt:          base.Add(-2 * time.Second),
	}

	// Debounced future-dated ON: absorbed (no Stop), advances LastProcessedTarget=ON.
	afterFlip := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: base})
	require.Equal(t, TargetOff, afterFlip.LastTarget, "the ON flip is debounced — state stays OFF")
	require.Equal(t, TargetOn, afterFlip.LastProcessedTarget)
	require.Empty(t, svc.stopCalls, "a debounced flip must not dispatch Stop")

	// Redelivery outside debounce must still be a duplicate.
	afterRedelivery := w.handleMessage(context.Background(), afterFlip,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: base.Add(10 * time.Second)})
	assert.Equal(t, TargetOff, afterRedelivery.LastTarget, "redelivered duplicate must not flip state to ON")
	assert.Empty(t, svc.stopCalls, "redelivered duplicate of a debounced future-dated ON must not Stop the curtailment")
}

// Cold-start ON is not a debounced flip.
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

// State-load errors retry before the worker subscribes or starts its watchdog,
// so a transient read failure cannot erase a recent persisted ON.
func TestWorker_Run_StateLoadErrorRetriesBeforeWatchdog(t *testing.T) {
	t.Parallel()

	src := workerSource()
	store := newFakeStore()
	store.getStateErrs = []error{errors.New("transient db error"), nil}
	store.state[src.ID] = SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastReceivedAt: time.Now().UTC(),
	}
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond // fire quickly

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	assertEventually(t, 2*time.Second, func() bool {
		return svc.listActiveCallsLen() >= 1
	})
	assert.Equal(t, 0, svc.startCallsLen(), "recent persisted ON must not cold-start to UNKNOWN and fire the watchdog")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorker_Run_RetriesInitialReconcileBeforeProcessingOn(t *testing.T) {
	t.Parallel()

	src := workerSource()
	store := newFakeStore()
	store.state[src.ID] = SourceState{SourceConfigID: src.ID, LastTarget: TargetOn}

	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveErrs: []error{errors.New("db down"), nil},
		listActiveResult: []*models.Event{
			testSourceEvent(src, eventUUID, models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: eventUUID},
	}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond

	clients := make(chan *fakeMQTTClient, 2)
	w.cfg.NewClient = func() MQTTClient {
		c := newFakeMQTTClient()
		clients <- c
		return c
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	var primary *fakeMQTTClient
	select {
	case primary = <-clients:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not subscribe after reconcile retry")
	}
	select {
	case <-clients:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not create secondary client after reconcile retry")
	}
	require.GreaterOrEqual(t, svc.listActiveCallsLen(), 2, "startup must retry active-event reconciliation before subscribing")

	now := time.Now().UTC()
	body, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)
	primary.deliver(body, now)

	assertEventually(t, 2*time.Second, func() bool { return svc.stopCallsLen() == 1 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorker_Run_SubscriptionSupersedesStartupPendingOnBeforeReplay(t *testing.T) {
	t.Parallel()

	src := workerSource()
	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := newFakeStore()
	store.state[src.ID] = SourceState{
		SourceConfigID:      src.ID,
		LastTarget:          TargetOff,
		LastTargetAt:        pendingAt.Add(-time.Minute),
		LastReceivedAt:      pendingAt,
		LastReceivedBroker:  src.BrokerPrimaryHost,
		LastEdgeAt:          pendingAt.Add(-time.Minute),
		LastEdgeEventUUID:   uuid.NewString(),
		LastProcessedTarget: TargetOff,
		LastProcessedTargets: []Target{
			TargetOff,
		},
		PendingEdge: &PendingEdge{
			Direction:      EdgeOffToOn,
			Target:         TargetOn,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: src.BrokerPrimaryHost,
			PriorEdgeAt:    pendingAt.Add(-time.Minute),
		},
	}

	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(src, eventUUID, models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: eventUUID},
	}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond

	clients := make(chan *fakeMQTTClient, 2)
	w.cfg.NewClient = func() MQTTClient {
		c := newFakeMQTTClient()
		clients <- c
		return c
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	var primary *fakeMQTTClient
	select {
	case primary = <-clients:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not create primary client")
	}
	select {
	case <-clients:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not create secondary client")
	}

	offAt := pendingAt.Add(time.Second)
	offBody, err := json.Marshal(map[string]any{"target": 0, "timestamp": offAt.Unix()})
	require.NoError(t, err)
	primary.deliver(offBody, offAt)

	assertEventually(t, 2*time.Second, func() bool {
		state, err := store.GetSourceState(context.Background(), src.ID)
		return err == nil &&
			state.PendingEdge == nil &&
			state.LastTarget == TargetOff &&
			state.LastReceivedAt.Equal(offAt)
	})
	time.Sleep(3 * w.cfg.WatchdogTickEvery)
	assert.Equal(t, 0, svc.stopCallsLen(), "newer OFF must supersede startup pending ON before restore can replay")
	assert.Empty(t, svc.recurtailCalls, "newer OFF must not be treated as a restore-then-recurtail race")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorker_Run_ReplaysStartupPendingOffWhenBrokersStayDown(t *testing.T) {
	t.Parallel()

	src := workerSource()
	pendingAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := newFakeStore()
	store.state[src.ID] = SourceState{
		SourceConfigID:     src.ID,
		LastTarget:         TargetOn,
		LastTargetAt:       pendingAt.Add(-time.Minute),
		LastReceivedAt:     pendingAt,
		LastReceivedBroker: src.BrokerPrimaryHost,
		LastEdgeAt:         pendingAt.Add(-time.Minute),
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			TargetAt:       pendingAt,
			ReceivedAt:     pendingAt,
			ReceivedBroker: src.BrokerPrimaryHost,
			PriorEdgeAt:    pendingAt.Add(-time.Minute),
		},
	}

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond
	w.cfg.NewClient = func() MQTTClient {
		c := newFakeMQTTClient()
		c.connectBlocks = true
		return c
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	assertEventually(t, 2*time.Second, func() bool {
		state, err := store.GetSourceState(context.Background(), src.ID)
		return err == nil &&
			state.PendingEdge == nil &&
			state.LastTarget == TargetOff &&
			state.LastEdgeEventUUID == newUUID.String()
	})
	assert.Equal(t, 1, svc.startCallsLen(), "pending OFF must replay even when no broker subscribes")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorker_Run_StartupWatchdogGraceAllowsRetainedOn(t *testing.T) {
	t.Parallel()

	src := workerSource()
	src.StalenessThreshold = time.Hour
	store := newFakeStore()
	store.state[src.ID] = SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastReceivedAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 50 * time.Millisecond

	clients := make(chan *fakeMQTTClient, 2)
	w.cfg.NewClient = func() MQTTClient {
		c := newFakeMQTTClient()
		clients <- c
		return c
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	var primary *fakeMQTTClient
	select {
	case primary = <-clients:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not create primary client")
	}
	select {
	case <-clients:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not create secondary client")
	}

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)
	primary.deliver(onBody, now)

	time.Sleep(3 * w.cfg.WatchdogTickEvery)
	assert.Equal(t, 0, svc.startCallsLen(), "fresh ON during startup grace must prevent a spurious watchdog curtail")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorker_Run_BrokersNeverConnectStartupGraceEventuallyFailsSafe(t *testing.T) {
	t.Parallel()

	src := workerSource()
	src.StalenessThreshold = 80 * time.Millisecond
	store := newFakeStore()
	eventUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &eventUUID}}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond
	w.cfg.NewClient = func() MQTTClient {
		c := newFakeMQTTClient()
		c.connectBlocks = true
		return c
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	time.Sleep(src.StalenessThreshold / 2)
	assert.Equal(t, 0, svc.startCallsLen(), "startup grace must not curtail before the staleness bound")

	assertEventually(t, 2*time.Second, func() bool { return svc.startCallsLen() >= 1 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorker_Run_RetriesInitialBrokerConnect(t *testing.T) {
	t.Parallel()

	src := workerSource()
	store := newFakeStore()
	store.state[src.ID] = SourceState{SourceConfigID: src.ID, LastTarget: TargetOff}

	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(src, eventUUID, models.EventStateActive),
		},
		stopResult: &models.Event{EventUUID: eventUUID},
	}
	w := newTestWorker(t, store, svc, src)
	w.cfg.WatchdogTickEvery = 10 * time.Millisecond

	primary := newFakeMQTTClient()
	primary.connectErrs = []error{errors.New("broker down")}
	secondary := newFakeMQTTClient()
	secondary.connectBlocks = true
	clients := []*fakeMQTTClient{primary, secondary}
	nextClient := 0
	w.cfg.NewClient = func() MQTTClient {
		c := clients[nextClient]
		nextClient++
		return c
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()

	select {
	case <-primary.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not retry and subscribe after initial broker connect failure")
	}
	require.GreaterOrEqual(t, primary.connectCallsLen(), 2)

	now := time.Now().UTC()
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)
	primary.deliver(onBody, now)

	assertEventually(t, 2*time.Second, func() bool { return svc.stopCallsLen() == 1 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestInitialBrokerRetryBackoff(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 20*time.Millisecond, nextInitialBrokerRetry(10*time.Millisecond))
	assert.Equal(t, initialBrokerRetryMax, nextInitialBrokerRetry(initialBrokerRetryMax))
	assert.Equal(t, initialBrokerRetryMax, nextInitialBrokerRetry(initialBrokerRetryMax+time.Second))

	base := 100 * time.Millisecond
	got := jitterRetryDelay(base, rand.New(rand.NewSource(1)))
	assert.GreaterOrEqual(t, got, base)
	assert.LessOrEqual(t, got, base+base/5)
}

func TestMQTTClientIdentityIncludesBrokerPort(t *testing.T) {
	t.Parallel()

	src := workerSource()
	left := mqttClientIdentity(src, src.BrokerPrimaryHost)
	src.BrokerPort++
	right := mqttClientIdentity(src, src.BrokerPrimaryHost)

	assert.NotEqual(t, left, right)
}

// A held source event satisfies OFF.
func TestWorker_HandleWatchdog_Off_ActiveEvent_Idle(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(workerSource(), uuid.New(), models.EventStateActive),
		},
	}
	w := newTestWorker(t, store, svc, workerSource())

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}
	next := w.handleWatchdog(context.Background(), prior)

	assert.Equal(t, TargetOff, next.LastTarget)
	assert.Empty(t, svc.startCalls, "this source's event still holds — no re-curtail")
	require.Len(t, svc.listActiveCalls, 1)
}

// A restoring source event no longer satisfies OFF.
func TestWorker_HandleWatchdog_Off_RestoringEvent_Recurtails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(workerSource(), eventUUID, models.EventStateRestoring),
		},
		recurtailResult: &models.Event{EventUUID: eventUUID, State: models.EventStatePending},
	}
	w := newTestWorker(t, store, svc, workerSource())

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}
	next := w.handleWatchdog(context.Background(), prior)

	require.Len(t, svc.recurtailCalls, 1, "a restoring event must be re-curtailed in place (resumed), not replayed via Start")
	assert.Equal(t, eventUUID, svc.recurtailCalls[0].EventUUID)
	assert.Empty(t, svc.startCalls, "resume must not dispatch a fresh WATCHDOG_OFF Start (which would replay the restoring event)")
	assert.Equal(t, TargetOff, next.LastTarget)
}

// A missing source event while OFF starts a fresh curtailment.
func TestWorker_HandleWatchdog_Off_NoActiveEvent_Recurtails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{listActiveResult: nil, startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	prior := SourceState{SourceConfigID: w.source.ID, LastTarget: TargetOff}
	next := w.handleWatchdog(context.Background(), prior)

	require.NotEmpty(t, svc.listActiveCalls, "watchdog must check whether this source still has a non-terminal event")
	require.Equal(t, 1, svc.startCallsLen(), "event gone while OFF — must re-curtail")
	assert.Equal(t, models.PriorityEmergency, svc.startCallAt(0).Priority)
	assert.Equal(t, TargetOff, next.LastTarget)

	persisted, err := store.GetSourceState(context.Background(), w.source.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOff, persisted.LastTarget)
}

func TestWorker_HandleWatchdog_Off_EmptyFullFleetNoopThrottlesWindow(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	src := workerSource()
	src.CurtailMode = string(models.ModeFullFleet)
	src.ContractedCurtailmentKw = 0
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	svc := &fakeService{listActiveResult: nil, startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, src)
	w.cfg.Clock = func() time.Time { return now }

	prior := SourceState{SourceConfigID: src.ID, LastTarget: TargetOff}
	afterFirst := w.handleWatchdog(context.Background(), prior)
	afterSecond := w.handleWatchdog(context.Background(), afterFirst)

	require.Equal(t, 1, svc.startCallsLen(), "empty FULL_FLEET no-op should not create one terminal event per tick")
	assert.Equal(t, afterFirst.LastEmptyFullFleetWatchdogRef, afterSecond.LastEmptyFullFleetWatchdogRef)
	assert.NotEmpty(t, afterFirst.LastEmptyFullFleetWatchdogRef)
}

// OFF during restore re-curtails the restoring event in place.
func TestWorker_HandleMessage_OffWhileRestoring_Recurtails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	eventUUID := uuid.New()
	svc := &fakeService{
		listActiveResult: []*models.Event{
			testSourceEvent(workerSource(), eventUUID, models.EventStateRestoring),
		},
		recurtailResult: &models.Event{EventUUID: eventUUID, State: models.EventStatePending},
	}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	body, err := json.Marshal(map[string]any{"target": 0, "timestamp": now.Unix()})
	require.NoError(t, err)

	prior := SourceState{
		SourceConfigID: w.source.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-30 * time.Second),
		LastEdgeAt:     now.Add(-30 * time.Second),
	}

	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.primaryHost, payload: body, receivedAt: now})

	require.Len(t, svc.recurtailCalls, 1, "OFF must re-curtail the restoring source event in place")
	assert.Equal(t, eventUUID, svc.recurtailCalls[0].EventUUID)
	assert.Empty(t, svc.startCalls, "OFF while restoring must not Start a competing event")
	assert.Equal(t, TargetOff, next.LastTarget)
	assert.Equal(t, eventUUID.String(), next.LastEdgeEventUUID)
}

// Failed active-event checks retry on the next tick.
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

// Message-driven OFF idempotency uses publisher time.
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

// Out-of-order payloads cannot stop curtailment.
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

// Age-stale retained payloads do not refresh cold-start liveness.
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

// Cold-start age-stale primary must not mask a live secondary.
func TestWorker_HandleMessage_EvictsAgeStaleWinner_ThenProcessesFresh(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	w := newTestWorker(t, store, svc, workerSource()) // StalenessThreshold = 240 s

	now := time.Now().UTC()
	// Primary wins by receive-time but is age-stale.
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

func TestWorker_HandleMessage_FreshSecondaryAdvancesLivenessWhenPrimaryWins(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := &fakeService{}
	src := workerSource()
	src.StalenessThreshold = 10 * time.Second
	w := newTestWorker(t, store, svc, src) // BrokerFreshness = 60 s

	now := time.Now().UTC()
	primaryAt := now.Add(-30 * time.Second)
	w.lastObs[BrokerPrimary] = &Observation{
		Broker:     w.primaryHost,
		Role:       BrokerPrimary,
		Payload:    Payload{Target: TargetOn, PublishedAt: primaryAt},
		ReceivedAt: primaryAt,
	}
	prior := SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   primaryAt,
		LastReceivedAt: primaryAt,
	}

	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": now.Unix()})
	require.NoError(t, err)
	next := w.handleMessage(context.Background(), prior,
		observation{broker: w.secondaryHost, payload: onBody, receivedAt: now})

	assert.Equal(t, TargetOn, next.LastTarget)
	assert.Equal(t, now, next.LastReceivedAt, "live secondary traffic must keep the watchdog freshness clock current")
	assert.Equal(t, w.secondaryHost, next.LastReceivedBroker)
	assert.Empty(t, svc.startCalls)
}

// Stale precedence winners must not mask a fresh OFF.
func TestWorker_HandleMessage_EvictsStaleWinner_ThenProcessesFresh(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	w := newTestWorker(t, store, svc, workerSource())

	now := time.Now().UTC()
	t0 := now.Add(-20 * time.Second) // last processed; source is ON
	// Primary wins by receive-time but is stale by publisher time.
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

// Startup reconciles only held source events to OFF.
func TestWorker_LoadInitialState_ReconcilesWithActiveSourceEvent(t *testing.T) {
	t.Parallel()

	eventUUID := uuid.New()
	foreign := "user:42"
	sourceEvent := func(state models.EventState) *models.Event {
		return testSourceEvent(workerSource(), eventUUID, state)
	}

	cases := []struct {
		name        string
		persisted   *SourceState // nil → cold (GetSourceState returns NotFound)
		active      *models.Event
		wantTarget  Target
		wantEventID string
	}{
		{"cold + own active event reconciles to OFF", nil, sourceEvent(models.EventStateActive), TargetOff, eventUUID.String()},
		{"persisted ON + own active event reconciles to OFF", &SourceState{LastTarget: TargetOn}, sourceEvent(models.EventStateActive), TargetOff, eventUUID.String()},
		{"cold + own restoring event stays cold", nil, sourceEvent(models.EventStateRestoring), TargetUnknown, ""},
		{"persisted ON + own restoring event preserves ON", &SourceState{LastTarget: TargetOn}, sourceEvent(models.EventStateRestoring), TargetOn, ""},
		{"cold + no active event stays cold", nil, nil, TargetUnknown, ""},
		{"cold + foreign active event stays cold", nil, &models.Event{EventUUID: uuid.New(), SourceActorID: &foreign, State: models.EventStateActive}, TargetUnknown, ""},
		{"persisted OFF left as-is", &SourceState{LastTarget: TargetOff}, sourceEvent(models.EventStateActive), TargetOff, ""},
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

			state, ok := w.loadInitialState(context.Background())

			require.True(t, ok)
			assert.Equal(t, tc.wantTarget, state.LastTarget)
			if tc.wantEventID != "" {
				assert.Equal(t, tc.wantEventID, state.LastEdgeEventUUID)
			}
		})
	}
}

// Recovery seeds anchors so retained pre-event ON does not stop curtailment.
func TestWorker_LoadInitialState_SeedsAnchorsFromActiveEvent(t *testing.T) {
	t.Parallel()

	store := newFakeStore()                              // cold start: GetSourceState returns NotFound
	eventStart := time.Now().UTC().Add(-2 * time.Minute) // curtailment began 2 min ago
	active := testSourceEvent(workerSource(), uuid.New(), models.EventStateActive)
	active.CreatedAt = eventStart
	svc := &fakeService{
		listActiveResult: []*models.Event{active},
		stopResult:       &models.Event{EventUUID: uuid.New()},
	}
	w := newTestWorker(t, store, svc, workerSource())

	recovered, ok := w.loadInitialState(context.Background())
	require.True(t, ok)
	require.Equal(t, TargetOff, recovered.LastTarget, "an active own event reconciles to OFF")
	assert.Equal(t, eventStart, recovered.LastTargetAt, "ordering anchor seeded from the active event")
	assert.Equal(t, eventStart, recovered.LastEdgeAt, "debounce anchor seeded from the active event")

	// Retained pre-event ON must not stop the recovered curtailment.
	onBody, err := json.Marshal(map[string]any{"target": 100, "timestamp": eventStart.Add(-30 * time.Second).Unix()})
	require.NoError(t, err)
	after := w.handleMessage(context.Background(), recovered,
		observation{broker: w.primaryHost, payload: onBody, receivedAt: time.Now().UTC()})

	assert.Equal(t, TargetOff, after.LastTarget, "a retained pre-event ON must not lift the recovered curtailment")
	assert.Empty(t, svc.stopCalls, "stale retained ON must not dispatch Stop")
}

// Device-overlap AlreadyExists leaves OFF unsettled for retry.
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

// One blocked broker must not stall the other broker or watchdog.
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
