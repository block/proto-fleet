package poolconfigurations

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"strings"
	"unicode/utf8"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type WorkerNameChange struct {
	DeviceIdentifier, Before, After string
	DisplayName                     string
	HashboardID                     int64
	Mode                            string
}

// WorkerNameOptions selects an explicit suffix; it never changes the payout account.
type WorkerNameOptions struct {
	MACAddress bool
	Custom     string
}

func (o WorkerNameOptions) mode() string {
	if o.MACAddress {
		return "mac_address"
	}
	if o.Custom != "" {
		return "custom"
	}
	return "none"
}

func (o WorkerNameOptions) Name(mac string) (string, error) {
	if o.MACAddress {
		if o.Custom != "" {
			return "", fleeterror.NewInvalidArgumentError("choose one worker name option")
		}
		if _, err := net.ParseMAC(mac); err != nil {
			return "", fleeterror.NewInvalidArgumentError("equipment has no valid MAC address")
		}
		return sdk.MACWorkerName(mac), nil
	}
	if !validWorkerName(o.Custom) {
		return "", fleeterror.NewInvalidArgumentError("invalid worker name")
	}
	return o.Custom, nil
}

type workerNameResolver func(sqlc.Querier, Principal) ([]AssignmentTarget, map[AssignmentTarget]string, error)

