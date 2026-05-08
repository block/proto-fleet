// Package reconciler drives non-terminal curtailment events forward: it
// dispatches Curtail commands for pending targets, watches telemetry for
// drift on confirmed targets, and retries within a bounded budget.
package reconciler

import (
	"context"
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
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// reconcilerActorName is the synthetic principal used in dispatch ctx.
	reconcilerActorName = "curtailment-reconciler"

	// defaultTickInterval matches the design doc's 30s reconciler cadence.
	defaultTickInterval = 30 * time.Second

	// defaultShutdownDeadline bounds Stop()'s wait for the in-flight tick.
	defaultShutdownDeadline = 10 * time.Second

	// defaultMaxRetries caps per-target re-dispatch attempts. Crossing the
	// cap leaves the target in a terminal state (drifted at the budget
	// boundary, or restore_failed when dispatch itself never landed).
	defaultMaxRetries int32 = 3

	// defaultDriftThresholdFactor: a confirmed target that reports
	// power_w > baseline_power_w * factor is considered drifted (the
	// miner has restored mining). 0.5 catches partial-restore as well as
	// full-restore cases.
	defaultDriftThresholdFactor = 0.5
)

// CommandDispatcher is the subset of command.Service the reconciler needs.
// The interface keeps unit tests free of the full command service graph.
type CommandDispatcher interface {
	Curtail(ctx context.Context, selector *pb.DeviceSelector, level sdk.CurtailLevel) (*command.CommandResult, error)
	Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error)
}

// Config carries the runtime tunables. Zero-valued fields fall back to the
// defaults; fleetd may override them via its config layer.
type Config struct {
	TickInterval         time.Duration
	ShutdownDeadline     time.Duration
	MaxRetries           int32
	DriftThresholdFactor float64
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
	if c.DriftThresholdFactor <= 0 {
		c.DriftThresholdFactor = defaultDriftThresholdFactor
	}
	return c
}

// Reconciler is a singleton goroutine that ticks every config.TickInterval.
// The tick is serial: it reads all non-terminal events, dispatches/observes
// per event with per-event panic isolation, then upserts the heartbeat.
type Reconciler struct {
	cfg   Config
	store interfaces.CurtailmentStore
	cmd   CommandDispatcher
	now   func() time.Time

	stopCancel context.CancelFunc
	workCancel context.CancelFunc
	wg         sync.WaitGroup

	mu      sync.Mutex
	running bool
}

// New builds a Reconciler with the given dependencies. nil store / dispatcher
// is rejected at Start time, not here, so a misconfigured fleetd surfaces
// during the lifecycle bring-up.
func New(cfg Config, store interfaces.CurtailmentStore, cmd CommandDispatcher) *Reconciler {
	return &Reconciler{
		cfg:   cfg.withDefaults(),
		store: store,
		cmd:   cmd,
		now:   time.Now,
	}
}

// Start spins up the tick loop. Returns an error if dependencies are missing.
// Repeat calls without an intervening Stop are no-ops so a misbehaving
// lifecycle wiring cannot fork two reconcilers against the same store.
func (r *Reconciler) Start(_ context.Context) error {
	if r.store == nil {
		return fmt.Errorf("curtailment reconciler: store is required")
	}
	if r.cmd == nil {
		return fmt.Errorf("curtailment reconciler: command dispatcher is required")
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

// Stop signals the tick loop to exit and waits up to ShutdownDeadline for
// the in-flight tick to drain. Late ticks see workCtx canceled and bail out.
//
// running flips to false under the mutex *before* wg.Wait so a concurrent
// second Stop sees `running == false` at the guard and returns immediately
// instead of falling through to a duplicate wg.Wait.
//
// Known concurrency edge: a Start arriving in the window between the
// mu.Unlock above and the goroutine's wg.Done can observe running=false,
// install fresh stopCancel/workCancel, and add a second goroutine to the
// same WaitGroup. Stop's wg.Wait would then return after only the first
// goroutine drains, leaving the second live. fleetd's lifecycle calls
// Start once at startup and Stop once at shutdown, so this is unreachable
// in practice. Adding a `stopping` state guard is the fix if the lifecycle
// ever grows a restart path.
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

// safeTick wraps runTick in a defer/recover so a panic outside the per-event
// loop (e.g. ListNonTerminalEvents, heartbeat upsert) does not tear down the
// goroutine. The next tick continues normally.
func (r *Reconciler) safeTick(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("curtailment reconciler: recovered panic in tick", "panic", rec)
		}
	}()
	r.runTick(ctx)
}

