package mininggroups

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

const activationPoolRecovery = "Pool assignment could not follow activation. Remove the container assignment keeping device settings, then review and assign the configuration to the hashboards."

type activationPoolBoard struct {
	Binding   poolapplications.BoardBinding
	Ownership string
}

// Only immutable references and credential-free bindings enter the journal.
// Encrypted execution material lives in the existing group snapshot store.
type activationPoolCapture struct {
	Source     sqlc.GetPoolConfigurationAssignmentRow
	Runtime    sqlc.GetPoolConfigurationRuntimeRow
	Ownership  string
	SnapshotID string
	Boards     []activationPoolBoard
}

func (s *Service) captureActivationPools(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult) (activationPoolCapture, error) {
	capture := activationPoolCapture{}
	source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: row.DeviceID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return capture, err
	}
	if err == nil && !source.RetiredAt.Valid {
		// Without a pre-activation selection, current membership cannot prove which
		// physical installations inherited the former whole-container assignment.
		if observed.Enabled {
			return capture, fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
		if observed.Pending || observed.Suspended {
			return capture, fleeterror.NewFailedPreconditionError("Wait for the controller to settle before enabling mining groups")
		}
		if source.AccountID.Valid {
			return capture, fleeterror.NewForbiddenError("Container pool assignment belongs to another configuration owner")
		}
		frozen, err := q.PoolConfigurationWritersFrozen(ctx, row.OrganizationID)
		if err != nil {
			return capture, err
		}
		if frozen {
			return capture, fleeterror.NewFailedPreconditionError("Pool configuration maintenance is in progress")
		}
		if err := devicewrite.RequireNoConflictingPoolWrites(ctx, q, row.OrganizationID, row.DeviceID); err != nil {
			return capture, err
		}
		scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID}
		capture.Source = source
		capture.Ownership, err = s.pools.AuthorizePoolEquipment(ctx, q, scope, row.ActorID.Int64, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID})
		if err != nil {
			return capture, err
		}
		capture.Runtime, err = q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: row.OrganizationID, ID: row.DeviceID})
		if err != nil {
			return capture, err
		}
		revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: row.OrganizationID, AccountID: source.AccountID, ConfigurationID: source.ConfigurationID, RevisionID: source.RevisionID})
		if err != nil {
			return capture, err
		}
		slots, err := poolconfigurations.DecodeSlots(revision.Slots)
		if err != nil {
			return capture, err
		}
		var usernames []string
		if source.OperationID.Valid {
			work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: row.OrganizationID, OperationID: source.OperationID.String, TargetKind: poolconfigurations.TargetDevice, TargetID: row.DeviceID})
			if err != nil {
				return capture, err
			}
			if work.State == "pending" || work.State == statusApplying || work.ResultCode == "write_uncertain" {
				return capture, fleeterror.NewFailedPreconditionError("Wait for the container pool update to finish before enabling mining groups")
			}
			var binding poolapplications.Binding
			if err := decode([]byte(work.ReviewedBinding), &binding); err != nil {
				return capture, err
			}
			if binding.Ownership != capture.Ownership || !slices.Equal(binding.Device.IdentityKeys, capture.Runtime.IdentityKeys) || len(binding.Usernames) != len(slots) {
				return capture, fleeterror.NewFailedPreconditionError(activationPoolRecovery)
			}
			usernames = binding.Usernames
		} else {
			workerID, workerName, err := s.sharedReviewer.WorkerInputs(ctx, q, scope, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID}, capture.Runtime)
			if err != nil {
				return capture, err
			}
			for _, slot := range slots {
				username, err := poolconfigurations.RenderWorker(slot.Username, slot.WorkerTemplate, workerID, workerName)
				if err != nil {
					return capture, err
				}
				usernames = append(usernames, username)
			}
		}
		owners, err := q.ListPoolEquipmentModuleOwners(ctx, sqlc.ListPoolEquipmentModuleOwnersParams{OrgID: row.OrganizationID, ID: row.DeviceID})
		if err != nil {
			return capture, err
		}
		var boardIDs []int64
		for _, owner := range owners {
			if owner.HashboardID > 0 {
				boardIDs = append(boardIDs, owner.HashboardID)
			}
		}
		slices.Sort(boardIDs)
		boardIDs = slices.Compact(boardIDs)
		boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, HashboardIds: boardIDs})
		if err != nil {
			return capture, err
		}
		if len(boards) == 0 || len(boards) != len(boardIDs) {
			return capture, fleeterror.NewFailedPreconditionError("Refresh complete hashboard inventory before enabling mining groups")
		}
		targets := make([]poolconfigurations.AssignmentTarget, len(boards))
		seenMACs := make(map[string]bool, len(boards))
		for i, board := range boards {
			if seenMACs[board.MacAddress] {
				return capture, fleeterror.NewFailedPreconditionError("Hashboard identity is ambiguous; refresh the controller inventory")
			}
			seenMACs[board.MacAddress] = true
			if !board.Ready || board.DeviceID != row.DeviceID {
				return capture, fleeterror.NewFailedPreconditionError("Refresh complete hashboard inventory before enabling mining groups")
			}
			targets[i] = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: board.InstallationID, DeviceID: row.DeviceID}
			if _, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: targets[i].Kind, TargetID: targets[i].ID}); !errors.Is(err, sql.ErrNoRows) {
				if err != nil {
					return capture, err
				}
				return capture, fleeterror.NewFailedPreconditionError(activationPoolRecovery)
			}
		}
		ownership, err := s.pools.AuthorizePoolEquipmentBatch(ctx, q, scope, row.ActorID.Int64, targets)
		if err != nil {
			return capture, err
		}
		for i, board := range boards {
			owner, ok := ownership[i]
			if !ok {
				return capture, fleeterror.NewForbiddenError("Hashboard ownership changed")
			}
			capture.Boards = append(capture.Boards, activationPoolBoard{Binding: poolapplications.BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}, Ownership: owner})
		}
		for i := range slots {
			slots[i].Username = usernames[i]
			slots[i].WorkerTemplate = ""
		}
		encoded, err := encode(slots)
		if err != nil {
			return capture, err
		}
		capture.SnapshotID = uuid.NewSHA1(row.OperationID.UUID, []byte("activation-pools")).String()
		if err := q.CreatePoolGroupSnapshot(ctx, sqlc.CreatePoolGroupSnapshotParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, SnapshotID: capture.SnapshotID, ConfigurationID: source.ConfigurationID, RevisionID: source.RevisionID, Slots: encoded}); err != nil {
			return capture, err
		}
	}
	encoded, err := encode(capture)
	if err != nil {
		return capture, err
	}
	return capture, q.CreateMiningGroupPoolHandoff(ctx, sqlc.CreateMiningGroupPoolHandoffParams{OrganizationID: row.OrganizationID, OperationID: row.OperationID.UUID, DeviceID: row.DeviceID, Captured: encoded})
}

