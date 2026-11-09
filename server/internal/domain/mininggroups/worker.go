package mininggroups

import (
	"context"
	"database/sql"
	"errors"

	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// Fleet Node allows 25 seconds for a command. Leave additional time for
// admission, transport, the acknowledgement, and committing the journal.
const reconciliationTimeout = 40 * time.Second

// This budget counts only explicit terminal FAILED receipts, across the entire
// user operation. Timeouts, pending transfers and revision conflicts do not count.
const maxTransferFailures = 3

const (
	actionClearPools      = "clear_pools"
	actionReplacePools    = "replace_pools"
	transferPhaseComplete = "COMPLETE"
	transferPhaseFailed   = "FAILED"
)

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	s.run(ctx, ticker.C, s.reconcileScheduled)
}

func (s *Service) run(ctx context.Context, ticks <-chan time.Time, reconcile func(context.Context, sqlc.ListMiningGroupControllersRow) error) {
	// Keep admission across ticks so slow containers occupy only their own
	// slots. A device remains running through retry bookkeeping as well as I/O.
	var pending sync.WaitGroup
	defer pending.Wait()
	var runningMu sync.Mutex
	running := map[int64]struct{}{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			rows, err := s.queries.ListMiningGroupControllers(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "list mining group operations", "error", err)
				continue
			}
			for _, r := range rows {
				if ctx.Err() != nil {
					return
				}
				runningMu.Lock()
				if len(running) >= 8 {
					runningMu.Unlock()
					break // Recheck ready work on the next tick without waiting.
				}
				if _, exists := running[r.DeviceID]; exists {
					runningMu.Unlock()
					continue
				}
				running[r.DeviceID] = struct{}{}
				runningMu.Unlock()
				pending.Add(1)
				go func() {
					defer pending.Done()
					defer func() {
						runningMu.Lock()
						delete(running, r.DeviceID)
						runningMu.Unlock()
					}()
					if err := reconcile(ctx, r); err != nil {
						slog.WarnContext(ctx, "mining group reconciliation paused", "device_id", r.DeviceID, "error", err)
					}
				}()
			}
		}
	}
}
func (s *Service) Reconcile(ctx context.Context, orgID, deviceID int64, identifier string) error {
	initial, err := s.queries.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	if !active(initial.Status) {
		return nil
	}
	attempt, err := s.writes.ResumeWorkflow(ctx, orgID, deviceID, initial.AttemptID.UUID)
	if err != nil {
		return err
	}
	ctx = attempt.Context(ctx)
	// The binding transaction takes device/route locks before the journal lock.
	// It never retries external calls. The committed step survives any lost reply.
	return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		return s.writes.WithBindingObservation(ctx, deviceID, func(q sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
			if err != nil {
				return err
			}
			if row.AttemptID != initial.AttemptID || !active(row.Status) {
				return nil
			}
			// Settling an already admitted transfer is read-only. Loss of user
			// authority or installation freshness must not stop receipt polling.
			if row.ControllerOperationID != "" {
				return s.pollTransfer(ctx, q, row)
			}
			target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{OrgID: orgID, DeviceIdentifier: identifier})
			if err != nil {
				return err
			}
			permission := authz.PermMinerUpdatePools
			if row.OperationKind == operationActivate {
				permission = authz.PermMinerActivateMiningGroups
			}
			shared, authorityErr := s.authorizeSharedWorkflow(ctx, q, row)
			if authorityErr == nil && !shared {
				authorityErr = authorize(ctx, q, row.ActorID.Int64, orgID, target.SiteID, permission, authz.PermMinerStartMining, authz.PermMinerStopMining)
			}
			if err := authorityErr; err != nil {
				if !fleeterror.IsForbiddenError(err) {
					return err
				}
				if row.OperationKind == operationActivate {
					return s.interrupt(ctx, q, row, "Operator access changed. Activation ended without further changes; review the controller's mining state before continuing.")
				}
				return s.interrupt(ctx, q, row, "Operator access changed. Applied assignments were kept; an authorized operator can submit a new request.")
			}
			if row.OperationKind != operationActivate {
				detached, err := hasDetachedIntents(ctx, q, orgID, deviceID)
				if err != nil {
					return err
				}
				if detached {
					return s.interrupt(ctx, q, row, "A board installation changed. Applied assignments were kept; refresh the boards and submit a new request.")
				}
			}
			desired, err := s.liveIntent(ctx, q, row, nil)
			if err != nil {
				return s.block(ctx, q, row, "Hashboard inventory changed or is stale; configuration is paused")
			}
			if row.OperationKind == operationActivate {
				return s.reconcileActivation(ctx, q, row)
			}
			if row.Step.Valid {
				return s.apply(ctx, q, row, desired)
			}
			observed, err := s.call(ctx, deviceID, sdk.MiningGroupRequest{Action: "observe"})
			if err != nil {
				return s.block(ctx, q, row, "Controller unavailable; waiting to refresh mining groups")
			}
			row.Observed, err = encode(observed)
			if err != nil {
				return err
			}
			row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
			if !observed.Supported {
				return s.block(ctx, q, row, "Controller firmware does not support mining groups")
			}
			var request sdk.MiningGroupRequest
			snapshot, err := s.plannerSnapshot(ctx, q, row, observed)
			if err != nil {
				return s.block(ctx, q, row, "Pool configuration unavailable; configuration is paused")
			}
			if err := retainObservedIntents(ctx, q, row, snapshot); err != nil {
				return err
			}
			if shared && !observed.Pending && (!observed.Enabled || observed.Suspended) {
				// No saved request or transfer receipt is outstanding on this path.
				// A confirmed incompatible mode ends the workflow, releasing its
				// reservation so an operator can correct the controller mode.
				return s.failOperation(ctx, q, row, desired, snapshot, "Controller mining-group mode changed. Review the controller and submit a new pool request.")
			}
			if shared && observed.Suspended {
				return s.block(ctx, q, row, "Controller is suspended with unfinished work; waiting for a settled observation")
			}
			step, err := Plan(desired, snapshot)
			if err == nil && step.Kind != Wait && step.Kind != Done {
				// A changed policy may block a later transfer even when this next
				// setup step is legal. Check the remaining sequence before writing.
				err = ValidateAssignment(desired, snapshot)
			}
			if row.TransferFailureCount >= maxTransferFailures && !snapshot.Pending && snapshot.Enabled && (err != nil || step.Kind != Done) {
				return s.failOperation(ctx, q, row, desired, snapshot, "Pool assignment stopped after three transfer failures. Applied assignments were kept; review the controller before trying again.")
			}
			if err != nil {
				var unsupported *unsupportedAssignmentError
				if errors.As(err, &unsupported) {
					return s.failOperation(ctx, q, row, desired, snapshot, "Pool assignment stopped: "+unsupported.Error()+". Applied assignments were kept; review the configuration before trying again.")
				}
				return s.block(ctx, q, row, err.Error())
			}
			switch step.Kind {
			case Done:
				return s.complete(ctx, q, row)
			case Wait:
				row.Status = statusApplying
				row.Message = "Waiting for the controller to finish moving boards"
				return save(ctx, q, row)
			case Remove:
				request = sdk.MiningGroupRequest{Action: "remove", GroupID: step.GroupID, Revision: step.ExpectedRevision}
			case Create:
				request = sdk.MiningGroupRequest{Action: "create", ConfigID: step.ConfigRevision}
			case Configure:
				pools, err := s.groupCredentials(ctx, q, orgID, deviceID, step.ConfigRevision)
				if err != nil {
					return s.block(ctx, q, row, "Pool configuration unavailable")
				}
				request = sdk.MiningGroupRequest{Action: actionReplacePools, GroupID: step.GroupID, Revision: step.ExpectedRevision, Pools: pools}
				if poolconfigurations.IsClearedPoolIntent(step.ConfigRevision) {
					request.Action = actionClearPools
				}
			case Stop:
				request = sdk.MiningGroupRequest{Action: "stop", GroupID: step.GroupID, Revision: step.ExpectedRevision}
			case Start:
				request = sdk.MiningGroupRequest{Action: "start", GroupID: step.GroupID, Revision: step.ExpectedRevision}
			case Transfer:
				request = sdk.MiningGroupRequest{Action: string(Transfer), GroupID: step.GroupID, SourceID: step.SourceID, Revision: step.ExpectedRevision, SourceRevision: step.SourceExpectedRevision, MACs: step.MACs}
			default:
				return fmt.Errorf("unknown reconciliation step")
			}
			return s.prepareStep(ctx, q, row, request, step.ConfigRevision, "Applying pool configuration")
		})
	})
}

