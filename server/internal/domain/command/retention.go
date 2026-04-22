package command

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// retentionStepTimeout bounds a single paginated delete statement. Each
// retention pass may issue many of these in a loop.
const retentionStepTimeout = 30 * time.Second

// Bounds applied to retention configs to keep misconfigurations from
// hammering the DB or leaving orphan rows behind. Chosen to be permissive
// enough that realistic operator tuning isn't clamped, while preventing
// obvious foot-guns (e.g. a 1ms cleanup interval, a million-row delete
// limit, or retention ordering that drops batch headers before their
// queue-message children have aged out).
const (
	minCleanupInterval = time.Minute
	maxDeleteBatchSize = 50000
)

// defaultRetentionConfig fills in sensible values for any RetentionConfig
// field left at its zero value, then clamps pathological combinations so the
// cleaner never runs with FK-breaking ordering or with values that would
// overwhelm the database on a single tick. Applied when NewRetentionCleaner
// is called so tests and call-sites that build a Config by hand get a
// working cleaner.
func defaultRetentionConfig(rc *RetentionConfig) {
	if rc.QueueMessageRetention <= 0 {
		rc.QueueMessageRetention = 720 * time.Hour
	}
	if rc.DeviceLogRetention <= 0 {
		rc.DeviceLogRetention = 2160 * time.Hour
	}
	if rc.BatchLogRetention <= 0 {
		rc.BatchLogRetention = 4320 * time.Hour
	}
	if rc.CleanupInterval <= 0 {
		rc.CleanupInterval = time.Hour
	}
	if rc.DeleteBatchLimit <= 0 {
		rc.DeleteBatchLimit = 1000
	}

	// Enforce FK-safe ordering: queue_message rows reference the batch header
	// by uuid (no FK), and command_on_device_log rows reference the batch id
	// directly, so any batch-header row that is still referenced by a
	// per-device child will be skipped. But if queue-message retention were
	// longer than device-log retention, we'd have queue rows whose batch has
	// been deleted out from under them. Similarly for batch-log vs device-log.
	//
	// Apply the outermost invariant first (batch -> device, then device ->
	// queue) so a three-way violation cascades correctly. If we clamped queue
	// first we could leave queue > device after a later device clamp; doing
	// batch first ensures each subsequent clamp sees the already-reduced
	// upstream value.
	if rc.BatchLogRetention < rc.DeviceLogRetention {
		slog.Warn("command retention: DeviceLogRetention exceeds BatchLogRetention, clamping to avoid FK-orphan device log rows",
			"before", rc.DeviceLogRetention, "after", rc.BatchLogRetention)
		rc.DeviceLogRetention = rc.BatchLogRetention
	}
	if rc.DeviceLogRetention < rc.QueueMessageRetention {
		slog.Warn("command retention: QueueMessageRetention exceeds DeviceLogRetention, clamping to avoid FK-orphan queue rows",
			"before", rc.QueueMessageRetention, "after", rc.DeviceLogRetention)
		rc.QueueMessageRetention = rc.DeviceLogRetention
	}

	if rc.CleanupInterval < minCleanupInterval {
		slog.Warn("command retention: CleanupInterval below minimum, clamping",
			"before", rc.CleanupInterval, "after", minCleanupInterval)
		rc.CleanupInterval = minCleanupInterval
	}
	if rc.DeleteBatchLimit > maxDeleteBatchSize {
		slog.Warn("command retention: DeleteBatchLimit above maximum, clamping",
			"before", rc.DeleteBatchLimit, "after", maxDeleteBatchSize)
		rc.DeleteBatchLimit = maxDeleteBatchSize
	}
}

