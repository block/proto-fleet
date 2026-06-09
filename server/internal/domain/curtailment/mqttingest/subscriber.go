package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// MQTTClient is one broker connection for one source.
type MQTTClient interface {
	Connect(ctx context.Context, host string, port int32, transport string, username, password, clientIdentity string) error
	Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error
	Disconnect(shutdownDeadline time.Duration)
}

// MQTTClientFactory builds a fresh client per source/broker.
type MQTTClientFactory func() MQTTClient

// PasswordDecryptor unwraps encrypted source credentials.
type PasswordDecryptor interface {
	Decrypt(encrypted string) ([]byte, error)
}

type RuntimeStatusUpdate struct {
	SourceID   int64
	Broker     string
	Connected  bool
	Subscribed bool
	Error      string
}

type RuntimeStatusReporter func(RuntimeStatusUpdate)

const (
	brokerTransportTCP = "tcp"
	brokerTransportTLS = "tls"
)

const workerRestartBackoffMax = 30 * time.Second

// Config bundles the subscriber's runtime dependencies and tunables.
type Config struct {
	Store             Store
	Driver            *Driver
	NewClient         MQTTClientFactory
	Decryptor         PasswordDecryptor
	Logger            *slog.Logger
	Clock             func() time.Time
	WatchdogTickEvery time.Duration
	BrokerFreshness   time.Duration
	ShutdownDeadline  time.Duration
	StatusReporter    RuntimeStatusReporter
}

type sourceWorkerHandle struct {
	worker      *sourceWorker
	cancel      context.CancelFunc
	done        <-chan struct{}
	fingerprint string
}

type brokerRuntimeStatus struct {
	connected  bool
	subscribed bool
	lastError  string
}

// Subscriber owns per-source workers.
type Subscriber struct {
	cfg            Config
	runDone        <-chan struct{}
	workers        map[int64]*sourceWorkerHandle
	statuses       map[int64]RuntimeStatus
	brokerStatuses map[int64]map[string]brokerRuntimeStatus
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	mu             sync.Mutex
	reconcileMu    sync.Mutex
}

// NewSubscriber validates dependencies and applies runtime defaults.
func NewSubscriber(cfg Config) (*Subscriber, error) {
	if cfg.Store == nil {
		return nil, errors.New("mqttingest: Store is required")
	}
	if cfg.Driver == nil {
		return nil, errors.New("mqttingest: Driver is required")
	}
	if cfg.NewClient == nil {
		return nil, errors.New("mqttingest: NewClient factory is required")
	}
	if cfg.Decryptor == nil {
		return nil, errors.New("mqttingest: Decryptor is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.WatchdogTickEvery <= 0 {
		cfg.WatchdogTickEvery = time.Second
	}
	if cfg.BrokerFreshness <= 0 {
		cfg.BrokerFreshness = 60 * time.Second
	}
	if cfg.ShutdownDeadline <= 0 {
		cfg.ShutdownDeadline = 10 * time.Second
	}
	s := &Subscriber{
		cfg:            cfg,
		workers:        make(map[int64]*sourceWorkerHandle),
		statuses:       make(map[int64]RuntimeStatus),
		brokerStatuses: make(map[int64]map[string]brokerRuntimeStatus),
	}
	externalReporter := cfg.StatusReporter
	s.cfg.StatusReporter = func(update RuntimeStatusUpdate) {
		s.recordRuntimeStatus(update)
		if externalReporter != nil {
			externalReporter(update)
		}
	}
	return s, nil
}

// Start starts the runtime and performs the initial source reconciliation.
func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return errors.New("mqttingest: subscriber already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.runDone = runCtx.Done()
	s.cancel = cancel
	s.workers = make(map[int64]*sourceWorkerHandle)
	s.mu.Unlock()

	if _, _, err := s.reconcile(runCtx, true); err != nil {
		cancel()
		s.mu.Lock()
		s.runDone = nil
		s.cancel = nil
		s.workers = make(map[int64]*sourceWorkerHandle)
		s.mu.Unlock()
		return err
	}
	return nil
}

// Reconcile applies enabled-source settings to the running subscriber.
func (s *Subscriber) Reconcile(ctx context.Context) error {
	_, _, err := s.reconcile(ctx, false)
	return err
}

// Stop cancels all workers and waits up to ShutdownDeadline for them to drain.
func (s *Subscriber) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	if cancel == nil {
		s.mu.Unlock()
		return
	}
	handles := make([]*sourceWorkerHandle, 0, len(s.workers))
	for _, handle := range s.workers {
		handles = append(handles, handle)
	}
	s.runDone = nil
	s.cancel = nil
	s.mu.Unlock()

	for _, handle := range handles {
		handle.cancel()
	}
	cancel()
	s.cfg.Logger.Info("mqttingest subscriber draining workers",
		slog.Duration("deadline", s.cfg.ShutdownDeadline))
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.cfg.Logger.Info("mqttingest subscriber stopped cleanly")
	case <-time.After(s.cfg.ShutdownDeadline):
		s.cfg.Logger.Warn("mqttingest subscriber shutdown deadline exceeded")
	}

	s.mu.Lock()
	for sourceID := range s.workers {
		s.setSourceStatusLocked(sourceID, RuntimeStateStopped, "")
	}
	s.workers = make(map[int64]*sourceWorkerHandle)
	s.brokerStatuses = make(map[int64]map[string]brokerRuntimeStatus)
	s.mu.Unlock()
}

