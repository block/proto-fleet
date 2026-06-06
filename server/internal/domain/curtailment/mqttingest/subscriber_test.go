package mqttingest

import (
	"context"
	"encoding/json"
	"errors"
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
	mu             sync.Mutex
	sources        []SourceConfig
	state          map[int64]SourceState
	listSourcesErr error
	getStateErr    error
	getStateErrs   []error
	getStatePanics int
	upsertErr      error
	upsertErrs     []error
	nonMembers     map[int64]bool // user IDs treated as not belonging to their org
	nonIngestUsers map[userOrgKey]bool
}

type userOrgKey struct {
	userID int64
	orgID  int64
}

func newFakeStore(sources ...SourceConfig) *fakeStore {
	return &fakeStore{sources: sources, state: make(map[int64]SourceState)}
}

func (f *fakeStore) ListEnabledSources(_ context.Context) ([]SourceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listSourcesErr != nil {
		return nil, f.listSourcesErr
	}
	cp := make([]SourceConfig, len(f.sources))
	copy(cp, f.sources)
	return cp, nil
}

func (f *fakeStore) GetSourceState(_ context.Context, id int64) (SourceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getStatePanics > 0 {
		f.getStatePanics--
		panic("fake get source state panic")
	}
	if f.getStateErr != nil {
		return SourceState{}, f.getStateErr
	}
	if len(f.getStateErrs) > 0 {
		err := f.getStateErrs[0]
		f.getStateErrs = f.getStateErrs[1:]
		if err != nil {
			return SourceState{}, err
		}
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
	if f.upsertErr != nil {
		return f.upsertErr
	}
	if len(f.upsertErrs) > 0 {
		err := f.upsertErrs[0]
		f.upsertErrs = f.upsertErrs[1:]
		if err != nil {
			return err
		}
	}
	f.state[u.SourceConfigID] = SourceState{
		SourceConfigID:                u.SourceConfigID,
		LastTarget:                    u.LastTarget,
		LastTargetAt:                  u.LastTargetAt,
		LastProcessedTarget:           u.LastProcessedTarget,
		LastProcessedTargets:          append([]Target(nil), u.LastProcessedTargets...),
		LastReceivedAt:                u.LastReceivedAt,
		LastReceivedBroker:            u.LastReceivedBroker,
		LastEdgeAt:                    u.LastEdgeAt,
		LastEdgeEventUUID:             u.LastEdgeEventUUID,
		PendingEdge:                   clonePendingEdge(u.PendingEdge),
		LastEmptyFullFleetWatchdogRef: u.LastEmptyFullFleetWatchdogRef,
	}
	return nil
}

func (f *fakeStore) UserCanIngestCurtailment(_ context.Context, userID, orgID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nonIngestUsers != nil && f.nonIngestUsers[userOrgKey{userID: userID, orgID: orgID}] {
		return false, nil
	}
	return !f.nonMembers[userID], nil
}

func clonePendingEdge(edge *PendingEdge) *PendingEdge {
	if edge == nil {
		return nil
	}
	cp := *edge
	return &cp
}

// fakeMQTTClient records Connect / Subscribe and routes deliver() calls through
// the subscribed handler.
type fakeMQTTClient struct {
	mu            sync.Mutex
	host          string
	subscribed    bool
	connectBlocks bool
	connectErrs   []error
	connectCalls  int
	connected     bool
	disconnect    chan struct{}
	handler       func(payload []byte, receivedAt time.Time)
	ready         chan struct{}
	readyClosed   bool
}

func newFakeMQTTClient() *fakeMQTTClient {
	return &fakeMQTTClient{
		disconnect: make(chan struct{}),
		ready:      make(chan struct{}),
	}
}

func (f *fakeMQTTClient) Connect(ctx context.Context, host string, _ int32, _ string, _ string, _ string, _ string) error {
	if f.connectBlocks {
		<-ctx.Done()
		return fmt.Errorf("fake mqtt connect canceled: %w", ctx.Err())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectCalls++
	if len(f.connectErrs) > 0 {
		err := f.connectErrs[0]
		f.connectErrs = f.connectErrs[1:]
		if err != nil {
			return err
		}
	}
	f.host = host
	f.connected = true
	f.markReadyLocked()
	return nil
}

func (f *fakeMQTTClient) Subscribe(_ context.Context, _ string, handler func(payload []byte, receivedAt time.Time)) error {
	f.mu.Lock()
	f.subscribed = true
	f.handler = handler
	f.markReadyLocked()
	f.mu.Unlock()
	return nil
}

func (f *fakeMQTTClient) markReadyLocked() {
	if f.connected && f.subscribed && !f.readyClosed {
		close(f.ready)
		f.readyClosed = true
	}
}

func (f *fakeMQTTClient) connectCallsLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connectCalls
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

type countingDecryptor struct {
	calls int
}

func (d *countingDecryptor) Decrypt(s string) ([]byte, error) {
	d.calls++
	return []byte(s), nil
}

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
	driver := NewDriver(svc)

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

// A source whose service user lacks curtailment:ingest is rejected at startup:
// the worker is not started, so any org member cannot drive emergency
// curtailment just by being referenced from the source row.
func TestSubscriber_StartWorker_RejectsServiceUserWithoutIngestPermission(t *testing.T) {
	t.Parallel()

	src := SourceConfig{
		ID:                  1,
		OrganizationID:      7,
		ServiceUserID:       99,
		SourceName:          "site-a",
		BrokerPrimaryHost:   "10.0.0.1",
		BrokerSecondaryHost: "10.0.0.2",
		Enabled:             true,
	}

	store := newFakeStore(src)
	store.nonIngestUsers = map[userOrgKey]bool{{userID: 99, orgID: 7}: true}

	cfg := Config{
		Store:            store,
		Driver:           NewDriver(&fakeService{}),
		NewClient:        func() MQTTClient { return newFakeMQTTClient() },
		Decryptor:        passthroughDecryptor{},
		Logger:           slog.New(slog.DiscardHandler),
		ShutdownDeadline: time.Second,
	}
	sub, err := NewSubscriber(cfg)
	require.NoError(t, err)

	var wg sync.WaitGroup
	w, err := sub.startWorker(context.Background(), src, &wg)

	require.Error(t, err)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), "lacks curtailment:ingest")
}