// RetentionCleaner periodically ages out command-audit tables per
// RetentionConfig. Deletes are paginated and ordered so FK constraints never
// block the cleaner:
//
//	queue_message (terminal)   -> command_on_device_log -> command_batch_log
//
// Each table is drained in a loop until its LIMIT-bounded delete returns zero
// rows, then the cleaner sleeps until the next tick.
type RetentionCleaner struct {
	conn   *sql.DB
	config *RetentionConfig
	now    func() time.Time

	// Two mutexes cooperate here:
	//   - lifecycleMu serializes entire Start/Stop calls so concurrent
	//     invocations take turns installing and draining their generation.
	//     Without this, two concurrent Start calls could both observe
	//     prev=nil, both spawn a goroutine, and one would leak.
	//   - mu guards the short cancel/done field reads/writes. Held only for
	//     the duration of a snapshot or install -- never across the drain,
	//     so a worker goroutine that someday needs mu cannot deadlock
	//     against Start/Stop waiting on <-done.
	//
	// The worker goroutine does not touch either mutex today.
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewRetentionCleaner returns a cleaner that mutates cfg to apply defaults for
// zero-valued fields (the caller owns cfg so the defaults stick).
func NewRetentionCleaner(conn *sql.DB, cfg *RetentionConfig) *RetentionCleaner {
	defaultRetentionConfig(cfg)
	return &RetentionCleaner{
		conn:   conn,
		config: cfg,
		now:    time.Now,
	}
}

// Start launches the cleaner goroutine. Safe to call with a nil receiver
// and safe to call multiple times -- lifecycleMu serializes overlapping
// callers so the previous generation's goroutine is always drained before a
// new one is installed.
//
// Locking order: Start/Stop run under lifecycleMu. Inside, cancel/done are
// read and written under c.mu but the drain of a previous generation
// happens outside c.mu so a worker that ever needs c.mu (none do today,
// defence in depth) cannot deadlock against Start/Stop waiting on <-done.
func (c *RetentionCleaner) Start(ctx context.Context) {
	if c == nil {
		return
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	// Snapshot and clear any previous generation under the field lock, drain
	// it outside. Since lifecycleMu serializes Start/Stop callers, we know
	// nobody else will touch cancel/done while we're draining.
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
					slog.Error("command retention cleaner run failed", "error", err)
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

// RunOnceForTest invokes a single retention pass without starting the
// goroutine. Exposed for integration tests that want deterministic control
// over when a pass runs.
func (c *RetentionCleaner) RunOnceForTest(ctx context.Context) error {
	return c.runOnce(ctx)
}

// runOnce performs a full pass: queue_message terminals first, then
// per-device logs, then batch headers. Exposed for tests.
func (c *RetentionCleaner) runOnce(ctx context.Context) error {
	now := c.now()

	qmDeleted, err := c.drain(ctx,
		"queue_message terminal rows",
		now.Add(-c.config.QueueMessageRetention),
		c.deleteQueueMessages,
	)
	if err != nil {
		return fmt.Errorf("draining queue_message: %w", err)
	}

	codlDeleted, err := c.drain(ctx,
		"command_on_device_log rows",
		now.Add(-c.config.DeviceLogRetention),
		c.deleteDeviceLogs,
	)
	if err != nil {
		return fmt.Errorf("draining command_on_device_log: %w", err)
	}

	cblDeleted, err := c.drain(ctx,
		"command_batch_log headers",
		now.Add(-c.config.BatchLogRetention),
		c.deleteBatchLogs,
	)
	if err != nil {
		return fmt.Errorf("draining command_batch_log: %w", err)
	}

	if qmDeleted+codlDeleted+cblDeleted > 0 {
		slog.Info("command retention cleaner deleted rows",
			"queue_message", qmDeleted,
			"command_on_device_log", codlDeleted,
			"command_batch_log", cblDeleted)
	}
	return nil
}

// drain runs the supplied delete function in a loop until it returns zero rows
// or the context is done. Returns the total rows deleted.
func (c *RetentionCleaner) drain(
	ctx context.Context,
	label string,
	cutoff time.Time,
	fn func(ctx context.Context, cutoff time.Time, limit int32) (int64, error),
) (int64, error) {
	total := int64(0)
	// #nosec G115 -- DeleteBatchLimit is bounded by the config's int range.
	limit := int32(c.config.DeleteBatchLimit)
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		stepCtx, cancel := context.WithTimeout(ctx, retentionStepTimeout)
		deleted, err := fn(stepCtx, cutoff, limit)
		cancel()
		if err != nil {
			return total, fmt.Errorf("%s: %w", label, err)
		}
		total += deleted
		if deleted < int64(limit) {
			return total, nil
		}
	}
}

func (c *RetentionCleaner) deleteQueueMessages(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	return db.WithTransaction(ctx, c.conn, func(q *sqlc.Queries) (int64, error) {
		return q.DeleteTerminalQueueMessagesOlderThan(ctx, sqlc.DeleteTerminalQueueMessagesOlderThanParams{
			Cutoff:  cutoff,
			MaxRows: limit,
		})
	})
}

func (c *RetentionCleaner) deleteDeviceLogs(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	return db.WithTransaction(ctx, c.conn, func(q *sqlc.Queries) (int64, error) {
		return q.DeleteCommandOnDeviceLogsOlderThan(ctx, sqlc.DeleteCommandOnDeviceLogsOlderThanParams{
			Cutoff:  cutoff,
			MaxRows: limit,
		})
	})
}

func (c *RetentionCleaner) deleteBatchLogs(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	return db.WithTransaction(ctx, c.conn, func(q *sqlc.Queries) (int64, error) {
		return q.DeleteCommandBatchLogsOlderThan(ctx, sqlc.DeleteCommandBatchLogsOlderThanParams{
			Cutoff:  sql.NullTime{Time: cutoff, Valid: true},
			MaxRows: limit,
		})
	})
}
