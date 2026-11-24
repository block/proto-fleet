package mqttingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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

type blockingDisconnectClient struct {
	reg     *clientRegistry
	release <-chan struct{}
}

type blockingListStore struct {
	Store
	started     chan struct{}
	startedOnce sync.Once
	release     <-chan struct{}
}

func (s *blockingListStore) ListEnabledSources(ctx context.Context) ([]SourceConfig, error) {
	s.startedOnce.Do(func() { close(s.started) })
	<-s.release
	return s.Store.ListEnabledSources(ctx)
}

func (c *blockingDisconnectClient) Connect(context.Context, string, int32, string, string, string, string) error {
	c.reg.onConnect()
	return nil
}

func (c *blockingDisconnectClient) Subscribe(context.Context, string, func([]byte, time.Time)) error {
	return nil
}

func (c *blockingDisconnectClient) Disconnect(time.Duration) {
	<-c.release
	c.reg.onDisconnect()
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

type runtimeStatusClientRegistry struct {
	mu      sync.Mutex
	clients map[string]*runtimeStatusClient
}

func newRuntimeStatusClientRegistry() *runtimeStatusClientRegistry {
	return &runtimeStatusClientRegistry{
		clients: make(map[string]*runtimeStatusClient),
	}
}

func (r *runtimeStatusClientRegistry) newClient() MQTTClient {
	return &runtimeStatusClient{reg: r}
}

func (r *runtimeStatusClientRegistry) register(host string, client *runtimeStatusClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[host] = client
}

func (r *runtimeStatusClientRegistry) report(t *testing.T, host string, connected bool, subscribed bool, err error) {
	t.Helper()
	var client *runtimeStatusClient
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		client = r.clients[host]
		return client != nil
	}, 2*time.Second, 10*time.Millisecond, "broker client %s was not registered", host)
	client.report(connected, subscribed, err)
}

type runtimeStatusClient struct {
	reg      *runtimeStatusClientRegistry
	mu       sync.Mutex
	reporter func(connected bool, subscribed bool, err error)
}

func (c *runtimeStatusClient) Connect(_ context.Context, host string, _ int32, _ string, _ string, _ string, _ string) error {
	c.reg.register(host, c)
	return nil
}

func (c *runtimeStatusClient) Subscribe(_ context.Context, _ string, _ func(payload []byte, receivedAt time.Time)) error {
	return nil
}

func (c *runtimeStatusClient) Disconnect(time.Duration) {}

