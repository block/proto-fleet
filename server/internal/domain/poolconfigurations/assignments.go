package poolconfigurations

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

const (
	TargetDevice       = "device"
	TargetInstallation = "hashboard_installation"
)

// AssignmentTarget identifies a whole miner/container or one physical board
// installation, never a module slot whose future occupant could inherit intent.
type AssignmentTarget struct {
	Kind         string
	ID, DeviceID int64
}

type AssignmentChange struct {
	// ReviewedBinding is private, credential-free dispatch context.
	ReviewedBinding string
	Target          AssignmentTarget
	ExpectedVersion int64
}

type OperationTarget struct {
	Kind                  string
	ID, AssignmentVersion int64
	State, ResultCode     string
	Label                 string
}

type Operation struct {
	ID, ConfigurationID, RevisionID string
	Targets                         []OperationTarget
}

// Assign records already authorized, reviewed intent and its durable work in
// the caller's transaction. Authorization, capabilities, physical write fences,
// and audit are the service's responsibility; this method sends no device RPCs.
// Every writer takes the organization lock before configuration/assignment locks
// so switches and edits serialize, including insertion of previously absent rows.
func (r *Repository) Assign(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, configID, revisionID string, changes []AssignmentChange) (Operation, error) {
	if err := scope.validate(); err != nil {
		return Operation{}, err
	}
	if actorID <= 0 || len(changes) == 0 {
		return Operation{}, fleeterror.NewInvalidArgumentError("pool assignment requires an actor and equipment")
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return Operation{}, storageError(err)
	}
	config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: configID})
	if err != nil {
		return Operation{}, storageError(err)
	}
	if config.Archived || config.CurrentRevisionID.String != revisionID {
		return Operation{}, fleeterror.NewFailedPreconditionError("configuration changed; refresh and review again")
	}
	changes = slices.Clone(changes)
	slices.SortFunc(changes, func(a, b AssignmentChange) int {
		return cmp.Or(cmp.Compare(a.Target.Kind, b.Target.Kind), cmp.Compare(a.Target.ID, b.Target.ID))
	})
	for i, change := range changes {
		target := change.Target
		if target.ID <= 0 || target.DeviceID <= 0 || change.ExpectedVersion < 0 ||
			(target.Kind != TargetDevice && target.Kind != TargetInstallation) || (target.Kind == TargetDevice && target.ID != target.DeviceID) ||
			(i > 0 && target.Kind == changes[i-1].Target.Kind && target.ID == changes[i-1].Target.ID) {
			return Operation{}, fleeterror.NewInvalidArgumentError("select unique pool assignment targets")
		}
		if _, err := q.GetPoolAssignmentDevice(ctx, sqlc.GetPoolAssignmentDeviceParams{OrgID: scope.OrganizationID, ID: target.DeviceID}); err != nil {
			return Operation{}, storageError(err)
		}
		if target.Kind == TargetInstallation {
			if _, err := q.GetPoolAssignmentInstallation(ctx, sqlc.GetPoolAssignmentInstallationParams{OrgID: scope.OrganizationID, ID: target.ID, DeviceID: target.DeviceID}); err != nil {
				return Operation{}, fleeterror.NewFailedPreconditionError("hashboard installation changed; refresh and review again")
			}
		}
		previous, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
		if errors.Is(err, sql.ErrNoRows) {
			if change.ExpectedVersion != 0 {
				return Operation{}, staleAssignment()
			}
		} else if err != nil {
			return Operation{}, storageError(err)
		} else {
			if !previous.RetiredAt.Valid && previous.AccountID != scope.account() {
				return Operation{}, fleeterror.NewForbiddenError("equipment belongs to another configuration owner")
			}
			if previous.Version != change.ExpectedVersion || previous.DeviceID != target.DeviceID {
				return Operation{}, staleAssignment()
			}
		}
	}
	operation, err := q.CreatePoolConfigurationOperation(ctx, sqlc.CreatePoolConfigurationOperationParams{OperationID: uuid.NewString(), OrganizationID: scope.OrganizationID, ConfigurationID: configID, RevisionID: revisionID, ActorID: actorID})
	if err != nil {
		return Operation{}, storageError(err)
	}
	result := Operation{ID: operation.OperationID, ConfigurationID: configID, RevisionID: revisionID, Targets: make([]OperationTarget, 0, len(changes))}
	for _, change := range changes {
		target := change.Target
		if err := q.SupersedePoolConfigurationTarget(ctx, sqlc.SupersedePoolConfigurationTargetParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID}); err != nil {
			return Operation{}, storageError(err)
		}
		assignment, err := q.SetPoolConfigurationAssignment(ctx, sqlc.SetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID, DeviceID: target.DeviceID, ConfigurationID: configID, RevisionID: revisionID, OperationID: result.ID})
		if err != nil {
			return Operation{}, storageError(err)
		}
		row, err := q.CreatePoolConfigurationOperationTarget(ctx, sqlc.CreatePoolConfigurationOperationTargetParams{OrganizationID: scope.OrganizationID, OperationID: result.ID, TargetKind: target.Kind, TargetID: target.ID, AssignmentVersion: assignment.Version, ReviewedBinding: change.ReviewedBinding})
		if err != nil {
			return Operation{}, storageError(err)
		}
		result.Targets = append(result.Targets, publicOperationTarget(row))
	}
	return result, nil
}

func staleAssignment() error {
	return fleeterror.NewFailedPreconditionError("equipment assignment changed; refresh and review again")
}

// RetireAssignment preserves the generation and history while ending ongoing
// updates. Mode transitions retire whole-container intent before establishing
// board intent; physical write fencing remains the transition's responsibility.
func (r *Repository) RetireAssignment(ctx context.Context, q sqlc.Querier, scope Scope, target AssignmentTarget, expectedVersion int64) error {
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return storageError(err)
	}
	n, err := q.RetirePoolConfigurationAssignment(ctx, sqlc.RetirePoolConfigurationAssignmentParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID, AccountID: scope.account(), TargetKind: target.Kind, TargetID: target.ID, ExpectedVersion: expectedVersion})
	if err := targetMutationResult(n, err); err != nil {
		return err
	}
	if err := q.SupersedePoolConfigurationTarget(ctx, sqlc.SupersedePoolConfigurationTargetParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID}); err != nil {
		return storageError(err)
	}
	return nil
}

func publicOperationTarget(row sqlc.PoolConfigurationOperationTarget) OperationTarget {
	var display struct{ Label string }
	// Only the deliberately public label leaves the private dispatch binding.
	_ = json.Unmarshal([]byte(row.ReviewedBinding), &display)
	return OperationTarget{Kind: row.TargetKind, ID: row.TargetID, AssignmentVersion: row.AssignmentVersion, State: row.State, ResultCode: row.ResultCode, Label: display.Label}
}