// runTick is one reconciliation pass. Errors at the per-event boundary are
// logged and isolated; the heartbeat upsert happens regardless so a single
// bad event cannot blind the liveness alert.
func (r *Reconciler) runTick(ctx context.Context) {
	tickStart := r.now()
	tickUUID := uuid.New()
	events, err := r.store.ListNonTerminalEvents(ctx)
	if err != nil {
		slog.Error("curtailment reconciler: failed to list non-terminal events", "error", err)
		// Heartbeat still updates with active_event_count=0 so liveness alerts
		// continue to fire-or-not based on tick freshness, not query health.
		r.upsertHeartbeat(ctx, tickStart, tickUUID, 0)
		return
	}

	for _, ev := range events {
		r.processEvent(ctx, ev)
	}

	r.upsertHeartbeat(ctx, tickStart, tickUUID, int32(len(events))) //nolint:gosec // bounded by org event count
}

func (r *Reconciler) upsertHeartbeat(_ context.Context, tickStart time.Time, tickUUID uuid.UUID, activeCount int32) {
	durationMS := int32(r.now().Sub(tickStart).Milliseconds()) //nolint:gosec // tick durations fit in int32 well past pathological cases
	// Detach from workCtx so the shutdown-watchdog cancellation cannot drop
	// the final heartbeat — liveness alerts must see the last completed tick
	// even when the process is winding down. A short bound keeps a stuck DB
	// from blocking shutdown.
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

// processEvent dispatches per-state work for a single non-terminal event.
// The defer/recover here is load-bearing for per-event isolation: a panic
// in one event must not abort processing of the remaining events in the
// same tick. safeTick's outer recover is a backstop for tick-level infra
// (ListNonTerminalEvents, heartbeat upsert), not per-event isolation.
func (r *Reconciler) processEvent(ctx context.Context, ev *models.Event) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("curtailment reconciler: recovered panic processing event",
				"event_id", ev.ID, "event_uuid", ev.EventUUID, "panic", rec)
		}
	}()
	switch ev.State { //nolint:exhaustive // Terminal states (completed/cancelled/failed/...) are filtered upstream by ListNonTerminalEvents; default arm logs if one slips through.
	case models.EventStatePending:
		r.dispatchPending(ctx, ev)
	case models.EventStateActive:
		r.observeActive(ctx, ev)
	case models.EventStateRestoring:
		// The restorer owns the restoring path. Pending/active reconciliation
		// does not write here; touching restoring rows would race it.
	default:
		slog.Warn("curtailment reconciler: unexpected event state",
			"event_id", ev.ID, "state", ev.State)
	}
}

// dispatchPending dispatches Curtail per pending target, then confirms any
// dispatched targets whose telemetry already shows curtailment. The event
// flips to `active` once every target has reached confirmed (or has been
// terminally abandoned via the retry-budget exhaustion path).
//
// Targets are read once per event per tick; the three phases mutate the
// in-memory slice so downstream phases see the latest local state without
// a second ListTargetsByEvent round-trip.
func (r *Reconciler) dispatchPending(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if len(targets) == 0 {
		// Service.Start rejects empty plans; an event with zero targets is a
		// contract violation. Log and skip — manual operator intervention is
		// the recovery path.
		slog.Error("curtailment reconciler: pending event has no targets",
			"event_id", ev.ID, "event_uuid", ev.EventUUID)
		return
	}

	cmdCtx := reconcilerContext(ctx, ev.OrgID, ev.CreatedByUserID)
	for _, t := range targets {
		if t.State != models.TargetStatePending {
			continue
		}
		r.dispatchOneCurtail(cmdCtx, ev, t, models.TargetStatePending)
	}

	// Confirm any already-dispatched targets via the latest telemetry sample
	// before deciding whether the event itself can flip to active.
	r.confirmDispatched(ctx, ev, targets)
	r.maybeMarkActive(ctx, ev, targets)
}

// confirmDispatched walks the event's targets and promotes dispatched →
// confirmed when telemetry shows the device is curtailed. Pending and
// drifted rows are unaffected here. Targets is the shared per-tick slice;
// confirmation updates are mirrored back onto the slice. Per-target work
// delegates to confirmOneDispatched so this loop and observeActive's
// re-entry path share a single confirmation primitive.
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
	cands, err := r.store.ListCandidates(ctx, ev.OrgID, deviceIDs)
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (confirm) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	candByID := make(map[string]*models.Candidate, len(cands))
	for _, c := range cands {
		candByID[c.DeviceIdentifier] = c
	}
	for _, t := range targets {
		if t.State != models.TargetStateDispatched {
			continue
		}
		r.confirmOneDispatched(ctx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
	}
}

