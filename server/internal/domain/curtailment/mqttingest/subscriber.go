package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
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
}

// Subscriber owns per-source workers.
type Subscriber struct {
	cfg     Config
	workers map[int64]*sourceWorker
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
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
	return &Subscriber{
		cfg:     cfg,
		workers: make(map[int64]*sourceWorker),
	}, nil
}

// Start starts enabled sources once. Enable/disable changes take effect after
// restart; per-source startup errors are logged so other sources can still run.
func (s *Subscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return errors.New("mqttingest: subscriber already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.workers = make(map[int64]*sourceWorker)
	s.mu.Unlock()

	sources, err := s.cfg.Store.ListEnabledSources(runCtx)
	if err != nil {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
		return fmt.Errorf("mqttingest: list enabled sources: %w", err)
	}

	s.cfg.Logger.Info("mqttingest subscriber starting", slog.Int("source_count", len(sources)))

	started := 0
	var firstStartErr error
	for _, src := range sources {
		w, err := s.startWorker(runCtx, src, &s.wg)
		if err != nil {
			if firstStartErr == nil {
				firstStartErr = err
			}
			s.cfg.Logger.Error("mqttingest: start worker failed",
				slog.String("source", src.SourceName),
				slog.Any("error", err))
			continue
		}
		started++
		s.mu.Lock()
		s.workers[src.ID] = w
		s.mu.Unlock()
	}

	if len(sources) > 0 && started == 0 {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.workers = make(map[int64]*sourceWorker)
		s.mu.Unlock()
		return fmt.Errorf("mqttingest: no enabled sources started (source_count=%d): %w", len(sources), firstStartErr)
	}

	return nil
}

// Stop cancels all workers and waits up to ShutdownDeadline for them to drain.
func (s *Subscriber) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	if cancel == nil {
		s.mu.Unlock()
		return
	}
	s.cancel = nil
	s.mu.Unlock()

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
	s.workers = make(map[int64]*sourceWorker)
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

// startWorker boots one source's worker goroutine.
func (s *Subscriber) startWorker(ctx context.Context, src SourceConfig, wg *sync.WaitGroup) (*sourceWorker, error) {
	primary, secondary, ok := ResolveBrokerRoles(src.BrokerPrimaryHost, src.BrokerSecondaryHost)
	if !ok {
		return nil, fmt.Errorf("mqttingest: source %s has identical broker hosts", src.SourceName)
	}
	if err := validateBrokerTransport(src, primary, secondary); err != nil {
		return nil, err
	}
	if _, err := scopeForSource(src); err != nil {
		return nil, fmt.Errorf("mqttingest: source %s invalid scope: %w", src.SourceName, err)
	}

	// The service user must hold the machine-ingest permission for the org it can curtail.
	canIngest, err := s.cfg.Store.UserCanIngestCurtailment(ctx, src.ServiceUserID, src.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: verify service user for %s: %w", src.SourceName, err)
	}
	if !canIngest {
		return nil, fmt.Errorf("mqttingest: source %s service user %d lacks curtailment:ingest in org %d",
			src.SourceName, src.ServiceUserID, src.OrganizationID)
	}

	decoder, err := decoderForFormat(src.PayloadFormat)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: source %s: %w", src.SourceName, err)
	}

	password, err := s.cfg.Decryptor.Decrypt(src.MQTTPasswordEncrypted)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: decrypt password for %s: %w", src.SourceName, err)
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

	wg.Add(1)
	go s.superviseWorker(ctx, src, decoder, primary, secondary, workerPassword, wg)
	return w, nil
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
			s.cfg.Logger.Error("mqttingest: source worker panic",
				slog.String("source", w.source.SourceName),
				slog.Any("panic", r))
		}
	}()
	w.run(ctx)
	return false
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
