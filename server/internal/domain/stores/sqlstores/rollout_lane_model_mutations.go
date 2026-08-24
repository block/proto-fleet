package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	rolloutdomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
)

type modelOperation string

const (
	modelOperationCreateDeclaration modelOperation = "create_declaration"
	modelOperationPublishTarget     modelOperation = "publish_target"
	modelOperationUpdateMembership  modelOperation = "update_membership"
)

func (s *SQLRolloutLaneStore) CreateModelDeclaration(
	ctx context.Context,
	req betweenchannel.CreateModelDeclarationRequest,
) (*betweenchannel.Lane, error) {
	value, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		q := s.GetQueries(txCtx)
		if replay, replayErr := replayModelOperation(txCtx, q, req.OrgID, req.LaneID, req.IdempotencyKey, req.RequestFingerprint, modelOperationCreateDeclaration); replayErr == nil {
			return replay, nil
		} else if !errors.Is(replayErr, sql.ErrNoRows) {
			return nil, replayErr
		}
		if err := requireModelTopology(txCtx, q, req.OrgID); err != nil {
			return nil, err
		}
		laneRow, err := q.GetRolloutLane(txCtx, sqlc.GetRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, betweenchannel.ErrLaneNotFound
		}
		if err != nil {
			return nil, err
		}
		target := req.ReleaseTargets[0]
		modelKey := betweenchannel.CanonicalModelIdentityKey(target.Manufacturer, target.Model)
		_, err = q.LockRolloutLaneModelForMutation(txCtx, modelSelectorParams(req.LaneID, req.OrgID, uuid.Nil, modelKey))
		if err == nil {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		candidates, preview, err := lockAndPreviewDeclarationMembers(txCtx, q, req, target)
		if err != nil {
			return nil, err
		}
		if err = validateDeclarationConfirmations(req, preview); err != nil {
			return nil, err
		}

		sourceBindings, err := validatedSourceBindings(txCtx, q, req.OrgID, candidates)
		if err != nil {
			return nil, err
		}
		sourceModelIDs := distinctSourceModelIDs(sourceBindings, uuid.Nil)
		if err = lockAndCheckModelWork(txCtx, q, req.OrgID, sourceModelIDs); err != nil {
			return nil, err
		}
		deviceIDs := candidateDeviceIDs(candidates)
		if len(deviceIDs) > 0 {
			locked, lockErr := q.LockBetweenChannelDevices(txCtx, sqlc.LockBetweenChannelDevicesParams{
				OrgID: req.OrgID, DeviceIds: deviceIDs,
			})
			if lockErr != nil || len(locked) != len(deviceIDs) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			revalidatedCandidates, revalidatedPreview, revalidateErr := lockAndPreviewDeclarationMembers(
				txCtx,
				q,
				req,
				target,
			)
			if revalidateErr != nil {
				return nil, revalidateErr
			}
			revalidatedBindings, revalidateErr := validatedSourceBindings(
				txCtx,
				q,
				req.OrgID,
				revalidatedCandidates,
			)
			if revalidateErr != nil ||
				!slices.Equal(sourceModelIDs, distinctSourceModelIDs(revalidatedBindings, uuid.Nil)) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			candidates = revalidatedCandidates
			preview = revalidatedPreview
			sourceBindings = revalidatedBindings
			if err = validateDeclarationConfirmations(req, preview); err != nil {
				return nil, err
			}
		}

		position, err := q.GetNextRolloutLaneChannelPosition(
			txCtx,
			sqlc.GetNextRolloutLaneChannelPositionParams{LaneID: req.LaneID, OrgID: req.OrgID},
		)
		if err != nil {
			return nil, err
		}
		releaseSetID, releaseTargetID, channelID, err := createSingletonModelChannel(
			txCtx, q, req.OrgID, req.LaneID, position, target,
		)
		if err != nil {
			return nil, err
		}
		if _, err = q.CreateRolloutLaneChannel(txCtx, sqlc.CreateRolloutLaneChannelParams{
			LaneID: req.LaneID, OrgID: req.OrgID, ChannelID: channelID, Position: position,
		}); err != nil {
			return nil, err
		}
		declaration, err := q.CreateRolloutLaneModelDeclaration(txCtx, sqlc.CreateRolloutLaneModelDeclarationParams{
			LaneModelID: req.LaneModelID, LaneID: req.LaneID, OrgID: req.OrgID,
			ModelIdentityKey: modelKey, Manufacturer: target.Manufacturer, Model: target.Model,
			CurrentChannelID: channelID, CurrentReleaseSetID: releaseSetID, CurrentReleaseTargetID: releaseTargetID,
		})
		if err != nil {
			return nil, err
		}
		if _, err = q.CreateRolloutLaneModelChannel(txCtx, sqlc.CreateRolloutLaneModelChannelParams{
			LaneModelID: req.LaneModelID, LaneID: req.LaneID, OrgID: req.OrgID,
			ChannelID: channelID, ReleaseSetID: releaseSetID, ReleaseTargetID: releaseTargetID,
		}); err != nil {
			return nil, err
		}
		deviceIDs = candidateDeviceIDs(candidates)
		if err = moveModelMembership(
			txCtx, q, req.OperationID, req.OrgID, req.LaneID, req.LaneModelID,
			channelID, modelKey, deviceIDs, sourceBindings,
		); err != nil {
			return nil, err
		}
		if len(deviceIDs) > 0 {
			if err = createModelEnforcement(
				txCtx, q, req.OrgID, req.LaneID, req.LaneModelID, req.OperationID,
				"rollout_lane_initial", req.LaneModelID.String(), req.ActorUserID, deviceIDs,
			); err != nil {
				return nil, err
			}
		}
		if len(sourceModelIDs) > 0 {
			if _, err = q.BumpRolloutLaneModelRevisions(txCtx, sqlc.BumpRolloutLaneModelRevisionsParams{
				OrgID: req.OrgID, LaneModelIds: sourceModelIDs,
			}); err != nil {
				return nil, err
			}
		}
		if err = recordModelOperation(txCtx, q, modelOperationAuditRecord[
			createModelDeclarationRequestedPayload,
			createModelDeclarationAppliedPayload,
		]{
			OperationID: req.OperationID, OrgID: req.OrgID, LaneID: req.LaneID,
			LaneModelID: req.LaneModelID, Operation: modelOperationCreateDeclaration,
			IdempotencyKey: req.IdempotencyKey, Fingerprint: req.RequestFingerprint,
			ExpectedRevision: 0, ResultingRevision: declaration.Revision, Reason: req.Reason,
			ActorUserID: req.ActorUserID, ActorType: req.ActorType, ActorCredentialID: req.ActorCredentialID,
			Requested: createModelDeclarationRequestedPayload{
				LaneID: req.LaneID.String(), LaneModelID: req.LaneModelID.String(),
				ExpectedRevision: 0,
			},
			Applied: createModelDeclarationAppliedPayload{
				LaneModelID: req.LaneModelID.String(), ResultingRevision: declaration.Revision,
				Added: req.DeviceIdentifiers,
			},
		}); err != nil {
			return nil, err
		}
		affectedLaneIDs := append(sourceLaneIDs(sourceBindings), req.LaneID)
		if _, err = q.BumpRolloutLaneMembershipRevisions(txCtx, sqlc.BumpRolloutLaneMembershipRevisionsParams{
			OrgID: req.OrgID, LaneIds: uniqueUUIDs(affectedLaneIDs),
		}); err != nil {
			return nil, err
		}
		laneRow.Revision++
		return loadRolloutLane(txCtx, q, laneRow, true, nil)
	})
	if err != nil {
		if replay, replayErr := replayModelOperation(
			ctx, s.GetQueries(ctx), req.OrgID, req.LaneID, req.IdempotencyKey,
			req.RequestFingerprint, modelOperationCreateDeclaration,
		); replayErr == nil {
			return replay, nil
		}
		return nil, mapModelMutationStoreError("create rollout lane model declaration", err)
	}
	lane, ok := value.(*betweenchannel.Lane)
	if !ok {
		return nil, fmt.Errorf("create rollout lane model declaration: unexpected result %T", value)
	}
	return lane, nil
}

