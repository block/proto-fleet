package reconciler

// All-paired policy support for the reconciler. An all-paired FULL_FLEET
// event durably owns every paired-like miner in scope: commandable miners are
// dispatched normally while non-commandable ones are held in the
// TargetStateUnavailable parking state and promoted to pending when they
// become actionable. This file collects the policy-specific admission,
// readiness-refresh, and release logic; the shared closed-loop flow in
// reconciler.go branches into it via isAllPairedPolicyEvent.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func isAllPairedPolicyEvent(ev *models.Event) bool {
	return ev != nil && ev.ForceIncludeAllPairedMiners
}

// isAllPairedPolicyReleasedCurtailTarget identifies dormant policy rows:
// released while the event still wants them curtailed. They hold no dispatch
// work but may be reopened by a later admission pass.
func isAllPairedPolicyReleasedCurtailTarget(ev *models.Event, target *models.Target) bool {
	return isAllPairedPolicyEvent(ev) &&
		target != nil &&
		target.State == models.TargetStateReleased &&
		target.DesiredState == models.DesiredStateCurtailed
}

// claimAllPairedPolicyTargets inserts or reopens durable policy rows for
// every paired-like miner in scope that no event owns yet. Unlike the
// closed-loop dispatch claim, rows enter in their computed policy state
// (pending or unavailable) and dispatch on a later pending pass, so this
// always returns nil.
func (r *Reconciler) claimAllPairedPolicyTargets(
	ctx context.Context,
	ev *models.Event,
	existingTargets []*models.Target,
	candidates []*models.Candidate,
	params interfaces.ListCandidatesParams,
) []*models.Target {
	orgConfig, err := r.store.GetOrgConfig(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: get org config (all-paired admission) failed",
			"event_id", ev.ID, "error", err)
		return nil
	}
	activeDevices, err := r.store.ListActiveCurtailmentTargetDevices(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: list active devices (all-paired admission) failed",
			"event_id", ev.ID, "error", err)
		return nil
	}
	activeSet := toStringSet(activeDevices)
	for _, target := range existingTargets {
		delete(activeSet, target.DeviceIdentifier)
	}
	plan := curtailment.BuildAllPairedPolicyPlan(
		candidates,
		activeSet,
		ev.IncludeMaintenance && ev.ForceIncludeMaintenance,
		candidateMinPowerWForEvent(ev, orgConfig.CandidateMinPowerW),
	)
	targets := curtailment.BuildInsertTargetParams(
		plan.Selected,
		models.ModeFullFleet,
		candidateMinPowerWForEvent(ev, orgConfig.CandidateMinPowerW),
	)
	targets = excludeNonReopenableExistingTargetParams(targets, existingTargets)
	if len(targets) == 0 {
		return nil
	}
	claimed, err := r.store.ClaimAllPairedPolicyTargets(ctx, ev.ID, targets)
	if err != nil {
		slog.Error("curtailment reconciler: claim all-paired policy targets failed",
			"event_id", ev.ID, "candidate_count", len(targets),
			"scope_device_count", len(params.DeviceIdentifiers),
			"scope_site_count", len(params.SiteIDs),
			"error", err)
		return nil
	}
	if claimed > 0 {
		slog.Info("curtailment reconciler: claimed all-paired policy targets",
			"event_id", ev.ID, "claimed", claimed)
	}
	return nil
}

