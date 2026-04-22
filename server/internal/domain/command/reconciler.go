package command

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// ActivityLogger is the subset of the activity service the reconciler needs.
// Declared as an interface so tests can substitute a spy without depending on
// the domain service struct. LogStrict surfaces persistence errors so the
// reconciler can decide whether to log + skip vs. retry (today it logs and
// continues; returning the error also lets composition layers see it).
type ActivityLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
	LogStrict(ctx context.Context, event activitymodels.Event) error
}

// reconcilerDBTimeout bounds individual database calls performed by the
// reconciler. Each tick may issue several of these in sequence.
const reconcilerDBTimeout = 15 * time.Second

// CompletionReconciler periodically backfills '<event_type>.completed' activity
// rows for batches that FINISHED without one. This covers:
//
//   - Server crashes between the batch being marked FINISHED and the activity
//     finalizer writing its row.
//   - Finalizer callbacks that exhausted their 3 retries (see
//     initializeStatusUpdateRoutine) and gave up.
//
// The reconciler only acts on batches whose creator already wrote an
// '<event_type>' row (i.e. the normal user-initiated path); internally
// triggered batches (worker-name reapply) stay out of the activity timeline.
//
// Idempotency is enforced by the partial unique index
// uq_activity_log_batch_completed, which the SQLActivityStore swallows via
// isCompletedBatchDuplicate. Concurrent reconcilers across replicas are
// therefore safe.
type CompletionReconciler struct {
	conn           *sql.DB
	config         *Config
	activityLogger ActivityLogger
	now            func() time.Time

	// lifecycleMu serializes Start/Stop so overlapping callers take turns
	// installing and draining their generation; mu guards the short
	// cancel/done field reads/writes. See command.RetentionCleaner for the
	// full rationale.
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewCompletionReconciler builds a reconciler ready to Start. Injecting the
// ActivityLogger interface keeps the reconciler decoupled from the activity
// domain package's concrete Service type (helpful for tests).
func NewCompletionReconciler(conn *sql.DB, config *Config, activityLogger ActivityLogger) *CompletionReconciler {
	if config.ReconcilerInterval <= 0 {
		config.ReconcilerInterval = 5 * time.Minute
	}
	if config.ReconcilerGracePeriod <= 0 {
		config.ReconcilerGracePeriod = 2 * time.Minute
	}
	if config.ReconcilerMaxBatches <= 0 {
		config.ReconcilerMaxBatches = 200
	}
	return &CompletionReconciler{
		conn:           conn,
		config:         config,
		activityLogger: activityLogger,
		now:            time.Now,
	}
}

// Start launches the reconciler goroutine. Calling Start more than once
// replaces the previous goroutine; overlapping callers take turns via
// lifecycleMu so the previous generation is always drained before a new one
// is installed. Safe to call with a nil receiver.
//
// Locking order: Start/Stop run under lifecycleMu; cancel/done are
// read/written under r.mu but the drain happens outside r.mu so a worker
// that ever needs r.mu cannot deadlock against Start/Stop on <-done.
func (r *CompletionReconciler) Start(ctx context.Context) {
	if r == nil {
		return
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	prevCancel, prevDone := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if prevCancel != nil {
		prevCancel()
		<-prevDone
	}

	reconcilerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	r.mu.Lock()
	r.cancel = cancel
	r.done = done
	r.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(r.config.ReconcilerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-reconcilerCtx.Done():
				return
			case <-ticker.C:
				if err := r.runOnce(reconcilerCtx); err != nil {
					slog.Error("completion reconciler run failed", "error", err)
				}
			}
		}
	}()
}