func (c *runtimeStatusClient) SetRuntimeStatusReporter(reporter func(connected bool, subscribed bool, err error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reporter = reporter
}

func (c *runtimeStatusClient) report(connected bool, subscribed bool, err error) {
	c.mu.Lock()
	reporter := c.reporter
	c.mu.Unlock()
	if reporter != nil {
		reporter(connected, subscribed, err)
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

func requireRuntimeStatus(t *testing.T, s *Subscriber, sourceID int64, state RuntimeState, running int, subscribed int) RuntimeStatus {
	t.Helper()
	var status RuntimeStatus
	require.Eventually(t, func() bool {
		status = s.SourceRuntimeStatus(sourceID)
		return status.State == state &&
			status.RunningBrokerCount == running &&
			status.SubscribedBrokerCount == subscribed
	}, 2*time.Second, 10*time.Millisecond,
		"source %d never reported state=%v running=%d subscribed=%d",
		sourceID, state, running, subscribed)
	return status
}

func TestSubscriber_ReconcileStartsAndStopsWorkers(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))
	defer func() { require.NoError(t, s.Stop(context.Background())) }()

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

type failingListStore struct {
	Store
	failing atomic.Bool
	calls   atomic.Int64
}

func (s *failingListStore) ListEnabledSources(ctx context.Context) ([]SourceConfig, error) {
	s.calls.Add(1)
	if s.failing.Load() {
		return nil, errors.New("database unavailable")
	}
	return s.Store.ListEnabledSources(ctx)
}

func TestSubscriber_ThrottlesReconcileWarningsWhileRetrying(t *testing.T) {
	t.Parallel()

	store := &failingListStore{Store: newFakeSourceStore()}
	store.failing.Store(true)
	s := newReconcileTestSubscriber(t, store, &clientRegistry{})
	s.cfg.WatchdogTickEvery = time.Millisecond
	var logs bytes.Buffer
	s.cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	var clockSeconds atomic.Int64
	clockSeconds.Store(1_700_000_000)
	s.cfg.Clock = func() time.Time { return time.Unix(clockSeconds.Load(), 0) }
	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
	require.Eventually(t, func() bool { return store.calls.Load() >= 5 }, time.Second, time.Millisecond)
	clockSeconds.Add(int64(reconcileWarningInterval / time.Second))
	previousCalls := store.calls.Load()
	require.Eventually(t, func() bool { return store.calls.Load() >= previousCalls+2 }, 2*time.Second, time.Millisecond)
	require.NoError(t, s.Stop(t.Context()))

	var warnings []uint64
	for line := range bytes.SplitSeq(logs.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Message    string `json:"msg"`
			Suppressed uint64 `json:"suppressed_warnings"`
		}
		require.NoError(t, json.Unmarshal(line, &entry))
		if entry.Message == "mqttingest: source reconciliation failed; retrying" {
			warnings = append(warnings, entry.Suppressed)
		}
	}
	require.Len(t, warnings, 2, "log once per minute while continuing every retry")
	assert.Zero(t, warnings[0])
	assert.Positive(t, warnings[1], "the next warning reports suppressed attempts")
}

func TestSubscriber_StartRetriesSourceListingAndSupportsRestart(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := &failingListStore{Store: newFakeSourceStore(src)}
	store.failing.Store(true)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	s.cfg.WatchdogTickEvery = 5 * time.Millisecond
	require.NoError(t, s.Start(t.Context()), "database failure must not stop the runtime group")
	require.ErrorContains(t, s.Start(t.Context()), "already started", "degraded activation still owns its lifecycle")
	require.Eventually(t, func() bool { return store.calls.Load() >= 2 }, time.Second, time.Millisecond)
	store.failing.Store(false)
	requireRunningBrokers(t, s, src.ID, 2)
	require.NoError(t, s.Stop(t.Context()))

	store.failing.Store(true)
	require.NoError(t, s.Start(t.Context()))
	require.NoError(t, s.Stop(t.Context()), "shutdown must cancel the startup retry timer")
	require.NoError(t, s.Start(t.Context()), "a stopped degraded activation must release ownership")
	store.failing.Store(false)
	requireRunningBrokers(t, s, src.ID, 2)
	require.NoError(t, s.Stop(t.Context()))
	_, maxActive, connects := reg.snapshot()
	assert.LessOrEqual(t, maxActive, 2)
	assert.Equal(t, 4, connects)
}

func TestSubscriber_StartRetriesInvalidSourcesWhileHealthySourcesKeepRunning(t *testing.T) {
	t.Parallel()

	for _, withHealthy := range []bool{false, true} {
		t.Run(fmt.Sprintf("healthy_source_%v", withHealthy), func(t *testing.T) {
			t.Parallel()
			bad := reconcileTestSource(1, "broken")
			bad.PayloadFormat = "unknown"
			healthy := reconcileTestSource(2, "healthy")
			store := newFakeSourceStore(bad)
			if withHealthy {
				store.setSources(bad, healthy)
			}
			reg := &clientRegistry{}
			s := newReconcileTestSubscriber(t, store, reg)
			s.cfg.WatchdogTickEvery = 5 * time.Millisecond
			require.NoError(t, s.Start(t.Context()))
			t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
			assert.Equal(t, RuntimeStateError, s.SourceRuntimeStatus(bad.ID).State)
			if withHealthy {
				requireRunningBrokers(t, s, healthy.ID, 2)
			}
			bad.PayloadFormat = payloadFormatTargetTimestamp
			if withHealthy {
				store.setSources(bad, healthy)
			} else {
				store.setSources(bad)
			}
			requireRunningBrokers(t, s, bad.ID, 2)
			_, _, connects := reg.snapshot()
			wantConnects := 2
			if withHealthy {
				wantConnects = 4
			}
			assert.Equal(t, wantConnects, connects, "successful workers must not restart during source retries")
		})
	}
}

func TestSubscriber_StartupRetriesPreserveQuiescenceUntilSettingsReconcile(t *testing.T) {
	t.Parallel()

	for _, writeSucceeded := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable_write_succeeded_%v", writeSucceeded), func(t *testing.T) {
			t.Parallel()
			healthy := reconcileTestSource(1, "healthy")
			invalid := reconcileTestSource(2, "invalid")
			invalid.PayloadFormat = "unknown"
			sources := newFakeSourceStore(healthy, invalid)
			store := &failingListStore{Store: sources}
			reg := &clientRegistry{}
			s := newReconcileTestSubscriber(t, store, reg)
			s.cfg.WatchdogTickEvery = 5 * time.Millisecond
			require.NoError(t, s.Start(t.Context()))
			t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
			requireRunningBrokers(t, s, healthy.ID, 2)
			require.NoError(t, s.QuiesceSource(t.Context(), healthy.ID))
			// Leave the stored source enabled while a settings write is in flight.
			callsAfterQuiesce := store.calls.Load()
			require.Eventually(t, func() bool { return store.calls.Load() >= callsAfterQuiesce+3 }, time.Second, time.Millisecond)
			active, _, connects := reg.snapshot()
			assert.Zero(t, active, "background startup retry must not reopen a quiesced source")
			assert.Equal(t, 2, connects)
			if writeSucceeded {
				sources.setSources(invalid)
				require.Eventually(t, func() bool {
					s.mu.Lock()
					defer s.mu.Unlock()
					return len(s.activation.quiescedIDs) == 0
				}, time.Second, time.Millisecond, "disabled sources must not leave stale quiescence entries")
			}
			require.NoError(t, s.ReconcileSource(t.Context(), healthy.ID), "settings reconcile releases the pause after commit or rollback")
			if !writeSucceeded {
				requireRunningBrokers(t, s, healthy.ID, 2)
				_, _, connects = reg.snapshot()
				assert.Equal(t, 4, connects)
			} else {
				active, _, connects = reg.snapshot()
				assert.Zero(t, active)
				assert.Equal(t, 2, connects)
			}
			s.mu.Lock()
			assert.Empty(t, s.activation.quiescedIDs)
			s.mu.Unlock()
		})
	}
}

