// Package reconciler drives curtailment events through bounded command retries,
// telemetry confirmation, drift correction and restoration.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// reconcilerActorName tags the synthetic dispatch ctx so audit + filter
	// bypass recognize reconciler self-traffic.
	reconcilerActorName = "curtailment-reconciler"

	defaultTickInterval            = 30 * time.Second
	defaultMaxRetries        int32 = 10
	defaultCurtailMaxRetries int32 = 50

	// 0.5: power_w > baseline*factor is drifted; catches partial restore.
	defaultDriftThresholdFactor = 0.5

	// Per-target telemetry confirmation timeouts; both increment retry_count.
	defaultCurtailDispatchTimeoutSec = 5
	defaultRestoreDispatchTimeoutSec = 30
)

const (
	skipPendingDispatchClock   = false
	recordPendingDispatchClock = true
)

// CommandDispatcher is the subset of command.Service the reconciler needs;
// keeps tests free of the full command-service graph.
type CommandDispatcher interface {
	Curtail(ctx context.Context, selector *pb.DeviceSelector, level sdk.CurtailLevel) (*command.CommandResult, error)
	Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error)
}

// Config carries runtime tunables. Zero-valued fields use defaults.
type Config struct {
	TickInterval         time.Duration `help:"Interval between curtailment reconciler ticks. Zero uses the default; values below 1s are rejected." default:"0s" env:"TICK_INTERVAL"`
	MaxRetries           int32
	CurtailMaxRetries    int32
	DriftThresholdFactor float64
	// CurtailDispatchTimeoutSec ages out curtail-phase targets stuck in
	// Dispatched without confirming telemetry (increments retry_count).
	CurtailDispatchTimeoutSec int `help:"Seconds a curtail target may stay dispatched without telemetry confirmation before incrementing retry_count and retrying. Curtail targets give up after CurtailMaxRetries attempts. Zero uses the default; positive values must be at least 1." default:"0" env:"CURTAIL_DISPATCH_TIMEOUT_SEC"`
	// RestoreDispatchTimeoutSec ages out restore-phase targets stuck in
	// Dispatched without confirming telemetry (increments retry_count).
	RestoreDispatchTimeoutSec int `help:"Seconds a restore target may stay dispatched without telemetry confirmation before consuming retry budget. Zero uses the default." default:"0" env:"RESTORE_DISPATCH_TIMEOUT_SEC"`
	// ConfirmationFastPathEnabled turns on the wake-driven confirmation
	// pulse (see confirmation_fast_path.go). Disabled restores exact
	// tick-only confirmation semantics and starts no pulse goroutine.
	ConfirmationFastPathEnabled bool `help:"Enable the curtailment confirmation fast path: a wake-driven pulse that confirms dispatched targets from fresh telemetry between full reconciler ticks." default:"true" env:"CONFIRMATION_FAST_PATH_ENABLED"`
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = defaultMaxRetries
	}
	if c.CurtailMaxRetries <= 0 {
		c.CurtailMaxRetries = defaultCurtailMaxRetries
	}
	if c.DriftThresholdFactor <= 0 {
		c.DriftThresholdFactor = defaultDriftThresholdFactor
	}
	if c.CurtailDispatchTimeoutSec == 0 {
		c.CurtailDispatchTimeoutSec = defaultCurtailDispatchTimeoutSec
	}
	if c.RestoreDispatchTimeoutSec <= 0 {
		c.RestoreDispatchTimeoutSec = defaultRestoreDispatchTimeoutSec
	}
	return c
}