func (s *Service) activationPoolCapture(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) (activationPoolCapture, bool, error) {
	var capture activationPoolCapture
	journal, err := q.GetMiningGroupPoolHandoff(ctx, sqlc.GetMiningGroupPoolHandoffParams{OrganizationID: row.OrganizationID, OperationID: row.OperationID.UUID, DeviceID: row.DeviceID})
	if errors.Is(err, sql.ErrNoRows) {
		// Pre-upgrade activations have no trustworthy frozen installation selection.
		source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: row.DeviceID})
		if errors.Is(err, sql.ErrNoRows) || err == nil && source.RetiredAt.Valid {
			return capture, true, nil
		}
		if err != nil {
			return capture, false, err
		}
		return capture, false, fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	if err != nil {
		return capture, false, err
	}
	if err := decode(journal.Captured, &capture); err != nil {
		return capture, false, err
	}
	return capture, journal.CompletedAt.Valid || capture.SnapshotID == "", nil
}

func (s *Service) validateActivationPools(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, capture activationPoolCapture, transferred bool) error {
	scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID}
	if !transferred {
		source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: row.DeviceID})
		if err != nil {
			return err
		}
		if source.Version != capture.Source.Version || source.RetiredAt.Valid || source.ConfigurationID != capture.Source.ConfigurationID || source.RevisionID != capture.Source.RevisionID || source.AccountID != capture.Source.AccountID {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
	}
	runtime, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: row.OrganizationID, ID: row.DeviceID})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(runtime, capture.Runtime) {
		return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	actorCtx, err := poolActorContext(ctx, row.OrganizationID, row.ActorID.Int64, "")
	if err != nil {
		return err
	}
	owner, err := s.pools.AuthorizePoolEquipment(actorCtx, q, scope, row.ActorID.Int64, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID})
	if err != nil {
		return err
	}
	if owner != capture.Ownership {
		return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	targets := make([]poolconfigurations.AssignmentTarget, len(capture.Boards))
	ids := make([]int64, len(capture.Boards))
	for i, board := range capture.Boards {
		ids[i] = board.Binding.InstallationID
		targets[i] = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: ids[i], DeviceID: row.DeviceID}
	}
	owners, err := s.pools.AuthorizePoolEquipmentBatch(actorCtx, q, scope, row.ActorID.Int64, targets)
	if err != nil {
		return err
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, InstallationIds: ids})
	if err != nil {
		return err
	}
	if len(boards) != len(capture.Boards) {
		return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
	}
	for i, board := range boards {
		saved := capture.Boards[i]
		current := poolapplications.BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}
		if !board.Ready || board.DeviceID != row.DeviceID || saved.Binding != current || owners[i] != saved.Ownership {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
		assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: board.InstallationID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if transferred {
			if err != nil || assignment.RetiredAt.Valid || assignment.Version != 1 || assignment.OperationID.Valid || assignment.ConfigurationID != capture.Source.ConfigurationID || assignment.RevisionID != capture.Source.RevisionID {
				return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
			}
		} else if err == nil {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
	}
	return nil
}

