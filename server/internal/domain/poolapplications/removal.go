package poolapplications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

// Board removals use the controller's durable reconciliation journal.
type BoardRemovalCoordinator interface {
	ReviewBoardRemoval(ctx context.Context, q sqlc.Querier, principal poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) error
	BeginBoardRemoval(ctx context.Context, principal poolconfigurations.Principal, removal sqlc.GetPoolRemovalRow, proof poolconfigurations.CredentialProof) error
}

func (c *Checker) WithBoardRemovals(coordinator BoardRemovalCoordinator) *Checker {
	c.boardRemovals = coordinator
	return c
}

const removalReadbackUnavailable = "check_unavailable"
const removalUncertain = "uncertain"

// Removal reviews capture authority and physical identity without requiring pool
// update capability: unassigning is valid even for offline equipment.
func (c *Checker) removalBinding(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget, removalID uuid.UUID) (sqlc.GetPoolConfigurationAssignmentRow, Binding, error) {
	var binding Binding
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, p.Scope.OrganizationID); err != nil {
		return sqlc.GetPoolConfigurationAssignmentRow{}, binding, err
	}
	current, err := c.library.AuthorizePoolConfiguration(ctx, q, p.AccountID, poolconfigurations.Read)
	if err != nil {
		return sqlc.GetPoolConfigurationAssignmentRow{}, binding, err
	}
	if current.Scope != p.Scope || current.ActorID != p.ActorID {
		return sqlc.GetPoolConfigurationAssignmentRow{}, binding, fleeterror.NewForbiddenError("equipment access changed")
	}
	a, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err != nil {
		return a, binding, err
	}
	if a.RetiredAt.Valid || a.AccountID.Int64 != p.Scope.AccountID {
		return a, binding, fleeterror.NewFailedPreconditionError("equipment assignment changed; review removal again")
	}
	target.DeviceID = a.DeviceID
	if err := lockPoolBindings(ctx, q, p.Scope, []poolconfigurations.AssignmentTarget{target}, true); err != nil {
		return a, binding, err
	}
	if err := devicewrite.RequireNoConflictingPoolWritesForRemoval(ctx, q, p.Scope.OrganizationID, target.DeviceID, removalID); err != nil {
		return a, binding, err
	}
	frozen, err := q.PoolConfigurationWritersFrozen(ctx, p.Scope.OrganizationID)
	if err != nil {
		return a, binding, err
	}
	if frozen {
		return a, binding, fleeterror.NewFailedPreconditionError("pool configuration maintenance is in progress")
	}
	owner, err := c.reviewer.authority.AuthorizePoolEquipment(ctx, q, p.Scope, p.ActorID, target)
	if err != nil {
		return a, binding, err
	}
	device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: p.Scope.OrganizationID, ID: a.DeviceID})
	if err != nil {
		return a, binding, err
	}
	if device.State != "host" {
		return a, binding, fleeterror.NewFailedPreconditionError("equipment is reserved for another operation")
	}
	if a.OperationID.Valid {
		work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: p.Scope.OrganizationID, OperationID: a.OperationID.String, TargetKind: target.Kind, TargetID: target.ID})
		if err != nil {
			return a, binding, err
		}
		if work.State == "pending" || work.State == stateApplying {
			return a, binding, fleeterror.NewFailedPreconditionError("wait for the pool configuration update to finish before removing it")
		}
	}
	binding = Binding{Version: 1, Ownership: owner, Device: device}
	if commonpb.DeviceClass(device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
		state, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: p.Scope.OrganizationID, DeviceID: a.DeviceID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return a, binding, err
		}
		if err == nil && !(removalID != uuid.Nil && state.OperationID.Valid && state.OperationID.UUID == removalID) && (state.Status == "pending" || state.Status == stateApplying || state.Status == "blocked") {
			return a, binding, fleeterror.NewFailedPreconditionError("wait for the mining-group operation to finish before removing the assignment")
		}
	}
	if target.Kind == poolconfigurations.TargetInstallation {
		rows, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: []int64{target.ID}})
		if err != nil {
			return a, binding, err
		}
		if len(rows) != 1 || !rows[0].Ready || rows[0].DeviceID != a.DeviceID {
			return a, binding, fleeterror.NewFailedPreconditionError("board installation changed or its inventory is stale")
		}
		b := rows[0]
		binding.Board = &BoardBinding{InstallationID: b.InstallationID, HashboardID: b.HashboardID, ModuleID: b.ModuleID, Position: b.Position, MAC: b.MacAddress}
	}
	return a, binding, nil
}

