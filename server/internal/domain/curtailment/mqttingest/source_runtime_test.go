package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSourceStore struct {
	mu      sync.Mutex
	sources []SourceConfig
	state   map[int64]SourceState
}

func newFakeSourceStore(sources ...SourceConfig) *fakeSourceStore {
	return &fakeSourceStore{
		sources: append([]SourceConfig(nil), sources...),
		state:   make(map[int64]SourceState),
	}
}

func (f *fakeSourceStore) ListEnabledSources(context.Context) ([]SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SourceConfig(nil), f.sources...), nil
}

func (f *fakeSourceStore) GetSourceState(_ context.Context, sourceConfigID int64) (SourceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.state[sourceConfigID]
	if !ok {
		return SourceState{}, ErrSourceStateNotFound
	}
	return state, nil
}

func (f *fakeSourceStore) UpsertSourceState(_ context.Context, update StateUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[update.SourceConfigID] = SourceState{
		SourceConfigID:       update.SourceConfigID,
		LastTarget:           update.LastTarget,
		LastTargetAt:         update.LastTargetAt,
		LastProcessedTarget:  update.LastProcessedTarget,
		LastProcessedTargets: append([]Target(nil), update.LastProcessedTargets...),
		LastReceivedAt:       update.LastReceivedAt,
		LastReceivedBroker:   update.LastReceivedBroker,
		LastEdgeAt:           update.LastEdgeAt,
		PendingEdge:          cloneTestPendingEdge(update.PendingEdge),
	}
	return nil
}

func cloneTestPendingEdge(edge *PendingEdge) *PendingEdge {
	if edge == nil {
		return nil
	}
	cp := *edge
	return &cp
}

type passthroughDecryptor struct{}

func (passthroughDecryptor) Decrypt(s string) ([]byte, error) { return []byte(s), nil }

type fakeMQTTClient struct {
	handler func(payload []byte, receivedAt time.Time)
}

func (f *fakeMQTTClient) Connect(context.Context, string, int32, string, string, string, string) error {
	return nil
}

func (f *fakeMQTTClient) Subscribe(_ context.Context, _ string, handler func(payload []byte, receivedAt time.Time)) error {
	f.handler = handler
	return nil
}

func (f *fakeMQTTClient) Disconnect(time.Duration) {}

func testSourceConfig() SourceConfig {
	return SourceConfig{
		ID:                    1,
		OrganizationID:        7,
		ServiceUserID:         99,
		SourceName:            "maestro",
		Topic:                 "maestro/target",
		BrokerPrimaryHost:     "10.0.0.1",
		BrokerSecondaryHost:   "10.0.0.2",
		BrokerPort:            1883,
		BrokerTransport:       brokerTransportTCP,
		MQTTUsername:          "operator",
		MQTTPasswordEncrypted: "secret",
		PayloadFormat:         payloadFormatTargetTimestamp,
		StalenessThreshold:    240 * time.Second,
		Enabled:               true,
	}
}

func newTestSourceWorker(store *fakeSourceStore, src SourceConfig, clock func() time.Time) *sourceWorker {
	if clock == nil {
		clock = time.Now
	}
	return &sourceWorker{
		cfg: Config{
			Store:             store,
			NewClient:         func() MQTTClient { return &fakeMQTTClient{} },
			Decryptor:         passthroughDecryptor{},
			Logger:            slog.New(slog.DiscardHandler),
			Clock:             clock,
			WatchdogTickEvery: time.Second,
			BrokerFreshness:   60 * time.Second,
			ShutdownDeadline:  time.Second,
		},
		source:        src,
		decoder:       targetTimestampDecoder{},
		primaryHost:   src.BrokerPrimaryHost,
		secondaryHost: src.BrokerSecondaryHost,
		lastObs:       map[BrokerRole]*Observation{},
	}
}

