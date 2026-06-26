// Package reconciler drives non-terminal curtailment events: dispatches
// Curtail commands for pending targets, watches telemetry for drift on
// confirmed targets, and retries within a bounded budget.
package reconciler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"connectrpc.com/authn"
	"github.com/google/uuid"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// reconcilerActorName tags the synthetic dispatch ctx so audit + filter
	// bypass recognize reconciler self-traffic.
	reconcilerActorName = "curtailment-reconciler"

	defaultTickInterval            = 30 * time.Second
	defaultShutdownDeadline        = 10 * time.Second
	defaultMaxRetries        int32 = 10
	defaultCurtailMaxRetries int32 = 50

	// 0.5: power_w > baseline*factor is drifted; catches partial restore.
	defaultDriftThresholdFactor = 0.5

	// Per-target telemetry confirmation timeouts; both burn retry budget.
	defaultCurtailDispatchTimeoutSec = 5
	defaultRestoreDispatchTimeoutSec = 30
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
	ShutdownDeadline     time.Duration
	MaxRetries           int32
	CurtailMaxRetries    int32
	DriftThresholdFactor float64
	// CurtailDispatchTimeoutSec ages out curtail-phase targets stuck in
	// Dispatched without confirming telemetry (burns retry budget).
	CurtailDispatchTimeoutSec int `help:"Seconds a curtail target may stay dispatched without telemetry confirmation before consuming retry budget. Zero uses the default; positive values must be at least 1." default:"0" env:"CURTAIL_DISPATCH_TIMEOUT_SEC"`
	// RestoreDispatchTimeoutSec ages out restore-phase targets stuck in
	// Dispatched without confirming telemetry (burns retry budget).
	RestoreDispatchTimeoutSec int `help:"Seconds a restore target may stay dispatched without telemetry confirmation before consuming retry budget. Zero uses the default." default:"0" env:"RESTORE_DISPATCH_TIMEOUT_SEC"`
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}
	if c.ShutdownDeadline <= 0 {
		c.ShutdownDeadline = defaultShutdownDeadline
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
	cfg     Config
	store   interfaces.CurtailmentStore
	cmd     CommandDispatcher
	metrics curtailment.Metrics
	now     func() time.Time

	stopCancel context.CancelFunc
	workCancel context.CancelFunc
	wg         sync.WaitGroup

	mu      sync.Mutex
	running bool
}

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

// New builds a Reconciler. nil store/dispatcher is rejected at Start, not
// here, so a misconfigured fleetd surfaces during lifecycle bring-up.
func New(cfg Config, store interfaces.CurtailmentStore, cmd CommandDispatcher, opts ...Option) *Reconciler {
	r := &Reconciler{
		cfg:     cfg.withDefaults(),
		store:   store,
		cmd:     cmd,
		metrics: curtailment.NoOpMetrics{},
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Start spins up the tick loop. Repeat Starts without an intervening Stop
// are no-ops so misbehaving wiring cannot fork two reconcilers.
func (r *Reconciler) Start(_ context.Context) error {
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

	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = true
	stopCtx, stopCancel := context.WithCancel(context.Background())
	workCtx, workCancel := context.WithCancel(context.Background())
	r.stopCancel = stopCancel
	r.workCancel = workCancel
	r.mu.Unlock()

	r.wg.Add(1)
	go r.tickLoop(stopCtx, workCtx)
	slog.Info("curtailment reconciler started", "tick_interval", r.cfg.TickInterval)
	return nil
}

// Stop signals the tick loop and waits up to ShutdownDeadline for the
// in-flight tick to drain. Concurrent second Stop is a no-op. Adding a
// Start-after-Stop restart path needs a `stopping` guard to prevent
// stacking goroutines on the same WaitGroup.
func (r *Reconciler) Stop() error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = false
	stopCancel := r.stopCancel
	workCancel := r.workCancel
	r.stopCancel = nil
	r.workCancel = nil
	r.mu.Unlock()

	if workCancel != nil {
		watchdog := time.AfterFunc(r.cfg.ShutdownDeadline, workCancel)
		defer watchdog.Stop()
	}
	if stopCancel != nil {
		stopCancel()
	}
	r.wg.Wait()
	if workCancel != nil {
		workCancel()
	}
	slog.Info("curtailment reconciler stopped")
	return nil
}

func (r *Reconciler) tickLoop(stopCtx, workCtx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCtx.Done():
			return
		case <-ticker.C:
			r.safeTick(workCtx)
		}
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

	for _, ev := range events {
		if tickCtx.Err() != nil {
			break
		}
		eventCtx, eventCancel := context.WithTimeout(tickCtx, 2*r.cfg.TickInterval)
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

// dispatchPending dispatches Curtail per pending target, confirms
// already-dispatched targets via telemetry, then flips the event to
// active once every target is confirmed or terminally failed.
func (r *Reconciler) dispatchPending(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if len(targets) == 0 {
		if isClosedLoopFullFleet(ev) {
			now := r.now()
			if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateActive, &now, nil); err != nil {
				r.logEventStateUpdateError(ev, "pending→active(empty closed-loop)", err)
			}
			return
		}
		// Service.Start rejects empty open-loop plans; zero targets is a
		// contract violation needing manual recovery.
		slog.Error("curtailment reconciler: pending event has no targets",
			"event_id", ev.ID, "event_uuid", ev.EventUUID)
		return
	}

	// Liveness check; per-target race-closure happens in dispatchCurtailBatch.
	// DISPATCHING is included alongside PENDING because ticks are serial,
	// so any DISPATCHING seen here is from an interrupted prior tick — safe
	// to redispatch (Curtail is device-idempotent).
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}
	cmdCtx := reconcilerCommandContext(ctx, ev.OrgID, ev.CreatedByUserID)
	r.dispatchPendingCurtailBatches(cmdCtx, ev, targets)

	// Confirm just-dispatched targets via current telemetry before deciding
	// whether the event itself can flip to active.
	r.confirmDispatched(ctx, ev, targets)
	r.maybeMarkActive(ctx, ev, targets)
}