func TestSubscriber_StartWorker_RejectsUnsupportedSiteScopeAtStartup(t *testing.T) {
	t.Parallel()

	siteID := int64(42)
	src := SourceConfig{
		ID:                  1,
		OrganizationID:      7,
		ServiceUserID:       99,
		SourceName:          "site-a",
		BrokerPrimaryHost:   "10.0.0.1",
		BrokerSecondaryHost: "10.0.0.2",
		ScopeType:           "site",
		ScopeSiteID:         &siteID,
		Enabled:             true,
	}
	store := newFakeStore(src)

	cfg := Config{
		Store:            store,
		Driver:           NewDriver(&fakeService{}),
		NewClient:        func() MQTTClient { return newFakeMQTTClient() },
		Decryptor:        passthroughDecryptor{},
		Logger:           slog.New(slog.DiscardHandler),
		ShutdownDeadline: time.Second,
	}
	sub, err := NewSubscriber(cfg)
	require.NoError(t, err)

	var wg sync.WaitGroup
	w, err := sub.startWorker(context.Background(), src, &wg)

	require.Error(t, err)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), "unsupported scope type")
}

func TestSubscriber_StartWorker_ValidatesDecoderBeforeDecrypt(t *testing.T) {
	t.Parallel()

	src := SourceConfig{
		ID:                    1,
		OrganizationID:        7,
		ServiceUserID:         99,
		SourceName:            "site-a",
		BrokerPrimaryHost:     "10.0.0.1",
		BrokerSecondaryHost:   "10.0.0.2",
		PayloadFormat:         "nope",
		MQTTPasswordEncrypted: "pw",
		Enabled:               true,
	}
	store := newFakeStore(src)
	decryptor := &countingDecryptor{}

	cfg := Config{
		Store:            store,
		Driver:           NewDriver(&fakeService{}),
		NewClient:        func() MQTTClient { return newFakeMQTTClient() },
		Decryptor:        decryptor,
		Logger:           slog.New(slog.DiscardHandler),
		ShutdownDeadline: time.Second,
	}
	sub, err := NewSubscriber(cfg)
	require.NoError(t, err)

	var wg sync.WaitGroup
	w, err := sub.startWorker(context.Background(), src, &wg)

	require.Error(t, err)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), "unknown payload_format")
	assert.Zero(t, decryptor.calls, "invalid decoder config must fail before decrypting credentials")
}