func TestSourceWorker_HandleMessageRecordsOffSignalOnly(t *testing.T) {
	t.Parallel()

	store := newFakeSourceStore()
	src := testSourceConfig()
	w := newTestSourceWorker(store, src, nil)
	receivedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetUnknown,
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092800}`),
		receivedAt: receivedAt,
	})

	require.Equal(t, TargetOff, state.LastTarget)
	assert.Equal(t, receivedAt, state.LastEdgeAt)
	assert.Nil(t, state.PendingEdge)

	persisted := store.state[src.ID]
	assert.Equal(t, TargetOff, persisted.LastTarget)
	assert.Nil(t, persisted.PendingEdge)
}

func TestSourceWorker_HandleMessageReassertsFreshRepeatedOff(t *testing.T) {
	t.Parallel()

	store := newFakeSourceStore()
	src := testSourceConfig()
	publishedAt := time.Unix(1781092800, 0).UTC()
	nextPublishedAt := publishedAt.Add(time.Minute)
	receivedAt := nextPublishedAt.Add(500 * time.Millisecond)
	w := newTestSourceWorker(store, src, func() time.Time { return receivedAt })
	executor := &fakeSignalExecutor{}
	w.cfg.SignalExecutor = executor

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID:       src.ID,
		LastTarget:           TargetOff,
		LastTargetAt:         publishedAt,
		LastProcessedTarget:  TargetOff,
		LastProcessedTargets: []Target{TargetOff},
		LastReceivedAt:       publishedAt,
		LastEdgeAt:           publishedAt,
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092860}`),
		receivedAt: receivedAt,
	})

	require.Equal(t, TargetOff, state.LastTarget)
	assert.Equal(t, nextPublishedAt, state.LastTargetAt)
	assert.Equal(t, publishedAt, state.LastEdgeAt)
	assert.Nil(t, state.PendingEdge)
	assert.Equal(t, 1, executor.calls)
	assert.Equal(t, TargetOff, executor.last.Target)
	assert.Equal(t, EdgeReassertOff, executor.last.Direction)

	persisted := store.state[src.ID]
	assert.Equal(t, TargetOff, persisted.LastTarget)
	assert.Equal(t, nextPublishedAt, persisted.LastTargetAt)
	assert.Nil(t, persisted.PendingEdge)
}

func TestSourceWorker_HandleMessageSuppressesFrequentRepeatedOff(t *testing.T) {
	t.Parallel()

	store := newFakeSourceStore()
	src := testSourceConfig()
	publishedAt := time.Unix(1781092800, 0).UTC()
	nextPublishedAt := publishedAt.Add(10 * time.Second)
	receivedAt := nextPublishedAt.Add(500 * time.Millisecond)
	w := newTestSourceWorker(store, src, func() time.Time { return receivedAt })
	executor := &fakeSignalExecutor{}
	w.cfg.SignalExecutor = executor

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID:       src.ID,
		LastTarget:           TargetOff,
		LastTargetAt:         publishedAt,
		LastProcessedTarget:  TargetOff,
		LastProcessedTargets: []Target{TargetOff},
		LastReceivedAt:       publishedAt,
		LastEdgeAt:           publishedAt,
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092810}`),
		receivedAt: receivedAt,
	})

	require.Equal(t, TargetOff, state.LastTarget)
	assert.Equal(t, publishedAt, state.LastTargetAt)
	assert.Equal(t, receivedAt, state.LastReceivedAt)
	assert.Equal(t, publishedAt, state.LastEdgeAt)
	assert.Nil(t, state.PendingEdge)
	assert.Equal(t, 0, executor.calls)

	persisted := store.state[src.ID]
	assert.Equal(t, TargetOff, persisted.LastTarget)
	assert.Equal(t, publishedAt, persisted.LastTargetAt)
	assert.Nil(t, persisted.PendingEdge)
}

func TestSourceWorker_RepeatedOffDoesNotDebounceFollowingOn(t *testing.T) {
	t.Parallel()

	store := newFakeSourceStore()
	src := testSourceConfig()
	publishedAt := time.Unix(1781092800, 0).UTC()
	reassertedAt := publishedAt.Add(time.Minute)
	onAt := reassertedAt.Add(time.Second)
	w := newTestSourceWorker(store, src, func() time.Time { return onAt })
	executor := &fakeSignalExecutor{}
	w.cfg.SignalExecutor = executor

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID:       src.ID,
		LastTarget:           TargetOff,
		LastTargetAt:         publishedAt,
		LastProcessedTarget:  TargetOff,
		LastProcessedTargets: []Target{TargetOff},
		LastReceivedAt:       publishedAt,
		LastEdgeAt:           publishedAt,
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092860}`),
		receivedAt: reassertedAt,
	})
	require.Equal(t, TargetOff, state.LastTarget)
	assert.Equal(t, publishedAt, state.LastEdgeAt)
	assert.Equal(t, 1, executor.calls)

	state = w.handleMessage(context.Background(), state, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":100,"timestamp":1781092861}`),
		receivedAt: onAt,
	})

	require.Equal(t, TargetOn, state.LastTarget)
	assert.Equal(t, onAt, state.LastEdgeAt)
	assert.Equal(t, 2, executor.calls)
	assert.Equal(t, TargetOn, executor.last.Target)
	assert.Equal(t, EdgeOffToOn, executor.last.Direction)
}

