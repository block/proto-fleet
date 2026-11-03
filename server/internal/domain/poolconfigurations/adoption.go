package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Adopt preserves complete, explicitly established legacy intent without
// issuing a command or asserting verified settings. Maintenance collectors must
// first lock sources and verify current ownership and exact physical identity.
// Existing intent, including tombstones, is never replaced by a repeated import.
func (r *Repository) Adopt(ctx context.Context, q sqlc.Querier, scope Scope, target AssignmentTarget, configID, revisionID string) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if target.ID <= 0 || target.DeviceID <= 0 || (target.Kind != TargetDevice && target.Kind != TargetInstallation) ||
		(target.Kind == TargetDevice && target.ID != target.DeviceID) {
		return fleeterror.NewInvalidArgumentError("invalid adoption target")
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return storageError(err)
	}
	config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: configID})
	if err != nil {
		return storageError(err)
	}
	if config.Archived || config.CurrentRevisionID.String != revisionID {
		return fleeterror.NewFailedPreconditionError("imported configuration changed; review the assignment before adoption")
	}
	previous, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err == nil {
		if previous.RetiredAt.Valid || previous.OperationID.Valid || previous.ConfigurationID != configID || previous.RevisionID != revisionID || previous.DeviceID != target.DeviceID {
			return fleeterror.NewFailedPreconditionError("equipment already has managed or retired intent; import did not replace it")
		}
		// Avoid INSERT ... ON CONFLICT: its BEFORE INSERT trigger would advance
		// the configuration's assignment version even on an idempotent replay.
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storageError(err)
	}
	conflicting, err := q.HasPoolAssignmentKindConflict(ctx, sqlc.HasPoolAssignmentKindConflictParams{OrganizationID: scope.OrganizationID, DeviceID: target.DeviceID, TargetKind: target.Kind})
	if err != nil {
		return storageError(err)
	}
	if conflicting {
		return fleeterror.NewFailedPreconditionError("equipment already has intent for another mining mode; import did not replace it")
	}
	if _, err := q.GetPoolAssignmentDevice(ctx, sqlc.GetPoolAssignmentDeviceParams{OrgID: scope.OrganizationID, ID: target.DeviceID}); err != nil {
		return storageError(err)
	}
	if target.Kind == TargetInstallation {
		if _, err := q.GetPoolAssignmentInstallation(ctx, sqlc.GetPoolAssignmentInstallationParams{OrgID: scope.OrganizationID, ID: target.ID, DeviceID: target.DeviceID}); err != nil {
			return fleeterror.NewFailedPreconditionError("hashboard installation changed before adoption")
		}
	}
	n, err := q.AdoptPoolConfigurationAssignment(ctx, sqlc.AdoptPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID, DeviceID: target.DeviceID, ConfigurationID: configID, RevisionID: revisionID})
	if err != nil {
		return storageError(err)
	}
	return targetMutationResult(n, nil)
}
