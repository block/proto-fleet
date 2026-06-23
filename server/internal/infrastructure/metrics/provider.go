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

	"github.com/block/proto-fleet/server/internal/domain/alerts"
)

const ServiceName = "proto-fleet-api"

const maxPostgresBindParameters = 65535

var maxSamplesPerInsert = maxPostgresBindParameters / columnsPerSample

type Config struct {
	Enabled         bool                 `help:"Persist Proto Fleet metrics into TimescaleDB for Grafana alerting" default:"false" env:"ENABLED"`
	FlushInterval   time.Duration        `help:"How often the in-process buffer is flushed to TimescaleDB" default:"5s" env:"FLUSH_INTERVAL"`
	BufferSize      int                  `help:"Bounded channel size between emit and flush; oldest samples are dropped when full" default:"4096" env:"BUFFER_SIZE"`
	BatchSize       int                  `help:"Maximum number of samples written per INSERT statement" default:"512" env:"BATCH_SIZE"`
	RetryBufferSize int                  `help:"Maximum number of samples queued for retry after a failed flush; oldest are dropped when full" default:"8192" env:"RETRY_BUFFER_SIZE"`
	MaxRetryBackoff time.Duration        `help:"Upper bound on the exponential backoff between retries after a failed flush" default:"1m" env:"MAX_RETRY_BACKOFF"`
	WebhookToken    string               `help:"Shared secret required on incoming Alertmanager webhook deliveries as 'Authorization: Bearer <token>'. Configure the same value into Grafana's webhook contact point (authorization_scheme: Bearer, authorization_credentials: <token>). When empty the receiver refuses every request." env:"WEBHOOK_TOKEN"`
	Grafana         alerts.GrafanaConfig `embed:"" prefix:"grafana-" envprefix:"GRAFANA_"`

	AlertDestinations alerts.DestinationPolicy `embed:""`
}

type Provider struct {
	cfg     Config
	enabled bool

	samples chan Sample
	store   Store

	dropMu sync.Mutex

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}

	dropped atomic.Uint64

	failedFlushes atomic.Uint64

	retryDropped atomic.Uint64
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
	if cfg.RetryBufferSize <= 0 {
		// A few full batches' worth of retry headroom.
		cfg.RetryBufferSize = max(cfg.BatchSize*16, 4*cfg.BufferSize)
	}
	if cfg.MaxRetryBackoff <= 0 {
		cfg.MaxRetryBackoff = time.Minute
	}
	if cfg.MaxRetryBackoff < cfg.FlushInterval {
		cfg.MaxRetryBackoff = cfg.FlushInterval
	}
	return cfg
}

func newDisabledProvider(cfg Config) *Provider {
	return &Provider{cfg: cfg, enabled: false}
}

func (p *Provider) Enabled() bool { return p != nil && p.enabled }

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
	if failed := p.failedFlushes.Load(); failed > 0 {
		slog.Warn("metrics flushes failed",
			"failed_flushes_total", failed,
			"retry_dropped_total", p.retryDropped.Load(),
		)
	}
	return p.store.Close()
}

func (p *Provider) record(sample Sample) {
	if p == nil || !p.enabled {
		return
	}
	if sample.Time.IsZero() {
		sample.Time = time.Now().UTC()
	}
	select {
	case p.samples <- sample:
		return
	default:
	}

	// Buffer is full.
	p.dropMu.Lock()
	defer p.dropMu.Unlock()
	for {
		select {
		case p.samples <- sample:
			return
		default:
		}
		// Discard one oldest sample and retry.
		select {
		case <-p.samples:
			p.dropped.Add(1)
		default:
			// Reader drained the channel out from under us
		}
	}
}

