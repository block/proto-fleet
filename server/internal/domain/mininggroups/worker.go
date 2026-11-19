package mininggroups

import (
	"cmp"
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
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// Fleet Node allows 25 seconds for a command. Leave additional time for
// admission, transport, the acknowledgement, and committing the journal.
const reconciliationTimeout = 40 * time.Second

const (
	maxWorkflowTasks = 8
	// Separate readiness capacity can serve multiple fleets while slow reads
	// remain bounded. Each organization can occupy at most half of it.
	maxReadinessTasks             = 64
	maxOrganizationReadinessTasks = 32
	controllerDiscoveryInterval   = time.Minute
	// Reconciliation has its own 40-second I/O bound plus five seconds to
	// commit retry bookkeeping. Bound the scheduler callback as well.
	readinessTaskTimeout = reconciliationTimeout + 5*time.Second
)

// This budget counts only explicit terminal FAILED receipts, across the entire
// user operation. Timeouts, pending transfers and revision conflicts do not count.
const maxTransferFailures = 3

const (
	actionClearPools      = "clear_pools"
	actionReplacePools    = "replace_pools"
	transferPhaseComplete = "COMPLETE"
	transferPhaseFailed   = "FAILED"
)

// Readiness retains binding transactions during I/O. Leave real dispatch slots
// for accepted workflows as well as logical scheduler slots. The database's
// dispatch budget already reserves separate connections for pending markers and
// completion SQL. Small pools split their dispatch budget between both classes.
func readinessCapacity(conn *sql.DB) (int, error) {
	capacity, err := db.DispatchCapacity(conn)
	if err != nil {
		return 0, err
	}
	reserved := min(maxWorkflowTasks, max(1, capacity/2))
	return min(maxReadinessTasks, capacity-reserved), nil
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	s.run(ctx, ticker.C, s.reconcileScheduled)
}

func (s *Service) run(ctx context.Context, ticks <-chan time.Time, reconcile func(context.Context, sqlc.ListMiningGroupControllersRow) error) {
	observationLimit, err := readinessCapacity(s.conn)
	if err != nil {
		slog.ErrorContext(ctx, "mining group readiness capacity unavailable", "error", err)
	}
	organizationLimit := min(maxOrganizationReadinessTasks, max(1, observationLimit/2))
	// Keep admission across ticks so slow containers occupy only their own
	// slots. A device remains running through retry bookkeeping as well as I/O.
	var pending sync.WaitGroup
	defer pending.Wait()
	var runningMu sync.Mutex
	running := map[int64]struct{}{}
	var workflows, observations int
	observationsByOrganization := map[int64]int{}
	observationReady := make(chan struct{}, 1)
	start := func(row sqlc.ListMiningGroupControllersRow, observation bool) bool {
		runningMu.Lock()
		defer runningMu.Unlock()
		if _, exists := running[row.DeviceID]; exists {
			return true
		}
		// Idle reads cannot occupy the slots reserved for accepted changes.
		if observation && (observations >= observationLimit || observationsByOrganization[row.OrganizationID] >= organizationLimit) || !observation && workflows >= maxWorkflowTasks {
			return false
		}
		running[row.DeviceID] = struct{}{}
		if observation {
			observations++
			observationsByOrganization[row.OrganizationID]++
		} else {
			workflows++
		}
		pending.Add(1)
		go func() {
			defer pending.Done()
			defer func() {
				runningMu.Lock()
				delete(running, row.DeviceID)
				if observation {
					observations--
					observationsByOrganization[row.OrganizationID]--
				} else {
					workflows--
				}
				runningMu.Unlock()
				select {
				case observationReady <- struct{}{}:
				default:
				}
			}()
			taskCtx := ctx
			if observation {
				var cancel context.CancelFunc
				taskCtx, cancel = context.WithTimeout(ctx, readinessTaskTimeout)
				defer cancel()
			}
			if err := reconcile(taskCtx, row); err != nil {
				slog.WarnContext(ctx, "mining group reconciliation paused", "device_id", row.DeviceID, "error", err)
			}
		}()
		return true
	}
	var queuedReadiness []sqlc.ListMiningGroupReadinessControllersRow
	startReadiness := func() {
		// Scan past organizations at their limit so their slow controllers do
		// not prevent another fleet from using the remaining read capacity.
		for i := 0; i < len(queuedReadiness) && ctx.Err() == nil; {
			if !start(sqlc.ListMiningGroupControllersRow(queuedReadiness[i]), true) {
				i++
				continue
			}
			queuedReadiness = append(queuedReadiness[:i], queuedReadiness[i+1:]...)
		}
	}
	var nextDiscovery time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-observationReady:
			startReadiness()
		case <-ticks:
			if !time.Now().Before(nextDiscovery) {
				if err := s.queries.DiscoverMiningGroupControllers(ctx); err != nil {
					slog.ErrorContext(ctx, "discover mining group controllers", "error", err)
				} else {
					nextDiscovery = time.Now().Add(controllerDiscoveryInterval)
				}
			}
			rows, err := s.queries.ListMiningGroupControllers(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "list mining group operations", "error", err)
				continue
			}
			for _, r := range rows {
				if ctx.Err() != nil {
					return
				}
				if !start(r, false) {
					break
				}
			}
			ready, err := s.queries.ListMiningGroupReadinessControllers(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "list mining group readiness", "error", err)
				continue
			}
			// Merge fresh discoveries even when one organization's tasks are
			// slow. Keep existing queue order and avoid duplicate controller work.
			queued := make(map[int64]bool, len(queuedReadiness))
			for _, row := range queuedReadiness {
				queued[row.DeviceID] = true
			}
			for _, row := range ready {
				if !queued[row.DeviceID] {
					queuedReadiness = append(queuedReadiness, row)
				}
			}
			startReadiness()
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
	var recordedErr error
	// The binding transaction takes device/route locks before the journal lock.
	// It never retries external calls. The committed step survives any lost reply.
	err = s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		// Activation and pool work recheck shared assignment authority. Take its
		// organization admission before binding locks. Host deactivation and
		// return-to-Default work need only the per-device binding reservation.
		receiptOnly := initial.ControllerOperationID != ""
		if !receiptOnly && initial.OperationKind != operationDeactivate && initial.OperationKind != operationReturnDefault {
			if _, err := q.LockPoolEquipmentOrganization(ctx, orgID); err != nil {
				return err
			}
		}
		bindingErr := s.writes.WithBindingObservation(ctx, deviceID, func(q sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
			if err != nil {
				return err
			}
			if row.AttemptID != initial.AttemptID || !active(row.Status) {
				return nil
			}
			// A receipt-only pass has no organization authority reservation. If
			// another worker settled it, restart through normal mutation admission.
			if receiptOnly && row.ControllerOperationID != initial.ControllerOperationID {
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
			if row.OperationKind == operationDeactivate {
				permission = authz.PermMinerDeactivateMiningGroups
			}
			shared, authorityErr := s.authorizeSharedWorkflow(ctx, q, row)
			if authorityErr == nil && !shared {
				authorityErr = authorize(ctx, q, row.ActorID.Int64, orgID, target.SiteID, permission, authz.PermMinerStartMining, authz.PermMinerStopMining)
			}
			if err := authorityErr; err != nil {
				if isInventoryStale(err) || isWorkflowChanged(err) || poolapplications.IsBindingChanged(err) || fleeterror.IsFailedPreconditionError(err) {
					return s.handleBoardValidation(ctx, q, row, err)
				}
				if !fleeterror.IsForbiddenError(err) {
					return err
				}
				if row.OperationKind == operationActivate {
					return s.interrupt(ctx, q, row, "Operator access changed. Activation ended without further changes; review the controller's mining state before continuing.")
				}
				return s.interrupt(ctx, q, row, "Operator access changed. Applied assignments were kept; an authorized operator can submit a new request.")
			}
			if row.OperationKind == operationDeactivate {
				return s.reconcileDeactivation(ctx, q, row)
			}
			if row.OperationKind == operationReturnDefault {
				return s.reconcileDefaultReturn(ctx, q, row)
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
				return s.handleBoardValidation(ctx, q, row, err)
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
			snapshot, err := s.plannerSnapshot(ctx, q, row, observed, desired)
			if err != nil {
				if errors.Is(err, errDefaultSettingsUnavailable) {
					return s.block(ctx, q, row, defaultReadBlockedMessage(err))
				}
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
				} else {
					group, err := observedGroupAtRevision(observed.Groups, request)
					if err != nil {
						return err
					}
					request.PoolIDs, request.PoolNames = replacementPoolSlots(group.Pools, len(pools))
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
		var recorded *recordedWorkflowError
		if errors.As(bindingErr, &recorded) {
			recordedErr = recorded.err
			return nil
		}
		return bindingErr
	})
	if recordedErr != nil {
		return errors.Join(err, recordedErr)
	}
	var handoff *activationPoolHandoffReady
	if !errors.As(err, &handoff) {
		return err
	}
	applied, err := s.commitActivationPoolHandoff(ctx, handoff)
	if err != nil || !applied {
		return err
	}
	// Resume under a new shared organization lock and fresh controller observation.
	// The handoff is now complete, so this pass cannot request another handoff.
	return s.Reconcile(ctx, orgID, deviceID, identifier)
}

// Installation identity for all historical intents is checked by Reconcile
// before this call. Freshness is required only for the current selection and
// any historical boards affected by the next write.
func (s *Service) liveIntent(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, affected []string) (map[string]string, error) {
	desired := map[string]string{}
	if changesMode(row.OperationKind) {
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
			return nil, newWorkflowChangedError("Ambiguous board installation; review the selected hashboards.")
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
		return nil, newInventoryStaleError("Hashboard inventory is unavailable or stale; waiting for fresh inventory.")
	}
	for i, b := range boards {
		want := required[i]
		if b.HashboardID != want.HashboardID || b.InstallationID != want.InstallationID || b.DeviceID != row.DeviceID || b.MacAddress != want.MacAddress {
			return nil, newWorkflowChangedError("Board installation changed; review the selected hashboards.")
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

var errDefaultSettingsUnavailable = errors.New("Default mining settings unavailable")

func (s *Service) plannerSnapshot(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult, desired map[string]string) (Snapshot, error) {
	bindings := map[string]binding{}
	if err := decode(row.Bindings, &bindings); err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Enabled: observed.Enabled, MaxGroups: observed.MaxGroups, Pending: observed.Pending}
	for _, g := range observed.Groups {
		b, managed := bindings[g.ID]
		configured := false
		// The planner only uses Default's pools to verify a board that can keep
		// its inherited configuration. Transfers use the observed group revision.
		needsPools := g.ID != "default" || (b.Inherited && !observed.Pending && !observed.Suspended && slices.ContainsFunc(g.MACs, func(mac string) bool {
			want, selected := desired[mac]
			return selected && want == b.ConfigID
		}))
		if managed && b.Configured && needsPools {
			if g.ID == "default" && g.Pools == nil {
				current, err := s.readDefault(ctx, row.DeviceID)
				if err != nil {
					return Snapshot{}, fmt.Errorf("%w: %w", errDefaultSettingsUnavailable, err)
				}
				if current.Revision != g.Revision || current.Pools == nil {
					return Snapshot{}, errDefaultSettingsUnavailable
				}
				g.Pools = current.Pools
			}
			pools, err := s.groupCredentials(ctx, q, row.OrganizationID, row.DeviceID, b.ConfigID)
			if err != nil {
				return Snapshot{}, err
			}
			configured = poolsMatch(g.Pools, pools)
		}
		snapshot.Groups = append(snapshot.Groups, Group{ID: g.ID, Revision: g.Revision, ConfigRevision: b.ConfigID, Managed: managed, Configured: configured, Inherited: b.Inherited, Mining: g.Mining, CustomPolicy: g.CustomPolicy, Members: g.MACs})
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

// Resolve IDs and names before journaling so every retry sends the same pools.
func replacementPoolSlots(existing []sdk.MiningGroupPool, count int) ([]int32, []string) {
	sorted := slices.Clone(existing)
	slices.SortFunc(sorted, func(a, b sdk.MiningGroupPool) int {
		if a.Priority != b.Priority {
			return cmp.Compare(a.Priority, b.Priority)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	// The controller writes replacement rows before deleting unused rows.
	usedNames := make(map[string]bool, len(existing))
	for _, pool := range existing {
		usedNames[pool.Name] = true
	}
	ids := make([]int32, count)
	names := make([]string, count)
	for i := range min(len(sorted), count) {
		ids[i] = sorted[i].ID
		names[i] = sorted[i].Name
	}
	legacySlots := true
	for i, name := range names {
		slotName := fmt.Sprintf("Pool %d", i+1)
		if name == "" {
			name = slotName
			for suffix := 2; usedNames[name]; suffix++ {
				name = fmt.Sprintf("%s (%d)", slotName, suffix)
			}
			usedNames[name] = true
		}
		names[i] = name
		legacySlots = legacySlots && int64(ids[i]) == int64(i)+1 && name == slotName
	}
	if legacySlots {
		return nil, nil
	}
	return ids, names
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
	if err := s.validateStepBoards(ctx, q, row, step.AffectedMACs); err != nil {
		return s.handleBoardValidation(ctx, q, row, err)
	}
	// Re-authorize profiles on replay as well as when preparing a new step.
	if step.ConfigID != "" {
		if _, err := s.groupCredentials(ctx, q, row.OrganizationID, row.DeviceID, step.ConfigID); err != nil {
			return s.block(ctx, q, row, "Pool configuration is no longer available")
		}
	}
	if request.Action == string(Transfer) && row.OperationKind != operationReturnDefault {
		for _, mac := range request.MACs {
			if desired[mac] != step.ConfigID {
				return s.interrupt(ctx, q, row, "Selected board intent changed; review the selected hashboards.")
			}
		}
	}
	// Earlier versions could journal a Default start. Discard that step after
	// observation so an old request cannot start mining on an older Fleet Node.
	if row.OperationKind == operationActivate && request.Action == "start" && request.GroupID == "default" {
		return finishStep(ctx, q, row)
	}
	response, err := s.dispatchStep(ctx, row, request)
	if err != nil {
		var tracking *stepDispatchError
		if errors.As(err, &tracking) {
			return s.blockWithDiagnostic(ctx, q, row, tracking.message, err)
		}
		message := "Controller has not confirmed the change; retrying the same request"
		if (request.Action == "create" || len(request.PoolIDs) != 0) && errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
			message = "Upgrade the Fleet Node and reconnect it to apply the pool configuration."
		}
		if request.Action == operationActivate {
			message = activationWaitingMessage(err)
		}
		if request.Action == operationActivate && errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
			message = defaultReadBlockedMessage(err)
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
	case operationDeactivate:
		if !response.Deactivated {
			return s.block(ctx, q, row, "Waiting for mining group deactivation")
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
	return s.completeWithMessage(ctx, q, row, "Configuration applied")
}

func (s *Service) completeWithMessage(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, message string) error {
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
	row.Message = message
	return save(ctx, q, row)
}

// failOperation settles only a workflow whose dispatched writes are
// definitively terminal and whose fresh snapshot has no pending controller work.
// It preserves completed effects and drops desires that were never applied.
func (s *Service) failOperation(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, desired map[string]string, snapshot Snapshot, message string) error {
	if removal, err := q.GetBoardPoolRemovalAttempt(ctx, sqlc.GetBoardPoolRemovalAttemptParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID}); err == nil {
		retired, err := retireObsoleteAutomaticRemoval(ctx, q, row, removal, snapshot.Pending)
		if err != nil {
			return err
		}
		if !retired {
			return s.block(ctx, q, row, "Pool clearing needs recovery. The Fleet assignment was kept; "+message)
		}
		message = "Automatic pool clearing ended because ownership or the pool assignment changed."
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

func (s *Service) validateStepBoards(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, affected []string) error {
	if row.OperationKind == operationReturnDefault {
		_, err := s.validateDefaultReturn(ctx, q, row)
		return err
	}
	_, err := s.liveIntent(ctx, q, row, affected)
	return err
}

func (s *Service) prepareStep(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, request sdk.MiningGroupRequest, config, message string) error {
	affected, err := affectedBoardMACs(row, request)
	if err != nil {
		return err
	}
	if err := s.validateStepBoards(ctx, q, row, affected); err != nil {
		return s.handleBoardValidation(ctx, q, row, err)
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
	if err := q.RegisterMiningGroupStep(ctx, sqlc.RegisterMiningGroupStepParams{StepKey: key, AttemptID: row.AttemptID.UUID}); err != nil {
		return err
	}
	// No mutation in this transaction: dispatch begins on the next iteration,
	// after the exact idempotent request has durably committed.
	return save(ctx, q, row)
}

// Transfers affect only the selected MACs. Pool changes and start affect every
// member of the addressed group. Persist that membership with the step: UI
// observations may replace row.Observed while its outcome is unresolved. The
// request's revision precondition prevents applying it to changed membership.
func affectedBoardMACs(row sqlc.ContainerMiningGroupState, request sdk.MiningGroupRequest) ([]string, error) {
	if changesMode(row.OperationKind) {
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
		group, err := observedGroupAtRevision(observed.Groups, request)
		return group.MACs, err
	case "create", "remove":
		// Creation and removal operate on empty groups.
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown pool assignment action %q", request.Action)
	}
}

func observedGroupAtRevision(groups []sdk.MiningGroup, request sdk.MiningGroupRequest) (sdk.MiningGroup, error) {
	for _, group := range groups {
		if group.ID == request.GroupID && group.Revision == request.Revision {
			return group, nil
		}
	}
	return sdk.MiningGroup{}, fmt.Errorf("mining group step does not match its observation")
}