// Installation identity for all historical intents is checked by Reconcile
// before this call. Freshness is required only for the current selection and
// any historical boards affected by the next write.
func (s *Service) liveIntent(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, affected []string) (map[string]string, error) {
	desired := map[string]string{}
	if row.OperationKind == operationActivate {
		return desired, nil
	}
	cutover, err := q.HasPoolConfigurationCutover(ctx, row.OrganizationID)
	if err != nil {
		return nil, err
	}
	if cutover {
		desired, err := s.sharedIntents(ctx, q, row, affected)
		if err != nil {
			return nil, err
		}
		if err := s.overlayClearedIntents(ctx, q, row, desired, affected); err != nil {
			return nil, err
		}
		return desired, s.authorizeAffectedWorkflowBoards(ctx, q, row, affected)
	}
	intents, err := q.ListUnreplacedHashboardPoolIntents(ctx, sqlc.ListUnreplacedHashboardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(intents))
	required := make([]sqlc.HashboardPoolIntent, 0, len(intents))
	for _, i := range intents {
		if desired[i.MacAddress] != "" {
			return nil, fleeterror.NewFailedPreconditionError("ambiguous board installation")
		}
		desired[i.MacAddress] = i.PoolRevisionID
		if i.OperationID == row.OperationID.UUID || slices.Contains(affected, i.MacAddress) {
			ids = append(ids, i.HashboardID)
			required = append(required, i)
		}
	}
	boards, err := q.GetMiningGroupBoards(ctx, sqlc.GetMiningGroupBoardsParams{OrganizationID: row.OrganizationID, HashboardIds: ids})
	if err != nil {
		return nil, err
	}
	if len(boards) != len(required) {
		return nil, fleeterror.NewFailedPreconditionError("board inventory changed")
	}
	for i, b := range boards {
		want := required[i]
		if b.HashboardID != want.HashboardID || b.InstallationID != want.InstallationID || b.DeviceID != row.DeviceID || b.MacAddress != want.MacAddress {
			return nil, fleeterror.NewFailedPreconditionError("board installation changed")
		}
	}
	affectedBoards := make([]poolconfigurations.AssignmentTarget, len(required))
	for i, intent := range required {
		affectedBoards[i] = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: intent.InstallationID, DeviceID: row.DeviceID}
	}
	if err := s.authorizeWorkflowBoards(ctx, q, row, affectedBoards); err != nil {
		return nil, err
	}
	shared, err := s.sharedIntents(ctx, q, row, affected)
	if err != nil {
		return nil, err
	}
	for mac, id := range shared {
		desired[mac] = id
	}
	if err := s.overlayClearedIntents(ctx, q, row, desired, affected); err != nil {
		return nil, err
	}
	return desired, s.authorizeAffectedWorkflowBoards(ctx, q, row, affected)
}
func (s *Service) plannerSnapshot(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult) (Snapshot, error) {
	bindings := map[string]binding{}
	if err := decode(row.Bindings, &bindings); err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Enabled: observed.Enabled, MaxGroups: observed.MaxGroups, Pending: observed.Pending}
	for _, g := range observed.Groups {
		b, managed := bindings[g.ID]
		configured := false
		if managed && b.Configured {
			pools, err := s.groupCredentials(ctx, q, row.OrganizationID, row.DeviceID, b.ConfigID)
			if err != nil {
				return Snapshot{}, err
			}
			configured = poolsMatch(g.Pools, pools)
		}
		snapshot.Groups = append(snapshot.Groups, Group{ID: g.ID, Revision: g.Revision, ConfigRevision: b.ConfigID, Managed: managed, Configured: configured, Mining: g.Mining, CustomPolicy: g.CustomPolicy, Members: g.MACs})
	}
	return snapshot, nil
}
func poolsMatch(observed []sdk.MiningGroupPool, want []sdk.MiningGroupPoolInput) bool {
	if len(observed) != len(want) {
		return false
	}
	sorted := slices.Clone(observed)
	slices.SortFunc(sorted, func(a, b sdk.MiningGroupPool) int { return int(a.Priority - b.Priority) })
	for i, p := range sorted {
		if p.URL != want[i].URL || p.Username != want[i].Username || int64(p.Priority) != int64(i) || !p.Enabled || p.ShareWeight != nil {
			return false
		}
	}
	return true
}
func (s *Service) apply(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, desired map[string]string) error {
	var step savedStep
	if err := decode(row.Step.RawMessage, &step); err != nil {
		return err
	}
	data, err := s.secrets.Decrypt(step.Ciphertext)
	if err != nil {
		return err
	}
	request, err := sdk.DecodeMiningGroupRequest(data)
	if err != nil {
		return err
	}
	if request.Key != row.StepKey.UUID.String() {
		return fmt.Errorf("mining group step identity mismatch")
	}
	if _, err := s.liveIntent(ctx, q, row, step.AffectedMACs); err != nil {
		return s.block(ctx, q, row, "A board affected by this change is unavailable or stale; configuration is paused")
	}
	// Re-authorize profiles on replay as well as when preparing a new step.
	if step.ConfigID != "" {
		if _, err := s.groupCredentials(ctx, q, row.OrganizationID, row.DeviceID, step.ConfigID); err != nil {
			return s.block(ctx, q, row, "Pool configuration is no longer available")
		}
	}
	if request.Action == string(Transfer) {
		for _, mac := range request.MACs {
			if desired[mac] != step.ConfigID {
				return s.block(ctx, q, row, "Selected board intent changed; transfer is paused")
			}
		}
	}
	var response sdk.MiningGroupResult
	err = devicewrite.Dispatch(ctx, func(ctx context.Context) error {
		var e error
		response, e = s.call(ctx, row.DeviceID, request)
		return e
	})
	if err != nil {
		message := "Controller has not confirmed the change; retrying the same request"
		if request.Action == operationActivate {
			message = activationWaitingMessage(err)
		}
		return s.block(ctx, q, row, message)
	}
	if response.RevisionConflict {
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
		row.Status = "pending"
		row.Message = "Controller configuration changed; refreshing before retry"
		return save(ctx, q, row)
	}
	bindings := map[string]binding{}
	if err := decode(row.Bindings, &bindings); err != nil {
		return err
	}
	switch request.Action {
	case "remove":
		if !response.Removed {
			return s.block(ctx, q, row, "Waiting for a group removal receipt")
		}
		delete(bindings, request.GroupID)
	case "create":
		if response.Group == nil || response.Group.ID == "" {
			return s.block(ctx, q, row, "Waiting for a valid group creation receipt")
		}
		bindings[response.Group.ID] = binding{ConfigID: step.ConfigID}
	case actionReplacePools, actionClearPools:
		if response.Group == nil || response.Group.ID != request.GroupID {
			return s.block(ctx, q, row, "Waiting for a valid pool configuration receipt")
		}
		bindings[request.GroupID] = binding{ConfigID: step.ConfigID, Configured: true}
		if !poolconfigurations.IsClearedPoolIntent(step.ConfigID) && poolsMatch(response.Group.Pools, request.Pools) {
			if err := q.RetainAppliedPoolGroupIntents(ctx, sqlc.RetainAppliedPoolGroupIntentsParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID, GroupSnapshotID: sql.NullString{String: step.ConfigID, Valid: true}, MacAddresses: response.Group.MACs}); err != nil {
				return err
			}
		}
	case "stop":
		if response.Group == nil || response.Group.ID != request.GroupID || response.Group.Mining {
			return s.block(ctx, q, row, "Waiting for stopped group intent")
		}
	case "start":
		if response.Group == nil || response.Group.ID != request.GroupID || !response.Group.Mining {
			return s.block(ctx, q, row, "Waiting for a valid mining intent receipt")
		}
	case operationActivate:
		if !response.Enabled {
			return s.block(ctx, q, row, "Waiting for mining group activation")
		}
	case string(Transfer):
		return s.recordTransfer(ctx, q, row, response.Operation)
	}
	row.Bindings, err = encode(bindings)
	if err != nil {
		return err
	}
	return finishStep(ctx, q, row)
}
func (s *Service) block(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, message string) error {
	row.Status = "blocked"
	row.Message = message
	return save(ctx, q, row)
}
func (s *Service) complete(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	// Verify the selected board's runtime before retiring its assignment.
	if err := s.finishBoardRemoval(ctx, q, row); err != nil {
		var pending *boardRemovalVerificationPending
		if !errors.As(err, &pending) {
			return err
		}
		changed, err := q.FinishPoolRemoval(ctx, sqlc.FinishPoolRemovalParams{RemovalID: pending.removalID, State: "uncertain", ResultCode: pending.code})
		if err != nil {
			return err
		}
		if changed != 1 {
			return fmt.Errorf("pool removal changed during verification")
		}
		return s.block(ctx, q, row, pending.Error())
	}
	// The planner reaches Done only after observed pool fields and board
	// membership match the intent. MDK passwords are write-only.
	if err := q.FinishPoolGroupAttempt(ctx, sqlc.FinishPoolGroupAttemptParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID, State: "verified"}); err != nil {
		return err
	}
	if err := recordOutcomeActivity(ctx, q, row, "completed"); err != nil {
		return err
	}
	if err := devicewrite.CompleteWorkflow(ctx, q, row.AttemptID.UUID); err != nil {
		return err
	}
	row.Status = statusComplete
	row.Message = "Configuration applied"
	return save(ctx, q, row)
}