func (s *SQLRolloutLaneStore) PublishModelTarget(
	ctx context.Context,
	req betweenchannel.PublishModelTargetRequest,
) (*betweenchannel.Lane, error) {
	value, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		q := s.GetQueries(txCtx)
		if replay, replayErr := replayModelOperation(txCtx, q, req.OrgID, req.LaneID, req.IdempotencyKey, req.RequestFingerprint, modelOperationPublishTarget); replayErr == nil {
			return replay, nil
		} else if !errors.Is(replayErr, sql.ErrNoRows) {
			return nil, replayErr
		}
		if err := requireModelTopology(txCtx, q, req.OrgID); err != nil {
			return nil, err
		}
		declaration, err := q.LockRolloutLaneModelForMutation(
			txCtx,
			modelSelectorParams(req.LaneID, req.OrgID, req.LaneModelID, req.ModelIdentityKey),
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		if err != nil {
			return nil, err
		}
		if declaration.Revision != req.ExpectedRevision {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		if err = lockAndCheckModelWork(
			txCtx,
			q,
			req.OrgID,
			[]uuid.UUID{declaration.ID},
		); err != nil {
			return nil, err
		}
		count, err := q.CountActiveRolloutLaneModelBindings(txCtx, sqlc.CountActiveRolloutLaneModelBindingsParams{
			LaneModelID: declaration.ID, LaneID: declaration.LaneID, OrgID: declaration.OrgID,
		})
		if err != nil {
			return nil, err
		}
		if count != 0 {
			return nil, betweenchannel.ErrMembershipConflict
		}
		target := req.ReleaseTargets[0]
		if betweenchannel.CanonicalModelIdentityKey(target.Manufacturer, target.Model) != declaration.ModelIdentityKey {
			return nil, betweenchannel.ErrCompatibility
		}
		position, err := q.GetNextRolloutLaneChannelPosition(
			txCtx,
			sqlc.GetNextRolloutLaneChannelPositionParams{LaneID: req.LaneID, OrgID: req.OrgID},
		)
		if err != nil {
			return nil, err
		}
		releaseSetID, releaseTargetID, channelID, err := createSingletonModelChannel(
			txCtx, q, req.OrgID, req.LaneID, position, target,
		)
		if err != nil {
			return nil, err
		}
		if _, err = q.CreateRolloutLaneChannel(txCtx, sqlc.CreateRolloutLaneChannelParams{
			LaneID: req.LaneID, OrgID: req.OrgID, ChannelID: channelID, Position: position,
		}); err != nil {
			return nil, err
		}
		if _, err = q.CreateRolloutLaneModelChannel(txCtx, sqlc.CreateRolloutLaneModelChannelParams{
			LaneModelID: declaration.ID, LaneID: req.LaneID, OrgID: req.OrgID,
			ChannelID: channelID, ReleaseSetID: releaseSetID, ReleaseTargetID: releaseTargetID,
		}); err != nil {
			return nil, err
		}
		advanced, err := q.AdvanceRolloutLaneModelTarget(txCtx, sqlc.AdvanceRolloutLaneModelTargetParams{
			ChannelID: channelID, ReleaseSetID: releaseSetID, ReleaseTargetID: releaseTargetID,
			LaneModelID: declaration.ID, LaneID: req.LaneID, OrgID: req.OrgID,
			ExpectedRevision: req.ExpectedRevision,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		if err != nil {
			return nil, err
		}
		if err = recordModelOperation(txCtx, q, modelOperationAuditRecord[
			publishModelTargetRequestedPayload,
			publishModelTargetAppliedPayload,
		]{
			OperationID: req.OperationID, OrgID: req.OrgID, LaneID: req.LaneID,
			LaneModelID: declaration.ID, Operation: modelOperationPublishTarget,
			IdempotencyKey: req.IdempotencyKey, Fingerprint: req.RequestFingerprint,
			ExpectedRevision: req.ExpectedRevision, ResultingRevision: advanced.Revision, Reason: req.Reason,
			ActorUserID: req.ActorUserID, ActorType: req.ActorType, ActorCredentialID: req.ActorCredentialID,
			Requested: publishModelTargetRequestedPayload{
				LaneID: req.LaneID.String(), LaneModelID: declaration.ID.String(),
				ExpectedRevision: req.ExpectedRevision,
			},
			Applied: publishModelTargetAppliedPayload{
				LaneModelID: declaration.ID.String(), ResultingRevision: advanced.Revision,
			},
		}); err != nil {
			return nil, err
		}
		if _, err = q.BumpRolloutLaneMembershipRevisions(txCtx, sqlc.BumpRolloutLaneMembershipRevisionsParams{
			OrgID: req.OrgID, LaneIds: []uuid.UUID{req.LaneID},
		}); err != nil {
			return nil, err
		}
		laneRow, err := q.GetRolloutLane(txCtx, sqlc.GetRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID})
		if err != nil {
			return nil, err
		}
		return loadRolloutLane(txCtx, q, laneRow, false, nil)
	})
	if err != nil {
		if replay, replayErr := replayModelOperation(
			ctx, s.GetQueries(ctx), req.OrgID, req.LaneID, req.IdempotencyKey,
			req.RequestFingerprint, modelOperationPublishTarget,
		); replayErr == nil {
			return replay, nil
		}
		return nil, mapModelMutationStoreError("publish rollout lane model target", err)
	}
	lane, ok := value.(*betweenchannel.Lane)
	if !ok {
		return nil, fmt.Errorf("publish rollout lane model target: unexpected result %T", value)
	}
	return lane, nil
}

func (s *SQLRolloutLaneStore) PreviewModelMembershipChange(
	ctx context.Context,
	req betweenchannel.PreviewModelMembershipChangeRequest,
) (betweenchannel.MembershipChangePreview, error) {
	q := s.GetQueries(ctx)
	if err := requireModelTopology(ctx, q, req.OrgID); err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	declaration, err := q.LockRolloutLaneModelForMutation(
		ctx,
		modelSelectorParams(req.LaneID, req.OrgID, req.LaneModelID, req.ModelIdentityKey),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrDeclarationConflict
	}
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	return previewModelMembership(ctx, q, declaration, req.AddIdentifiers, req.RemoveIdentifiers)
}

func (s *SQLRolloutLaneStore) UpdateModelMembership(
	ctx context.Context,
	req betweenchannel.UpdateModelMembershipRequest,
) (betweenchannel.UpdateMembershipResult, error) {
	value, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		q := s.GetQueries(txCtx)
		if replay, replayErr := replayModelMembership(txCtx, q, req); replayErr == nil {
			return replay, nil
		} else if !errors.Is(replayErr, sql.ErrNoRows) {
			return nil, replayErr
		}
		if err := requireModelTopology(txCtx, q, req.OrgID); err != nil {
			return nil, err
		}
		declaration, err := q.LockRolloutLaneModelForMutation(
			txCtx,
			modelSelectorParams(req.LaneID, req.OrgID, req.LaneModelID, req.ModelIdentityKey),
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		if err != nil {
			return nil, err
		}
		if declaration.Revision != req.ExpectedRevision {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		identifiers := append(append([]string(nil), req.AddIdentifiers...), req.RemoveIdentifiers...)
		candidates, err := q.ListRolloutLaneMembershipCandidates(
			txCtx,
			sqlc.ListRolloutLaneMembershipCandidatesParams{OrgID: req.OrgID, DeviceIdentifiers: identifiers},
		)
		if err != nil || len(candidates) != len(identifiers) {
			return nil, betweenchannel.ErrMembershipConflict
		}
		deviceIDs := candidateDeviceIDs(candidates)
		sourceBindings, err := validatedSourceBindings(txCtx, q, req.OrgID, candidates)
		if err != nil {
			return nil, err
		}
		affectedModelIDs := distinctSourceModelIDs(sourceBindings, declaration.ID)
		lockedModels, err := q.LockRolloutLaneModelsForMutation(
			txCtx,
			sqlc.LockRolloutLaneModelsForMutationParams{OrgID: req.OrgID, LaneModelIds: affectedModelIDs},
		)
		if err != nil || len(lockedModels) != len(affectedModelIDs) {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		if _, err = q.LockBetweenChannelDevices(txCtx, sqlc.LockBetweenChannelDevicesParams{
			OrgID: req.OrgID, DeviceIds: deviceIDs,
		}); err != nil {
			return nil, err
		}
		if err = lockAndCheckModelWork(txCtx, q, req.OrgID, affectedModelIDs); err != nil {
			return nil, err
		}
		preview, err := previewModelMembership(
			txCtx, q, declaration, req.AddIdentifiers, req.RemoveIdentifiers,
		)
		if err != nil {
			return nil, err
		}
		if preview.RequiresFirmwareConfirmation && !req.ConfirmFirmware {
			return nil, betweenchannel.ErrFirmwareConfirmationRequired
		}
		if preview.RequiresReassignConfirmation && !req.ConfirmReassign {
			return nil, betweenchannel.ErrReassignmentConfirmationRequired
		}
		addCandidates := candidatesForIdentifiers(candidates, req.AddIdentifiers)
		addDeviceIDs := candidateDeviceIDs(addCandidates)
		removeCandidates := candidatesForIdentifiers(candidates, req.RemoveIdentifiers)
		removeDeviceIDs := candidateDeviceIDs(removeCandidates)
		sourceForAdds := bindingsForDeviceIDs(sourceBindings, addDeviceIDs)
		if err = moveModelMembership(
			txCtx, q, req.OperationID, req.OrgID, req.LaneID, declaration.ID,
			declaration.CurrentChannelID, declaration.ModelIdentityKey,
			addDeviceIDs, sourceForAdds,
		); err != nil {
			return nil, err
		}
		if len(removeDeviceIDs) > 0 {
			ended, endErr := q.EndRolloutLaneModelBindings(txCtx, sqlc.EndRolloutLaneModelBindingsParams{
				OperationID: uuid.NullUUID{UUID: req.OperationID, Valid: true},
				LaneID:      req.LaneID, LaneModelID: declaration.ID, OrgID: req.OrgID, DeviceIds: removeDeviceIDs,
			})
			if endErr != nil || len(ended) != len(removeDeviceIDs) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			removed, removeErr := q.RemoveRolloutLaneModelMembershipDevices(
				txCtx,
				sqlc.RemoveRolloutLaneModelMembershipDevicesParams{
					OrgID: req.OrgID, ChannelID: declaration.CurrentChannelID, DeviceIds: removeDeviceIDs,
				},
			)
			if removeErr != nil || len(removed) != len(removeDeviceIDs) {
				return nil, betweenchannel.ErrMembershipConflict
			}
		}
		if len(addDeviceIDs) > 0 {
			if err = createModelEnforcement(
				txCtx, q, req.OrgID, req.LaneID, declaration.ID, req.OperationID,
				"rollout_lane_membership", req.OperationID.String(), req.ActorUserID, addDeviceIDs,
			); err != nil {
				return nil, err
			}
		}
		revisions, err := q.BumpRolloutLaneModelRevisions(txCtx, sqlc.BumpRolloutLaneModelRevisionsParams{
			OrgID: req.OrgID, LaneModelIds: affectedModelIDs,
		})
		if err != nil || len(revisions) != len(affectedModelIDs) {
			return nil, betweenchannel.ErrDeclarationConflict
		}
		var resultingRevision int64
		for _, revision := range revisions {
			if revision.ID == declaration.ID {
				resultingRevision = revision.Revision
			}
		}
		if err = recordModelOperation(txCtx, q, modelOperationAuditRecord[
			updateModelMembershipRequestedPayload,
			updateModelMembershipAppliedPayload,
		]{
			OperationID: req.OperationID, OrgID: req.OrgID, LaneID: req.LaneID,
			LaneModelID: declaration.ID, Operation: modelOperationUpdateMembership,
			IdempotencyKey: req.IdempotencyKey, Fingerprint: req.RequestFingerprint,
			ExpectedRevision: req.ExpectedRevision, ResultingRevision: resultingRevision, Reason: req.Reason,
			ActorUserID: req.ActorUserID, ActorType: req.ActorType, ActorCredentialID: req.ActorCredentialID,
			Requested: updateModelMembershipRequestedPayload{
				LaneID: req.LaneID.String(), LaneModelID: declaration.ID.String(),
				ExpectedRevision: req.ExpectedRevision,
			},
			Applied: updateModelMembershipAppliedPayload{
				LaneModelID: declaration.ID.String(), ResultingRevision: resultingRevision,
				Added: req.AddIdentifiers,
			},
		}); err != nil {
			return nil, err
		}
		affectedLaneIDs := append(sourceLaneIDs(sourceBindings), req.LaneID)
		if _, err = q.BumpRolloutLaneMembershipRevisions(txCtx, sqlc.BumpRolloutLaneMembershipRevisionsParams{
			OrgID: req.OrgID, LaneIds: uniqueUUIDs(affectedLaneIDs),
		}); err != nil {
			return nil, err
		}
		laneRow, err := q.GetRolloutLane(txCtx, sqlc.GetRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID})
		if err != nil {
			return nil, err
		}
		lane, err := loadRolloutLane(txCtx, q, laneRow, false, nil)
		if err != nil {
			return nil, err
		}
		members, err := laneMembersForIdentifiers(txCtx, q, req.OrgID, req.LaneID, req.AddIdentifiers)
		if err != nil {
			return nil, err
		}
		return &betweenchannel.UpdateMembershipResult{Lane: lane, TransitionMembers: members}, nil
	})
	if err != nil {
		if replay, replayErr := replayModelMembership(ctx, s.GetQueries(ctx), req); replayErr == nil {
			return *replay, nil
		}
		return betweenchannel.UpdateMembershipResult{}, mapModelMutationStoreError(
			"update rollout lane model membership",
			err,
		)
	}
	result, ok := value.(*betweenchannel.UpdateMembershipResult)
	if !ok {
		return betweenchannel.UpdateMembershipResult{}, fmt.Errorf(
			"update rollout lane model membership: unexpected result %T",
			value,
		)
	}
	return *result, nil
}

func requireModelTopology(ctx context.Context, q sqlc.Querier, orgID int64) error {
	enabled, err := rolloutLaneTopologyEnabled(ctx, q, orgID)
	if err != nil {
		return err
	}
	if !enabled {
		return betweenchannel.ErrTopologyNotReady
	}
	return nil
}

func modelSelectorParams(
	laneID uuid.UUID,
	orgID int64,
	laneModelID uuid.UUID,
	modelIdentityKey string,
) sqlc.LockRolloutLaneModelForMutationParams {
	return sqlc.LockRolloutLaneModelForMutationParams{
		LaneID:           laneID,
		OrgID:            orgID,
		LaneModelID:      uuid.NullUUID{UUID: laneModelID, Valid: laneModelID != uuid.Nil},
		ModelIdentityKey: emptyToNullString(modelIdentityKey),
	}
}

func createSingletonModelChannel(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	laneID uuid.UUID,
	position int32,
	target betweenchannel.ReleaseTarget,
) (int64, int64, int64, error) {
	releaseSet, err := q.CreateFirmwareReleaseSet(ctx, orgID)
	if err != nil {
		return 0, 0, 0, err
	}
	releaseTarget, err := q.CreateFirmwareReleaseTarget(ctx, sqlc.CreateFirmwareReleaseTargetParams{
		ReleaseSetID: releaseSet.ID, OrgID: orgID, FirmwareFileID: target.FirmwareFileID,
		TargetManufacturer: target.Manufacturer, TargetModel: target.Model,
		FirmwareVersion: target.FirmwareVersion, Sha256: target.SHA256,
	})
	if err != nil {
		return 0, 0, 0, err
	}
	channelID, err := createLanePhysicalChannel(ctx, q, orgID, laneID, position, releaseSet.ID)
	return releaseSet.ID, releaseTarget.ID, channelID, err
}

func lockAndPreviewDeclarationMembers(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.CreateModelDeclarationRequest,
	target betweenchannel.ReleaseTarget,
) ([]sqlc.ListRolloutLaneMembershipCandidatesRow, betweenchannel.InitialEnforcementPreview, error) {
	if len(req.DeviceIdentifiers) == 0 {
		return nil, betweenchannel.InitialEnforcementPreview{Targets: req.ReleaseTargets}, nil
	}
	candidates, err := q.ListRolloutLaneMembershipCandidates(
		ctx,
		sqlc.ListRolloutLaneMembershipCandidatesParams{
			OrgID: req.OrgID, DeviceIdentifiers: req.DeviceIdentifiers,
		},
	)
	if err != nil || len(candidates) != len(req.DeviceIdentifiers) {
		return nil, betweenchannel.InitialEnforcementPreview{}, betweenchannel.ErrMembershipConflict
	}
	preview, _, err := previewLaneWithCandidates(
		betweenchannel.PreviewLaneRequest{
			OrgID: req.OrgID, ReleaseTargets: []betweenchannel.ReleaseTarget{target},
			DeviceIdentifiers: req.DeviceIdentifiers,
		},
		candidates,
	)
	return candidates, preview, err
}

func validateDeclarationConfirmations(
	req betweenchannel.CreateModelDeclarationRequest,
	preview betweenchannel.InitialEnforcementPreview,
) error {
	if preview.RequiresConfirmation() && !req.ConfirmInitialEnforcement {
		return betweenchannel.ErrInitialEnforcementConfirmationRequired
	}
	if preview.RequiresReassignConfirmation && !req.ConfirmReassignment {
		return betweenchannel.ErrReassignmentConfirmationRequired
	}
	if !preview.RequiresReassignConfirmation {
		return nil
	}
	token, err := betweenchannel.ReassignmentConfirmationToken(
		betweenchannel.PreviewLaneRequest{
			OrgID:             req.OrgID,
			ReleaseTargets:    req.ReleaseTargets,
			DeviceIdentifiers: req.DeviceIdentifiers,
		},
		preview,
	)
	if err != nil || token != req.ReassignmentConfirmationToken {
		return betweenchannel.ErrReassignmentConfirmationRequired
	}
	return nil
}

func validatedSourceBindings(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
) ([]sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	rows, err := q.ListActiveRolloutLaneModelBindingsForDevices(
		ctx,
		sqlc.ListActiveRolloutLaneModelBindingsForDevicesParams{
			OrgID: orgID, DeviceIds: candidateDeviceIDs(candidates),
		},
	)
	if err != nil {
		return nil, err
	}
	byDeviceID := make(map[int64]sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow, len(rows))
	for _, row := range rows {
		if !row.PhysicalChannelID.Valid || row.PhysicalChannelID.Int64 != row.ChannelID {
			return nil, betweenchannel.ErrMembershipConflict
		}
		if _, exists := byDeviceID[row.DeviceID]; exists {
			return nil, betweenchannel.ErrMembershipConflict
		}
		byDeviceID[row.DeviceID] = row
	}
	for _, candidate := range candidates {
		if candidate.SourceLaneID.Valid {
			binding, ok := byDeviceID[candidate.DeviceID]
			if !ok || binding.LaneID != candidate.SourceLaneID.UUID ||
				binding.ChannelID != candidate.ChannelID.Int64 {
				return nil, betweenchannel.ErrMembershipConflict
			}
		}
	}
	return rows, nil
}

func previewModelMembership(
	ctx context.Context,
	q sqlc.Querier,
	declaration sqlc.RolloutLaneModel,
	addIdentifiers []string,
	removeIdentifiers []string,
) (betweenchannel.MembershipChangePreview, error) {
	targetRow, err := q.GetRolloutLaneModelCurrentTarget(ctx, sqlc.GetRolloutLaneModelCurrentTargetParams{
		LaneModelID: declaration.ID, LaneID: declaration.LaneID, OrgID: declaration.OrgID,
	})
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	target := betweenchannel.ReleaseTarget{
		FirmwareFileID:  targetRow.FirmwareFileID,
		Manufacturer:    targetRow.Manufacturer,
		Model:           targetRow.Model,
		FirmwareVersion: targetRow.FirmwareVersion,
		SHA256:          targetRow.Sha256,
	}
	identifiers := append(append([]string(nil), addIdentifiers...), removeIdentifiers...)
	candidates, err := q.ListRolloutLaneMembershipCandidates(
		ctx,
		sqlc.ListRolloutLaneMembershipCandidatesParams{OrgID: declaration.OrgID, DeviceIdentifiers: identifiers},
	)
	if err != nil || len(candidates) != len(identifiers) {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
	}
	sourceBindings, err := validatedSourceBindings(ctx, q, declaration.OrgID, candidates)
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	bindingByDevice := make(map[int64]sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow, len(sourceBindings))
	for _, binding := range sourceBindings {
		bindingByDevice[binding.DeviceID] = binding
	}
	candidateByIdentifier := membershipCandidatesByIdentifier(candidates)
	devices := make([]initialLaneDevice, 0, len(addIdentifiers))
	reassignments := make([]betweenchannel.MembershipReassignment, 0)
	for _, identifier := range addIdentifiers {
		candidate := candidateByIdentifier[identifier]
		if betweenchannel.CanonicalModelIdentityKey(candidate.Manufacturer, candidate.Model) != declaration.ModelIdentityKey {
			return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrCompatibility
		}
		if binding, ok := bindingByDevice[candidate.DeviceID]; ok {
			if binding.LaneModelID == declaration.ID {
				return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
			}
			reassignments = append(reassignments, betweenchannel.MembershipReassignment{
				DeviceIdentifier: identifier, SourceLaneID: binding.LaneID, SourceLaneLabel: binding.LaneLabel,
				SourceChannelID: binding.ChannelID, SourceLaneRevision: binding.LaneModelRevision,
			})
		} else if candidate.ChannelID.Valid {
			return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
		}
		devices = append(devices, initialLaneDevice{
			DeviceID: candidate.DeviceID, DeviceIdentifier: identifier,
			Manufacturer: candidate.Manufacturer, Model: candidate.Model,
			CurrentFirmwareVersion: candidate.ObservedFirmwareVersion,
		})
	}
	removalCandidates := candidatesForIdentifiers(candidates, removeIdentifiers)
	for _, candidate := range removalCandidates {
		binding, ok := bindingByDevice[candidate.DeviceID]
		if !ok || binding.LaneModelID != declaration.ID ||
			binding.ChannelID != declaration.CurrentChannelID {
			return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
		}
	}
	removals, err := laneMembersForIdentifiers(
		ctx, q, declaration.OrgID, declaration.LaneID, removeIdentifiers,
	)
	if err != nil || len(removals) != len(removeIdentifiers) {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
	}
	firmwarePreview := buildInitialEnforcementPreview(devices, []betweenchannel.ReleaseTarget{target})
	firmwarePreview.Targets = []betweenchannel.ReleaseTarget{target}
	sort.Slice(reassignments, func(i, j int) bool {
		return reassignments[i].DeviceIdentifier < reassignments[j].DeviceIdentifier
	})
	return betweenchannel.MembershipChangePreview{
		TargetFirmwarePreview:        firmwarePreview,
		Reassignments:                reassignments,
		Removals:                     removals,
		RequiresFirmwareConfirmation: firmwarePreview.RequiresConfirmation(),
		RequiresReassignConfirmation: len(reassignments) > 0,
	}, nil
}

func moveModelMembership(
	ctx context.Context,
	q sqlc.Querier,
	operationID uuid.UUID,
	orgID int64,
	laneID uuid.UUID,
	laneModelID uuid.UUID,
	channelID int64,
	modelIdentityKey string,
	deviceIDs []int64,
	sourceBindings []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow,
) error {
	if len(deviceIDs) == 0 {
		return nil
	}
	if err := endSourceModelBindings(ctx, q, operationID, orgID, sourceBindings); err != nil {
		return err
	}
	for sourceChannelID, sourceDeviceIDs := range sourceDevicesByChannel(sourceBindings) {
		removed, err := q.RemoveRolloutLaneModelMembershipDevices(
			ctx,
			sqlc.RemoveRolloutLaneModelMembershipDevicesParams{
				OrgID: orgID, ChannelID: sourceChannelID, DeviceIds: sourceDeviceIDs,
			},
		)
		if err != nil || len(removed) != len(sourceDeviceIDs) {
			return betweenchannel.ErrMembershipConflict
		}
	}
	added, err := q.AddRolloutLaneModelMembershipDevices(ctx, sqlc.AddRolloutLaneModelMembershipDevicesParams{
		ChannelID: channelID, OrgID: orgID, DeviceIds: deviceIDs,
	})
	if err != nil || len(added) != len(deviceIDs) {
		return betweenchannel.ErrMembershipConflict
	}
	bindings, err := q.CreateRolloutLaneModelBindings(ctx, sqlc.CreateRolloutLaneModelBindingsParams{
		LaneID: laneID, LaneModelID: laneModelID, ChannelID: channelID, OrgID: orgID,
		DeviceIds: deviceIDs, ModelIdentityKey: sql.NullString{String: modelIdentityKey, Valid: true},
	})
	if err != nil || len(bindings) != len(deviceIDs) {
		return betweenchannel.ErrCompatibility
	}
	return nil
}

func endSourceModelBindings(
	ctx context.Context,
	q sqlc.Querier,
	operationID uuid.UUID,
	orgID int64,
	bindings []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow,
) error {
	type modelKey struct {
		laneID      uuid.UUID
		laneModelID uuid.UUID
	}
	devicesByModel := make(map[modelKey][]int64)
	for _, binding := range bindings {
		key := modelKey{laneID: binding.LaneID, laneModelID: binding.LaneModelID}
		devicesByModel[key] = append(devicesByModel[key], binding.DeviceID)
	}
	for key, deviceIDs := range devicesByModel {
		ended, err := q.EndRolloutLaneModelBindings(ctx, sqlc.EndRolloutLaneModelBindingsParams{
			OperationID: uuid.NullUUID{UUID: operationID, Valid: true},
			LaneID:      key.laneID, LaneModelID: key.laneModelID,
			OrgID: orgID, DeviceIds: deviceIDs,
		})
		if err != nil || len(ended) != len(deviceIDs) {
			return betweenchannel.ErrMembershipConflict
		}
	}
	return nil
}

func sourceDevicesByChannel(
	bindings []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow,
) map[int64][]int64 {
	result := make(map[int64][]int64)
	for _, binding := range bindings {
		result[binding.ChannelID] = append(result[binding.ChannelID], binding.DeviceID)
	}
	return result
}

func createModelEnforcement(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	laneID uuid.UUID,
	laneModelID uuid.UUID,
	operationID uuid.UUID,
	causeType string,
	causeReference string,
	actorUserID int64,
	deviceIDs []int64,
) error {
	authority, err := q.CreateChannelFirmwareAuthority(ctx, sqlc.CreateChannelFirmwareAuthorityParams{
		ID: uuid.New(), OrgID: orgID, AuthorityType: causeType,
		AuthorityReference: causeReference, CreatedByUserID: actorUserID,
	})
	if err != nil {
		return err
	}
	count, err := q.CreateRolloutLaneModelEnforcements(ctx, sqlc.CreateRolloutLaneModelEnforcementsParams{
		CauseType: causeType, CauseReference: sql.NullString{String: operationID.String(), Valid: true},
		DeviceIds: deviceIDs, AuthorityID: authority.ID, AuthorityRevision: authority.Revision,
		LaneModelID: laneModelID, LaneID: laneID, OrgID: orgID,
	})
	if err != nil || count != int64(len(deviceIDs)) {
		return betweenchannel.ErrCompatibility
	}
	return nil
}

func lockAndCheckModelWork(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	modelIDs []uuid.UUID,
) error {
	models, err := q.LockRolloutLaneModelsForMutation(ctx, sqlc.LockRolloutLaneModelsForMutationParams{
		OrgID: orgID, LaneModelIds: modelIDs,
	})
	if err != nil || len(models) != len(modelIDs) {
		return betweenchannel.ErrDeclarationConflict
	}
	for _, model := range models {
		active, err := q.HasActiveRolloutLaneModelWork(ctx, sqlc.HasActiveRolloutLaneModelWorkParams{
			LaneModelID: model.ID, LaneID: model.LaneID, OrgID: orgID,
		})
		if err != nil {
			return err
		}
		if active.Valid && active.Bool {
			return betweenchannel.ErrModelWorkActive
		}
	}
	return nil
}

type createModelDeclarationRequestedPayload struct {
	LaneID           string `json:"lane_id"`
	LaneModelID      string `json:"lane_model_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type createModelDeclarationAppliedPayload struct {
	LaneModelID       string   `json:"lane_model_id"`
	ResultingRevision int64    `json:"resulting_revision"`
	Added             []string `json:"added"`
}

type publishModelTargetRequestedPayload struct {
	LaneID           string `json:"lane_id"`
	LaneModelID      string `json:"lane_model_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type publishModelTargetAppliedPayload struct {
	LaneModelID       string   `json:"lane_model_id"`
	ResultingRevision int64    `json:"resulting_revision"`
	Added             []string `json:"added"`
}

type updateModelMembershipRequestedPayload struct {
	LaneID           string `json:"lane_id"`
	LaneModelID      string `json:"lane_model_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type updateModelMembershipAppliedPayload struct {
	LaneModelID       string   `json:"lane_model_id"`
	ResultingRevision int64    `json:"resulting_revision"`
	Added             []string `json:"added"`
}

type modelOperationAuditRecord[Requested any, Applied any] struct {
	OperationID       uuid.UUID
	OrgID             int64
	LaneID            uuid.UUID
	LaneModelID       uuid.UUID
	Operation         modelOperation
	IdempotencyKey    string
	Fingerprint       string
	ExpectedRevision  int64
	ResultingRevision int64
	Reason            string
	ActorUserID       int64
	ActorType         rolloutdomain.ActorType
	ActorCredentialID *string
	Requested         Requested
	Applied           Applied
}

func recordModelOperation[Requested any, Applied any](
	ctx context.Context,
	q sqlc.Querier,
	record modelOperationAuditRecord[Requested, Applied],
) error {
	requested, err := json.Marshal(record.Requested)
	if err != nil {
		return fmt.Errorf("marshal requested model operation: %w", err)
	}
	applied, err := json.Marshal(record.Applied)
	if err != nil {
		return fmt.Errorf("marshal applied model operation: %w", err)
	}
	_, err = q.CreateRolloutLaneTopologyAdminOperation(ctx, sqlc.CreateRolloutLaneTopologyAdminOperationParams{
		OperationID: record.OperationID, OrgID: record.OrgID, Operation: string(record.Operation),
		LaneID:         uuid.NullUUID{UUID: record.LaneID, Valid: true},
		LaneModelID:    uuid.NullUUID{UUID: record.LaneModelID, Valid: true},
		IdempotencyKey: record.IdempotencyKey, RequestFingerprint: record.Fingerprint,
		ExpectedRevision: record.ExpectedRevision, ResultingRevision: record.ResultingRevision,
		Reason: record.Reason, Requested: requested, Applied: applied, ActorUserID: record.ActorUserID,
		ActorType: persistedActorType(record.ActorType), ActorCredentialID: ptrToNullString(record.ActorCredentialID),
	})
	return err
}

func replayModelOperation(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	laneID uuid.UUID,
	idempotencyKey string,
	fingerprint string,
	operation modelOperation,
) (*betweenchannel.Lane, error) {
	row, err := q.GetRolloutLaneTopologyAdminOperationByKey(
		ctx,
		sqlc.GetRolloutLaneTopologyAdminOperationByKeyParams{OrgID: orgID, IdempotencyKey: idempotencyKey},
	)
	if err != nil {
		return nil, err
	}
	if row.Operation != string(operation) || row.RequestFingerprint != fingerprint ||
		!row.LaneID.Valid || row.LaneID.UUID != laneID {
		return nil, betweenchannel.ErrIdempotencyConflict
	}
	laneRow, err := q.GetRolloutLane(ctx, sqlc.GetRolloutLaneParams{LaneID: laneID, OrgID: orgID})
	if err != nil {
		return nil, err
	}
	return loadRolloutLane(ctx, q, laneRow, false, nil)
}

func replayModelMembership(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.UpdateModelMembershipRequest,
) (*betweenchannel.UpdateMembershipResult, error) {
	lane, err := replayModelOperation(
		ctx, q, req.OrgID, req.LaneID, req.IdempotencyKey,
		req.RequestFingerprint, modelOperationUpdateMembership,
	)
	if err != nil {
		return nil, err
	}
	members, err := laneMembersForIdentifiers(
		ctx, q, req.OrgID, req.LaneID, req.AddIdentifiers,
	)
	if err != nil {
		return nil, err
	}
	return &betweenchannel.UpdateMembershipResult{Lane: lane, TransitionMembers: members}, nil
}

func mapModelMutationStoreError(operation string, err error) error {
	switch {
	case isUniqueViolationOn(err, "uq_rollout_lane_topology_admin_operation_key"):
		return betweenchannel.ErrIdempotencyConflict
	case isUniqueViolationOn(err, "uq_rollout_lane_model_identity"):
		return betweenchannel.ErrDeclarationConflict
	case isUniqueViolationOn(err, "uq_rollout_lane_model_binding_active_device"),
		isUniqueViolationOn(err, "device_set_membership_org_id_device_id_device_set_type_key"):
		return betweenchannel.ErrMembershipConflict
	case isUniqueViolationOn(err, "uq_channel_firmware_enforcement_active_device"):
		return betweenchannel.ErrModelWorkActive
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func distinctSourceModelIDs(
	bindings []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow,
	target uuid.UUID,
) []uuid.UUID {
	values := make([]uuid.UUID, 0, len(bindings)+1)
	if target != uuid.Nil {
		values = append(values, target)
	}
	for _, binding := range bindings {
		values = append(values, binding.LaneModelID)
	}
	return uniqueUUIDs(values)
}

func sourceLaneIDs(
	bindings []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow,
) []uuid.UUID {
	values := make([]uuid.UUID, 0, len(bindings))
	for _, binding := range bindings {
		values = append(values, binding.LaneID)
	}
	return uniqueUUIDs(values)
}

func uniqueUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func candidatesForIdentifiers(
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
	identifiers []string,
) []sqlc.ListRolloutLaneMembershipCandidatesRow {
	byIdentifier := membershipCandidatesByIdentifier(candidates)
	result := make([]sqlc.ListRolloutLaneMembershipCandidatesRow, 0, len(identifiers))
	for _, identifier := range identifiers {
		if candidate, ok := byIdentifier[identifier]; ok {
			result = append(result, candidate)
		}
	}
	return result
}

func bindingsForDeviceIDs(
	bindings []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow,
	deviceIDs []int64,
) []sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow {
	selected := make(map[int64]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		selected[deviceID] = struct{}{}
	}
	result := make([]sqlc.ListActiveRolloutLaneModelBindingsForDevicesRow, 0)
	for _, binding := range bindings {
		if _, ok := selected[binding.DeviceID]; ok {
			result = append(result, binding)
		}
	}
	return result
}
