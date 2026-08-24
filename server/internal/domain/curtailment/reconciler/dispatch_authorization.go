package reconciler

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// DispatchPermissionResolver reloads the event creator's live permissions
// immediately before a topology-scoped Curtail command is sent.
type DispatchPermissionResolver interface {
	LoadEffective(ctx context.Context, userID, organizationID int64) (*authz.EffectivePermissions, error)
}

func WithDispatchPermissionResolver(resolver DispatchPermissionResolver) Option {
	return func(r *Reconciler) { r.permissions = resolver }
}

func (r *Reconciler) authorizeTopologyCurtailDispatch(
	ctx context.Context,
	ev *models.Event,
	targets []*models.Target,
) bool {
	if ev.ScopeType != models.ScopeTypeMixed {
		return true
	}
	reject := func(reason string, cause error) bool {
		attrs := []any{"event_id", ev.ID, "event_uuid", ev.EventUUID, "reason", reason}
		if cause != nil {
			attrs = append(attrs, "error", cause)
		}
		slog.Error("curtailment reconciler: topology dispatch authorization failed", attrs...)
		if _, restoreErr := r.store.BeginRestoreTransition(
			ctx,
			ev.OrgID,
			ev.EventUUID,
			interfaces.BeginRestoreTransitionParams{},
		); restoreErr != nil {
			slog.Error("curtailment reconciler: failed to restore after topology dispatch authorization failure",
				"event_id", ev.ID, "event_uuid", ev.EventUUID, "error", restoreErr)
		}
		return false
	}
	scope, hasScope, err := curtailment.ScopeFromJSON(ev.ScopeJSON)
	if err != nil || !hasScope {
		return reject("parse persisted mixed scope", err)
	}
	if !curtailment.IsTopologyScope(scope) {
		return true
	}

	latest, err := r.store.GetEventByUUID(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		return reject("reload persisted event", err)
	}
	if latest == nil || latest.State != ev.State || latest.State.IsTerminal() {
		return reject("event is no longer dispatchable", nil)
	}
	latestScope, hasScope, err := curtailment.ScopeFromJSON(latest.ScopeJSON)
	if err != nil || !hasScope || !curtailment.IsTopologyScope(latestScope) {
		return reject("reload persisted topology scope", err)
	}
	envelope, err := curtailment.AuthorizationEnvelopeFromJSON(latest.AuthorizationEnvelopeJSON)
	if err != nil {
		return reject("parse persisted authorization envelope", err)
	}
	filter, err := curtailment.ListCandidatesParamsForScope(latestScope)
	if err != nil {
		return reject("parse persisted topology selector", err)
	}
	filter.OrgID = latest.OrgID
	topologyStore, ok := r.store.(interfaces.CurtailmentTopologyScopeStore)
	if !ok {
		return reject("topology scope resolver is not configured", nil)
	}
	coverage, err := topologyStore.ResolveCurtailmentTopologyScope(ctx, filter)
	if err != nil {
		return reject("reload topology coverage", err)
	}
	if !topologyCoverageWithinEnvelope(coverage, envelope) {
		return reject("current topology exceeds the persisted authorization envelope", nil)
	}
	candidates, err := r.store.ListCandidates(ctx, filter)
	if err != nil {
		return reject("reload topology members", err)
	}
	currentMembers := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil {
			currentMembers[candidate.DeviceIdentifier] = struct{}{}
		}
	}
	for _, target := range targets {
		if target == nil {
			continue
		}
		if _, ok := currentMembers[target.DeviceIdentifier]; !ok {
			return reject("claimed miner is no longer in the persisted topology scope", nil)
		}
	}

	if r.permissions == nil {
		return reject("dispatch permission resolver is not configured", nil)
	}
	effective, err := r.permissions.LoadEffective(ctx, latest.CreatedByUserID, latest.OrgID)
	if err != nil {
		return reject("reload event creator permissions", err)
	}
	if !authorizationEnvelopeAllowsDispatch(effective, envelope, coverage) {
		return reject("event creator no longer has required permissions", nil)
	}
	return true
}

func topologyCoverageWithinEnvelope(
	coverage interfaces.CurtailmentTopologyScopeCoverage,
	envelope models.AuthorizationEnvelope,
) bool {
	if envelope.MinerScopeUnbounded {
		return true
	}
	if coverage.RequireOrgWide {
		return false
	}
	return siteIDsWithinEnvelope(coverage.SelectedResourceSiteIDs, envelope.SelectedResourceSiteIDs) &&
		siteIDsWithinEnvelope(coverage.CurrentMemberSiteIDs, envelope.CurrentMemberSiteIDs)
}

func siteIDsWithinEnvelope(current, persisted []int64) bool {
	allowed := make(map[int64]struct{}, len(persisted))
	for _, siteID := range persisted {
		allowed[siteID] = struct{}{}
	}
	for _, siteID := range current {
		if _, ok := allowed[siteID]; !ok {
			return false
		}
	}
	return true
}

func authorizationEnvelopeAllowsDispatch(
	effective *authz.EffectivePermissions,
	envelope models.AuthorizationEnvelope,
	coverage interfaces.CurtailmentTopologyScopeCoverage,
) bool {
	requireOrgWideManage := envelope.MinerScopeUnbounded || envelope.FacilityFanScopeUnbounded || coverage.RequireOrgWide
	if requireOrgWideManage && !effective.HasOrgWide(authz.PermCurtailmentManage) {
		return false
	}
	manageSites := make(map[int64]struct{})
	for _, sites := range [][]int64{
		envelope.SelectedResourceSiteIDs,
		envelope.CurrentMemberSiteIDs,
		envelope.FacilityFanSiteIDs,
		coverage.SelectedResourceSiteIDs,
		coverage.CurrentMemberSiteIDs,
	} {
		for _, siteID := range sites {
			manageSites[siteID] = struct{}{}
		}
	}
	for siteID := range manageSites {
		id := siteID
		if !effective.Has(authz.PermCurtailmentManage, authz.ResourceContext{SiteID: &id}) {
			return false
		}
	}
	if envelope.FacilityFanScopeUnbounded && !effective.HasOrgWide(authz.PermSiteRead) {
		return false
	}
	for _, siteID := range envelope.FacilityFanSiteIDs {
		id := siteID
		if !effective.Has(authz.PermSiteRead, authz.ResourceContext{SiteID: &id}) {
			return false
		}
	}
	return true
}
