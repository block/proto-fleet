package mininggroups

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
	"golang.org/x/sync/errgroup"
)

const operationReturnDefault = "return_default"

type DefaultReturnReview struct {
	Identifier    string
	Boards        []sqlc.ListDefaultReturnBoardsRow
	Default       sdk.MiningGroup
	Token         string
	BlockedReason string
	BoardNames    map[int64]string
}

// Membership and revisions advance during a transfer. The confirmed settings
// must remain unchanged across all steps, while each step uses fresh revisions.
type defaultReturnCapture struct {
	Boards   []sqlc.ListDefaultReturnBoardsRow `json:"boards"`
	Settings string                            `json:"settings"`
}

func digest(value any) (string, error) {
	raw, err := encode(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func defaultSettings(group sdk.MiningGroup) (string, error) {
	group.Revision = 0
	group.MACs = nil
	return digest(group)
}

// SDK snapshots report only the presence of custom power and tuning policies.
// Their values cannot be reviewed or protected by the settings digest.
func defaultReturnPolicyBlocker(observed sdk.MiningGroupResult, boards []sqlc.ListDefaultReturnBoardsRow) string {
	for _, group := range observed.Groups {
		if !group.CustomPolicy {
			continue
		}
		if group.ID == "default" || slices.ContainsFunc(boards, func(board sqlc.ListDefaultReturnBoardsRow) bool {
			return slices.Contains(group.MACs, board.MacAddress)
		}) {
			return "Fleet cannot move hashboards from or to a group with custom power or tuning settings."
		}
	}
	return ""
}

func defaultGroup(observed sdk.MiningGroupResult) (sdk.MiningGroup, bool) {
	for _, group := range observed.Groups {
		if group.ID == "default" {
			return group, true
		}
	}
	return sdk.MiningGroup{}, false
}

func currentDefaultReturnObservation(state State) bool {
	return state.Record.ObservedAt.Valid && time.Since(state.Record.ObservedAt.Time) <= 2*time.Minute && time.Until(state.Record.ObservedAt.Time) <= 5*time.Second && state.Observed.Supported
}

func checkDefaultReturnInventory(observed sdk.MiningGroupResult, boards []sqlc.ListDefaultReturnBoardsRow) error {
	known := make(map[string]bool, len(boards))
	for _, board := range boards {
		known[board.MacAddress] = true
	}
	for _, group := range observed.Groups {
		if group.ID == "default" {
			continue
		}
		for _, mac := range group.MACs {
			if !known[mac] {
				return fleeterror.NewFailedPreconditionError("Refresh the container inventory. Some boards outside Default have no current installation.")
			}
		}
	}
	return nil
}

// Routine review polls use committed observations. Confirmation still checks
// permissions, installations, pool work, and live controller revisions again.
func (s *Service) defaultReturnReviewState(ctx context.Context, orgID, userID int64, identifier string) (State, error) {
	target, err := s.ensure(ctx, orgID, userID, identifier)
	if err != nil {
		return State{}, err
	}
	row, err := s.queries.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: orgID, DeviceID: target.ID})
	if err != nil {
		return State{}, err
	}
	state := State{Record: row}
	if err := decode(row.Observed, &state.Observed); err != nil {
		return State{}, err
	}
	if currentDefaultReturnObservation(state) {
		return state, nil
	}
	return s.GetState(ctx, identifier)
}

