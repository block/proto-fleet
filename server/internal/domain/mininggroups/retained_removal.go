package mininggroups

import (
	"context"
	"encoding/json"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
)

// A retained whole-container assignment can become unremovable through the normal
// path when module membership is incomplete: unresolved board ownership denies
// ListPoolEquipmentModuleOwners authorization even for the organization's own
// saved intent. This operator-only path authorizes retiring that retained intent
// without treating missing membership as proof of board ownership. It never
// authorizes a device write or a customer-owned assignment.
func (s *Service) AuthorizeRetainedContainerRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) error {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return err
	}
	if p.Scope.AccountID != 0 || p.AccountID != "" || p.ActorID <= 0 || info.UserID != p.ActorID || info.OrganizationID != p.Scope.OrganizationID || target.Kind != poolconfigurations.TargetDevice || target.ID != target.DeviceID {
		return fleeterror.NewForbiddenError("operator access to the container assignment is required")
	}
	source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return err
	}
	// Account-owned equipment is never retired through this operator path. The
	// still-active requirement is enforced at review time, not here, so that reading
	// a completed (already retired) recovery removal stays authorized.
	if source.AccountID.Valid {
		return fleeterror.NewForbiddenError("operator access to the container assignment is required")
	}
	device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: p.Scope.OrganizationID, ID: target.DeviceID})
	if err != nil {
		return err
	}
	return authorize(ctx, q, p.ActorID, p.Scope.OrganizationID, device.SiteID, authz.PermMinerActivateMiningGroups, authz.PermMinerUpdatePools)
}

// ReviewRetainedContainerRemoval re-authorizes and binds removal to the exact
// retained assignment on a mining-group container at rest. It returns a stable,
// credential-free ownership receipt used to detect concurrent change before the
// assignment is retired. Keep-settings only: it authorizes no device write and
// is offered only for mining-group containers with no in-flight operation and no
// pending activation transfer (the journaled transfer has its own recovery path).
func (s *Service) ReviewRetainedContainerRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) (string, error) {
	if err := s.AuthorizeRetainedContainerRemoval(ctx, q, p, target); err != nil {
		return "", err
	}
	// A missing state row means this container is not managed by mining groups;
	// recovery is not offered and the caller keeps the original authorization error.
	state, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID})
	if err != nil {
		return "", err
	}
	if state.Status != "idle" || state.OperationKind != "" || state.ControllerOperationID != "" || state.Step.Valid {
		return "", fleeterror.NewFailedPreconditionError("Wait for the mining-group operation to settle before removing the container assignment.")
	}
	pending, err := q.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID})
	if err != nil {
		return "", err
	}
	if pending {
		return "", fleeterror.NewFailedPreconditionError("A pending assignment transfer exists. Review the assignment again.")
	}
	source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return "", err
	}
	// Only a still-active, organization-owned device intent is recoverable.
	if source.RetiredAt.Valid || source.AccountID.Valid {
		return "", fleeterror.NewFailedPreconditionError("The retained container assignment changed. Review the assignment again.")
	}
	receipt, err := json.Marshal(struct {
		RetainedContainerRecovery bool
		DeviceID                  int64
		ConfigurationID           string
		RevisionID                string
	}{true, target.DeviceID, source.ConfigurationID, source.RevisionID})
	return string(receipt), err
}