// dispatchOneCurtail issues one Curtail command for a single target and
// records the dispatch outcome on the row. nonTerminalFailureState is the
// state the target should fall back to when the dispatch fails but retry
// budget remains — pending callers pass TargetStatePending so the next
// tick's dispatchPending picks it up; drifted callers pass TargetStateDrifted
// so observeActive's drift arm picks it up.
//
// Filter-skip handling: when result.Skipped contains the target's identifier
// the command never enqueued, so promoting to dispatched would drop the work
// silently. Treat as a failed dispatch and surface the skip reason on the
// row so the next tick can retry (or exhaust the budget).
//
// On success the row clears LastError so a transient miss does not shadow
// the resolved state in the UI.
//
// Restart-safety gap: dispatch is enqueued before UpdateTargetState writes
// the dispatched-state row. If the process crashes between the two, the
// command is in-flight while the target stays pending and the next tick
// will redispatch — a duplicate Curtail is safe (idempotent) but the audit
// shows two batches. A two-phase target-state-then-dispatch design is
// deferred to a follow-up commit.
func (r *Reconciler) dispatchOneCurtail(ctx context.Context, ev *models.Event, t *models.Target, nonTerminalFailureState models.TargetState) {
	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: []string{t.DeviceIdentifier},
			},
		},
	}
	result, dispatchErr := r.cmd.Curtail(ctx, selector, sdk.CurtailLevelFull)
	if dispatchErr != nil {
		errMsg := dispatchErr.Error()
		slog.Error("curtailment reconciler: dispatch failed",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "error", dispatchErr)
		r.recordDispatchFailure(ctx, ev, t, errMsg, nonTerminalFailureState)
		return
	}
	if skipReason, skipped := skipReasonForDevice(result, t.DeviceIdentifier); skipped {
		slog.Warn("curtailment reconciler: dispatch filter-skipped",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "reason", skipReason)
		r.recordDispatchFailure(ctx, ev, t, skipReason, nonTerminalFailureState)
		return
	}
	// processCommand can return nil-error with empty BatchIdentifier when no
	// device IDs resolved (e.g. miner unpaired or deleted between Start and
	// reconcile). No batch was enqueued, so the target must NOT be marked
	// dispatched — treat as a failed dispatch attempt that consumes a retry.
	if result == nil || result.BatchIdentifier == "" {
		const reason = "command produced no batch (no live devices to dispatch)"
		slog.Warn("curtailment reconciler: dispatch produced empty batch",
			"event_id", ev.ID, "device", t.DeviceIdentifier)
		r.recordDispatchFailure(ctx, ev, t, reason, nonTerminalFailureState)
		return
	}

	now := r.now()
	emptyErr := ""
	batchID := result.BatchIdentifier
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:            models.TargetStateDispatched,
		LastDispatchedAt: &now,
		LastError:        &emptyErr,
		LastBatchUUID:    &batchID,
	}
	if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
		slog.Error("curtailment reconciler: target dispatch update failed",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		return
	}
	// Mutate the in-memory row so downstream phases in this same tick see the
	// new state without a second ListTargetsByEvent fetch.
	t.State = models.TargetStateDispatched
	t.LastDispatchedAt = &now
	t.LastError = nil
	t.LastBatchUUID = &batchID
}

// recordDispatchFailure increments the target's retry counter and either
// keeps it in nonTerminalFailureState (still has budget) or transitions it
// to RestoreFailed once the budget is exhausted. The terminal transition
// lets the event proceed to active even though this target never confirmed.
//
// Callers pass the state the target should fall back to on a non-terminal
// failure: pending dispatches stay pending; drift redispatches stay drifted
// so observeActive's drift arm picks them up next tick.
func (r *Reconciler) recordDispatchFailure(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState) {
	newRetry := t.RetryCount + 1
	state := nonTerminalFailureState
	if newRetry >= r.cfg.MaxRetries {
		state = models.TargetStateRestoreFailed
	}
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:      state,
		LastError:  &errMsg,
		RetryCount: &newRetry,
	}
	if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
		slog.Error("curtailment reconciler: target update after dispatch failure failed",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		return
	}
	// Mirror the persisted state on the in-memory row for the rest of the tick.
	t.State = state
	t.RetryCount = newRetry
	t.LastError = &errMsg
}

// skipReasonForDevice extracts the filter-skip reason for deviceID from a
// CommandResult. Returns ("", false) when the device was not skipped.
func skipReasonForDevice(result *command.CommandResult, deviceID string) (string, bool) {
	if result == nil {
		return "", false
	}
	for _, s := range result.Skipped {
		if s.DeviceIdentifier == deviceID {
			if s.Reason != "" {
				return s.Reason, true
			}
			if s.FilterName != "" {
				return "filtered by " + s.FilterName, true
			}
			return "filtered by command preflight", true
		}
	}
	return "", false
}