func TestSubscriber_QuiescenceDoesNotSurviveActivationRestart(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(t.Context()))
	requireRunningBrokers(t, s, src.ID, 2)
	require.NoError(t, s.QuiesceSource(t.Context(), src.ID))
	require.NoError(t, s.Stop(t.Context()))
	require.NoError(t, s.Start(t.Context()))
	requireRunningBrokers(t, s, src.ID, 2)
	require.NoError(t, s.Stop(t.Context()))
	_, _, connects := reg.snapshot()
	assert.Equal(t, 4, connects)
}

func TestSubscriber_StartupRetryWaitsForQuiescedSourceWithoutWorker(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	src.PayloadFormat = "unknown"
	sources := newFakeSourceStore(src)
	store := &failingListStore{Store: sources}
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	s.cfg.WatchdogTickEvery = 5 * time.Millisecond
	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
	require.NoError(t, s.QuiesceSource(t.Context(), src.ID))
	callsAfterQuiesce := store.calls.Load()
	require.Eventually(t, func() bool { return store.calls.Load() >= callsAfterQuiesce+2 }, time.Second, time.Millisecond,
		"startup retry must remain owned while settings are in flight")
	_, _, connects := reg.snapshot()
	assert.Zero(t, connects)

	// The disable write failed and its explicit reconcile still sees an invalid
	// source. Subsequent recovery must retain the original automatic retry loop.
	require.NoError(t, s.ReconcileSource(t.Context(), src.ID))
	src.PayloadFormat = payloadFormatTargetTimestamp
	sources.setSources(src)
	requireRunningBrokers(t, s, src.ID, 2)
}

