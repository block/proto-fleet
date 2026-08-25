package rollout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	defaultControlReconcilerInterval   = 5 * time.Second
	defaultControlReconcilerBatchSize  = 100
	defaultControlReconcilerStaleAfter = 30 * time.Second
)

type ControlReconcilerConfig struct {
	TickInterval time.Duration `help:"Interval between rollout control reconciliation passes." default:"5s" env:"TICK_INTERVAL"`
	BatchSize    int32         `help:"Maximum started rollout controls reconciled per pass." default:"100" env:"BATCH_SIZE"`
	StaleAfter   time.Duration `help:"Minimum started-control age before reconciliation." default:"30s" env:"STALE_AFTER"`
}

func (c ControlReconcilerConfig) withDefaults() ControlReconcilerConfig {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultControlReconcilerInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultControlReconcilerBatchSize
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = defaultControlReconcilerStaleAfter
	}
	return c
}

type StartedControlCandidate struct {
	ID        uuid.UUID
	RolloutID uuid.UUID
	OrgID     int64
	Operation ControlOperation
	UpdatedAt time.Time
}

type ControlReconciliationOutcome string

const (
	ControlReconciliationCommitted  ControlReconciliationOutcome = "committed"
	ControlReconciliationRolledBack ControlReconciliationOutcome = "definitively_rolled_back"
	ControlReconciliationDeferred   ControlReconciliationOutcome = "deferred"
	ControlReconciliationSettled    ControlReconciliationOutcome = "already_settled"
)

type ControlReconciliationStore interface {
	ListStartedControlCandidates(
		ctx context.Context,
		staleBefore time.Time,
		limit int32,
	) ([]StartedControlCandidate, error)
	ReconcileStartedControl(
		ctx context.Context,
		candidate StartedControlCandidate,
	) (ControlReconciliationOutcome, error)
}

type ControlReconciler struct {
	cfg   ControlReconcilerConfig
	store ControlReconciliationStore
	now   func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

var _ runtimejobs.Lifecycle = (*ControlReconciler)(nil)

func NewControlReconciler(
	cfg ControlReconcilerConfig,
	store ControlReconciliationStore,
) *ControlReconciler {
	return &ControlReconciler{
		cfg:   cfg.withDefaults(),
		store: store,
		now:   time.Now,
	}
}

func (r *ControlReconciler) Start(ctx context.Context) error {
	if r.store == nil {
		return errors.New("rollout control reconciler: store is required")
	}
	if r.cfg.TickInterval < time.Second {
		return fmt.Errorf(
			"rollout control reconciler: tick_interval must be at least 1s, got %s",
			r.cfg.TickInterval,
		)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rollout control reconciler: start: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		select {
		case <-r.done:
			return errors.New("rollout control reconciler: previous activation is stopping")
		default:
			return nil
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done
	go r.loop(runCtx, done)
	return nil
}

func (r *ControlReconciler) Stop(ctx context.Context) error {
	r.mu.Lock()
	if r.cancel == nil {
		r.mu.Unlock()
		return nil
	}
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	cancel()
	select {
	case <-done:
		r.mu.Lock()
		if r.done == done {
			r.cancel = nil
			r.done = nil
		}
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("rollout control reconciler: stop: %w", ctx.Err())
	}
}

func (r *ControlReconciler) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	reportProgress := runtimejobs.TrackProgress(ctx, r.cfg.TickInterval)
	r.RunOnce(ctx)
	reportProgress()

	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
			reportProgress()
		}
	}
}

func (r *ControlReconciler) RunOnce(ctx context.Context) {
	candidates, err := r.store.ListStartedControlCandidates(
		ctx,
		r.now().UTC().Add(-r.cfg.StaleAfter),
		r.cfg.BatchSize,
	)
	if err != nil {
		slog.Error("rollout control reconciler failed to list candidates", "error", err)
		return
	}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}
		outcome, reconcileErr := r.store.ReconcileStartedControl(ctx, candidate)
		if reconcileErr != nil {
			slog.Error(
				"rollout control reconciliation failed",
				"rollout_id", candidate.RolloutID,
				"control_id", candidate.ID,
				"operation", candidate.Operation,
				"error", reconcileErr,
			)
			continue
		}
		if outcome == ControlReconciliationDeferred {
			slog.Warn(
				"rollout control reconciliation deferred for inconsistent durable state",
				"rollout_id", candidate.RolloutID,
				"control_id", candidate.ID,
				"operation", candidate.Operation,
			)
		}
	}
}
