package poolapplications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

// ProcessLifecycle prepares durable automatic intent. It never writes device
// settings itself; accepted work uses the normal apply or removal executor.
func (w *Worker) ProcessLifecycle(ctx context.Context, id uuid.UUID) error {
	ctx = poolconfigurations.WithLifecycleTransition(ctx, id)
	ctx = devicewrite.WithPoolOperation(ctx, id)
	// Reconcile a persisted pre-dispatch failure before loading work. This also
	// recovers a process that stopped between failure recording and retry.
	if err := w.queries.RetryUndispatchedPoolLifecycleRemoval(ctx, id); err != nil {
		return err
	}
	if err := w.queries.ReprepareUndispatchedPoolLifecycleRemoval(ctx, id); err != nil {
		return err
	}
	row, err := w.resolveLifecycle(ctx, id)
	if err != nil {
		return err
	}
	if (row.State == "uncertain" || row.State == stateApplying) && !row.ConfigurationID.Valid && row.TargetKind == poolconfigurations.TargetDevice && w.removals != nil {
		p := poolconfigurations.Principal{Scope: poolconfigurations.Scope{OrganizationID: row.OrganizationID, AccountID: row.OwnerAccountID}, AccountID: row.ExternalAccountID, AccountName: row.AccountName}
		_, err := w.removals.RecoverRemoval(ctx, p, id.String())
		if err != nil {
			return err
		}
		return w.queries.DeferPoolLifecycleTransition(ctx, sqlc.DeferPoolLifecycleTransitionParams{TransitionID: id, ResultCode: row.ResultCode})
	}
	if row.State != "pending" && row.State != "ready" {
		return nil
	}
	newer, err := w.queries.PoolLifecycleHasNewerAssignment(ctx, id)
	if err != nil {
		return err
	}
	if newer {
		_, err := w.queries.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: id, State: "superseded", ResultCode: "newer_assignment"})
		return err
	}

	currentOwner, err := w.queries.GetPoolLifecycleAllocation(ctx, sqlc.GetPoolLifecycleAllocationParams{OrganizationID: row.OrganizationID, Kind: row.AssetKind, AssetID: row.AssetID, DeviceID: row.DeviceID})
	if err != nil {
		return err
	}
	if currentOwner.OwnerAccountID != row.OwnerAccountID || currentOwner.Revision != row.AllocationRevision {
		_, err := w.queries.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: id, State: "superseded", ResultCode: "ownership_changed"})
		return err
	}
	if row.Reason == "pairing" && !row.ConfigurationID.Valid {
		_, err := w.queries.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: id, State: "completed", ResultCode: "no_default"})
		return err
	}
	scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID, AccountID: row.OwnerAccountID}
	p := poolconfigurations.Principal{Scope: scope, AccountID: row.ExternalAccountID, AccountName: row.AccountName}
	target, err := lifecycleTarget(ctx, w.queries, row)
	if err != nil {
		return err
	}
	if row.State == "pending" {
		if err := NewReadiness(w.conn, w.reviewer, w.miners).RefreshTargets(ctx, scope, 0, []poolconfigurations.AssignmentTarget{target}); err != nil {
			return err
		}
		err = db.WithTransactionNoResult(ctx, w.conn, func(q sqlc.Querier) error {
			if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
				return err
			}
			current, err := poolconfigurations.AuthorizeLifecycleTransition(ctx, q, scope, row.DeviceID)
			if err != nil {
				return err
			}
			if current.State != "pending" {
				return nil
			}
			runtime, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: scope.OrganizationID, ID: row.DeviceID})
			if err != nil {
				return err
			}
			if commonpb.DeviceClass(runtime.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
				state, err := q.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: scope.OrganizationID, DeviceID: row.DeviceID})
				if err != nil {
					return err
				}
				var observed sdk.MiningGroupResult
				if json.Unmarshal(state.Observed, &observed) != nil || !observed.Supported || observed.Pending || observed.Suspended || !state.ObservedAt.Valid || time.Since(state.ObservedAt.Time) > 2*time.Minute {
					return fleeterror.NewFailedPreconditionError("container pool mode is unavailable")
				}
				if !observed.Enabled {
					target = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID}
					// Whole-container authority must include every installed board.
					if _, err := w.reviewer.authority.AuthorizePoolEquipment(ctx, q, scope, 0, target); err != nil {
						return err
					}
					if err := q.SupersedeContainerPoolLifecycleTransitions(ctx, sqlc.SupersedeContainerPoolLifecycleTransitionsParams{OrganizationID: scope.OrganizationID, DeviceID: row.DeviceID, OwnerAccountID: scope.AccountID, TransitionID: id}); err != nil {
						return err
					}
				}
			}
			var binding string
			if current.ConfigurationID.Valid {
				revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: scope.OrganizationID, AccountID: sql.NullInt64{Int64: scope.AccountID, Valid: scope.AccountID != 0}, ConfigurationID: current.ConfigurationID.String, RevisionID: current.RevisionID.String})
				if err != nil {
					return err
				}
				slots, err := poolconfigurations.DecodeSlots(revision.Slots)
				if err != nil {
					return err
				}
				endpoints := make([]poolconfigurations.Endpoint, len(slots))
				for i, slot := range slots {
					endpoints[i] = poolconfigurations.Endpoint{ID: slot.ID, URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, HasPassword: slot.HasPassword}
				}
				approvals, err := w.reviewer.ReviewTargets(ctx, q, scope, 0, []poolconfigurations.AssignmentTarget{target}, endpoints)
				if err != nil {
					return err
				}
				binding = approvals[0].Binding
			} else {
				if w.removals == nil {
					return fleeterror.NewInternalError("pool clearing is unavailable")
				}
				b, err := w.removals.lifecycleRemovalBinding(ctx, q, p, target)
				if err != nil {
					return err
				}
				encoded, err := json.Marshal(b)
				if err != nil {
					return fleeterror.NewInternalError("encode lifecycle binding failed")
				}
				binding = string(encoded)
			}
			_, err = q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: target.Kind, TargetID: target.ID, ReviewedBinding: binding})
			return err
		})
		if err != nil {
			return err
		}
		row, err = w.queries.GetPoolLifecycleTransition(ctx, id)
		if err != nil {
			return err
		}
	}
	if row.State != "ready" {
		return nil
	}
	if !row.ConfigurationID.Valid {
		return w.removals.ExecuteRemoval(ctx, p, id.String())
	}
	return db.WithTransactionNoResult(ctx, w.conn, func(q sqlc.Querier) error {
		if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
			return err
		}
		// Pairing changes do not take the organization lock. Retain the device
		// and pairing locks through assignment creation, so an old cycle cannot
		// create a "newer" assignment after the next cycle captures its default.
		if row.Reason == "pairing" {
			if _, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: scope.OrganizationID, ID: row.DeviceID}); err != nil {
				return err
			}
			if _, err := q.LockMinerBindingPairing(ctx, row.DeviceID); err != nil {
				return err
			}
		}
		current, err := poolconfigurations.AuthorizeLifecycleTransition(ctx, q, scope, row.DeviceID)
		if err != nil {
			return err
		}
		if current.State != "ready" {
			return nil
		}
		target := poolconfigurations.AssignmentTarget{Kind: current.TargetKind, ID: current.TargetID, DeviceID: current.DeviceID}
		previous, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		operation, err := w.repository.Assign(ctx, q, scope, 0, current.ConfigurationID.String, current.RevisionID.String, []poolconfigurations.AssignmentChange{{Target: target, ExpectedVersion: previous.Version, ReviewedBinding: current.ReviewedBinding}})
		if err != nil {
			return err
		}
		if err := q.LinkPoolLifecycleOperation(ctx, sqlc.LinkPoolLifecycleOperationParams{TransitionID: id, OperationID: sql.NullString{String: operation.ID, Valid: true}}); err != nil {
			return err
		}
		return poolconfigurations.RecordLifecycleAssignmentAudit(ctx, q, p, current.ConfigurationID.String, operation.ID)
	})
}