// Reconciler is a singleton goroutine ticking every config.TickInterval.
// Each tick reads non-terminal events, dispatches/observes per event with
// per-event panic isolation, then upserts the heartbeat.
type Reconciler struct {
	cfg         Config
	store       interfaces.CurtailmentStore
	fanStore    interfaces.CurtailmentFanStateStore
	cmd         CommandDispatcher
	permissions DispatchPermissionResolver
	metrics     curtailment.Metrics
	fans        curtailment.FacilityFanController
	fanAlert    FacilityFanAlertEmitter
	now         func() time.Time

	// sampler backs the confirmation fast path (see
	// confirmation_fast_path.go); required only when
	// cfg.ConfirmationFastPathEnabled.
	sampler           ConfirmationSampler
	confirmationStore interfaces.CurtailmentConfirmationStore
	// confirmationWake coalesces pulse wakes; buffered size 1.
	confirmationWake chan struct{}
	// confirmationPulse is the between-pass cadence while eligible work
	// exists. Defaults to confirmationPulseInterval; tests shorten it.
	confirmationPulse time.Duration
	// confirmationPassTimeout bounds the sampling half of one pulse pass
	// (eligibility read + batch sampling). Defaults to the
	// confirmationPassTimeout constant; tests shorten it to force the
	// split-budget path where sampling exhausts the pass budget while the
	// separate write budget stays live.
	confirmationPassTimeout time.Duration
	// confirmationCursor is activation-owned keyset state. Each successful
	// eligibility read advances it even when no target promotes, preventing
	// a nonconfirming page from monopolizing the active pulse.
	confirmationCursor interfaces.ConfirmationPageCursor

	loopCancel  context.CancelFunc
	workCancel  context.CancelFunc
	runCanceled <-chan struct{}
	runDone     <-chan struct{}

	lifecycleMu sync.Mutex
	mu          sync.Mutex
}

var _ runtimejobs.Lifecycle = (*Reconciler)(nil)

var _ runtimejobs.Aborter = (*Reconciler)(nil)

// Option configures a Reconciler at construction time.
type Option func(*Reconciler)

// WithMetrics injects the operational metrics recorder; nil keeps the
// NoOpMetrics default.
func WithMetrics(m curtailment.Metrics) Option {
	return func(r *Reconciler) {
		if m != nil {
			r.metrics = m
		}
	}
}

func WithFacilityFanController(controller curtailment.FacilityFanController) Option {
	return func(r *Reconciler) { r.fans = controller }
}

// FacilityFanAlertEmitter is implemented by the metrics provider. The
// reconciler emits a per-event state gauge when failed fan-ON commands reach
// the point where miner restoration is allowed to proceed.
type FacilityFanAlertEmitter interface {
	EmitCurtailmentFanRestoreFailure(ctx context.Context, orgID int64, eventUUID string, failed bool)
}

func WithFacilityFanAlertEmitter(emitter FacilityFanAlertEmitter) Option {
	return func(r *Reconciler) { r.fanAlert = emitter }
}