func (s *Service) ReviewReturnToDefault(ctx context.Context, identifier string, ids []int64) ([]DefaultReturnReview, error) {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if identifier == "" && len(ids) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("Select a container or hashboards.")
	}
	var containerState *State
	if identifier != "" {
		state, err := s.defaultReturnReviewState(ctx, info.OrganizationID, info.UserID, identifier)
		if err != nil {
			return nil, err
		}
		if !currentDefaultReturnObservation(state) {
			return nil, fleeterror.NewFailedPreconditionError("Reconnect the controller and refresh its mining-group state.")
		}
		containerState = &state
	}
	boards, err := s.queries.ListDefaultReturnBoards(ctx, sqlc.ListDefaultReturnBoardsParams{OrganizationID: info.OrganizationID, DeviceIdentifier: identifier, HashboardIds: ids})
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 && len(boards) != len(ids) {
		return nil, fleeterror.NewFailedPreconditionError("Board installations changed. Review the recalled boards again.")
	}
	if containerState != nil && len(ids) == 0 {
		if err := checkDefaultReturnInventory(containerState.Observed, boards); err != nil {
			return nil, err
		}
	}
	type reviewSelection struct {
		state State
		ids   []int64
	}
	var selections []reviewSelection
	for len(boards) > 0 {
		end := 1
		for end < len(boards) && boards[end].DeviceID == boards[0].DeviceID {
			end++
		}
		selection := boards[:end]
		boards = boards[end:]
		var state State
		if containerState != nil {
			state = *containerState
		} else {
			state, err = s.defaultReturnReviewState(ctx, info.OrganizationID, info.UserID, selection[0].DeviceIdentifier)
			if err != nil {
				return nil, err
			}
			if !currentDefaultReturnObservation(state) {
				return nil, fleeterror.NewFailedPreconditionError("Reconnect the controller and refresh its mining-group state.")
			}
		}
		current, _ := defaultGroup(state.Observed)
		if slices.ContainsFunc(selection, func(board sqlc.ListDefaultReturnBoardsRow) bool {
			return board.ClientID == 0 && slices.Contains(current.MACs, board.MacAddress)
		}) {
			// A cached Default membership must never turn a requested move into
			// a no-op. ProtoOS can move a board after Fleet's last observation.
			state, err = s.GetState(ctx, selection[0].DeviceIdentifier)
			if err != nil {
				return nil, err
			}
			if !currentDefaultReturnObservation(state) || !state.Observed.Enabled || state.Observed.Suspended || state.Observed.Pending {
				return nil, fleeterror.NewFailedPreconditionError("Reconnect the controller and refresh its mining-group state.")
			}
			if containerState != nil && len(ids) == 0 {
				if err := checkDefaultReturnInventory(state.Observed, selection); err != nil {
					return nil, err
				}
			}
			current, _ = defaultGroup(state.Observed)
		}
		selected := make([]int64, 0, len(selection))
		for _, board := range selection {
			if board.ClientID != 0 {
				if len(ids) > 0 {
					return nil, fleeterror.NewFailedPreconditionError("Recall the selected boards to the host before returning them to Default.")
				}
				continue
			}
			if !slices.Contains(current.MACs, board.MacAddress) {
				selected = append(selected, board.HashboardID)
			}
		}
		if len(selected) == 0 {
			continue
		}
		selections = append(selections, reviewSelection{state: state, ids: selected})
	}
	reviews := make([]DefaultReturnReview, len(selections))
	reads, readCtx := errgroup.WithContext(ctx)
	reads.SetLimit(4)
	for i, selection := range selections {
		reads.Go(func() error {
			state := selection.state
			review, err := s.reviewDefaultReturn(readCtx, s.queries, info.UserID, state.Record, state.Observed, selection.ids, func() (sdk.MiningGroup, error) {
				return s.readReviewedDefault(readCtx, state.Record.DeviceID)
			})
			reviews[i] = review
			return err
		})
	}
	if err := reads.Wait(); err != nil {
		return nil, fmt.Errorf("review Default pools: %w", err)
	}
	selectedIDs := []int64{}
	for _, review := range reviews {
		for _, board := range review.Boards {
			selectedIDs = append(selectedIDs, board.HashboardID)
		}
	}
	if len(selectedIDs) > 0 {
		names, err := s.queries.ListDefaultReturnBoardNames(ctx, sqlc.ListDefaultReturnBoardNamesParams{OrganizationID: info.OrganizationID, HashboardIds: selectedIDs})
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]string, len(names))
		for _, name := range names {
			byID[name.HashboardID] = name.Name
		}
		for index := range reviews {
			reviews[index].BoardNames = byID
		}
	}
	return reviews, nil
}

