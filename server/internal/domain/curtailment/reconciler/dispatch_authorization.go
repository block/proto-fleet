package reconciler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"

	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// DispatchPermissionResolver reloads the event creator's live permissions
// immediately before a Curtail command is sent.
type DispatchPermissionResolver interface {
	LoadEffective(ctx context.Context, userID, organizationID int64) (*authz.EffectivePermissions, error)
	LoadRoleName(ctx context.Context, userID, organizationID int64) (string, error)
}

func WithDispatchPermissionResolver(resolver DispatchPermissionResolver) Option {
	return func(r *Reconciler) { r.permissions = resolver }
}

// authorizeTopologyCurtailDispatch also checks ordinary scopes: every send
// reloads the execution principal, while topology sends additionally fence
// changing membership under the persisted authorization envelope.
func (r *Reconciler) authorizeTopologyCurtailDispatch(
	ctx context.Context, ev *models.Event, targets []*models.Target,
	knownUnsentDeviceIdentifiers []string, command func(),
) (bool, bool) {
	latch, ok := r.store.(interfaces.CurtailmentDispatchRejectionStore)
	if !ok {
		return true, false
	}
	rejected, err := latch.GetDispatchRejection(ctx, ev.OrgID, ev.ID)
	if err != nil {
		return true, false
	} // infrastructure: defer, retain ownership
	reject := func(reason string) (bool, bool) {
		if err := latch.RejectDispatch(ctx, ev.OrgID, ev.ID, reason); err != nil {
			slog.Error("curtailment reconciler: persist dispatch rejection", "event_id", ev.ID, "error", err)
			return true, false
		}
		// No targets are released on a recovered latch: an earlier process may
		// have sent commands before rejection. Conservative restoration is safe.
		unsent := knownUnsentDeviceIdentifiers
		if rejected != nil {
			unsent = nil
		}
		_, err := r.store.BeginRestoreTransition(ctx, ev.OrgID, ev.EventUUID, interfaces.BeginRestoreTransitionParams{KnownUnsentDeviceIdentifiers: unsent})
		if err != nil {
			slog.Error("curtailment reconciler: restore rejected dispatch", "event_id", ev.ID, "error", err)
		}
		return true, false
	}
	if rejected != nil {
		return reject(*rejected)
	}
	plan := eventPlanFromContext(ctx, ev)
	if plan.scopeErr != nil {
		return reject("parse persisted scope: " + plan.scopeErr.Error())
	}

	generalFence, hasGeneralFence := r.store.(interfaces.CurtailmentDispatchFenceStore)
	if !plan.topology && !hasGeneralFence {
		reason, err := r.authorizeDispatchPrincipal(ctx, ev, interfaces.CurtailmentTopologyScopeCoverage{})
		if err != nil {
			return true, false
		}
		if reason != "" {
			return reject(reason)
		}
		if !r.eventStillDispatchable(ctx, ev) {
			return true, false
		}
		command()
		return true, true
	}
	var withFence func(context.Context, *models.Event, interfaces.ListCandidatesParams, []string, func(interfaces.CurtailmentTopologyDispatchFenceSnapshot) error) error
	if hasGeneralFence {
		withFence = generalFence.WithCurtailmentDispatchFence
	} else {
		fence, ok := r.store.(interfaces.CurtailmentTopologyDispatchFenceStore)
		if !ok {
			return true, false
		}
		withFence = fence.WithCurtailmentTopologyDispatchFence
	}
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		if target != nil {
			ids = append(ids, target.DeviceIdentifier)
		}
	}
	filter := plan.candidates
	filter.OrgID = ev.OrgID
	var reason string
	err = withFence(ctx, ev, filter, ids, func(snapshot interfaces.CurtailmentTopologyDispatchFenceSnapshot) error {
		if !bytes.Equal(plan.scopeJSON, snapshot.Event.ScopeJSON) {
			reason = "persisted scope changed before dispatch"
			return errTopologyDispatchRejected
		}
		var err error
		reason, err = r.authorizeDispatchPrincipal(ctx, snapshot.Event, snapshot.Topology.Coverage)
		if err != nil {
			return err
		}
		if reason != "" {
			return errTopologyDispatchRejected
		}
		members := curtailment.ToStringSet(snapshot.Topology.DispatchMemberDeviceIdentifiers)
		for _, id := range ids {
			if _, ok := members[id]; !ok {
				reason = "claimed miner no longer belongs to persisted topology scope"
				return errTopologyDispatchRejected
			}
		}
		command()
		return nil
	})
	if reason != "" {
		return reject(reason)
	}
	if fleeterror.IsNotFoundError(err) || fleeterror.IsInvalidArgumentError(err) || fleeterror.IsFailedPreconditionError(err) || fleeterror.IsResourceExhaustedError(err) {
		return reject("persisted topology scope is no longer dispatchable")
	}
	// Lock acquisition, permission reads, and commit failures are infrastructure
	// failures. They defer this tick and never latch an authorization denial.
	return true, err == nil
}