// dispatchPendingCurtailBatches drains retryable Curtail work in command
// batches. Orphaned DISPATCHING rows from an interrupted prior tick are
// recovered before fresh PENDING rows. curtail_batch_size=NULL dispatches all
// remaining targets; a positive interval paces fresh pending batches.
func (r *Reconciler) dispatchPendingCurtailBatches(ctx context.Context, ev *models.Event, targets []*models.Target) {
	batchSize := curtailBatchSizeForEvent(ev, len(targets))
	dispatchByState := func(state models.TargetState, singleBatch bool) bool {
		claim := make([]*models.Target, 0, batchSize)
		for _, t := range targets {
			if t.State != state {
				continue
			}
			claim = append(claim, t)
			if int32(len(claim)) >= batchSize { //nolint:gosec // batchSize already bounded
				if !r.dispatchCurtailBatch(ctx, ev, claim, state) {
					return false
				}
				if singleBatch {
					return true
				}
				claim = make([]*models.Target, 0, batchSize)
			}
		}
		if len(claim) == 0 {
			return true
		}
		return r.dispatchCurtailBatch(ctx, ev, claim, state)
	}

	intervalActive := curtailBatchIntervalActive(ev)
	hadDispatching := hasTargetsInState(targets, models.TargetStateDispatching)
	if !dispatchByState(models.TargetStateDispatching, intervalActive) {
		return
	}
	if intervalActive && hadDispatching {
		return
	}
	if intervalActive && !r.curtailBatchIntervalElapsed(ev, targets) {
		return
	}
	_ = dispatchByState(models.TargetStatePending, intervalActive)
}

// confirmDispatched promotes Dispatched → Confirmed when telemetry
// shows the device is curtailed.
func (r *Reconciler) confirmDispatched(ctx context.Context, ev *models.Event, targets []*models.Target) {
	deviceIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.State == models.TargetStateDispatched {
			deviceIDs = append(deviceIDs, t.DeviceIdentifier)
		}
	}
	if len(deviceIDs) == 0 {
		return
	}
	cands, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
		OrgID:             ev.OrgID,
		DeviceIdentifiers: deviceIDs,
	})
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (confirm) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	candByID := candidatesByDeviceID(cands)
	for _, t := range targets {
		if t.State != models.TargetStateDispatched {
			continue
		}
		r.confirmOneDispatched(ctx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
	}
}

// dispatchOneCurtail issues one Curtail and records the outcome.
// nonTerminalFailureState is where the target lands on a non-terminal
// failure (Pending or Drifted, per caller).
//
// Race-closure: the DISPATCHING pre-write makes a concurrent
// AdminTerminate see an in-flight target and reject as Stop-first; its
// EXISTS guard against the parent event state catches a terminate that
// committed between the per-tick liveness check and this write.
// Restart-safety: a crash between pre-write and command leaves the
// target in DISPATCHING; the next tick redispatches via
// nonTerminalFailureState (Curtail is device-idempotent).
func (r *Reconciler) dispatchOneCurtail(ctx context.Context, ev *models.Event, t *models.Target, nonTerminalFailureState models.TargetState) {
	_ = r.dispatchCurtailBatch(ctx, ev, []*models.Target{t}, nonTerminalFailureState)
}

// dispatchCurtailBatch issues one Curtail command for every device in claim and
// records per-target dispatched/skipped/failed outcomes.
func (r *Reconciler) dispatchCurtailBatch(ctx context.Context, ev *models.Event, claim []*models.Target, nonTerminalFailureState models.TargetState) bool {
	if len(claim) == 0 {
		return true
	}
	if !r.eventStillDispatchable(ctx, ev) {
		return false
	}
	// last_dispatched_at is *not* stamped here — only successful enqueues
	// advance it (used by the restore-batch interval gate).
	dispatchSet := make([]*models.Target, 0, len(claim))
	for _, t := range claim {
		dispatchingParams := interfaces.UpdateCurtailmentTargetStateParams{
			State: models.TargetStateDispatching,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, dispatchingParams); err != nil {
			if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				return false
			}
			slog.Error("curtailment reconciler: dispatching pre-write failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			// Symmetric to dispatchRestoreBatch: burn one retry slot so a
			// row-specific persistent write failure escalates to terminal
			// after MaxRetries instead of stalling the event indefinitely.
			r.recordDispatchFailure(ctx, ev, t, err.Error(), nonTerminalFailureState)
			continue
		}
		t.State = models.TargetStateDispatching
		dispatchSet = append(dispatchSet, t)
	}
	if len(dispatchSet) == 0 {
		return true
	}
	if !r.eventStillDispatchable(ctx, ev) {
		return false
	}

	deviceIDs := make([]string, 0, len(dispatchSet))
	for _, t := range dispatchSet {
		deviceIDs = append(deviceIDs, t.DeviceIdentifier)
	}
	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: deviceIDs,
			},
		},
	}
	result, dispatchErr := r.cmd.Curtail(ctx, selector, sdk.CurtailLevelFull)
	if dispatchErr != nil {
		errMsg := dispatchErr.Error()
		slog.Error("curtailment reconciler: curtail batch dispatch failed",
			"event_id", ev.ID, "batch_size", len(dispatchSet), "error", dispatchErr)
		for _, t := range dispatchSet {
			r.recordDispatchFailure(ctx, ev, t, errMsg, nonTerminalFailureState)
		}
		return true
	}

	skippedSet := make(map[string]string)
	if result != nil {
		skippedSet = make(map[string]string, len(result.Skipped))
		for _, s := range result.Skipped {
			skippedSet[s.DeviceIdentifier] = skippedDeviceReason(s)
		}
	}
	if result == nil || result.BatchIdentifier == "" {
		const reason = "command produced no batch (no live devices to dispatch)"
		slog.Warn("curtailment reconciler: curtail batch produced empty result",
			"event_id", ev.ID, "batch_size", len(dispatchSet))
		for _, t := range dispatchSet {
			if skipReason, skipped := skippedSet[t.DeviceIdentifier]; skipped {
				r.recordDispatchFailure(ctx, ev, t, skipReason, nonTerminalFailureState)
				continue
			}
			r.recordDispatchFailure(ctx, ev, t, reason, nonTerminalFailureState)
		}
		return true
	}
	dispatchedSet := make(map[string]struct{}, len(result.DispatchedDeviceIdentifiers))
	for _, deviceID := range result.DispatchedDeviceIdentifiers {
		dispatchedSet[deviceID] = struct{}{}
	}

	now := r.now()
	emptyErr := ""
	batchID := result.BatchIdentifier
	desiredCurtailed := models.DesiredStateCurtailed
	for _, t := range dispatchSet {
		if skipReason, skipped := skippedSet[t.DeviceIdentifier]; skipped {
			slog.Warn("curtailment reconciler: dispatch filter-skipped",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "reason", skipReason)
			r.recordDispatchFailure(ctx, ev, t, skipReason, nonTerminalFailureState)
			continue
		}
		if _, dispatched := dispatchedSet[t.DeviceIdentifier]; !dispatched {
			const reason = "curtail command did not enqueue device"
			slog.Warn("curtailment reconciler: curtail device not dispatched",
				"event_id", ev.ID, "device", t.DeviceIdentifier)
			r.recordDispatchFailure(ctx, ev, t, reason, nonTerminalFailureState)
			continue
		}
		// Explicit dispatch direction at the call site; writeTargetState's
		// auto-fill would derive the same value from ev.State.
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:                models.TargetStateDispatched,
			LastDispatchedAt:     &now,
			LastError:            &emptyErr,
			LastBatchUUID:        &batchID,
			ExpectedDesiredState: &desiredCurtailed,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: target dispatch update failed",
					"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
				r.recordDispatchFailure(ctx, ev, t, err.Error(), nonTerminalFailureState)
			}
			continue
		}
		// Mirror to the in-memory row for this tick's downstream phases.
		t.State = models.TargetStateDispatched
		t.LastDispatchedAt = &now
		t.LastError = nil
		t.LastBatchUUID = &batchID
		t.CurtailPhase.State = models.TargetStateDispatched
		t.CurtailPhase.DispatchedAt = &now
		t.CurtailPhase.BatchUUID = &batchID
	}
	return true
}