func (p *Provider) flushLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]Sample, 0, p.cfg.BatchSize)
	var pendingRetry []Sample
	var backoff time.Duration
	var nextRetry time.Time

	// flush attempts to persist pendingRetry + batch.
	flush := func(parent context.Context, force bool) {
		if len(batch) == 0 && len(pendingRetry) == 0 {
			return
		}
		if !force && !nextRetry.IsZero() && time.Now().Before(nextRetry) {
			// Still in the backoff window after the previous failure.
			if len(batch) > 0 {
				pendingRetry = appendBoundedRetry(pendingRetry, batch, p.cfg.RetryBufferSize, &p.retryDropped)
				batch = batch[:0]
			}
			return
		}

		samples := pendingRetry
		if len(batch) > 0 {
			// Allocate a fresh slice so pendingRetry's backing array
			// isn't aliased with the InsertSamples argument.
			samples = make([]Sample, 0, len(pendingRetry)+len(batch))
			samples = append(samples, pendingRetry...)
			samples = append(samples, batch...)
		}
		batch = batch[:0]

		chunkSize := p.cfg.BatchSize
		if chunkSize <= 0 || chunkSize > maxSamplesPerInsert {
			chunkSize = maxSamplesPerInsert
		}

		for offset := 0; offset < len(samples); {
			end := offset + chunkSize
			if end > len(samples) {
				end = len(samples)
			}
			chunk := samples[offset:end]

			flushCtx, cancel := context.WithTimeout(parent, 10*time.Second)
			err := p.store.InsertSamples(flushCtx, chunk)
			cancel()

			if err != nil {
				p.failedFlushes.Add(1)
				backoff = nextBackoff(backoff, p.cfg.FlushInterval, p.cfg.MaxRetryBackoff)
				nextRetry = time.Now().Add(backoff)
				pendingRetry = appendBoundedRetry(nil, samples[offset:], p.cfg.RetryBufferSize, &p.retryDropped)
				slog.Error("metrics: flush to TimescaleDB failed",
					"error", err,
					"samples_pending", len(samples)-offset,
					"chunk_size", end-offset,
					"chunks_succeeded", offset/chunkSize,
					"next_retry_in", backoff,
					"failed_flushes_total", p.failedFlushes.Load(),
					"retry_dropped_total", p.retryDropped.Load(),
				)
				return
			}
			offset = end
		}

		pendingRetry = pendingRetry[:0]
		backoff = 0
		nextRetry = time.Time{}
	}

	for {
		select {
		case <-p.stopCh:
		drain:
			for {
				select {
				case sample := <-p.samples:
					batch = append(batch, sample)
					if len(batch) >= p.cfg.BatchSize {
						flush(ctx, true)
					}
				default:
					break drain
				}
			}
			flush(ctx, true)
			return

		case <-ticker.C:
			flush(ctx, false)

		case sample := <-p.samples:
			batch = append(batch, sample)
			if len(batch) >= p.cfg.BatchSize {
				flush(ctx, false)
			}
		}
	}
}

// appendBoundedRetry returns retry ++ extra, capped at maxLen.
func appendBoundedRetry(retry, extra []Sample, maxLen int, dropped *atomic.Uint64) []Sample {
	if len(extra) == 0 {
		return retry
	}
	if maxLen <= 0 {
		return append(retry, extra...)
	}

	combined := len(retry) + len(extra)
	if combined <= maxLen {
		return append(retry, extra...)
	}

	overflow := combined - maxLen
	if overflow >= len(retry) {
		// Even after dropping every existing retry entry we still exceed the cap
		extraDrop := overflow - len(retry)
		dropped.Add(uint64(len(retry) + extraDrop)) // #nosec G115 -- both appends are slice lengths, non-negative
		out := make([]Sample, 0, maxLen)
		return append(out, extra[extraDrop:]...)
	}

	// Drop the oldest entries off the front of the retry queue, then concatenate.
	dropped.Add(uint64(overflow)) // #nosec G115 -- overflow = combined-maxLen and combined > maxLen, so > 0
	out := make([]Sample, 0, maxLen)
	out = append(out, retry[overflow:]...)
	return append(out, extra...)
}

// nextBackoff returns the next exponential backoff value, doubling from base up to ceiling.
func nextBackoff(current, base, ceiling time.Duration) time.Duration {
	if current <= 0 {
		return base
	}
	next := current * 2
	if next > ceiling {
		next = ceiling
	}
	return next
}

// DroppedSamples is exposed for tests that want to verify the backpressure path.
func (p *Provider) DroppedSamples() uint64 {
	if p == nil {
		return 0
	}
	return p.dropped.Load()
}

// cumulative number of InsertSamples calls that returned an error.
func (p *Provider) FailedFlushes() uint64 {
	if p == nil {
		return 0
	}
	return p.failedFlushes.Load()
}

// cumulative number of samples that were ultimately discarded because of the bounded retry.
func (p *Provider) RetryDroppedSamples() uint64 {
	if p == nil {
		return 0
	}
	return p.retryDropped.Load()
}
