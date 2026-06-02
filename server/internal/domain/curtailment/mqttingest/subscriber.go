package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// MQTTClient is the interface the subscriber depends on; the production
// (paho) binding is a separate adapter so tests can use a fake and the
// package compiles without an MQTT library. One client per (source,
// broker), never shared across goroutines.
type MQTTClient interface {
	// Connect establishes a session, retrying transient failures with
	// backoff; returns when connected or ctx is canceled.
	Connect(ctx context.Context, host string, port int32, username, password string) error
	// Subscribe registers a handler for the topic at QoS 1. The handler
	// runs on the client's read goroutine and must not block.
	Subscribe(ctx context.Context, topic string, handler func(payload []byte, receivedAt time.Time)) error
	// Disconnect tears down the session within shutdownDeadline.
	Disconnect(shutdownDeadline time.Duration)
}

// MQTTClientFactory builds a fresh MQTTClient per (source, broker).
// Tests inject a factory that returns a fake; production uses paho.
type MQTTClientFactory func() MQTTClient

// PasswordDecryptor unwraps the encrypted credential stored on the
// source-config row. The production adapter wraps
// infrastructure/encrypt.Service; tests inject a pass-through.
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

// Subscriber owns per-source workers. Construct with NewSubscriber,
// call Run from the fleetd boot wiring, and Stop on shutdown.
type Subscriber struct {
	cfg     Config
	workers map[int64]*sourceWorker
	mu      sync.Mutex
}

// NewSubscriber validates the supplied config and returns a ready
// subscriber. Returns an error when required deps are nil.
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

// Run reads the enabled-source list once, starts a worker per source,
// and blocks until ctx is canceled. Sources added or disabled while
// Run is in flight do not take effect until the next Run invocation.
// Hot reconfiguration is not yet supported.
func (s *Subscriber) Run(ctx context.Context) error {
	sources, err := s.cfg.Store.ListEnabledSources(ctx)
	if err != nil {
		return fmt.Errorf("mqttingest: list enabled sources: %w", err)
	}

	s.cfg.Logger.Info("mqttingest subscriber starting", slog.Int("source_count", len(sources)))

	var wg sync.WaitGroup
	for _, src := range sources {
		w, err := s.startWorker(ctx, src, &wg)
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

	<-ctx.Done()
	s.cfg.Logger.Info("mqttingest subscriber draining workers",
		slog.Duration("deadline", s.cfg.ShutdownDeadline))
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.cfg.Logger.Info("mqttingest subscriber stopped cleanly")
	case <-time.After(s.cfg.ShutdownDeadline):
		s.cfg.Logger.Warn("mqttingest subscriber shutdown deadline exceeded")
	}
	return nil
}

// startWorker boots one source's worker goroutine. The worker runs
// until ctx is canceled or its panic boundary trips.
func (s *Subscriber) startWorker(ctx context.Context, src SourceConfig, wg *sync.WaitGroup) (*sourceWorker, error) {
	primary, secondary, ok := ResolveBrokerRoles(src.BrokerPrimaryHost, src.BrokerSecondaryHost)
	if !ok {
		return nil, fmt.Errorf("mqttingest: source %s has identical broker hosts", src.SourceName)
	}

	password, err := s.cfg.Decryptor.Decrypt(src.MQTTPasswordEncrypted)
	if err != nil {
		return nil, fmt.Errorf("mqttingest: decrypt password for %s: %w", src.SourceName, err)
	}

	w := &sourceWorker{
		cfg:           s.cfg,
		source:        src,
		primaryHost:   primary,
		secondaryHost: secondary,
		password:      string(password),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.cfg.Logger.Error("mqttingest: source worker panic",
					slog.String("source", src.SourceName),
					slog.Any("panic", r))
			}
			// Bound plaintext credentials to the worker lifetime.
			w.password = ""
		}()
		w.run(ctx)
	}()
	return w, nil
}
