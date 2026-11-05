package poolconfigurations

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type WorkerNameChange struct{ DeviceIdentifier, Before, After string }

// Worker names are reviewed with each miner's current configuration. Reviewing
// never changes equipment metadata; confirmation saves metadata and canonical
// intent in the same transaction. A mixed selection has one review per config.
func (s *Service) ReviewWorkerNames(ctx context.Context, names map[string]string) ([]Review, error) {
	if len(names) == 0 || len(names) > 10000 {
		return nil, fleeterror.NewInvalidArgumentError("select between 1 and 10000 worker names")
	}
	identifiers := make([]string, 0, len(names))
	for identifier, name := range names {
		if identifier == "" || strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 {
			return nil, fleeterror.NewInvalidArgumentError("worker names must contain 1 to 100 characters")
		}
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	resolve := func(q sqlc.Querier, p Principal) ([]AssignmentTarget, map[int64]string, error) {
		rows, err := q.GetOrgDeviceIDsWithIdentifiers(ctx, sqlc.GetOrgDeviceIDsWithIdentifiersParams{OrgID: p.Scope.OrganizationID, DeviceIdentifiers: identifiers})
		if err != nil {
			return nil, nil, err
		}
		if len(rows) != len(identifiers) {
			return nil, nil, fleeterror.NewForbiddenError("selected equipment is unavailable")
		}
		targets := make([]AssignmentTarget, len(rows))
		desired := make(map[int64]string, len(rows))
		for i, row := range rows {
			targets[i] = AssignmentTarget{Kind: TargetDevice, ID: row.ID, DeviceID: row.ID}
			desired[row.ID] = names[row.DeviceIdentifier]
		}
		return targets, desired, nil
	}
	if err := s.refreshReadiness(ctx, "", Read, func(q sqlc.Querier, p Principal) ([]AssignmentTarget, error) {
		targets, _, err := resolve(q, p)
		return targets, err
	}); err != nil {
		return nil, err
	}
	return withAuthority(ctx, s, "", Read, func(q sqlc.Querier, p Principal) ([]Review, error) {
		if err := authorizeWorkerNames(ctx, q, p.Scope, p.ActorID); err != nil {
			return nil, err
		}
		if _, err := q.LockPoolConfigurationImportOrganization(ctx, p.Scope.OrganizationID); err != nil {
			return nil, err
		}
		frozen, err := q.PoolConfigurationWritersFrozen(ctx, p.Scope.OrganizationID)
		if err != nil {
			return nil, err
		}
		if frozen {
			return nil, fleeterror.NewFailedPreconditionError("Pool configuration maintenance is in progress.")
		}
		targets, desired, err := resolve(q, p)
		if err != nil {
			return nil, err
		}
		deviceIDs := make([]int64, len(targets))
		for i, target := range targets {
			deviceIDs[i] = target.ID
		}
		assignments, err := q.ListPoolConfigurationAssignmentsForDevices(ctx, sqlc.ListPoolConfigurationAssignmentsForDevicesParams{OrganizationID: p.Scope.OrganizationID, DeviceIds: deviceIDs})
		if err != nil {
			return nil, err
		}
		byDevice := make(map[int64]sqlc.ListPoolConfigurationAssignmentsForDevicesRow, len(assignments))
		for _, assignment := range assignments {
			byDevice[assignment.TargetID] = assignment
		}
		groups := map[string][]AssignmentTarget{}
		for _, target := range targets {
			assignment, ok := byDevice[target.ID]
			if ok && (assignment.ConfigurationID == "" || assignment.RetiredAt.Valid) && sameWorkerName(assignment.WorkerName, desired[target.ID]) {
				continue
			}
			if !ok || assignment.ConfigurationID == "" || assignment.RetiredAt.Valid {
				return nil, fleeterror.NewFailedPreconditionError("Assign a pool configuration before changing worker names.")
			}
			if assignment.AccountID != p.Scope.account() {
				return nil, fleeterror.NewForbiddenError("equipment belongs to another configuration owner")
			}
			groups[assignment.ConfigurationID] = append(groups[assignment.ConfigurationID], target)
		}
		ids := make([]string, 0, len(groups))
		for id := range groups {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		reviews := make([]Review, 0, len(ids))
		for _, id := range ids {
			config, err := s.repository.Get(ctx, q, p.Scope, id)
			if err != nil {
				return nil, err
			}
			selected := make([]AssignmentTarget, 0, len(groups[id]))
			for _, target := range groups[id] {
				if sameWorkerName(byDevice[target.ID].WorkerName, desired[target.ID]) {
					retry, err := workerNameRetryEligible(ctx, q, p.Scope, target, config.RevisionID)
					if err != nil {
						return nil, err
					}
					if !retry {
						continue
					}
				}
				selected = append(selected, target)
			}
			if len(selected) == 0 {
				continue
			}
			review, err := s.repository.reviewAssignment(ctx, q, p.Scope, p.ActorID, id, config.Version, selected, s.equipment, desired)
			if err != nil {
				return nil, err
			}
			reviews = append(reviews, review)
		}
		return reviews, nil
	})
}

func authorizeWorkerNames(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64) error {
	if scope.AccountID != 0 {
		return fleeterror.NewForbiddenError("worker naming requires operator access")
	}
	permissions, err := authz.LoadEffectiveForShare(ctx, q, actorID, scope.OrganizationID)
	if err != nil {
		return err
	}
	if !permissions.Has(authz.PermMinerUpdateWorkerName, authz.ResourceContext{}) {
		return fleeterror.NewForbiddenError("worker naming permission required")
	}
	return nil
}

func workerNameChanges(targets []reviewedTarget) []WorkerNameChange {
	var changes []WorkerNameChange
	for _, target := range targets {
		if target.DesiredWorkerName == nil {
			continue
		}
		var binding struct {
			Device sqlc.GetPoolConfigurationRuntimeRow
		}
		_ = json.Unmarshal([]byte(target.Binding), &binding)
		changes = append(changes, WorkerNameChange{DeviceIdentifier: binding.Device.DeviceIdentifier, Before: binding.Device.WorkerName, After: *target.DesiredWorkerName})
	}
	return changes
}

func sameWorkerName(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

// Matching metadata is not evidence that the miner accepted the name. Only the
// current canonical assignment and its latest readback can authorize a reapply.
func workerNameRetryEligible(ctx context.Context, q sqlc.Querier, scope Scope, target AssignmentTarget, revisionID string) (bool, error) {
	assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return false, storageError(err)
	}
	if assignment.RetiredAt.Valid || !assignment.OperationID.Valid {
		return false, nil
	}
	work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: scope.OrganizationID, OperationID: assignment.OperationID.String, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return false, storageError(err)
	}
	// Observations cannot authorize duplicate dispatch or clear uncertain writes.
	if work.ResultCode == resultWriteUncertain {
		return false, retryUnavailable()
	}
	if work.State == "pending" || work.State == "applying" || work.State == "superseded" {
		return false, nil
	}
	state, code, err := latestRetryOutcome(ctx, q, work)
	if err != nil {
		return false, err
	}
	if code == resultWriteUncertain {
		return false, retryUnavailable()
	}
	if !retryableOutcome(state, code) {
		return false, nil
	}
	// Retrying a name must not silently apply a newer configuration revision.
	if work.RevisionID != revisionID {
		return false, staleAssignment()
	}
	return true, nil
}

// Called after physical admission locks, both when freezing the review and when
// confirming it. Hold the operation outcome lock until canonical intent commits.
func checkWorkerNameRetries(ctx context.Context, q sqlc.Querier, scope Scope, revisionID string, targets []reviewedTarget) error {
	for _, item := range targets {
		changes := workerNameChanges([]reviewedTarget{item})
		if len(changes) == 0 || !sameWorkerName(changes[0].Before, changes[0].After) {
			continue
		}
		target := item.Change.Target
		if _, err := q.LockCurrentPoolConfigurationOperationTarget(ctx, sqlc.LockCurrentPoolConfigurationOperationTargetParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID}); err != nil {
			return storageError(err)
		}
		retry, err := workerNameRetryEligible(ctx, q, scope, target, revisionID)
		if err != nil {
			return err
		}
		if !retry {
			return retryUnavailable()
		}
	}
	return nil
}