func (c *Checker) ReviewRemoval(ctx context.Context, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) (poolconfigurations.Removal, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Reopening an interrupted removal resumes its result, never creates a new
	// clear command. Only the original actor and exact target can recover it.
	q := sqlc.New(c.conn)
	existing, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	if err == nil && !existing.RetiredAt.Valid && existing.AccountID.Int64 == p.Scope.AccountID {
		active, err := q.ListActivePoolRemovals(ctx, sqlc.ListActivePoolRemovalsParams{OrganizationID: p.Scope.OrganizationID, DeviceID: existing.DeviceID})
		if err != nil {
			return poolconfigurations.Removal{}, err
		}
		for _, item := range active {
			removal, err := c.GetRemoval(ctx, p, item.RemovalID.String())
			if err == nil && removal.Target.Kind == target.Kind && removal.Target.ID == target.ID {
				return removal, nil
			}
		}
	}
	var assignment sqlc.GetPoolConfigurationAssignmentRow
	var binding Binding
	err = db.WithTransactionNoResult(ctx, c.conn, func(q sqlc.Querier) error {
		var err error
		assignment, binding, err = c.removalBinding(ctx, q, p, target, uuid.Nil)
		return err
	})
	if err != nil {
		return poolconfigurations.Removal{}, err
	}
	target.DeviceID = assignment.DeviceID
	// Optional hardware reads must leave time to persist an unavailable review,
	// so the user can still choose assignment-only removal for an offline device.
	observationCtx, cancelObservation := context.WithTimeout(ctx, 10*time.Second)
	defer cancelObservation()
	checked, checkErr := c.CheckConfiguration(observationCtx, p, assignment.ConfigurationID, target)
	if checkErr != nil {
		checked.State = "unavailable"
	}
	caps := c.reviewer.capabilities.GetRawCapabilitiesForDevice(observationCtx, binding.Device.DriverName, binding.Device.Manufacturer, binding.Device.Model)
	supported := target.Kind == poolconfigurations.TargetDevice && caps[sdk.CapabilityPoolClear] || target.Kind == poolconfigurations.TargetInstallation && c.boardRemovals != nil && caps[sdk.CapabilityMiningGroupPoolClear]
	if supported {
		// Static driver capability is insufficient for firmware that lacks durable
		// clearing/readback. Probe the live scope while holding authority locks.
		err := c.writes.WithHostObservation(observationCtx, p.Scope.OrganizationID, target.DeviceID, binding.Device.Epoch, func(ctx context.Context) error {
			return c.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
				a, b, err := c.removalBinding(ctx, q, p, target, uuid.Nil)
				if err != nil {
					return err
				}
				if a.Version != assignment.Version || b.Device.Epoch != binding.Device.Epoch {
					return fleeterror.NewFailedPreconditionError("equipment changed during verification")
				}
				return c.writes.WithBindingObservation(ctx, target.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
					if target.Kind == poolconfigurations.TargetInstallation {
						return c.boardRemovals.ReviewBoardRemoval(ctx, q, p, target)
					}
					miner, err := c.miners.GetMinerForCommand(ctx, target.DeviceID)
					if err != nil {
						return err
					}
					_, err = removalPoolsEmpty(ctx, miner, commonpb.DeviceClass(b.Device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
					return err
				})
			})
		})
		if fleeterror.IsUnimplementedError(err) {
			supported = false
		} else if err != nil {
			checked.State = resultUnverified
		}
	}
	cancelObservation()
	encoded, err := json.Marshal(binding)
	if err != nil {
		return poolconfigurations.Removal{}, fleeterror.NewInternalError("encode removal binding failed")
	}
	return db.WithTransaction(ctx, c.conn, func(q sqlc.Querier) (poolconfigurations.Removal, error) {
		a, b, err := c.removalBinding(ctx, q, p, target, uuid.Nil)
		if err != nil {
			return poolconfigurations.Removal{}, err
		}
		fresh, err := json.Marshal(b)
		if err != nil {
			return poolconfigurations.Removal{}, fleeterror.NewInternalError("encode removal binding failed")
		}
		if a.Version != assignment.Version || string(fresh) != string(encoded) {
			return poolconfigurations.Removal{}, fleeterror.NewFailedPreconditionError("equipment changed during verification; review removal again")
		}
		config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: p.Scope.OrganizationID, AccountID: a.AccountID, ConfigurationID: a.ConfigurationID})
		if err != nil {
			return poolconfigurations.Removal{}, err
		}
		revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: p.Scope.OrganizationID, AccountID: a.AccountID, ConfigurationID: a.ConfigurationID, RevisionID: a.RevisionID})
		if err != nil {
			return poolconfigurations.Removal{}, err
		}
		targets, err := json.Marshal([]poolconfigurations.RemovalReviewTarget{{Change: poolconfigurations.AssignmentChange{Target: target, ExpectedVersion: a.Version}, Binding: string(encoded), RevisionID: a.RevisionID, VerificationState: checked.State, ClearSupported: supported}})
		if err != nil {
			return poolconfigurations.Removal{}, fleeterror.NewInternalError("encode removal review failed")
		}
		review, err := q.CreatePoolConfigurationReview(ctx, sqlc.CreatePoolConfigurationReviewParams{ReviewID: uuid.NewString(), OrganizationID: p.Scope.OrganizationID, ConfigurationID: a.ConfigurationID, ActorID: p.ActorID, ConfigurationVersion: config.Version, AssignmentVersion: config.AssignmentVersion, Name: config.Name, Slots: revision.Slots, Targets: targets, Kind: "remove"})
		if err != nil {
			return poolconfigurations.Removal{}, err
		}
		row, err := c.loadRemoval(ctx, q, p, review.ReviewID)
		return removalResult(row), err
	})
}

