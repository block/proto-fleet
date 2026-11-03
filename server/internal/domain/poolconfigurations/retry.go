package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// ReviewRetry produces a fresh, password-confirmed operation under the retrying
// actor. It must retain the original exact revision and current assignment;
// it never revives superseded work or changes who authorized an old operation.
func (s *Service) ReviewRetry(ctx context.Context, accountID, operationID string, targets []AssignmentTarget) (Review, error) {
	if len(targets) == 0 || len(targets) > 500 {
		return Review{}, fleeterror.NewInvalidArgumentError("select between 1 and 500 update results")
	}
	if err := s.refreshReadiness(ctx, accountID, Read, func(q sqlc.Querier, p Principal) ([]AssignmentTarget, error) {
		if _, err := s.repository.GetOperation(ctx, q, p.Scope, operationID); err != nil {
			return nil, err
		}
		resolved := make([]AssignmentTarget, len(targets))
		for i, target := range targets {
			work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: p.Scope.OrganizationID, OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID})
			if err != nil {
				return nil, storageError(err)
			}
			resolved[i] = AssignmentTarget{Kind: work.TargetKind, ID: work.TargetID, DeviceID: work.DeviceID}
		}
		return resolved, nil
	}); err != nil {
		return Review{}, err
	}
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (Review, error) {
		operation, err := s.repository.GetOperation(ctx, q, p.Scope, operationID)
		if err != nil {
			return Review{}, err
		}
		configuration, err := s.repository.Get(ctx, q, p.Scope, operation.ConfigurationID)
		if err != nil {
			return Review{}, err
		}
		if configuration.RevisionID != operation.RevisionID {
			return Review{}, staleAssignment()
		}
		resolved := make([]AssignmentTarget, len(targets))
		for i, target := range targets {
			work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: p.Scope.OrganizationID, OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID})
			if err != nil {
				return Review{}, storageError(err)
			}
			state, code := work.State, work.ResultCode
			observation, err := q.GetPoolConfigurationObservation(ctx, sqlc.GetPoolConfigurationObservationParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID, AssignmentVersion: work.AssignmentVersion})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return Review{}, storageError(err)
			}
			if err == nil && observation.ObservedAt.After(work.UpdatedAt) {
				state, code = observation.State, observation.ResultCode
			}
			if code == "write_uncertain" || (state != "failed" && state != "different" && state != "applied_unverified") {
				return Review{}, fleeterror.NewFailedPreconditionError("this update cannot be retried; refresh its results")
			}
			resolved[i] = AssignmentTarget{Kind: work.TargetKind, ID: work.TargetID, DeviceID: work.DeviceID}
		}
		return s.repository.ReviewAssignment(ctx, q, p.Scope, p.ActorID, operation.ConfigurationID, configuration.Version, resolved, s.equipment)
	})
}