func TestSubscriber_SourceCompletionSurvivesReconcileLockTimeout(t *testing.T) {
	t.Parallel()

	for _, disableCommitted := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable_committed_%v", disableCommitted), func(t *testing.T) {
			t.Parallel()
			src := reconcileTestSource(1, "maestro")
			sources := newFakeSourceStore(src)
			store := &failingListStore{Store: sources}
			reg := &clientRegistry{}
			s := newReconcileTestSubscriber(t, store, reg)
			s.cfg.WatchdogTickEvery = 5 * time.Millisecond
			require.NoError(t, s.Start(t.Context()))
			t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
			requireRunningBrokers(t, s, src.ID, 2)
			require.NoError(t, s.QuiesceSource(t.Context(), src.ID))
			if disableCommitted {
				sources.setSources()
			}
			// A separate reconciliation owns the lock longer than the settings
			// writer's completion budget, after a healthy startup with no errors.
			s.reconcileMu.Lock()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			err := s.ReconcileSource(ctx, src.ID)
			cancel()
			s.mu.Lock()
			queued := len(s.activation.resumeRequests)
			paused := len(s.activation.quiescedIDs)
			s.mu.Unlock()
			s.reconcileMu.Unlock()
			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Equal(t, 1, queued, "completion must stay queued after the caller times out")
			assert.Equal(t, 1, paused, "pause release must wait for the reconcile lock")
			if !disableCommitted {
				requireRunningBrokers(t, s, src.ID, 2)
			}
			require.Eventually(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return len(s.activation.resumeRequests) == 0 && len(s.activation.quiescedIDs) == 0
			}, time.Second, time.Millisecond)
			active, _, connects := reg.snapshot()
			if disableCommitted {
				assert.Zero(t, active)
				assert.Equal(t, 2, connects, "completed disable must never reconnect")
			} else {
				assert.Equal(t, 2, active)
				assert.Equal(t, 4, connects, "failed disable must eventually resume")
			}
		})
	}
}

func TestSubscriber_DelayedCompletionCannotReleaseNewerSourcePause(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, newFakeSourceStore(src), reg)
	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, s.Stop(context.Background())) })
	requireRunningBrokers(t, s, src.ID, 2)
	require.NoError(t, s.QuiesceSource(t.Context(), src.ID))
	// Model an old completed write queued behind reconcileMu, followed by a
	// newer writer's quiescence before the old completion is applied.
	s.mu.Lock()
	previousPause := s.activation.quiescedIDs[src.ID]
	newerPause := &sourcePause{sourceID: src.ID}
	s.activation.resumeRequests[src.ID] = previousPause
	s.activation.quiescedIDs[src.ID] = newerPause
	s.mu.Unlock()
	require.NoError(t, s.Reconcile(t.Context()))
	s.mu.Lock()
	assert.Same(t, newerPause, s.activation.quiescedIDs[src.ID])
	assert.Empty(t, s.activation.resumeRequests)
	s.mu.Unlock()
	_, _, connects := reg.snapshot()
	assert.Equal(t, 2, connects, "old completion must not restart the newer writer's source")
	require.NoError(t, s.ReconcileSource(t.Context(), src.ID))
	requireRunningBrokers(t, s, src.ID, 2)
}

type cancelableListStore struct {
	Store
	started chan struct{}
	once    sync.Once
}

func (s *cancelableListStore) ListEnabledSources(ctx context.Context) ([]SourceConfig, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return nil, fmt.Errorf("list enabled sources canceled: %w", ctx.Err())
}

