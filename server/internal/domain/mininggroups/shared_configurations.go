package mininggroups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

func (s *Service) WithPoolConfigurations(repository *poolconfigurations.Repository, reviewer *poolapplications.Reviewer) *Service {
	s.sharedRepository = repository
	s.sharedReviewer = reviewer
	return s
}

// ExecutePoolConfiguration hands off all current targets on one controller in
// one transaction. Claims, encrypted snapshots and the physical workflow journal
// commit together, so a crash cannot leave partially admitted board work.
func (s *Service) ExecutePoolConfiguration(ctx context.Context, orgID int64, operationID string, deviceID int64) error {
	rows, err := s.queries.ListPoolGroupWork(ctx, sqlc.ListPoolGroupWorkParams{OrganizationID: orgID, OperationID: operationID, DeviceID: deviceID})
	if err != nil || len(rows) == 0 {
		return err
	}
	if s.sharedRepository == nil || s.sharedReviewer == nil {
		return fleeterror.NewInternalError("shared board applications are unavailable")
	}
	first, err := s.queries.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: orgID, OperationID: operationID, TargetKind: poolconfigurations.TargetInstallation, TargetID: rows[0].TargetID})
	if err != nil {
		return err
	}
	scope := poolconfigurations.Scope{OrganizationID: orgID, AccountID: first.AccountID.Int64}
	ctx, err = poolActorContext(ctx, orgID, first.ActorID, first.ExternalAccountID)
	if err != nil {
		return err
	}
	// An existing controller workflow retains exclusive admission; leave later
	// requests pending until it settles instead of turning contention into failure.
	state, err := s.queries.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	if active(state.Status) {
		return nil
	}
	// Admission checks durable authority and binding only. Reconciliation owns
	// the fresh controller observation before planning any hardware mutation.
	err = s.writes.BeginWorkflow(ctx, orgID, deviceID, func(ctx context.Context, q sqlc.Querier, attempt uuid.UUID, epoch int64) error {
		current, err := q.ListPoolGroupWork(ctx, sqlc.ListPoolGroupWorkParams{OrganizationID: orgID, OperationID: operationID, DeviceID: deviceID})
		if err != nil {
			return err
		}
		if len(current) == 0 {
			return errReplay
		}
		targets := make([]poolconfigurations.AssignmentTarget, len(current))
		works := make([]sqlc.GetPoolConfigurationWorkRow, len(current))
		for i, row := range current {
			targets[i] = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: row.TargetID, DeviceID: deviceID}
			works[i], err = q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: orgID, OperationID: operationID, TargetKind: targets[i].Kind, TargetID: targets[i].ID})
			if err != nil {
				return err
			}
		}
		slots, err := poolconfigurations.DecodeSlots(first.Slots)
		if err != nil {
			return err
		}
		endpoints := make([]poolconfigurations.Endpoint, len(slots))
		for i, slot := range slots {
			endpoints[i] = poolconfigurations.Endpoint{ID: slot.ID, URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, HasPassword: slot.HasPassword}
		}
		approvals, err := s.sharedReviewer.ReviewBindings(ctx, q, scope, first.ActorID, targets, endpoints)
		if err != nil {
			return err
		}
		var rendered poolapplications.Binding
		for i, approval := range approvals {
			if approval.Binding != works[i].ReviewedBinding {
				return fleeterror.NewFailedPreconditionError("reviewed board binding changed")
			}
			if err := decode([]byte(approval.Binding), &rendered); err != nil {
				return err
			}
		}
		if len(rendered.Usernames) != len(slots) {
			return fleeterror.NewFailedPreconditionError("reviewed pool rendering is unavailable")
		}
		// All boards of this controller use its existing worker name; the exact
		// rendered usernames are retained even if that name changes subsequently.
		key, err := encode([]any{deviceID, first.RevisionID, rendered.Usernames})
		if err != nil {
			return err
		}
		snapshotID := uuid.NewSHA1(uuid.NameSpaceOID, key).String()
		for i := range slots {
			slots[i].Username = rendered.Usernames[i]
			slots[i].WorkerTemplate = ""
		}
		encoded, err := encode(slots)
		if err != nil {
			return err
		}
		if err := q.CreatePoolGroupSnapshot(ctx, sqlc.CreatePoolGroupSnapshotParams{SnapshotID: snapshotID, OrganizationID: orgID, DeviceID: deviceID, ConfigurationID: first.ConfigurationID, RevisionID: first.RevisionID, Slots: encoded}); err != nil {
			return err
		}
		boards := make([]int64, len(targets))
		for i, target := range targets {
			claimed, err := s.sharedRepository.ClaimTarget(ctx, q, scope, operationID, target)
			if err != nil {
				return err
			}
			n, err := q.LinkPoolConfigurationAttempt(ctx, sqlc.LinkPoolConfigurationAttemptParams{OrganizationID: orgID, OperationID: operationID, TargetKind: target.Kind, TargetID: target.ID, Attempt: claimed, AttemptID: uuid.NullUUID{UUID: attempt, Valid: true}})
			if err != nil {
				return err
			}
			if n != 1 {
				return fleeterror.NewFailedPreconditionError("board application changed")
			}
			n, err = q.LinkPoolGroupSnapshot(ctx, sqlc.LinkPoolGroupSnapshotParams{OrganizationID: orgID, OperationID: operationID, TargetID: target.ID, Attempts: claimed, GroupSnapshotID: sql.NullString{String: snapshotID, Valid: true}})
			if err != nil {
				return err
			}
			if n != 1 {
				return fleeterror.NewFailedPreconditionError("board snapshot changed")
			}
			var binding poolapplications.Binding
			if err := decode([]byte(works[i].ReviewedBinding), &binding); err != nil {
				return err
			}
			boards[i] = binding.Board.HashboardID
		}
		installationIDs := make([]int64, len(targets))
		for i, target := range targets {
			installationIDs[i] = target.ID
		}
		cutover, err := q.HasPoolConfigurationCutover(ctx, orgID)
		if err != nil {
			return err
		}
		if !cutover {
			if err := q.DeleteReplacedLegacyPoolIntents(ctx, sqlc.DeleteReplacedLegacyPoolIntentsParams{OrganizationID: orgID, InstallationIds: installationIDs}); err != nil {
				return err
			}
		}
		row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
		if err != nil {
			return err
		}
		if active(row.Status) {
			return fleeterror.NewFailedPreconditionError("a mining group change is already pending")
		}
		row.OperationID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		row.AttemptID = uuid.NullUUID{UUID: attempt, Valid: true}
		row.ActorID = sql.NullInt64{Int64: first.ActorID, Valid: true}
		row.OperationKind = "pools"
		row.WriteEpoch = epoch
		row.Status = "pending"
		row.Message = messageWaitingToApply
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
		row.Revision++
		row.RetryCount = 0
		row.TransferFailureCount = 0
		row.NextReconcileAt = time.Now().UTC()
		device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: orgID, ID: deviceID})
		if err != nil {
			return err
		}
		if err := recordAcceptedActivity(ctx, q, row, device.DeviceIdentifier, device.SiteID, first.RevisionID, boards); err != nil {
			return err
		}
		return save(ctx, q, row)
	})
	return s.finishPoolConfigurationAdmission(ctx, scope, operationID, rows, err)
}

