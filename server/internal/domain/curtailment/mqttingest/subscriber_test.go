package mqttingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// fakeStore is an in-memory Store satisfying the subscriber's read/
// write surface. Tests preload sources and inspect state after the
// subscriber drains.
type fakeStore struct {
	mu          sync.Mutex
	sources     []SourceConfig
	state       map[int64]SourceState
	getStateErr error
}

func newFakeStore(sources ...SourceConfig) *fakeStore {
	return &fakeStore{sources: sources, state: make(map[int64]SourceState)}
}

func (f *fakeStore) ListEnabledSources(_ context.Context) ([]SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]SourceConfig, len(f.sources))
	copy(cp, f.sources)
	return cp, nil
}

func (f *fakeStore) GetSourceState(_ context.Context, id int64) (SourceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getStateErr != nil {
		return SourceState{}, f.getStateErr
	}
	s, ok := f.state[id]
	if !ok {
		return SourceState{}, ErrSourceStateNotFound
	}
	return s, nil
}

func (f *fakeStore) UpsertSourceState(_ context.Context, u StateUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing := f.state[u.SourceConfigID]
	existing.SourceConfigID = u.SourceConfigID
	if u.LastTarget != nil {
		existing.LastTarget = *u.LastTarget
	}
	if u.LastTargetAt != nil {
		existing.LastTargetAt = *u.LastTargetAt
	}
	if u.LastReceivedAt != nil {
		existing.LastReceivedAt = *u.LastReceivedAt
	}
	if u.LastReceivedBroker != nil {
		existing.LastReceivedBroker = *u.LastReceivedBroker
	}
	if u.LastEdgeAt != nil {
		existing.LastEdgeAt = *u.LastEdgeAt
	}
	if u.LastEdgeEventUUID != nil {
		existing.LastEdgeEventUUID = *u.LastEdgeEventUUID
	}
	f.state[u.SourceConfigID] = existing
	return nil
}

func (f *fakeStore) ListSourcesForWatchdog(_ context.Context) ([]WatchdogRow, error) {
	return nil, nil // unused by the subscriber test path
}

// fakeMQTTClient delivers operator-injected payloads on a single
// topic. Connect / Subscribe record the call sequence; Publish on the
// returned channel routes through the handler the subscriber
// registered.
type fakeMQTTClient struct {
	mu            sync.Mutex
	host          string
	subscribed    bool
	connectBlocks bool
	disconnect    chan struct{}
	handler       func(payload []byte, receivedAt time.Time)
	ready         chan struct{}
}

func newFakeMQTTClient() *fakeMQTTClient {
	return &fakeMQTTClient{
		disconnect: make(chan struct{}),
		ready:      make(chan struct{}),
	}
}

func (f *fakeMQTTClient) Connect(ctx context.Context, host string, _ int32, _ string, _ string) error {
	if f.connectBlocks {
		<-ctx.Done()
		return fmt.Errorf("fake mqtt connect canceled: %w", ctx.Err())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.host = host
	return nil
}

func (f *fakeMQTTClient) Subscribe(_ context.Context, _ string, handler func(payload []byte, receivedAt time.Time)) error {
	f.mu.Lock()
	f.subscribed = true
	f.handler = handler
	f.mu.Unlock()
	close(f.ready)
	return nil
}

func (f *fakeMQTTClient) Disconnect(_ time.Duration) {
	select {
	case <-f.disconnect:
	default:
		close(f.disconnect)
	}
}

func (f *fakeMQTTClient) deliver(payload []byte, receivedAt time.Time) {
	<-f.ready
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		h(payload, receivedAt)
	}
}

// passthroughDecryptor returns the input unchanged. Tests don't
// exercise the encryption path.
type passthroughDecryptor struct{}

func (passthroughDecryptor) Decrypt(s string) ([]byte, error) { return []byte(s), nil }

