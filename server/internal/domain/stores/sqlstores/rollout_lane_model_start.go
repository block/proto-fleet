package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
)

func (s *SQLRolloutLaneStore) startModelRollout(
	ctx context.Context,
	req betweenchannel.StartRolloutRequest,
) (betweenchannel.StartRolloutResult, error) {
	value, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		q := s.GetQueries(txCtx)
		lane, lockErr := q.LockRolloutLane(txCtx, sqlc.LockRolloutLaneParams{
			LaneID: req.LaneID,
			OrgID:  req.OrgID,
		})
		if errors.Is(lockErr, sql.ErrNoRows) {
			return nil, betweenchannel.ErrLaneNotFound
		}
		if lockErr != nil {
			return nil, lockErr
		}

		existing, getErr := q.GetFirmwareRolloutGroupByStartKey(
			txCtx,
			sqlc.GetFirmwareRolloutGroupByStartKeyParams{
				LaneID:         req.LaneID,
				OrgID:          req.OrgID,
				IdempotencyKey: req.IdempotencyKey,
			},
		)
		if getErr == nil {
			if existing.CreateFingerprint != req.RequestFingerprint {
				return nil, betweenchannel.ErrIdempotencyConflict
			}
			return loadModelStartResult(txCtx, q, lane, existing)
		}
		if !errors.Is(getErr, sql.ErrNoRows) {
			return nil, getErr
		}

		cutover, cutoverErr := q.LockRolloutLaneTopologyCutover(txCtx, req.OrgID)
		if cutoverErr != nil {
			return nil, cutoverErr
		}
		if !cutover.Enabled {
			return nil, betweenchannel.ErrTopologyNotReady
		}
		if req.LegacyCompatibility {
			models, modelsErr := q.ListRolloutLaneModels(
				txCtx,
				sqlc.ListRolloutLaneModelsParams{LaneID: req.LaneID, OrgID: req.OrgID},
			)
			if modelsErr != nil {
				return nil, modelsErr
			}
			if len(req.ModelPlans) != 1 || len(models) != 1 ||
				models[0].ID != req.ModelPlans[0].LaneModelID {
				return nil, betweenchannel.ErrScalarProjectionUnavailable
			}
		}
		if _, releaseErr := releaseRolloutLaneActiveParentIfSettled(
			txCtx,
			q,
			req.LaneID,
			req.OrgID,
		); releaseErr != nil {
			return nil, releaseErr
		}
		if _, claimErr := q.GetRolloutLaneActiveParent(
			txCtx,
			sqlc.GetRolloutLaneActiveParentParams{LaneID: req.LaneID, OrgID: req.OrgID},
		); claimErr == nil {
			return nil, betweenchannel.ErrLaneWorkActive
		} else if !errors.Is(claimErr, sql.ErrNoRows) {
			return nil, claimErr
		}

		plans := append([]betweenchannel.StartRolloutModelPlan(nil), req.ModelPlans...)
		betweenchannel.SortStartRolloutModelPlans(plans)
		declarationIDs := make([]uuid.UUID, 0, len(plans))
		seenDeclarations := make(map[uuid.UUID]struct{}, len(plans))
		seenModelKeys := make(map[string]struct{}, len(plans))
		seenStartKeys := make(map[string]struct{}, len(plans))
		for _, plan := range plans {
			modelKey := betweenchannel.CanonicalModelIdentityKey(
				plan.ReleaseTarget.Manufacturer,
				plan.ReleaseTarget.Model,
			)
			if _, duplicate := seenDeclarations[plan.LaneModelID]; duplicate {
				return nil, betweenchannel.ErrDeclarationConflict
			}
			if _, duplicate := seenModelKeys[modelKey]; duplicate {
				return nil, betweenchannel.ErrDeclarationConflict
			}
			if _, duplicate := seenStartKeys[plan.ModelStartKey]; duplicate {
				return nil, betweenchannel.ErrIdempotencyConflict
			}
			seenDeclarations[plan.LaneModelID] = struct{}{}
			seenModelKeys[modelKey] = struct{}{}
			seenStartKeys[plan.ModelStartKey] = struct{}{}
			declarationIDs = append(declarationIDs, plan.LaneModelID)
		}
		declarations, declarationErr := q.LockRolloutLaneModelsForMutation(
			txCtx,
			sqlc.LockRolloutLaneModelsForMutationParams{
				OrgID:        req.OrgID,
				LaneModelIds: declarationIDs,
			},
		)
		if declarationErr != nil {
			return nil, declarationErr
		}
		if len(declarations) != len(plans) {
			return nil, betweenchannel.ErrLaneNotFound
		}
		declarationByID := make(map[uuid.UUID]sqlc.RolloutLaneModel, len(declarations))
		for _, declaration := range declarations {
			if declaration.LaneID != req.LaneID {
				return nil, betweenchannel.ErrLaneNotFound
			}
			declarationByID[declaration.ID] = declaration
		}

		prepared := make([]preparedModelStart, 0, len(plans))
		channelIDs := make([]int64, 0, len(plans))
		seenChannelIDs := make(map[int64]struct{}, len(plans))
		allDeviceIDs := make([]int64, 0)
		seenDeviceIDs := make(map[int64]struct{})
		for _, plan := range plans {
			declaration := declarationByID[plan.LaneModelID]
			if declaration.Revision != plan.ExpectedModelRevision {
				return nil, betweenchannel.ErrDeclarationConflict
			}
			activeModelWork, activeErr := q.HasActiveRolloutLaneModelWork(
				txCtx,
				sqlc.HasActiveRolloutLaneModelWorkParams{
					LaneModelID: declaration.ID,
					LaneID:      req.LaneID,
					OrgID:       req.OrgID,
				},
			)
			if activeErr != nil {
				return nil, activeErr
			}
			if activeModelWork.Valid && activeModelWork.Bool {
				return nil, betweenchannel.ErrModelWorkActive
			}
			targetIdentity := betweenchannel.CanonicalModelIdentityKey(
				plan.ReleaseTarget.Manufacturer,
				plan.ReleaseTarget.Model,
			)
			if targetIdentity == "" || targetIdentity != declaration.ModelIdentityKey {
				return nil, betweenchannel.ErrCompatibility
			}
			transitions, transitionErr := modelStartTransitions(txCtx, q, req, declaration)
			if transitionErr != nil {
				return nil, transitionErr
			}
			if len(transitions) == 0 {
				return nil, betweenchannel.ErrLaneEmpty
			}
			if populationErr := validateFrozenPopulation(transitions, plan.Batches); populationErr != nil {
				return nil, populationErr
			}
			if compatibilityErr := betweenchannel.ValidateTransitionTargetsForStore(
				transitions,
				[]betweenchannel.ReleaseTarget{plan.ReleaseTarget},
			); compatibilityErr != nil {
				return nil, compatibilityErr
			}
			for _, transition := range transitions {
				if _, duplicate := seenDeviceIDs[transition.DeviceID]; duplicate {
					return nil, betweenchannel.ErrMembershipConflict
				}
				seenDeviceIDs[transition.DeviceID] = struct{}{}
				allDeviceIDs = append(allDeviceIDs, transition.DeviceID)
			}
			if _, seen := seenChannelIDs[declaration.CurrentChannelID]; !seen {
				seenChannelIDs[declaration.CurrentChannelID] = struct{}{}
				channelIDs = append(channelIDs, declaration.CurrentChannelID)
			}
			prepared = append(prepared, preparedModelStart{
				plan: plan, declaration: declaration, transitions: transitions,
			})
		}
		sort.Slice(channelIDs, func(i, j int) bool { return channelIDs[i] < channelIDs[j] })
		sort.Slice(allDeviceIDs, func(i, j int) bool { return allDeviceIDs[i] < allDeviceIDs[j] })
		lockedChannels, lockErr := q.LockBetweenChannelChannels(
			txCtx,
			sqlc.LockBetweenChannelChannelsParams{OrgID: req.OrgID, ChannelIds: channelIDs},
		)
		if lockErr != nil {
			return nil, lockErr
		}
		if len(lockedChannels) != len(channelIDs) {
			return nil, betweenchannel.ErrLaneConflict
		}
		lockedDevices, lockErr := q.LockBetweenChannelDevices(
			txCtx,
			sqlc.LockBetweenChannelDevicesParams{OrgID: req.OrgID, DeviceIds: allDeviceIDs},
		)
		if lockErr != nil {
			return nil, lockErr
		}
		if len(lockedDevices) != len(allDeviceIDs) {
			return nil, betweenchannel.ErrMembershipConflict
		}
		for index := range prepared {
			transitions, transitionErr := modelStartTransitions(
				txCtx,
				q,
				req,
				prepared[index].declaration,
			)
			if transitionErr != nil {
				return nil, transitionErr
			}
			if populationErr := validateFrozenPopulation(
				transitions,
				prepared[index].plan.Batches,
			); populationErr != nil {
				return nil, populationErr
			}
			prepared[index].transitions = transitions
		}

		parent, createErr := q.CreateFirmwareRolloutGroup(
			txCtx,
			sqlc.CreateFirmwareRolloutGroupParams{
				GroupID:           req.ParentID,
				LaneID:            req.LaneID,
				OrgID:             req.OrgID,
				Name:              req.Name,
				IdempotencyKey:    req.IdempotencyKey,
				CreateFingerprint: req.RequestFingerprint,
				Reason:            req.Reason,
				CreatedByUserID:   req.ActorUserID,
				ActorType:         persistedActorType(req.ActorType),
				ActorCredentialID: ptrToNullString(req.ActorCredentialID),
			},
		)
		if createErr != nil {
			return nil, createErr
		}
		if _, createErr = q.ClaimRolloutLaneActiveParent(
			txCtx,
			sqlc.ClaimRolloutLaneActiveParentParams{
				LaneID:              req.LaneID,
				OrgID:               req.OrgID,
				GroupID:             parent.ID,
				ClaimIdempotencyKey: req.IdempotencyKey,
				ClaimFingerprint:    req.RequestFingerprint,
			},
		); createErr != nil {
			return nil, createErr
		}

		for index, item := range prepared {
			plan := item.plan
			declaration := item.declaration
			position, createErr := q.GetNextRolloutLaneChannelPosition(
				txCtx,
				sqlc.GetNextRolloutLaneChannelPositionParams{LaneID: req.LaneID, OrgID: req.OrgID},
			)
			if createErr != nil {
				return nil, createErr
			}
			targetReleaseSetID, targetReleaseTargetID, targetChannelID, createErr := createSingletonModelChannel(
				txCtx,
				q,
				req.OrgID,
				req.LaneID,
				position,
				plan.ReleaseTarget,
			)
			if createErr != nil {
				return nil, createErr
			}
			childReq := req
			if index > 0 {
				childReq.ID = uuid.New()
			}
			childReq.ModelPlans = []betweenchannel.StartRolloutModelPlan{plan}
			validationBoundary := time.Now().UTC()
			childRow, createErr := createBetweenChannelRollout(
				txCtx,
				q,
				betweenChannelRolloutCreate{
					Request:            childReq,
					SourceChannelID:    declaration.CurrentChannelID,
					TargetChannelID:    targetChannelID,
					Transitions:        item.transitions,
					TargetReleaseSetID: targetReleaseSetID,
					ModelContext: &modelRolloutStartContext{
						GroupID:                  parent.ID,
						LaneModelID:              declaration.ID,
						ModelIdentityKey:         declaration.ModelIdentityKey,
						ModelIdentityValidatedAt: validationBoundary,
						SourceReleaseTargetID:    declaration.CurrentReleaseTargetID,
						TargetReleaseTargetID:    targetReleaseTargetID,
						Manufacturer:             declaration.Manufacturer,
						Model:                    declaration.Model,
					},
				},
			)
			if createErr != nil {
				return nil, createErr
			}
			if _, createErr = q.CreateRolloutLaneChannel(
				txCtx,
				sqlc.CreateRolloutLaneChannelParams{
					LaneID:              req.LaneID,
					OrgID:               req.OrgID,
					ChannelID:           targetChannelID,
					Position:            position,
					RolloutID:           uuid.NullUUID{UUID: childRow.ID, Valid: true},
					StartIdempotencyKey: sql.NullString{String: plan.ModelStartKey, Valid: true},
					StartFingerprint:    sql.NullString{String: req.RequestFingerprint, Valid: true},
				},
			); createErr != nil {
				return nil, createErr
			}
			if _, createErr = q.CreateFirmwareRolloutGroupModel(
				txCtx,
				sqlc.CreateFirmwareRolloutGroupModelParams{
					GroupID:               parent.ID,
					LaneID:                req.LaneID,
					LaneModelID:           declaration.ID,
					OrgID:                 req.OrgID,
					ModelIdentityKey:      declaration.ModelIdentityKey,
					SourceChannelID:       declaration.CurrentChannelID,
					SourceReleaseSetID:    declaration.CurrentReleaseSetID,
					SourceReleaseTargetID: declaration.CurrentReleaseTargetID,
					TargetChannelID:       targetChannelID,
					TargetReleaseSetID:    targetReleaseSetID,
					TargetReleaseTargetID: targetReleaseTargetID,
					Snapshot: marshalSnapshot(map[string]any{
						"manufacturer": declaration.Manufacturer,
						"model":        declaration.Model,
						"member_count": len(item.transitions),
					}),
				},
			); createErr != nil {
				return nil, createErr
			}
			if _, createErr = q.AttachFirmwareRolloutGroupModelChild(
				txCtx,
				sqlc.AttachFirmwareRolloutGroupModelChildParams{
					ChildRolloutID: uuid.NullUUID{UUID: childRow.ID, Valid: true},
					GroupID:        parent.ID,
					LaneModelID:    declaration.ID,
					OrgID:          req.OrgID,
				},
			); createErr != nil {
				return nil, createErr
			}
			if _, createErr = q.CreateRolloutLaneModelChannel(
				txCtx,
				sqlc.CreateRolloutLaneModelChannelParams{
					LaneModelID:     declaration.ID,
					LaneID:          req.LaneID,
					OrgID:           req.OrgID,
					ChannelID:       targetChannelID,
					ReleaseSetID:    targetReleaseSetID,
					ReleaseTargetID: targetReleaseTargetID,
				},
			); createErr != nil {
				return nil, createErr
			}
		}
		return loadModelStartResult(txCtx, q, lane, parent)
	})
	if err != nil {
		if isUniqueViolationOn(err, "uq_rollout_lane_active_parent") ||
			isUniqueViolationOn(err, "rollout_lane_active_parent_pkey") {
			return betweenchannel.StartRolloutResult{}, betweenchannel.ErrLaneWorkActive
		}
		if isUniqueViolationOn(err, "uq_firmware_rollout_group_idempotency") {
			return betweenchannel.StartRolloutResult{}, betweenchannel.ErrIdempotencyConflict
		}
		return betweenchannel.StartRolloutResult{}, fmt.Errorf("start model rollout lane: %w", err)
	}
	result, ok := value.(*betweenchannel.StartRolloutResult)
	if !ok {
		return betweenchannel.StartRolloutResult{}, fmt.Errorf(
			"start model rollout lane: unexpected result %T",
			value,
		)
	}
	return *result, nil
}