// recordDispatchFailure bumps retry_count. Restore targets transition to
// RestoreFailed at MaxRetries so the event can complete; curtail targets stay
// retryable while OFF remains asserted, with retry_count surfacing the alert.
func (r *Reconciler) recordDispatchFailure(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState) {
	newRetry := t.RetryCount + 1
	state := nonTerminalFailureState
	if r.retryBudgetTerminalizes(t, newRetry) {
		state = models.TargetStateRestoreFailed
	}
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:      state,
		LastError:  &errMsg,
		RetryCount: &newRetry,
	}
	err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params)
	if err == nil {
		t.State = state
		t.RetryCount = newRetry
		t.LastError = &errMsg
		return
	}
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		return
	}
	slog.Error("curtailment reconciler: target update after dispatch failure failed",
		"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
	// Fallback: advance retry budget only. State stays at the prior value;
	// terminal restore escalation lands on the next successful UpdateTargetState.
	if bumpErr := r.store.BumpTargetRetry(ctx, ev.ID, t.DeviceIdentifier); bumpErr != nil {
		if !errors.Is(bumpErr, interfaces.ErrCurtailmentEventStateRaceLoss) {
			r.metrics.IncTargetWriteFailure()
			slog.Error("curtailment reconciler: retry-budget bump fallback failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", bumpErr)
		}
		return
	}
	t.RetryCount = newRetry
}

func (r *Reconciler) maxRetriesForTarget(t *models.Target) int32 {
	if isCurtailRetryTarget(t) {
		return r.cfg.CurtailMaxRetries
	}
	return r.cfg.MaxRetries
}

func (r *Reconciler) retryBudgetTerminalizes(t *models.Target, retryCount int32) bool {
	return t == nil || (!isCurtailRetryTarget(t) && retryCount >= r.maxRetriesForTarget(t))
}

func isCurtailRetryTarget(t *models.Target) bool {
	return t != nil && (t.DesiredState == "" || t.DesiredState == models.DesiredStateCurtailed)
}

// candidatesByDeviceID indexes a candidate slice by device identifier for
// the per-tick observe loops that join targets against telemetry.
func candidatesByDeviceID(cands []*models.Candidate) map[string]*models.Candidate {
	out := make(map[string]*models.Candidate, len(cands))
	for _, c := range cands {
		out[c.DeviceIdentifier] = c
	}
	return out
}

// skippedDeviceReason renders the priority-ordered reason string for a
// filter-skipped device: explicit reason first, filter name next, generic
// fallback last. Shared by the single-device and batch dispatch paths so
// both produce the same audit string.
func skippedDeviceReason(s command.SkippedDevice) string {
	switch {
	case s.Reason != "":
		return s.Reason
	case s.FilterName != "":
		return "filtered by " + s.FilterName
	default:
		return "filtered by command preflight"
	}
}

// observeActive checks drift on confirmed targets and re-dispatches drifted
// targets up to MaxRetries. ListCandidates over-fetches columns the drift
// check ignores; acceptable at the per-tick fanout scale.
func (r *Reconciler) observeActive(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets (active) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if r.enforceMaxDuration(ctx, ev, targets) {
		return
	}

	// Per-tick liveness check; per-target race closure is in dispatchOneCurtail.
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}
	cmdCtx := reconcilerCommandContext(ctx, ev.OrgID, ev.CreatedByUserID)
	if len(targets) > 0 {
		deviceIDs := make([]string, 0, len(targets))
		for _, t := range targets {
			deviceIDs = append(deviceIDs, t.DeviceIdentifier)
		}
		cands, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
			OrgID:             ev.OrgID,
			DeviceIdentifiers: deviceIDs,
		})
		if err != nil {
			slog.Error("curtailment reconciler: list candidates (drift) failed",
				"event_id", ev.ID, "error", err)
		} else {
			candByID := candidatesByDeviceID(cands)
			for _, t := range targets {
				switch t.State {
				case models.TargetStateConfirmed:
					r.checkDrift(cmdCtx, ev, t, candByID[t.DeviceIdentifier])
				case models.TargetStateDispatched:
					// Re-entry: drifted-then-redispatched, waiting on confirmation.
					r.confirmOneDispatched(cmdCtx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
				case models.TargetStateDispatching:
					// Orphan from an interrupted prior tick; redispatched after
					// observation in batch-aware order.
					if r.retryBudgetTerminalizes(t, t.RetryCount) {
						// Escalate restore targets instead of leaving the row pinned
						// in DISPATCHING after retry_count passes MaxRetries.
						r.recordDispatchFailure(cmdCtx, ev, t,
							"retry budget exhausted from interrupted dispatch",
							models.TargetStateDispatching)
						continue
					}
				case models.TargetStateDrifted:
					if r.retryBudgetTerminalizes(t, t.RetryCount) {
						// Symmetric to the DISPATCHING arm: a Drifted target whose
						// retry budget was bumped past MaxRetries by the
						// BumpTargetRetry fallback must terminalize, not loop.
						r.recordDispatchFailure(cmdCtx, ev, t,
							"retry budget exhausted on drifted target",
							models.TargetStateDrifted)
						continue
					}
					r.dispatchOneCurtail(cmdCtx, ev, t, models.TargetStateDrifted)
				case models.TargetStatePending,
					models.TargetStateResolved, models.TargetStateReleased,
					models.TargetStateRestoreFailed:
					// Pending rows are handled by the active closed-loop dispatch pass
					// below. Terminal states are restorer-owned.
				}
			}
			r.dispatchPendingCurtailBatches(cmdCtx, ev, targets)
		}
	}
	claimed := r.claimClosedLoopFullFleetTargets(ctx, ev, targets)
	r.dispatchClaimedCurtailTargets(cmdCtx, ev, claimed)
}