// Worker names are reviewed with each miner's current configuration. Reviewing
// never changes equipment metadata; confirmation saves metadata and canonical
// intent in the same transaction. A mixed selection has one review per config.
func (s *Service) ReviewWorkerNames(ctx context.Context, names map[string]string) ([]Review, error) {
	if len(names) == 0 || len(names) > 10000 {
		return nil, fleeterror.NewInvalidArgumentError("select between 1 and 10000 worker names")
	}
	identifiers := make([]string, 0, len(names))
	for identifier, name := range names {
		if identifier == "" || !validWorkerName(name) {
			return nil, fleeterror.NewInvalidArgumentError("worker names must contain at most 100 characters without whitespace or controls")
		}
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	resolve := func(q sqlc.Querier, p Principal) ([]AssignmentTarget, map[AssignmentTarget]string, error) {
		rows, err := q.GetOrgDeviceIDsWithIdentifiers(ctx, sqlc.GetOrgDeviceIDsWithIdentifiersParams{OrgID: p.Scope.OrganizationID, DeviceIdentifiers: identifiers})
		if err != nil {
			return nil, nil, err
		}
		if len(rows) != len(identifiers) {
			return nil, nil, fleeterror.NewForbiddenError("selected equipment is unavailable")
		}
		targets := make([]AssignmentTarget, len(rows))
		desired := make(map[AssignmentTarget]string, len(rows))
		for i, row := range rows {
			targets[i] = AssignmentTarget{Kind: TargetDevice, ID: row.ID, DeviceID: row.ID}
			desired[targets[i]] = names[row.DeviceIdentifier]
		}
		return targets, desired, nil
	}
	return s.reviewWorkerNames(ctx, resolve, "")
}

func (s *Service) ReviewBoardWorkerNames(ctx context.Context, selection BoardSelection, options WorkerNameOptions) ([]Review, int, error) {
	if len(selection.DeviceIDs) != 0 {
		return nil, 0, fleeterror.NewInvalidArgumentError("select hashboards or modules")
	}
	count := 0
	resolve := func(q sqlc.Querier, p Principal) ([]AssignmentTarget, map[AssignmentTarget]string, error) {
		rows, err := resolveBoards(ctx, q, p, selection, true)
		if err != nil {
			return nil, nil, err
		}
		count = len(rows)
		targets := make([]AssignmentTarget, len(rows))
		names := make(map[AssignmentTarget]string, len(rows))
		for i, row := range rows {
			targets[i] = AssignmentTarget{Kind: TargetInstallation, ID: row.InstallationID, DeviceID: row.DeviceID}
			name, err := options.Name(row.MacAddress)
			if err != nil {
				return nil, nil, err
			}
			names[targets[i]] = name
		}
		return targets, names, nil
	}
	reviews, err := s.reviewWorkerNames(ctx, resolve, options.mode())
	return reviews, count, err
}

func (s *Service) reviewWorkerNames(ctx context.Context, resolve workerNameResolver, mode string) ([]Review, error) {
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
		var deviceIDs, installationIDs []int64
		for _, target := range targets {
			if target.Kind == TargetInstallation {
				installationIDs = append(installationIDs, target.ID)
			} else {
				deviceIDs = append(deviceIDs, target.ID)
			}
		}
		byTarget := make(map[AssignmentTarget]sqlc.ListPoolConfigurationAssignmentsForDevicesRow, len(targets))
		if len(deviceIDs) > 0 {
			assignments, err := q.ListPoolConfigurationAssignmentsForDevices(ctx, sqlc.ListPoolConfigurationAssignmentsForDevicesParams{OrganizationID: p.Scope.OrganizationID, DeviceIds: deviceIDs})
			if err != nil {
				return nil, err
			}
			for _, a := range assignments {
				byTarget[AssignmentTarget{Kind: TargetDevice, ID: a.TargetID}] = a
			}
		}
		if len(installationIDs) > 0 {
			assignments, err := q.ListPoolConfigurationAssignmentsForBoards(ctx, sqlc.ListPoolConfigurationAssignmentsForBoardsParams{OrganizationID: p.Scope.OrganizationID, InstallationIds: installationIDs})
			if err != nil {
				return nil, err
			}
			for _, a := range assignments {
				byTarget[AssignmentTarget{Kind: TargetInstallation, ID: a.TargetID}] = sqlc.ListPoolConfigurationAssignmentsForDevicesRow(a)
			}
		}
		groups := map[string][]AssignmentTarget{}
		for _, target := range targets {
			assignment, ok := byTarget[AssignmentTarget{Kind: target.Kind, ID: target.ID}]
			if ok && (assignment.ConfigurationID == "" || assignment.RetiredAt.Valid) && assignment.WorkerNameSet && sameWorkerName(assignment.WorkerName, desired[target]) && (target.Kind != TargetInstallation || assignment.WorkerNameMode == mode) {
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
				if assignment := byTarget[AssignmentTarget{Kind: target.Kind, ID: target.ID}]; assignment.WorkerNameSet && sameWorkerName(assignment.WorkerName, desired[target]) && (target.Kind != TargetInstallation || assignment.WorkerNameMode == mode) {
					retry, err := workerNameRetryEligible(ctx, q, p.Scope, target, config.RevisionID, desired[target])
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
			review, err := s.repository.reviewAssignment(ctx, q, p.Scope, p.ActorID, id, config.Version, selected, s.equipment, desired, mode)
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
		var binding workerNameBinding
		_ = json.Unmarshal([]byte(target.Binding), &binding)
		change := WorkerNameChange{DeviceIdentifier: binding.Device.DeviceIdentifier, Before: binding.EffectiveWorkerName, After: *target.DesiredWorkerName}
		if target.PreviousWorkerName != nil {
			change.Before = *target.PreviousWorkerName
		}
		if binding.Board != nil {
			change.HashboardID = binding.Board.HashboardID
			change.DisplayName = binding.Label
			change.Mode = target.DesiredWorkerNameMode
		}
		changes = append(changes, change)
	}
	return changes
}

func sameWorkerName(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

// Matching metadata is not evidence that the miner accepted the name. Only the
// current canonical assignment and its latest readback can authorize a reapply.
func workerNameRetryEligible(ctx context.Context, q sqlc.Querier, scope Scope, target AssignmentTarget, revisionID, desired string) (bool, error) {
	assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return false, storageError(err)
	}
	if assignment.RetiredAt.Valid {
		return false, nil
	}
	if !assignment.OperationID.Valid {
		changed, _, err := adoptedWorkerNameChange(ctx, q, scope, target, revisionID, desired)
		return changed, err
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
	inheritedChange := assignment.InheritedWorkerName.Valid && assignment.InheritedWorkerName.String != desired
	if !inheritedChange && !retryableOutcome(state, code) {
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
		if len(changes) == 0 {
			continue
		}
		var binding workerNameBinding
		if err := json.Unmarshal([]byte(item.Binding), &binding); err != nil {
			return fleeterror.NewInternalError("decode reviewed worker binding failed")
		}
		savedName := binding.Device.WorkerName
		explicit := binding.Device.WorkerNameSet
		if binding.Board != nil {
			if binding.Board.WorkerNameMode != item.DesiredWorkerNameMode {
				continue
			}
			savedName = binding.Board.WorkerName
			explicit = binding.Board.WorkerNameSet
		}
		if !explicit || !sameWorkerName(savedName, changes[0].After) {
			continue
		}
		target := item.Change.Target
		inheritedChange, _, err := adoptedWorkerNameChange(ctx, q, scope, target, revisionID, changes[0].After)
		if err != nil {
			return err
		}
		if inheritedChange {
			continue
		}
		if _, err := q.LockCurrentPoolConfigurationOperationTarget(ctx, sqlc.LockCurrentPoolConfigurationOperationTargetParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID}); err != nil {
			return storageError(err)
		}
		retry, err := workerNameRetryEligible(ctx, q, scope, target, revisionID, changes[0].After)
		if err != nil {
			return err
		}
		if !retry {
			return retryUnavailable()
		}
	}
	return nil
}

// The review owns only the worker fields of the equipment's private binding.
type workerNameBinding struct {
	EffectiveWorkerName string
	Label               string
	Device              sqlc.GetPoolConfigurationRuntimeRow
	Board               *struct {
		HashboardID     int64
		MAC, WorkerName string
		WorkerNameSet   bool
		WorkerNameMode  string
	}
}

func validWorkerName(name string) bool {
	return validPoolText(name, 400, false) && utf8.RuneCountInString(name) <= 100
}