// observeActive walks the event's targets, computing drift against the
// latest telemetry sample and re-dispatching up to MaxRetries.
//
// Telemetry trade-off: ListCandidates pulls more columns than the drift
// check needs (avg_efficiency, pairing status, ...). We accept that cost
// for now to avoid a parallel sqlc query; the per-tick fanout is small
// (a handful of events times a few hundred targets at the v1 cap).
func (r *Reconciler) observeActive(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets (active) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if len(targets) == 0 {
		return
	}

	deviceIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		deviceIDs = append(deviceIDs, t.DeviceIdentifier)
	}
	cands, err := r.store.ListCandidates(ctx, ev.OrgID, deviceIDs)
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (drift) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	candByID := make(map[string]*models.Candidate, len(cands))
	for _, c := range cands {
		candByID[c.DeviceIdentifier] = c
	}

	cmdCtx := reconcilerContext(ctx, ev.OrgID, ev.CreatedByUserID)
	for _, t := range targets {
		switch t.State {
		case models.TargetStateConfirmed:
			r.checkDrift(cmdCtx, ev, t, candByID[t.DeviceIdentifier])
		case models.TargetStateDispatched:
			// Re-entry path: a target that drifted then re-dispatched is
			// waiting on confirmation telemetry. Run the same confirm logic
			// the pending phase uses; on success the next tick checks drift.
			r.confirmOneDispatched(cmdCtx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
		case models.TargetStateDrifted:
			// Re-dispatch unless the retry budget is exhausted. The dispatch
			// path bumps retry_count only on failure, so a successful
			// redispatch consumes the same budget slot as the eventual
			// confirmation. The `>= MaxRetries` check is a defensive backstop:
			// recordDispatchFailure routes drifted-budget-exhausted targets to
			// RestoreFailed at the boundary, so a TargetStateDrifted row with
			// RetryCount>=MaxRetries should not occur in normal operation —
			// only after a failed UpdateTargetState write.
			if t.RetryCount >= r.cfg.MaxRetries {
				continue
			}
			r.dispatchOneCurtail(cmdCtx, ev, t, models.TargetStateDrifted)
		case models.TargetStatePending, models.TargetStateResolved,
			models.TargetStateReleased, models.TargetStateRestoreFailed:
			// Pending: shouldn't appear on an active event, leave alone.
			// Resolved / Released / RestoreFailed: terminal — restorer owns.
		}
	}
}

// confirmOneDispatched is the per-target confirm path used both in the
// dispatchPending phase (via confirmDispatched) and on observeActive's
// drift-then-redispatch re-entry. Promotes the target to confirmed when
// telemetry shows curtailment; resets retry_count on confirmation.
//
// nonTerminalState is where the target lands when the candidate row is
// missing (device unpaired/deleted after dispatch). recordDispatchFailure
// consumes a retry slot and routes to RestoreFailed at budget exhaustion,
// so a vanished device cannot stall the event indefinitely.
func (r *Reconciler) confirmOneDispatched(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate, nonTerminalState models.TargetState) {
	if c == nil {
		r.recordDispatchFailure(ctx, ev, t, "candidate row missing (device unpaired or deleted)", nonTerminalState)
		return
	}
	if !isCurtailed(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor, true) {
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
	if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
		slog.Error("curtailment reconciler: target confirm update failed",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
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

// checkDrift evaluates a confirmed target against the latest telemetry. If
// the device looks uncurtailed, transition to `drifted` and re-dispatch when
// budget remains. Drift detection itself is just a state change; the retry
// budget represents *dispatch attempts*, so the bump happens inside
// dispatchOneCurtail's failure path, not here.
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
		if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
			slog.Error("curtailment reconciler: target drift update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			return
		}
		t.State = models.TargetStateDrifted
		t.ObservedAt = &now
		if params.ObservedPowerW != nil {
			t.ObservedPowerW = params.ObservedPowerW
		}
		// Stay drifted (no re-dispatch) once the budget is exhausted; matches
		// observeActive's drift arm so detection and re-entry agree.
		if t.RetryCount >= r.cfg.MaxRetries {
			return
		}
		r.dispatchOneCurtail(ctx, ev, t, models.TargetStateDrifted)
		return
	}
	// Still curtailed; refresh observed_power_w / observed_at as a
	// continuously updated rolling read.
	now := r.now()
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:      models.TargetStateConfirmed,
		ObservedAt: &now,
	}
	if c.LatestPowerW != nil && isFinite(*c.LatestPowerW) {
		power := *c.LatestPowerW
		params.ObservedPowerW = &power
	}
	if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
		slog.Error("curtailment reconciler: target observe update failed",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		return
	}
	t.ObservedAt = &now
	if params.ObservedPowerW != nil {
		t.ObservedPowerW = params.ObservedPowerW
	}
}