func (r *Reconciler) claimClosedLoopFullFleetTargets(ctx context.Context, ev *models.Event, existingTargets []*models.Target) []*models.Target {
	if !isClosedLoopFullFleet(ev) || (ev.State != models.EventStatePending && ev.State != models.EventStateActive) {
		return nil
	}
	if curtailBatchIntervalActive(ev) && !r.curtailBatchIntervalElapsed(ev, existingTargets) {
		return nil
	}
	if hasInFlightCurtailDispatch(existingTargets) {
		return nil
	}
	params, ok := listCandidatesParamsForEventScope(ev)
	if !ok {
		slog.Warn("curtailment reconciler: unsupported closed-loop full-fleet scope",
			"event_id", ev.ID, "scope_type", ev.ScopeType)
		return nil
	}
	params.OrgID = ev.OrgID
	candidates, err := r.store.ListCandidates(ctx, params)
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (full_fleet admission) failed",
			"event_id", ev.ID, "error", err)
		return nil
	}
	orgConfig, err := r.store.GetOrgConfig(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: get org config (full_fleet admission) failed",
			"event_id", ev.ID, "error", err)
		return nil
	}
	targets, _ := curtailment.BuildFullFleetAdmissionTargets(
		candidates,
		ev.IncludeMaintenance && ev.ForceIncludeMaintenance,
		candidateMinPowerWForEvent(ev, orgConfig.CandidateMinPowerW),
	)
	targets = excludeExistingTargetParams(targets, existingTargets)
	activeDevices, err := r.store.ListActiveCurtailmentTargetDevices(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: list active devices (full_fleet admission) failed",
			"event_id", ev.ID, "error", err)
		return nil
	}
	targets = excludeDeviceIdentifiers(targets, activeDevices)
	cooldownSec := postEventCooldownSecForEvent(ev)
	if cooldownSec > 0 {
		cooldownDevices, err := r.store.ListRecentlyResolvedCurtailedDevices(
			ctx,
			interfaces.ListRecentlyResolvedCurtailedDevicesParams{
				OrgID:             ev.OrgID,
				CooldownSec:       cooldownSec,
				DeviceIdentifiers: params.DeviceIdentifiers,
				SiteIDs:           params.SiteIDs,
			},
		)
		if err != nil {
			slog.Error("curtailment reconciler: list cooldown devices (full_fleet admission) failed",
				"event_id", ev.ID, "error", err)
			return nil
		}
		targets = excludeDeviceIdentifiers(targets, cooldownDevices)
	}
	if len(targets) == 0 {
		return nil
	}
	if batchSize := curtailBatchSizeForEvent(ev, len(targets)); len(targets) > int(batchSize) {
		targets = targets[:batchSize]
	}
	claimed, err := r.store.ClaimClosedLoopFullFleetTargets(ctx, ev.ID, ev.OrgID, cooldownSec, targets)
	if err != nil {
		slog.Error("curtailment reconciler: claim full_fleet targets failed",
			"event_id", ev.ID, "candidate_count", len(targets), "error", err)
		return nil
	}
	if len(claimed) > 0 {
		slog.Info("curtailment reconciler: claimed full_fleet targets",
			"event_id", ev.ID, "claimed", len(claimed))
	}
	return claimed
}

func hasInFlightCurtailDispatch(targets []*models.Target) bool {
	for _, target := range targets {
		if target.DesiredState == models.DesiredStateCurtailed && target.State == models.TargetStateDispatching {
			return true
		}
	}
	return false
}

func candidateMinPowerWForEvent(ev *models.Event, fallback int32) int32 {
	if ev == nil || len(ev.DecisionSnapshotJSON) == 0 {
		return fallback
	}
	var snapshot struct {
		CandidateMinPowerW int32 `json:"candidate_min_power_w"`
	}
	if err := json.Unmarshal(ev.DecisionSnapshotJSON, &snapshot); err != nil || snapshot.CandidateMinPowerW <= 0 {
		return fallback
	}
	return snapshot.CandidateMinPowerW
}

func postEventCooldownSecForEvent(ev *models.Event) int32 {
	if ev == nil || len(ev.DecisionSnapshotJSON) == 0 {
		return 0
	}
	var snapshot struct {
		PostEventCooldownSec int32 `json:"post_event_cooldown_sec"`
	}
	if err := json.Unmarshal(ev.DecisionSnapshotJSON, &snapshot); err != nil || snapshot.PostEventCooldownSec <= 0 {
		return 0
	}
	return snapshot.PostEventCooldownSec
}

