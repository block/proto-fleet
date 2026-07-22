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

	"github.com/block/proto-fleet/server/internal/runtimejobs"
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

type runtimeStatusReportingMQTTClient interface {
	SetRuntimeStatusReporter(reporter func(connected bool, subscribed bool, err error))
}

const (
	brokerTransportTCP = "tcp"
	brokerTransportTLS = "tls"
)

const workerRestartBackoffMax = 30 * time.Second
const reconcileRetryTimeout = 30 * time.Second

var errSubscriberNotStarted = errors.New("mqttingest: subscriber is not started")

// Config bundles the subscriber's runtime dependencies and tunables.
type Config struct {
	Store             Store
	NewClient         MQTTClientFactory
	Decryptor         PasswordDecryptor
	Logger            *slog.Logger
	Clock             func() time.Time
	WatchdogTickEvery time.Duration
	BrokerFreshness   time.Duration
	ShutdownDeadline  time.Duration
	ReconcileTimeout  time.Duration
	StatusReporter    RuntimeStatusReporter
	SignalExecutor    SignalExecutor
}

type sourceWorkerHandle struct {
	worker      *sourceWorker
	cancel      context.CancelFunc
	done        <-chan struct{}
	fingerprint string
	retryOnce   sync.Once
}

type brokerRuntimeStatus struct {
	connected  bool
	subscribed bool
	lastError  string
}

type subscriberActivation struct {
	runCanceled <-chan struct{}
	cancel      context.CancelFunc
	done        chan struct{}
	workerWG    sync.WaitGroup
	cleanupOnce sync.Once
}

// Subscriber owns per-source workers.
type Subscriber struct {
	cfg            Config
	workers        map[int64]*sourceWorkerHandle
	statuses       map[int64]RuntimeStatus
	brokerStatuses map[int64]map[string]brokerRuntimeStatus
	activation     *subscriberActivation
	lifecycleMu    sync.Mutex
	mu             sync.Mutex
	reconcileMu    sync.Mutex
}

var _ runtimejobs.Lifecycle = (*Subscriber)(nil)