// Handoff is database-only, after a settled activation observation. Retirement,
// installation intent, unverified status, and provenance commit together.
func (s *Service) finishActivationPools(ctx context.Context, q sqlc.Querier, row *sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult, capture activationPoolCapture) error {
	scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID}
	repository := poolconfigurations.NewRepository(s.secrets)
	if err := repository.RetireAssignment(ctx, q, scope, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID}, capture.Source.Version); err != nil {
		return err
	}
	for _, board := range capture.Boards {
		// A transfer preserves the assigned revision even when the library has since
		// advanced. It never creates physical work or overwrites a prior board row.
		n, err := q.AdoptPoolConfigurationAssignment(ctx, sqlc.AdoptPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: board.Binding.InstallationID, DeviceID: row.DeviceID, ConfigurationID: capture.Source.ConfigurationID, RevisionID: capture.Source.RevisionID})
		if err != nil {
			return err
		}
		if n != 1 {
			return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
		}
		if err := q.AdoptPoolGroupIntent(ctx, sqlc.AdoptPoolGroupIntentParams{OrganizationID: row.OrganizationID, InstallationID: board.Binding.InstallationID, GroupSnapshotID: capture.SnapshotID}); err != nil {
			return err
		}
		// The controller observation predates the lock gap. Adopt the durable
		// assignment evidence now, but verify only from the next fresh readback.
		if err := q.SavePoolConfigurationObservation(ctx, sqlc.SavePoolConfigurationObservationParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: board.Binding.InstallationID, AssignmentVersion: 1, State: "applied_unverified", ResultCode: "readback_unavailable"}); err != nil {
			return err
		}
	}
	bindings := map[string]binding{}
	if err := decode(row.Bindings, &bindings); err != nil {
		return err
	}
	// Only default's proven inherited identity receives this exception. Other
	// controller groups are never claimed by a URL or name match.
	for _, group := range observed.Groups {
		if group.ID == "default" {
			if _, exists := bindings[group.ID]; exists {
				return fleeterror.NewFailedPreconditionError(activationPoolRecovery)
			}
			bindings[group.ID] = binding{ConfigID: capture.SnapshotID, Configured: true, Inherited: true}
		}
	}
	encoded, err := encode(bindings)
	if err != nil {
		return err
	}
	row.Bindings = encoded
	if err := q.CompleteMiningGroupPoolHandoff(ctx, sqlc.CompleteMiningGroupPoolHandoffParams{OrganizationID: row.OrganizationID, OperationID: row.OperationID.UUID, DeviceID: row.DeviceID}); err != nil {
		return err
	}
	return save(ctx, q, *row)
}