func excludeExistingTargetParams(targets []models.InsertTargetParams, existingTargets []*models.Target) []models.InsertTargetParams {
	if len(targets) == 0 || len(existingTargets) == 0 {
		return targets
	}
	existing := make(map[string]struct{}, len(existingTargets))
	for _, target := range existingTargets {
		existing[target.DeviceIdentifier] = struct{}{}
	}
	filtered := targets[:0]
	for _, target := range targets {
		if _, ok := existing[target.DeviceIdentifier]; ok {
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered
}

func excludeDeviceIdentifiers(targets []models.InsertTargetParams, deviceIdentifiers []string) []models.InsertTargetParams {
	if len(targets) == 0 || len(deviceIdentifiers) == 0 {
		return targets
	}
	excluded := make(map[string]struct{}, len(deviceIdentifiers))
	for _, deviceIdentifier := range deviceIdentifiers {
		excluded[deviceIdentifier] = struct{}{}
	}
	filtered := targets[:0]
	for _, target := range targets {
		if _, ok := excluded[target.DeviceIdentifier]; ok {
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered
}

func (r *Reconciler) dispatchClaimedCurtailTargets(ctx context.Context, ev *models.Event, claimed []*models.Target) {
	if len(claimed) == 0 {
		return
	}
	_ = r.dispatchCurtailBatch(ctx, ev, claimed, models.TargetStateDispatching)
}

func isClosedLoopFullFleet(ev *models.Event) bool {
	return ev != nil && ev.Mode == models.ModeFullFleet && ev.LoopType == models.LoopTypeClosed
}

func listCandidatesParamsForEventScope(ev *models.Event) (interfaces.ListCandidatesParams, bool) {
	switch ev.ScopeType {
	case models.ScopeTypeWholeOrg, "":
		return interfaces.ListCandidatesParams{}, true
	case models.ScopeTypeSite:
		var scope struct {
			SiteID int64 `json:"site_id"`
		}
		if err := json.Unmarshal(ev.ScopeJSON, &scope); err != nil || scope.SiteID <= 0 {
			return interfaces.ListCandidatesParams{}, false
		}
		return interfaces.ListCandidatesParams{SiteIDs: []int64{scope.SiteID}}, true
	case models.ScopeTypeMixed:
		scope, hasScope, err := curtailment.ScopeFromJSON(ev.ScopeJSON)
		if err != nil || !hasScope || !curtailment.IsSiteOnlyScope(scope) {
			return interfaces.ListCandidatesParams{}, false
		}
		return interfaces.ListCandidatesParams{SiteIDs: scope.SiteIDs}, true
	case models.ScopeTypeDeviceSets, models.ScopeTypeDeviceList:
		return interfaces.ListCandidatesParams{}, false
	default:
		return interfaces.ListCandidatesParams{}, false
	}
}

// confirmOneDispatched promotes Dispatched → Confirmed when telemetry
// shows curtailment, resetting retry_count. Missing candidate row goes
// through recordDispatchFailure so a vanished device can't stall.
func (r *Reconciler) confirmOneDispatched(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate, nonTerminalState models.TargetState) {
	if c == nil {
		r.recordDispatchFailure(ctx, ev, t, "candidate row missing (device unpaired or deleted)", nonTerminalState)
		return
	}
	if !isCurtailed(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor, true) {
		if t.LastDispatchedAt != nil && r.cfg.CurtailDispatchTimeoutSec > 0 {
			timeout := time.Duration(r.cfg.CurtailDispatchTimeoutSec) * time.Second
			if r.now().Sub(*t.LastDispatchedAt) > timeout {
				slog.Info("curtailment reconciler: curtail telemetry timeout aging initiated",
					"event_id", ev.ID, "device", t.DeviceIdentifier,
					"timeout_sec", r.cfg.CurtailDispatchTimeoutSec)
				r.recordDispatchFailure(ctx, ev, t,
					"curtail telemetry timeout",
					models.TargetStateDispatching)
			}
		}
		return
	}
	now := r.now()
	zero := int32(0)
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:       models.TargetStateConfirmed,
		ConfirmedAt: &now,
		ObservedAt:  &now,
		RetryCount:  &zero,
	}
	if c.LatestPowerW != nil && isFinite(*c.LatestPowerW) {
		power := *c.LatestPowerW
		params.ObservedPowerW = &power
	}
	if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: target confirm update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
		return
	}
	t.State = models.TargetStateConfirmed
	t.ConfirmedAt = &now
	t.ObservedAt = &now
	t.RetryCount = 0
	if params.ObservedPowerW != nil {
		t.ObservedPowerW = params.ObservedPowerW
	}
}

// checkDrift evaluates a confirmed target against telemetry. Uncurtailed
// → Drifted, re-dispatch if budget remains.
func (r *Reconciler) checkDrift(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate) {
	if c == nil {
		r.recordDispatchFailure(ctx, ev, t, "candidate row missing (device unpaired or deleted)", models.TargetStateDrifted)
		return
	}
	if !isCurtailed(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor, false) {
		now := r.now()
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:      models.TargetStateDrifted,
			ObservedAt: &now,
		}
		if c.LatestPowerW != nil && isFinite(*c.LatestPowerW) {
			power := *c.LatestPowerW
			params.ObservedPowerW = &power
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: target drift update failed",
					"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			}
			return
		}
		t.State = models.TargetStateDrifted
		t.ObservedAt = &now
		if params.ObservedPowerW != nil {
			t.ObservedPowerW = params.ObservedPowerW
		}
		// Restore targets terminalize at budget; curtail targets keep retrying
		// while OFF is asserted.
		if r.retryBudgetTerminalizes(t, t.RetryCount) {
			return
		}
		r.dispatchOneCurtail(ctx, ev, t, models.TargetStateDrifted)
		return
	}
	// Still curtailed: refresh observed_power_w / observed_at as a rolling read.
	now := r.now()
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:      models.TargetStateConfirmed,
		ObservedAt: &now,
	}
	if c.LatestPowerW != nil && isFinite(*c.LatestPowerW) {
		power := *c.LatestPowerW
		params.ObservedPowerW = &power
	}
	if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: target observe update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
		return
	}
	t.ObservedAt = &now
	if params.ObservedPowerW != nil {
		t.ObservedPowerW = params.ObservedPowerW
	}
}

// maybeMarkActive flips Pending → Active once every target is Confirmed or
// terminally failed. All-failed events skip past Active to
// completed_with_failures so they can't sit indefinitely.
func (r *Reconciler) maybeMarkActive(ctx context.Context, ev *models.Event, targets []*models.Target) {
	confirmed, terminalFailures := 0, 0
	for _, t := range targets {
		switch t.State {
		case models.TargetStateConfirmed:
			confirmed++
		case models.TargetStateRestoreFailed:
			terminalFailures++
		case models.TargetStatePending, models.TargetStateDispatching,
			models.TargetStateDispatched, models.TargetStateDrifted:
			// In flight.
			return
		case models.TargetStateResolved, models.TargetStateReleased:
			// Unreachable on a pending event; hold for manual cleanup.
			return
		}
	}
	if confirmed == 0 && terminalFailures > 0 {
		// All-failed: nothing curtailed → skip Active.
		now := r.now()
		slog.Warn("curtailment reconciler: pending event has all-terminal targets; marking completed_with_failures",
			"event_id", ev.ID, "failed_target_count", terminalFailures)
		if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateCompletedWithFailures, nil, &now); err != nil {
			r.logEventStateUpdateError(ev, "pending→completed_with_failures", err)
		}
		return
	}
	now := r.now()
	if terminalFailures > 0 {
		slog.Warn("curtailment reconciler: pending→active with terminal-failed targets",
			"event_id", ev.ID, "failed_target_count", terminalFailures, "confirmed_count", confirmed)
	}
	if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateActive, &now, nil); err != nil {
		r.logEventStateUpdateError(ev, "pending→active", err)
	}
}

// logEventStateUpdateError buckets store.UpdateEventState errors:
// race-loss → Warn + IncEventStateRaceLoss; other errors → Error.
func (r *Reconciler) logEventStateUpdateError(ev *models.Event, transition string, err error) {
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		r.metrics.IncEventStateRaceLoss()
		slog.Warn("curtailment reconciler: event state advanced concurrently; skipping transition",
			"event_id", ev.ID, "event_uuid", ev.EventUUID,
			"loaded_state", ev.State, "transition", transition)
		return
	}
	slog.Error("curtailment reconciler: "+transition+" transition failed",
		"event_id", ev.ID, "error", err)
}