// Stop signals the reconciler goroutine to exit and waits for it to drain.
// Safe to call with a nil receiver or before Start. See Start for the
// locking-order rationale.
func (r *CompletionReconciler) Stop() {
	if r == nil {
		return
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// RunOnceForTest invokes a single reconcile pass without starting the
// goroutine. Exposed for integration tests that need deterministic control.
func (r *CompletionReconciler) RunOnceForTest(ctx context.Context) error {
	return r.runOnce(ctx)
}

// runOnce performs a single reconcile pass. Exposed for tests.
func (r *CompletionReconciler) runOnce(ctx context.Context) error {
	cutoff := r.now().Add(-r.config.ReconcilerGracePeriod)
	rows, err := r.listFinishedWithoutCompletion(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("listing finished batches: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	slog.Info("completion reconciler backfilling activity rows",
		"count", len(rows),
		"cutoff", cutoff)

	for _, row := range rows {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := r.backfillOne(ctx, row); err != nil {
			// Log and continue -- one stuck row shouldn't block the rest.
			slog.Error("completion reconciler backfill failed",
				"batch_id", row.BatchID, "error", err)
		}
	}
	return nil
}

func (r *CompletionReconciler) listFinishedWithoutCompletion(
	ctx context.Context, cutoff time.Time,
) ([]sqlc.ListFinishedBatchesWithoutCompletionRow, error) {
	listCtx, cancel := context.WithTimeout(ctx, reconcilerDBTimeout)
	defer cancel()
	// #nosec G115 -- ReconcilerMaxBatches is bounded by the config's int range.
	return db.WithTransaction(listCtx, r.conn, func(q *sqlc.Queries) ([]sqlc.ListFinishedBatchesWithoutCompletionRow, error) {
		return q.ListFinishedBatchesWithoutCompletion(listCtx, sqlc.ListFinishedBatchesWithoutCompletionParams{
			Cutoff:     sql.NullTime{Time: cutoff, Valid: true},
			MaxBatches: int32(r.config.ReconcilerMaxBatches),
		})
	})
}

func (r *CompletionReconciler) backfillOne(
	ctx context.Context, row sqlc.ListFinishedBatchesWithoutCompletionRow,
) error {
	countsCtx, cancel := context.WithTimeout(ctx, reconcilerDBTimeout)
	defer cancel()
	counts, err := db.WithTransaction(countsCtx, r.conn, func(q *sqlc.Queries) (sqlc.GetBatchStatusAndDeviceCountsRow, error) {
		return q.GetBatchStatusAndDeviceCounts(countsCtx, row.BatchID)
	})
	if err != nil {
		return fmt.Errorf("reading counts for %s: %w", row.BatchID, err)
	}

	// #nosec G115 -- devices_count is bounded by the batch size we created.
	scopeCount := int(counts.DevicesCount)
	batchID := row.BatchID

	// Pruned case: the batch header still exists but every per-device row
	// has been retention-deleted (90d default codl retention vs 180d cbl
	// retention leaves a window where this can happen after a reconciler
	// outage). Writing "0 succeeded, 0 failed" with result=success would be
	// a lie -- the batch did have devices, we just no longer know how they
	// fared. Flag the row as ResultUnknown and stop the reconciler from
	// revisiting the batch (the partial unique index on (batch_id, event_type)
	// makes this insert idempotent).
	observed := counts.SuccessfulDevices + counts.FailedDevices
	pruned := counts.DevicesCount > 0 && observed == 0

	var (
		result         activitymodels.ResultType
		completionDesc string
		metadata       = map[string]any{
			"batch_id":    batchID,
			"total_count": counts.DevicesCount,
			"reconciled":  true,
		}
	)
	switch {
	case pruned:
		result = activitymodels.ResultUnknown
		completionDesc = fmt.Sprintf("%s completed: per-device detail no longer available", row.Description)
		metadata["device_logs_pruned"] = true
	case counts.FailedDevices > 0:
		result = activitymodels.ResultFailure
		completionDesc = fmt.Sprintf("%s completed: %d succeeded, %d failed",
			row.Description, counts.SuccessfulDevices, counts.FailedDevices)
		metadata["success_count"] = counts.SuccessfulDevices
		metadata["failure_count"] = counts.FailedDevices
	default:
		result = activitymodels.ResultSuccess
		completionDesc = fmt.Sprintf("%s completed: %d succeeded, %d failed",
			row.Description, counts.SuccessfulDevices, counts.FailedDevices)
		metadata["success_count"] = counts.SuccessfulDevices
		metadata["failure_count"] = counts.FailedDevices
	}

	event := activitymodels.Event{
		Category:    activitymodels.CategoryDeviceCommand,
		Type:        stripCompletedSuffix(row.InitiatedEventType) + activitymodels.CompletedEventSuffix,
		Description: completionDesc,
		Result:      result,
		ScopeCount:  &scopeCount,
		ActorType:   activitymodels.ActorType(row.ActorType),
		BatchID:     &batchID,
		Metadata:    metadata,
	}
	if row.UserID.Valid {
		v := row.UserID.String
		event.UserID = &v
	}
	if row.Username.Valid {
		v := row.Username.String
		event.Username = &v
	}
	if row.OrganizationID.Valid {
		v := row.OrganizationID.Int64
		event.OrganizationID = &v
	}

	if err := r.activityLogger.LogStrict(ctx, event); err != nil {
		return fmt.Errorf("logging completion for %s: %w", batchID, err)
	}
	return nil
}

// stripCompletedSuffix is a defensive helper: the initiated row should never
// itself end in '.completed', but if it does, we trim before re-appending so
// the resulting event type is stable.
func stripCompletedSuffix(eventType string) string {
	return strings.TrimSuffix(eventType, activitymodels.CompletedEventSuffix)
}
