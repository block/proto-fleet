package activity

import (
	"context"
	"fmt"
	"log/slog"
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

	cancel context.CancelFunc
	done   chan struct{}
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

// Start launches the cleaner goroutine. Safe with a nil receiver.
func (c *RetentionCleaner) Start(ctx context.Context) {
	if c == nil {
		return
	}
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
	cleanCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})

	go func() {
		defer close(c.done)
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
func (c *RetentionCleaner) Stop() {
	if c == nil || c.cancel == nil {
		return
	}
	c.cancel()
	<-c.done
	c.cancel = nil
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