func removalResult(r sqlc.GetPoolRemovalRow) poolconfigurations.Removal {
	if (r.State == "applying" || r.State == removalUncertain) && r.AttemptStatus == "unknown" && !r.KeepDeviceSettings {
		r.ResultCode = "operator_recovery_required"
	}
	return poolconfigurations.Removal{ID: r.RemovalID.String(), Target: poolconfigurations.AssignmentTarget{Kind: r.TargetKind, ID: r.TargetID, DeviceID: r.DeviceID}, VerificationState: r.VerificationState, ClearSupported: r.ClearSupported, State: r.State, ResultCode: r.ResultCode, KeepDeviceSettings: r.KeepDeviceSettings}
}

func (c *Checker) loadRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, id string) (sqlc.GetPoolRemovalRow, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return sqlc.GetPoolRemovalRow{}, fleeterror.NewInvalidArgumentError("invalid removal")
	}
	row, err := q.GetPoolRemoval(ctx, sqlc.GetPoolRemovalParams{RemovalID: parsed, OrganizationID: p.Scope.OrganizationID, AccountID: sql.NullInt64{Int64: p.Scope.AccountID, Valid: p.Scope.AccountID != 0}, ActorID: p.ActorID})
	if errors.Is(err, sql.ErrNoRows) {
		err = fleeterror.NewNotFoundError("pool removal not found")
	}
	return row, err
}

func (c *Checker) GetRemoval(ctx context.Context, p poolconfigurations.Principal, id string) (poolconfigurations.Removal, error) {
	return db.WithTransaction(ctx, c.conn, func(q sqlc.Querier) (poolconfigurations.Removal, error) {
		row, err := c.loadRemoval(ctx, q, p, id)
		if err != nil {
			return poolconfigurations.Removal{}, err
		}
		_, err = c.reviewer.authority.AuthorizePoolEquipment(ctx, q, p.Scope, p.ActorID, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID})
		return removalResult(row), err
	})
}