func TestValidateBrokerTransport_TCPAllowsPrivateMaestroHosts(t *testing.T) {
	t.Parallel()

	src := SourceConfig{SourceName: "site-a", BrokerTransport: brokerTransportTCP}

	err := validateBrokerTransport(src, "10.155.0.3", "10.155.0.4")

	require.NoError(t, err)
}

func TestValidateBrokerTransport_TCPRejectsPublicHosts(t *testing.T) {
	t.Parallel()

	src := SourceConfig{SourceName: "site-a", BrokerTransport: brokerTransportTCP}

	err := validateBrokerTransport(src, "203.0.113.1", "10.155.0.4")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-local broker")
}

func TestValidateBrokerTransport_TLSAllowsDNSHosts(t *testing.T) {
	t.Parallel()

	src := SourceConfig{SourceName: "site-a", BrokerTransport: brokerTransportTLS}

	err := validateBrokerTransport(src, "broker.example.com", "backup.example.com")

	require.NoError(t, err)
}

func TestSubscriber_Start_ReturnsListSourcesError(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.listSourcesErr = errors.New("db down")
	sub, err := NewSubscriber(Config{
		Store:     store,
		Driver:    NewDriver(&fakeService{}),
		NewClient: func() MQTTClient { return newFakeMQTTClient() },
		Decryptor: passthroughDecryptor{},
		Logger:    slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	err = sub.Start(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list enabled sources")
	sub.Stop()
}

func TestSubscriber_Start_ReturnsErrorWhenAllSourcesFail(t *testing.T) {
	t.Parallel()

	src := SourceConfig{
		ID:                  1,
		OrganizationID:      7,
		ServiceUserID:       99,
		SourceName:          "site-a",
		BrokerPrimaryHost:   "10.0.0.1",
		BrokerSecondaryHost: "10.0.0.1",
		Enabled:             true,
	}
	store := newFakeStore(src)
	sub, err := NewSubscriber(Config{
		Store:     store,
		Driver:    NewDriver(&fakeService{}),
		NewClient: func() MQTTClient { return newFakeMQTTClient() },
		Decryptor: passthroughDecryptor{},
		Logger:    slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	err = sub.Start(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enabled sources started")
	assert.Nil(t, sub.cancel)
}

func TestSubscriber_WorkerPanicRestartsSource(t *testing.T) {
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
		ContractedCurtailmentKw: 12500,
		StalenessThreshold:      240 * time.Second,
		MinCurtailedDuration:    600 * time.Second,
		Enabled:                 true,
	}
	store := newFakeStore(src)
	store.getStatePanics = 1
	store.state[src.ID] = SourceState{SourceConfigID: src.ID, LastTarget: TargetOff}
	eventUUID := uuid.New()
	svc := &fakeService{startResult: &curtailment.Plan{EventUUID: &eventUUID}}
	sub, err := NewSubscriber(Config{
		Store:  store,
		Driver: NewDriver(svc),
		NewClient: func() MQTTClient {
			c := newFakeMQTTClient()
			c.connectBlocks = true
			return c
		},
		Decryptor:         passthroughDecryptor{},
		Logger:            slog.New(slog.DiscardHandler),
		WatchdogTickEvery: 10 * time.Millisecond,
		ShutdownDeadline:  time.Second,
	})
	require.NoError(t, err)

	require.NoError(t, sub.Start(context.Background()))
	defer sub.Stop()

	assertEventually(t, 2*time.Second, func() bool {
		return svc.startCallsLen() >= 1
	})
}

func TestSubscriber_NewSubscriber_RejectsMissingDeps(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	driver := NewDriver(&fakeService{})
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
		Driver:            NewDriver(&fakeService{}),
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