// New builds a Reconciler. nil store/dispatcher is rejected at Start, not
// here, so a misconfigured fleetd surfaces during lifecycle bring-up.
func New(cfg Config, store interfaces.CurtailmentStore, cmd CommandDispatcher, opts ...Option) *Reconciler {
	r := &Reconciler{
		cfg:                     cfg.withDefaults(),
		store:                   store,
		cmd:                     cmd,
		metrics:                 curtailment.NoOpMetrics{},
		now:                     time.Now,
		confirmationWake:        make(chan struct{}, 1),
		confirmationPulse:       confirmationPulseInterval,
		confirmationPassTimeout: confirmationPassTimeout,
	}
	if fanStore, ok := store.(interfaces.CurtailmentFanStateStore); ok {
		r.fanStore = fanStore
	}
	if confirmationStore, ok := store.(interfaces.CurtailmentConfirmationStore); ok {
		r.confirmationStore = confirmationStore
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Start spins up the tick loop for the lifetime of ctx. Repeat Starts without
// an intervening Stop are no-ops so misbehaving wiring cannot fork two
// reconcilers.
func (r *Reconciler) Start(ctx context.Context) error {
	if r.store == nil {
		return fmt.Errorf("curtailment reconciler: store is required")
	}
	if r.cmd == nil {
		return fmt.Errorf("curtailment reconciler: command dispatcher is required")
	}
	if r.cfg.TickInterval < time.Second {
		return fmt.Errorf("curtailment reconciler: tick_interval must be at least 1s, got %s", r.cfg.TickInterval)
	}
	if r.cfg.CurtailDispatchTimeoutSec < 1 {
		return fmt.Errorf("curtailment reconciler: curtail_dispatch_timeout_sec must be at least 1, got %d", r.cfg.CurtailDispatchTimeoutSec)
	}
	if r.cfg.ConfirmationFastPathEnabled && r.sampler == nil {
		return fmt.Errorf("curtailment reconciler: confirmation fast path is enabled but no sampler is configured (WithConfirmationSampler)")
	}
	if r.cfg.ConfirmationFastPathEnabled && r.confirmationStore == nil {
		return fmt.Errorf("curtailment reconciler: confirmation fast path is enabled but the store does not support bulk confirmation")
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	if r.runDone != nil {
		stopping := channelClosed(r.runCanceled)
		r.mu.Unlock()
		if stopping {
			return errors.New("curtailment reconciler: previous activation is still stopping")
		}
		return nil
	}
	workCtx, workCancel := context.WithCancel(context.WithoutCancel(ctx))
	loopCtx, loopCancel := context.WithCancel(ctx)
	runDone := make(chan struct{})
	r.loopCancel = loopCancel
	r.workCancel = workCancel
	r.runCanceled = loopCtx.Done()
	r.runDone = runDone
	r.confirmationCursor = interfaces.ConfirmationPageCursor{}
	r.mu.Unlock()

	go r.tickLoop(loopCtx, workCtx, runDone)
	slog.Debug("configured curtailment reconciler",
		"tick_interval", r.cfg.TickInterval,
		"confirmation_fast_path_enabled", r.cfg.ConfirmationFastPathEnabled)
	return nil
}

// Stop cancels the activation and waits for it to drain within ctx. A timed-out
// activation retains ownership until its goroutine actually exits, so Start can
// never overlap it.
func (r *Reconciler) Stop(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	if r.runDone == nil {
		r.mu.Unlock()
		return nil
	}
	loopCancel := r.loopCancel
	workCancel := r.workCancel
	runDone := r.runDone
	r.mu.Unlock()

	if loopCancel != nil {
		loopCancel()
	}
	select {
	case <-runDone:
		if workCancel != nil {
			workCancel()
		}
		return nil
	case <-ctx.Done():
		if workCancel != nil {
			workCancel()
		}
		return fmt.Errorf("curtailment reconciler: stop: %w", ctx.Err())
	}
}

// Abort immediately cancels admission and detached work before a fatal exit.
func (r *Reconciler) Abort() {
	r.mu.Lock()
	loopCancel := r.loopCancel
	workCancel := r.workCancel
	r.mu.Unlock()
	if loopCancel != nil {
		loopCancel()
	}
	if workCancel != nil {
		workCancel()
	}
}

func (r *Reconciler) tickLoop(loopCtx, workCtx context.Context, runDone chan<- struct{}) {
	defer close(runDone)
	defer r.finishActivation()

	reportProgress := runtimejobs.TrackProgress(loopCtx, r.cfg.TickInterval)
	var confirmationDone <-chan struct{}
	if r.cfg.ConfirmationFastPathEnabled {
		done := make(chan struct{})
		confirmationDone = done
		go func() {
			defer close(done)
			// The confirmation pulse is an acceleration only, so Stop may
			// cancel an active pass immediately. Full ticks keep using the
			// detached work context so their existing drain semantics remain
			// unchanged.
			r.confirmationLoop(loopCtx, loopCtx)
		}()
		// Startup recovery: rows may already sit in dispatched from a
		// previous process; run one pass immediately rather than waiting
		// for the first full tick's wake.
		r.wakeConfirmation()
	}
	if confirmationDone != nil {
		defer func() { <-confirmationDone }()
	}

	// HA activation must recover pending and restoring work immediately.
	if loopCtx.Err() == nil {
		r.safeTick(workCtx)
		reportProgress()
	}
	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-loopCtx.Done():
			return
		case <-ticker.C:
			r.safeTick(workCtx)
			if loopCtx.Err() != nil {
				return
			}
			reportProgress()
		}
	}
}

func (r *Reconciler) finishActivation() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loopCancel = nil
	r.workCancel = nil
	r.runCanceled = nil
	r.runDone = nil
}