func (c *Checker) ValidateRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, row sqlc.GetPoolRemovalRow, allowOwn bool) error {
	if p.ActorID == 0 {
		return c.validateLifecycleRemoval(ctx, q, p, row)
	}
	allowed := uuid.Nil
	if allowOwn {
		allowed = row.RemovalID
	}
	a, b, err := c.removalBinding(ctx, q, p, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}, allowed)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return fleeterror.NewInternalError("encode removal binding failed")
	}
	if a.Version != row.AssignmentVersion || a.ConfigurationID != row.ConfigurationID || a.RevisionID != row.RevisionID || string(encoded) != row.ReviewedBinding {
		return fleeterror.NewFailedPreconditionError("equipment assignment changed; review removal again")
	}
	return nil
}

// EnqueueRemoval records the explicit desired action under the shared review lock.
// Confirmation performs no device I/O; the normal pool worker owns execution.
func (c *Checker) EnqueueRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, id string, keep bool) error {
	row, err := c.loadRemoval(ctx, q, p, id)
	if err != nil {
		return err
	}
	if row.State != "reviewed" {
		if row.KeepDeviceSettings != keep {
			return fleeterror.NewFailedPreconditionError("this removal was already confirmed with a different choice")
		}
		return nil
	}
	if !keep && (!row.ClearSupported || (row.VerificationState != "verified" && row.VerificationState != "different")) {
		return fleeterror.NewFailedPreconditionError("recheck verification or keep the device settings to remove only the assignment")
	}
	if err := c.ValidateRemoval(ctx, q, p, row, false); err != nil {
		return err
	}
	n, err := q.EnqueuePoolRemoval(ctx, sqlc.EnqueuePoolRemovalParams{ReviewID: id, KeepDeviceSettings: keep})
	if err != nil {
		return err
	}
	if n != 1 {
		return fleeterror.NewFailedPreconditionError("removal review expired or changed")
	}
	if keep {
		return c.FinishRemoval(ctx, q, p, row, true)
	}
	return nil
}

// ExecuteRemoval consumes a pending pool-free intent. The recorded actor's live
// authority and exact assignment generation are revalidated before dispatch.
func (c *Checker) ExecuteRemoval(ctx context.Context, p poolconfigurations.Principal, id string) error {
	row, err := c.loadRemoval(ctx, sqlc.New(c.conn), p, id)
	if err != nil {
		return err
	}
	if row.State != "pending" {
		return nil
	}
	ctx = devicewrite.WithPoolOperation(ctx, row.RemovalID)
	proof := func(ctx context.Context, q sqlc.Querier) error { return c.ValidateRemoval(ctx, q, p, row, true) }
	if row.TargetKind == poolconfigurations.TargetInstallation {
		if c.boardRemovals == nil {
			return fleeterror.NewUnavailableErrorf("hashboard pool clearing is unavailable")
		}
		err = c.boardRemovals.BeginBoardRemoval(ctx, p, row, proof)
	} else {
		_, err = c.clearRemoval(ctx, p, row, proof)
	}
	if err != nil && !temporaryPreparationFailure(err) {
		// Only pending work can be rejected here: an admitted attempt retains its
		// own stronger outcome and recovery requirements.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, saveErr := sqlc.New(c.conn).RejectPendingPoolRemoval(cleanupCtx, row.RemovalID.String())
		if saveErr != nil {
			return saveErr
		}
	}
	return err
}

func (c *Checker) RecoverRemoval(ctx context.Context, p poolconfigurations.Principal, id string) (poolconfigurations.Removal, error) {
	row, err := c.loadRemoval(ctx, sqlc.New(c.conn), p, id)
	if err != nil {
		return poolconfigurations.Removal{}, err
	}
	return c.recoverRemoval(ctx, p, row)
}

