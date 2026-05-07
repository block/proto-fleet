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

	// defaultMaxRetries caps per-target re-dispatch attempts after drift.
	// Crossing the cap leaves the target as `drifted` for BE-4/BE-5 to
	// surface through alerts.
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
func (r *Reconciler) Start(_ context.Context) error {
	if r.store == nil {
		return fmt.Errorf("curtailment reconciler: store is required")
	}
	if r.cmd == nil {
		return fmt.Errorf("curtailment reconciler: command dispatcher is required")
	}

	stopCtx, stopCancel := context.WithCancel(context.Background())
	workCtx, workCancel := context.WithCancel(context.Background())
	r.stopCancel = stopCancel
	r.workCancel = workCancel

	r.wg.Add(1)
	go r.tickLoop(stopCtx, workCtx)
	slog.Info("curtailment reconciler started", "tick_interval", r.cfg.TickInterval)
	return nil
}

// Stop signals the tick loop to exit and waits up to ShutdownDeadline for
// the in-flight tick to drain. Late ticks see workCtx canceled and bail out.
func (r *Reconciler) Stop() error {
	if r.workCancel != nil {
		watchdog := time.AfterFunc(r.cfg.ShutdownDeadline, r.workCancel)
		defer watchdog.Stop()
	}
	if r.stopCancel != nil {
		r.stopCancel()
	}
	r.wg.Wait()
	if r.workCancel != nil {
		r.workCancel()
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
			r.runTick(workCtx)
		}
	}
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

func (r *Reconciler) upsertHeartbeat(ctx context.Context, tickStart time.Time, tickUUID uuid.UUID, activeCount int32) {
	durationMS := int32(r.now().Sub(tickStart).Milliseconds()) //nolint:gosec // tick durations fit in int32 well past pathological cases
	if err := r.store.UpsertHeartbeat(ctx, interfaces.UpsertCurtailmentHeartbeatParams{
		LastTickAt:         tickStart,
		LastTickUUID:       tickUUID,
		LastTickDurationMS: &durationMS,
		ActiveEventCount:   activeCount,
	}); err != nil {
		slog.Error("curtailment reconciler: heartbeat upsert failed", "error", err)
	}
}

// processEvent wraps per-event work in a defer/recover so a panic in one
// event's processing does not abort the rest of the tick.
func (r *Reconciler) processEvent(ctx context.Context, ev *models.Event) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("curtailment reconciler: recovered panic processing event",
				"event_id", ev.ID, "event_uuid", ev.EventUUID, "panic", rec)
		}
	}()
	switch ev.State {
	case models.EventStatePending:
		r.dispatchPending(ctx, ev)
	case models.EventStateActive:
		r.observeActive(ctx, ev)
	case models.EventStateRestoring:
		// BE-4 owns the restoring path. The reconciler's pending/active
		// half does not write here; touching restoring rows would race the
		// restorer.
	case models.EventStateCompleted, models.EventStateCompletedWithFailures,
		models.EventStateCancelled, models.EventStateFailed:
		// Terminal — filtered upstream by ListNonTerminalEvents; if one
		// slips through we silently ignore it rather than retransitioning.
	}
}

// dispatchPending dispatches Curtail per pending target, then confirms any
// dispatched targets whose telemetry already shows curtailment. The event
// flips to `active` once every target has reached confirmed (or has been
// abandoned with a terminal error).
func (r *Reconciler) dispatchPending(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets failed",
			"event_id", ev.ID, "error", err)
		return
	}
	if len(targets) == 0 {
		// Defense-in-depth: Service.Start rejects empty plans, so this would
		// indicate a row written outside the v1 contract. Mark failed so a
		// manual cleanup is the only recovery path.
		now := r.now()
		if err := r.store.UpdateEventState(ctx, ev.ID, models.EventStateFailed, nil, &now); err != nil {
			slog.Error("curtailment reconciler: failed to mark empty event failed",
				"event_id", ev.ID, "error", err)
		}
		return
	}

	cmdCtx := reconcilerContext(ctx, ev.OrgID)
	for _, t := range targets {
		if t.State != models.TargetStatePending {
			continue
		}
		r.dispatchOneCurtail(cmdCtx, ev, t)
	}

	// Confirm any already-dispatched targets via the latest telemetry sample
	// before deciding whether the event itself can flip to active.
	r.confirmDispatched(ctx, ev)
	r.maybeMarkActive(ctx, ev)
}

// confirmDispatched walks the event's targets and promotes dispatched →
// confirmed when telemetry shows the device is curtailed. Pending and
// drifted rows are unaffected here.
func (r *Reconciler) confirmDispatched(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		return
	}
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
		c := candByID[t.DeviceIdentifier]
		if c == nil {
			continue
		}
		if !isCurtailedByPower(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor) {
			continue
		}
		now := r.now()
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:       models.TargetStateConfirmed,
			ConfirmedAt: &now,
			ObservedAt:  &now,
		}
		if c.LatestPowerW != nil && isFinite(*c.LatestPowerW) {
			power := *c.LatestPowerW
			params.ObservedPowerW = &power
		}
		if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
			slog.Error("curtailment reconciler: target confirm update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
	}
}