func channelClosed(done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// safeTick recovers panics in tick-level infra so the goroutine survives;
// per-event isolation lives in processEvent.
func (r *Reconciler) safeTick(ctx context.Context) {
	tickStart := r.now()
	defer func() {
		r.metrics.ObserveTickDuration(r.now().Sub(tickStart))
	}()
	defer func() {
		if rec := recover(); rec != nil {
			r.metrics.IncTickFailure()
			slog.Error("curtailment reconciler: recovered panic in tick", "panic", rec)
		}
	}()
	r.runTick(ctx)
}

// runTick is one reconciliation pass. Heartbeat upsert always fires so a
// bad event can't blind liveness; per-event deadlines stop one slow event
// from spending the whole tick's context budget.
func (r *Reconciler) runTick(ctx context.Context) {
	tickStart := r.now()
	tickUUID := uuid.New()
	tickCtx, cancel := context.WithTimeout(ctx, 2*r.cfg.TickInterval)
	defer cancel()
	events, err := r.store.ListNonTerminalEvents(tickCtx)
	if err != nil {
		slog.Error("curtailment reconciler: failed to list non-terminal events", "error", err)
		r.metrics.IncTickFailure()
		// Heartbeat advances on tick freshness, not query health. The SQL
		// staleness alert thus distinguishes "reconciler dead" (no upsert)
		// from "DB read path degraded" (upsert advances, IncTickFailure
		// rises).
		r.upsertHeartbeat(ctx, tickStart, tickUUID, 0)
		return
	}

	for index, ev := range events {
		if tickCtx.Err() != nil {
			break
		}
		deadline, ok := tickCtx.Deadline()
		if !ok {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		// Divide the remaining tick budget across the remaining events. An
		// unreachable fan set or another slow boundary on an earlier event can
		// consume its share, but cannot starve every later event in ID order.
		remainingEvents := len(events) - index
		eventCtx, eventCancel := context.WithTimeout(tickCtx, remaining/time.Duration(remainingEvents))
		r.processEvent(eventCtx, ev)
		eventCancel()
	}

	r.upsertHeartbeat(ctx, tickStart, tickUUID, int32(len(events))) //nolint:gosec // bounded by org event count
}

func (r *Reconciler) upsertHeartbeat(_ context.Context, tickStart time.Time, tickUUID uuid.UUID, activeCount int32) {
	durationMS := int32(r.now().Sub(tickStart).Milliseconds()) //nolint:gosec // tick durations fit in int32
	// Detached ctx so shutdown-watchdog cancellation cannot drop the final
	// heartbeat; 5s bounds a stuck DB.
	hbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.store.UpsertHeartbeat(hbCtx, interfaces.UpsertCurtailmentHeartbeatParams{
		LastTickAt:         tickStart,
		LastTickUUID:       tickUUID,
		LastTickDurationMS: &durationMS,
		ActiveEventCount:   activeCount,
	}); err != nil {
		slog.Error("curtailment reconciler: heartbeat upsert failed", "error", err)
	}
}

// processEvent dispatches per-state work for one event; recover keeps
// a per-event panic from aborting the rest of the tick.
func (r *Reconciler) processEvent(ctx context.Context, ev *models.Event) {
	defer func() {
		if rec := recover(); rec != nil {
			r.metrics.IncTickFailure()
			slog.Error("curtailment reconciler: recovered panic processing event",
				"event_id", ev.ID, "event_uuid", ev.EventUUID, "panic", rec)
		}
	}()
	ctx = context.WithValue(ctx, eventPlanContextKey{}, decodeEventPlan(ev))
	switch ev.State { //nolint:exhaustive // Terminal states are filtered upstream by ListNonTerminalEvents; default logs if one slips through.
	case models.EventStatePending:
		r.dispatchPending(ctx, ev)
	case models.EventStateActive:
		r.observeActive(ctx, ev)
	case models.EventStateRestoring:
		r.observeRestoring(ctx, ev)
	default:
		slog.Warn("curtailment reconciler: unexpected event state",
			"event_id", ev.ID, "state", ev.State)
	}
}