// Transient preparation failures leave work pending. Only explicit admission
// rejections become terminal, so reconnecting does not require a new request.
func (s *Service) finishPoolConfigurationAdmission(ctx context.Context, scope poolconfigurations.Scope, operationID string, rows []sqlc.ListPoolGroupWorkRow, err error) error {
	if errors.Is(err, errReplay) {
		return nil
	}
	if err == nil {
		return nil
	}
	// Conflicting queued or unresolved writes can settle without invalidating
	// this request. Leave it pending for a fresh admission check on the next pass.
	if devicewrite.IsMutationConflict(err) {
		return err
	}
	if !fleeterror.IsFailedPreconditionError(err) && !fleeterror.IsForbiddenError(err) {
		return err
	}
	// Admission was rejected before any firmware change was dispatched.
	// Preserve the durable failure so the UI can offer a fresh reviewed retry.
	return db.WithTransactionNoResult(ctx, s.conn, func(q sqlc.Querier) error {
		for _, row := range rows {
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: row.TargetID, DeviceID: row.DeviceID}
			attempt, claimErr := s.sharedRepository.ClaimTarget(ctx, q, scope, operationID, target)
			if fleeterror.IsFailedPreconditionError(claimErr) {
				continue
			}
			if claimErr != nil {
				return claimErr
			}
			if err := s.sharedRepository.FinishTarget(ctx, q, scope, operationID, target, attempt, "failed", "rejected"); err != nil {
				return err
			}
		}
		return nil
	})
}

