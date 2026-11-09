package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const resultWriteUncertain = "write_uncertain"

// ReviewRetry produces a fresh, password-confirmed operation under the retrying
// actor. It must retain the original exact revision and current assignment;
// it never revives superseded work or changes who authorized an old operation.
func (s *Service) ReviewRetry(ctx context.Context, accountID, operationID string, targets []AssignmentTarget) (Review, error) {
	if len(targets) == 0 || len(targets) > 500 {
		return Review{}, fleeterror.NewInvalidArgumentError("select between 1 and 500 update results")
	}
	operation, err := s.GetOperation(ctx, accountID, operationID)
	if err != nil {
		return Review{}, err
	}
	if operation.Kind == "clear" || operation.Kind == "unassign" {
		if len(targets) != 1 || len(operation.Targets) != 1 || operation.Targets[0].Kind != targets[0].Kind || operation.Targets[0].ID != targets[0].ID {
			return Review{}, retryUnavailable()
		}
		p, manager, err := s.removalPrincipal(ctx, accountID)
		if err != nil {
			return Review{}, err
		}
		r, err := manager.RecoverRemoval(ctx, p, operationID)
		if err != nil {
			return Review{}, err
		}
		if r.State == "failed" {
			r, err = manager.ReviewRemoval(ctx, p, targets[0])
			if err != nil {
				return Review{}, err
			}
		}
		return Review{ID: r.ID, Targets: []AssignmentTarget{r.Target}}, nil
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
			state, code, err := latestRetryOutcome(ctx, q, work)
			if err != nil {
				return Review{}, err
			}
			if !retryableOutcome(state, code) {
				return Review{}, retryUnavailable()
			}
			resolved[i] = AssignmentTarget{Kind: work.TargetKind, ID: work.TargetID, DeviceID: work.DeviceID}
		}
		return s.repository.ReviewAssignment(ctx, q, p.Scope, p.ActorID, operation.ConfigurationID, configuration.Version, resolved, s.equipment)
	})
}

func latestRetryOutcome(ctx context.Context, q sqlc.Querier, work sqlc.GetPoolConfigurationWorkRow) (string, string, error) {
	state, code := work.State, work.ResultCode
	observation, err := q.GetPoolConfigurationObservation(ctx, sqlc.GetPoolConfigurationObservationParams{OrganizationID: work.OrganizationID, TargetKind: work.TargetKind, TargetID: work.TargetID, AssignmentVersion: work.AssignmentVersion})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", storageError(err)
	}
	if err == nil && observation.ObservedAt.After(work.UpdatedAt) {
		state, code = observation.State, observation.ResultCode
	}
	return state, code, nil
}

func retryableOutcome(state, code string) bool {
	return code != resultWriteUncertain && (state == "failed" || state == "different" || state == "applied_unverified")
}

func retryUnavailable() error {
	return fleeterror.NewFailedPreconditionError("this update cannot be retried; refresh its results")
}