// Called under the observation transaction's binding and authority locks, after
// validating the adopted assignments. Never verify using pre-handoff readback.
func (s *Service) observeActivationPools(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult, capture activationPoolCapture) error {
	snapshot, err := q.GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, SnapshotID: capture.SnapshotID})
	if err != nil {
		return err
	}
	slots, err := poolconfigurations.DecodeSlots(snapshot.Slots)
	if err != nil {
		return err
	}
	expected := make([]poolconfigurations.PoolReading, len(slots))
	for i, slot := range slots {
		expected[i] = poolconfigurations.PoolReading{Priority: int32(i), URL: slot.URL, Username: slot.Username}
	} // #nosec G115 -- at most three pools.
	for _, board := range capture.Boards {
		state, code, _ := poolapplications.CheckBoardPools(observed, board.Binding.MAC, expected)
		if err := q.SavePoolConfigurationObservation(ctx, sqlc.SavePoolConfigurationObservationParams{OrganizationID: row.OrganizationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: board.Binding.InstallationID, AssignmentVersion: 1, State: state, ResultCode: code}); err != nil {
			return err
		}
	}
	return nil
}

// Return the settled observation to Reconcile without upgrading its shared
// organization lock while retaining device locks. No physical write occurs in
// the observation transaction on this path; it can safely roll back its locks.
type activationPoolHandoffReady struct {
	row        sqlc.ContainerMiningGroupState
	observed   sdk.MiningGroupResult
	observedAt time.Time
}

func (*activationPoolHandoffReady) Error() string { return "activation pool handoff ready" }

func (s *Service) commitActivationPoolHandoff(ctx context.Context, ready *activationPoolHandoffReady) (bool, error) {
	applied := false
	err := s.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		applied = false
		if _, err := q.LockPoolConfigurationImportOrganization(ctx, ready.row.OrganizationID); err != nil {
			return err
		}
		return s.writes.WithBindingObservation(ctx, ready.row.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: ready.row.OrganizationID, DeviceID: ready.row.DeviceID})
			if err != nil {
				return err
			}
			// Another reconciler or UI observation may have advanced the journal while
			// locks were released. Discard the observation and retry on the next pass.
			if !reflect.DeepEqual(row, ready.row) || time.Since(ready.observedAt) > reconciliationTimeout {
				return nil
			}
			capture, done, err := s.activationPoolCapture(ctx, q, row)
			if err != nil || done {
				return err
			}
			if err := s.validateActivationPools(ctx, q, row, capture, false); err != nil {
				return err
			}
			// Activation authority may be revoked independently of pool-update authority.
			target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{OrgID: row.OrganizationID, DeviceIdentifier: capture.Runtime.DeviceIdentifier})
			if err != nil {
				return err
			}
			if err := authorize(ctx, q, row.ActorID.Int64, row.OrganizationID, target.SiteID, authz.PermMinerActivateMiningGroups, authz.PermMinerStartMining, authz.PermMinerStopMining); err != nil {
				return err
			}
			row.Observed, err = encode(ready.observed)
			if err != nil {
				return err
			}
			row.ObservedAt = sql.NullTime{Time: ready.observedAt, Valid: true}
			if err := s.finishActivationPools(ctx, q, &row, ready.observed, capture); err != nil {
				return err
			}
			applied = true
			return nil
		})
	})
	return applied, err
}
