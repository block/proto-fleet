package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// ClaimTarget is bookkeeping, not physical write admission. The executor must
// retain the device's write fence through dispatch and check current assignment
// intent there. Never recover an uncertain write by simply resetting this row.
func (r *Repository) ClaimTarget(ctx context.Context, q sqlc.Querier, scope Scope, operationID string, target AssignmentTarget) (int32, error) {
	attempt, err := q.ClaimPoolConfigurationTarget(ctx, sqlc.ClaimPoolConfigurationTargetParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID, AccountID: scope.account(), OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, staleAssignment()
	}
	if err != nil {
		return 0, storageError(err)
	}
	return attempt, nil
}

// FinishTarget accepts only sanitized result categories, never plugin error
// strings (which can contain credentials). Attempts distinguish late responses
// from a newer retry of the same immutable operation.
func (r *Repository) FinishTarget(ctx context.Context, q sqlc.Querier, scope Scope, operationID string, target AssignmentTarget, attempt int32, state, resultCode string) error {
	valid := false
	switch state {
	case "verified":
		valid = resultCode == ""
	case "applied_unverified":
		valid = resultCode == "readback_unavailable"
	case "different":
		valid = resultCode == "settings_differ"
	case "failed":
		valid = resultCode == "unavailable" || resultCode == "unsupported" || resultCode == "rejected" || resultCode == "write_uncertain"
	}
	if !valid || attempt <= 0 {
		return fleeterror.NewInvalidArgumentError("invalid pool update result")
	}
	n, err := q.FinishPoolConfigurationTarget(ctx, sqlc.FinishPoolConfigurationTargetParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID, AccountID: scope.account(), OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID, Attempt: attempt, State: state, ResultCode: resultCode})
	return targetMutationResult(n, err)
}

// RetryTarget requires the caller's live equipment authorization and write
// readiness checks. It never changes the revision or resurrects superseded work.
func (r *Repository) RetryTarget(ctx context.Context, q sqlc.Querier, scope Scope, operationID string, target AssignmentTarget) error {
	n, err := q.RetryPoolConfigurationTarget(ctx, sqlc.RetryPoolConfigurationTargetParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID, AccountID: scope.account(), OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID})
	return targetMutationResult(n, err)
}

func (r *Repository) GetOperation(ctx context.Context, q sqlc.Querier, scope Scope, operationID string) (Operation, error) {
	row, err := q.GetPoolConfigurationOperation(ctx, sqlc.GetPoolConfigurationOperationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), OperationID: operationID})
	if err != nil {
		return Operation{}, storageError(err)
	}
	targets, err := q.ListPoolConfigurationOperationTargets(ctx, sqlc.ListPoolConfigurationOperationTargetsParams{OrganizationID: scope.OrganizationID, OperationID: operationID})
	if err != nil {
		return Operation{}, storageError(err)
	}
	result := Operation{ID: row.OperationID, ConfigurationID: row.ConfigurationID, RevisionID: row.RevisionID, Targets: make([]OperationTarget, 0, len(targets))}
	for _, target := range targets {
		result.Targets = append(result.Targets, publicOperationTarget(target))
	}
	return result, nil
}

func targetMutationResult(n int64, err error) error {
	if err != nil {
		return storageError(err)
	}
	if n != 1 {
		return staleAssignment()
	}
	return nil
}
