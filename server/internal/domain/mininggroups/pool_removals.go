package mininggroups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

const removalCompleted = "completed"

func (s *Service) WithPoolRemovals(checker *poolapplications.Checker) *Service {
	s.removals = checker
	checker.WithBoardRemovals(s)
	return s
}

func (s *Service) ReviewBoardRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) error {
	row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID})
	if err != nil {
		return err
	}
	observed, err := s.call(ctx, target.DeviceID, sdk.MiningGroupRequest{Action: "observe"})
	if err != nil {
		return err
	}
	if !observed.Supported || !observed.Enabled || observed.Pending || observed.Suspended {
		return fleeterror.NewFailedPreconditionError("mining groups must be enabled and settled before clearing board pools")
	}
	// Preflight must not inherit the previous operation's freshness selection.
	row.AttemptID = uuid.NullUUID{}
	desired := map[string]string{}
	cutover, err := q.HasPoolConfigurationCutover(ctx, row.OrganizationID)
	if err != nil {
		return err
	}
	if !cutover {
		legacy, err := q.ListUnreplacedHashboardPoolIntents(ctx, sqlc.ListUnreplacedHashboardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
		if err != nil {
			return err
		}
		for _, intent := range legacy {
			desired[intent.MacAddress] = intent.PoolRevisionID
		}
	}
	intents, err := q.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: p.Scope.OrganizationID, DeviceID: target.DeviceID})
	if err != nil {
		return err
	}
	for _, i := range intents {
		desired[i.MacAddress] = i.GroupSnapshotID.String
	}
	if err := s.overlayClearedIntents(ctx, q, row, desired, nil); err != nil {
		return err
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: []int64{target.ID}})
	if err != nil {
		return err
	}
	if len(boards) != 1 || !boards[0].Ready || boards[0].DeviceID != target.DeviceID {
		return fleeterror.NewFailedPreconditionError("board installation is unavailable")
	}
	desired[boards[0].MacAddress] = poolconfigurations.ClearedPoolIntent(p.Scope.AccountID)
	snapshot, err := s.plannerSnapshot(ctx, q, row, observed, desired)
	if err != nil {
		if errors.Is(err, errDefaultSettingsUnavailable) {
			return fleeterror.NewFailedPreconditionError(defaultReadBlockedMessage(err))
		}
		return err
	}
	// Probe runtime readback before admitting changes. A false result is normal
	// while pools are still configured; an unavailable API must block clearing.
	found := false
	for _, group := range observed.Groups {
		if slices.Contains(group.MACs, boards[0].MacAddress) {
			// Firmware does not expose hosted-group diagnostics for the default
			// group. Moving a board just to discover support would mutate before
			// preflight. Keep assignment-only removal available for this scope.
			if group.ID == "default" && p.ActorID == 0 {
				// Automatic ownership policy authorizes moving only the selected
				// board into a stopped, pool-free group. That group's runtime
				// readback is verified after transfer; the default is unchanged.
				found = true
				continue
			}
			if group.ID == "default" {
				return fleeterror.NewUnimplementedError("verified pool clearing is unavailable for boards in the default mining group")
			}
			if _, err := s.call(ctx, target.DeviceID, sdk.MiningGroupRequest{Action: "verify_cleared", GroupID: group.ID, MACs: []string{boards[0].MacAddress}}); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return fleeterror.NewFailedPreconditionError("selected board mining-group membership is unavailable")
	}
	return ValidateAssignment(desired, snapshot)
}

