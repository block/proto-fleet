package poolconfigurations

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// An adopted assignment has no operation to retry. Its immutable snapshot is
// the evidence for whether an explicit worker-name application would change it.
func adoptedWorkerNameChange(ctx context.Context, q sqlc.Querier, scope Scope, target AssignmentTarget, revisionID, desired string) (bool, *string, error) {
	if target.Kind != TargetInstallation {
		return false, nil, nil
	}
	a, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, storageError(err)
	}
	if a.RetiredAt.Valid || a.OperationID.Valid {
		return false, nil, nil
	}
	frozen, err := q.GetAdoptedPoolBoardSnapshot(ctx, sqlc.GetAdoptedPoolBoardSnapshotParams{OrganizationID: scope.OrganizationID, TargetID: target.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, storageError(err)
	}
	revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: scope.OrganizationID, AccountID: a.AccountID, ConfigurationID: a.ConfigurationID, RevisionID: a.RevisionID})
	if err != nil {
		return false, nil, storageError(err)
	}
	slots, err := DecodeSlots(revision.Slots)
	if err != nil {
		return false, nil, err
	}
	actual, err := DecodeSlots(frozen)
	if err != nil {
		return false, nil, err
	}
	device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: scope.OrganizationID, ID: a.DeviceID})
	if err != nil {
		return false, nil, storageError(err)
	}
	if len(actual) != len(slots) {
		return false, nil, inheritedWorkerUnavailable()
	}
	var before *string
	different := a.InheritedWorkerName.Valid && a.InheritedWorkerName.String != desired
	for i, slot := range slots {
		if slot.ID != actual[i].ID {
			return false, nil, inheritedWorkerUnavailable()
		}
		username, err := RenderWorker(slot.Username, slot.WorkerTemplate, device.DeviceIdentifier, desired)
		if err != nil {
			return false, nil, err
		}
		different = different || username != actual[i].Username
		if strings.Contains(slot.WorkerTemplate, "{miner_name}") {
			name, ok := workerNameFromRendering(slot.Username, slot.WorkerTemplate, device.DeviceIdentifier, actual[i].Username)
			if !ok || before != nil && *before != name {
				return false, nil, inheritedWorkerUnavailable()
			}
			before = &name
		}
	}
	if different && a.RevisionID != revisionID {
		return false, nil, staleAssignment()
	}
	return different, before, nil
}

func inheritedWorkerUnavailable() error {
	return fleeterror.NewFailedPreconditionError("Inherited worker settings cannot be resolved. Review and assign the pool configuration again.")
}

// Recover only a value whose expansion exactly reproduces the known snapshot.
// Splitting on the template token avoids guessing at dots in payout usernames,
// literal prefixes/suffixes, or repeated placeholders.
func workerNameFromRendering(username, template, identifier, rendered string) (string, bool) {
	parts := strings.Split(template, "{miner_name}")
	count := len(parts) - 1
	if count == 0 {
		return "", false
	}
	if empty, err := RenderWorker(username, template, identifier, ""); err == nil && empty == rendered {
		return "", true
	}
	fixed := len(username) + 1
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], "{miner_id}", identifier)
		fixed += len(parts[i])
	}
	remaining := len(rendered) - fixed
	prefix := username + "." + parts[0]
	if remaining < 0 || remaining%count != 0 || !strings.HasPrefix(rendered, prefix) {
		return "", false
	}
	length := remaining / count
	if len(prefix)+length > len(rendered) {
		return "", false
	}
	name := rendered[len(prefix) : len(prefix)+length]
	expected, err := RenderWorker(username, template, identifier, name)
	return name, err == nil && expected == rendered
}
