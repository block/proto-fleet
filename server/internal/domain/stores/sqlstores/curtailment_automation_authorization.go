package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	curtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/google/uuid"
)

func saveAutomationExecutionBinding(ctx context.Context, q sqlc.Querier, ruleID, orgID int64, admin bool, triggerType models.AutomationTriggerType) error {
	if err := q.UpsertCurtailmentAutomationExecutionGrant(ctx, sqlc.UpsertCurtailmentAutomationExecutionGrantParams{RuleID: ruleID, OrgID: orgID, AdminControlsGranted: admin}); err != nil {
		return err
	}
	if triggerType == models.AutomationTriggerTypeSchedule {
		// The window producer asserts the saved schedule from scratch. A save or
		// a re-enable from disabled also renews the schedule revision, but
		// enabling an already-enabled schedule keeps it; clearing the row lets
		// that rebind retry the current window now instead of after its backoff.
		return q.DeleteCurtailmentAutomationExecution(ctx, ruleID)
	}
	return q.QueueCurrentCurtailmentAutomationExecution(ctx, ruleID)
}

// getAutomationExecutionGrant reads the rule's grant with the trigger-specific
// locks: MQTT grants require the enabled source, schedule grants the schedule.
func getAutomationExecutionGrant(ctx context.Context, q sqlc.Querier, event *models.Event, ruleID int64) (sqlc.CurtailmentAutomationExecutionGrant, error) {
	if event.SourceActorType == models.SourceActorScheduler {
		return q.GetCurtailmentScheduleExecutionGrant(ctx, sqlc.GetCurtailmentScheduleExecutionGrantParams{RuleID: ruleID, OrgID: event.OrgID})
	}
	return q.GetCurtailmentAutomationExecutionGrant(ctx, sqlc.GetCurtailmentAutomationExecutionGrantParams{RuleID: ruleID, OrgID: event.OrgID})
}

// AuthorizeAutomationCurtailDispatch evaluates the organization's revocable,
// profile-bound automation grant. The original human identity is audit data.
func (s *SQLCurtailmentStore) AuthorizeAutomationCurtailDispatch(ctx context.Context, event *models.Event, coverage interfaces.CurtailmentTopologyScopeCoverage) (bool, error) {
	ruleID, valid := automationDispatchRuleID(event)
	if !valid {
		return false, nil
	}
	err := authorizePersistedAutomationGrant(ctx, s.GetQueries(ctx), ruleID, event, &coverage)
	if fleeterror.IsForbiddenError(err) || fleeterror.IsFailedPreconditionError(err) {
		return false, nil
	}
	return err == nil, err
}

func authorizePersistedAutomationGrant(ctx context.Context, q sqlc.Querier, ruleID int64, event *models.Event, coverage *interfaces.CurtailmentTopologyScopeCoverage) error {
	// Insert authorizations have no event UUID yet, but carry the same immutable
	// markers that persisted dispatch authorizations must verify.
	if err := curtailment.ValidateAutomationEventOwnership(event, event.OrgID, ruleID); err != nil {
		return err
	}
	grant, err := getAutomationExecutionGrant(ctx, q, event, ruleID)
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewForbiddenError("automation execution grant revoked or binding changed; rebind_required")
	}
	if err != nil {
		return err
	}
	var binding struct {
		ResponseProfileID       int64  `json:"response_profile_id"`
		ResponseProfileRevision string `json:"response_profile_revision"`
	}
	if err := json.Unmarshal(event.DecisionSnapshotJSON, &binding); err != nil {
		return fleeterror.NewFailedPreconditionError("automation profile binding missing")
	}
	revision, err := uuid.Parse(binding.ResponseProfileRevision)
	if err != nil || binding.ResponseProfileID != grant.ResponseProfileID || revision != grant.ResponseProfileRevision {
		return fleeterror.NewForbiddenError("automation event no longer matches execution grant")
	}
	grantEnvelope, err := curtailment.AuthorizationEnvelopeFromJSON(grant.AuthorizationEnvelopeJson)
	if err != nil {
		return err
	}
	envelope, err := curtailment.AuthorizationEnvelopeFromJSON(event.AuthorizationEnvelopeJSON)
	if err != nil {
		return err
	}
	var selected, members []int64
	orgWide := false
	if coverage != nil {
		selected = coverage.SelectedResourceSiteIDs
		members = coverage.CurrentMemberSiteIDs
		orgWide = coverage.RequireOrgWide
	}
	if !automationGrantCoversScope(grantEnvelope, envelope, selected, members, orgWide) || !curtailment.AuthorizationEnvelopeAllows(automationGrantPermissions(grantEnvelope), envelope, selected, members, orgWide) {
		return fleeterror.NewForbiddenError("automation scope exceeds its organization execution grant")
	}
	requiresAdmin, err := curtailment.EventRequiresAdminControls(event)
	if err != nil {
		return err
	}
	if requiresAdmin && !grant.AdminControlsGranted {
		return fleeterror.NewForbiddenError("automation execution grant does not allow admin controls")
	}
	return nil
}