func (s *Service) BeginBoardRemoval(ctx context.Context, p poolconfigurations.Principal, r sqlc.GetPoolRemovalRow, proof poolconfigurations.CredentialProof) error {
	if s.removals == nil {
		return fleeterror.NewUnavailableErrorf("hashboard pool clearing unavailable")
	}
	var binding poolapplications.Binding
	if err := decode([]byte(r.ReviewedBinding), &binding); err != nil {
		return err
	}
	if binding.Board == nil {
		return fmt.Errorf("missing reviewed board")
	}
	// Controller reads need a committed observation admission. The workflow
	// callback below only records database state; its attempt is not yet visible
	// to the plugin's admission guard. Revalidate binding and authority in both
	// transactions so a change between preflight and admission cannot slip through.
	err := s.writes.WithHostObservation(ctx, p.Scope.OrganizationID, r.DeviceID, binding.Device.Epoch, func(ctx context.Context) error {
		return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			if err := s.removals.ValidateRemoval(ctx, q, p, r, true); err != nil {
				return err
			}
			if err := proof(ctx, q); err != nil {
				return err
			}
			return s.writes.WithBindingObservation(ctx, r.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
				target := poolconfigurations.AssignmentTarget{Kind: r.TargetKind, ID: r.TargetID, DeviceID: r.DeviceID}
				return s.ReviewBoardRemoval(ctx, q, p, target)
			})
		})
	})
	if err != nil {
		return err
	}
	return s.writes.BeginPoolWorkflow(ctx, p.Scope.OrganizationID, r.DeviceID, func(ctx context.Context, q sqlc.Querier, attempt uuid.UUID, epoch int64) error {
		if err := s.removals.ValidateRemoval(ctx, q, p, r, true); err != nil {
			return err
		}
		if err := proof(ctx, q); err != nil {
			return err
		}
		n, err := q.AcceptPoolRemoval(ctx, sqlc.AcceptPoolRemovalParams{RemovalID: r.RemovalID, PhysicalAttemptID: uuid.NullUUID{UUID: attempt, Valid: true}})
		if err != nil {
			return err
		}
		if n != 1 {
			return fleeterror.NewFailedPreconditionError("removal changed; review again")
		}
		row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: p.Scope.OrganizationID, DeviceID: r.DeviceID})
		if err != nil {
			return err
		}
		row.OperationID = uuid.NullUUID{UUID: r.RemovalID, Valid: true}
		row.AttemptID = uuid.NullUUID{UUID: attempt, Valid: true}
		row.ActorID = sql.NullInt64{Int64: p.ActorID, Valid: p.ActorID > 0}
		row.OperationKind = "pools"
		row.WriteEpoch = epoch
		row.Status = "pending"
		row.Message = "Clearing selected hashboard pools"
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
		row.Revision++
		row.RetryCount = 0
		row.TransferFailureCount = 0
		row.NextReconcileAt = time.Now().UTC()
		device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: p.Scope.OrganizationID, ID: r.DeviceID})
		if err != nil {
			return err
		}
		if err := recordAcceptedActivity(ctx, q, row, device.DeviceIdentifier, device.SiteID, "", []int64{binding.Board.HashboardID}); err != nil {
			return err
		}
		return save(ctx, q, row)
	})
}

func (s *Service) workflowRemoval(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) (context.Context, poolconfigurations.Principal, sqlc.GetPoolRemovalRow, error) {
	meta, err := q.GetBoardPoolRemovalAttempt(ctx, sqlc.GetBoardPoolRemovalAttemptParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID})
	if err != nil {
		return ctx, poolconfigurations.Principal{}, sqlc.GetPoolRemovalRow{}, err
	}
	if meta.ActorID == 0 {
		ctx = poolconfigurations.WithLifecycleTransition(ctx, meta.RemovalID)
	}
	actorCtx, err := poolActorContext(ctx, row.OrganizationID, meta.ActorID, meta.ExternalAccountID)
	if err != nil {
		return ctx, poolconfigurations.Principal{}, sqlc.GetPoolRemovalRow{}, err
	}
	p, err := s.pools.AuthorizePoolConfiguration(actorCtx, q, meta.ExternalAccountID, poolconfigurations.Read)
	if err != nil {
		return actorCtx, p, sqlc.GetPoolRemovalRow{}, err
	}
	r, err := q.GetPoolRemoval(actorCtx, sqlc.GetPoolRemovalParams{RemovalID: meta.RemovalID, OrganizationID: row.OrganizationID, ActorID: meta.ActorID, AccountID: sql.NullInt64{Int64: p.Scope.AccountID, Valid: p.Scope.AccountID != 0}})
	return actorCtx, p, r, err
}