// Run starts enabled sources once and blocks until ctx is canceled.
func (s *Subscriber) Run(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	s.Stop()
	return nil
}

func (s *Subscriber) SourceRuntimeStatus(sourceID int64) RuntimeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.statuses[sourceID]
	return status
}

func (s *Subscriber) reconcile(ctx context.Context, failIfNoneStarted bool) (int, int, error) {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	runDone := s.runDone
	existing := make(map[int64]*sourceWorkerHandle, len(s.workers))
	for id, handle := range s.workers {
		existing[id] = handle
	}
	s.mu.Unlock()
	if runDone == nil {
		return 0, 0, errors.New("mqttingest: subscriber is not started")
	}

	sources, err := s.cfg.Store.ListEnabledSources(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("mqttingest: list enabled sources: %w", err)
	}
	s.cfg.Logger.Info("mqttingest subscriber reconciling", slog.Int("source_count", len(sources)))

	desired := make(map[int64]SourceConfig, len(sources))
	for _, src := range sources {
		desired[src.ID] = src
	}
	stopping := make([]*sourceWorkerHandle, 0)
	for sourceID, handle := range existing {
		src, ok := desired[sourceID]
		if ok && sourceConfigFingerprint(src) == handle.fingerprint {
			continue
		}
		handle.cancel()
		stopping = append(stopping, handle)
	}
	for _, handle := range stopping {
		if err := s.waitForHandleStopped(ctx, handle); err != nil {
			s.recordSourceError(handle.worker.source.ID, err.Error())
			return 0, len(sources), err
		}
		s.mu.Lock()
		if current, stillCurrent := s.workers[handle.worker.source.ID]; stillCurrent && current == handle {
			delete(s.workers, handle.worker.source.ID)
			delete(s.brokerStatuses, handle.worker.source.ID)
			s.setSourceStatusLocked(handle.worker.source.ID, RuntimeStateStopped, "")
		}
		s.mu.Unlock()
	}

	started := 0
	var firstStartErr error
	for _, src := range sources {
		fingerprint := sourceConfigFingerprint(src)
		s.mu.Lock()
		current, ok := s.workers[src.ID]
		if ok && current.fingerprint == fingerprint {
			s.mu.Unlock()
			continue
		}
		s.setSourceStatusLocked(src.ID, RuntimeStateStarting, "")
		s.brokerStatuses[src.ID] = make(map[string]brokerRuntimeStatus)
		s.mu.Unlock()

		workerCtx, workerCancel := contextWithDone(runDone)
		w, done, err := s.startWorker(workerCtx, src, &s.wg)
		if err != nil {
			workerCancel()
			if firstStartErr == nil {
				firstStartErr = err
			}
			s.recordSourceError(src.ID, err.Error())
			s.cfg.Logger.Error("mqttingest: start worker failed",
				slog.String("source", src.SourceName),
				slog.Any("error", err))
			continue
		}

		handle := &sourceWorkerHandle{
			worker:      w,
			cancel:      workerCancel,
			done:        done,
			fingerprint: fingerprint,
		}
		s.mu.Lock()
		if s.runDone != runDone {
			s.mu.Unlock()
			workerCancel()
			continue
		}
		if previous, ok := s.workers[src.ID]; ok {
			previous.cancel()
		}
		s.workers[src.ID] = handle
		s.mu.Unlock()
		started++
	}

	if failIfNoneStarted && len(sources) > 0 {
		s.mu.Lock()
		runningCount := len(s.workers)
		s.mu.Unlock()
		if runningCount == 0 {
			if firstStartErr == nil {
				return started, len(sources), fmt.Errorf("mqttingest: no enabled sources started (source_count=%d)", len(sources))
			}
			return started, len(sources), fmt.Errorf("mqttingest: no enabled sources started (source_count=%d): %w", len(sources), firstStartErr)
		}
	}
	return started, len(sources), nil
}

