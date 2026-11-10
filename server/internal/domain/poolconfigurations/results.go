package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

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
		valid = resultCode == "unavailable" || resultCode == "unsupported" || resultCode == "rejected" || resultCode == "write_failed" || resultCode == "write_uncertain" || resultCode == "interrupted"
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
	if errors.Is(err, sql.ErrNoRows) {
		id, parseErr := uuid.Parse(operationID)
		if parseErr != nil {
			return Operation{}, storageError(err)
		}
		event, eventErr := q.GetPoolLifecycleTransition(ctx, id)
		if eventErr != nil || event.OrganizationID != scope.OrganizationID || event.OwnerAccountID != scope.AccountID {
			return Operation{}, fleeterror.NewNotFoundError("pool operation not found")
		}
		kind, name := "clear", ""
		if event.Reason == "pairing" {
			kind = "apply"
		}
		if event.ConfigurationID.Valid {
			kind = "apply"
			config, err := r.Get(ctx, q, scope, event.ConfigurationID.String)
			if err != nil {
				return Operation{}, err
			}
			name = config.Name
		}
		state := event.State
		if state == "ready" {
			state = "pending"
		}
		target := publicOperationTarget(sqlc.PoolConfigurationOperationTarget{TargetKind: event.TargetKind, TargetID: event.TargetID, State: state, ResultCode: event.ResultCode, UpdatedAt: event.UpdatedAt, ReviewedBinding: event.ReviewedBinding})
		if target.ID == 0 {
			target.Kind = TargetDevice
			target.ID = event.DeviceID
			if event.AssetKind == 7 {
				target.Kind = TargetInstallation
				boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: scope.OrganizationID, HashboardIds: []int64{event.AssetID}})
				if err != nil {
					return Operation{}, err
				}
				if len(boards) == 1 {
					target.ID = boards[0].InstallationID
				}
			}
		}
		return Operation{Kind: kind, Name: name, ID: operationID, ConfigurationID: event.ConfigurationID.String, RevisionID: event.RevisionID.String, CreatedAt: event.CreatedAt, Targets: []OperationTarget{target}}, nil
	}
	if err != nil {
		return Operation{}, storageError(err)
	}
	targets, err := q.ListPoolConfigurationOperationTargets(ctx, sqlc.ListPoolConfigurationOperationTargetsParams{OrganizationID: scope.OrganizationID, OperationID: operationID})
	if err != nil {
		return Operation{}, storageError(err)
	}
	configuration, err := r.Get(ctx, q, scope, row.ConfigurationID)
	if err != nil {
		return Operation{}, err
	}
	result := Operation{Kind: row.Kind, Name: configuration.Name, CreatedAt: row.CreatedAt.UTC(), ID: row.OperationID, ConfigurationID: row.ConfigurationID, RevisionID: row.RevisionID, Targets: make([]OperationTarget, 0, len(targets))}
	// Historical equipment identity comes from the authorized review snapshot.
	// Live metadata can belong to a different owner after reassignment.
	for _, target := range targets {
		public := publicOperationTarget(target.PoolConfigurationOperationTarget)
		if row.Kind == "clear" && (public.State == "applying" || public.State == "uncertain") && target.PhysicalAttemptStatus == "unknown" {
			public.ResultCode = "operator_recovery_required"
		}
		result.Targets = append(result.Targets, public)
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

type OperationPage struct {
	Operations    []sqlc.ListPoolConfigurationOperationsRow
	NextPageToken string
}

func (s *Service) ListOperations(ctx context.Context, accountID, configurationID, pageToken string) (OperationPage, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (OperationPage, error) {
		if _, err := s.repository.Get(ctx, q, p.Scope, configurationID); err != nil {
			return OperationPage{}, err
		}
		if pageToken != "" {
			cursor, err := q.GetPoolConfigurationOperation(ctx, sqlc.GetPoolConfigurationOperationParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), OperationID: pageToken})
			if errors.Is(err, sql.ErrNoRows) {
				return OperationPage{}, fleeterror.NewInvalidArgumentError("invalid operation page token")
			}
			if err != nil {
				return OperationPage{}, storageError(err)
			}
			if cursor.ConfigurationID != configurationID {
				return OperationPage{}, fleeterror.NewInvalidArgumentError("invalid operation page token")
			}
		}
		rows, err := q.ListPoolConfigurationOperations(ctx, sqlc.ListPoolConfigurationOperationsParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ConfigurationID: configurationID, BeforeID: pageToken})
		if err != nil {
			return OperationPage{}, storageError(err)
		}
		page := OperationPage{Operations: rows}
		if len(rows) > 25 {
			page.Operations = rows[:25]
			page.NextPageToken = rows[24].OperationID
		}
		return page, nil
	})
}
