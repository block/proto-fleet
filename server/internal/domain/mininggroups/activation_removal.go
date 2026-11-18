package mininggroups

import (
	"context"
	"encoding/json"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
)

// Removing retained intent is an operator-only database action. It does not
// require the missing boards to return and cannot authorize a controller write.
func (s *Service) AuthorizeActivationPoolRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) error {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return err
	}
	if p.Scope.AccountID != 0 || p.AccountID != "" || p.ActorID <= 0 || info.UserID != p.ActorID || info.OrganizationID != p.Scope.OrganizationID || target.Kind != poolconfigurations.TargetDevice || target.ID != target.DeviceID {
		return fleeterror.NewForbiddenError("operator access to the container assignment is required")
	}
	device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: p.Scope.OrganizationID, ID: target.DeviceID})
	if err != nil {
		return err
	}
	return authorize(ctx, q, p.ActorID, p.Scope.OrganizationID, device.SiteID, authz.PermMinerActivateMiningGroups, authz.PermMinerUpdatePools)
}

// The caller holds organization and device locks. This receipt binds review to
// one completed activation and its retained source, not to physical placement.
func (s *Service) ReviewActivationPoolRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) (string, error) {
	if err := s.AuthorizeActivationPoolRemoval(ctx, q, p, target); err != nil {
		return "", err
	}
	row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID})
	if err != nil {
		return "", err
	}
	if (row.Status != statusComplete && row.Status != "failed") || row.OperationKind != operationActivate || row.Step.Valid || row.ControllerOperationID != "" {
		return "", fleeterror.NewFailedPreconditionError("Wait for the activation operation to settle before removing its assignment.")
	}
	journal, err := q.GetMiningGroupPoolHandoff(ctx, sqlc.GetMiningGroupPoolHandoffParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID, OperationID: row.OperationID.UUID})
	if err != nil {
		return "", err
	}
	if !journal.PlacementPending || !journal.CompletedAt.Valid {
		return "", fleeterror.NewFailedPreconditionError("The activation transfer changed. Review the assignment again.")
	}
	var capture activationPoolCapture
	if err := decode(journal.Captured, &capture); err != nil {
		return "", err
	}
	source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return "", err
	}
	if !capture.Deferred || source.RetiredAt.Valid || source.AccountID.Valid || source.Version != capture.Source.Version || source.ConfigurationID != capture.Source.ConfigurationID || source.RevisionID != capture.Source.RevisionID {
		return "", fleeterror.NewFailedPreconditionError("The retained assignment changed. Review the assignment again.")
	}
	receipt, err := json.Marshal(struct {
		OperationID string
		Capture     activationPoolCapture
	}{row.OperationID.UUID.String(), capture})
	return string(receipt), err
}

// Called only for reviewed keep-settings removal, in the same transaction as
// assignment retirement and its audit record. The frozen default-group binding
// remains a description of existing device settings, not a future pool write.
func (s *Service) AbandonActivationPoolTransfer(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) error {
	if _, err := s.ReviewActivationPoolRemoval(ctx, q, p, target); err != nil {
		return err
	}
	row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID})
	if err != nil {
		return err
	}
	if err := q.SetMiningGroupPoolHandoffPlacementPending(ctx, sqlc.SetMiningGroupPoolHandoffPlacementPendingParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID, OperationID: row.OperationID.UUID}); err != nil {
		return err
	}
	row.Message = "Mining groups are enabled. The container assignment was removed and its pending transfer was canceled. Device pool settings were kept."
	return save(ctx, q, row)
}
