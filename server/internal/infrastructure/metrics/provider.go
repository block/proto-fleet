package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const ServiceName = "proto-fleet-api"

type Config struct {
	Enabled       bool          `help:"Enable notifications metrics emission to TimescaleDB" default:"false" env:"ENABLED"`
	FlushInterval time.Duration `help:"Maximum age of a buffered metric sample before it is flushed to TimescaleDB" default:"5s" env:"FLUSH_INTERVAL"`
	BufferSize    int           `help:"In-memory ring buffer size before back-pressure kicks in" default:"4096" env:"BUFFER_SIZE"`
	BatchSize     int           `help:"Maximum number of samples per INSERT batch" default:"500" env:"BATCH_SIZE"`
	InstanceID    string        `help:"Override for the service.instance.id resource attribute" default:"" env:"INSTANCE_ID"`
}

// DB is the small subset of *sql.DB the writer depends on. Exposed as an
// interface so tests can substitute a fake without standing up a real
// TimescaleDB.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Provider buffers metric samples and writes them to TimescaleDB in batches.
// A disabled provider drops everything silently.
type Provider struct {
	cfg     Config
	db      DB
	enabled bool

	queue  chan sample
	cancel context.CancelFunc
	wg     sync.WaitGroup

	dropsMu sync.Mutex
	drops   uint64
}

// Setup returns a Provider that writes samples to db. If cfg.Enabled is false
// the provider is a no-op (db may be nil).
func Setup(ctx context.Context, _ string, cfg Config, db DB) (*Provider, error) {
	if !cfg.Enabled {
		return &Provider{cfg: cfg, enabled: false}, nil
	}
	if db == nil {
		return nil, errors.New("metrics: TimescaleDB connection is required when metrics are enabled")
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 4096
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}

	runCtx, cancel := context.WithCancel(context.Background())
	p := &Provider{
		cfg:     cfg,
		db:      db,
		enabled: true,
		queue:   make(chan sample, cfg.BufferSize),
		cancel:  cancel,
	}

	p.wg.Add(1)
	go p.run(runCtx)

	return p, nil
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || !p.enabled {
		return nil
	}
	p.cancel()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("metrics: shutdown timed out: %w", ctx.Err())
	}
	return nil
}

func (p *Provider) Enabled() bool { return p != nil && p.enabled }

// enqueue pushes a sample onto the writer queue without blocking. Overflow is
// counted, not retried — back-pressure is the writer's job.
func (p *Provider) enqueue(s sample) {
	if p == nil || !p.enabled {
		return
	}
	select {
	case p.queue <- s:
	default:
		p.dropsMu.Lock()
		p.drops++
		d := p.drops
		p.dropsMu.Unlock()
		// Log once per power-of-two threshold so a sustained overflow doesn't
		// drown the logger.
		if d&(d-1) == 0 {
			slog.Warn("metrics: queue full, dropping sample", "drops", d)
		}
	}
}

func (p *Provider) run(ctx context.Context) {
	defer p.wg.Done()

	batch := make([]sample, 0, p.cfg.BatchSize)
	ticker := time.NewTicker(p.cfg.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := p.writeBatch(ctx, batch); err != nil {
			slog.Error("metrics: batch write failed", "error", err, "samples", len(batch))
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain whatever is left in the queue before exiting.
			for {
				select {
				case s := <-p.queue:
					batch = append(batch, s)
					if len(batch) >= p.cfg.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}

		case s := <-p.queue:
			batch = append(batch, s)
			if len(batch) >= p.cfg.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}
