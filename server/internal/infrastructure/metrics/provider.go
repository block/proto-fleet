package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const ServiceName = "proto-fleet-api"

type Config struct {
	Enabled       bool          `help:"Persist Proto Fleet metrics into TimescaleDB for Grafana alerting" default:"false" env:"ENABLED"`
	FlushInterval time.Duration `help:"How often the in-process buffer is flushed to TimescaleDB" default:"5s" env:"FLUSH_INTERVAL"`
	BufferSize    int           `help:"Bounded channel size between emit and flush; oldest samples are dropped when full" default:"4096" env:"BUFFER_SIZE"`
	BatchSize     int           `help:"Maximum number of samples written per INSERT statement" default:"512" env:"BATCH_SIZE"`
}

type Provider struct {
	cfg     Config
	enabled bool

	samples chan Sample
	store   Store

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}

	// dropped counts samples we threw away because the buffer was
	// full. Exposed for tests and the log line on shutdown.
	dropped atomic.Uint64
}

func Setup(ctx context.Context, version string, cfg Config, db *sql.DB) (*Provider, error) {
	if !cfg.Enabled {
		return newDisabledProvider(cfg), nil
	}
	if db == nil {
		return nil, errors.New("metrics: Setup called with nil *sql.DB; pass the fleet-api connection or disable the provider")
	}

	store := NewSQLStore(db)
	return startProvider(ctx, version, cfg, store), nil
}

// SetupWithStore is the test-facing constructor.
func SetupWithStore(ctx context.Context, version string, cfg Config, store Store) *Provider {
	if !cfg.Enabled {
		return newDisabledProvider(cfg)
	}
	if store == nil {
		store = NewInMemoryStore()
	}
	return startProvider(ctx, version, cfg, store)
}

func startProvider(ctx context.Context, version string, cfg Config, store Store) *Provider {
	cfg = applyDefaults(cfg)

	p := &Provider{
		cfg:     cfg,
		enabled: true,
		samples: make(chan Sample, cfg.BufferSize),
		store:   store,
		stopCh:  make(chan struct{}),
	}

	p.wg.Add(1)
	go p.flushLoop(ctx)

	slog.Info("metrics provider started",
		"service", ServiceName,
		"version", version,
		"buffer_size", cfg.BufferSize,
		"flush_interval", cfg.FlushInterval,
		"batch_size", cfg.BatchSize,
	)
	return p
}

func applyDefaults(cfg Config) Config {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 4096
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > cfg.BufferSize {
		cfg.BatchSize = min(512, cfg.BufferSize)
	}
	return cfg
}

func newDisabledProvider(cfg Config) *Provider {
	return &Provider{cfg: cfg, enabled: false}
}

// Enabled reports whether the provider will actually persist samples.
func (p *Provider) Enabled() bool { return p != nil && p.enabled }

// Shutdown stops the flusher and drains any buffered samples through
// one final InsertSamples call. Safe to call multiple times.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || !p.enabled {
		return nil
	}

	p.stopOnce.Do(func() {
		close(p.stopCh)
	})

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("provider final error: %w", ctx.Err())
	}

	if dropped := p.dropped.Load(); dropped > 0 {
		slog.Warn("metrics buffer dropped samples under pressure",
			"dropped_total", dropped,
		)
	}
	return p.store.Close()
}

// record is the single funnel for all Emit* methods. Returning quickly
// when the channel is full keeps the hot path bounded: under steady
// overload we drop a sample rather than block the caller.
func (p *Provider) record(sample Sample) {
	if p == nil || !p.enabled {
		return
	}
	if sample.Time.IsZero() {
		sample.Time = time.Now().UTC()
	}
	select {
	case p.samples <- sample:
	default:
		p.dropped.Add(1)
	}
}

func (p *Provider) flushLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]Sample, 0, p.cfg.BatchSize)

	flush := func(parent context.Context) {
		if len(batch) == 0 {
			return
		}
		flushCtx, cancel := context.WithTimeout(parent, 10*time.Second)
		defer cancel()
		if err := p.store.InsertSamples(flushCtx, batch); err != nil {
			// Don't log per sample — that would flood the logger if
			// TimescaleDB is unreachable. One line per failed flush.
			slog.Error("metrics: flush to TimescaleDB failed",
				"error", err,
				"samples", len(batch),
			)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-p.stopCh:
			// Drain anything queued before exiting. The select below
			// is non-blocking so we don't wedge on a slow producer.
		drain:
			for {
				select {
				case sample := <-p.samples:
					batch = append(batch, sample)
					if len(batch) >= p.cfg.BatchSize {
						flush(context.Background())
					}
				default:
					break drain
				}
			}
			flush(context.Background())
			return

		case <-ticker.C:
			flush(ctx)

		case sample := <-p.samples:
			batch = append(batch, sample)
			if len(batch) >= p.cfg.BatchSize {
				flush(ctx)
			}
		}
	}
}

// DroppedSamples is exposed for tests that want to verify the
// backpressure path.
func (p *Provider) DroppedSamples() uint64 {
	if p == nil {
		return 0
	}
	return p.dropped.Load()
}