func (c *Checker) FinishRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, row sqlc.GetPoolRemovalRow, keep bool) error {
	target := poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}
	n, err := q.FinishPoolRemoval(ctx, sqlc.FinishPoolRemovalParams{RemovalID: row.RemovalID, State: "completed"})
	if err != nil {
		return err
	}
	if n != 1 {
		return fleeterror.NewFailedPreconditionError("removal changed")
	}
	if p.ActorID == 0 {
		if !keep && row.TargetKind == poolconfigurations.TargetDevice {
			if err := q.CompleteCoveredContainerPoolClears(ctx, row.RemovalID); err != nil {
				return err
			}
		}
		return poolconfigurations.RecordRemovalAudit(ctx, q, p, row.RemovalID.String(), "", false)
	}
	if err := poolconfigurations.NewRepository(c.secrets).RetireAssignment(ctx, q, p.Scope, target, row.AssignmentVersion); err != nil {
		return err
	}
	return poolconfigurations.RecordRemovalAudit(ctx, q, p, row.RemovalID.String(), row.ConfigurationID, keep)
}

// A new physical attempt and the removal admission commit before dispatch. The
// assignment remains active until complete stored readback establishes emptiness.
func (c *Checker) clearRemoval(ctx context.Context, p poolconfigurations.Principal, row sqlc.GetPoolRemovalRow, proof poolconfigurations.CredentialProof) (poolconfigurations.Removal, error) {
	var binding Binding
	if json.Unmarshal([]byte(row.ReviewedBinding), &binding) != nil {
		return poolconfigurations.Removal{}, fleeterror.NewInternalError("invalid removal binding")
	}
	admitted, dispatched := false, false
	resultCode := "write_failed"
	err := c.writes.WithRecordedHostAttempt(ctx, p.Scope.OrganizationID, row.DeviceID, binding.Device.Epoch, commandtype.UpdateMiningPools, func(q sqlc.Querier, id uuid.UUID) error {
		n, err := q.AcceptPoolRemoval(ctx, sqlc.AcceptPoolRemovalParams{RemovalID: row.RemovalID, PhysicalAttemptID: uuid.NullUUID{UUID: id, Valid: true}})
		if err != nil {
			return err
		}
		if n != 1 {
			return fleeterror.NewFailedPreconditionError("removal changed; reload its result")
		}
		return nil
	}, func(ctx context.Context) error {
		admitted = true
		return c.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			if err := c.ValidateRemoval(ctx, q, p, row, true); err != nil {
				return err
			}
			if err := proof(ctx, q); err != nil {
				return err
			}
			miner, err := c.miners.GetMinerForCommand(ctx, row.DeviceID)
			if err != nil {
				return err
			}
			// Read before clearing. An unavailable device or incomplete readback cannot
			// silently convert the reviewed action into assignment-only removal.
			if _, err := removalPoolsEmpty(ctx, miner, commonpb.DeviceClass(binding.Device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER); err != nil {
				resultCode = removalReadbackUnavailable
				return err
			}
			return c.writes.WithBindingDispatch(ctx, row.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
				dispatched = true
				if err := miner.UpdateMiningPools(ctx, dto.UpdateMiningPoolsPayload{ClearAll: true}); err != nil {
					return err
				}
				empty, err := removalPoolsEmpty(ctx, miner, commonpb.DeviceClass(binding.Device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
				if err != nil {
					resultCode = removalReadbackUnavailable
					return err
				}
				if !empty {
					resultCode = "pools_remain"
					return fleeterror.NewFailedPreconditionError("device pools remain configured")
				}
				// This transaction holds binding and authority locks until retirement.
				return c.FinishRemoval(ctx, q, p, row, false)
			})
		})
	})
	if err != nil && admitted {
		state := "failed"
		if dispatched && !devicewrite.IsNotDispatched(err) {
			state = removalUncertain
		}
		if p.ActorID == 0 && !dispatched {
			// Persist positive no-dispatch evidence. Failed verification after
			// an ordered write can also have an empty physical receipt.
			resultCode = "clear_not_dispatched"
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		saveErr := db.WithTransactionNoResult(finishCtx, c.conn, func(q sqlc.Querier) error {
			if _, err := q.FinishPoolRemoval(finishCtx, sqlc.FinishPoolRemovalParams{RemovalID: row.RemovalID, State: state, ResultCode: resultCode}); err != nil {
				return err
			}
			if p.ActorID == 0 && !dispatched {
				return q.RetryUndispatchedPoolLifecycleRemoval(finishCtx, row.RemovalID)
			}
			return nil
		})
		if saveErr != nil {
			return poolconfigurations.Removal{}, saveErr
		}
	} else if err != nil {
		return poolconfigurations.Removal{}, err
	}
	return c.GetRemoval(ctx, p, row.RemovalID.String())
}

// Empty storage alone is insufficient: a firmware override or a failed reload
// can leave runtime pools active. Apply the same checks during recovery.
func removalPoolsEmpty(ctx context.Context, miner interfaces.Miner, container bool) (bool, error) {
	if container {
		groups, ok := miner.(sdk.DeviceMiningGroups)
		if !ok {
			return false, fleeterror.NewFailedPreconditionError("container pool mode is unavailable")
		}
		mode, err := groups.MiningGroups(ctx, sdk.MiningGroupRequest{Action: "observe"})
		if err != nil || !mode.Supported || mode.Enabled || mode.Pending || mode.Suspended {
			return false, fleeterror.NewFailedPreconditionError("clearing requires a container in legacy pool mode with no mining-group operation in progress")
		}
	}
	if verifier, ok := miner.(interfaces.PoolClearVerifier); ok {
		return verifier.VerifyPoolsCleared(ctx)
	}
	reader, ok := miner.(interfaces.StoredMiningPoolReader)
	if !ok {
		return false, fleeterror.NewUnimplementedError("complete stored pool readback is unavailable")
	}
	pools, err := reader.GetStoredMiningPools(ctx)
	if err != nil {
		return false, err
	}
	for _, pool := range pools {
		if pool.URL != "" || pool.Username != "" || pool.Password != nil && *pool.Password != "" {
			return false, nil
		}
		if pool.Password == nil {
			return false, fleeterror.NewFailedPreconditionError("stored pool credentials could not be verified as empty")
		}
	}
	runtime, err := miner.GetMiningPools(ctx)
	if err != nil {
		return false, err
	}
	for _, pool := range runtime {
		if pool.URL != "" || pool.Username != "" {
			return false, nil
		}
	}
	return true, nil
}

// Retrying an accepted removal observes only. It never sends another clear.
// Unresolved transport attempts must first use the existing write recovery flow.
func (c *Checker) recoverRemoval(ctx context.Context, p poolconfigurations.Principal, row sqlc.GetPoolRemovalRow) (poolconfigurations.Removal, error) {
	if (row.State != "applying" && row.State != removalUncertain) || row.TargetKind == poolconfigurations.TargetInstallation || row.AttemptStatus != "complete" || row.KeepDeviceSettings {
		return c.GetRemoval(ctx, p, row.RemovalID.String())
	}
	var binding Binding
	if json.Unmarshal([]byte(row.ReviewedBinding), &binding) != nil {
		return poolconfigurations.Removal{}, fleeterror.NewInternalError("invalid removal binding")
	}
	err := c.writes.WithHostObservation(ctx, p.Scope.OrganizationID, row.DeviceID, binding.Device.Epoch, func(ctx context.Context) error {
		return c.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			if err := c.ValidateRemoval(ctx, q, p, row, true); err != nil {
				return err
			}
			miner, err := c.miners.GetMinerForCommand(ctx, row.DeviceID)
			if err != nil {
				return err
			}
			empty, err := removalPoolsEmpty(ctx, miner, commonpb.DeviceClass(binding.Device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
			if err != nil {
				return err
			}
			if !empty {
				_, err = q.FinishPoolRemoval(ctx, sqlc.FinishPoolRemovalParams{RemovalID: row.RemovalID, State: "failed", ResultCode: "pools_remain"})
				return err
			}
			return c.FinishRemoval(ctx, q, p, row, false)
		})
	})
	if err != nil {
		return poolconfigurations.Removal{}, err
	}
	return c.GetRemoval(ctx, p, row.RemovalID.String())
}
