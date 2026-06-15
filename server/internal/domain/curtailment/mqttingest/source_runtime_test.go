package mqttingest

import (
	"context"
	"errors"
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
