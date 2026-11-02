package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

type closedLoopTopologySnapshot struct {
	params     interfaces.ListCandidatesParams
	candidates []*models.Candidate
}

func (r *Reconciler) prepareClosedLoopTopologyTick(
	ctx context.Context,
	ev *models.Event,
	existingTargets []*models.Target,
) (*closedLoopTopologySnapshot, bool) {
	if !isClosedLoopFullFleet(ev) || (ev.State != models.EventStatePending && ev.State != models.EventStateActive) ||
		ev.ScopeType != models.ScopeTypeMixed {
		return nil, false
	}
	plan := eventPlanFromContext(ctx, ev)
	if plan.scopeErr != nil {
		r.rejectClosedLoopAdmission(ctx, ev, "persisted topology scope is invalid", plan.scopeErr)
		return nil, true
	}
	if !plan.topology {
		return nil, false
	}
	params := plan.candidates
	params.OrgID = ev.OrgID
	if !r.validateClosedLoopTopologyAdmission(ctx, ev, params) {
		return nil, true
	}
	params.ResultLimit = curtailment.ScopeResolvedMinerMax + 1
	candidates, err := r.store.ListCandidates(ctx, params)
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (topology reconciliation) failed",
			"event_id", ev.ID, "error", err)
		return nil, true
	}
	if len(candidates) > curtailment.ScopeResolvedMinerMax {
		r.rejectClosedLoopAdmission(ctx, ev, "resolved miner limit exceeded", nil,
			"resolved_count", len(candidates), "limit", curtailment.ScopeResolvedMinerMax)
		return nil, true
	}
	departures, ok := r.reconcileClosedLoopTopologyDepartures(ctx, ev, existingTargets, candidates)
	if !ok || departures {
		return nil, true
	}
	return &closedLoopTopologySnapshot{params: params, candidates: candidates}, false
}