func poolActorContext(ctx context.Context, orgID, actorID int64, accountID string) (context.Context, error) {
	ctx = authn.SetInfo(ctx, &session.Info{UserID: actorID, OrganizationID: orgID, AuthMethod: session.AuthMethodSession})
	if accountID != "" {
		return hostedaccounts.WithRequestAccountID(ctx, accountID)
	}
	return ctx, nil
}

// groupCredentials reads immutable execution material. The legacy reader exists
// only for retained pre-cutover controller bindings, never for new assignments.
func (s *Service) groupCredentials(ctx context.Context, q sqlc.Querier, orgID, deviceID int64, id string) ([]sdk.MiningGroupPoolInput, error) {
	snapshot, err := q.GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: orgID, DeviceID: deviceID, SnapshotID: id})
	if errors.Is(err, sql.ErrNoRows) {
		cutover, gateErr := q.HasPoolConfigurationCutover(ctx, orgID)
		if gateErr != nil {
			return nil, gateErr
		}
		if !cutover {
			return s.pools.HostGroupCredentials(ctx, q, orgID, id)
		}
	}
	if err != nil {
		return nil, err
	}
	slots, err := poolconfigurations.DecodeSlots(snapshot.Slots)
	if err != nil {
		return nil, err
	}
	pools := make([]sdk.MiningGroupPoolInput, len(slots))
	for i, slot := range slots {
		password, err := s.secrets.Decrypt(slot.PasswordCiphertext)
		if err != nil {
			return nil, err
		}
		pools[i] = sdk.MiningGroupPoolInput{URL: slot.URL, Username: slot.Username, Password: string(password)}
	}
	request := sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "validation", Revision: 1, Key: "validation", Pools: pools}
	return pools, request.Validate()
}

// Every new mutation rechecks the initiating user's current account authority,
// ownership and exact physical installation. Receipt polling stays read-only.
func (s *Service) authorizeSharedWorkflow(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) (bool, error) {
	targets, err := q.ListPoolGroupAttemptTargets(ctx, sqlc.ListPoolGroupAttemptTargetsParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID})
	if err != nil {
		return false, err
	}
	if len(targets) == 0 {
		return false, nil
	}
	for _, target := range targets {
		actorCtx, err := poolActorContext(ctx, row.OrganizationID, target.ActorID, target.ExternalAccountID)
		if err != nil {
			return true, err
		}
		scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID, AccountID: target.AccountID.Int64}
		selected := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: target.TargetID, DeviceID: row.DeviceID}
		ownership, err := s.pools.AuthorizePoolEquipment(actorCtx, q, scope, target.ActorID, selected)
		if err != nil {
			return true, err
		}
		var saved poolapplications.Binding
		if json.Unmarshal([]byte(target.ReviewedBinding), &saved) != nil || saved.Ownership != ownership {
			return true, fleeterror.NewForbiddenError("equipment ownership changed")
		}
		current, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: row.OrganizationID, OperationID: target.OperationID, TargetKind: target.TargetKind, TargetID: target.TargetID})
		if err != nil || current.State != statusApplying {
			return true, fleeterror.NewForbiddenError("board assignment changed")
		}
		runtime, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: row.OrganizationID, ID: row.DeviceID})
		if err != nil {
			return true, err
		}
		if !reflect.DeepEqual(runtime, saved.Device) {
			return true, fleeterror.NewForbiddenError("reviewed device binding changed")
		}
		boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, InstallationIds: []int64{target.TargetID}})
		if err != nil {
			return true, err
		}
		if len(boards) != 1 || saved.Board == nil || boards[0].MacAddress != saved.Board.MAC || boards[0].DeviceID != row.DeviceID {
			return true, fleeterror.NewForbiddenError("board installation changed")
		}
	}
	return true, nil
}

