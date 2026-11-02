package reconciler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func excludeNonReopenableExistingTargetParams(targets []models.InsertTargetParams, existingTargets []*models.Target) []models.InsertTargetParams {
	if len(targets) == 0 || len(existingTargets) == 0 {
		return targets
	}
	existing := make(map[string]*models.Target, len(existingTargets))
	for _, target := range existingTargets {
		existing[target.DeviceIdentifier] = target
	}
	filtered := targets[:0]
	for _, target := range targets {
		previous, ok := existing[target.DeviceIdentifier]
		if ok && (!isReopenableTargetState(previous.State) ||
			(previous.State == models.TargetStateRestoreFailed && previous.DesiredState != models.DesiredStateActive)) {
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered
}

func isReopenableTargetState(state models.TargetState) bool {
	return state == models.TargetStateResolved ||
		state == models.TargetStateRestoreFailed ||
		state == models.TargetStateReleased
}

func excludeDeviceIdentifiers(targets []models.InsertTargetParams, deviceIdentifiers []string) []models.InsertTargetParams {
	if len(targets) == 0 || len(deviceIdentifiers) == 0 {
		return targets
	}
	excluded := curtailment.ToStringSet(deviceIdentifiers)
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
	dispatchable := claimed[:0]
	for _, target := range claimed {
		if target.State == models.TargetStatePending || target.State == models.TargetStateDispatching {
			dispatchable = append(dispatchable, target)
		}
	}
	if len(dispatchable) == 0 {
		return
	}
	knownUnsentDeviceIdentifiers := make([]string, 0, len(dispatchable))
	for _, target := range dispatchable {
		knownUnsentDeviceIdentifiers = append(knownUnsentDeviceIdentifiers, target.DeviceIdentifier)
	}
	_ = r.dispatchCurtailBatchWithKnownUnsent(
		ctx,
		ev,
		dispatchable,
		models.TargetStateDispatching,
		recordPendingDispatchClock,
		knownUnsentDeviceIdentifiers,
	)
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
		if err != nil || !hasScope {
			return interfaces.ListCandidatesParams{}, false
		}
		params, err := curtailment.ListCandidatesParamsForScope(scope)
		if err != nil {
			return interfaces.ListCandidatesParams{}, false
		}
		return params, true
	case models.ScopeTypeDeviceList:
		return interfaces.ListCandidatesParams{}, false
	default:
		return interfaces.ListCandidatesParams{}, false
	}
}

// enforceMaxDuration restores an event once its finite safety cap expires.
// Pending and active work share the clock from event creation (or an earlier
// started_at; see curtailment.MaxDurationElapsed). Incomplete confirmation
// cannot leave already curtailed miners held indefinitely. A failed transition
// still suppresses further curtail dispatch until a later tick can retry it.
func (r *Reconciler) enforceMaxDuration(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	elapsed, expired := curtailment.MaxDurationElapsed(ev, r.now())
	if !expired {
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