type preparedModelStart struct {
	plan        betweenchannel.StartRolloutModelPlan
	declaration sqlc.RolloutLaneModel
	transitions []betweenchannel.DeviceTransition
}

func modelStartTransitions(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.StartRolloutRequest,
	declaration sqlc.RolloutLaneModel,
) ([]betweenchannel.DeviceTransition, error) {
	rows, err := q.ListRolloutLaneModelTransitions(
		ctx,
		sqlc.ListRolloutLaneModelTransitionsParams{
			LaneModelID: declaration.ID,
			LaneID:      req.LaneID,
			OrgID:       req.OrgID,
		},
	)
	if err != nil {
		return nil, err
	}
	transitions := make([]betweenchannel.DeviceTransition, 0, len(rows))
	for _, row := range rows {
		if row.ModelIdentityKey != declaration.ModelIdentityKey ||
			!row.ModelIdentityObservedAt.Valid {
			return nil, betweenchannel.ErrMembershipConflict
		}
		transitions = append(transitions, betweenchannel.DeviceTransition{
			DeviceID:                row.DeviceID,
			DeviceIdentifier:        row.DeviceIdentifier,
			Manufacturer:            row.Manufacturer,
			Model:                   row.Model,
			SourceReleaseTargetID:   row.SourceReleaseTargetID,
			SourceFirmwareFileID:    row.SourceFirmwareFileID,
			SourceFirmwareVersion:   row.SourceFirmwareVersion,
			SourceSHA256:            row.SourceSha256,
			ModelIdentityKey:        row.ModelIdentityKey,
			ModelIdentityObservedAt: &row.ModelIdentityObservedAt.Time,
		})
	}
	return transitions, nil
}

func loadModelStartResult(
	ctx context.Context,
	q sqlc.Querier,
	laneRow sqlc.RolloutLane,
	parentRow sqlc.FirmwareRolloutGroup,
) (*betweenchannel.StartRolloutResult, error) {
	lane, err := loadRolloutLane(ctx, q, laneRow, false, nil)
	if err != nil {
		return nil, err
	}
	parent, err := loadRolloutGroup(ctx, q, parentRow)
	if err != nil {
		return nil, err
	}
	if len(parent.Children) == 0 {
		return nil, betweenchannel.ErrLaneConflict
	}
	started := make([]betweenchannel.StartedRolloutModel, 0, len(parent.Children))
	for index := range parent.Children {
		child := &parent.Children[index]
		if len(child.Batches) == 0 {
			return nil, betweenchannel.ErrLaneConflict
		}
		started = append(started, betweenchannel.StartedRolloutModel{
			Child: child, FirstBatchID: child.Batches[0].ID,
		})
	}
	return &betweenchannel.StartRolloutResult{
		Lane: lane, Parent: parent, Children: started,
	}, nil
}