func TestSubscriber_StartCancellationRemainsAnError(t *testing.T) {
	t.Parallel()

	store := &cancelableListStore{Store: newFakeSourceStore(), started: make(chan struct{})}
	s := newReconcileTestSubscriber(t, store, &clientRegistry{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	<-store.started
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled initial source listing did not drain")
	}
	require.NoError(t, s.Stop(t.Context()))
}

func TestSubscriber_StartContextCancellationAllowsRestartAfterWorkersDrain(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	runCtx, cancel := context.WithCancel(context.Background())
	require.NoError(t, s.Start(runCtx))
	requireRunningBrokers(t, s, src.ID, 2)

	cancel()
	require.Eventually(t, func() bool {
		return s.Start(context.Background()) == nil
	}, 2*time.Second, 10*time.Millisecond, "subscriber should restart after its canceled activation drains")
	requireRunningBrokers(t, s, src.ID, 2)

	_, maxActive, connects := reg.snapshot()
	assert.LessOrEqual(t, maxActive, 2, "replacement activation must not overlap canceled workers")
	assert.Equal(t, 4, connects)
	require.NoError(t, s.Stop(context.Background()))
}

func TestSubscriber_StopCanCancelBlockedInitialReconcile(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	store := &blockingListStore{
		Store:   newFakeSourceStore(),
		started: started,
		release: release,
	}
	s := newReconcileTestSubscriber(t, store, &clientRegistry{})

	startDone := make(chan error, 1)
	go func() {
		startDone <- s.Start(context.Background())
	}()
	<-started

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopCancel()
	require.ErrorIs(t, s.Stop(stopCtx), context.DeadlineExceeded)
	require.ErrorContains(t, s.Start(context.Background()), "previous subscriber activation is still stopping")

	close(release)
	require.ErrorContains(t, <-startDone, "subscriber is not started")
	require.NoError(t, s.Stop(context.Background()))
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Stop(context.Background()))
}

func TestSubscriber_ReconcileDoesNotStartWorkersAfterStop(t *testing.T) {
	store := newFakeSourceStore()
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))

	started := make(chan struct{})
	release := make(chan struct{})
	store.setSources(reconcileTestSource(1, "maestro"))
	s.cfg.Store = &blockingListStore{
		Store:   store,
		started: started,
		release: release,
	}

	reconcileDone := make(chan error, 1)
	go func() {
		reconcileDone <- s.Reconcile(context.Background())
	}()
	<-started

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- s.Stop(context.Background())
	}()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.activation != nil && channelClosed(s.activation.runCanceled)
	}, time.Second, 10*time.Millisecond)

	close(release)
	require.ErrorContains(t, <-reconcileDone, "subscriber is not started")
	require.NoError(t, <-stopDone)
	active, maxActive, connects := reg.snapshot()
	assert.Zero(t, active)
	assert.Zero(t, maxActive)
	assert.Zero(t, connects)
}

func TestSubscriber_CleanupStopsStatusesForUninstalledWorkers(t *testing.T) {
	s := newReconcileTestSubscriber(t, newFakeSourceStore(), &clientRegistry{})
	runCtx, cancel := context.WithCancel(t.Context())
	activation := &subscriberActivation{
		runCanceled: runCtx.Done(),
		cancel:      cancel,
		done:        make(chan struct{}),
		sourceIDs:   map[int64]struct{}{1: {}},
	}
	s.activation = activation
	s.statuses[1] = RuntimeStatus{State: RuntimeStateRunning}

	cancel()
	s.startActivationCleanup(activation)
	select {
	case <-activation.done:
	case <-time.After(time.Second):
		t.Fatal("subscriber activation did not finish cleanup")
	}
	assert.Equal(t, RuntimeStateStopped, s.SourceRuntimeStatus(1).State)
}

func TestSubscriber_StopTimeoutAllowsRestartAfterWorkersEventuallyDrain(t *testing.T) {
	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	release := make(chan struct{})
	s, err := NewSubscriber(Config{
		Store:             store,
		NewClient:         func() MQTTClient { return &blockingDisconnectClient{reg: reg, release: release} },
		Decryptor:         passthroughDecryptor{},
		Logger:            slog.New(slog.DiscardHandler),
		ShutdownDeadline:  time.Second,
		WatchdogTickEvery: time.Millisecond,
	})
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))
	requireRunningBrokers(t, s, src.ID, 2)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopCancel()
	require.ErrorIs(t, s.Stop(stopCtx), context.DeadlineExceeded)
	require.Error(t, s.Start(context.Background()), "timed-out workers must keep ownership of the activation")
	require.Error(t, s.Reconcile(context.Background()), "a stopping activation must reject new workers")

	close(release)
	require.Eventually(t, func() bool {
		return s.Start(context.Background()) == nil
	}, 2*time.Second, 10*time.Millisecond, "subscriber should restart after timed-out workers eventually drain")
	requireRunningBrokers(t, s, src.ID, 2)
	_, maxActive, _ := reg.snapshot()
	assert.LessOrEqual(t, maxActive, 2, "replacement activation must not overlap timed-out workers")
	require.NoError(t, s.Stop(context.Background()))
}

