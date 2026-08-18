package betweenchannel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	defaultFinalizerInterval  = 5 * time.Second
	defaultFinalizerBatchSize = 100
)

type FinalizerConfig struct {
	TickInterval time.Duration `help:"Interval between rollout membership finalization passes." default:"5s" env:"TICK_INTERVAL"`
	BatchSize    int32         `help:"Maximum rollout members finalized per pass." default:"100" env:"BATCH_SIZE"`
}

func (c FinalizerConfig) withDefaults() FinalizerConfig {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultFinalizerInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultFinalizerBatchSize
	}
	return c
}

type ActivityLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

type Finalizer struct {
	cfg      FinalizerConfig
	store    FinalizationStore
	activity ActivityLogger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

var _ runtimejobs.Lifecycle = (*Finalizer)(nil)

func NewFinalizer(
	cfg FinalizerConfig,
	store FinalizationStore,
	activity ActivityLogger,
) *Finalizer {
	return &Finalizer{
		cfg:      cfg.withDefaults(),
		store:    store,
		activity: activity,
	}
}

func (f *Finalizer) Start(ctx context.Context) error {
	if f.store == nil {
		return errors.New("between-channel rollout finalizer: store is required")
	}
	if f.cfg.TickInterval < time.Second {
		return fmt.Errorf(
			"between-channel rollout finalizer: tick_interval must be at least 1s, got %s",
			f.cfg.TickInterval,
		)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel != nil {
		select {
		case <-f.done:
			return errors.New("between-channel rollout finalizer: previous activation is stopping")
		default:
			return nil
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	f.cancel = cancel
	f.done = done
	go f.loop(runCtx, done)
	return nil
}

func (f *Finalizer) Stop(ctx context.Context) error {
	f.mu.Lock()
	if f.cancel == nil {
		f.mu.Unlock()
		return nil
	}
	cancel := f.cancel
	done := f.done
	f.mu.Unlock()

	cancel()
	select {
	case <-done:
		f.mu.Lock()
		if f.done == done {
			f.cancel = nil
			f.done = nil
		}
		f.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("between-channel rollout finalizer: stop: %w", ctx.Err())
	}
}

func (f *Finalizer) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	reportProgress := runtimejobs.TrackProgress(ctx, f.cfg.TickInterval)
	f.RunOnce(ctx)
	reportProgress()

	ticker := time.NewTicker(f.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.RunOnce(ctx)
			reportProgress()
		}
	}
}

func (f *Finalizer) RunOnce(ctx context.Context) {
	rows, err := f.store.ListFinalizations(ctx, f.cfg.BatchSize)
	if err != nil {
		slog.Error("between-channel rollout finalizer failed to list members", "error", err)
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		result, finalizeErr := f.store.Finalize(ctx, row)
		if finalizeErr != nil {
			slog.Error(
				"between-channel rollout member finalization failed",
				"rollout_id",
				row.RolloutID,
				"member_id",
				row.MemberID,
				"error",
				finalizeErr,
			)
			continue
		}
		f.projectActivity(ctx, result)
	}
}

func (f *Finalizer) projectActivity(
	ctx context.Context,
	result FinalizationResult,
) {
	if f.activity == nil || !result.ProjectActivity {
		return
	}
	orgID := result.OrgID
	scopeType := "rollout_lane"
	scopeLabel := result.LaneID.String()
	f.activity.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySystem,
		Type:           "between_channel_rollout_member." + string(result.Outcome),
		Description:    "Finalized rollout lane membership",
		ScopeType:      &scopeType,
		ScopeLabel:     &scopeLabel,
		ActorType:      activitymodels.ActorSystem,
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"rollout_id":        result.RolloutID.String(),
			"member_id":         result.MemberID,
			"device_identifier": result.DeviceIdentifier,
			"source_channel_id": result.SourceChannelID,
			"target_channel_id": result.TargetChannelID,
		},
	})
}
