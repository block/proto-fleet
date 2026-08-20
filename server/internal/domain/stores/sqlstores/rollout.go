package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type SQLRolloutStore struct {
	conn *sql.DB
}

var _ rollout.Store = (*SQLRolloutStore)(nil)

func NewSQLRolloutStore(conn *sql.DB) *SQLRolloutStore {
	return &SQLRolloutStore{conn: conn}
}

func rolloutPositionToInt32(value int) (int32, error) {
	if value < 0 || value > math.MaxInt32 {
		return 0, fmt.Errorf("rollout position %d is outside the int32 range", value)
	}
	return int32(value), nil
}

func (s *SQLRolloutStore) Create(
	ctx context.Context,
	req rollout.CreateRequest,
) (rollout.CreateResult, error) {
	type createResult struct {
		row      sqlc.FirmwareRollout
		replayed bool
	}
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (createResult, error) {
		existing, getErr := q.GetFirmwareRolloutByIdempotencyKey(
			ctx,
			sqlc.GetFirmwareRolloutByIdempotencyKeyParams{
				OrgID:          req.OrgID,
				IdempotencyKey: req.IdempotencyKey,
			},
		)
		switch {
		case getErr == nil:
			if existing.CreateFingerprint != req.RequestFingerprint {
				return createResult{}, rollout.ErrIdempotencyConflict
			}
			return createResult{row: existing, replayed: true}, nil
		case !errors.Is(getErr, sql.ErrNoRows):
			return createResult{}, getErr
		}

		authorityID := uuid.New()
		authority, createAuthorityErr := q.CreateChannelFirmwareAuthority(
			ctx,
			sqlc.CreateChannelFirmwareAuthorityParams{
				ID:                 authorityID,
				OrgID:              req.OrgID,
				AuthorityType:      "rollout",
				AuthorityReference: req.ID.String(),
				CreatedByUserID:    req.ActorUserID,
			},
		)
		if createAuthorityErr != nil {
			return createResult{}, createAuthorityErr
		}
		row, createErr := q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
			RolloutID:                req.ID,
			OrgID:                    req.OrgID,
			Name:                     req.Name,
			StrategyKey:              req.StrategyKey,
			ForwardAuthorityID:       authority.ID,
			ForwardAuthorityRevision: authority.Revision,
			SourceChannelID:          ptrToNullInt64(req.SourceChannelID),
			TargetChannelID:          ptrToNullInt64(req.TargetChannelID),
			SourceReleaseSetID:       ptrToNullInt64(req.SourceReleaseSetID),
			TargetReleaseSetID:       ptrToNullInt64(req.TargetReleaseSetID),
			SourceSnapshot:           marshalSnapshot(req.SourceSnapshot),
			TargetSnapshot:           marshalSnapshot(req.TargetSnapshot),
			RevertSnapshot:           marshalSnapshot(req.RevertSnapshot),
			HashratePolicyMaxDropBasisPoints: ptrToNullInt32(
				hashratePolicyMaxDrop(req.HashratePolicy),
			),
			HashratePolicyHealthyDurationSeconds: ptrToNullInt32(
				hashratePolicyHealthyDuration(req.HashratePolicy),
			),
			IdempotencyKey:    req.IdempotencyKey,
			CreateFingerprint: req.RequestFingerprint,
			Reason:            req.Reason,
			CreatedByUserID:   req.ActorUserID,
		})
		if createErr != nil {
			return createResult{}, createErr
		}

		type batchInput struct {
			Position int32  `json:"position"`
			Label    string `json:"label"`
		}
		type memberInput struct {
			BatchPosition    int32          `json:"batch_position"`
			Position         int32          `json:"position"`
			DeviceIdentifier string         `json:"device_identifier"`
			SourceSnapshot   map[string]any `json:"source_snapshot"`
			TargetSnapshot   map[string]any `json:"target_snapshot"`
			RevertSnapshot   map[string]any `json:"revert_snapshot"`
		}
		batchInputs := make([]batchInput, 0, len(req.Batches))
		memberInputs := make([]memberInput, 0)
		memberPosition := 0
		for batchPosition, inputBatch := range req.Batches {
			batchPositionInt32, positionErr := rolloutPositionToInt32(batchPosition)
			if positionErr != nil {
				return createResult{}, fmt.Errorf("convert rollout batch position: %w", positionErr)
			}
			batchInputs = append(batchInputs, batchInput{
				Position: batchPositionInt32,
				Label:    inputBatch.Label,
			})
			for _, inputMember := range inputBatch.Members {
				memberPositionInt32, positionErr := rolloutPositionToInt32(memberPosition)
				if positionErr != nil {
					return createResult{}, fmt.Errorf("convert rollout member position: %w", positionErr)
				}
				memberInputs = append(memberInputs, memberInput{
					BatchPosition:    batchPositionInt32,
					Position:         memberPositionInt32,
					DeviceIdentifier: inputMember.DeviceIdentifier,
					SourceSnapshot:   nonNilSnapshot(inputMember.SourceSnapshot),
					TargetSnapshot:   nonNilSnapshot(inputMember.TargetSnapshot),
					RevertSnapshot:   nonNilSnapshot(inputMember.RevertSnapshot),
				})
				memberPosition++
			}
		}
		batchJSON, marshalErr := json.Marshal(batchInputs)
		if marshalErr != nil {
			return createResult{}, fmt.Errorf("marshal rollout batches: %w", marshalErr)
		}
		batches, batchErr := q.CreateFirmwareRolloutBatches(
			ctx,
			sqlc.CreateFirmwareRolloutBatchesParams{
				RolloutID: req.ID,
				OrgID:     req.OrgID,
				Batches:   batchJSON,
			},
		)
		if batchErr != nil {
			return createResult{}, batchErr
		}
		if len(batches) != len(batchInputs) {
			return createResult{}, fmt.Errorf("inserted %d of %d rollout batches", len(batches), len(batchInputs))
		}
		memberJSON, marshalErr := json.Marshal(memberInputs)
		if marshalErr != nil {
			return createResult{}, fmt.Errorf("marshal rollout members: %w", marshalErr)
		}
		members, memberErr := q.CreateFirmwareRolloutMembers(
			ctx,
			sqlc.CreateFirmwareRolloutMembersParams{
				RolloutID: req.ID,
				OrgID:     req.OrgID,
				Members:   memberJSON,
			},
		)
		if memberErr != nil {
			return createResult{}, memberErr
		}
		if len(members) != len(memberInputs) {
			return createResult{}, rollout.ErrNotFound
		}
		if _, causeErr := q.CreateFirmwareRolloutCause(
			ctx,
			sqlc.CreateFirmwareRolloutCauseParams{
				RolloutID:         req.ID,
				OrgID:             req.OrgID,
				Operation:         "create",
				Reason:            req.Reason,
				ActorUserID:       req.ActorUserID,
				ActorType:         persistedActorType(req.ActorType),
				ActorCredentialID: ptrToNullString(req.ActorCredentialID),
				ToState:           string(rollout.StateCreated),
				RolloutRevision:   row.Revision,
			},
		); causeErr != nil {
			return createResult{}, causeErr
		}
		return createResult{row: row}, nil
	})
	if err != nil {
		if isUniqueViolationOn(err, "uq_firmware_rollout_active_owner") {
			return rollout.CreateResult{}, rollout.ErrOwnershipConflict
		}
		if isUniqueViolationOn(err, "uq_firmware_rollout_idempotency") {
			existing, getErr := s.getByIdempotencyKey(ctx, req.OrgID, req.IdempotencyKey)
			if getErr == nil && existing.CreateFingerprint == req.RequestFingerprint {
				full, fullErr := s.Get(ctx, req.OrgID, existing.ID)
				return rollout.CreateResult{Rollout: full, Replayed: true}, fullErr
			}
			return rollout.CreateResult{}, rollout.ErrIdempotencyConflict
		}
		return rollout.CreateResult{}, fmt.Errorf("create firmware rollout: %w", err)
	}
	full, err := s.Get(ctx, req.OrgID, result.row.ID)
	if err != nil {
		return rollout.CreateResult{}, err
	}
	return rollout.CreateResult{Rollout: full, Replayed: result.replayed}, nil
}