// failOperation settles only a workflow whose dispatched writes are
// definitively terminal and whose fresh snapshot has no pending controller work.
// It preserves completed effects and drops desires that were never applied.
func (s *Service) failOperation(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, desired map[string]string, snapshot Snapshot, message string) error {
	if _, err := q.GetBoardPoolRemovalAttempt(ctx, sqlc.GetBoardPoolRemovalAttemptParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID}); err == nil {
		return s.block(ctx, q, row, "Pool clearing needs recovery. The Fleet assignment was kept; "+message)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	applied := []string{}
	for _, group := range snapshot.Groups {
		if !group.Managed || !group.Configured {
			continue
		}
		for _, mac := range group.Members {
			if desired[mac] == group.ConfigRevision {
				applied = append(applied, mac)
			}
		}
	}
	if err := retainObservedIntents(ctx, q, row, snapshot); err != nil {
		return err
	}
	cutover, err := q.HasPoolConfigurationCutover(ctx, row.OrganizationID)
	if err != nil {
		return err
	}
	if !cutover {
		if err := q.DeleteUnappliedHashboardPoolIntents(ctx, sqlc.DeleteUnappliedHashboardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, AppliedMacs: applied}); err != nil {
			return err
		}
	}
	if err := q.FinishPoolGroupAttempt(ctx, sqlc.FinishPoolGroupAttemptParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID, State: "failed", ResultCode: "interrupted"}); err != nil {
		return err
	}
	if err := recordOutcomeActivity(ctx, q, row, "failed"); err != nil {
		return err
	}
	if err := devicewrite.CompleteWorkflow(ctx, q, row.AttemptID.UUID); err != nil {
		return err
	}
	row.Status = "failed"
	row.Message = message
	return save(ctx, q, row)
}