func TestSourceWorker_HandleMessageSuppressesDuplicateRepeatedOff(t *testing.T) {
	t.Parallel()

	store := newFakeSourceStore()
	src := testSourceConfig()
	publishedAt := time.Unix(1781092800, 0).UTC()
	receivedAt := publishedAt.Add(time.Second)
	w := newTestSourceWorker(store, src, func() time.Time { return receivedAt })
	executor := &fakeSignalExecutor{}
	w.cfg.SignalExecutor = executor

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID:       src.ID,
		LastTarget:           TargetOff,
		LastTargetAt:         publishedAt,
		LastProcessedTarget:  TargetOff,
		LastProcessedTargets: []Target{TargetOff},
		LastReceivedAt:       publishedAt,
		LastEdgeAt:           publishedAt,
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092800}`),
		receivedAt: receivedAt,
	})

	require.Equal(t, TargetOff, state.LastTarget)
	assert.Nil(t, state.PendingEdge)
	assert.Equal(t, 0, executor.calls)

	persisted := store.state[src.ID]
	assert.Equal(t, TargetOff, persisted.LastTarget)
	assert.Nil(t, persisted.PendingEdge)
}

func TestSourceWorker_SuppressesSameTimestampOffReplayAfterOn(t *testing.T) {
	t.Parallel()

	store := newFakeSourceStore()
	src := testSourceConfig()
	publishedAt := time.Unix(1781092800, 0).UTC()
	receivedAt := publishedAt.Add(2 * time.Second)
	w := newTestSourceWorker(store, src, func() time.Time { return receivedAt })
	executor := &fakeSignalExecutor{}
	w.cfg.SignalExecutor = executor

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID:       src.ID,
		LastTarget:           TargetOn,
		LastTargetAt:         publishedAt,
		LastProcessedTarget:  TargetOn,
		LastProcessedTargets: []Target{TargetOff, TargetOn},
		LastReceivedAt:       publishedAt,
		LastEdgeAt:           publishedAt,
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092800}`),
		receivedAt: receivedAt,
	})

	require.Equal(t, TargetOn, state.LastTarget)
	assert.Nil(t, state.PendingEdge)
	assert.Equal(t, 0, executor.calls)
}

func TestSourceWorker_HandleWatchdogRecordsOffWithoutEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 10, 12, 10, 0, 0, time.UTC)
	store := newFakeSourceStore()
	src := testSourceConfig()
	w := newTestSourceWorker(store, src, func() time.Time { return now })

	state := w.handleWatchdog(context.Background(), SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-10 * time.Minute),
		LastReceivedAt: now.Add(-10 * time.Minute),
	})

	require.Equal(t, TargetOff, state.LastTarget)
	assert.Equal(t, now, state.LastEdgeAt)
	assert.Nil(t, state.PendingEdge)
}

func TestSourceWorker_HandleMessageRetainsPendingEdgeWhenExecutorFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := newFakeSourceStore()
	src := testSourceConfig()
	w := newTestSourceWorker(store, src, func() time.Time { return now })
	executor := &fakeSignalExecutor{err: errors.New("automation unavailable")}
	w.cfg.SignalExecutor = executor

	state := w.handleMessage(context.Background(), SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   now.Add(-time.Minute),
		LastReceivedAt: now.Add(-time.Minute),
		LastEdgeAt:     now.Add(-time.Minute),
	}, observation{
		broker:     src.BrokerPrimaryHost,
		payload:    []byte(`{"target":0,"timestamp":1781092800}`),
		receivedAt: now,
	})

	require.NotNil(t, state.PendingEdge)
	assert.Equal(t, TargetOff, state.PendingEdge.Target)
	assert.Equal(t, now.Add(time.Second), state.PendingEdge.RetryAt)
	assert.Equal(t, TargetOn, state.LastTarget, "failed executor must not settle the source target")
	assert.Equal(t, 1, executor.calls)
	assert.Equal(t, TargetOff, executor.last.Target)

	persisted := store.state[src.ID]
	require.NotNil(t, persisted.PendingEdge)
	assert.Equal(t, TargetOff, persisted.PendingEdge.Target)
	assert.Equal(t, now.Add(time.Second), persisted.PendingEdge.RetryAt)
	assert.Equal(t, TargetOn, persisted.LastTarget)
}