// NewSubscriber validates dependencies and applies runtime defaults.
func NewSubscriber(cfg Config) (*Subscriber, error) {
	if cfg.Store == nil {
		return nil, errors.New("mqttingest: Store is required")
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
	if cfg.ReconcileTimeout <= 0 {
		cfg.ReconcileTimeout = reconcileRetryTimeout
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

// Start runs the subscriber for the lifetime of ctx and performs the initial
// source reconciliation.
func (s *Subscriber) Start(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.mu.Lock()
	if s.activation != nil {
		stopping := channelClosed(s.activation.runCanceled)
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		if stopping {
			return errors.New("mqttingest: previous subscriber activation is still stopping")
		}
		return errors.New("mqttingest: subscriber already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	activation := &subscriberActivation{
		runCanceled: runCtx.Done(),
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	s.activation = activation
	s.workers = make(map[int64]*sourceWorkerHandle)
	s.mu.Unlock()
	s.lifecycleMu.Unlock()
	go s.cleanupActivationWhenCanceled(activation)

	if _, _, err := s.reconcile(runCtx, true); err != nil {
		cancel()
		s.startActivationCleanup(activation)
		<-activation.done
		return err
	}
	if err := runCtx.Err(); err != nil {
		s.startActivationCleanup(activation)
		<-activation.done
		return fmt.Errorf("mqttingest: start subscriber: %w", err)
	}
	return nil
}

// Reconcile applies enabled-source settings to the running subscriber.
func (s *Subscriber) Reconcile(ctx context.Context) error {
	_, _, err := s.reconcile(ctx, false)
	return err
}

// Stop cancels all workers and waits for them within ctx. Cleanup continues
// after the caller's budget expires, and the activation remains installed
// until every worker has drained so a replacement activation cannot overlap it.
func (s *Subscriber) Stop(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.mu.Lock()
	activation := s.activation
	if activation == nil {
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		return nil
	}
	cancel := activation.cancel
	done := activation.done
	s.mu.Unlock()
	s.lifecycleMu.Unlock()

	cancel()
	s.startActivationCleanup(activation)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqttingest: stop subscriber: %w", ctx.Err())
	}
}

func (s *Subscriber) cleanupActivationWhenCanceled(activation *subscriberActivation) {
	<-activation.runCanceled
	s.startActivationCleanup(activation)
}

func (s *Subscriber) startActivationCleanup(activation *subscriberActivation) {
	activation.cleanupOnce.Do(func() {
		go s.cleanupActivation(activation)
	})
}

func (s *Subscriber) cleanupActivation(activation *subscriberActivation) {
	// Exclude Add calls before waiting on this activation's WaitGroup. Once the
	// activation context is canceled, later reconciles fail before adding work.
	s.reconcileMu.Lock()
	s.mu.Lock()
	if s.activation != activation {
		close(activation.done)
		s.mu.Unlock()
		s.reconcileMu.Unlock()
		return
	}
	handles := make([]*sourceWorkerHandle, 0, len(s.workers))
	for _, handle := range s.workers {
		handles = append(handles, handle)
	}
	s.mu.Unlock()
	for _, handle := range handles {
		handle.cancel()
	}
	s.reconcileMu.Unlock()

	s.cfg.Logger.Info("mqttingest subscriber draining workers")
	activation.workerWG.Wait()

	s.mu.Lock()
	if s.activation == activation {
		for sourceID := range s.workers {
			s.setSourceStatusLocked(sourceID, RuntimeStateStopped, "")
		}
		s.workers = make(map[int64]*sourceWorkerHandle)
		s.brokerStatuses = make(map[int64]map[string]brokerRuntimeStatus)
		s.activation = nil
	}
	close(activation.done)
	s.mu.Unlock()
	s.cfg.Logger.Info("mqttingest subscriber stopped cleanly")
}

func (s *Subscriber) SourceRuntimeStatus(sourceID int64) RuntimeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.statuses[sourceID]
	return status
}

func (s *Subscriber) QuiesceSource(ctx context.Context, sourceID int64) error {
	if err := s.lockReconcile(ctx); err != nil {
		return err
	}
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	activation := s.activation
	handle := s.workers[sourceID]
	s.mu.Unlock()
	if activation == nil || handle == nil {
		return nil
	}
	runCanceled := activation.runCanceled
	handle.cancel()
	if err := s.waitForHandleStopped(ctx, handle); err != nil {
		s.recordSourceError(handle.worker.source.ID, err.Error())
		s.reconcileWhenHandleStops(runCanceled, handle)
		return err
	}
	s.mu.Lock()
	if current, stillCurrent := s.workers[sourceID]; stillCurrent && current == handle {
		delete(s.workers, sourceID)
		delete(s.brokerStatuses, sourceID)
		s.setSourceStatusLocked(sourceID, RuntimeStateStopped, "")
	}
	s.mu.Unlock()
	return nil
}

func (s *Subscriber) reconcile(ctx context.Context, failIfNoneStarted bool) (int, int, error) {
	if err := s.lockReconcile(ctx); err != nil {
		return 0, 0, err
	}
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	activation := s.activation
	existing := make(map[int64]*sourceWorkerHandle, len(s.workers))
	for id, handle := range s.workers {
		existing[id] = handle
	}
	s.mu.Unlock()
	if activation == nil || channelClosed(activation.runCanceled) {
		return 0, 0, errSubscriberNotStarted
	}
	runCanceled := activation.runCanceled

	sources, err := s.cfg.Store.ListEnabledSources(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("mqttingest: list enabled sources: %w", err)
	}
	if channelClosed(runCanceled) {
		return 0, len(sources), errSubscriberNotStarted
	}
	s.cfg.Logger.Info("mqttingest subscriber reconciling", slog.Int("source_count", len(sources)))

	desired := make(map[int64]SourceConfig, len(sources))
	for _, src := range sources {
		desired[src.ID] = src
	}
	stopping := make([]*sourceWorkerHandle, 0)
	for sourceID, handle := range existing {
		if handleStopped(handle) {
			s.mu.Lock()
			if current, stillCurrent := s.workers[sourceID]; stillCurrent && current == handle {
				delete(s.workers, sourceID)
				delete(s.brokerStatuses, sourceID)
				s.setSourceStatusLocked(sourceID, RuntimeStateStopped, "")
			}
			s.mu.Unlock()
			continue
		}
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
			s.reconcileWhenHandleStops(runCanceled, handle)
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
		if channelClosed(runCanceled) {
			return started, len(sources), errSubscriberNotStarted
		}
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

		workerCtx, workerCancel := contextWithDone(runCanceled)
		if channelClosed(runCanceled) {
			workerCancel()
			return started, len(sources), errSubscriberNotStarted
		}
		w, done, err := s.startWorker(ctx, workerCtx, src, &activation.workerWG)
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
		if s.activation != activation || channelClosed(runCanceled) {
			s.mu.Unlock()
			workerCancel()
			return started, len(sources), errSubscriberNotStarted
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

func (s *Subscriber) lockReconcile(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if s.reconcileMu.TryLock() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("mqttingest: reconcile lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func handleStopped(handle *sourceWorkerHandle) bool {
	if handle == nil || handle.done == nil {
		return true
	}
	select {
	case <-handle.done:
		return true
	default:
		return false
	}
}

func (s *Subscriber) reconcileWhenHandleStops(runCanceled <-chan struct{}, handle *sourceWorkerHandle) {
	if handle == nil || handle.done == nil {
		return
	}
	handle.retryOnce.Do(func() {
		go s.reconcileAfterHandleStops(runCanceled, handle)
	})
}

func (s *Subscriber) reconcileAfterHandleStops(runCanceled <-chan struct{}, handle *sourceWorkerHandle) {
	if runCanceled == nil {
		return
	}
	select {
	case <-runCanceled:
		return
	case <-handle.done:
	}
	select {
	case <-runCanceled:
		return
	default:
	}
	runCtx, runCancel := contextWithDone(runCanceled)
	defer runCancel()
	reconcileCtx, timeoutCancel := context.WithTimeout(runCtx, s.cfg.ReconcileTimeout)
	defer timeoutCancel()
	if err := s.Reconcile(reconcileCtx); err != nil {
		s.cfg.Logger.Warn("mqttingest: retry reconcile after worker stop failed",
			slog.String("source", handle.worker.source.SourceName),
			slog.Any("error", err))
	}
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

func channelClosed(done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
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

// startWorker validates one source with setupCtx, then boots its long-lived
// worker on workerCtx.
func (s *Subscriber) startWorker(setupCtx, workerCtx context.Context, src SourceConfig, wg *sync.WaitGroup) (*sourceWorker, <-chan struct{}, error) {
	primary, secondary, ok := ResolveBrokerRoles(src.BrokerPrimaryHost, src.BrokerSecondaryHost)
	if !ok {
		return nil, nil, fmt.Errorf("mqttingest: source %s has identical broker hosts", src.SourceName)
	}
	if err := validateBrokerTransport(src, primary, secondary); err != nil {
		return nil, nil, err
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
		s.superviseWorker(workerCtx, src, decoder, primary, secondary, workerPassword, wg)
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
	if subscribed == 0 {
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
				return fmt.Errorf("MaestroOS source %q uses TCP transport with non-local broker host %q", src.SourceName, host)
			}
		}
		return nil
	case brokerTransportTLS:
		return nil
	default:
		return fmt.Errorf("MaestroOS source %q has unsupported broker_transport %q", src.SourceName, src.BrokerTransport)
	}
}

func sourceConfigFingerprint(src SourceConfig) string {
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
		src.PayloadFormat,
		fmt.Sprintf("%d", int64(src.StalenessThreshold/time.Second)),
	}, "\x1e")
}