// writeTargetState wraps store.UpdateTargetState so race-loss routes to
// IncEventStateRaceLoss (benign concurrency) and other errors route to
// IncTargetWriteFailure (operator-actionable). Returns the error for
// site-specific logging.
func (r *Reconciler) writeTargetState(ctx context.Context, ev *models.Event, deviceID string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	if params.ExpectedEventState == nil {
		expectedState := ev.State
		params.ExpectedEventState = &expectedState
	}
	if params.ExpectedDesiredState == nil {
		if desired := expectedDesiredStateForEventState(ev.State); desired != "" {
			expectedDesired := desired
			params.ExpectedDesiredState = &expectedDesired
		}
	}
	err := r.store.UpdateTargetState(ctx, ev.ID, deviceID, params)
	if err == nil {
		return nil
	}
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		r.metrics.IncEventStateRaceLoss()
		slog.Warn("curtailment reconciler: target state advanced concurrently; skipping update",
			"event_id", ev.ID, "event_uuid", ev.EventUUID, "device", deviceID)
		return err
	}
	r.metrics.IncTargetWriteFailure()
	return err
}

func expectedDesiredStateForEventState(state models.EventState) string {
	switch state {
	case models.EventStatePending, models.EventStateActive:
		return models.DesiredStateCurtailed
	case models.EventStateRestoring:
		return models.DesiredStateActive
	case models.EventStateCompleted, models.EventStateCompletedWithFailures,
		models.EventStateCancelled, models.EventStateFailed:
		return ""
	}
	return ""
}

// reconcilerContext stamps synthetic session.Info on the dispatch ctx.
// Actor=ActorCurtailment lets CurtailmentActiveFilter recognize self-traffic;
// userID (from curtailment_event.created_by_user_id) satisfies
// command_batch_log.created_by's NOT NULL FK to user(id).
func reconcilerContext(parent context.Context, orgID int64, userID int64) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      reconcilerActorName,
		UserID:         userID,
		OrganizationID: orgID,
		ExternalUserID: reconcilerActorName,
		Username:       reconcilerActorName,
		Actor:          session.ActorCurtailment,
	})
}

func reconcilerCommandContext(parent context.Context, orgID int64, userID int64) context.Context {
	return command.WithCommandActivitySuppressed(reconcilerContext(parent, orgID, userID))
}

func batchSizeForEvent(ev *models.Event) int32 {
	batchSize := int32(1)
	if ev != nil && ev.EffectiveBatchSize != nil && *ev.EffectiveBatchSize > 0 {
		batchSize = *ev.EffectiveBatchSize
	}
	return batchSize
}

func curtailBatchSizeForEvent(ev *models.Event, targetCount int) int32 {
	if ev != nil && ev.CurtailBatchSize != nil && *ev.CurtailBatchSize > 0 {
		return *ev.CurtailBatchSize
	}
	if targetCount <= 0 {
		return 1
	}
	if targetCount > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(targetCount) //nolint:gosec // bounded above
}

func curtailBatchIntervalActive(ev *models.Event) bool {
	return ev != nil && ev.CurtailBatchSize != nil && ev.CurtailBatchIntervalSec > 0
}

func hasTargetsInState(targets []*models.Target, state models.TargetState) bool {
	for _, t := range targets {
		if t.State == state {
			return true
		}
	}
	return false
}

func (r *Reconciler) curtailBatchIntervalElapsed(ev *models.Event, targets []*models.Target) bool {
	if !curtailBatchIntervalActive(ev) {
		return true
	}
	interval := time.Duration(ev.CurtailBatchIntervalSec) * time.Second
	var newest *time.Time
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if t.CurtailPhase.DispatchedAt == nil {
			continue
		}
		if newest == nil || t.CurtailPhase.DispatchedAt.After(*newest) {
			ts := *t.CurtailPhase.DispatchedAt
			newest = &ts
		}
	}
	return newest == nil || r.now().Sub(*newest) >= interval
}

// isCurtailed decides whether telemetry shows the target is curtailed.
// Power-vs-baseline ranks above hash_rate; missing baseline falls back
// to hash. requirePositiveEvidence=true is the confirm path (no sample
// → not curtailed); false is the drift path (missing sample preserves
// curtailed so a flaky sensor doesn't restorm).
func isCurtailed(latestPowerW *float64, baselinePowerW *float64, latestHashRateHS *float64, driftThresholdFactor float64, requirePositiveEvidence bool) bool {
	if latestPowerW == nil || !isFinite(*latestPowerW) {
		if requirePositiveEvidence {
			return false
		}
		// Drift path: zero/missing hash → still curtailed; positive hash → resumed.
		if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
			return true
		}
		return *latestHashRateHS <= 0
	}
	if baselinePowerW != nil && isFinite(*baselinePowerW) && *baselinePowerW > 0 {
		threshold := *baselinePowerW * driftThresholdFactor
		return *latestPowerW <= threshold
	}
	// Baseline missing: hash-only fallback.
	if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
		return !requirePositiveEvidence
	}
	return *latestHashRateHS <= 0
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// enforceMaxDuration transitions an active event to restoring when the
// max_duration_seconds cap elapses since started_at. Returns true so the
// caller skips further active-phase work this tick. AllowUnbounded events
// and events without started_at short-circuit out.
func (r *Reconciler) enforceMaxDuration(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	if ev.AllowUnbounded || ev.MaxDurationSeconds == nil || *ev.MaxDurationSeconds <= 0 {
		return false
	}
	if ev.StartedAt == nil {
		return false
	}
	maxDur := time.Duration(*ev.MaxDurationSeconds) * time.Second
	elapsed := r.now().Sub(*ev.StartedAt)
	if elapsed < maxDur {
		return false
	}

	if _, err := r.store.BeginRestoreTransition(ctx, ev.OrgID, ev.EventUUID, interfaces.BeginRestoreTransitionParams{}); err != nil {
		slog.Error("curtailment reconciler: max_duration→restoring transition failed",
			"event_id", ev.ID, "max_duration_seconds", *ev.MaxDurationSeconds,
			"elapsed_seconds", int64(elapsed.Seconds()), "error", err)
		// Skip drift dispatch this tick — re-curtailing past the cap is wrong.
		return true
	}
	slog.Info("curtailment reconciler: max_duration elapsed → forced restore",
		"event_id", ev.ID, "event_uuid", ev.EventUUID,
		"max_duration_seconds", *ev.MaxDurationSeconds,
		"elapsed_seconds", int64(elapsed.Seconds()))
	return true
}