func TestSourceWorker_RetryPendingEdgeBacksOffExecutorFailures(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	now := receivedAt.Add(time.Minute)
	store := newFakeSourceStore()
	src := testSourceConfig()
	w := newTestSourceWorker(store, src, func() time.Time { return now })
	executor := &fakeSignalExecutor{err: errors.New("automation unavailable")}
	w.cfg.SignalExecutor = executor

	state, settled := w.retryPendingEdge(context.Background(), SourceState{
		SourceConfigID: src.ID,
		LastTarget:     TargetOn,
		LastTargetAt:   receivedAt.Add(-time.Minute),
		LastReceivedAt: receivedAt.Add(-time.Minute),
		LastEdgeAt:     receivedAt.Add(-time.Minute),
		PendingEdge: &PendingEdge{
			Direction:      EdgeOnToOff,
			Target:         TargetOff,
			ReceivedAt:     receivedAt,
			ReceivedBroker: src.BrokerPrimaryHost,
		},
	})

	require.True(t, settled)
	require.NotNil(t, state.PendingEdge)
	assert.Equal(t, now.Add(64*time.Second), state.PendingEdge.RetryAt)
	assert.Equal(t, TargetOn, state.LastTarget)
	assert.Equal(t, 1, executor.calls)

	persisted := store.state[src.ID]
	require.NotNil(t, persisted.PendingEdge)
	assert.Equal(t, now.Add(64*time.Second), persisted.PendingEdge.RetryAt)
}

func TestSourceWorker_HeartbeatsPreservePendingEdgeBackoffAcrossRestart(t *testing.T) {
	t.Parallel()

	firstReceivedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	now := firstReceivedAt
	store := newFakeSourceStore()
	src := testSourceConfig()
	executor := &fakeSignalExecutor{err: errors.New("automation unavailable")}
	w := newTestSourceWorker(store, src, func() time.Time { return now })
	w.cfg.SignalExecutor = executor
	state := SourceState{SourceConfigID: src.ID, LastTarget: TargetOn}
	var lastAttemptAt time.Time
	for elapsed := time.Duration(0); elapsed <= 130*time.Second; elapsed += time.Second {
		now = firstReceivedAt.Add(elapsed)
		state = w.handleMessage(t.Context(), state, observation{
			broker:     src.BrokerPrimaryHost,
			payload:    []byte(fmt.Sprintf(`{"target":0,"timestamp":%d}`, now.Unix())),
			receivedAt: now,
		})
		require.NotNil(t, state.PendingEdge)
		assert.Equal(t, firstReceivedAt, state.PendingEdge.ReceivedAt, "heartbeat must keep the first edge timestamp")
		assert.Equal(t, firstReceivedAt, state.PendingEdge.TargetAt, "heartbeat must keep the first publisher timestamp")
		assert.Equal(t, now, state.LastReceivedAt, "publisher liveness should still advance")
		if executor.calls > 0 && executor.last.ReceivedAt.Equal(firstReceivedAt) {
			lastAttemptAt = state.PendingEdge.RetryAt
		}
		if elapsed == 10*time.Second {
			// Reload the actual persisted retry state into a replacement worker.
			w = newTestSourceWorker(store, src, func() time.Time { return now })
			w.cfg.SignalExecutor = executor
			var ok bool
			state, ok = w.loadInitialState(t.Context())
			require.True(t, ok)
		}
	}
	assert.Equal(t, 8, executor.calls, "attempts should occur at seconds 0,1,3,7,15,31,63,127")
	assert.Equal(t, firstReceivedAt.Add(255*time.Second), lastAttemptAt)
	assert.Equal(t, TargetOn, state.LastTarget, "failed OFF must remain pending")

	executor.err = nil
	now = state.PendingEdge.RetryAt
	state = w.handleWatchdog(t.Context(), state)
	assert.Nil(t, state.PendingEdge)
	assert.Equal(t, TargetOff, state.LastTarget)
	assert.Equal(t, firstReceivedAt.Add(130*time.Second), state.LastReceivedAt, "settling old retry must not move publisher liveness backwards")
	assert.Equal(t, firstReceivedAt, executor.last.ReceivedAt)
}