func (r *Reconciler) claimClosedLoopFullFleetTargetsFromSnapshot(
	ctx context.Context,
	ev *models.Event,
	existingTargets []*models.Target,
	topologySnapshot *closedLoopTopologySnapshot,
) ([]*models.Target, bool) {
	if !isClosedLoopFullFleet(ev) || (ev.State != models.EventStatePending && ev.State != models.EventStateActive) {
		return nil, false
	}
	var params interfaces.ListCandidatesParams
	var candidates []*models.Candidate
	if topologySnapshot != nil {
		params = topologySnapshot.params
		candidates = topologySnapshot.candidates
	} else {
		var ok bool
		params, ok = eventPlanFromContext(ctx, ev).candidates, eventPlanFromContext(ctx, ev).hasCandidateScope
		if !ok {
			slog.Warn("curtailment reconciler: unsupported closed-loop full-fleet scope",
				"event_id", ev.ID, "scope_type", ev.ScopeType)
			return nil, false
		}
		params.OrgID = ev.OrgID
		if !r.validateClosedLoopTopologyAdmission(ctx, ev, params) {
			return nil, isTopologyCandidateFilter(params)
		}
		// Closed-loop selectors are re-expanded on every admission pass. Bound the
		// query at max+1 so an oversized topology change fails closed without
		// materializing an unbounded candidate set in the reconciler.
		params.ResultLimit = curtailment.ScopeResolvedMinerMax + 1
	}
	topologyFilter := isTopologyCandidateFilter(params)
	if !topologyFilter {
		if curtailBatchIntervalActive(ev) && !r.curtailBatchIntervalElapsed(ev) {
			return nil, false
		}
		if hasInFlightCurtailDispatch(existingTargets) {
			return nil, false
		}
	}
	if topologySnapshot == nil {
		var err error
		candidates, err = r.store.ListCandidates(ctx, params)
		if err != nil {
			slog.Error("curtailment reconciler: list candidates (full_fleet admission) failed",
				"event_id", ev.ID, "error", err)
			return nil, topologyFilter
		}
		if len(candidates) > curtailment.ScopeResolvedMinerMax {
			r.rejectClosedLoopAdmission(ctx, ev, "resolved miner limit exceeded", nil,
				"resolved_count", len(candidates), "limit", curtailment.ScopeResolvedMinerMax)
			return nil, topologyFilter
		}
		if topologyFilter {
			departures, ok := r.reconcileClosedLoopTopologyDepartures(ctx, ev, existingTargets, candidates)
			if !ok || departures {
				return nil, true
			}
		}
	}
	if isAllPairedPolicyEvent(ev) {
		return nil, r.claimAllPairedPolicyTargets(ctx, ev, existingTargets, candidates, params)
	}
	orgConfig, err := r.store.GetOrgConfig(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: get org config (full_fleet admission) failed",
			"event_id", ev.ID, "error", err)
		return nil, false
	}
	targets, _ := curtailment.BuildFullFleetAdmissionTargets(
		candidates,
		ev.IncludeMaintenance && ev.ForceIncludeMaintenance,
		candidateMinPowerWForPlan(ctx, ev, orgConfig.CandidateMinPowerW),
	)
	targets = excludeNonReopenableExistingTargetParams(targets, existingTargets)
	activeDevices, err := r.store.ListActiveCurtailmentTargetDevices(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: list active devices (full_fleet admission) failed",
			"event_id", ev.ID, "error", err)
		return nil, false
	}
	targets = excludeDeviceIdentifiers(targets, activeDevices)
	cooldownSec := postEventCooldownForPlan(ctx, ev)
	if cooldownSec > 0 {
		candidateIdentifiers := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate != nil && candidate.DeviceIdentifier != "" {
				candidateIdentifiers = append(candidateIdentifiers, candidate.DeviceIdentifier)
			}
		}
		cooldownDevices, err := r.store.ListRecentlyResolvedCurtailedDevices(
			ctx,
			interfaces.ListRecentlyResolvedCurtailedDevicesParams{
				OrgID:             ev.OrgID,
				ExcludeEventID:    ev.ID,
				CooldownSec:       cooldownSec,
				DeviceIdentifiers: candidateIdentifiers,
			},
		)
		if err != nil {
			slog.Error("curtailment reconciler: list cooldown devices (full_fleet admission) failed",
				"event_id", ev.ID, "error", err)
			return nil, false
		}
		targets = excludeDeviceIdentifiers(targets, cooldownDevices)
	}
	if len(targets) == 0 {
		return nil, false
	}
	if topologyFilter && !r.topologyAdmissionAirflowReady(ctx, ev) {
		return nil, true
	}
	// Admission pacing never delays topology departures or airflow recovery:
	// both are handled above before new Curtail work is gated.
	if topologyFilter {
		if curtailBatchIntervalActive(ev) && !r.curtailBatchIntervalElapsed(ev) {
			return nil, false
		}
		if hasInFlightCurtailDispatch(existingTargets) {
			return nil, false
		}
	}
	batchSize := curtailBatchSizeForEvent(ev, len(targets))
	claimed, err := r.store.ClaimClosedLoopFullFleetTargets(
		ctx,
		ev.ID,
		ev.OrgID,
		cooldownSec,
		int(batchSize),
		targets,
	)
	if err != nil {
		slog.Error("curtailment reconciler: claim full_fleet targets failed",
			"event_id", ev.ID, "candidate_count", len(targets), "error", err)
		return nil, false
	}
	if len(claimed) > 0 {
		slog.Info("curtailment reconciler: claimed full_fleet targets",
			"event_id", ev.ID, "claimed", len(claimed))
	}
	return claimed, false
}

// topologyAdmissionAirflowReady keeps newly discovered topology members
// unclaimed until facility airflow has been restored for the configured delay.
// Returning to the next tick is intentional: scope membership and permissions
// must be resolved again after the delay before ownership is claimed.
func (r *Reconciler) topologyAdmissionAirflowReady(ctx context.Context, ev *models.Event) bool {
	if ev == nil || ev.FanOffSentAt == nil || len(ev.FacilityFanDeviceIDs) == 0 {
		return true
	}
	if fanAirflowNeedsReopen(ev) {
		_ = r.reopenEventFans(ctx, ev)
		return false
	}
	delay := max(ev.FanRestoreDelaySec, 0)
	return !r.now().Before(ev.FanAirflowReopenedAt.Add(time.Duration(delay) * time.Second))
}

func fanAirflowNeedsReopen(ev *models.Event) bool {
	return ev != nil && ev.FanOffSentAt != nil &&
		(ev.FanAirflowReopenedAt == nil || ev.FanLastError != nil)
}

func isTopologyCandidateFilter(params interfaces.ListCandidatesParams) bool {
	return len(params.BuildingIDs) > 0 || len(params.RackIDs) > 0 || len(params.GroupIDs) > 0
}