// dispatchOneCurtail issues one Curtail command for a single target and
// records the dispatch outcome on the row.
func (r *Reconciler) dispatchOneCurtail(ctx context.Context, ev *models.Event, t *models.Target) {
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
		if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, interfaces.UpdateCurtailmentTargetStateParams{
			State:     models.TargetStatePending,
			LastError: &errMsg,
		}); err != nil {
			slog.Error("curtailment reconciler: target update after dispatch error failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
		return
	}

	now := r.now()
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:            models.TargetStateDispatched,
		LastDispatchedAt: &now,
	}
	if result != nil && result.BatchIdentifier != "" {
		batchID := result.BatchIdentifier
		params.LastBatchUUID = &batchID
	}
	if err := r.store.UpdateTargetState(ctx, ev.ID, t.DeviceIdentifier, params); err != nil {
		slog.Error("curtailment reconciler: target dispatch update failed",
			"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
	}
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

	cmdCtx := reconcilerContext(ctx, ev.OrgID)
	for _, t := range targets {
		switch t.State {
		case models.TargetStateConfirmed:
			r.checkDrift(cmdCtx, ev, t, candByID[t.DeviceIdentifier])
		case models.TargetStateDrifted:
			// A drifted target whose retry budget is exhausted stays drifted;
			// otherwise re-dispatch and bump retry_count.
			if t.RetryCount >= r.cfg.MaxRetries {
				continue
			}
			r.retryCurtail(cmdCtx, ev, t)
		case models.TargetStatePending, models.TargetStateDispatched,
			models.TargetStateResolved, models.TargetStateReleased,
			models.TargetStateRestoreFailed:
			// Pending: dispatchPending handles. Dispatched: confirmDispatched.
			// Resolved/Released/RestoreFailed: BE-4's restorer owns these.
		}
	}
}

// checkDrift evaluates a confirmed target against the latest telemetry. If
// the device looks uncurtailed, transition to `drifted` and dispatch again.
func (r *Reconciler) checkDrift(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate) {
	if c == nil {
		// No candidate row — device may have been unpaired or deleted from
		// under us. Skip silently; BE-5 will surface this through metrics.
		return
	}
	if !isCurtailedByPower(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor) {
		// Drift detected. Mark drifted, increment retry, dispatch again.
		newRetry := t.RetryCount + 1
		now := r.now()
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:      models.TargetStateDrifted,
			RetryCount: &newRetry,
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
		// Only re-dispatch if we still have retry budget; otherwise leave the
		// row drifted for BE-5 alerting.
		if newRetry <= r.cfg.MaxRetries {
			r.dispatchOneCurtail(ctx, ev, t)
		}
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
	}
}

// retryCurtail re-dispatches a Curtail and bumps last_dispatched_at. The
// retry counter was already bumped when we marked the row drifted.
func (r *Reconciler) retryCurtail(ctx context.Context, ev *models.Event, t *models.Target) {
	r.dispatchOneCurtail(ctx, ev, t)
}

// maybeMarkActive flips the event from pending to active once every target
// has been confirmed. Targets stuck in dispatched (waiting for telemetry)
// keep the event in pending; the next tick re-evaluates after a fresh
// telemetry pull.
func (r *Reconciler) maybeMarkActive(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		return
	}
	for _, t := range targets {
		if t.State != models.TargetStateConfirmed {
			return
		}
	}
	now := r.now()
	if err := r.store.UpdateEventState(ctx, ev.ID, models.EventStateActive, &now, nil); err != nil {
		slog.Error("curtailment reconciler: pending→active transition failed",
			"event_id", ev.ID, "error", err)
	}
}

// reconcilerContext stamps a synthetic session.Info on the dispatch ctx so
// command preflight (CurtailmentActiveFilter) recognizes our self-traffic.
func reconcilerContext(parent context.Context, orgID int64) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      reconcilerActorName,
		UserID:         0,
		OrganizationID: orgID,
		ExternalUserID: reconcilerActorName,
		Username:       reconcilerActorName,
		Actor:          session.ActorCurtailment,
	})
}

// isCurtailedByPower decides whether a target is still curtailed using
// dual-signal logic. Returns true when the device is below the drift
// threshold OR (when baseline is nil) when hash_rate is non-positive.
// Non-finite samples are treated as "no signal" and preserve curtailed=true
// so a transient bad sensor reading does not trigger a redispatch storm.
func isCurtailedByPower(latestPowerW *float64, baselinePowerW *float64, latestHashRateHS *float64, driftThresholdFactor float64) bool {
	if latestPowerW == nil || !isFinite(*latestPowerW) {
		// No power signal. Fall back to hash-rate: zero-or-missing hash =>
		// curtailed; positive hash => drifted (mining resumed). Non-finite
		// hash => curtailed (preserve current state, do not redispatch).
		if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
			return true
		}
		return *latestHashRateHS <= 0
	}
	if baselinePowerW != nil && isFinite(*baselinePowerW) && *baselinePowerW > 0 {
		threshold := *baselinePowerW * driftThresholdFactor
		return *latestPowerW <= threshold
	}
	// Baseline missing: dual-signal fallback uses hash_rate alone. Positive
	// hash means the miner restarted; treat as drifted.
	if latestHashRateHS == nil || !isFinite(*latestHashRateHS) {
		return true
	}
	return *latestHashRateHS <= 0
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