func TestSourceWorker_NewerHeartbeatRejectsDelayedOlderSignalAfterRetry(t *testing.T) {
	t.Parallel()

	for _, settleBy := range []string{"heartbeat retry", "watchdog retry"} {
		t.Run(settleBy, func(t *testing.T) {
			t.Parallel()
			start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
			now := start
			store := newFakeSourceStore()
			src := testSourceConfig()
			executor := &fakeSignalExecutor{err: errors.New("automation unavailable")}
			w := newTestSourceWorker(store, src, func() time.Time { return now })
			w.cfg.SignalExecutor = executor
			message := func(target int, publishedAt, receivedAt time.Time) observation {
				return observation{
					broker:     src.BrokerPrimaryHost,
					payload:    []byte(fmt.Sprintf(`{"target":%d,"timestamp":%d}`, target, publishedAt.Unix())),
					receivedAt: receivedAt,
				}
			}

			state := w.handleMessage(t.Context(), SourceState{SourceConfigID: src.ID, LastTarget: TargetOn}, message(0, start, start))
			require.NotNil(t, state.PendingEdge, "the first OFF fails and stays pending")

			now = start.Add(10 * time.Second)
			if settleBy == "heartbeat retry" {
				executor.err = nil
			}
			state = w.handleMessage(t.Context(), state, message(0, now, now))
			if settleBy == "watchdog retry" {
				require.NotNil(t, state.PendingEdge)
				executor.err = nil
				now = state.PendingEdge.RetryAt
				state = w.handleWatchdog(t.Context(), state)
			}
			require.Nil(t, state.PendingEdge)
			require.Equal(t, TargetOff, state.LastTarget)
			require.Equal(t, start, executor.last.TargetAt, "the retry settles the original edge")
			callsAfterOff := executor.calls

			w = newTestSourceWorker(store, src, func() time.Time { return now })
			w.cfg.SignalExecutor = executor
			state, ok := w.loadInitialState(t.Context())
			require.True(t, ok)

			delayedOnAt := now.Add(time.Second)
			state = w.handleMessage(t.Context(), state, message(100, start.Add(5*time.Second), delayedOnAt))

			assert.Equal(t, TargetOff, state.LastTarget, "an ON published before the newer OFF must not restore miners")
			assert.Equal(t, callsAfterOff, executor.calls, "the delayed ON must not reach automation")
			assert.Nil(t, state.PendingEdge)
		})
	}
}

func TestSourceWorker_WatchdogKeepsPublisherStaleAcrossRetryAndRestart(t *testing.T) {
	t.Parallel()

	for _, hadPublisher := range []bool{false, true} {
		t.Run(fmt.Sprintf("had_publisher_%v", hadPublisher), func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, 6, 10, 12, 10, 0, 0, time.UTC)
			store := newFakeSourceStore()
			src := testSourceConfig()
			w := newTestSourceWorker(store, src, func() time.Time { return now })
			executor := &fakeSignalExecutor{err: errors.New("automation unavailable")}
			w.cfg.SignalExecutor = executor
			prior := SourceState{SourceConfigID: src.ID, LastTarget: TargetUnknown}
			if hadPublisher {
				prior.LastTarget = TargetOn
				prior.LastReceivedAt = now.Add(-10 * time.Minute)
				prior.LastReceivedBroker = src.BrokerPrimaryHost
				prior.LastTargetAt = prior.LastReceivedAt
			}
			state := w.handleWatchdog(t.Context(), prior)
			require.NotNil(t, state.PendingEdge)
			assert.Equal(t, EdgeWatchdogOff, state.PendingEdge.Direction)
			now = state.PendingEdge.RetryAt
			executor.err = nil
			w = newTestSourceWorker(store, src, func() time.Time { return now })
			w.cfg.SignalExecutor = executor
			state, ok := w.loadInitialState(t.Context())
			require.True(t, ok)
			state = w.handleWatchdog(t.Context(), state)
			assert.Nil(t, state.PendingEdge)
			assert.Equal(t, TargetOff, state.LastTarget)
			assert.Equal(t, prior.LastReceivedAt, state.LastReceivedAt)
			assert.Equal(t, prior.LastReceivedBroker, state.LastReceivedBroker)
			svc := &SettingsService{clock: func() time.Time { return now }}
			assert.True(t, svc.viewFor(src, state, true).Stale, "synthetic OFF must not hide the dead publisher")
			persisted, err := store.GetSourceState(t.Context(), src.ID)
			require.NoError(t, err)
			assert.Equal(t, state.LastReceivedAt, persisted.LastReceivedAt)
			assert.Equal(t, TargetOff, persisted.LastTarget)
		})
	}
}

func TestNewSubscriberDoesNotRequireCurtailmentDriver(t *testing.T) {
	t.Parallel()

	s, err := NewSubscriber(Config{
		Store:     newFakeSourceStore(),
		NewClient: func() MQTTClient { return &fakeMQTTClient{} },
		Decryptor: passthroughDecryptor{},
	})

	require.NoError(t, err)
	assert.NotNil(t, s)
}

type fakeSignalExecutor struct {
	calls int
	last  SignalEdge
	err   error
}

func (f *fakeSignalExecutor) HandleMQTTSignal(_ context.Context, signal SignalEdge) error {
	f.calls++
	f.last = signal
	return f.err
}