func (s *Service) authorizeRemovalWorkflow(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, boards []poolconfigurations.AssignmentTarget) (bool, error) {
	actorCtx, p, r, err := s.workflowRemoval(ctx, q, row)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if s.removals == nil {
		return true, fmt.Errorf("removal coordinator unavailable")
	}
	if err := s.removals.ValidateRemoval(actorCtx, q, p, r, true); err != nil {
		return true, err
	}
	for _, b := range boards {
		if _, err := s.pools.AuthorizePoolEquipment(actorCtx, q, p.Scope, p.ActorID, b); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (s *Service) overlayClearedIntents(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, desired map[string]string, affected []string) error {
	removals, err := q.ListClearedBoardPoolIntents(ctx, sqlc.ListClearedBoardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(removals))
	for _, r := range removals {
		ids = append(ids, r.TargetID)
	}
	if len(ids) == 0 {
		return nil
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, InstallationIds: ids})
	if err != nil {
		return err
	}
	byInstallation := make(map[int64]sqlc.ListPoolSelectionBoardsRow, len(boards))
	for _, board := range boards {
		byInstallation[board.InstallationID] = board
	}
	for _, r := range removals {
		var saved poolapplications.Binding
		if err := decode([]byte(r.ReviewedBinding), &saved); err != nil {
			return err
		}
		if saved.Board == nil || saved.Board.MAC != r.MacAddress {
			if r.State == removalCompleted {
				continue
			}
			return fmt.Errorf("cleared board identity changed")
		}
		board, found := byInstallation[r.TargetID]
		if !found || board.DeviceID != row.DeviceID || board.MacAddress != saved.Board.MAC {
			if r.State == removalCompleted {
				continue
			}
			return fmt.Errorf("cleared board installation changed")
		}
		if (r.PhysicalAttemptID == row.AttemptID || slices.Contains(affected, r.MacAddress)) && !board.Ready {
			return fmt.Errorf("cleared board inventory is stale")
		}
		desired[r.MacAddress] = poolconfigurations.ClearedPoolIntent(r.AccountID.Int64)
	}
	return nil
}

// A verification failure retains the workflow reservation and is safe to
// observe again. It must be committed as progress, not rolled back as an error.
type boardRemovalVerificationPending struct {
	removalID uuid.UUID
	code      string
}

func (e *boardRemovalVerificationPending) Error() string {
	if e.code == "runtime_pools_remain" {
		return "Runtime pools are still present. Fleet will keep checking; the assignment was kept."
	}
	return "Pool clearing could not be verified. Fleet will keep checking; the assignment was kept."
}

func (s *Service) finishBoardRemoval(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	actorCtx, p, r, err := s.workflowRemoval(ctx, q, row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.removals.ValidateRemoval(actorCtx, q, p, r, true); err != nil {
		return err
	}
	var binding poolapplications.Binding
	if err := decode([]byte(r.ReviewedBinding), &binding); err != nil {
		return err
	}
	if binding.Board == nil {
		return fmt.Errorf("missing board binding")
	}
	var observed sdk.MiningGroupResult
	if err := decode(row.Observed, &observed); err != nil {
		return err
	}
	for _, group := range observed.Groups {
		if !slices.Contains(group.MACs, binding.Board.MAC) {
			continue
		}
		verified, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "verify_cleared", GroupID: group.ID, MACs: []string{binding.Board.MAC}})
		if err != nil || verified.PoolsCleared == nil {
			return &boardRemovalVerificationPending{removalID: r.RemovalID, code: "clear_verification_unavailable"}
		}
		if !*verified.PoolsCleared {
			return &boardRemovalVerificationPending{removalID: r.RemovalID, code: "runtime_pools_remain"}
		}
		return s.removals.FinishRemoval(actorCtx, q, p, r, false)
	}
	return fmt.Errorf("selected board group is unavailable")
}

// Called only after a fresh settled observation, in the same transaction that
// retires the workflow and releases its admission. Superseding old policy does
// not claim its pools were cleared and must not modify the new owner's intent.
func retireObsoleteAutomaticRemoval(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, removal sqlc.GetBoardPoolRemovalAttemptRow, pending bool) (bool, error) {
	if removal.ActorID != 0 || pending || row.Step.Valid || row.ControllerOperationID != "" {
		return false, nil
	}
	transition, err := q.GetPoolLifecycleTransition(ctx, removal.RemovalID)
	if err != nil {
		return false, err
	}
	owner, err := q.GetPoolLifecycleAllocation(ctx, sqlc.GetPoolLifecycleAllocationParams{OrganizationID: transition.OrganizationID, Kind: transition.AssetKind, AssetID: transition.AssetID, DeviceID: transition.DeviceID})
	if err != nil {
		return false, err
	}
	newer, err := q.PoolLifecycleHasNewerAssignment(ctx, transition.TransitionID)
	if err != nil {
		return false, err
	}
	if owner.OwnerAccountID == transition.OwnerAccountID && owner.Revision == transition.AllocationRevision && !newer {
		return false, nil
	}
	changed, err := q.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: transition.TransitionID, State: "superseded", ResultCode: "policy_replaced"})
	return changed == 1, err
}
