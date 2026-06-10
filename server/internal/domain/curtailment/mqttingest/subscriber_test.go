package mqttingest

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientRegistry counts broker connections across every client a subscriber
// creates. maxActive is the high-water mark of simultaneously-connected
// clients, which lets a test assert that a source reload never overlaps the
// old worker's broker sessions with the replacement's.
type clientRegistry struct {
	mu        sync.Mutex
	active    int
	maxActive int
	connects  int
}

func (r *clientRegistry) onConnect() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active++
	r.connects++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
}

func (r *clientRegistry) onDisconnect() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active > 0 {
		r.active--
	}
}

func (r *clientRegistry) snapshot() (active, maxActive, connects int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active, r.maxActive, r.connects
}

// countingClient is a no-op MQTT client that reports its connect/disconnect
// transitions to a shared registry exactly once each.
type countingClient struct {
	reg       *clientRegistry
	mu        sync.Mutex
	connected bool
}

func (c *countingClient) Connect(context.Context, string, int32, string, string, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		c.connected = true
		c.reg.onConnect()
	}
	return nil
}

func (c *countingClient) Subscribe(_ context.Context, _ string, _ func(payload []byte, receivedAt time.Time)) error {
	return nil
}

func (c *countingClient) Disconnect(time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connected {
		c.connected = false
		c.reg.onDisconnect()
	}
}

func (f *fakeSourceStore) setSources(sources ...SourceConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sources = append([]SourceConfig(nil), sources...)
}

func reconcileTestSource(id int64, name string) SourceConfig {
	src := testSourceConfig()
	src.ID = id
	src.SourceName = name
	return src
}

func newReconcileTestSubscriber(t *testing.T, store Store, reg *clientRegistry) *Subscriber {
	t.Helper()
	s, err := NewSubscriber(Config{
		Store:            store,
		NewClient:        func() MQTTClient { return &countingClient{reg: reg} },
		Decryptor:        passthroughDecryptor{},
		Logger:           slog.New(slog.DiscardHandler),
		ShutdownDeadline: 2 * time.Second,
	})
	require.NoError(t, err)
	return s
}

func requireRunningBrokers(t *testing.T, s *Subscriber, sourceID int64, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		status := s.SourceRuntimeStatus(sourceID)
		return status.State == RuntimeStateRunning && status.RunningBrokerCount == want
	}, 2*time.Second, 10*time.Millisecond, "source %d never reported %d running brokers", sourceID, want)
}

func TestSubscriber_ReconcileStartsAndStopsWorkers(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))
	defer s.Stop()

	requireRunningBrokers(t, s, src.ID, 2)

	// Removing the source from the enabled set must stop its worker on the next
	// reconcile and report it stopped with no running brokers.
	store.setSources()
	require.NoError(t, s.Reconcile(context.Background()))

	status := s.SourceRuntimeStatus(src.ID)
	assert.Equal(t, RuntimeStateStopped, status.State)
	assert.Zero(t, status.RunningBrokerCount)

	require.Eventually(t, func() bool {
		active, _, _ := reg.snapshot()
		return active == 0
	}, 2*time.Second, 10*time.Millisecond, "broker sessions should drain after the source is removed")
}

func TestSubscriber_ReconcileRestartsChangedSourceWithoutOverlap(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))
	defer s.Stop()

	requireRunningBrokers(t, s, src.ID, 2)

	// A config change (new topic) changes the fingerprint and must restart the
	// worker. The replacement may only connect after the prior worker's broker
	// sessions have drained.
	changed := src
	changed.Topic = "maestro/changed"
	store.setSources(changed)
	require.NoError(t, s.Reconcile(context.Background()))

	requireRunningBrokers(t, s, src.ID, 2)

	_, maxActive, connects := reg.snapshot()
	assert.LessOrEqual(t, maxActive, 2, "old worker brokers must disconnect before the replacement connects")
	assert.Equal(t, 4, connects, "source change should fully restart both broker connections")
}

func TestSubscriber_ReconcileSkipsUnchangedSource(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))
	defer s.Stop()

	requireRunningBrokers(t, s, src.ID, 2)

	// Reconciling with an unchanged source must not churn the running worker.
	require.NoError(t, s.Reconcile(context.Background()))

	_, _, connects := reg.snapshot()
	assert.Equal(t, 2, connects, "unchanged source must not reconnect its brokers")
	assert.Equal(t, RuntimeStateRunning, s.SourceRuntimeStatus(src.ID).State)
}

func TestSubscriber_QuiesceSourceStopsOnlyTargetSource(t *testing.T) {
	t.Parallel()

	first := reconcileTestSource(1, "maestro")
	second := reconcileTestSource(2, "qse-bridge")
	store := newFakeSourceStore(first, second)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))
	defer s.Stop()

	requireRunningBrokers(t, s, first.ID, 2)
	requireRunningBrokers(t, s, second.ID, 2)

	require.NoError(t, s.QuiesceSource(context.Background(), first.ID))

	stopped := s.SourceRuntimeStatus(first.ID)
	assert.Equal(t, RuntimeStateStopped, stopped.State)
	assert.Zero(t, stopped.RunningBrokerCount)

	running := s.SourceRuntimeStatus(second.ID)
	assert.Equal(t, RuntimeStateRunning, running.State)
	assert.Equal(t, 2, running.RunningBrokerCount)
}
