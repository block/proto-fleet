// Package reconciler drives non-terminal curtailment events: dispatches
// Curtail commands for pending targets, watches telemetry for drift on
// confirmed targets, and retries within a bounded budget.
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
	// reconcilerActorName tags the synthetic dispatch ctx so audit + filter
	// bypass can recognize reconciler self-traffic.
	reconcilerActorName = "curtailment-reconciler"

	defaultTickInterval           = 30 * time.Second
	defaultShutdownDeadline       = 10 * time.Second
	defaultMaxRetries       int32 = 3

	// defaultDriftThresholdFactor: power_w > baseline_power_w * factor is
	// considered drifted. 0.5 catches partial- and full-restore.
	defaultDriftThresholdFactor = 0.5
)

// CommandDispatcher is the subset of command.Service the reconciler needs;
// keeps tests free of the full command-service graph.
type CommandDispatcher interface {
	Curtail(ctx context.Context, selector *pb.DeviceSelector, level sdk.CurtailLevel) (*command.CommandResult, error)
	Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error)
}

// Config carries runtime tunables. Zero-valued fields use defaults.
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

// Reconciler is a singleton goroutine ticking every config.TickInterval.
// Each tick reads non-terminal events, dispatches/observes per event with
// per-event panic isolation, then upserts the heartbeat.
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

// New builds a Reconciler. nil store/dispatcher is rejected at Start, not
// here, so a misconfigured fleetd surfaces during lifecycle bring-up.
func New(cfg Config, store interfaces.CurtailmentStore, cmd CommandDispatcher) *Reconciler {
	return &Reconciler{
		cfg:   cfg.withDefaults(),
		store: store,
		cmd:   cmd,
		now:   time.Now,
	}
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
// the in-flight tick to drain. running flips to false under the mutex
// before wg.Wait so a concurrent second Stop is a no-op.
//
// Known edge: a Start arriving between mu.Unlock and the old goroutine's
// wg.Done can install fresh cancel funcs and add a second goroutine to the
// same WaitGroup, leaving it live after Stop returns. Unreachable in
// fleetd today (Start/Stop each fire once); add a `stopping` state guard
// if the lifecycle ever grows a restart path.
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

// safeTick recovers panics in tick-level infra (ListNonTerminalEvents,
// heartbeat upsert) so the goroutine survives. Per-event isolation is
// handled separately in processEvent.
func (r *Reconciler) safeTick(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("curtailment reconciler: recovered panic in tick", "panic", rec)
		}
	}()
	r.runTick(ctx)
}

// runTick is one reconciliation pass. Per-event errors are isolated; the
// heartbeat upsert always fires so a bad event cannot blind liveness.
func (r *Reconciler) runTick(ctx context.Context) {
	tickStart := r.now()
	tickUUID := uuid.New()
	events, err := r.store.ListNonTerminalEvents(ctx)
	if err != nil {
		slog.Error("curtailment reconciler: failed to list non-terminal events", "error", err)
		// Heartbeat advances on tick freshness, not query health.
		r.upsertHeartbeat(ctx, tickStart, tickUUID, 0)
		return
	}

	for _, ev := range events {
		r.processEvent(ctx, ev)
	}

	r.upsertHeartbeat(ctx, tickStart, tickUUID, int32(len(events))) //nolint:gosec // bounded by org event count
}

