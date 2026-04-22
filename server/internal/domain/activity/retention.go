package activity

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// retentionStepTimeout bounds a single paginated delete statement.
const retentionStepTimeout = 30 * time.Second

// Bounds keep a misconfiguration from pinning the DB.
const (
	minCleanupInterval = time.Minute
	maxDeleteBatchSize = 50000
)

// defaultRetentionConfig fills in sensible values for any RetentionConfig
// field left at its zero value, then clamps pathological ones so the cleaner
// cannot run away with a 1ms interval or a million-row delete per tick.
func defaultRetentionConfig(rc *RetentionConfig) {
	if rc.ActivityLogRetention <= 0 {
		rc.ActivityLogRetention = 8760 * time.Hour // 1 year
	}
	if rc.CleanupInterval <= 0 {
		rc.CleanupInterval = 6 * time.Hour
	}
	if rc.DeleteBatchLimit <= 0 {
		rc.DeleteBatchLimit = 1000
	}

	if rc.CleanupInterval < minCleanupInterval {
		slog.Warn("activity retention: CleanupInterval below minimum, clamping",
			"before", rc.CleanupInterval, "after", minCleanupInterval)
		rc.CleanupInterval = minCleanupInterval
	}
	if rc.DeleteBatchLimit > maxDeleteBatchSize {
		slog.Warn("activity retention: DeleteBatchLimit above maximum, clamping",
			"before", rc.DeleteBatchLimit, "after", maxDeleteBatchSize)
		rc.DeleteBatchLimit = maxDeleteBatchSize
	}
}

// RetentionCleaner ages out activity_log rows per RetentionConfig. The
// activity log stays append-only; retention deletes apply only once a row is
// older than ActivityLogRetention.
//
// Kept separate from the command retention cleaner because activity has
// longer retention (1 year default vs 180 days for batch headers): the two
// tables serve different audiences (operators/compliance vs. per-miner
// debugging) and should be tuned independently.
type RetentionCleaner struct {
	store  interfaces.ActivityStore
	config *RetentionConfig
	now    func() time.Time

	// lifecycleMu serializes entire Start/Stop bodies so concurrent calls
	// install and drain generations in turn; mu guards the short cancel/done
	// field reads/writes. See command.RetentionCleaner for the full rationale.
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewRetentionCleaner wires the cleaner to the store. It mutates cfg to apply
// defaults for zero-valued fields.
func NewRetentionCleaner(store interfaces.ActivityStore, cfg *RetentionConfig) *RetentionCleaner {
	defaultRetentionConfig(cfg)
	return &RetentionCleaner{
		store:  store,
		config: cfg,
		now:    time.Now,
	}
}

// Start launches the cleaner goroutine. Safe to call with a nil receiver
// and safe to call multiple times -- lifecycleMu serializes overlapping
// callers so the previous generation is always drained before a new one is
// installed.
//
// Locking order: Start/Stop run under lifecycleMu; cancel/done are
// read/written under c.mu but the drain happens outside c.mu so a worker
// that ever needs c.mu cannot deadlock against Start/Stop on <-done.
func (c *RetentionCleaner) Start(ctx context.Context) {
	if c == nil {
		return
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.Lock()
	prevCancel, prevDone := c.cancel, c.done
	c.cancel, c.done = nil, nil
	c.mu.Unlock()
	if prevCancel != nil {
		prevCancel()
		<-prevDone
	}

	cleanCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	c.mu.Lock()
	c.cancel = cancel
	c.done = done
	c.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(c.config.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-cleanCtx.Done():
				return
			case <-ticker.C:
				if err := c.runOnce(cleanCtx); err != nil {
					slog.Error("activity retention cleaner run failed", "error", err)
				}
			}
		}
	}()
}

// Stop signals the cleaner goroutine to exit and waits for it to drain.
// See Start for the locking-order rationale.
func (c *RetentionCleaner) Stop() {
	if c == nil {
		return
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done = nil, nil
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// runOnce performs a single retention pass. Exposed for tests.
func (c *RetentionCleaner) runOnce(ctx context.Context) error {
	cutoff := c.now().Add(-c.config.ActivityLogRetention)
	// #nosec G115 -- DeleteBatchLimit is bounded by the config's int range.
	limit := int32(c.config.DeleteBatchLimit)

	total := int64(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		stepCtx, cancel := context.WithTimeout(ctx, retentionStepTimeout)
		deleted, err := c.store.DeleteOlderThan(stepCtx, cutoff, limit)
		cancel()
		if err != nil {
			return fmt.Errorf("activity retention delete: %w", err)
		}
		total += deleted
		if deleted < int64(limit) {
			break
		}
	}

	if total > 0 {
		slog.Info("activity retention cleaner deleted rows", "count", total)
	}
	return nil
}