func contextWithDone(done <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (s *Subscriber) waitForHandleStopped(ctx context.Context, handle *sourceWorkerHandle) error {
	if handle == nil || handle.done == nil {
		return nil
	}
	timer := time.NewTimer(s.cfg.ShutdownDeadline)
	defer timer.Stop()
	select {
	case <-handle.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqttingest: stop source %s: %w", handle.worker.source.SourceName, ctx.Err())
	case <-timer.C:
		return fmt.Errorf("mqttingest: source %s worker did not stop within %s",
			handle.worker.source.SourceName,
			s.cfg.ShutdownDeadline,
		)
	}
}

// startWorker boots one source's worker goroutine.
func (s *Subscriber) startWorker(ctx context.Context, src SourceConfig, wg *sync.WaitGroup) (*sourceWorker, <-chan struct{}, error) {
	primary, secondary, ok := ResolveBrokerRoles(src.BrokerPrimaryHost, src.BrokerSecondaryHost)
	if !ok {
		return nil, nil, fmt.Errorf("mqttingest: source %s has identical broker hosts", src.SourceName)
	}
	if err := validateBrokerTransport(src, primary, secondary); err != nil {
		return nil, nil, err
	}
	if _, err := scopeForSource(src); err != nil {
		return nil, nil, fmt.Errorf("mqttingest: source %s invalid scope: %w", src.SourceName, err)
	}

	// The service user must hold the machine-ingest permission for the org it can curtail.
	canIngest, err := s.cfg.Store.UserCanIngestCurtailment(ctx, src.ServiceUserID, src.OrganizationID)
	if err != nil {
		return nil, nil, fmt.Errorf("mqttingest: verify service user for %s: %w", src.SourceName, err)
	}
	if !canIngest {
		return nil, nil, fmt.Errorf("mqttingest: source %s service user %d lacks curtailment:ingest in org %d",
			src.SourceName, src.ServiceUserID, src.OrganizationID)
	}

	decoder, err := decoderForFormat(src.PayloadFormat)
	if err != nil {
		return nil, nil, fmt.Errorf("mqttingest: source %s: %w", src.SourceName, err)
	}

	password, err := s.cfg.Decryptor.Decrypt(src.MQTTPasswordEncrypted)
	if err != nil {
		return nil, nil, fmt.Errorf("mqttingest: decrypt password for %s: %w", src.SourceName, err)
	}

	workerPassword := string(password)
	w := &sourceWorker{
		cfg:           s.cfg,
		source:        src,
		decoder:       decoder,
		primaryHost:   primary,
		secondaryHost: secondary,
	}
	// Bound plaintext credentials to the worker lifetime.
	clear(password)

	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer close(done)
		s.superviseWorker(ctx, src, decoder, primary, secondary, workerPassword, wg)
	}()
	return w, done, nil
}

func (s *Subscriber) superviseWorker(
	ctx context.Context,
	src SourceConfig,
	decoder PayloadDecoder,
	primary string,
	secondary string,
	password string,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer func() {
		password = ""
	}()

	backoff := startupRetryEveryFor(s.cfg.WatchdogTickEvery)
	for {
		w := &sourceWorker{
			cfg:           s.cfg,
			source:        src,
			decoder:       decoder,
			primaryHost:   primary,
			secondaryHost: secondary,
			password:      password,
		}
		panicked := s.runWorkerOnce(ctx, w)
		w.password = ""
		if !panicked || ctx.Err() != nil {
			return
		}

		retryAfter := backoff
		s.cfg.Logger.Warn("mqttingest: restarting source worker after panic",
			slog.String("source", src.SourceName),
			slog.Duration("retry_after", retryAfter))
		timer := time.NewTimer(retryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = nextWorkerRestartBackoff(backoff)
	}
}

func (s *Subscriber) runWorkerOnce(ctx context.Context, w *sourceWorker) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			s.recordSourceError(w.source.ID, fmt.Sprintf("source worker panic: %v", r))
			s.cfg.Logger.Error("mqttingest: source worker panic",
				slog.String("source", w.source.SourceName),
				slog.Any("panic", r))
		}
	}()
	w.run(ctx)
	return false
}