// maybeMarkActive flips the event from pending to active once every target
// has reached a confirmed or terminal-failure state. Targets stuck in
// dispatched / pending (still waiting for telemetry or retry budget) keep
// the event in pending; the next tick re-evaluates after a fresh sample.
//
// When every target is in a terminal-failure state, transition the event to
// completed_with_failures rather than letting it sit indefinitely.
func (r *Reconciler) maybeMarkActive(ctx context.Context, ev *models.Event, targets []*models.Target) {
	confirmed, terminalFailures := 0, 0
	for _, t := range targets {
		switch t.State {
		case models.TargetStateConfirmed:
			confirmed++
		case models.TargetStateRestoreFailed:
			terminalFailures++
		case models.TargetStatePending, models.TargetStateDispatched,
			models.TargetStateDrifted:
			// Still in flight; hold the event in pending until the next tick.
			return
		case models.TargetStateResolved, models.TargetStateReleased:
			// Not reachable on a pending event, but list explicitly for
			// exhaustiveness — a row in either state means the restorer
			// already ran, which would be a contract violation here. Hold
			// pending so a manual cleanup is the only escape.
			return
		}
	}
	if confirmed == 0 && terminalFailures > 0 {
		// Every target failed permanently; nothing curtailed, nothing to
		// restore. Skip past active and land directly on the failure
		// terminal so the event doesn't stay non-terminal forever.
		now := r.now()
		slog.Warn("curtailment reconciler: pending event has all-terminal targets; marking completed_with_failures",
			"event_id", ev.ID, "failed_target_count", terminalFailures)
		if err := r.store.UpdateEventState(ctx, ev.ID, models.EventStateCompletedWithFailures, nil, &now); err != nil {
			slog.Error("curtailment reconciler: pending→completed_with_failures transition failed",
				"event_id", ev.ID, "error", err)
		}
		return
	}
	now := r.now()
	if terminalFailures > 0 {
		slog.Warn("curtailment reconciler: pending→active with terminal-failed targets",
			"event_id", ev.ID, "failed_target_count", terminalFailures, "confirmed_count", confirmed)
	}
	if err := r.store.UpdateEventState(ctx, ev.ID, models.EventStateActive, &now, nil); err != nil {
		slog.Error("curtailment reconciler: pending→active transition failed",
			"event_id", ev.ID, "error", err)
	}
}

// reconcilerContext stamps a synthetic session.Info on the dispatch ctx so
// command preflight (CurtailmentActiveFilter) recognizes our self-traffic.
// userID is the operator captured at Start time (curtailment_event.created_by_user_id);
// it satisfies command_batch_log.created_by's NOT NULL FK to user(id).
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

// isCurtailed decides whether telemetry indicates the target is curtailed.
// requirePositiveEvidence flips the missing-sample policy:
//   - true (confirmation path): missing/non-finite power returns false outright
//     so a `dispatched` target is not promoted to `confirmed` without
//     positive evidence the device actually went down. Missing/non-finite
//     hash also returns false when baseline is absent (no usable signal).
//   - false (drift detection path): missing/non-finite samples preserve
//     curtailed=true so a transient bad sensor reading does not trigger a
//     redispatch storm.
//
// Power vs. baseline ranks above hash_rate; missing baseline falls back to
// hash_rate alone (positive hash = mining resumed = not curtailed).
func isCurtailed(latestPowerW *float64, baselinePowerW *float64, latestHashRateHS *float64, driftThresholdFactor float64, requirePositiveEvidence bool) bool {
	if latestPowerW == nil || !isFinite(*latestPowerW) {
		if requirePositiveEvidence {
			return false
		}
		// Drift path: fall back to hash. Zero-or-missing hash → still curtailed;
		// positive hash → mining resumed.
		if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
			return true
		}
		return *latestHashRateHS <= 0
	}
	if baselinePowerW != nil && isFinite(*baselinePowerW) && *baselinePowerW > 0 {
		threshold := *baselinePowerW * driftThresholdFactor
		return *latestPowerW <= threshold
	}
	// Baseline missing: dual-signal fallback uses hash_rate alone.
	if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
		// Confirm path: no usable signal → no evidence. Drift path: preserve
		// curtailed.
		return !requirePositiveEvidence
	}
	return *latestHashRateHS <= 0
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