func TestSubscriber_Run_DispatchesOnOffEdge(t *testing.T) {
	t.Parallel()

	src := SourceConfig{
		ID:                      1,
		OrganizationID:          7,
		ServiceUserID:           99,
		SourceName:              "site-a",
		Topic:                   "vendor/target",
		BrokerPrimaryHost:       "10.0.0.1",
		BrokerSecondaryHost:     "10.0.0.2",
		BrokerPort:              1883,
		MQTTUsername:            "user",
		MQTTPasswordEncrypted:   "pw",
		ContractedCurtailmentKw: 12500,
		StalenessThreshold:      240 * time.Second,
		MinCurtailedDuration:    600 * time.Second,
		Enabled:                 true,
	}

	store := newFakeStore(src)

	newUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &newUUID}}
	driver := NewDriver(svc, nil)

	// Two fake clients — primary and secondary. We deliver the OFF
	// message on the primary; precedence dedup makes it canonical.
	var clients []*fakeMQTTClient
	var clientsMu sync.Mutex
	factory := func() MQTTClient {
		c := newFakeMQTTClient()
		clientsMu.Lock()
		clients = append(clients, c)
		clientsMu.Unlock()
		return c
	}

	cfg := Config{
		Store:             store,
		Driver:            driver,
		NewClient:         factory,
		Decryptor:         passthroughDecryptor{},
		Logger:            slog.New(slog.DiscardHandler),
		WatchdogTickEvery: 24 * time.Hour, // effectively disabled for this test
		ShutdownDeadline:  time.Second,
	}
	sub, err := NewSubscriber(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneRun := make(chan error, 1)
	go func() { doneRun <- sub.Run(ctx) }()

	// Wait for both clients to subscribe.
	waitForClients := func() {
		deadline := time.After(2 * time.Second)
		for {
			clientsMu.Lock()
			ready := len(clients) == 2
			clientsMu.Unlock()
			if ready {
				return
			}
			select {
			case <-deadline:
				t.Fatal("clients never registered")
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	waitForClients()

	clientsMu.Lock()
	primary := clients[0]
	clientsMu.Unlock()

	// Deliver an OFF payload on the primary broker.
	now := time.Now().UTC()
	off := map[string]any{"target": 0, "timestamp": now.Unix()}
	body, err := json.Marshal(off)
	require.NoError(t, err)
	primary.deliver(body, now)

	// Drain until the driver's start call lands.
	assertEventually(t, 2*time.Second, func() bool {
		return svc.startCallsLen() == 1
	})

	cancel()
	select {
	case <-doneRun:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop after context cancel")
	}

	require.Equal(t, 1, svc.startCallsLen())
	start := svc.startCallAt(0)
	require.NotNil(t, start.ExternalReference)
	assert.Contains(t, *start.ExternalReference, "site-a:")
	assert.Equal(t, models.PriorityEmergency, start.Priority)

	// State persisted: last target should be OFF and edge UUID set.
	s, err := store.GetSourceState(context.Background(), src.ID)
	require.NoError(t, err)
	assert.Equal(t, TargetOff, s.LastTarget)
	assert.Equal(t, newUUID.String(), s.LastEdgeEventUUID)
}

func TestSubscriber_NewSubscriber_RejectsMissingDeps(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	driver := NewDriver(&fakeService{}, nil)
	factory := func() MQTTClient { return newFakeMQTTClient() }

	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "missing store",
			cfg:  Config{Driver: driver, NewClient: factory, Decryptor: passthroughDecryptor{}},
			want: "Store is required",
		},
		{
			name: "missing driver",
			cfg:  Config{Store: store, NewClient: factory, Decryptor: passthroughDecryptor{}},
			want: "Driver is required",
		},
		{
			name: "missing client factory",
			cfg:  Config{Store: store, Driver: driver, Decryptor: passthroughDecryptor{}},
			want: "NewClient factory is required",
		},
		{
			name: "missing decryptor",
			cfg:  Config{Store: store, Driver: driver, NewClient: factory},
			want: "Decryptor is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewSubscriber(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func assertEventually(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.After(within)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition did not become true within deadline")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Non-positive durations are treated as unset and defaulted, so a misconfigured
// caller can't make time.NewTicker panic in the worker run loop.
func TestNewSubscriber_NonPositiveDurationsDefault(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Store:             newFakeStore(),
		Driver:            NewDriver(&fakeService{}, nil),
		NewClient:         func() MQTTClient { return newFakeMQTTClient() },
		Decryptor:         passthroughDecryptor{},
		WatchdogTickEvery: -1 * time.Second,
		BrokerFreshness:   -5 * time.Second,
		ShutdownDeadline:  -1 * time.Second,
	}
	sub, err := NewSubscriber(cfg)
	require.NoError(t, err)
	assert.Greater(t, sub.cfg.WatchdogTickEvery, time.Duration(0))
	assert.Greater(t, sub.cfg.BrokerFreshness, time.Duration(0))
	assert.Greater(t, sub.cfg.ShutdownDeadline, time.Duration(0))
}
