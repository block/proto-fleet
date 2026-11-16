package mininggroups

import (
	"context"
	"database/sql"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/google/uuid"
)

// Reauthorizing the existing request preserves both its physical admission and
// its controller idempotency key. A different operator must explicitly consent;
// background reconciliation cannot borrow another operator's authority.
func (s *Service) resumeDeactivation(ctx context.Context, orgID, userID int64, identifier string, deviceID int64, requestID uuid.UUID) error {
	initial, err := s.queries.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
	if err != nil || !resumableDeactivation(initial, requestID) {
		return err
	}
	attempt, err := s.writes.ResumeWorkflow(ctx, orgID, deviceID, initial.AttemptID.UUID)
	if err != nil {
		return err
	}
	ctx = attempt.Context(ctx)
	return s.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		if _, err := q.LockPoolEquipmentOrganization(ctx, orgID); err != nil {
			return err
		}
		return s.writes.WithBindingObservation(ctx, deviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
			if err != nil || row.AttemptID != initial.AttemptID || !resumableDeactivation(row, requestID) {
				return err
			}
			target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{OrgID: orgID, DeviceIdentifier: identifier})
			if err != nil {
				return err
			}
			if err := authorize(ctx, q, userID, orgID, target.SiteID, authz.PermMinerDeactivateMiningGroups, authz.PermMinerStartMining, authz.PermMinerStopMining); err != nil {
				return err
			}
			row.ActorID = sql.NullInt64{Int64: userID, Valid: true}
			row.Status = statusApplying
			row.Message = "Resuming mining-group deactivation"
			row.RetryCount = 0
			row.NextReconcileAt = time.Now().UTC()
			row.Revision++
			if err := recordResumedActivity(ctx, q, row, identifier, target.SiteID); err != nil {
				return err
			}
			return save(ctx, q, row)
		})
	})
}

func resumableDeactivation(row sqlc.ContainerMiningGroupState, requestID uuid.UUID) bool {
	return row.OperationKind == operationDeactivate && row.Status == "blocked" && row.Step.Valid && row.OperationID.Valid && row.OperationID.UUID == requestID
}