func (s *Service) reviewDefaultReturn(ctx context.Context, q sqlc.Querier, userID int64, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult, ids []int64, readDefault func() (sdk.MiningGroup, error)) (DefaultReturnReview, error) {
	target, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: row.OrganizationID, ID: row.DeviceID})
	if err != nil {
		return DefaultReturnReview{}, err
	}
	if err := authorize(ctx, q, userID, row.OrganizationID, target.SiteID, authz.PermMinerUpdatePools, authz.PermMinerStartMining, authz.PermMinerStopMining); err != nil {
		return DefaultReturnReview{}, err
	}
	review := DefaultReturnReview{Identifier: target.DeviceIdentifier}
	review.Boards, err = q.ListDefaultReturnBoards(ctx, sqlc.ListDefaultReturnBoardsParams{OrganizationID: row.OrganizationID, DeviceIdentifier: target.DeviceIdentifier, HashboardIds: ids})
	if err != nil {
		return review, err
	}
	if len(review.Boards) != len(ids) || len(ids) == 0 || len(ids) > 288 {
		return review, fleeterror.NewFailedPreconditionError("Board installations changed. Review the recalled boards again.")
	}
	for _, board := range review.Boards {
		if board.ClientID != 0 {
			return review, fleeterror.NewFailedPreconditionError("Recall the selected boards to the host before returning them to Default.")
		}
	}
	var found bool
	review.Default, found = defaultGroup(observed)
	if !found {
		review.BlockedReason = "Refresh the controller to load its Default group."
		return review, nil
	}
	review.BlockedReason, err = s.defaultReturnBlocker(ctx, q, row, observed, ids)
	if err != nil || review.BlockedReason != "" {
		return review, err
	}
	if review.BlockedReason = defaultReturnPolicyBlocker(observed, review.Boards); review.BlockedReason != "" {
		return review, nil
	}
	current, err := readDefault()
	if err != nil {
		review.Default.Pools = nil
		review.BlockedReason = defaultReadBlockedMessage(err)
		return review, nil
	}
	if current.Revision != review.Default.Revision {
		review.BlockedReason = "Default changed. Refresh the controller and review again."
		return review, nil
	}
	current.MACs = review.Default.MACs
	review.Default = current
	settings, err := defaultSettings(review.Default)
	if err != nil {
		return review, err
	}
	review.Token, err = digest(defaultReturnCapture{Boards: review.Boards, Settings: settings})
	return review, err
}

func (s *Service) defaultReturnBlocker(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult, ids []int64) (string, error) {
	if active(row.Status) {
		return "Wait for the current mining-group change or recovery to finish.", nil
	}
	if !row.ObservedAt.Valid || time.Since(row.ObservedAt.Time) > 2*time.Minute || time.Until(row.ObservedAt.Time) > 5*time.Second || !observed.Supported || !observed.Enabled || observed.Suspended || observed.Pending {
		return "Reconnect the controller and wait for mining groups to be enabled and settled.", nil
	}
	pending, err := q.HasPendingDefaultReturnPoolPolicy(ctx, sqlc.HasPendingDefaultReturnPoolPolicyParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	if err != nil {
		return "", err
	}
	if pending.Bool {
		return "Wait for pool changes and ownership cleanup to finish. The boards remain recalled.", nil
	}
	if _, err := q.GetBlockingFrigateUpdateJobForDevice(ctx, row.DeviceID); err == nil {
		return "Wait for the controller software update to finish.", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := devicewrite.RequireNoConflictingPoolWrites(ctx, q, row.OrganizationID, row.DeviceID); err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			return "Wait for pending device changes or resolve uncertain writes before moving boards.", nil
		}
		return "", err
	}
	boards, err := q.GetMiningGroupBoards(ctx, sqlc.GetMiningGroupBoardsParams{OrganizationID: row.OrganizationID, HashboardIds: ids})
	if err != nil {
		return "", err
	}
	if len(boards) != len(ids) {
		return "Refresh the controller's hashboard inventory before moving boards.", nil
	}
	for _, board := range boards {
		if board.DeviceID != row.DeviceID {
			return "Board installations changed. Review the boards again.", nil
		}
	}
	return "", nil
}