func (r *Reconciler) upsertHeartbeat(_ context.Context, tickStart time.Time, tickUUID uuid.UUID, activeCount int32) {
	durationMS := int32(r.now().Sub(tickStart).Milliseconds()) //nolint:gosec // tick durations fit in int32
	// Detach from workCtx so shutdown-watchdog cancellation cannot drop the
	// final heartbeat; the timeout bounds blocking on a stuck DB.
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

// processEvent dispatches per-state work for one non-terminal event.
// The defer/recover here is load-bearing: a panic in one event must not
// abort processing of the rest of the tick.
func (r *Reconciler) processEvent(ctx context.Context, ev *models.Event) {
	defer func() {
		if rec := recover(); rec != nil {
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
		// Restorer owns this path; touching restoring rows would race it.
	default:
		slog.Warn("curtailment reconciler: unexpected event state",
			"event_id", ev.ID, "state", ev.State)
	}
}

// dispatchPending dispatches Curtail per pending target, confirms any
// already-dispatched targets via telemetry, then flips the event to active
// once every target is confirmed or terminally failed. Targets are read
// once per tick; phases mutate the in-memory slice in place.
func (r *Reconciler) dispatchPending(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if len(targets) == 0 {
		// Service.Start rejects empty plans, so a zero-target event is a
		// contract violation; manual intervention is the only recovery.
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

	// Confirm just-dispatched targets via current telemetry before deciding
	// whether the event itself can flip to active.
	r.confirmDispatched(ctx, ev, targets)
	r.maybeMarkActive(ctx, ev, targets)
}

// confirmDispatched promotes Dispatched → Confirmed when telemetry shows
// the device is curtailed. Per-target work delegates to confirmOneDispatched
// so this and observeActive's re-entry path share one primitive.
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

// dispatchOneCurtail issues one Curtail and records the outcome.
// nonTerminalFailureState is where the target lands on a non-terminal
// failure (pending → Pending; drifted → Drifted). Filter-skips and
// empty-batch results are treated as failed dispatches so the work isn't
// silently dropped. Success clears LastError.
//
// Restart-safety gap: command is enqueued before the Dispatched-state
// write; a crash between the two leaves the target Pending with an
// in-flight batch. Next tick redispatches (Curtail is idempotent at the
// device, but audit logs show two batches).
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
	// Empty BatchIdentifier means no device IDs resolved (miner unpaired
	// or deleted post-Start). No batch enqueued; treat as a failed attempt.
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
	// Mirror to the in-memory row so this tick's downstream phases see it.
	t.State = models.TargetStateDispatched
	t.LastDispatchedAt = &now
	t.LastError = nil
	t.LastBatchUUID = &batchID
}

// recordDispatchFailure bumps retry_count; keeps the target in
// nonTerminalFailureState while budget remains, transitions to RestoreFailed
// at exhaustion so the event can still proceed to active.
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
	// Mirror to in-memory row for the rest of this tick.
	t.State = state
	t.RetryCount = newRetry
	t.LastError = &errMsg
}

// skipReasonForDevice extracts the filter-skip reason for deviceID.
// Returns ("", false) when the device was not skipped.
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
			// Re-entry: drifted-then-redispatched, waiting on confirmation.
			r.confirmOneDispatched(cmdCtx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
		case models.TargetStateDrifted:
			// Re-dispatch unless the budget is exhausted. The `>= MaxRetries`
			// check is a backstop: recordDispatchFailure routes
			// budget-exhausted targets to RestoreFailed at the boundary, so a
			// Drifted row with RetryCount>=MaxRetries only occurs after a
			// failed UpdateTargetState write.
			if t.RetryCount >= r.cfg.MaxRetries {
				continue
			}
			r.dispatchOneCurtail(cmdCtx, ev, t, models.TargetStateDrifted)
		case models.TargetStatePending, models.TargetStateResolved,
			models.TargetStateReleased, models.TargetStateRestoreFailed:
			// Pending: shouldn't appear on active. Resolved/Released/RestoreFailed: terminal, restorer owns.
		}
	}
}

// confirmOneDispatched promotes Dispatched → Confirmed when telemetry shows
// curtailment, resetting retry_count. Used by both dispatchPending (via
// confirmDispatched) and observeActive's redispatch re-entry. Missing
// candidate row routes through recordDispatchFailure so a vanished device
// can't stall the event.
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

// checkDrift evaluates a confirmed target against telemetry. Uncurtailed
// → Drifted, re-dispatch if budget remains. retry_count tracks dispatch
// attempts, so the bump lives in dispatchOneCurtail, not here.
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
		// Budget exhausted: stay Drifted (matches observeActive's drift arm).
		if t.RetryCount >= r.cfg.MaxRetries {
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
		case models.TargetStatePending, models.TargetStateDispatched,
			models.TargetStateDrifted:
			// In flight; hold Pending for the next tick.
			return
		case models.TargetStateResolved, models.TargetStateReleased:
			// Unreachable on a pending event (restorer hasn't run yet); a row
			// here is a contract violation. Hold Pending; manual cleanup only.
			return
		}
	}
	if confirmed == 0 && terminalFailures > 0 {
		// All-failed: nothing curtailed, nothing to restore. Skip Active
		// and land on the failure terminal directly.
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

// isCurtailed decides whether telemetry shows the target is curtailed.
// Power-vs-baseline ranks above hash_rate; missing baseline falls back to
// hash_rate alone (positive hash = mining resumed).
//
// requirePositiveEvidence flips the missing-sample policy:
//   - true (confirm path): missing/non-finite samples → false. A Dispatched
//     target is not promoted to Confirmed without positive evidence.
//   - false (drift path): missing/non-finite samples preserve curtailed=true
//     so a flaky sensor doesn't trigger a redispatch storm.
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