func (r *Reconciler) authorizeDispatchPrincipal(ctx context.Context, latest *models.Event, coverage interfaces.CurtailmentTopologyScopeCoverage) (string, error) {
	if latest.ExternalSource != nil && *latest.ExternalSource == "curtailment_automation" {
		automation, ok := r.store.(interfaces.CurtailmentAutomationDispatchAuthorizationStore)
		if !ok {
			return "", errors.New("automation dispatch authorization store is not configured")
		}
		allowed, err := automation.AuthorizeAutomationCurtailDispatch(ctx, latest, coverage)
		if err != nil {
			return "", err
		}
		if !allowed {
			return "automation execution grant no longer permits dispatch", nil
		}
		return "", nil
	}
	envelope, valid := dispatchAuthorizationEnvelope(latest.AuthorizationEnvelopeJSON)
	if !valid {
		return "parse persisted authorization envelope", nil
	}
	withinEnvelope := topologyCoverageWithinEnvelope(coverage, envelope)
	if latest.ScopeType != models.ScopeTypeMixed || !eventPlanFromContext(ctx, latest).topology {
		allowedSites := append(append([]int64(nil), envelope.SelectedResourceSiteIDs...), envelope.CurrentMemberSiteIDs...)
		withinEnvelope = envelope.MinerScopeUnbounded || (!coverage.RequireOrgWide && siteIDsWithinEnvelope(coverage.CurrentMemberSiteIDs, allowedSites))
	}
	if !withinEnvelope {
		return "current topology exceeds persisted authorization envelope", nil
	}
	if r.permissions == nil {
		return "", errors.New("dispatch permission resolver is not configured")
	}
	effective, err := r.permissions.LoadEffective(ctx, latest.CreatedByUserID, latest.OrgID)
	if err != nil {
		return "", err
	}
	if !authorizationEnvelopeAllowsDispatch(effective, envelope, coverage) {
		return "event creator no longer has required permissions", nil
	}
	adminAllowed, err := r.eventCreatorCanUseRequiredAdminControls(ctx, latest)
	if err != nil {
		return "", err
	}
	if !adminAllowed {
		return "event creator no longer has required admin role", nil
	}
	return "", nil

}

// Invalid persisted coverage is a durable policy denial. Keep that decision
// separate from infrastructure errors, which must only defer dispatch.
func dispatchAuthorizationEnvelope(raw []byte) (models.AuthorizationEnvelope, bool) {
	envelope, err := curtailment.AuthorizationEnvelopeFromJSON(raw)
	return envelope, err == nil
}

func (r *Reconciler) fenceTopologyRestoreDispatch(
	ctx context.Context,
	ev *models.Event,
	targets []*models.Target,
	command func([]*models.Target),
) (handled bool, fenceReached bool, commandTargets []*models.Target, err error) {
	if ev.ScopeType != models.ScopeTypeMixed {
		return false, false, targets, nil
	}
	plan := eventPlanFromContext(ctx, ev)
	if plan.scopeErr != nil {
		return true, false, nil, fleeterror.NewInternalErrorf("parse persisted restore scope: %v", plan.scopeErr)
	}
	if !plan.topology {
		return false, false, targets, nil
	}
	fenceStore, ok := r.store.(interfaces.CurtailmentTopologyRestoreDispatchFenceStore)
	if !ok {
		return true, false, nil, fleeterror.NewInternalError("topology restore dispatch fence is not configured")
	}
	deviceIdentifiers := make([]string, 0, len(targets))
	for _, target := range targets {
		if target != nil {
			deviceIdentifiers = append(deviceIdentifiers, target.DeviceIdentifier)
		}
	}
	err = fenceStore.WithCurtailmentTopologyRestoreDispatchFence(
		ctx,
		ev,
		deviceIdentifiers,
		func(fenced interfaces.CurtailmentTopologyRestoreDispatchFenceSnapshot) error {
			fenceReached = true
			if fenced.Event.State == models.EventStateRestoring {
				commandTargets = targets
				command(commandTargets)
				return nil
			}
			currentMembers := curtailment.ToStringSet(fenced.Topology.DispatchMemberDeviceIdentifiers)
			commandTargets = make([]*models.Target, 0, len(targets))
			returnedDeviceIdentifiers := make([]string, 0, len(targets))
			for _, target := range targets {
				if target == nil {
					continue
				}
				if _, returned := currentMembers[target.DeviceIdentifier]; returned {
					returnedDeviceIdentifiers = append(returnedDeviceIdentifiers, target.DeviceIdentifier)
					continue
				}
				commandTargets = append(commandTargets, target)
			}
			if err := fenced.ParkReturnedTargets(returnedDeviceIdentifiers); err != nil {
				return err
			}
			if len(commandTargets) > 0 {
				command(commandTargets)
			}
			return nil
		},
	)
	return true, fenceReached, commandTargets, err
}

func (r *Reconciler) eventCreatorCanUseRequiredAdminControls(ctx context.Context, ev *models.Event) (bool, error) {
	required, err := curtailment.EventRequiresAdminControls(ev)
	if err != nil || !required {
		return !required, err
	}
	roleName, err := r.permissions.LoadRoleName(ctx, ev.CreatedByUserID, ev.OrgID)
	if err != nil {
		return false, err
	}
	return roleName == domainAuth.AdminRoleName || roleName == domainAuth.SuperAdminRoleName, nil
}

var errTopologyDispatchRejected = errors.New("topology dispatch rejected")

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
	return curtailment.AuthorizationEnvelopeAllows(
		effective,
		envelope,
		coverage.SelectedResourceSiteIDs,
		coverage.CurrentMemberSiteIDs,
		coverage.RequireOrgWide,
	)
}