func (s *Service) ReturnToDefault(ctx context.Context, identifier, token, requestID string, ids []int64) (uuid.UUID, error) {
	info, err := OperatorPrincipal(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(requestID)
	if err != nil || token == "" || len(ids) == 0 || len(ids) > 288 {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("A reviewed selection and request ID are required.")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != len(ids) || ids[0] <= 0 {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("Select unique hashboards.")
	}
	fingerprint, err := digest([]any{identifier, operationReturnDefault, token, ids})
	if err != nil {
		return uuid.Nil, err
	}
	target, err := s.ensure(ctx, info.OrganizationID, info.UserID, identifier)
	if err != nil {
		return uuid.Nil, err
	}
	replay := func(q sqlc.Querier) (bool, error) {
		previous, err := q.GetMiningGroupRequest(ctx, sqlc.GetMiningGroupRequestParams{OrganizationID: info.OrganizationID, RequestID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if previous.Fingerprint != fingerprint || previous.DeviceID != target.ID {
			return false, fleeterror.NewInvalidArgumentError("Request ID already used for another change.")
		}
		return true, nil
	}
	if done, err := replay(s.queries); done || err != nil {
		return id, err
	}
	if err := authorize(ctx, s.queries, info.UserID, info.OrganizationID, target.SiteID, authz.PermMinerUpdatePools, authz.PermMinerStartMining, authz.PermMinerStopMining); err != nil {
		return uuid.Nil, err
	}
	current, currentErr := s.readReviewedDefault(ctx, target.ID)
	err = s.writes.BeginPoolWorkflow(ctx, info.OrganizationID, target.ID, func(ctx context.Context, q sqlc.Querier, attempt uuid.UUID, epoch int64) error {
		if done, err := replay(q); err != nil {
			return err
		} else if done {
			return errReplay
		}
		row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: info.OrganizationID, DeviceID: target.ID})
		if err != nil {
			return err
		}
		var observed sdk.MiningGroupResult
		if err := decode(row.Observed, &observed); err != nil {
			return err
		}
		review, err := s.reviewDefaultReturn(ctx, q, info.UserID, row, observed, ids, func() (sdk.MiningGroup, error) {
			return current, currentErr
		})
		if err != nil {
			return err
		}
		if review.BlockedReason != "" {
			return fleeterror.NewFailedPreconditionError(review.BlockedReason)
		}
		if review.Token != token {
			return fleeterror.NewFailedPreconditionError("Default settings, ownership or board installations changed. Review and confirm again.")
		}
		settings, err := defaultSettings(review.Default)
		if err != nil {
			return err
		}
		capture, err := encode(defaultReturnCapture{Boards: review.Boards, Settings: settings})
		if err != nil {
			return err
		}
		if err := q.SaveMiningGroupDefaultReturn(ctx, sqlc.SaveMiningGroupDefaultReturnParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, DefaultReturn: pqtype.NullRawMessage{RawMessage: capture, Valid: true}}); err != nil {
			return err
		}
		row.OperationID = uuid.NullUUID{UUID: id, Valid: true}
		row.AttemptID = uuid.NullUUID{UUID: attempt, Valid: true}
		row.ActorID = sql.NullInt64{Int64: info.UserID, Valid: true}
		row.OperationKind = operationReturnDefault
		row.Status = "pending"
		row.Message = "Returning recalled hashboards to Default"
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
		row.WriteEpoch = epoch
		row.Revision++
		row.RetryCount = 0
		row.TransferFailureCount = 0
		row.NextReconcileAt = time.Now().UTC()
		if err := q.RecordMiningGroupRequest(ctx, sqlc.RecordMiningGroupRequestParams{OrganizationID: row.OrganizationID, RequestID: id, DeviceID: row.DeviceID, Fingerprint: fingerprint}); err != nil {
			return err
		}
		if err := recordAcceptedActivity(ctx, q, row, identifier, target.SiteID, "", ids); err != nil {
			return err
		}
		return save(ctx, q, row)
	})
	if errors.Is(err, errReplay) {
		err = nil
	}
	return id, err
}

func (s *Service) validateDefaultReturn(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) (defaultReturnCapture, error) {
	var capture defaultReturnCapture
	if !row.DefaultReturn.Valid {
		return capture, fmt.Errorf("missing Default return selection")
	}
	if err := decode(row.DefaultReturn.RawMessage, &capture); err != nil {
		return capture, err
	}
	ids := make([]int64, len(capture.Boards))
	for i, board := range capture.Boards {
		ids[i] = board.HashboardID
	}
	boards, err := q.ListDefaultReturnBoards(ctx, sqlc.ListDefaultReturnBoardsParams{OrganizationID: row.OrganizationID, HashboardIds: ids})
	if err != nil {
		return capture, err
	}
	if len(ids) == 0 || !reflect.DeepEqual(boards, capture.Boards) {
		return capture, newWorkflowChangedError("Board ownership, installation or pool policy changed. Review the recalled boards again.")
	}
	ready, err := q.GetMiningGroupBoards(ctx, sqlc.GetMiningGroupBoardsParams{OrganizationID: row.OrganizationID, HashboardIds: ids})
	if err != nil {
		return capture, err
	}
	if len(ready) != len(ids) {
		return capture, newInventoryStaleError("Hashboard inventory is stale. Waiting for a fresh inventory.")
	}
	return capture, nil
}

func (s *Service) reconcileDefaultReturn(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	// Replay an unresolved request exactly, even if its transfer already moved
	// the boards. A later observation cannot substitute for the saved receipt.
	if row.Step.Valid {
		unsent, err := q.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID})
		if err != nil {
			return err
		}
		if unsent {
			current, err := s.readReviewedDefault(ctx, row.DeviceID)
			if err != nil {
				return s.block(ctx, q, row, defaultReadBlockedMessage(err))
			}
			if current.CustomPolicy {
				return s.interrupt(ctx, q, row, "Fleet cannot move hashboards from or to a group with custom power or tuning settings.")
			}
			var capture defaultReturnCapture
			if err := decode(row.DefaultReturn.RawMessage, &capture); err != nil {
				return err
			}
			settings, err := defaultSettings(current)
			if err != nil {
				return err
			}
			if settings != capture.Settings {
				return s.interrupt(ctx, q, row, "Default settings changed. Review and confirm again. The boards remain recalled.")
			}
		}
		return s.apply(ctx, q, row, nil)
	}
	observed, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "observe"})
	if err != nil || !observed.Supported || observed.Pending {
		return s.block(ctx, q, row, "Waiting for the controller to confirm the board transfer state")
	}
	row.Observed, err = encode(observed)
	if err != nil {
		return err
	}
	row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	capture, err := s.validateDefaultReturn(ctx, q, row)
	if err != nil {
		if isWorkflowChanged(err) {
			return s.interrupt(ctx, q, row, err.Error())
		}
		if isInventoryStale(err) {
			return s.block(ctx, q, row, err.Error())
		}
		return err
	}
	target, found := defaultGroup(observed)
	if !found || !observed.Enabled || observed.Suspended {
		return s.finishDefaultReturn(ctx, q, row, false, "Controller mode changed. Review the container before retrying the move to Default.")
	}
	if reason := defaultReturnPolicyBlocker(observed, capture.Boards); reason != "" {
		return s.finishDefaultReturn(ctx, q, row, false, reason)
	}
	current, err := s.readReviewedDefault(ctx, row.DeviceID)
	if err != nil {
		return s.block(ctx, q, row, defaultReadBlockedMessage(err))
	}
	if current.Revision != target.Revision {
		return s.block(ctx, q, row, "Default changed during observation. Waiting for current settings.")
	}
	settings, err := defaultSettings(current)
	if err != nil {
		return err
	}
	if settings != capture.Settings {
		return s.finishDefaultReturn(ctx, q, row, false, "Default settings changed. Review and confirm again. The boards remain recalled.")
	}
	membership := map[string]sdk.MiningGroup{}
	for _, group := range observed.Groups {
		for _, mac := range group.MACs {
			if _, found := membership[mac]; found {
				return s.block(ctx, q, row, "Controller reports ambiguous board membership")
			}
			membership[mac] = group
		}
	}
	returned := 0
	for _, board := range capture.Boards {
		if membership[board.MacAddress].ID == "default" {
			returned++
		}
	}
	if returned == len(capture.Boards) {
		return s.finishDefaultReturn(ctx, q, row, true, "Hashboards returned to Default and use its pool and mining settings.")
	}
	if row.TransferFailureCount >= maxTransferFailures {
		return s.finishDefaultReturn(ctx, q, row, false, "The controller could not move all boards to Default. The boards remain recalled. Review and retry the remaining boards.")
	}
	// One source group per transfer; unrelated siblings are never included.
	for _, board := range capture.Boards {
		source := membership[board.MacAddress]
		if source.ID == "default" {
			continue
		}
		macs := []string{}
		for _, selected := range capture.Boards {
			if membership[selected.MacAddress].ID == source.ID {
				macs = append(macs, selected.MacAddress)
			}
		}
		return s.prepareStep(ctx, q, row, sdk.MiningGroupRequest{Action: "transfer", GroupID: "default", Revision: target.Revision, SourceID: source.ID, SourceRevision: source.Revision, MACs: macs}, "", "Moving recalled hashboards to Default")
	}
	return nil
}