func (s *Service) prepareStep(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, request sdk.MiningGroupRequest, config, message string) error {
	affected, err := affectedBoardMACs(row, request)
	if err != nil {
		return err
	}
	if _, err := s.liveIntent(ctx, q, row, affected); err != nil {
		return s.block(ctx, q, row, "A board affected by this change is unavailable or stale; configuration is paused")
	}
	key := uuid.New()
	request.Key = key.String()
	if err := request.Validate(); err != nil {
		return err
	}
	data, err := encode(request)
	if err != nil {
		return err
	}
	ciphertext, err := s.secrets.Encrypt(data)
	if err != nil {
		return err
	}
	journal, err := encode(savedStep{Ciphertext: ciphertext, ConfigID: config, AffectedMACs: affected})
	if err != nil {
		return err
	}
	row.Step = pqtype.NullRawMessage{RawMessage: journal, Valid: true}
	row.StepKey = uuid.NullUUID{UUID: key, Valid: true}
	row.Status = statusApplying
	row.Message = message
	// No mutation in this transaction: dispatch begins on the next iteration,
	// after the exact idempotent request has durably committed.
	return save(ctx, q, row)
}

// Transfers affect only the selected MACs. Pool changes and start affect every
// member of the addressed group. Persist that membership with the step: UI
// observations may replace row.Observed while its outcome is unresolved. The
// request's revision precondition prevents applying it to changed membership.
func affectedBoardMACs(row sqlc.ContainerMiningGroupState, request sdk.MiningGroupRequest) ([]string, error) {
	if row.OperationKind == operationActivate {
		return nil, nil
	}
	switch request.Action {
	case string(Transfer):
		return request.MACs, nil
	case actionReplacePools, actionClearPools, "start", "stop":
		var observed sdk.MiningGroupResult
		if err := decode(row.Observed, &observed); err != nil {
			return nil, err
		}
		for _, group := range observed.Groups {
			if group.ID == request.GroupID && group.Revision == request.Revision {
				return group.MACs, nil
			}
		}
		return nil, fmt.Errorf("mining group step does not match its observation")
	case "create", "remove":
		// Creation and removal operate on empty groups.
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown pool assignment action %q", request.Action)
	}
}