// observeRestoring drives a restoring event toward terminal. Per tick:
// confirm dispatched restores via telemetry; flip terminal when all
// targets are terminal; claim the next batch behind the in-flight +
// interval gates (inrush/thermal-shock protection).
func (r *Reconciler) observeRestoring(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets (restoring) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if len(targets) == 0 {
		// Contract violation; BeginRestoreTransition needs targets.
		slog.Error("curtailment reconciler: restoring event has no targets",
			"event_id", ev.ID, "event_uuid", ev.EventUUID)
		return
	}

	r.confirmDispatchedRestores(ctx, ev, targets)
	if r.maybeCompleteRestoring(ctx, ev, targets) {
		return
	}
	r.maybeClaimRestoreBatch(ctx, ev, targets)
}

// confirmDispatchedRestores promotes restore-phase Dispatched targets to
// Resolved when telemetry shows mining has resumed.
func (r *Reconciler) confirmDispatchedRestores(ctx context.Context, ev *models.Event, targets []*models.Target) {
	deviceIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.DesiredState == models.DesiredStateActive && t.State == models.TargetStateDispatched {
			deviceIDs = append(deviceIDs, t.DeviceIdentifier)
		}
	}
	if len(deviceIDs) == 0 {
		return
	}
	cands, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
		OrgID:             ev.OrgID,
		DeviceIdentifiers: deviceIDs,
	})
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (restore confirm) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	candByID := candidatesByDeviceID(cands)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive || t.State != models.TargetStateDispatched {
			continue
		}
		r.confirmOneRestore(ctx, ev, t, candByID[t.DeviceIdentifier])
	}
}

// confirmOneRestore promotes Dispatched → Resolved when telemetry shows
// the miner is back above the restore threshold; mirrors
// confirmOneDispatched. Vanished devices burn retry budget to keep
// progress.
func (r *Reconciler) confirmOneRestore(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate) {
	if c == nil {
		r.recordDispatchFailure(ctx, ev, t,
			"candidate row missing (device unpaired or deleted)",
			models.TargetStatePending)
		return
	}
	if !isRestored(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor) {
		// Age out targets whose telemetry never confirms so a stale
		// candidate can't pin the event in restoring forever.
		if t.LastDispatchedAt != nil && r.cfg.RestoreDispatchTimeoutSec > 0 {
			timeout := time.Duration(r.cfg.RestoreDispatchTimeoutSec) * time.Second
			if r.now().Sub(*t.LastDispatchedAt) > timeout {
				slog.Info("curtailment reconciler: restore telemetry timeout aging initiated",
					"event_id", ev.ID, "device", t.DeviceIdentifier,
					"timeout_sec", r.cfg.RestoreDispatchTimeoutSec)
				r.recordDispatchFailure(ctx, ev, t,
					"restore telemetry timeout",
					models.TargetStatePending)
			}
		}
		return
	}
	now := r.now()
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:       models.TargetStateResolved,
		ConfirmedAt: &now,
		ObservedAt:  &now,
	}
	if c.LatestPowerW != nil && isFinite(*c.LatestPowerW) {
		power := *c.LatestPowerW
		params.ObservedPowerW = &power
	}
	if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: restore confirm update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
		return
	}
	t.State = models.TargetStateResolved
	t.ConfirmedAt = &now
	t.ObservedAt = &now
	if params.ObservedPowerW != nil {
		t.ObservedPowerW = params.ObservedPowerW
	}
}

// maybeCompleteRestoring transitions the event terminal once every target
// is in a terminal state. Returns true when the transition was attempted so
// the caller skips further work this tick.
func (r *Reconciler) maybeCompleteRestoring(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	successful, failed := 0, 0
	for _, t := range targets {
		switch t.State { //nolint:exhaustive // default arm is load-bearing: a future schema-added state must stay non-terminal until it ships its handling, not be silently swept into "complete." TestReconciler_Restoring_UnknownTargetStateKeepsEventNonTerminal pins the contract.
		case models.TargetStateResolved, models.TargetStateReleased:
			successful++
		case models.TargetStateRestoreFailed:
			failed++
		case models.TargetStatePending, models.TargetStateDispatching,
			models.TargetStateDispatched, models.TargetStateDrifted,
			models.TargetStateConfirmed:
			return false
		default:
			// Unknown future state: don't sweep prematurely.
			return false
		}
	}
	finalState := models.EventStateCompleted
	if failed > 0 {
		finalState = models.EventStateCompletedWithFailures
	}
	now := r.now()
	if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, finalState, nil, &now); err != nil {
		r.logEventStateUpdateError(ev, "restoring→"+string(finalState), err)
		return true
	}
	slog.Info("curtailment reconciler: event terminal",
		"event_id", ev.ID, "event_uuid", ev.EventUUID,
		"final_state", finalState, "successful", successful, "failed", failed)
	return true
}

// maybeClaimRestoreBatch enforces the in-flight + interval gates, then
// claims up to EffectiveBatchSize pending restore targets and dispatches
// one Uncurtail covering the batch.
func (r *Reconciler) maybeClaimRestoreBatch(ctx context.Context, ev *models.Event, targets []*models.Target) {
	// Gate 1: no in-flight restore batch.
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive {
			continue
		}
		if t.State == models.TargetStateDispatched || t.State == models.TargetStateDrifted {
			return
		}
	}

	// Gate 2: inter-batch interval elapsed. 0 = no wait.
	intervalSec := ev.RestoreBatchIntervalSec
	if intervalSec < 0 {
		intervalSec = 0
	}
	if intervalSec > 0 {
		interval := time.Duration(intervalSec) * time.Second
		var newest *time.Time
		for _, t := range targets {
			if t.DesiredState != models.DesiredStateActive {
				continue
			}
			// Terminal targets retain LastDispatchedAt and serve as the spacing reference.
			if t.LastDispatchedAt == nil {
				continue
			}
			if newest == nil || t.LastDispatchedAt.After(*newest) {
				ts := *t.LastDispatchedAt
				newest = &ts
			}
		}
		if newest != nil && r.now().Sub(*newest) < interval {
			return
		}
	}

	// Service.Start stamped this via ComputeEffectiveBatchSize; floor at 1
	// against a missing column.
	batchSize := int32(1)
	if ev.EffectiveBatchSize != nil && *ev.EffectiveBatchSize > 0 {
		batchSize = *ev.EffectiveBatchSize
	}

	// First pass: redispatch any DISPATCHING orphans from an interrupted
	// prior tick. Uncurtail is device-idempotent, so re-sending the
	// command is safe. Orphan recovery consumes this tick's batch slot
	// on its own — mixing orphans with fresh PENDING would double the
	// inrush and bypass the one-batch-per-interval throttle.
	orphans := make([]*models.Target, 0, batchSize)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive {
			continue
		}
		if t.State != models.TargetStateDispatching {
			continue
		}
		orphans = append(orphans, t)
		if int32(len(orphans)) >= batchSize { //nolint:gosec // batchSize already bounded
			break
		}
	}
	if len(orphans) > 0 {
		r.dispatchRestoreBatch(ctx, ev, orphans)
		return
	}

	// Second pass: claim fresh PENDING up to batchSize.
	claim := make([]*models.Target, 0, batchSize)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive {
			continue
		}
		if t.State != models.TargetStatePending {
			continue
		}
		claim = append(claim, t)
		if int32(len(claim)) >= batchSize { //nolint:gosec // batchSize already bounded
			break
		}
	}
	if len(claim) == 0 {
		return
	}

	r.dispatchRestoreBatch(ctx, ev, claim)
}