func retireDefaultReturnIntent(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, installations []int64) error {
	if len(installations) == 0 {
		return nil
	}
	if err := q.RetireDefaultReturnAssignments(ctx, sqlc.RetireDefaultReturnAssignmentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, InstallationIds: installations}); err != nil {
		return err
	}
	if err := q.SupersedeDefaultReturnClears(ctx, sqlc.SupersedeDefaultReturnClearsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, InstallationIds: installations}); err != nil {
		return err
	}
	if err := q.DeleteDefaultReturnBoardIntents(ctx, sqlc.DeleteDefaultReturnBoardIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, InstallationIds: installations}); err != nil {
		return err
	}
	return q.DeleteDefaultReturnLegacyIntents(ctx, sqlc.DeleteDefaultReturnLegacyIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, InstallationIds: installations})
}

func (s *Service) finishDefaultReturn(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, success bool, message string) error {
	// Keep established effects even when access or settings changed after one
	// transfer. Retire policy only for the exact, still-unallocated installation.
	if err := retireObservedDefaultReturns(ctx, q, row); err != nil {
		return err
	}
	outcome := "failed"
	if success {
		outcome = "completed"
	}
	if err := recordOutcomeActivity(ctx, q, row, outcome); err != nil {
		return err
	}
	if err := devicewrite.CompleteWorkflow(ctx, q, row.AttemptID.UUID); err != nil {
		return err
	}
	row.Status = "failed"
	if success {
		row.Status = statusComplete
	}
	row.Message = message
	return save(ctx, q, row)
}

func retireObservedDefaultReturns(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	var capture defaultReturnCapture
	if err := decode(row.DefaultReturn.RawMessage, &capture); err != nil {
		return err
	}
	var observed sdk.MiningGroupResult
	if err := decode(row.Observed, &observed); err != nil {
		return err
	}
	if !observed.Supported || observed.Pending {
		return fmt.Errorf("Default return requires a settled observation")
	}
	target, found := defaultGroup(observed)
	if !found {
		return nil
	}
	ids := make([]int64, len(capture.Boards))
	for i, board := range capture.Boards {
		ids[i] = board.HashboardID
	}
	current, err := q.ListDefaultReturnBoards(ctx, sqlc.ListDefaultReturnBoardsParams{OrganizationID: row.OrganizationID, HashboardIds: ids})
	if err != nil {
		return err
	}
	var returned []int64
	for _, board := range current {
		if slices.Contains(capture.Boards, board) && slices.Contains(target.MACs, board.MacAddress) {
			returned = append(returned, board.InstallationID)
		}
	}
	return retireDefaultReturnIntent(ctx, q, row, returned)
}