func automationGrantPermissions(envelope models.AuthorizationEnvelope) *authz.EffectivePermissions {
	var assignments []authz.Assignment
	if envelope.MinerScopeUnbounded || envelope.FacilityFanScopeUnbounded {
		assignments = append(assignments, authz.Assignment{ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermCurtailmentManage}})
	}
	if envelope.FacilityFanScopeUnbounded {
		assignments = append(assignments, authz.Assignment{ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermSiteRead}})
	}
	sites := make(map[int64]bool)
	for _, ids := range [][]int64{envelope.SelectedResourceSiteIDs, envelope.CurrentMemberSiteIDs, envelope.FacilityFanSiteIDs} {
		for _, id := range ids {
			sites[id] = true
		}
	}
	fanSites := make(map[int64]bool)
	for _, id := range envelope.FacilityFanSiteIDs {
		fanSites[id] = true
	}
	// Management covers the authorized miner/fan sites. Site-read authority is
	// granted only where the original profile authorized facility-fan controls.
	for id := range sites {
		siteID := id
		permissions := []string{authz.PermCurtailmentManage}
		if envelope.FacilityFanScopeUnbounded || fanSites[id] {
			permissions = append(permissions, authz.PermSiteRead)
		}
		assignments = append(assignments, authz.Assignment{ScopeType: authz.ScopeSite, SiteID: &siteID, Permissions: permissions})
	}
	return authz.NewEffectivePermissions(assignments)
}

// Keep miner and facility-fan grants distinct even though both use the
// curtailment:manage permission. A fan site grant cannot expand miner scope.
func automationGrantCoversScope(grant, event models.AuthorizationEnvelope, selected, members []int64, orgWide bool) bool {
	within := func(current, allowed []int64) bool {
		sites := make(map[int64]bool, len(allowed))
		for _, id := range allowed {
			sites[id] = true
		}
		for _, id := range current {
			if !sites[id] {
				return false
			}
		}
		return true
	}
	if !grant.MinerScopeUnbounded {
		if event.MinerScopeUnbounded || orgWide {
			return false
		}
		miners := append(append([]int64(nil), grant.SelectedResourceSiteIDs...), grant.CurrentMemberSiteIDs...)
		for _, current := range [][]int64{event.SelectedResourceSiteIDs, event.CurrentMemberSiteIDs, selected, members} {
			if !within(current, miners) {
				return false
			}
		}
	}
	return grant.FacilityFanScopeUnbounded || (!event.FacilityFanScopeUnbounded && within(event.FacilityFanSiteIDs, grant.FacilityFanSiteIDs))
}

// lockAutomationGrantForDispatch keeps revocation serialized with the physical
// enqueue boundary. Invalid identities are policy denials; missing grants are
// rejected by the callback's live authorization check. Infrastructure failures
// defer the dispatch.
func lockAutomationGrantForDispatch(ctx context.Context, q sqlc.Querier, event *models.Event) error {
	ruleID, valid := automationDispatchRuleID(event)
	if !valid {
		return fleeterror.NewFailedPreconditionError("invalid automation rule identity")
	}
	_, err := getAutomationExecutionGrant(ctx, q, event, ruleID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func automationDispatchRuleID(event *models.Event) (int64, bool) {
	if event == nil || event.SourceActorID == nil {
		return 0, false
	}
	ruleID, err := strconv.ParseInt(*event.SourceActorID, 10, 64)
	return ruleID, err == nil && ruleID > 0
}