func lifecycleTarget(ctx context.Context, q sqlc.Querier, row sqlc.GetPoolLifecycleTransitionRow) (poolconfigurations.AssignmentTarget, error) {
	if row.State != "pending" && row.TargetID > 0 {
		return poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}, nil
	}
	if row.AssetKind == 5 {
		return poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID}, nil
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: row.OrganizationID, HashboardIds: []int64{row.AssetID}})
	if err != nil {
		return poolconfigurations.AssignmentTarget{}, err
	}
	if len(boards) != 1 || boards[0].DeviceID != row.DeviceID || !boards[0].Ready {
		return poolconfigurations.AssignmentTarget{}, fleeterror.NewFailedPreconditionError("board inventory is unavailable")
	}
	return poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: boards[0].InstallationID, DeviceID: row.DeviceID}, nil
}

func (w *Worker) resolveLifecycle(ctx context.Context, id uuid.UUID) (sqlc.GetPoolLifecycleTransitionRow, error) {
	return db.WithTransaction(ctx, w.conn, func(q sqlc.Querier) (sqlc.GetPoolLifecycleTransitionRow, error) {
		row, err := q.GetPoolLifecycleTransition(ctx, id)
		if err != nil {
			return row, err
		}
		if (row.State == "pending" || row.State == "ready") && !row.CurrentPairingCycle {
			_, err := q.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: id, State: "superseded", ResultCode: "pairing_cycle_ended"})
			row.State = "superseded"
			return row, err
		}
		if row.State != "pending" || row.AssetKind != 0 {
			return row, nil
		}
		if _, err := q.LockPoolConfigurationImportOrganization(ctx, row.OrganizationID); err != nil {
			return row, err
		}
		row, err = q.GetPoolLifecycleTransition(ctx, id)
		if err != nil {
			return row, err
		}
		if row.State != "pending" || row.AssetKind != 0 {
			return row, nil
		}
		device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: row.OrganizationID, ID: row.DeviceID})
		if err != nil {
			return row, err
		}
		// Expansion shares the parent's cycle. Serialize with unpair before
		// creating children, including when the parent was loaded earlier.
		if _, err := q.LockMinerBindingPairing(ctx, row.DeviceID); err != nil {
			return row, err
		}
		row, err = q.GetPoolLifecycleTransition(ctx, id)
		if err != nil {
			return row, err
		}
		if row.State != "pending" || !row.CurrentPairingCycle {
			return row, nil
		}
		if commonpb.DeviceClass(device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
			owners, err := q.ListPoolEquipmentModuleOwners(ctx, sqlc.ListPoolEquipmentModuleOwnersParams{OrgID: row.OrganizationID, ID: row.DeviceID})
			if err != nil {
				return row, err
			}
			if len(owners) == 0 {
				return row, fleeterror.NewFailedPreconditionError("container inventory is unavailable")
			}
			for _, owner := range owners {
				if owner.AccountID < 0 || owner.HashboardID == 0 {
					return row, fleeterror.NewFailedPreconditionError("container inventory is incomplete")
				}
			}
			for _, owner := range owners {
				if err := q.CreatePoolLifecycleBoardTransition(ctx, sqlc.CreatePoolLifecycleBoardTransitionParams{ParentID: id, AssetID: owner.HashboardID, OwnerAccountID: owner.AccountID, AllocationRevision: owner.Revision}); err != nil {
					return row, err
				}
			}
			_, err = q.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: id, State: "completed"})
			row.State = "completed"
			return row, err
		}
		return row, fleeterror.NewFailedPreconditionError("pairing container identity changed")
	})
}
