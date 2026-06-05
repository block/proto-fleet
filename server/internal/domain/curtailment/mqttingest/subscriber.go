package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// MQTTClient is one broker connection for one source.
type MQTTClient interface {
	Connect(ctx context.Context, host string, port int32, username, password string) error
	Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error
	Disconnect(shutdownDeadline time.Duration)
}

// MQTTClientFactory builds a fresh client per source/broker.
type MQTTClientFactory func() MQTTClient

// PasswordDecryptor unwraps encrypted source credentials.
type PasswordDecryptor interface {
	Decrypt(encrypted string) ([]byte, error)
}

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

	for _, src := range sources {
		w, err := s.startWorker(runCtx, src, &s.wg)
		if err != nil {
			s.cfg.Logger.Error("mqttingest: start worker failed",
				slog.String("source", src.SourceName),
				slog.Any("error", err))
			continue
		}
		s.mu.Lock()
		s.workers[src.ID] = w
		s.mu.Unlock()
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

	// The service user must belong to the org it can curtail.
	member, err := s.cfg.Store.UserBelongsToOrg(ctx, src.ServiceUserID, src.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: verify service user for %s: %w", src.SourceName, err)
	}
	if !member {
		return nil, fmt.Errorf("mqttingest: source %s service user %d is not a member of org %d",
			src.SourceName, src.ServiceUserID, src.OrganizationID)
	}

	password, err := s.cfg.Decryptor.Decrypt(src.MQTTPasswordEncrypted)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: decrypt password for %s: %w", src.SourceName, err)
	}

	decoder, err := decoderForFormat(src.PayloadFormat)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: source %s: %w", src.SourceName, err)
	}

	w := &sourceWorker{
		cfg:           s.cfg,
		source:        src,
		decoder:       decoder,
		primaryHost:   primary,
		secondaryHost: secondary,
		password:      string(password),
	}
	// Bound plaintext credentials to the worker lifetime.
	clear(password)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.cfg.Logger.Error("mqttingest: source worker panic",
					slog.String("source", src.SourceName),
					slog.Any("panic", r))
			}
			w.password = ""
		}()
		w.run(ctx)
	}()
	return w, nil
}