func (s *SQLRolloutStore) Get(
	ctx context.Context,
	orgID int64,
	rolloutID uuid.UUID,
) (*rollout.Rollout, error) {
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*rollout.Rollout, error) {
		row, getErr := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{
			RolloutID: rolloutID,
			OrgID:     orgID,
		})
		if getErr != nil {
			return nil, getErr
		}
		return loadRollout(ctx, q, row)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rollout.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get firmware rollout: %w", err)
	}
	return result, nil
}

func (s *SQLRolloutStore) List(
	ctx context.Context,
	orgID int64,
	states []rollout.State,
) ([]rollout.Rollout, error) {
	stateValues := make([]string, len(states))
	for index, state := range states {
		stateValues[index] = string(state)
	}
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) ([]rollout.Rollout, error) {
		rows, listErr := q.ListFirmwareRollouts(ctx, sqlc.ListFirmwareRolloutsParams{
			OrgID:  orgID,
			States: stateValues,
		})
		if listErr != nil {
			return nil, listErr
		}
		loaded := make([]rollout.Rollout, 0, len(rows))
		for _, row := range rows {
			item, loadErr := loadRollout(ctx, q, row)
			if loadErr != nil {
				return nil, loadErr
			}
			loaded = append(loaded, *item)
		}
		return loaded, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list firmware rollouts: %w", err)
	}
	return result, nil
}