// refreshAllPairedPolicyTargets re-evaluates dispatch readiness for policy
// rows that can still change state (pending/unavailable, desired curtailed):
// promotes unavailable rows whose miner became commandable, demotes pending
// rows whose miner stopped being commandable, and releases rows whose miner
// is no longer paired-like.
//
// Readiness flips are batched into one bulk UPDATE: a mass readiness change
// (fleet-wide outage or recovery) must not issue one round trip per device
// inside the shared tick budget, where it would starve every other event's
// dispatch/drift/restore progress. Releases stay per-row — unpairing is rare.
func (r *Reconciler) refreshAllPairedPolicyTargets(
	ctx context.Context,
	ev *models.Event,
	targets []*models.Target,
	candidates map[string]*models.Candidate,
) {
	refreshable := make(map[string]*models.Target, len(targets))
	updates := make([]interfaces.AllPairedReadinessUpdate, 0, len(targets))
	for _, target := range targets {
		if target.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if target.State != models.TargetStatePending && target.State != models.TargetStateUnavailable {
			continue
		}
		candidate := candidates[target.DeviceIdentifier]
		if candidate == nil || !curtailment.IsAllPairedPolicyPairingStatus(candidate.PairingStatus) {
			r.releaseAllPairedPolicyTarget(ctx, ev, target, "released: device is no longer paired-like")
			continue
		}
		nextState, reason := curtailment.AllPairedPolicyTargetState(
			candidate,
			ev.IncludeMaintenance && ev.ForceIncludeMaintenance,
		)
		if nextState == target.State && reason == targetErrorString(target) {
			continue
		}
		if nextState != models.TargetStatePending && nextState != models.TargetStateUnavailable {
			continue
		}
		updates = append(updates, interfaces.AllPairedReadinessUpdate{
			DeviceIdentifier: target.DeviceIdentifier,
			State:            nextState,
			Reason:           reason,
		})
		refreshable[target.DeviceIdentifier] = target
	}
	if len(updates) == 0 {
		return
	}
	rows, err := r.store.BulkRefreshAllPairedTargetReadiness(ctx, ev.ID, ev.State, updates)
	if err != nil {
		r.metrics.IncTargetWriteFailure()
		slog.Error("curtailment reconciler: all-paired readiness bulk refresh failed",
			"event_id", ev.ID, "update_count", len(updates), "error", err)
		return
	}
	if rows < int64(len(updates)) {
		// Rows that advanced concurrently (dispatch claim, Stop reset,
		// event phase change) are skipped by the SQL guards; the next tick
		// re-reads them. Benign, but counted so sustained races surface.
		r.metrics.IncEventStateRaceLoss()
		slog.Warn("curtailment reconciler: all-paired readiness refresh skipped concurrently-advanced rows",
			"event_id", ev.ID, "update_count", len(updates), "applied", rows)
	}
	// Mirror the write optimistically; a row the SQL skipped self-heals on
	// the next tick, and same-tick dispatch writes are individually guarded.
	for _, update := range updates {
		target := refreshable[update.DeviceIdentifier]
		target.State = update.State
		if update.Reason == "" {
			target.LastError = nil
		} else {
			reason := update.Reason
			target.LastError = &reason
		}
	}
}

func (r *Reconciler) releaseAllPairedPolicyTarget(ctx context.Context, ev *models.Event, target *models.Target, reason string) {
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:     models.TargetStateReleased,
		LastError: &reason,
	}
	if err := r.writeTargetState(ctx, ev, target.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: all-paired target release failed",
				"event_id", ev.ID, "device", target.DeviceIdentifier, "error", err)
		}
		return
	}
	target.State = models.TargetStateReleased
	target.LastError = &reason
}

func targetErrorString(target *models.Target) string {
	if target == nil || target.LastError == nil {
		return ""
	}
	return *target.LastError
}

// allPairedPolicyRefreshDeviceIdentifiers narrows the readiness-refresh
// candidate query to targets whose state can actually change: dispatched,
// confirmed, released, and restore-phase rows are owned by other passes.
func allPairedPolicyRefreshDeviceIdentifiers(targets []*models.Target) []string {
	if len(targets) == 0 {
		return nil
	}
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.DeviceIdentifier == "" {
			continue
		}
		if target.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if target.State != models.TargetStatePending && target.State != models.TargetStateUnavailable {
			continue
		}
		out = append(out, target.DeviceIdentifier)
	}
	return out
}