func (s *Service) sharedIntents(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, affected []string) (map[string]string, error) {
	intents, err := q.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	if err != nil {
		return nil, err
	}
	requiredBoards := []poolconfigurations.AssignmentTarget{}
	desired := map[string]string{}
	if len(intents) == 0 {
		return desired, nil
	}
	installations := make([]int64, len(intents))
	for i, intent := range intents {
		installations[i] = intent.InstallationID
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, InstallationIds: installations})
	if err != nil {
		return nil, err
	}
	byInstallation := make(map[int64]sqlc.ListPoolSelectionBoardsRow, len(boards))
	for _, board := range boards {
		byInstallation[board.InstallationID] = board
	}
	for _, intent := range intents {
		board, found := byInstallation[intent.InstallationID]
		if !found || board.DeviceID != row.DeviceID || board.MacAddress != intent.MacAddress {
			return nil, fmt.Errorf("board installation changed")
		}
		required := intent.PhysicalAttemptID == row.AttemptID
		for _, mac := range affected {
			required = required || mac == intent.MacAddress
		}
		if required && !board.Ready {
			return nil, fmt.Errorf("board inventory is stale")
		}
		if required {
			requiredBoards = append(requiredBoards, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: intent.InstallationID, DeviceID: row.DeviceID})
		}
		if desired[intent.MacAddress] != "" {
			return nil, fmt.Errorf("ambiguous board installation")
		}
		desired[intent.MacAddress] = intent.GroupSnapshotID.String
	}
	return desired, s.authorizeWorkflowBoards(ctx, q, row, requiredBoards)
}

// A controller plan can touch previously configured boards as well as its new
// selection. Shared operations must authorize those affected boards even when
// their retained intent came from the legacy configuration model.
func (s *Service) authorizeWorkflowBoards(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, boards []poolconfigurations.AssignmentTarget) error {
	if len(boards) == 0 {
		return nil
	}
	authority, err := q.ListPoolGroupAttemptTargets(ctx, sqlc.ListPoolGroupAttemptTargetsParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID})
	if err != nil || len(authority) == 0 {
		return err
	}
	actor := authority[0]
	actorCtx, err := poolActorContext(ctx, row.OrganizationID, actor.ActorID, actor.ExternalAccountID)
	if err != nil {
		return err
	}
	for _, board := range boards {
		if _, err := s.pools.AuthorizePoolEquipment(actorCtx, q, poolconfigurations.Scope{OrganizationID: row.OrganizationID, AccountID: actor.AccountID.Int64}, actor.ActorID, board); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) authorizeAffectedWorkflowBoards(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, affected []string) error {
	if len(affected) == 0 {
		return nil
	}
	authority, err := q.ListPoolGroupAttemptTargets(ctx, sqlc.ListPoolGroupAttemptTargetsParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID})
	if err != nil || len(authority) == 0 {
		return err
	}
	boards, err := q.ListPoolAffectedBoards(ctx, sqlc.ListPoolAffectedBoardsParams{OrgID: row.OrganizationID, ID: row.DeviceID, MacAddresses: affected})
	if err != nil {
		return err
	}
	found := map[string]bool{}
	targets := make([]poolconfigurations.AssignmentTarget, len(boards))
	for i, board := range boards {
		if found[board.MacAddress] {
			return fleeterror.NewFailedPreconditionError("ambiguous affected board installation")
		}
		found[board.MacAddress] = true
		targets[i] = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: board.InstallationID, DeviceID: row.DeviceID}
	}
	for _, mac := range affected {
		if !found[mac] {
			return fleeterror.NewFailedPreconditionError("affected board inventory is unavailable or stale")
		}
	}
	return s.authorizeWorkflowBoards(ctx, q, row, targets)
}