func (s *SQLRolloutStore) CheckControlReplay(
	ctx context.Context,
	req rollout.ControlRequest,
) (bool, error) {
	control, err := db.WithTransaction(
		ctx,
		s.conn,
		func(q sqlc.Querier) (sqlc.FirmwareRolloutControl, error) {
			return q.GetFirmwareRolloutControlByKey(
				ctx,
				sqlc.GetFirmwareRolloutControlByKeyParams{
					RolloutID:      req.RolloutID,
					OrgID:          req.OrgID,
					IdempotencyKey: req.IdempotencyKey,
				},
			)
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check firmware rollout control replay: %w", err)
	}
	if control.Operation != string(req.Operation) ||
		control.RequestFingerprint != req.RequestFingerprint {
		return false, rollout.ErrIdempotencyConflict
	}
	return true, nil
}

func (s *SQLRolloutStore) ApplyControl(
	ctx context.Context,
	req rollout.ControlRequest,
) (rollout.ControlResult, error) {
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (rollout.ControlResult, error) {
		identity, getErr := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{
			RolloutID: req.RolloutID,
			OrgID:     req.OrgID,
		})
		if getErr != nil {
			return rollout.ControlResult{}, getErr
		}
		if identity.StrategyKey == betweenchannel.StrategyKey {
			lane, laneErr := q.GetRolloutLaneForRollout(
				ctx,
				sqlc.GetRolloutLaneForRolloutParams{
					RolloutID: uuid.NullUUID{UUID: req.RolloutID, Valid: true},
					OrgID:     req.OrgID,
				},
			)
			if laneErr != nil {
				return rollout.ControlResult{}, laneErr
			}
			if _, laneErr = q.LockRolloutLane(
				ctx,
				sqlc.LockRolloutLaneParams{
					LaneID: lane.ID,
					OrgID:  req.OrgID,
				},
			); laneErr != nil {
				return rollout.ControlResult{}, laneErr
			}
		}

		current, lockErr := q.LockFirmwareRollout(ctx, sqlc.LockFirmwareRolloutParams{
			RolloutID: req.RolloutID,
			OrgID:     req.OrgID,
		})
		if lockErr != nil {
			return rollout.ControlResult{}, lockErr
		}
		if current.StrategyKey != identity.StrategyKey {
			return rollout.ControlResult{}, rollout.ErrRevisionConflict
		}

		existing, existingErr := q.GetFirmwareRolloutControlByKey(
			ctx,
			sqlc.GetFirmwareRolloutControlByKeyParams{
				RolloutID:      req.RolloutID,
				OrgID:          req.OrgID,
				IdempotencyKey: req.IdempotencyKey,
			},
		)
		switch {
		case existingErr == nil:
			if existing.Operation != string(req.Operation) ||
				existing.RequestFingerprint != req.RequestFingerprint {
				return rollout.ControlResult{}, rollout.ErrIdempotencyConflict
			}
			loaded, loadErr := loadRollout(ctx, q, current)
			if loadErr != nil {
				return rollout.ControlResult{}, loadErr
			}
			return rollout.ControlResult{
				Rollout:  loaded,
				Batch:    findBatch(loaded.Batches, existing.BatchID),
				Control:  controlFromSQL(existing),
				Replayed: true,
			}, nil
		case !errors.Is(existingErr, sql.ErrNoRows):
			return rollout.ControlResult{}, existingErr
		}

		if current.Revision != req.ExpectedRevision {
			return rollout.ControlResult{}, rollout.ErrRevisionConflict
		}
		resumeState := rollout.StateRunning
		if current.ResumeState.Valid {
			resumeState = rollout.State(current.ResumeState.String)
		}
		targetState, stateErr := rollout.NextState(
			rollout.State(current.State),
			req.Operation,
			resumeState,
			req.WithFailures,
		)
		if stateErr != nil {
			return rollout.ControlResult{}, stateErr
		}

		var selectedBatch sqlc.FirmwareRolloutBatch
		var selectedBatchID sql.NullInt64
		if req.Operation == rollout.ControlOperationAdmit ||
			req.Operation == rollout.ControlOperationContinue {
			batchID := sql.NullInt64{}
			if req.Operation == rollout.ControlOperationAdmit && req.BatchID > 0 {
				batchID = sql.NullInt64{Int64: req.BatchID, Valid: true}
			}
			selectedBatch, stateErr = q.GetFirmwareRolloutBatchForControl(
				ctx,
				sqlc.GetFirmwareRolloutBatchForControlParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
					BatchID:   batchID,
				},
			)
			if errors.Is(stateErr, sql.ErrNoRows) {
				return rollout.ControlResult{}, fmt.Errorf(
					"%w: no pending rollout batch is available",
					rollout.ErrInvalidTransition,
				)
			}
			if stateErr != nil {
				return rollout.ControlResult{}, stateErr
			}
			selectedBatchID = sql.NullInt64{Int64: selectedBatch.ID, Valid: true}
		}

		forwardRevision := sql.NullInt64{}
		revertAuthorityID := uuid.NullUUID{}
		revertAuthorityRevision := sql.NullInt64{}
		switch req.Operation {
		case rollout.ControlOperationAdmit, rollout.ControlOperationContinue:
			authority, authorityErr := q.AdvanceChannelFirmwareAuthorityRevision(
				ctx,
				sqlc.AdvanceChannelFirmwareAuthorityRevisionParams{
					AuthorityID:      current.ForwardAuthorityID,
					OrgID:            req.OrgID,
					ExpectedRevision: current.ForwardAuthorityRevision,
				},
			)
			if errors.Is(authorityErr, sql.ErrNoRows) {
				return rollout.ControlResult{}, rollout.ErrRevisionConflict
			}
			if authorityErr != nil {
				return rollout.ControlResult{}, authorityErr
			}
			forwardRevision = sql.NullInt64{Int64: authority.Revision, Valid: true}
		case rollout.ControlOperationAbort:
			authority, authorityErr := q.HaltChannelFirmwareAuthority(
				ctx,
				sqlc.HaltChannelFirmwareAuthorityParams{
					AuthorityID:      current.ForwardAuthorityID,
					OrgID:            req.OrgID,
					ExpectedRevision: current.ForwardAuthorityRevision,
				},
			)
			if errors.Is(authorityErr, sql.ErrNoRows) {
				return rollout.ControlResult{}, rollout.ErrRevisionConflict
			}
			if authorityErr != nil {
				return rollout.ControlResult{}, authorityErr
			}
			forwardRevision = sql.NullInt64{Int64: authority.Revision, Valid: true}
		case rollout.ControlOperationRevert:
			authorityID := uuid.New()
			authority, authorityErr := q.CreateChannelFirmwareAuthority(
				ctx,
				sqlc.CreateChannelFirmwareAuthorityParams{
					ID:                 authorityID,
					OrgID:              req.OrgID,
					AuthorityType:      "rollout_revert",
					AuthorityReference: req.RolloutID.String(),
					CreatedByUserID:    req.ActorUserID,
				},
			)
			if authorityErr != nil {
				return rollout.ControlResult{}, authorityErr
			}
			revertAuthorityID = uuid.NullUUID{UUID: authority.ID, Valid: true}
			revertAuthorityRevision = sql.NullInt64{
				Int64: authority.Revision,
				Valid: true,
			}
		case rollout.ControlOperationComplete:
			if rollout.State(current.State) == rollout.StateReverting {
				if !current.RevertAuthorityID.Valid ||
					!current.RevertAuthorityRevision.Valid {
					return rollout.ControlResult{}, fmt.Errorf(
						"%w: reverting rollout has no revert authority",
						rollout.ErrInvalidTransition,
					)
				}
				authority, authorityErr := q.HaltChannelFirmwareAuthority(
					ctx,
					sqlc.HaltChannelFirmwareAuthorityParams{
						AuthorityID:      current.RevertAuthorityID.UUID,
						OrgID:            req.OrgID,
						ExpectedRevision: current.RevertAuthorityRevision.Int64,
					},
				)
				if errors.Is(authorityErr, sql.ErrNoRows) {
					return rollout.ControlResult{}, rollout.ErrRevisionConflict
				}
				if authorityErr != nil {
					return rollout.ControlResult{}, authorityErr
				}
				revertAuthorityRevision = sql.NullInt64{
					Int64: authority.Revision,
					Valid: true,
				}
			} else {
				authority, authorityErr := q.HaltChannelFirmwareAuthority(
					ctx,
					sqlc.HaltChannelFirmwareAuthorityParams{
						AuthorityID:      current.ForwardAuthorityID,
						OrgID:            req.OrgID,
						ExpectedRevision: current.ForwardAuthorityRevision,
					},
				)
				if errors.Is(authorityErr, sql.ErrNoRows) {
					return rollout.ControlResult{}, rollout.ErrRevisionConflict
				}
				if authorityErr != nil {
					return rollout.ControlResult{}, authorityErr
				}
				forwardRevision = sql.NullInt64{Int64: authority.Revision, Valid: true}
			}
		case rollout.ControlOperationCreate,
			rollout.ControlOperationPause,
			rollout.ControlOperationResume:
			// These operations do not change channel firmware authority.
		}

		nextResumeState := sql.NullString{}
		if req.Operation == rollout.ControlOperationPause {
			nextResumeState = sql.NullString{String: current.State, Valid: true}
		}
		updated, updateErr := q.ApplyFirmwareRolloutTransition(
			ctx,
			sqlc.ApplyFirmwareRolloutTransitionParams{
				TargetState:              string(targetState),
				ResumeState:              nextResumeState,
				ForwardAuthorityRevision: forwardRevision,
				RevertAuthorityID:        revertAuthorityID,
				RevertAuthorityRevision:  revertAuthorityRevision,
				RolloutID:                req.RolloutID,
				OrgID:                    req.OrgID,
				ExpectedRevision:         req.ExpectedRevision,
			},
		)
		if errors.Is(updateErr, sql.ErrNoRows) {
			return rollout.ControlResult{}, rollout.ErrRevisionConflict
		}
		if updateErr != nil {
			return rollout.ControlResult{}, updateErr
		}

		if selectedBatchID.Valid {
			_, updateErr = q.AdmitFirmwareRolloutBatch(
				ctx,
				sqlc.AdmitFirmwareRolloutBatchParams{
					BatchID:   selectedBatchID.Int64,
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			)
			if updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.AdmitFirmwareRolloutMembers(
				ctx,
				sqlc.AdmitFirmwareRolloutMembersParams{
					BatchID:   selectedBatchID.Int64,
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
		}
		if targetState == rollout.StateAborted {
			if _, updateErr = q.CancelPendingFirmwareRolloutBatches(
				ctx,
				sqlc.CancelPendingFirmwareRolloutBatchesParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.CancelPendingFirmwareRolloutMembers(
				ctx,
				sqlc.CancelPendingFirmwareRolloutMembersParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.CancelUnclaimedFirmwareRolloutMembers(
				ctx,
				sqlc.CancelUnclaimedFirmwareRolloutMembersParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.ReleaseTerminalFirmwareRolloutOwners(
				ctx,
				sqlc.ReleaseTerminalFirmwareRolloutOwnersParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.CompleteSettledBetweenChannelBatches(
				ctx,
				sqlc.CompleteSettledBetweenChannelBatchesParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
		}
		if targetState == rollout.StateReverting {
			if _, updateErr = q.PrepareFirmwareRolloutMembersForRevert(
				ctx,
				sqlc.PrepareFirmwareRolloutMembersForRevertParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
		}
		if targetState == rollout.StateCompleted ||
			targetState == rollout.StateCompletedWithFailures {
			if _, updateErr = q.CancelPendingFirmwareRolloutBatches(
				ctx,
				sqlc.CancelPendingFirmwareRolloutBatchesParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.CancelPendingFirmwareRolloutMembers(
				ctx,
				sqlc.CancelPendingFirmwareRolloutMembersParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.CompleteFirmwareRolloutBatches(
				ctx,
				sqlc.CompleteFirmwareRolloutBatchesParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
			if _, updateErr = q.ReleaseFirmwareRolloutOwners(
				ctx,
				sqlc.ReleaseFirmwareRolloutOwnersParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
		}
		if targetState == rollout.StateReverted {
			if _, updateErr = q.CompleteFirmwareRolloutRevertMembers(
				ctx,
				sqlc.CompleteFirmwareRolloutRevertMembersParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); updateErr != nil {
				return rollout.ControlResult{}, updateErr
			}
		}

		status := rollout.ControlStatusSucceeded
		if req.Operation == rollout.ControlOperationAdmit ||
			req.Operation == rollout.ControlOperationContinue ||
			req.Operation == rollout.ControlOperationRevert {
			status = rollout.ControlStatusStarted
		}
		control, controlErr := q.CreateFirmwareRolloutControl(
			ctx,
			sqlc.CreateFirmwareRolloutControlParams{
				ControlID:          uuid.New(),
				RolloutID:          req.RolloutID,
				OrgID:              req.OrgID,
				BatchID:            selectedBatchID,
				Operation:          string(req.Operation),
				IdempotencyKey:     req.IdempotencyKey,
				RequestFingerprint: req.RequestFingerprint,
				ExpectedRevision:   req.ExpectedRevision,
				ResultingRevision:  updated.Revision,
				Status:             string(status),
				CreatedByUserID:    req.ActorUserID,
				ActorType:          persistedActorType(req.ActorType),
				ActorCredentialID:  ptrToNullString(req.ActorCredentialID),
			},
		)
		if controlErr != nil {
			return rollout.ControlResult{}, controlErr
		}
		if _, causeErr := q.CreateFirmwareRolloutCause(
			ctx,
			sqlc.CreateFirmwareRolloutCauseParams{
				RolloutID:         req.RolloutID,
				ControlID:         uuid.NullUUID{UUID: control.ID, Valid: true},
				OrgID:             req.OrgID,
				Operation:         string(req.Operation),
				Reason:            req.Reason,
				ActorUserID:       req.ActorUserID,
				ActorType:         persistedActorType(req.ActorType),
				ActorCredentialID: ptrToNullString(req.ActorCredentialID),
				FromState:         sql.NullString{String: current.State, Valid: true},
				ToState:           updated.State,
				RolloutRevision:   updated.Revision,
			},
		); causeErr != nil {
			return rollout.ControlResult{}, causeErr
		}
		loaded, loadErr := loadRollout(ctx, q, updated)
		if loadErr != nil {
			return rollout.ControlResult{}, loadErr
		}
		return rollout.ControlResult{
			Rollout: loaded,
			Batch:   findBatch(loaded.Batches, selectedBatchID),
			Control: controlFromSQL(control),
		}, nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return rollout.ControlResult{}, rollout.ErrNotFound
	}
	if isUniqueViolationOn(err, "uq_firmware_rollout_active_owner") {
		return rollout.ControlResult{}, rollout.ErrOwnershipConflict
	}
	if isUniqueViolationOn(err, "uq_firmware_rollout_control_rollout_key") {
		return s.replayControl(ctx, req)
	}
	if err != nil {
		return rollout.ControlResult{}, fmt.Errorf("apply firmware rollout control: %w", err)
	}
	return result, nil
}

func (s *SQLRolloutStore) FinishControl(
	ctx context.Context,
	req rollout.FinishControlRequest,
) (*rollout.Rollout, error) {
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*rollout.Rollout, error) {
		control, getErr := q.GetFirmwareRolloutControl(
			ctx,
			sqlc.GetFirmwareRolloutControlParams{
				ControlID: req.ControlID,
				RolloutID: req.RolloutID,
				OrgID:     req.OrgID,
			},
		)
		if getErr != nil {
			return nil, getErr
		}
		shouldRestoreReview := control.Status == string(rollout.ControlStatusStarted) &&
			!req.Success &&
			(control.Operation == string(rollout.ControlOperationAdmit) ||
				control.Operation == string(rollout.ControlOperationContinue))
		if control.Status == string(rollout.ControlStatusStarted) {
			status := rollout.ControlStatusSucceeded
			if !req.Success {
				status = rollout.ControlStatusFailed
			}
			_, getErr = q.FinishFirmwareRolloutControl(
				ctx,
				sqlc.FinishFirmwareRolloutControlParams{
					Status:       string(status),
					ErrorMessage: sql.NullString{String: req.ErrorMessage, Valid: !req.Success},
					ControlID:    req.ControlID,
					RolloutID:    req.RolloutID,
					OrgID:        req.OrgID,
				},
			)
			if getErr != nil {
				return nil, getErr
			}
		}
		if shouldRestoreReview {
			if _, moveErr := q.MoveFirmwareRolloutToReviewAfterControlFailure(
				ctx,
				sqlc.MoveFirmwareRolloutToReviewAfterControlFailureParams{
					RolloutID: req.RolloutID,
					OrgID:     req.OrgID,
				},
			); moveErr != nil {
				return nil, moveErr
			}
		}
		row, getErr := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{
			RolloutID: req.RolloutID,
			OrgID:     req.OrgID,
		})
		if getErr != nil {
			return nil, getErr
		}
		return loadRollout(ctx, q, row)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rollout.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("finish firmware rollout control: %w", err)
	}
	return result, nil
}

func (s *SQLRolloutStore) UpdateMember(
	ctx context.Context,
	req rollout.MemberUpdateRequest,
) (rollout.Member, error) {
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FirmwareRolloutMember, error) {
		return q.UpdateFirmwareRolloutMember(ctx, sqlc.UpdateFirmwareRolloutMemberParams{
			State:            string(req.State),
			EnforcementID:    ptrToNullInt64(req.EnforcementID),
			CommandBatchUuid: ptrToNullString(req.CommandBatchUUID),
			LastError:        ptrToNullString(req.LastError),
			MemberID:         req.MemberID,
			RolloutID:        req.RolloutID,
			OrgID:            req.OrgID,
			ExpectedRevision: req.ExpectedRevision,
		})
	})
	if errors.Is(err, sql.ErrNoRows) {
		return rollout.Member{}, rollout.ErrRevisionConflict
	}
	if err != nil {
		return rollout.Member{}, fmt.Errorf("update firmware rollout member: %w", err)
	}
	return memberFromSQL(row, ""), nil
}

func (s *SQLRolloutStore) CaptureEvidence(
	ctx context.Context,
	req rollout.EvidenceRequest,
) ([]rollout.Evidence, error) {
	rows, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) ([]sqlc.FirmwareRolloutEvidence, error) {
		return q.CaptureFirmwareRolloutEvidence(
			ctx,
			sqlc.CaptureFirmwareRolloutEvidenceParams{
				Phase:       string(req.Phase),
				WindowStart: req.WindowStart,
				WindowEnd:   req.WindowEnd,
				FreshAfter:  req.FreshAfter,
				RolloutID:   req.RolloutID,
				OrgID:       req.OrgID,
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("capture firmware rollout evidence: %w", err)
	}
	result := make([]rollout.Evidence, 0, len(rows))
	for _, row := range rows {
		result = append(result, evidenceFromSQL(row))
	}
	return result, nil
}

func (s *SQLRolloutStore) replayControl(
	ctx context.Context,
	req rollout.ControlRequest,
) (rollout.ControlResult, error) {
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (rollout.ControlResult, error) {
		control, err := q.GetFirmwareRolloutControlByKey(
			ctx,
			sqlc.GetFirmwareRolloutControlByKeyParams{
				RolloutID:      req.RolloutID,
				OrgID:          req.OrgID,
				IdempotencyKey: req.IdempotencyKey,
			},
		)
		if err != nil {
			return rollout.ControlResult{}, err
		}
		if control.Operation != string(req.Operation) ||
			control.RequestFingerprint != req.RequestFingerprint {
			return rollout.ControlResult{}, rollout.ErrIdempotencyConflict
		}
		row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{
			RolloutID: req.RolloutID,
			OrgID:     req.OrgID,
		})
		if err != nil {
			return rollout.ControlResult{}, err
		}
		loaded, err := loadRollout(ctx, q, row)
		if err != nil {
			return rollout.ControlResult{}, err
		}
		return rollout.ControlResult{
			Rollout:  loaded,
			Batch:    findBatch(loaded.Batches, control.BatchID),
			Control:  controlFromSQL(control),
			Replayed: true,
		}, nil
	})
}

func (s *SQLRolloutStore) getByIdempotencyKey(
	ctx context.Context,
	orgID int64,
	key string,
) (sqlc.FirmwareRollout, error) {
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.FirmwareRollout, error) {
		return q.GetFirmwareRolloutByIdempotencyKey(
			ctx,
			sqlc.GetFirmwareRolloutByIdempotencyKeyParams{
				OrgID:          orgID,
				IdempotencyKey: key,
			},
		)
	})
}

func loadRollout(
	ctx context.Context,
	q sqlc.Querier,
	row sqlc.FirmwareRollout,
) (*rollout.Rollout, error) {
	result := rolloutFromSQL(row)
	batches, err := q.ListFirmwareRolloutBatches(
		ctx,
		sqlc.ListFirmwareRolloutBatchesParams{
			RolloutID: row.ID,
			OrgID:     row.OrgID,
		},
	)
	if err != nil {
		return nil, err
	}
	members, err := q.ListFirmwareRolloutMembers(
		ctx,
		sqlc.ListFirmwareRolloutMembersParams{
			RolloutID: row.ID,
			OrgID:     row.OrgID,
		},
	)
	if err != nil {
		return nil, err
	}
	evidence, err := q.ListFirmwareRolloutEvidence(
		ctx,
		sqlc.ListFirmwareRolloutEvidenceParams{
			RolloutID: row.ID,
			OrgID:     row.OrgID,
		},
	)
	if err != nil {
		return nil, err
	}
	causes, err := q.ListFirmwareRolloutCauses(
		ctx,
		sqlc.ListFirmwareRolloutCausesParams{
			RolloutID: row.ID,
			OrgID:     row.OrgID,
		},
	)
	if err != nil {
		return nil, err
	}

	evidenceByMember := make(map[int64][]rollout.Evidence)
	for _, evidenceRow := range evidence {
		evidenceByMember[evidenceRow.MemberID] = append(
			evidenceByMember[evidenceRow.MemberID],
			evidenceFromSQL(evidenceRow),
		)
	}
	membersByBatch := make(map[int64][]rollout.Member)
	result.Members = make([]rollout.Member, 0, len(members))
	for _, memberRow := range members {
		member := memberFromListRow(memberRow)
		member.Evidence = evidenceByMember[member.ID]
		result.Members = append(result.Members, member)
		membersByBatch[member.BatchID] = append(membersByBatch[member.BatchID], member)
	}
	result.Batches = make([]rollout.Batch, 0, len(batches))
	for _, batchRow := range batches {
		batch := batchFromSQL(batchRow)
		batch.Members = membersByBatch[batch.ID]
		result.Batches = append(result.Batches, batch)
	}
	result.Causes = make([]rollout.Cause, 0, len(causes))
	for _, causeRow := range causes {
		result.Causes = append(result.Causes, causeFromSQL(causeRow))
	}
	return &result, nil
}

func rolloutFromSQL(row sqlc.FirmwareRollout) rollout.Rollout {
	return rollout.Rollout{
		ID:                       row.ID,
		OrgID:                    row.OrgID,
		Name:                     row.Name,
		StrategyKey:              row.StrategyKey,
		State:                    rollout.State(row.State),
		ResumeState:              statePtr(row.ResumeState),
		Revision:                 row.Revision,
		ForwardAuthorityID:       row.ForwardAuthorityID,
		ForwardAuthorityRevision: row.ForwardAuthorityRevision,
		RevertAuthorityID:        uuidPtr(row.RevertAuthorityID),
		RevertAuthorityRevision:  nullInt64ToPtr(row.RevertAuthorityRevision),
		SourceChannelID:          nullInt64ToPtr(row.SourceChannelID),
		TargetChannelID:          nullInt64ToPtr(row.TargetChannelID),
		SourceReleaseSetID:       nullInt64ToPtr(row.SourceReleaseSetID),
		TargetReleaseSetID:       nullInt64ToPtr(row.TargetReleaseSetID),
		SourceSnapshot:           unmarshalSnapshot(row.SourceSnapshot),
		TargetSnapshot:           unmarshalSnapshot(row.TargetSnapshot),
		RevertSnapshot:           unmarshalSnapshot(row.RevertSnapshot),
		HashratePolicy:           hashratePolicyFromSQL(row),
		Reason:                   row.Reason,
		CreatedByUserID:          row.CreatedByUserID,
		StartedAt:                timePtr(row.StartedAt),
		PausedAt:                 timePtr(row.PausedAt),
		AbortedAt:                timePtr(row.AbortedAt),
		CompletedAt:              timePtr(row.CompletedAt),
		RevertingAt:              timePtr(row.RevertingAt),
		RevertedAt:               timePtr(row.RevertedAt),
		CreatedAt:                row.CreatedAt,
		UpdatedAt:                row.UpdatedAt,
	}
}

func batchFromSQL(row sqlc.FirmwareRolloutBatch) rollout.Batch {
	return rollout.Batch{
		ID:                                 row.ID,
		RolloutID:                          row.RolloutID,
		OrgID:                              row.OrgID,
		Position:                           row.Position,
		Label:                              row.Label,
		State:                              rollout.BatchState(row.State),
		Revision:                           row.Revision,
		CompletedAt:                        timePtr(row.CompletedAt),
		EvidenceStatus:                     rollout.EvidenceStatus(row.EvidenceStatus),
		EvidenceTotalCount:                 row.EvidenceTotalCount,
		EvidencePairedCount:                row.EvidencePairedCount,
		CumulativeBaselineHashrateHS:       float64Ptr(row.CumulativeBaselineHashrateHs),
		CumulativeCurrentHashrateHS:        float64Ptr(row.CumulativeCurrentHashrateHs),
		CumulativeDeltaBasisPoints:         nullInt32ToPtr(row.CumulativeDeltaBasisPoints),
		LatestPolicyBucketHashrateHS:       float64Ptr(row.LatestPolicyBucketHashrateHs),
		LatestPolicyBucketDeltaBasisPoints: nullInt32ToPtr(row.LatestPolicyBucketDeltaBasisPoints),
		HealthySince:                       timePtr(row.HealthySince),
		LastPolicyBucketBoundary:           timePtr(row.LastPolicyBucketBoundary),
		EvaluatedAt:                        timePtr(row.EvaluatedAt),
		EvidenceErrorMessage:               stringPtr(row.EvidenceErrorMessage),
		PostWindowFinalized:                row.PostWindowFinalized,
		PostWindowFinalizedAt:              timePtr(row.PostWindowFinalizedAt),
		CreatedAt:                          row.CreatedAt,
		UpdatedAt:                          row.UpdatedAt,
	}
}

func hashratePolicyMaxDrop(policy *rollout.HashratePolicy) *int32 {
	if policy == nil {
		return nil
	}
	return &policy.MaxDropBasisPoints
}

func hashratePolicyHealthyDuration(policy *rollout.HashratePolicy) *int32 {
	if policy == nil {
		return nil
	}
	return &policy.HealthyDurationSeconds
}

func hashratePolicyFromSQL(row sqlc.FirmwareRollout) *rollout.HashratePolicy {
	if !row.HashratePolicyMaxDropBasisPoints.Valid ||
		!row.HashratePolicyHealthyDurationSeconds.Valid {
		return nil
	}
	return &rollout.HashratePolicy{
		MaxDropBasisPoints:     row.HashratePolicyMaxDropBasisPoints.Int32,
		HealthyDurationSeconds: row.HashratePolicyHealthyDurationSeconds.Int32,
	}
}

func memberFromListRow(row sqlc.ListFirmwareRolloutMembersRow) rollout.Member {
	return rollout.Member{
		ID:               row.ID,
		RolloutID:        row.RolloutID,
		BatchID:          row.BatchID,
		OrgID:            row.OrgID,
		DeviceID:         row.DeviceID,
		DeviceIdentifier: row.DeviceIdentifier,
		Position:         row.Position,
		State:            rollout.MemberState(row.State),
		Revision:         row.Revision,
		SourceSnapshot:   unmarshalSnapshot(row.SourceSnapshot),
		TargetSnapshot:   unmarshalSnapshot(row.TargetSnapshot),
		RevertSnapshot:   unmarshalSnapshot(row.RevertSnapshot),
		EnforcementID:    nullInt64ToPtr(row.EnforcementID),
		CommandBatchUUID: stringPtr(row.CommandBatchUuid),
		LastError:        stringPtr(row.LastError),
		AdmittedAt:       timePtr(row.AdmittedAt),
		SettledAt:        timePtr(row.SettledAt),
		OwnerReleasedAt:  timePtr(row.OwnerReleasedAt),
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func memberFromSQL(row sqlc.FirmwareRolloutMember, deviceIdentifier string) rollout.Member {
	return rollout.Member{
		ID:               row.ID,
		RolloutID:        row.RolloutID,
		BatchID:          row.BatchID,
		OrgID:            row.OrgID,
		DeviceID:         row.DeviceID,
		DeviceIdentifier: deviceIdentifier,
		Position:         row.Position,
		State:            rollout.MemberState(row.State),
		Revision:         row.Revision,
		SourceSnapshot:   unmarshalSnapshot(row.SourceSnapshot),
		TargetSnapshot:   unmarshalSnapshot(row.TargetSnapshot),
		RevertSnapshot:   unmarshalSnapshot(row.RevertSnapshot),
		EnforcementID:    nullInt64ToPtr(row.EnforcementID),
		CommandBatchUUID: stringPtr(row.CommandBatchUuid),
		LastError:        stringPtr(row.LastError),
		AdmittedAt:       timePtr(row.AdmittedAt),
		SettledAt:        timePtr(row.SettledAt),
		OwnerReleasedAt:  timePtr(row.OwnerReleasedAt),
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func evidenceFromSQL(row sqlc.FirmwareRolloutEvidence) rollout.Evidence {
	return rollout.Evidence{
		ID:              row.ID,
		RolloutID:       row.RolloutID,
		MemberID:        row.MemberID,
		OrgID:           row.OrgID,
		Phase:           rollout.EvidencePhase(row.Phase),
		WindowStart:     row.WindowStart,
		WindowEnd:       row.WindowEnd,
		ObservedAt:      timePtr(row.ObservedAt),
		AvgHashrateHS:   float64Ptr(row.AvgHashrateHs),
		AvgPowerW:       float64Ptr(row.AvgPowerW),
		AvgTemperatureC: float64Ptr(row.AvgTemperatureC),
		ErrorCount:      nullInt64ToPtr(row.ErrorCount),
		SampleCount:     nullInt64ToPtr(row.SampleCount),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func causeFromSQL(row sqlc.FirmwareRolloutCause) rollout.Cause {
	return rollout.Cause{
		ID:                row.ID,
		RolloutID:         row.RolloutID,
		MemberID:          nullInt64ToPtr(row.MemberID),
		ControlID:         uuidPtr(row.ControlID),
		OrgID:             row.OrgID,
		Operation:         rollout.ControlOperation(row.Operation),
		Reason:            row.Reason,
		ActorUserID:       row.ActorUserID,
		ActorType:         rollout.ActorType(row.ActorType),
		ActorCredentialID: stringPtr(row.ActorCredentialID),
		FromState:         statePtr(row.FromState),
		ToState:           rollout.State(row.ToState),
		RolloutRevision:   row.RolloutRevision,
		CreatedAt:         row.CreatedAt,
	}
}

func controlFromSQL(row sqlc.FirmwareRolloutControl) rollout.Control {
	return rollout.Control{
		ID:                 row.ID,
		RolloutID:          row.RolloutID,
		OrgID:              row.OrgID,
		BatchID:            nullInt64ToPtr(row.BatchID),
		Operation:          rollout.ControlOperation(row.Operation),
		IdempotencyKey:     row.IdempotencyKey,
		RequestFingerprint: row.RequestFingerprint,
		ExpectedRevision:   row.ExpectedRevision,
		ResultingRevision:  row.ResultingRevision,
		Status:             rollout.ControlStatus(row.Status),
		ErrorMessage:       stringPtr(row.ErrorMessage),
		CreatedByUserID:    row.CreatedByUserID,
		ActorType:          rollout.ActorType(row.ActorType),
		ActorCredentialID:  stringPtr(row.ActorCredentialID),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func findBatch(batches []rollout.Batch, batchID sql.NullInt64) *rollout.Batch {
	if !batchID.Valid {
		return nil
	}
	for index := range batches {
		if batches[index].ID == batchID.Int64 {
			return &batches[index]
		}
	}
	return nil
}

func persistedActorType(actorType rollout.ActorType) string {
	if actorType == "" {
		return string(rollout.ActorTypeUser)
	}
	return string(actorType)
}

func marshalSnapshot(value map[string]any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`{}`)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func nonNilSnapshot(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func unmarshalSnapshot(value json.RawMessage) map[string]any {
	result := make(map[string]any)
	if len(value) == 0 {
		return result
	}
	if err := json.Unmarshal(value, &result); err != nil {
		return map[string]any{}
	}
	return result
}

func uuidPtr(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func statePtr(value sql.NullString) *rollout.State {
	if !value.Valid {
		return nil
	}
	result := rollout.State(value.String)
	return &result
}