// dispatchRestoreBatch issues one Uncurtail for every device in the batch
// and per-device commits transitions from the dispatched/skipped split.
// DISPATCHING pre-writes provide the same race-closure as dispatchOneCurtail;
// per-target pre-write failures burn one retry slot via recordDispatchFailure
// so persistent failures escalate to RESTORE_FAILED instead of cycling.
func (r *Reconciler) dispatchRestoreBatch(ctx context.Context, ev *models.Event, claim []*models.Target) {
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}
	dispatchSet := make([]*models.Target, 0, len(claim))
	for _, t := range claim {
		dispatchingParams := interfaces.UpdateCurtailmentTargetStateParams{
			State: models.TargetStateDispatching,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, dispatchingParams); err != nil {
			if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				return
			}
			slog.Error("curtailment reconciler: restore dispatching pre-write failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			r.recordDispatchFailure(ctx, ev, t, err.Error(), models.TargetStatePending)
			continue
		}
		t.State = models.TargetStateDispatching
		dispatchSet = append(dispatchSet, t)
	}
	if len(dispatchSet) == 0 {
		return
	}
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}

	deviceIDs := make([]string, 0, len(dispatchSet))
	for _, t := range dispatchSet {
		deviceIDs = append(deviceIDs, t.DeviceIdentifier)
	}
	cmdCtx := reconcilerCommandContext(ctx, ev.OrgID, ev.CreatedByUserID)
	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: deviceIDs,
			},
		},
	}
	result, dispatchErr := r.cmd.Uncurtail(cmdCtx, selector)
	if dispatchErr != nil {
		errMsg := dispatchErr.Error()
		slog.Error("curtailment reconciler: restore batch dispatch failed",
			"event_id", ev.ID, "batch_size", len(dispatchSet), "error", dispatchErr)
		for _, t := range dispatchSet {
			r.recordDispatchFailure(ctx, ev, t, errMsg, models.TargetStatePending)
		}
		return
	}

	// Empty BatchIdentifier = no device resolved through the queue.
	if result == nil || result.BatchIdentifier == "" {
		const reason = "uncurtail command produced no batch (no live devices to dispatch)"
		slog.Warn("curtailment reconciler: restore batch produced empty result",
			"event_id", ev.ID, "batch_size", len(dispatchSet))
		for _, t := range dispatchSet {
			r.recordDispatchFailure(ctx, ev, t, reason, models.TargetStatePending)
		}
		return
	}

	skippedSet := make(map[string]string, len(result.Skipped))
	for _, s := range result.Skipped {
		skippedSet[s.DeviceIdentifier] = skippedDeviceReason(s)
	}
	dispatchedSet := make(map[string]struct{}, len(result.DispatchedDeviceIdentifiers))
	for _, deviceID := range result.DispatchedDeviceIdentifiers {
		dispatchedSet[deviceID] = struct{}{}
	}

	now := r.now()
	batchID := result.BatchIdentifier
	for _, t := range dispatchSet {
		if reason, skipped := skippedSet[t.DeviceIdentifier]; skipped {
			slog.Warn("curtailment reconciler: restore device filter-skipped",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "reason", reason)
			r.recordDispatchFailure(ctx, ev, t, reason, models.TargetStatePending)
			continue
		}
		if _, dispatched := dispatchedSet[t.DeviceIdentifier]; !dispatched {
			const reason = "uncurtail command did not enqueue device"
			slog.Warn("curtailment reconciler: restore device not dispatched",
				"event_id", ev.ID, "device", t.DeviceIdentifier)
			r.recordDispatchFailure(ctx, ev, t, reason, models.TargetStatePending)
			continue
		}
		emptyErr := ""
		// Symmetric to the Curtail-phase post-cmd write above.
		desiredActive := models.DesiredStateActive
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:                models.TargetStateDispatched,
			LastDispatchedAt:     &now,
			LastBatchUUID:        &batchID,
			LastError:            &emptyErr,
			ExpectedDesiredState: &desiredActive,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: restore dispatch state update failed",
					"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
				r.recordDispatchFailure(ctx, ev, t, err.Error(), models.TargetStatePending)
			}
			continue
		}
		t.State = models.TargetStateDispatched
		t.LastDispatchedAt = &now
		t.LastBatchUUID = &batchID
		t.LastError = nil
	}
}

func (r *Reconciler) eventStillDispatchable(ctx context.Context, ev *models.Event) bool {
	latest, err := r.store.GetEventByUUID(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: event liveness check failed",
			"event_id", ev.ID, "event_uuid", ev.EventUUID, "error", err)
		return false
	}
	if latest == nil {
		slog.Warn("curtailment reconciler: event missing before dispatch",
			"event_id", ev.ID, "event_uuid", ev.EventUUID)
		return false
	}
	if latest.State.IsTerminal() || latest.State != ev.State {
		slog.Info("curtailment reconciler: skipping dispatch after event state changed",
			"event_id", ev.ID, "event_uuid", ev.EventUUID,
			"loaded_state", ev.State, "current_state", latest.State)
		return false
	}
	return true
}

// isRestored returns true when telemetry shows the target has resumed
// mining. Requires positive evidence — missing samples return false so
// a flaky sensor doesn't trigger a premature Resolved. The strict > vs
// isCurtailed's <= leaves a no-progress band at exactly baseline×factor.
func isRestored(latestPowerW *float64, baselinePowerW *float64, latestHashRateHS *float64, restoreThresholdFactor float64) bool {
	if latestPowerW != nil && isFinite(*latestPowerW) {
		if baselinePowerW != nil && isFinite(*baselinePowerW) && *baselinePowerW > 0 {
			return *latestPowerW > *baselinePowerW*restoreThresholdFactor
		}
	}
	// Baseline missing / power stale: hash-only fallback.
	if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
		return false
	}
	return *latestHashRateHS > 0
}