func (s *Subscriber) recordRuntimeStatus(update RuntimeStatusUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if update.SourceID <= 0 || update.Broker == "" {
		return
	}
	if s.brokerStatuses[update.SourceID] == nil {
		s.brokerStatuses[update.SourceID] = make(map[string]brokerRuntimeStatus)
	}
	s.brokerStatuses[update.SourceID][update.Broker] = brokerRuntimeStatus{
		connected:  update.Connected,
		subscribed: update.Subscribed,
		lastError:  update.Error,
	}
	running := 0
	subscribed := 0
	lastError := ""
	for _, broker := range s.brokerStatuses[update.SourceID] {
		if broker.connected {
			running++
		}
		if broker.subscribed {
			subscribed++
		}
		if broker.lastError != "" {
			lastError = broker.lastError
		}
	}
	state := RuntimeStateRunning
	if running == 0 {
		state = RuntimeStateStarting
		if lastError != "" {
			state = RuntimeStateError
		}
	}
	status := s.statuses[update.SourceID]
	status.State = state
	status.LastError = lastError
	status.RunningBrokerCount = running
	status.SubscribedBrokerCount = subscribed
	status.UpdatedAt = s.cfg.Clock()
	s.statuses[update.SourceID] = status
}

func (s *Subscriber) recordSourceError(sourceID int64, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setSourceStatusLocked(sourceID, RuntimeStateError, message)
}

func (s *Subscriber) setSourceStatusLocked(sourceID int64, state RuntimeState, message string) {
	status := s.statuses[sourceID]
	status.State = state
	status.LastError = message
	if state == RuntimeStateStarting || state == RuntimeStateStopped || state == RuntimeStateDisabled {
		status.RunningBrokerCount = 0
		status.SubscribedBrokerCount = 0
	}
	status.UpdatedAt = s.cfg.Clock()
	s.statuses[sourceID] = status
}

func startupRetryEveryFor(tick time.Duration) time.Duration {
	if tick > 0 && tick < time.Second {
		return tick
	}
	return time.Second
}

func nextWorkerRestartBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next <= 0 {
		return time.Second
	}
	if next > workerRestartBackoffMax {
		return workerRestartBackoffMax
	}
	return next
}

func validateBrokerTransport(src SourceConfig, hosts ...string) error {
	switch src.BrokerTransport {
	case "", brokerTransportTCP:
		for _, host := range hosts {
			addr, err := netip.ParseAddr(host)
			if err != nil || !(addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()) {
				return fmt.Errorf("mqttingest: source %s uses tcp transport with non-local broker host %q", src.SourceName, host)
			}
		}
		return nil
	case brokerTransportTLS:
		return nil
	default:
		return fmt.Errorf("mqttingest: source %s has unsupported broker_transport %q", src.SourceName, src.BrokerTransport)
	}
}

func sourceConfigFingerprint(src SourceConfig) string {
	siteID := ""
	if src.ScopeSiteID != nil {
		siteID = fmt.Sprintf("%d", *src.ScopeSiteID)
	}
	return strings.Join([]string{
		fmt.Sprintf("%d", src.OrganizationID),
		fmt.Sprintf("%d", src.ServiceUserID),
		src.SourceName,
		src.Topic,
		src.BrokerPrimaryHost,
		src.BrokerSecondaryHost,
		fmt.Sprintf("%d", src.BrokerPort),
		src.BrokerTransport,
		src.MQTTUsername,
		src.MQTTPasswordEncrypted,
		fmt.Sprintf("%d", src.ContractedCurtailmentKw),
		src.CurtailMode,
		src.PayloadFormat,
		src.ScopeType,
		siteID,
		strings.Join(src.ScopeDeviceIdentifiers, "\x1f"),
		fmt.Sprintf("%d", int64(src.StalenessThreshold/time.Second)),
		fmt.Sprintf("%d", int64(src.MinCurtailedDuration/time.Second)),
	}, "\x1e")
}
