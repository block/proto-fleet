package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// ReviewRetry produces a fresh, password-confirmed operation under the retrying
// actor. It must retain the original exact revision and current assignment;
// it never revives superseded work or changes who authorized an old operation.
func (s *Service) ReviewRetry(ctx context.Context, accountID, operationID string, targets []AssignmentTarget) (Review, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (Review, error) {
		if len(targets) == 0 || len(targets) > 500 {
			return Review{}, fleeterror.NewInvalidArgumentError("select between 1 and 500 update results")
		}
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
			if work.ResultCode == "write_uncertain" || (work.State != "failed" && work.State != "different" && work.State != "applied_unverified") {
				return Review{}, fleeterror.NewFailedPreconditionError("this update cannot be retried; refresh its results")
			}
			resolved[i] = AssignmentTarget{Kind: work.TargetKind, ID: work.TargetID, DeviceID: work.DeviceID}
		}
		return s.repository.ReviewAssignment(ctx, q, p.Scope, p.ActorID, operation.ConfigurationID, configuration.Version, resolved, s.equipment)
	})
}
