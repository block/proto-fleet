package reconciler

import (
	"context"
	"errors"
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
	knownUnsentDeviceIdentifiers []string,
	command func(),
) (bool, bool) {
	if ev.ScopeType != models.ScopeTypeMixed {
		return false, true
	}
	reject := func(rejection topologyDispatchRejection, cause error) bool {
		r.topologyDispatchRejects.Store(ev.EventUUID, rejection)
		attrs := []any{"event_id", ev.ID, "event_uuid", ev.EventUUID, "reason", rejection.reason}
		if cause != nil {
			attrs = append(attrs, "error", cause)
		}
		slog.Error("curtailment reconciler: topology dispatch authorization failed", attrs...)
		releaseUnsent := knownUnsentDeviceIdentifiers
		if rejection.preserveRestoreOwnership {
			releaseUnsent = nil
		}
		if _, restoreErr := r.store.BeginRestoreTransition(
			ctx,
			ev.OrgID,
			ev.EventUUID,
			interfaces.BeginRestoreTransitionParams{
				KnownUnsentDeviceIdentifiers: releaseUnsent,
			},
		); restoreErr != nil {
			slog.Error("curtailment reconciler: failed to restore after topology dispatch authorization failure",
				"event_id", ev.ID, "event_uuid", ev.EventUUID, "error", restoreErr)
		} else {
			r.topologyDispatchRejects.Delete(ev.EventUUID)
		}
		return false
	}
	rejectBeforeCommand := func(reason string, cause error) (bool, bool) {
		return true, reject(topologyDispatchRejection{reason: reason}, cause)
	}
	if rejectedValue, rejected := r.topologyDispatchRejects.Load(ev.EventUUID); rejected {
		rejection, ok := rejectedValue.(topologyDispatchRejection)
		if !ok {
			rejection = topologyDispatchRejection{
				reason:                   "previous topology dispatch authorization failure",
				preserveRestoreOwnership: true,
			}
		}
		return true, reject(rejection, nil)
	}
	scope, hasScope, err := curtailment.ScopeFromJSON(ev.ScopeJSON)
	if err != nil || !hasScope {
		return rejectBeforeCommand("parse persisted mixed scope", err)
	}
	if !curtailment.IsTopologyScope(scope) {
		return false, true
	}
	filter, err := curtailment.ListCandidatesParamsForScope(scope)
	if err != nil {
		return rejectBeforeCommand("parse persisted topology selector", err)
	}
	filter.OrgID = ev.OrgID
	fenceStore, ok := r.store.(interfaces.CurtailmentTopologyDispatchFenceStore)
	if !ok {
		return rejectBeforeCommand("topology dispatch fence is not configured", nil)
	}
	dispatchDeviceIdentifiers := make([]string, 0, len(targets))
	for _, target := range targets {
		if target != nil {
			dispatchDeviceIdentifiers = append(dispatchDeviceIdentifiers, target.DeviceIdentifier)
		}
	}
	var rejectionReason string
	var rejectionCause error
	commandAttempted := false
	rejectFence := func(reason string, cause error) error {
		rejectionReason = reason
		rejectionCause = cause
		return errTopologyDispatchRejected
	}
	err = fenceStore.WithCurtailmentTopologyDispatchFence(
		ctx,
		ev,
		filter,
		dispatchDeviceIdentifiers,
		func(fenced interfaces.CurtailmentTopologyDispatchFenceSnapshot) error {
			latest := fenced.Event
			latestScope, hasScope, err := curtailment.ScopeFromJSON(latest.ScopeJSON)
			if err != nil || !hasScope || !curtailment.IsTopologyScope(latestScope) {
				return rejectFence("reload persisted topology scope", err)
			}
			envelope, err := curtailment.AuthorizationEnvelopeFromJSON(latest.AuthorizationEnvelopeJSON)
			if err != nil {
				return rejectFence("parse persisted authorization envelope", err)
			}
			coverage := fenced.Topology.Coverage
			if !topologyCoverageWithinEnvelope(coverage, envelope) {
				return rejectFence("current topology exceeds the persisted authorization envelope", nil)
			}
			currentMembers := make(map[string]struct{}, len(fenced.Topology.DispatchMemberDeviceIdentifiers))
			for _, deviceIdentifier := range fenced.Topology.DispatchMemberDeviceIdentifiers {
				currentMembers[deviceIdentifier] = struct{}{}
			}
			for _, target := range targets {
				if target == nil {
					continue
				}
				if _, ok := currentMembers[target.DeviceIdentifier]; !ok {
					return rejectFence("claimed miner is no longer in the persisted topology scope", nil)
				}
			}
			if r.permissions == nil {
				return rejectFence("dispatch permission resolver is not configured", nil)
			}
			effective, err := r.permissions.LoadEffective(ctx, latest.CreatedByUserID, latest.OrgID)
			if err != nil {
				return rejectFence("reload event creator permissions", err)
			}
			if !authorizationEnvelopeAllowsDispatch(effective, envelope, coverage) {
				return rejectFence("event creator no longer has required permissions", nil)
			}
			commandAttempted = true
			command()
			return nil
		},
	)
	if rejectionReason != "" {
		return rejectBeforeCommand(rejectionReason, rejectionCause)
	}
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		return true, false
	}
	if err != nil {
		return true, reject(topologyDispatchRejection{
			reason:                   "acquire topology dispatch fence",
			preserveRestoreOwnership: commandAttempted,
		}, err)
	}
	return true, true
}

var errTopologyDispatchRejected = errors.New("topology dispatch rejected")

type topologyDispatchRejection struct {
	reason                   string
	preserveRestoreOwnership bool
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