func TestSubscriber_ReconcileRestartsChangedSourceWithoutOverlap(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := &clientRegistry{}
	s := newReconcileTestSubscriber(t, store, reg)
	require.NoError(t, s.Start(context.Background()))
	defer func() { require.NoError(t, s.Stop(context.Background())) }()

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
	defer func() { require.NoError(t, s.Stop(context.Background())) }()

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
	defer func() { require.NoError(t, s.Stop(context.Background())) }()

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

func TestSubscriber_RuntimeStatusTracksBrokerDisconnectAndReconnect(t *testing.T) {
	t.Parallel()

	src := reconcileTestSource(1, "maestro")
	store := newFakeSourceStore(src)
	reg := newRuntimeStatusClientRegistry()
	s, err := NewSubscriber(Config{
		Store:            store,
		NewClient:        reg.newClient,
		Decryptor:        passthroughDecryptor{},
		Logger:           slog.New(slog.DiscardHandler),
		ShutdownDeadline: 2 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))
	defer func() { require.NoError(t, s.Stop(context.Background())) }()

	requireRuntimeStatus(t, s, src.ID, RuntimeStateRunning, 2, 2)

	reg.report(t, src.BrokerPrimaryHost, false, false, errors.New("primary broker lost"))
	status := requireRuntimeStatus(t, s, src.ID, RuntimeStateRunning, 1, 1)
	assert.Equal(t, "primary broker lost", status.LastError)

	reg.report(t, src.BrokerSecondaryHost, false, false, errors.New("secondary broker lost"))
	status = requireRuntimeStatus(t, s, src.ID, RuntimeStateError, 0, 0)
	assert.NotEmpty(t, status.LastError)

	reg.report(t, src.BrokerPrimaryHost, true, true, nil)
	status = requireRuntimeStatus(t, s, src.ID, RuntimeStateRunning, 1, 1)
	assert.Equal(t, "secondary broker lost", status.LastError)

	reg.report(t, src.BrokerSecondaryHost, true, true, nil)
	status = requireRuntimeStatus(t, s, src.ID, RuntimeStateRunning, 2, 2)
	assert.Empty(t, status.LastError)
}

func TestSubscriber_RuntimeStatusTreatsConnectedUnsubscribedErrorsAsError(t *testing.T) {
	t.Parallel()

	s, err := NewSubscriber(Config{
		Store:     newFakeSourceStore(),
		NewClient: func() MQTTClient { return &countingClient{} },
		Decryptor: passthroughDecryptor{},
		Logger:    slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	s.recordRuntimeStatus(RuntimeStatusUpdate{
		SourceID:   1,
		Broker:     "primary",
		Connected:  true,
		Subscribed: false,
		Error:      "mqttclient: resubscribe \"curtailment/source\": subscription rejected",
	})
	status := s.SourceRuntimeStatus(1)
	assert.Equal(t, RuntimeStateError, status.State)
	assert.Equal(t, 1, status.RunningBrokerCount)
	assert.Zero(t, status.SubscribedBrokerCount)
	assert.Contains(t, status.LastError, "subscription rejected")

	s.recordRuntimeStatus(RuntimeStatusUpdate{
		SourceID:   1,
		Broker:     "secondary",
		Connected:  true,
		Subscribed: false,
		Error:      "mqttclient: resubscribe \"curtailment/source\": not authorized",
	})
	status = s.SourceRuntimeStatus(1)
	assert.Equal(t, RuntimeStateError, status.State)
	assert.Equal(t, 2, status.RunningBrokerCount)
	assert.Zero(t, status.SubscribedBrokerCount)

	s.recordRuntimeStatus(RuntimeStatusUpdate{
		SourceID:   1,
		Broker:     "primary",
		Connected:  true,
		Subscribed: true,
	})
	status = s.SourceRuntimeStatus(1)
	assert.Equal(t, RuntimeStateRunning, status.State)
	assert.Equal(t, 2, status.RunningBrokerCount)
	assert.Equal(t, 1, status.SubscribedBrokerCount)
	assert.Contains(t, status.LastError, "not authorized")
}