func (r *Reconciler) reconcileClosedLoopTopologyDepartures(
	ctx context.Context,
	ev *models.Event,
	existingTargets []*models.Target,
	candidates []*models.Candidate,
) (bool, bool) {
	members := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.DeviceIdentifier == "" {
			continue
		}
		if isAllPairedPolicyEvent(ev) {
			if !curtailment.IsAllPairedPolicyPairingStatus(candidate.PairingStatus) {
				continue
			}
		} else if candidate.PairingStatus != "PAIRED" {
			continue
		}
		members[candidate.DeviceIdentifier] = struct{}{}
	}
	departures := make([]string, 0)
	for _, target := range existingTargets {
		if target == nil || target.DesiredState == models.DesiredStateActive {
			continue
		}
		switch target.State { //nolint:exhaustive // Only terminal departure states are skipped.
		case models.TargetStateResolved, models.TargetStateRestoreFailed, models.TargetStateReleased:
			continue
		}
		if _, stillMember := members[target.DeviceIdentifier]; !stillMember {
			departures = append(departures, target.DeviceIdentifier)
		}
	}
	if len(departures) == 0 {
		return false, true
	}
	restoreStore, ok := r.store.(interfaces.CurtailmentTopologyTargetRestoreStore)
	if !ok {
		r.rejectClosedLoopAdmission(ctx, ev, "topology target restore store is not configured", nil)
		return true, false
	}
	transitioned, err := restoreStore.BeginCurtailmentTopologyTargetRestore(ctx, ev, departures)
	if err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: topology departure restore transition failed",
				"event_id", ev.ID, "departures", len(departures), "error", err)
		}
		return true, false
	}
	slog.Info("curtailment reconciler: topology departures entered restore",
		"event_id", ev.ID, "departures", len(departures), "transitioned", transitioned)
	return true, true
}

func (r *Reconciler) validateClosedLoopTopologyAdmission(
	ctx context.Context,
	ev *models.Event,
	params interfaces.ListCandidatesParams,
) bool {
	if len(params.BuildingIDs) == 0 && len(params.RackIDs) == 0 && len(params.GroupIDs) == 0 {
		return true
	}
	topologyStore, ok := r.store.(interfaces.CurtailmentTopologyScopeStore)
	if !ok {
		r.rejectClosedLoopAdmission(ctx, ev, "topology scope resolver is not configured", nil)
		return false
	}
	coverage, err := topologyStore.ResolveCurtailmentTopologyScope(ctx, params)
	if err != nil {
		if fleeterror.IsNotFoundError(err) || fleeterror.IsInvalidArgumentError(err) ||
			fleeterror.IsFailedPreconditionError(err) || fleeterror.IsResourceExhaustedError(err) {
			r.rejectClosedLoopAdmission(ctx, ev, "persisted topology scope is no longer valid", err)
		} else {
			slog.Error("curtailment reconciler: topology scope validation failed; admission deferred",
				"event_id", ev.ID, "error", err)
		}
		return false
	}
	reason, err := r.authorizeDispatchPrincipal(ctx, ev, coverage)
	if err != nil {
		slog.Error("curtailment reconciler: admission authorization deferred", "event_id", ev.ID, "error", err)
		return false
	}
	if reason != "" {
		r.rejectClosedLoopAdmission(ctx, ev, reason, nil)
		return false
	}
	return true
}

func (r *Reconciler) rejectClosedLoopAdmission(
	ctx context.Context,
	ev *models.Event,
	reason string,
	cause error,
	attributes ...any,
) {
	logAttributes := []any{"event_id", ev.ID, "event_uuid", ev.EventUUID, "reason", reason}
	if cause != nil {
		logAttributes = append(logAttributes, "error", cause)
	}
	logAttributes = append(logAttributes, attributes...)
	slog.Error("curtailment reconciler: closed-loop admission rejected", logAttributes...)
	latch, ok := r.store.(interfaces.CurtailmentDispatchRejectionStore)
	if !ok {
		return
	}
	if err := latch.RejectDispatch(ctx, ev.OrgID, ev.ID, reason); err != nil {
		return
	}
	if _, err := r.store.BeginRestoreTransition(
		ctx,
		ev.OrgID,
		ev.EventUUID,
		interfaces.BeginRestoreTransitionParams{},
	); err != nil && !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		slog.Error("curtailment reconciler: failed to restore after closed-loop admission rejection",
			"event_id", ev.ID, "event_uuid", ev.EventUUID, "error", err)
	}
}

func hasInFlightCurtailDispatch(targets []*models.Target) bool {
	for _, target := range targets {
		if target.DesiredState == models.DesiredStateCurtailed && target.State == models.TargetStateDispatching {
			return true
		}
	}
	return false
}
