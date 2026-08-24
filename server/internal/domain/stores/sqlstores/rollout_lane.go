package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	infrastructuredb "github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const baselineWindow = 30 * time.Minute

type SQLRolloutLaneStore struct {
	SQLConnectionManager
	transactor *SQLTransactor
}

var (
	_ betweenchannel.LaneStore         = (*SQLRolloutLaneStore)(nil)
	_ betweenchannel.StrategyStore     = (*SQLRolloutLaneStore)(nil)
	_ betweenchannel.FinalizationStore = (*SQLRolloutLaneStore)(nil)
)

func NewSQLRolloutLaneStore(conn *sql.DB) *SQLRolloutLaneStore {
	return &SQLRolloutLaneStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
		transactor:           NewSQLTransactor(conn),
	}
}

func (s *SQLRolloutLaneStore) PreviewLane(
	ctx context.Context,
	req betweenchannel.PreviewLaneRequest,
) (betweenchannel.InitialEnforcementPreview, error) {
	if len(req.DeviceIdentifiers) == 0 {
		return betweenchannel.InitialEnforcementPreview{
			Targets: req.ReleaseTargets,
		}, nil
	}
	q := s.GetQueries(ctx)
	candidates, err := q.ListRolloutLaneMembershipCandidates(
		ctx,
		sqlc.ListRolloutLaneMembershipCandidatesParams{
			OrgID:             req.OrgID,
			DeviceIdentifiers: req.DeviceIdentifiers,
		},
	)
	if err != nil {
		return betweenchannel.InitialEnforcementPreview{}, fmt.Errorf(
			"preview rollout lane devices: %w",
			err,
		)
	}
	if len(candidates) != len(req.DeviceIdentifiers) {
		return betweenchannel.InitialEnforcementPreview{}, betweenchannel.ErrMembershipConflict
	}
	preview, sourceLaneIDs, err := previewLaneWithCandidates(req, candidates)
	if err != nil {
		return betweenchannel.InitialEnforcementPreview{}, err
	}
	if len(sourceLaneIDs) > 0 {
		active, checkErr := q.HasActiveRolloutLaneManagementWork(
			ctx,
			sqlc.HasActiveRolloutLaneManagementWorkParams{
				OrgID:   req.OrgID,
				LaneIds: sourceLaneIDs,
			},
		)
		if checkErr != nil {
			return betweenchannel.InitialEnforcementPreview{}, checkErr
		}
		if active.Valid && active.Bool {
			return betweenchannel.InitialEnforcementPreview{}, betweenchannel.ErrLaneWorkActive
		}
	}
	return preview, nil
}

func (s *SQLRolloutLaneStore) CreateLane(
	ctx context.Context,
	req betweenchannel.CreateLaneRequest,
) (*betweenchannel.Lane, error) {
	result, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			existing, getErr := q.GetRolloutLaneByIdempotencyKey(
				txCtx,
				sqlc.GetRolloutLaneByIdempotencyKeyParams{
					OrgID:          req.OrgID,
					IdempotencyKey: req.IdempotencyKey,
				},
			)
			switch {
			case getErr == nil:
				if existing.DeletedAt.Valid || existing.CreateFingerprint != req.RequestFingerprint {
					return nil, betweenchannel.ErrIdempotencyConflict
				}
				return loadRolloutLane(txCtx, q, existing, true, nil)
			case !errors.Is(getErr, sql.ErrNoRows):
				return nil, getErr
			}
			topologyEnabled, topologyErr := rolloutLaneTopologyEnabled(txCtx, q, req.OrgID)
			if topologyErr != nil {
				return nil, topologyErr
			}
			if topologyEnabled && len(req.ReleaseTargets) != 1 {
				return nil, betweenchannel.ErrScalarProjectionUnavailable
			}

			var (
				candidates         []sqlc.ListRolloutLaneMembershipCandidatesRow
				sourceLaneIDs      []uuid.UUID
				sourceModelIDs     []uuid.UUID
				createdLaneModelID uuid.UUID
				preview            betweenchannel.InitialEnforcementPreview
			)
			if len(req.DeviceIdentifiers) > 0 {
				var candidateErr error
				candidates, candidateErr = q.ListRolloutLaneMembershipCandidates(
					txCtx,
					sqlc.ListRolloutLaneMembershipCandidatesParams{
						OrgID:             req.OrgID,
						DeviceIdentifiers: req.DeviceIdentifiers,
					},
				)
				if candidateErr != nil {
					return nil, candidateErr
				}
				if len(candidates) != len(req.DeviceIdentifiers) {
					return nil, betweenchannel.ErrMembershipConflict
				}
				_, sourceLaneIDs, candidateErr = previewLaneWithCandidates(
					createLanePreviewRequest(req),
					candidates,
				)
				if candidateErr != nil {
					return nil, candidateErr
				}

				if len(sourceLaneIDs) > 0 {
					lockedLanes, lockErr := q.LockRolloutLanes(
						txCtx,
						sqlc.LockRolloutLanesParams{
							OrgID:   req.OrgID,
							LaneIds: sourceLaneIDs,
						},
					)
					if lockErr != nil {
						return nil, lockErr
					}
					if len(lockedLanes) != len(sourceLaneIDs) {
						return nil, betweenchannel.ErrMembershipConflict
					}
					channelIDs, listErr := rolloutLaneChannelIDs(
						txCtx,
						q,
						req.OrgID,
						sourceLaneIDs,
					)
					if listErr != nil {
						return nil, listErr
					}
					lockedChannels, lockErr := q.LockBetweenChannelChannels(
						txCtx,
						sqlc.LockBetweenChannelChannelsParams{
							OrgID:      req.OrgID,
							ChannelIds: channelIDs,
						},
					)
					if lockErr != nil {
						return nil, lockErr
					}
					if len(lockedChannels) != len(channelIDs) {
						return nil, betweenchannel.ErrMembershipConflict
					}
				}

				deviceIDs := candidateDeviceIDs(candidates)
				lockedDevices, lockErr := q.LockBetweenChannelDevices(
					txCtx,
					sqlc.LockBetweenChannelDevicesParams{
						OrgID:     req.OrgID,
						DeviceIds: deviceIDs,
					},
				)
				if lockErr != nil {
					return nil, lockErr
				}
				if len(lockedDevices) != len(deviceIDs) {
					return nil, betweenchannel.ErrMembershipConflict
				}
				revalidated, revalidateErr := q.ListRolloutLaneMembershipCandidates(
					txCtx,
					sqlc.ListRolloutLaneMembershipCandidatesParams{
						OrgID:             req.OrgID,
						DeviceIdentifiers: req.DeviceIdentifiers,
					},
				)
				if revalidateErr != nil {
					return nil, revalidateErr
				}
				revalidatedPreview, revalidatedSourceLaneIDs, revalidateErr := previewLaneWithCandidates(
					createLanePreviewRequest(req),
					revalidated,
				)
				if revalidateErr != nil ||
					!slices.Equal(sourceLaneIDs, revalidatedSourceLaneIDs) {
					return nil, betweenchannel.ErrMembershipConflict
				}
				candidates = revalidated
				preview = revalidatedPreview

				if len(sourceLaneIDs) > 0 {
					if _, lockErr = q.LockRolloutLaneManagementAuthorities(
						txCtx,
						sqlc.LockRolloutLaneManagementAuthoritiesParams{
							OrgID:   req.OrgID,
							LaneIds: sourceLaneIDs,
						},
					); lockErr != nil {
						return nil, lockErr
					}
					if _, lockErr = q.LockRolloutLaneOwnedRolloutMembers(
						txCtx,
						sqlc.LockRolloutLaneOwnedRolloutMembersParams{
							OrgID:   req.OrgID,
							LaneIds: sourceLaneIDs,
						},
					); lockErr != nil {
						return nil, lockErr
					}
					active, checkErr := q.HasActiveRolloutLaneManagementWork(
						txCtx,
						sqlc.HasActiveRolloutLaneManagementWorkParams{
							OrgID:   req.OrgID,
							LaneIds: sourceLaneIDs,
						},
					)
					if checkErr != nil {
						return nil, checkErr
					}
					if active.Valid && active.Bool {
						return nil, betweenchannel.ErrLaneWorkActive
					}
				}
			}
			if preview.RequiresConfirmation() && !req.ConfirmInitialEnforcement {
				return nil, fmt.Errorf(
					"%w: %d mismatched and %d unknown miners",
					betweenchannel.ErrInitialEnforcementConfirmationRequired,
					preview.MismatchedCount,
					preview.UnknownCount,
				)
			}
			if preview.RequiresReassignConfirmation && !req.ConfirmReassignment {
				return nil, betweenchannel.ErrReassignmentConfirmationRequired
			}
			if !preview.RequiresReassignConfirmation && req.ReassignmentConfirmationToken != "" {
				return nil, betweenchannel.ErrReassignmentConfirmationRequired
			}
			if preview.RequiresReassignConfirmation {
				expectedToken, tokenErr := betweenchannel.ReassignmentConfirmationToken(
					createLanePreviewRequest(req),
					preview,
				)
				if tokenErr != nil {
					return nil, tokenErr
				}
				if req.ReassignmentConfirmationToken != expectedToken {
					return nil, betweenchannel.ErrReassignmentConfirmationRequired
				}
			}

			releaseSetID, createErr := createLaneReleaseSet(
				txCtx,
				q,
				req.OrgID,
				req.ReleaseTargets,
			)
			if createErr != nil {
				return nil, createErr
			}
			channelID, createErr := createLanePhysicalChannel(
				txCtx,
				q,
				req.OrgID,
				req.ID,
				0,
				releaseSetID,
			)
			if createErr != nil {
				return nil, createErr
			}
			candidateByIdentifier := membershipCandidatesByIdentifier(candidates)
			reassignmentIdentifiers := make([]string, 0, len(preview.Reassignments))
			for _, reassignment := range preview.Reassignments {
				reassignmentIdentifiers = append(
					reassignmentIdentifiers,
					reassignment.DeviceIdentifier,
				)
			}
			if len(reassignmentIdentifiers) > 0 {
				reassignedDeviceIDs := candidateIDsForIdentifiers(
					candidateByIdentifier,
					reassignmentIdentifiers,
				)
				if topologyEnabled {
					sourceBindings, bindingErr := validatedSourceBindings(
						txCtx,
						q,
						req.OrgID,
						candidatesForIdentifiers(candidates, reassignmentIdentifiers),
					)
					if bindingErr != nil {
						return nil, bindingErr
					}
					if bindingErr = endSourceModelBindings(
						txCtx,
						q,
						req.ChangeID,
						req.OrgID,
						sourceBindings,
					); bindingErr != nil {
						return nil, bindingErr
					}
					sourceModelIDs = distinctSourceModelIDs(sourceBindings, uuid.Nil)
				}
				removed, removeErr := q.RemoveRolloutLaneMembershipDevices(
					txCtx,
					sqlc.RemoveRolloutLaneMembershipDevicesParams{
						OrgID:     req.OrgID,
						LaneIds:   sourceLaneIDs,
						DeviceIds: reassignedDeviceIDs,
					},
				)
				if removeErr != nil {
					return nil, removeErr
				}
				if len(removed) != len(reassignedDeviceIDs) {
					return nil, betweenchannel.ErrMembershipConflict
				}
			}
			added, addErr := q.AddDevicesToDeviceSet(
				txCtx,
				sqlc.AddDevicesToDeviceSetParams{
					OrgID:             req.OrgID,
					DeviceSetID:       channelID,
					DeviceIdentifiers: req.DeviceIdentifiers,
				},
			)
			if addErr != nil {
				return nil, addErr
			}
			if len(added) != len(req.DeviceIdentifiers) {
				return nil, betweenchannel.ErrMembershipConflict
			}

			laneRow, createErr := q.CreateRolloutLane(
				txCtx,
				sqlc.CreateRolloutLaneParams{
					LaneID:            req.ID,
					OrgID:             req.OrgID,
					Label:             req.Label,
					Description:       req.Description,
					CurrentChannelID:  channelID,
					IdempotencyKey:    req.IdempotencyKey,
					CreateFingerprint: req.RequestFingerprint,
					CreatedByUserID:   req.ActorUserID,
				},
			)
			if createErr != nil {
				return nil, createErr
			}
			if _, createErr = q.CreateRolloutLaneChannel(
				txCtx,
				sqlc.CreateRolloutLaneChannelParams{
					LaneID:    req.ID,
					OrgID:     req.OrgID,
					ChannelID: channelID,
					Position:  0,
				},
			); createErr != nil {
				return nil, createErr
			}
			if topologyEnabled {
				target := req.ReleaseTargets[0]
				modelIdentityKey := betweenchannel.CanonicalModelIdentityKey(
					target.Manufacturer,
					target.Model,
				)
				releaseTarget, targetErr := q.GetRolloutLaneReleaseTargetByModel(
					txCtx,
					sqlc.GetRolloutLaneReleaseTargetByModelParams{
						ReleaseSetID:     releaseSetID,
						OrgID:            req.OrgID,
						ModelIdentityKey: modelIdentityKey,
					},
				)
				if targetErr != nil {
					return nil, targetErr
				}
				laneModelID := uuid.New()
				createdLaneModelID = laneModelID
				if _, createErr = q.CreateRolloutLaneModelDeclaration(
					txCtx,
					sqlc.CreateRolloutLaneModelDeclarationParams{
						LaneModelID:            laneModelID,
						LaneID:                 req.ID,
						OrgID:                  req.OrgID,
						ModelIdentityKey:       modelIdentityKey,
						Manufacturer:           target.Manufacturer,
						Model:                  target.Model,
						CurrentChannelID:       channelID,
						CurrentReleaseSetID:    releaseSetID,
						CurrentReleaseTargetID: releaseTarget.ID,
					},
				); createErr != nil {
					return nil, createErr
				}
				if _, createErr = q.CreateRolloutLaneModelChannel(
					txCtx,
					sqlc.CreateRolloutLaneModelChannelParams{
						LaneModelID:     laneModelID,
						LaneID:          req.ID,
						OrgID:           req.OrgID,
						ChannelID:       channelID,
						ReleaseSetID:    releaseSetID,
						ReleaseTargetID: releaseTarget.ID,
					},
				); createErr != nil {
					return nil, createErr
				}
				deviceIDs := candidateDeviceIDs(candidates)
				if len(deviceIDs) > 0 {
					bindings, bindingErr := q.CreateRolloutLaneModelBindings(
						txCtx,
						sqlc.CreateRolloutLaneModelBindingsParams{
							LaneID:           req.ID,
							LaneModelID:      laneModelID,
							ChannelID:        channelID,
							OrgID:            req.OrgID,
							DeviceIds:        deviceIDs,
							ModelIdentityKey: sql.NullString{String: modelIdentityKey, Valid: true},
						},
					)
					if bindingErr != nil {
						return nil, bindingErr
					}
					if len(bindings) != len(deviceIDs) {
						return nil, betweenchannel.ErrCompatibility
					}
				}
			}
			if topologyEnabled {
				if createErr = recordModelOperation(txCtx, q, modelOperationAuditRecord[
					createModelDeclarationRequestedPayload,
					createModelDeclarationAppliedPayload,
				]{
					OperationID: req.ChangeID, OrgID: req.OrgID, LaneID: req.ID,
					LaneModelID: createdLaneModelID, Operation: modelOperationCreateDeclaration,
					IdempotencyKey: req.IdempotencyKey, Fingerprint: req.RequestFingerprint,
					ExpectedRevision: 0, ResultingRevision: 1,
					Reason:      "Initial rollout lane model declaration",
					ActorUserID: req.ActorUserID, ActorType: req.ActorType,
					ActorCredentialID: req.ActorCredentialID,
					Requested: createModelDeclarationRequestedPayload{
						LaneID: req.ID.String(), LaneModelID: createdLaneModelID.String(),
						ExpectedRevision: 0,
					},
					Applied: createModelDeclarationAppliedPayload{
						LaneModelID: createdLaneModelID.String(), ResultingRevision: 1,
						Added: req.DeviceIdentifiers,
					},
				}); createErr != nil {
					return nil, createErr
				}
			}
			authority, createErr := q.CreateChannelFirmwareAuthority(
				txCtx,
				sqlc.CreateChannelFirmwareAuthorityParams{
					ID:                 uuid.New(),
					OrgID:              req.OrgID,
					AuthorityType:      "rollout_lane_initial",
					AuthorityReference: req.ID.String(),
					CreatedByUserID:    req.ActorUserID,
				},
			)
			if createErr != nil {
				return nil, createErr
			}
			enforcementCount, createErr := q.CreateInitialRolloutLaneEnforcements(
				txCtx,
				sqlc.CreateInitialRolloutLaneEnforcementsParams{
					LaneID:            req.ID,
					ReleaseSetID:      releaseSetID,
					AuthorityID:       authority.ID,
					AuthorityRevision: authority.Revision,
					OrgID:             req.OrgID,
					DeviceIdentifiers: req.DeviceIdentifiers,
				},
			)
			if createErr != nil {
				return nil, createErr
			}
			if enforcementCount != int64(len(req.DeviceIdentifiers)) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			if len(sourceLaneIDs) > 0 {
				revisions, bumpErr := q.BumpRolloutLaneMembershipRevisions(
					txCtx,
					sqlc.BumpRolloutLaneMembershipRevisionsParams{
						OrgID:   req.OrgID,
						LaneIds: sourceLaneIDs,
					},
				)
				if bumpErr != nil {
					return nil, bumpErr
				}
				if len(revisions) != len(sourceLaneIDs) {
					return nil, betweenchannel.ErrLaneConflict
				}
				if topologyEnabled && len(sourceModelIDs) > 0 {
					modelRevisions, modelErr := q.BumpRolloutLaneModelRevisions(
						txCtx,
						sqlc.BumpRolloutLaneModelRevisionsParams{
							OrgID:        req.OrgID,
							LaneModelIds: sourceModelIDs,
						},
					)
					if modelErr != nil || len(modelRevisions) != len(sourceModelIDs) {
						return nil, betweenchannel.ErrDeclarationConflict
					}
				}
				requestedJSON, appliedJSON, marshalErr := laneCreateMembershipAuditJSON(
					req,
					preview,
				)
				if marshalErr != nil {
					return nil, marshalErr
				}
				if _, createErr = q.CreateRolloutLaneMembershipChange(
					txCtx,
					sqlc.CreateRolloutLaneMembershipChangeParams{
						ChangeID:           req.ChangeID,
						OrgID:              req.OrgID,
						TargetLaneID:       req.ID,
						AuthorityID:        uuid.NullUUID{UUID: authority.ID, Valid: true},
						IdempotencyKey:     req.IdempotencyKey,
						RequestFingerprint: req.RequestFingerprint,
						Requested:          requestedJSON,
						Applied:            appliedJSON,
						Reason:             "Initial rollout lane membership reassignment",
						ActorUserID:        req.ActorUserID,
						ActorType:          persistedActorType(req.ActorType),
						ActorCredentialID:  ptrToNullString(req.ActorCredentialID),
					},
				); createErr != nil {
					return nil, createErr
				}
			}
			return loadRolloutLane(txCtx, q, laneRow, true, nil)
		},
	)
	if err != nil {
		if replay, replayErr := s.replayLaneCreate(ctx, req); replayErr == nil {
			return replay, nil
		} else if errors.Is(replayErr, betweenchannel.ErrIdempotencyConflict) {
			return nil, replayErr
		}
		if isUniqueViolationOn(err, "uq_rollout_lane_label") {
			return nil, betweenchannel.ErrLaneConflict
		}
		if isUniqueViolationOn(err, "uq_rollout_lane_idempotency") {
			return nil, betweenchannel.ErrIdempotencyConflict
		}
		return nil, fmt.Errorf("create rollout lane: %w", err)
	}
	lane, ok := result.(*betweenchannel.Lane)
	if !ok {
		return nil, fmt.Errorf("create rollout lane: unexpected result %T", result)
	}
	return lane, nil
}

func (s *SQLRolloutLaneStore) replayLaneCreate(
	ctx context.Context,
	req betweenchannel.CreateLaneRequest,
) (*betweenchannel.Lane, error) {
	q := s.GetQueries(ctx)
	existing, err := q.GetRolloutLaneByIdempotencyKey(
		ctx,
		sqlc.GetRolloutLaneByIdempotencyKeyParams{
			OrgID:          req.OrgID,
			IdempotencyKey: req.IdempotencyKey,
		},
	)
	if err != nil {
		return nil, err
	}
	if existing.DeletedAt.Valid || existing.CreateFingerprint != req.RequestFingerprint {
		return nil, betweenchannel.ErrIdempotencyConflict
	}
	return loadRolloutLane(ctx, q, existing, true, nil)
}

func (s *SQLRolloutLaneStore) GetLane(
	ctx context.Context,
	orgID int64,
	laneID uuid.UUID,
	includeFirmwareConvergenceMembers bool,
	firmwareConvergenceMembersUpdatedAfter *time.Time,
) (*betweenchannel.Lane, error) {
	row, err := s.GetQueries(ctx).GetRolloutLane(
		ctx,
		sqlc.GetRolloutLaneParams{LaneID: laneID, OrgID: orgID},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, betweenchannel.ErrLaneNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get rollout lane: %w", err)
	}
	lane, err := loadRolloutLane(
		ctx,
		s.GetQueries(ctx),
		row,
		includeFirmwareConvergenceMembers,
		firmwareConvergenceMembersUpdatedAfter,
	)
	if err != nil {
		return nil, fmt.Errorf("load rollout lane: %w", err)
	}
	return lane, nil
}

func (s *SQLRolloutLaneStore) GetLaneForRollout(
	ctx context.Context,
	orgID int64,
	rolloutID uuid.UUID,
) (*betweenchannel.Lane, error) {
	q := s.GetQueries(ctx)
	row, err := q.GetRolloutLaneForRollout(
		ctx,
		sqlc.GetRolloutLaneForRolloutParams{
			RolloutID: uuid.NullUUID{UUID: rolloutID, Valid: true},
			OrgID:     orgID,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, betweenchannel.ErrLaneNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get rollout lane for rollout: %w", err)
	}
	lane, err := loadRolloutLane(ctx, q, row, false, nil)
	if err != nil {
		return nil, fmt.Errorf("load rollout lane for rollout: %w", err)
	}
	return lane, nil
}

func (s *SQLRolloutLaneStore) ListLanes(
	ctx context.Context,
	orgID int64,
	activeFirmwareConvergenceOnly bool,
) ([]betweenchannel.Lane, error) {
	q := s.GetQueries(ctx)
	var (
		rows []sqlc.RolloutLane
		err  error
	)
	if activeFirmwareConvergenceOnly {
		rows, err = q.ListActiveFirmwareConvergenceRolloutLanes(ctx, orgID)
	} else {
		rows, err = q.ListRolloutLanes(ctx, orgID)
	}
	if err != nil {
		return nil, fmt.Errorf("list rollout lanes: %w", err)
	}
	topologyEnabled, err := rolloutLaneTopologyEnabled(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("load rollout lane topology cutover: %w", err)
	}
	laneIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		laneIDs = append(laneIDs, row.ID)
	}
	memberCountRows, err := q.CountRolloutLaneMembersByLaneIDs(
		ctx,
		sqlc.CountRolloutLaneMembersByLaneIDsParams{
			OrgID:   orgID,
			LaneIds: laneIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("count rollout lane members: %w", err)
	}
	memberCountByLane := make(map[uuid.UUID]int64, len(memberCountRows))
	for _, count := range memberCountRows {
		memberCountByLane[count.LaneID] = count.MemberCount
	}
	convergenceStatusByLane := make(
		map[uuid.UUID]betweenchannel.FirmwareConvergenceStatus,
		len(rows),
	)
	if activeFirmwareConvergenceOnly {
		statusRows, statusErr := q.ListActiveRolloutLaneFirmwareConvergenceStatuses(ctx, orgID)
		if statusErr != nil {
			return nil, fmt.Errorf("list active rollout lane firmware convergence statuses: %w", statusErr)
		}
		for _, status := range statusRows {
			convergenceStatusByLane[status.LaneID] = firmwareConvergenceStatus(firmwareConvergenceCounts{
				Total:     status.TotalCount,
				Pending:   status.PendingCount,
				Updating:  status.UpdatingCount,
				Verifying: status.VerifyingCount,
				Confirmed: status.ConfirmedCount,
				Attention: status.AttentionCount,
			})
		}
	} else {
		statusRows, statusErr := q.ListRolloutLaneFirmwareConvergenceStatuses(ctx, orgID)
		if statusErr != nil {
			return nil, fmt.Errorf("list rollout lane firmware convergence statuses: %w", statusErr)
		}
		for _, status := range statusRows {
			convergenceStatusByLane[status.LaneID] = firmwareConvergenceStatus(firmwareConvergenceCounts{
				Total:     status.TotalCount,
				Pending:   status.PendingCount,
				Updating:  status.UpdatingCount,
				Verifying: status.VerifyingCount,
				Confirmed: status.ConfirmedCount,
				Attention: status.AttentionCount,
			})
		}
	}
	channelRows, err := q.ListRolloutLaneChannelDetailsByLaneIDs(
		ctx,
		sqlc.ListRolloutLaneChannelDetailsByLaneIDsParams{OrgID: orgID, LaneIds: laneIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("list rollout lane channels: %w", err)
	}
	channelsByLane := make(map[uuid.UUID][]betweenchannel.LaneChannel, len(rows))
	for _, laneID := range laneIDs {
		channelsByLane[laneID] = make([]betweenchannel.LaneChannel, 0)
	}
	for _, channel := range channelRows {
		var rolloutID *uuid.UUID
		if channel.RolloutID.Valid {
			value := channel.RolloutID.UUID
			rolloutID = &value
		}
		channelsByLane[channel.LaneID] = append(channelsByLane[channel.LaneID], betweenchannel.LaneChannel{
			ChannelID: channel.ChannelID, ReleaseSetID: channel.ReleaseSetID,
			Position: channel.Position, RolloutID: rolloutID, CreatedAt: channel.CreatedAt,
		})
	}
	modelsByLane := make(map[uuid.UUID][]betweenchannel.LaneModel)
	if topologyEnabled {
		modelsByLane, err = loadRolloutLaneModelsByLaneIDs(ctx, q, laneIDs, orgID)
		if err != nil {
			return nil, fmt.Errorf("list rollout lane models: %w", err)
		}
	}
	result := make([]betweenchannel.Lane, 0, len(rows))
	for _, row := range rows {
		memberCount := memberCountByLane[row.ID]
		lane, loadErr := loadRolloutLaneWithFirmwareConvergenceStatus(
			ctx,
			q,
			row,
			convergenceStatusByLane[row.ID],
			&rolloutLaneAggregate{
				MemberCount: memberCount, Channels: channelsByLane[row.ID],
				TopologyEnabled: topologyEnabled, Models: modelsByLane[row.ID],
			},
		)
		if loadErr != nil {
			return nil, fmt.Errorf("load rollout lane: %w", loadErr)
		}
		result = append(result, *lane)
	}
	return result, nil
}

func (s *SQLRolloutLaneStore) ListMembers(
	ctx context.Context,
	req betweenchannel.ListMembersRequest,
) (betweenchannel.ListMembersResult, error) {
	value, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			lanes, lockErr := q.LockRolloutLanes(
				txCtx,
				sqlc.LockRolloutLanesParams{
					OrgID:   req.OrgID,
					LaneIds: []uuid.UUID{req.LaneID},
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if len(lanes) == 0 {
				return nil, betweenchannel.ErrLaneNotFound
			}
			lane := lanes[0]
			if req.ExpectedRevision > 0 && lane.Revision != req.ExpectedRevision {
				return nil, betweenchannel.ErrLaneConflict
			}
			var total int64
			if req.IncludeTotalCount {
				count, countErr := q.CountRolloutLaneMembers(
					txCtx,
					sqlc.CountRolloutLaneMembersParams{
						LaneID: req.LaneID, OrgID: req.OrgID,
						LaneModelID: uuid.NullUUID{UUID: req.LaneModelID, Valid: req.LaneModelID != uuid.Nil},
					},
				)
				if countErr != nil {
					return nil, countErr
				}
				total = count
			}
			rows, listErr := q.ListRolloutLaneMembers(txCtx, sqlc.ListRolloutLaneMembersParams{
				LaneID: req.LaneID, OrgID: req.OrgID,
				LaneModelID:     uuid.NullUUID{UUID: req.LaneModelID, Valid: req.LaneModelID != uuid.Nil},
				AfterIdentifier: req.AfterIdentifier, MemberLimit: req.Limit + 1,
			})
			if listErr != nil {
				return nil, listErr
			}
			result := betweenchannel.ListMembersResult{
				TotalCount: total,
				Revision:   lane.Revision,
			}
			if len(rows) > int(req.Limit) {
				result.NextIdentifier = rows[req.Limit-1].DeviceIdentifier
				rows = rows[:req.Limit]
			}
			result.Members = laneMembersFromListRows(rows)
			return result, nil
		},
	)
	if err != nil {
		return betweenchannel.ListMembersResult{}, err
	}
	result, ok := value.(betweenchannel.ListMembersResult)
	if !ok {
		return betweenchannel.ListMembersResult{}, fmt.Errorf(
			"list rollout lane members: unexpected result %T",
			value,
		)
	}
	return result, nil
}

func (s *SQLRolloutLaneStore) GetAssignments(
	ctx context.Context,
	orgID int64,
	deviceIdentifiers []string,
) ([]betweenchannel.LaneAssignment, error) {
	rows, err := s.GetQueries(ctx).GetRolloutLaneAssignments(
		ctx,
		sqlc.GetRolloutLaneAssignmentsParams{
			OrgID:             orgID,
			DeviceIdentifiers: deviceIdentifiers,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get rollout lane assignments: %w", err)
	}
	assignments := make([]betweenchannel.LaneAssignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, betweenchannel.LaneAssignment{
			DeviceIdentifier: row.DeviceIdentifier,
			LaneID:           row.LaneID,
			LaneLabel:        row.LaneLabel,
		})
	}
	return assignments, nil
}

func (s *SQLRolloutLaneStore) GetTopologyReadiness(
	ctx context.Context,
	orgID int64,
) (betweenchannel.TopologyReadiness, error) {
	return loadTopologyReadiness(ctx, s.GetQueries(ctx), orgID)
}

func (s *SQLRolloutLaneStore) RepairModelBinding(
	ctx context.Context,
	req betweenchannel.RepairModelBindingRequest,
) (betweenchannel.RepairModelBindingResult, error) {
	value, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			if backfillErr := refreshRolloutLaneTopologyBackfill(txCtx, q, req.OrgID); backfillErr != nil {
				return nil, backfillErr
			}
			if replay, replayErr := replayTopologyRepair(txCtx, q, req); replayErr == nil {
				return replay, nil
			} else if !errors.Is(replayErr, sql.ErrNoRows) {
				return nil, replayErr
			}

			declaration, lockErr := q.LockRolloutLaneModelForRepair(
				txCtx,
				sqlc.LockRolloutLaneModelForRepairParams{
					LaneModelID: req.LaneModelID,
					LaneID:      req.LaneID,
					OrgID:       req.OrgID,
				},
			)
			if errors.Is(lockErr, sql.ErrNoRows) {
				return nil, betweenchannel.ErrTopologyRepairConflict
			}
			if lockErr != nil {
				return nil, lockErr
			}
			if declaration.Revision != req.ExpectedRevision {
				if replay, replayErr := replayTopologyRepair(txCtx, q, req); replayErr == nil {
					return replay, nil
				}
				return nil, betweenchannel.ErrLaneConflict
			}

			device, deviceErr := q.LockRolloutLaneModelRepairDevice(
				txCtx,
				sqlc.LockRolloutLaneModelRepairDeviceParams{
					OrgID:            req.OrgID,
					DeviceIdentifier: req.DeviceIdentifier,
				},
			)
			if errors.Is(deviceErr, sql.ErrNoRows) {
				return nil, betweenchannel.ErrTopologyRepairConflict
			}
			if deviceErr != nil {
				return nil, deviceErr
			}
			if device.ChannelID != declaration.CurrentChannelID ||
				device.ModelIdentityKey == "" ||
				device.ModelIdentityKey != declaration.ModelIdentityKey ||
				!device.ModelIdentityObservedAt.Valid {
				return nil, betweenchannel.ErrTopologyRepairConflict
			}

			if _, endErr := q.EndActiveRolloutLaneModelBinding(
				txCtx,
				sqlc.EndActiveRolloutLaneModelBindingParams{
					OperationID: uuid.NullUUID{UUID: req.OperationID, Valid: true},
					LaneID:      req.LaneID,
					OrgID:       req.OrgID,
					DeviceID:    device.DeviceID,
				},
			); endErr != nil {
				return nil, endErr
			}
			binding, createErr := q.CreateRolloutLaneModelBindingRepair(
				txCtx,
				sqlc.CreateRolloutLaneModelBindingRepairParams{
					BindingID:               req.BindingID,
					LaneID:                  req.LaneID,
					LaneModelID:             req.LaneModelID,
					OrgID:                   req.OrgID,
					DeviceID:                device.DeviceID,
					ChannelID:               device.ChannelID,
					ModelIdentityKey:        device.ModelIdentityKey,
					ModelIdentityObservedAt: device.ModelIdentityObservedAt,
				},
			)
			if createErr != nil {
				return nil, createErr
			}
			resultingRevision, bumpErr := q.BumpRolloutLaneModelRevision(
				txCtx,
				sqlc.BumpRolloutLaneModelRevisionParams{
					LaneModelID:      req.LaneModelID,
					LaneID:           req.LaneID,
					OrgID:            req.OrgID,
					ExpectedRevision: req.ExpectedRevision,
				},
			)
			if errors.Is(bumpErr, sql.ErrNoRows) {
				return nil, betweenchannel.ErrLaneConflict
			}
			if bumpErr != nil {
				return nil, bumpErr
			}

			requested, _ := json.Marshal(map[string]any{
				"lane_id":           req.LaneID.String(),
				"lane_model_id":     req.LaneModelID.String(),
				"device_identifier": req.DeviceIdentifier,
			})
			applied, _ := json.Marshal(map[string]any{
				"binding_id": binding.ID.String(),
				"channel_id": binding.ChannelID,
			})
			if _, operationErr := q.CreateRolloutLaneTopologyAdminOperation(
				txCtx,
				sqlc.CreateRolloutLaneTopologyAdminOperationParams{
					OperationID:        req.OperationID,
					OrgID:              req.OrgID,
					Operation:          "repair_binding",
					LaneID:             uuid.NullUUID{UUID: req.LaneID, Valid: true},
					LaneModelID:        uuid.NullUUID{UUID: req.LaneModelID, Valid: true},
					DeviceID:           sql.NullInt64{Int64: device.DeviceID, Valid: true},
					IdempotencyKey:     req.IdempotencyKey,
					RequestFingerprint: req.RequestFingerprint,
					ExpectedRevision:   req.ExpectedRevision,
					ResultingRevision:  resultingRevision,
					Reason:             req.Reason,
					Requested:          requested,
					Applied:            applied,
					ActorUserID:        req.ActorUserID,
					ActorType:          persistedActorType(req.ActorType),
					ActorCredentialID:  ptrToNullString(req.ActorCredentialID),
				},
			); operationErr != nil {
				return nil, operationErr
			}
			readiness, readinessErr := loadTopologyReadiness(txCtx, q, req.OrgID)
			if readinessErr != nil {
				return nil, readinessErr
			}
			return betweenchannel.RepairModelBindingResult{
				BindingID:         binding.ID,
				ResultingRevision: resultingRevision,
				Readiness:         readiness,
			}, nil
		},
	)
	if err != nil {
		if isUniqueViolationOn(err, "uq_rollout_lane_topology_admin_operation_key") {
			return replayTopologyRepair(ctx, s.GetQueries(ctx), req)
		}
		return betweenchannel.RepairModelBindingResult{}, fmt.Errorf(
			"repair rollout lane model binding: %w",
			err,
		)
	}
	result, ok := value.(betweenchannel.RepairModelBindingResult)
	if !ok {
		return betweenchannel.RepairModelBindingResult{}, fmt.Errorf(
			"repair rollout lane model binding: unexpected result %T",
			value,
		)
	}
	return result, nil
}

func (s *SQLRolloutLaneStore) EnableTopology(
	ctx context.Context,
	req betweenchannel.EnableTopologyRequest,
) (betweenchannel.EnableTopologyResult, error) {
	value, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			if backfillErr := refreshRolloutLaneTopologyBackfill(txCtx, q, req.OrgID); backfillErr != nil {
				return nil, backfillErr
			}
			if replay, replayErr := replayTopologyEnable(txCtx, q, req); replayErr == nil {
				return replay, nil
			} else if !errors.Is(replayErr, sql.ErrNoRows) {
				return nil, replayErr
			}

			cutover, lockErr := q.LockRolloutLaneTopologyCutover(txCtx, req.OrgID)
			if lockErr != nil {
				return nil, lockErr
			}
			if cutover.Enabled {
				return nil, betweenchannel.ErrTopologyAlreadyEnabled
			}
			if cutover.Revision != req.ExpectedRevision {
				return nil, betweenchannel.ErrLaneConflict
			}
			anomalyCount, countErr := q.CountRolloutLaneTopologyAnomalies(txCtx, req.OrgID)
			if countErr != nil {
				return nil, countErr
			}
			activeLegacyCount, activeErr := q.CountActiveLegacyRolloutLaneWork(txCtx, req.OrgID)
			if activeErr != nil {
				return nil, activeErr
			}
			if anomalyCount != 0 || activeLegacyCount != 0 {
				return nil, betweenchannel.ErrTopologyNotReady
			}

			enabled, enableErr := q.EnableRolloutLaneModelTopology(
				txCtx,
				sqlc.EnableRolloutLaneModelTopologyParams{
					EnabledByUserID: sql.NullInt64{
						Int64: req.ActorUserID,
						Valid: true,
					},
					EnabledActorType: sql.NullString{
						String: persistedActorType(req.ActorType),
						Valid:  true,
					},
					EnabledActorCredentialID: ptrToNullString(req.ActorCredentialID),
					EnableReason: sql.NullString{
						String: req.Reason,
						Valid:  true,
					},
					EnableIdempotencyKey: sql.NullString{
						String: req.IdempotencyKey,
						Valid:  true,
					},
					OrgID:            req.OrgID,
					ExpectedRevision: req.ExpectedRevision,
				},
			)
			if errors.Is(enableErr, sql.ErrNoRows) {
				return nil, betweenchannel.ErrLaneConflict
			}
			if enableErr != nil {
				return nil, enableErr
			}
			requested, _ := json.Marshal(map[string]any{"org_id": req.OrgID})
			applied, _ := json.Marshal(map[string]any{"enabled": true})
			if _, operationErr := q.CreateRolloutLaneTopologyAdminOperation(
				txCtx,
				sqlc.CreateRolloutLaneTopologyAdminOperationParams{
					OperationID:        req.OperationID,
					OrgID:              req.OrgID,
					Operation:          "enable",
					IdempotencyKey:     req.IdempotencyKey,
					RequestFingerprint: req.RequestFingerprint,
					ExpectedRevision:   req.ExpectedRevision,
					ResultingRevision:  enabled.Revision,
					Reason:             req.Reason,
					Requested:          requested,
					Applied:            applied,
					ActorUserID:        req.ActorUserID,
					ActorType:          persistedActorType(req.ActorType),
					ActorCredentialID:  ptrToNullString(req.ActorCredentialID),
				},
			); operationErr != nil {
				return nil, operationErr
			}
			readiness, readinessErr := loadTopologyReadiness(txCtx, q, req.OrgID)
			if readinessErr != nil {
				return nil, readinessErr
			}
			return betweenchannel.EnableTopologyResult{Readiness: readiness}, nil
		},
	)
	if err != nil {
		if isUniqueViolationOn(err, "uq_rollout_lane_topology_admin_operation_key") {
			return replayTopologyEnable(ctx, s.GetQueries(ctx), req)
		}
		return betweenchannel.EnableTopologyResult{}, fmt.Errorf(
			"enable rollout lane model topology: %w",
			err,
		)
	}
	result, ok := value.(betweenchannel.EnableTopologyResult)
	if !ok {
		return betweenchannel.EnableTopologyResult{}, fmt.Errorf(
			"enable rollout lane model topology: unexpected result %T",
			value,
		)
	}
	return result, nil
}

func (s *SQLRolloutLaneStore) PreviewMembershipChange(
	ctx context.Context,
	req betweenchannel.PreviewMembershipChangeRequest,
) (betweenchannel.MembershipChangePreview, error) {
	q := s.GetQueries(ctx)
	topologyEnabled, err := rolloutLaneTopologyEnabled(ctx, q, req.OrgID)
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	if topologyEnabled {
		lane, laneErr := s.GetLane(ctx, req.OrgID, req.LaneID, false, nil)
		if laneErr != nil {
			return betweenchannel.MembershipChangePreview{}, laneErr
		}
		if !lane.ScalarProjectionAvailable || len(lane.Models) != 1 {
			return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrScalarProjectionUnavailable
		}
		return s.PreviewModelMembershipChange(ctx, betweenchannel.PreviewModelMembershipChangeRequest{
			OrgID: req.OrgID, LaneID: req.LaneID, LaneModelID: lane.Models[0].ID,
			AddIdentifiers: req.AddIdentifiers, RemoveIdentifiers: req.RemoveIdentifiers,
		})
	}
	if _, err := q.GetRolloutLane(
		ctx,
		sqlc.GetRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID},
	); errors.Is(err, sql.ErrNoRows) {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrLaneNotFound
	} else if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	return previewMembershipChange(ctx, q, req)
}

func (s *SQLRolloutLaneStore) UpdateMembership(
	ctx context.Context,
	req betweenchannel.UpdateMembershipRequest,
) (betweenchannel.UpdateMembershipResult, error) {
	topologyEnabled, topologyErr := rolloutLaneTopologyEnabled(ctx, s.GetQueries(ctx), req.OrgID)
	if topologyErr != nil {
		return betweenchannel.UpdateMembershipResult{}, topologyErr
	}
	if topologyEnabled {
		lane, laneErr := s.GetLane(ctx, req.OrgID, req.LaneID, false, nil)
		if laneErr != nil {
			return betweenchannel.UpdateMembershipResult{}, laneErr
		}
		if !lane.ScalarProjectionAvailable || len(lane.Models) != 1 {
			return betweenchannel.UpdateMembershipResult{}, betweenchannel.ErrScalarProjectionUnavailable
		}
		return s.UpdateModelMembership(ctx, betweenchannel.UpdateModelMembershipRequest{
			OperationID: req.ChangeID, OrgID: req.OrgID, LaneID: req.LaneID,
			LaneModelID: lane.Models[0].ID, ExpectedRevision: req.ExpectedRevision,
			AddIdentifiers: req.AddIdentifiers, RemoveIdentifiers: req.RemoveIdentifiers,
			ConfirmFirmware: req.ConfirmFirmware, ConfirmReassign: req.ConfirmReassign,
			IdempotencyKey: req.IdempotencyKey, RequestFingerprint: req.RequestFingerprint,
			Reason: req.Reason, ActorUserID: req.ActorUserID, ActorType: req.ActorType,
			ActorCredentialID: req.ActorCredentialID,
		})
	}
	result, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			replayed, replayErr := replayRolloutLaneMembershipChange(txCtx, q, req)
			if replayErr == nil {
				return replayed, nil
			}
			if !errors.Is(replayErr, sql.ErrNoRows) {
				return nil, replayErr
			}

			identifiers := append(
				append([]string(nil), req.AddIdentifiers...),
				req.RemoveIdentifiers...,
			)
			initialCandidates, candidateErr := q.ListRolloutLaneMembershipCandidates(
				txCtx,
				sqlc.ListRolloutLaneMembershipCandidatesParams{
					OrgID:             req.OrgID,
					DeviceIdentifiers: identifiers,
				},
			)
			if candidateErr != nil {
				return nil, candidateErr
			}
			if len(initialCandidates) != len(identifiers) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			affectedLaneIDs, laneErr := affectedMembershipLaneIDs(
				req.LaneID,
				req.AddIdentifiers,
				initialCandidates,
			)
			if laneErr != nil {
				return nil, laneErr
			}
			lockedLanes, lockErr := q.LockRolloutLanes(
				txCtx,
				sqlc.LockRolloutLanesParams{
					OrgID:   req.OrgID,
					LaneIds: affectedLaneIDs,
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if len(lockedLanes) != len(affectedLaneIDs) {
				return nil, betweenchannel.ErrLaneConflict
			}
			var targetLane sqlc.RolloutLane
			for _, lane := range lockedLanes {
				if lane.ID == req.LaneID {
					targetLane = lane
					break
				}
			}
			if targetLane.ID == uuid.Nil {
				return nil, betweenchannel.ErrLaneNotFound
			}
			if targetLane.Revision != req.ExpectedRevision {
				return nil, betweenchannel.ErrLaneConflict
			}

			channelIDs, listErr := rolloutLaneChannelIDs(
				txCtx,
				q,
				req.OrgID,
				affectedLaneIDs,
			)
			if listErr != nil {
				return nil, listErr
			}
			lockedChannels, lockErr := q.LockBetweenChannelChannels(
				txCtx,
				sqlc.LockBetweenChannelChannelsParams{
					OrgID:      req.OrgID,
					ChannelIds: channelIDs,
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if len(lockedChannels) != len(channelIDs) {
				return nil, betweenchannel.ErrLaneConflict
			}

			deviceIDs := candidateDeviceIDs(initialCandidates)
			lockedDevices, lockErr := q.LockBetweenChannelDevices(
				txCtx,
				sqlc.LockBetweenChannelDevicesParams{
					OrgID:     req.OrgID,
					DeviceIds: deviceIDs,
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if len(lockedDevices) != len(deviceIDs) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			revalidated, candidateErr := q.ListRolloutLaneMembershipCandidates(
				txCtx,
				sqlc.ListRolloutLaneMembershipCandidatesParams{
					OrgID:             req.OrgID,
					DeviceIdentifiers: identifiers,
				},
			)
			if candidateErr != nil {
				return nil, candidateErr
			}
			revalidatedLaneIDs, laneErr := affectedMembershipLaneIDs(
				req.LaneID,
				req.AddIdentifiers,
				revalidated,
			)
			if laneErr != nil || !slices.Equal(affectedLaneIDs, revalidatedLaneIDs) {
				return nil, betweenchannel.ErrMembershipConflict
			}

			if _, lockErr = q.LockRolloutLaneManagementAuthorities(
				txCtx,
				sqlc.LockRolloutLaneManagementAuthoritiesParams{
					OrgID:   req.OrgID,
					LaneIds: affectedLaneIDs,
				},
			); lockErr != nil {
				return nil, lockErr
			}
			if _, lockErr = q.LockRolloutLaneOwnedRolloutMembers(
				txCtx,
				sqlc.LockRolloutLaneOwnedRolloutMembersParams{
					OrgID:   req.OrgID,
					LaneIds: affectedLaneIDs,
				},
			); lockErr != nil {
				return nil, lockErr
			}
			active, checkErr := q.HasActiveRolloutLaneManagementWork(
				txCtx,
				sqlc.HasActiveRolloutLaneManagementWorkParams{
					OrgID:   req.OrgID,
					LaneIds: affectedLaneIDs,
				},
			)
			if checkErr != nil {
				return nil, checkErr
			}
			if active.Valid && active.Bool {
				return nil, betweenchannel.ErrLaneWorkActive
			}

			preview, previewErr := previewMembershipChangeWithCandidates(
				txCtx,
				q,
				betweenchannel.PreviewMembershipChangeRequest{
					OrgID:             req.OrgID,
					LaneID:            req.LaneID,
					AddIdentifiers:    req.AddIdentifiers,
					RemoveIdentifiers: req.RemoveIdentifiers,
				},
				revalidated,
			)
			if previewErr != nil {
				return nil, previewErr
			}
			if preview.RequiresFirmwareConfirmation && !req.ConfirmFirmware {
				return nil, betweenchannel.ErrFirmwareConfirmationRequired
			}
			if preview.RequiresReassignConfirmation && !req.ConfirmReassign {
				return nil, betweenchannel.ErrReassignmentConfirmationRequired
			}

			candidateByIdentifier := membershipCandidatesByIdentifier(revalidated)
			reassignmentIdentifiers := make([]string, 0, len(preview.Reassignments))
			for _, reassignment := range preview.Reassignments {
				reassignmentIdentifiers = append(
					reassignmentIdentifiers,
					reassignment.DeviceIdentifier,
				)
			}
			removedIdentifiers := append(
				append([]string(nil), req.RemoveIdentifiers...),
				reassignmentIdentifiers...,
			)
			removedDeviceIDs := candidateIDsForIdentifiers(
				candidateByIdentifier,
				removedIdentifiers,
			)
			if len(removedDeviceIDs) > 0 {
				removed, removeErr := q.RemoveRolloutLaneMembershipDevices(
					txCtx,
					sqlc.RemoveRolloutLaneMembershipDevicesParams{
						OrgID:     req.OrgID,
						LaneIds:   affectedLaneIDs,
						DeviceIds: removedDeviceIDs,
					},
				)
				if removeErr != nil {
					return nil, removeErr
				}
				if len(removed) != len(removedDeviceIDs) {
					return nil, betweenchannel.ErrMembershipConflict
				}
			}
			addedDeviceIDs := candidateIDsForIdentifiers(
				candidateByIdentifier,
				req.AddIdentifiers,
			)
			if len(addedDeviceIDs) > 0 {
				added, addErr := q.AddRolloutLaneMembershipDevices(
					txCtx,
					sqlc.AddRolloutLaneMembershipDevicesParams{
						DeviceIds: addedDeviceIDs,
						LaneID:    req.LaneID,
						OrgID:     req.OrgID,
					},
				)
				if addErr != nil {
					return nil, addErr
				}
				if len(added) != len(addedDeviceIDs) {
					return nil, betweenchannel.ErrMembershipConflict
				}
			}

			var authorityID uuid.NullUUID
			if len(addedDeviceIDs) > 0 {
				authority, createErr := q.CreateChannelFirmwareAuthority(
					txCtx,
					sqlc.CreateChannelFirmwareAuthorityParams{
						ID:                 uuid.New(),
						OrgID:              req.OrgID,
						AuthorityType:      "rollout_lane_membership",
						AuthorityReference: req.ChangeID.String(),
						CreatedByUserID:    req.ActorUserID,
					},
				)
				if createErr != nil {
					return nil, createErr
				}
				authorityID = uuid.NullUUID{UUID: authority.ID, Valid: true}
				enforcementCount, createErr := q.CreateRolloutLaneMembershipEnforcements(
					txCtx,
					sqlc.CreateRolloutLaneMembershipEnforcementsParams{
						ChangeID:          req.ChangeID,
						DeviceIds:         addedDeviceIDs,
						AuthorityID:       authority.ID,
						AuthorityRevision: authority.Revision,
						LaneID:            req.LaneID,
						OrgID:             req.OrgID,
					},
				)
				if createErr != nil {
					return nil, createErr
				}
				if enforcementCount != int64(len(addedDeviceIDs)) {
					return nil, betweenchannel.ErrCompatibility
				}
			}

			revisions, bumpErr := q.BumpRolloutLaneMembershipRevisions(
				txCtx,
				sqlc.BumpRolloutLaneMembershipRevisionsParams{
					OrgID:   req.OrgID,
					LaneIds: affectedLaneIDs,
				},
			)
			if bumpErr != nil {
				return nil, bumpErr
			}
			if len(revisions) != len(affectedLaneIDs) {
				return nil, betweenchannel.ErrLaneConflict
			}

			requestedJSON, appliedJSON, marshalErr := membershipChangeAuditJSON(req, preview)
			if marshalErr != nil {
				return nil, marshalErr
			}
			if _, createErr := q.CreateRolloutLaneMembershipChange(
				txCtx,
				sqlc.CreateRolloutLaneMembershipChangeParams{
					ChangeID:           req.ChangeID,
					OrgID:              req.OrgID,
					TargetLaneID:       req.LaneID,
					AuthorityID:        authorityID,
					IdempotencyKey:     req.IdempotencyKey,
					RequestFingerprint: req.RequestFingerprint,
					Requested:          requestedJSON,
					Applied:            appliedJSON,
					Reason:             req.Reason,
					ActorUserID:        req.ActorUserID,
					ActorType:          persistedActorType(req.ActorType),
					ActorCredentialID:  ptrToNullString(req.ActorCredentialID),
				},
			); createErr != nil {
				return nil, createErr
			}

			laneRow, getErr := q.GetRolloutLane(
				txCtx,
				sqlc.GetRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID},
			)
			if getErr != nil {
				return nil, getErr
			}
			lane, loadErr := loadRolloutLane(txCtx, q, laneRow, false, nil)
			if loadErr != nil {
				return nil, loadErr
			}
			addedMembers, memberErr := laneMembersForIdentifiers(
				txCtx,
				q,
				req.OrgID,
				req.LaneID,
				req.AddIdentifiers,
			)
			if memberErr != nil {
				return nil, memberErr
			}
			return &betweenchannel.UpdateMembershipResult{
				Lane:              lane,
				TransitionMembers: addedMembers,
			}, nil
		},
	)
	if err != nil {
		if replayed, replayErr := replayRolloutLaneMembershipChange(
			ctx,
			s.GetQueries(ctx),
			req,
		); replayErr == nil {
			return *replayed, nil
		} else if errors.Is(replayErr, betweenchannel.ErrIdempotencyConflict) {
			return betweenchannel.UpdateMembershipResult{}, replayErr
		}
		if isUniqueViolationOn(err, "uq_rollout_lane_membership_change_idempotency") {
			return betweenchannel.UpdateMembershipResult{}, betweenchannel.ErrIdempotencyConflict
		}
		if isUniqueViolationOn(err, "uq_channel_firmware_enforcement_active_device") {
			return betweenchannel.UpdateMembershipResult{}, betweenchannel.ErrLaneWorkActive
		}
		return betweenchannel.UpdateMembershipResult{}, fmt.Errorf(
			"update rollout lane membership: %w",
			err,
		)
	}
	updated, ok := result.(*betweenchannel.UpdateMembershipResult)
	if !ok {
		return betweenchannel.UpdateMembershipResult{}, fmt.Errorf(
			"update rollout lane membership: unexpected result %T",
			result,
		)
	}
	return *updated, nil
}

func rolloutLaneChannelIDs(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	laneIDs []uuid.UUID,
) ([]int64, error) {
	channels, err := q.ListRolloutLaneChannelsByLaneIDs(
		ctx,
		sqlc.ListRolloutLaneChannelsByLaneIDsParams{
			LaneIds: laneIDs,
			OrgID:   orgID,
		},
	)
	if err != nil {
		return nil, err
	}
	channelIDs := make([]int64, 0, len(channels))
	for _, channelRow := range channels {
		channelIDs = append(channelIDs, channelRow.ChannelID)
	}
	return channelIDs, nil
}

func (s *SQLRolloutLaneStore) DeleteLane(
	ctx context.Context,
	req betweenchannel.DeleteLaneRequest,
) error {
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		q := s.GetQueries(txCtx)
		lane, lockErr := q.LockRolloutLaneForArchive(
			txCtx,
			sqlc.LockRolloutLaneForArchiveParams{
				LaneID: req.LaneID,
				OrgID:  req.OrgID,
			},
		)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return betweenchannel.ErrLaneNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if lane.DeletedAt.Valid {
			if !lane.DeleteIdempotencyKey.Valid ||
				lane.DeleteIdempotencyKey.String != req.IdempotencyKey {
				return betweenchannel.ErrLaneNotFound
			}
			if lane.DeleteFingerprint.Valid &&
				lane.DeleteFingerprint.String == req.RequestFingerprint {
				return nil
			}
			return betweenchannel.ErrIdempotencyConflict
		}
		if lane.Revision != req.ExpectedRevision {
			return betweenchannel.ErrLaneConflict
		}

		laneChannels, listErr := q.ListRolloutLaneChannels(
			txCtx,
			sqlc.ListRolloutLaneChannelsParams{
				LaneID: req.LaneID,
				OrgID:  req.OrgID,
			},
		)
		if listErr != nil {
			return listErr
		}
		channelIDs := make([]int64, 0, len(laneChannels))
		for _, laneChannel := range laneChannels {
			channelIDs = append(channelIDs, laneChannel.ChannelID)
		}
		lockedChannels, lockErr := q.LockBetweenChannelChannels(
			txCtx,
			sqlc.LockBetweenChannelChannelsParams{
				OrgID:      req.OrgID,
				ChannelIds: channelIDs,
			},
		)
		if lockErr != nil {
			return lockErr
		}
		if len(lockedChannels) != len(channelIDs) {
			return betweenchannel.ErrLaneConflict
		}

		memberDeviceIDs, listErr := q.ListRolloutLaneMemberDeviceIDs(
			txCtx,
			sqlc.ListRolloutLaneMemberDeviceIDsParams{
				LaneID: req.LaneID,
				OrgID:  req.OrgID,
			},
		)
		if listErr != nil {
			return listErr
		}
		if len(memberDeviceIDs) > 0 {
			lockedDevices, deviceLockErr := q.LockRolloutLaneDevicesForArchive(
				txCtx,
				sqlc.LockRolloutLaneDevicesForArchiveParams{
					OrgID:     req.OrgID,
					DeviceIds: memberDeviceIDs,
				},
			)
			if deviceLockErr != nil {
				return deviceLockErr
			}
			if len(lockedDevices) != len(memberDeviceIDs) {
				return betweenchannel.ErrMembershipConflict
			}
		}

		initialAuthorities, lockErr := q.LockRolloutLaneInitialAuthorities(
			txCtx,
			sqlc.LockRolloutLaneInitialAuthoritiesParams{
				OrgID:  req.OrgID,
				LaneID: req.LaneID,
			},
		)
		if lockErr != nil {
			return lockErr
		}
		initialActive, checkErr := q.HasActiveRolloutLaneInitialWork(
			txCtx,
			sqlc.HasActiveRolloutLaneInitialWorkParams{
				OrgID:  req.OrgID,
				LaneID: req.LaneID,
			},
		)
		if checkErr != nil {
			return checkErr
		}
		if initialActive {
			return fmt.Errorf(
				"%w: firmware setup or membership enforcement must settle before deletion",
				betweenchannel.ErrLaneWorkActive,
			)
		}
		settlement, checkErr := q.GetRolloutLaneSettlementState(
			txCtx,
			sqlc.GetRolloutLaneSettlementStateParams{
				LaneID: uuid.NullUUID{UUID: req.LaneID, Valid: true},
				OrgID:  req.OrgID,
			},
		)
		if checkErr != nil {
			return checkErr
		}
		if blocker := rolloutLaneSettlementBlocker(settlement); blocker != "" {
			return fmt.Errorf(
				"%w: %s must settle before deletion",
				betweenchannel.ErrLaneWorkActive,
				blocker,
			)
		}
		if _, checkErr = releaseRolloutLaneActiveParentIfSettled(
			txCtx,
			q,
			req.LaneID,
			req.OrgID,
		); checkErr != nil {
			return checkErr
		}

		for _, authority := range initialAuthorities {
			if authority.HaltedAt.Valid {
				continue
			}
			_, haltErr := q.HaltChannelFirmwareAuthority(
				txCtx,
				sqlc.HaltChannelFirmwareAuthorityParams{
					AuthorityID:      authority.ID,
					OrgID:            req.OrgID,
					ExpectedRevision: authority.Revision,
				},
			)
			if errors.Is(haltErr, sql.ErrNoRows) {
				return betweenchannel.ErrLaneConflict
			}
			if haltErr != nil {
				return haltErr
			}
		}
		if _, endErr := q.EndRolloutLaneModelBindingsForArchive(
			txCtx,
			sqlc.EndRolloutLaneModelBindingsForArchiveParams{
				LaneID: req.LaneID,
				OrgID:  req.OrgID,
			},
		); endErr != nil {
			return endErr
		}
		removed, removeErr := q.RemoveRolloutLaneMemberships(
			txCtx,
			sqlc.RemoveRolloutLaneMembershipsParams{
				LaneID: req.LaneID,
				OrgID:  req.OrgID,
			},
		)
		if removeErr != nil {
			return removeErr
		}
		if len(removed) != len(memberDeviceIDs) {
			return betweenchannel.ErrMembershipConflict
		}
		_, archiveErr := q.ArchiveRolloutLane(
			txCtx,
			sqlc.ArchiveRolloutLaneParams{
				DeletedByUserID: sql.NullInt64{Int64: req.ActorUserID, Valid: true},
				DeletedActorType: sql.NullString{
					String: persistedActorType(req.ActorType),
					Valid:  true,
				},
				DeletedActorCredentialID: ptrToNullString(req.ActorCredentialID),
				DeleteReason:             sql.NullString{String: req.Reason, Valid: true},
				DeleteIdempotencyKey:     sql.NullString{String: req.IdempotencyKey, Valid: true},
				DeleteFingerprint:        sql.NullString{String: req.RequestFingerprint, Valid: true},
				LaneID:                   req.LaneID,
				OrgID:                    req.OrgID,
				ExpectedRevision:         req.ExpectedRevision,
			},
		)
		if errors.Is(archiveErr, sql.ErrNoRows) {
			return betweenchannel.ErrLaneConflict
		}
		return archiveErr
	})
	if err != nil {
		if isUniqueViolationOn(err, "uq_rollout_lane_delete_idempotency") {
			return betweenchannel.ErrIdempotencyConflict
		}
		return fmt.Errorf("delete rollout lane: %w", err)
	}
	return nil
}

func (s *SQLRolloutLaneStore) StartRollout(
	ctx context.Context,
	req betweenchannel.StartRolloutRequest,
) (betweenchannel.StartRolloutResult, error) {
	if len(req.ModelPlans) > 0 {
		return s.startModelRollout(ctx, req)
	}
	result, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			laneRow, lockErr := q.LockRolloutLane(
				txCtx,
				sqlc.LockRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID},
			)
			if errors.Is(lockErr, sql.ErrNoRows) {
				return nil, betweenchannel.ErrLaneNotFound
			}
			if lockErr != nil {
				return nil, lockErr
			}

			existing, getErr := q.GetRolloutLaneChannelByStartKey(
				txCtx,
				sqlc.GetRolloutLaneChannelByStartKeyParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
					StartIdempotencyKey: sql.NullString{
						String: req.IdempotencyKey,
						Valid:  true,
					},
				},
			)
			switch {
			case getErr == nil:
				if !existing.StartFingerprint.Valid ||
					existing.StartFingerprint.String != req.RequestFingerprint ||
					!existing.RolloutID.Valid {
					return nil, betweenchannel.ErrIdempotencyConflict
				}
				rolloutRow, rolloutErr := q.GetFirmwareRollout(
					txCtx,
					sqlc.GetFirmwareRolloutParams{
						RolloutID: existing.RolloutID.UUID,
						OrgID:     req.OrgID,
					},
				)
				if rolloutErr != nil {
					return nil, rolloutErr
				}
				loadedRollout, rolloutErr := loadRollout(txCtx, q, rolloutRow)
				if rolloutErr != nil {
					return nil, rolloutErr
				}
				loadedLane, laneErr := loadRolloutLane(txCtx, q, laneRow, false, nil)
				return &betweenchannel.StartRolloutResult{
					Lane:    loadedLane,
					Rollout: loadedRollout,
				}, laneErr
			case !errors.Is(getErr, sql.ErrNoRows):
				return nil, getErr
			}
			memberCount, countErr := q.CountRolloutLaneMembers(
				txCtx,
				sqlc.CountRolloutLaneMembersParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
				},
			)
			if countErr != nil {
				return nil, countErr
			}
			if memberCount == 0 {
				return nil, betweenchannel.ErrLaneEmpty
			}
			convergenceStatus, countErr := q.GetRolloutLaneFirmwareConvergenceStatus(
				txCtx,
				sqlc.GetRolloutLaneFirmwareConvergenceStatusParams{
					OrgID:  req.OrgID,
					LaneID: req.LaneID,
				},
			)
			if countErr != nil {
				return nil, countErr
			}
			unconfirmed := convergenceStatus.TotalCount - convergenceStatus.ConfirmedCount
			if unconfirmed > 0 {
				return nil, fmt.Errorf(
					"%w: %d miners are not confirmed",
					betweenchannel.ErrFirmwareConvergenceActive,
					unconfirmed,
				)
			}

			transitions, transitionErr := q.ListRolloutLaneChannelTransitions(
				txCtx,
				sqlc.ListRolloutLaneChannelTransitionsParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
				},
			)
			if transitionErr != nil {
				return nil, transitionErr
			}
			domainTransitions := transitionRows(transitions)
			if err := validateFrozenPopulation(domainTransitions, req.Batches); err != nil {
				return nil, err
			}

			laneChannels, listErr := q.ListRolloutLaneChannels(
				txCtx,
				sqlc.ListRolloutLaneChannelsParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
				},
			)
			if listErr != nil {
				return nil, listErr
			}
			channelIDs := make([]int64, 0, len(laneChannels))
			for _, laneChannel := range laneChannels {
				channelIDs = append(channelIDs, laneChannel.ChannelID)
			}
			lockedChannels, lockErr := q.LockBetweenChannelChannels(
				txCtx,
				sqlc.LockBetweenChannelChannelsParams{
					OrgID:      req.OrgID,
					ChannelIds: channelIDs,
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if len(lockedChannels) != len(channelIDs) {
				return nil, betweenchannel.ErrLaneConflict
			}
			nonCurrentMembers, countErr := q.CountRolloutLaneNonCurrentMembers(
				txCtx,
				sqlc.CountRolloutLaneNonCurrentMembersParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
				},
			)
			if countErr != nil {
				return nil, countErr
			}
			if nonCurrentMembers > 0 {
				return nil, fmt.Errorf(
					"%w: %d members remain on non-current rollout lane channels",
					betweenchannel.ErrMembershipConflict,
					nonCurrentMembers,
				)
			}
			deviceIDs := transitionDeviceIDs(domainTransitions)
			lockedDevices, lockErr := q.LockBetweenChannelDevices(
				txCtx,
				sqlc.LockBetweenChannelDevicesParams{
					OrgID:     req.OrgID,
					DeviceIds: deviceIDs,
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if len(lockedDevices) != len(deviceIDs) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			revalidated, transitionErr := q.ListRolloutLaneChannelTransitions(
				txCtx,
				sqlc.ListRolloutLaneChannelTransitionsParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
				},
			)
			if transitionErr != nil {
				return nil, transitionErr
			}
			domainTransitions = transitionRows(revalidated)
			if err := validateFrozenPopulation(domainTransitions, req.Batches); err != nil {
				return nil, err
			}
			if err := betweenchannel.ValidateTransitionTargetsForStore(
				domainTransitions,
				req.ReleaseTargets,
			); err != nil {
				return nil, err
			}

			releaseSetID, createErr := createLaneReleaseSet(
				txCtx,
				q,
				req.OrgID,
				req.ReleaseTargets,
			)
			if createErr != nil {
				return nil, createErr
			}
			position, listErr := q.GetNextRolloutLaneChannelPosition(
				txCtx,
				sqlc.GetNextRolloutLaneChannelPositionParams{
					LaneID: req.LaneID,
					OrgID:  req.OrgID,
				},
			)
			if listErr != nil {
				return nil, listErr
			}
			targetChannelID, createErr := createLanePhysicalChannel(
				txCtx,
				q,
				req.OrgID,
				req.LaneID,
				position,
				releaseSetID,
			)
			if createErr != nil {
				return nil, createErr
			}
			rolloutRow, createErr := createBetweenChannelRollout(
				txCtx,
				q,
				betweenChannelRolloutCreate{
					Request:            req,
					SourceChannelID:    laneRow.CurrentChannelID,
					TargetChannelID:    targetChannelID,
					Transitions:        domainTransitions,
					TargetReleaseSetID: releaseSetID,
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
					RolloutID:           uuid.NullUUID{UUID: req.ID, Valid: true},
					StartIdempotencyKey: sql.NullString{String: req.IdempotencyKey, Valid: true},
					StartFingerprint:    sql.NullString{String: req.RequestFingerprint, Valid: true},
				},
			); createErr != nil {
				return nil, createErr
			}
			loadedRollout, loadErr := loadRollout(txCtx, q, rolloutRow)
			if loadErr != nil {
				return nil, loadErr
			}
			loadedLane, loadErr := loadRolloutLane(txCtx, q, laneRow, false, nil)
			if loadErr != nil {
				return nil, loadErr
			}
			return &betweenchannel.StartRolloutResult{
				Lane:    loadedLane,
				Rollout: loadedRollout,
			}, nil
		},
	)
	if err != nil {
		if isUniqueViolationOn(err, "uq_firmware_rollout_active_owner") {
			return betweenchannel.StartRolloutResult{}, rollout.ErrOwnershipConflict
		}
		if isUniqueViolationOn(err, "uq_rollout_lane_start_key") {
			return betweenchannel.StartRolloutResult{}, betweenchannel.ErrIdempotencyConflict
		}
		return betweenchannel.StartRolloutResult{}, fmt.Errorf("start rollout lane: %w", err)
	}
	started, ok := result.(*betweenchannel.StartRolloutResult)
	if !ok {
		return betweenchannel.StartRolloutResult{}, fmt.Errorf(
			"start rollout lane: unexpected result %T",
			result,
		)
	}
	return *started, nil
}

func (s *SQLRolloutLaneStore) AdmitBatch(
	ctx context.Context,
	req rollout.AdmissionRequest,
) rollout.AdmissionResult {
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if req.Rollout.SourceChannelID == nil ||
			req.Rollout.TargetChannelID == nil ||
			req.Rollout.TargetReleaseSetID == nil {
			return betweenchannel.ErrCompatibility
		}
		q := s.GetQueries(txCtx)
		child, err := q.LockFirmwareRollout(
			txCtx,
			sqlc.LockFirmwareRolloutParams{
				RolloutID: req.Rollout.ID,
				OrgID:     req.Rollout.OrgID,
			},
		)
		if err != nil {
			return err
		}
		if child.LaneModelID.Valid {
			declaration, lockErr := lockModelChildScope(txCtx, q, child)
			if lockErr != nil {
				return lockErr
			}
			if declaration.CurrentChannelID != *req.Rollout.SourceChannelID {
				return betweenchannel.ErrLaneConflict
			}
		} else {
			lane, laneErr := q.GetRolloutLaneForRollout(
				txCtx,
				sqlc.GetRolloutLaneForRolloutParams{
					RolloutID: uuid.NullUUID{UUID: req.Rollout.ID, Valid: true},
					OrgID:     req.Rollout.OrgID,
				},
			)
			if errors.Is(laneErr, sql.ErrNoRows) {
				return betweenchannel.ErrLaneNotFound
			}
			if laneErr != nil {
				return laneErr
			}
			lane, laneErr = q.LockRolloutLane(
				txCtx,
				sqlc.LockRolloutLaneParams{LaneID: lane.ID, OrgID: lane.OrgID},
			)
			if laneErr != nil {
				return laneErr
			}
			if lane.CurrentChannelID != *req.Rollout.SourceChannelID {
				return betweenchannel.ErrLaneConflict
			}
		}
		if err := lockStartedRolloutControl(
			txCtx,
			q,
			req.Rollout.ID,
			req.Rollout.OrgID,
			req.ControlID,
		); err != nil {
			return err
		}
		lockedChannels, err := q.LockBetweenChannelChannels(
			txCtx,
			sqlc.LockBetweenChannelChannelsParams{
				OrgID: req.Rollout.OrgID,
				ChannelIds: []int64{
					*req.Rollout.SourceChannelID,
					*req.Rollout.TargetChannelID,
				},
			},
		)
		if err != nil {
			return err
		}
		if len(lockedChannels) != 2 {
			return betweenchannel.ErrLaneConflict
		}
		deviceIDs := make([]int64, 0, len(req.Batch.Members))
		for _, member := range req.Batch.Members {
			deviceIDs = append(deviceIDs, member.DeviceID)
		}
		lockedDevices, err := q.LockBetweenChannelDevices(
			txCtx,
			sqlc.LockBetweenChannelDevicesParams{
				OrgID:     req.Rollout.OrgID,
				DeviceIds: deviceIDs,
			},
		)
		if err != nil {
			return err
		}
		if len(lockedDevices) != len(deviceIDs) {
			return betweenchannel.ErrMembershipConflict
		}
		members, err := q.ListBetweenChannelAdmissionMembers(
			txCtx,
			sqlc.ListBetweenChannelAdmissionMembersParams{
				SourceChannelID: *req.Rollout.SourceChannelID,
				RolloutID:       req.Rollout.ID,
				BatchID:         req.Batch.ID,
				OrgID:           req.Rollout.OrgID,
			},
		)
		if err != nil {
			return err
		}
		admittedCount, err := q.CountBetweenChannelAdmittedBatchMembers(
			txCtx,
			sqlc.CountBetweenChannelAdmittedBatchMembersParams{
				RolloutID: req.Rollout.ID,
				BatchID:   req.Batch.ID,
				OrgID:     req.Rollout.OrgID,
			},
		)
		if err != nil {
			return err
		}
		if admittedCount == 0 {
			return nil
		}
		if int64(len(members)) != admittedCount {
			return betweenchannel.ErrMembershipConflict
		}
		if _, err = q.CreateBetweenChannelAdmissionEnforcements(
			txCtx,
			sqlc.CreateBetweenChannelAdmissionEnforcementsParams{
				AuthorityID:       req.Rollout.ForwardAuthorityID,
				AuthorityRevision: req.Rollout.ForwardAuthorityRevision,
				RolloutID:         req.Rollout.ID,
				BatchID:           req.Batch.ID,
				OrgID:             req.Rollout.OrgID,
			},
		); err != nil {
			return err
		}
		if _, err = q.AttachBetweenChannelAdmissionEnforcements(
			txCtx,
			sqlc.AttachBetweenChannelAdmissionEnforcementsParams{
				RolloutID:   req.Rollout.ID,
				BatchID:     req.Batch.ID,
				OrgID:       req.Rollout.OrgID,
				AuthorityID: req.Rollout.ForwardAuthorityID,
			},
		); err != nil {
			return err
		}
		attached, err := q.CountBetweenChannelAttachedAdmissionMembers(
			txCtx,
			sqlc.CountBetweenChannelAttachedAdmissionMembersParams{
				RolloutID:   req.Rollout.ID,
				BatchID:     req.Batch.ID,
				OrgID:       req.Rollout.OrgID,
				AuthorityID: req.Rollout.ForwardAuthorityID,
			},
		)
		if err != nil {
			return err
		}
		if attached != int64(len(members)) {
			return betweenchannel.ErrCompatibility
		}
		now := time.Now().UTC()
		_, err = q.CaptureBetweenChannelBatchBaseline(
			txCtx,
			sqlc.CaptureBetweenChannelBatchBaselineParams{
				WindowStart: now.Add(-baselineWindow),
				WindowEnd:   now,
				FreshAfter:  now.Add(-baselineWindow),
				RolloutID:   req.Rollout.ID,
				BatchID:     req.Batch.ID,
				OrgID:       req.Rollout.OrgID,
			},
		)
		return err
	})
	if err == nil {
		return rollout.AdmissionResult{Outcome: rollout.AdmissionOutcomeCommitted}
	}
	var outcomeUnknown *infrastructuredb.TransactionOutcomeUnknownError
	if errors.As(err, &outcomeUnknown) {
		return rollout.AdmissionResult{
			Outcome: rollout.AdmissionOutcomeUnknown,
			Err:     err,
		}
	}
	return rollout.AdmissionResult{
		Outcome: rollout.AdmissionOutcomeDefinitivelyRolledBack,
		Err:     err,
	}
}

func (s *SQLRolloutLaneStore) PrepareRevert(
	ctx context.Context,
	req rollout.RevertRequest,
) error {
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if req.Rollout.SourceChannelID == nil ||
			req.Rollout.TargetChannelID == nil ||
			req.Rollout.SourceReleaseSetID == nil ||
			req.Rollout.RevertAuthorityID == nil ||
			req.Rollout.RevertAuthorityRevision == nil {
			return betweenchannel.ErrCompatibility
		}
		q := s.GetQueries(txCtx)
		child, err := q.LockFirmwareRollout(
			txCtx,
			sqlc.LockFirmwareRolloutParams{
				RolloutID: req.Rollout.ID,
				OrgID:     req.Rollout.OrgID,
			},
		)
		if err != nil {
			return err
		}
		lane, err := q.GetRolloutLaneForRollout(
			txCtx,
			sqlc.GetRolloutLaneForRolloutParams{
				RolloutID: uuid.NullUUID{UUID: req.Rollout.ID, Valid: true},
				OrgID:     req.Rollout.OrgID,
			},
		)
		if err != nil {
			return err
		}
		expectedCurrentChannelID := *req.Rollout.TargetChannelID
		if req.Rollout.AbortedAt != nil {
			expectedCurrentChannelID = *req.Rollout.SourceChannelID
		}
		if child.LaneModelID.Valid {
			declaration, lockErr := lockModelChildScope(txCtx, q, child)
			if lockErr != nil {
				return lockErr
			}
			if declaration.CurrentChannelID != *req.Rollout.SourceChannelID &&
				declaration.CurrentChannelID != *req.Rollout.TargetChannelID {
				return betweenchannel.ErrLaneConflict
			}
		} else {
			lane, err = q.LockRolloutLane(
				txCtx,
				sqlc.LockRolloutLaneParams{LaneID: lane.ID, OrgID: lane.OrgID},
			)
			if err != nil {
				return err
			}
			if lane.CurrentChannelID != expectedCurrentChannelID {
				return betweenchannel.ErrLaneConflict
			}
		}
		if err := lockStartedRolloutControl(
			txCtx,
			q,
			req.Rollout.ID,
			req.Rollout.OrgID,
			req.ControlID,
		); err != nil {
			return err
		}
		lockedChannels, err := q.LockBetweenChannelChannels(
			txCtx,
			sqlc.LockBetweenChannelChannelsParams{
				OrgID: req.Rollout.OrgID,
				ChannelIds: []int64{
					*req.Rollout.SourceChannelID,
					*req.Rollout.TargetChannelID,
				},
			},
		)
		if err != nil {
			return err
		}
		if len(lockedChannels) != 2 {
			return betweenchannel.ErrLaneConflict
		}
		deviceIDs := make([]int64, 0)
		for _, member := range req.Rollout.Members {
			if member.State == rollout.MemberStateReverting {
				deviceIDs = append(deviceIDs, member.DeviceID)
			}
		}
		if len(deviceIDs) == 0 {
			return nil
		}
		if _, err = q.LockBetweenChannelDevices(
			txCtx,
			sqlc.LockBetweenChannelDevicesParams{
				OrgID:     req.Rollout.OrgID,
				DeviceIds: deviceIDs,
			},
		); err != nil {
			return err
		}
		if _, err = q.MarkBetweenChannelRevertMembershipConflicts(
			txCtx,
			sqlc.MarkBetweenChannelRevertMembershipConflictsParams{
				RolloutID:       req.Rollout.ID,
				OrgID:           req.Rollout.OrgID,
				TargetChannelID: *req.Rollout.TargetChannelID,
			},
		); err != nil {
			return err
		}
		if _, err = q.CreateBetweenChannelRevertEnforcements(
			txCtx,
			sqlc.CreateBetweenChannelRevertEnforcementsParams{
				TargetChannelID:   *req.Rollout.TargetChannelID,
				AuthorityID:       *req.Rollout.RevertAuthorityID,
				AuthorityRevision: *req.Rollout.RevertAuthorityRevision,
				RolloutID:         req.Rollout.ID,
				OrgID:             req.Rollout.OrgID,
			},
		); err != nil {
			return err
		}
		if _, err = q.AttachBetweenChannelRevertEnforcements(
			txCtx,
			sqlc.AttachBetweenChannelRevertEnforcementsParams{
				RolloutID:   req.Rollout.ID,
				OrgID:       req.Rollout.OrgID,
				AuthorityID: *req.Rollout.RevertAuthorityID,
			},
		); err != nil {
			return err
		}
		missing, err := q.CountBetweenChannelRevertMembersWithoutEnforcement(
			txCtx,
			sqlc.CountBetweenChannelRevertMembersWithoutEnforcementParams{
				AuthorityID: *req.Rollout.RevertAuthorityID,
				RolloutID:   req.Rollout.ID,
				OrgID:       req.Rollout.OrgID,
			},
		)
		if err != nil {
			return err
		}
		if missing != 0 {
			return betweenchannel.ErrCompatibility
		}
		settled, err := settleBetweenChannelRevert(
			txCtx,
			q,
			rolloutSettlementContext{
				RolloutID:                req.Rollout.ID,
				OrgID:                    req.Rollout.OrgID,
				RolloutState:             req.Rollout.State,
				RolloutRevision:          req.Rollout.Revision,
				ForwardAuthorityID:       req.Rollout.ForwardAuthorityID,
				ForwardAuthorityRevision: req.Rollout.ForwardAuthorityRevision,
				RevertAuthorityID:        req.Rollout.RevertAuthorityID,
				RevertAuthorityRevision:  req.Rollout.RevertAuthorityRevision,
				CreatedByUserID:          req.Rollout.CreatedByUserID,
				SourceChannelID:          *req.Rollout.SourceChannelID,
				TargetChannelID:          *req.Rollout.TargetChannelID,
				LaneID:                   lane.ID,
				CurrentChannelID:         lane.CurrentChannelID,
			},
		)
		if err != nil {
			return err
		}
		if settled {
			_, err = q.FinishFirmwareRolloutControl(
				txCtx,
				sqlc.FinishFirmwareRolloutControlParams{
					Status:    string(rollout.ControlStatusSucceeded),
					ControlID: req.ControlID,
					RolloutID: req.Rollout.ID,
					OrgID:     req.Rollout.OrgID,
				},
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLRolloutLaneStore) GetCompletionStatus(
	ctx context.Context,
	orgID int64,
	rolloutID uuid.UUID,
) (betweenchannel.CompletionStatus, error) {
	row, err := s.GetQueries(ctx).GetBetweenChannelCompletionCounts(
		ctx,
		sqlc.GetBetweenChannelCompletionCountsParams{
			RolloutID: rolloutID,
			OrgID:     orgID,
		},
	)
	if err != nil {
		return betweenchannel.CompletionStatus{}, fmt.Errorf(
			"get between-channel completion status: %w",
			err,
		)
	}
	return betweenchannel.CompletionStatus{
		TotalMembers:           row.TotalMembers,
		SucceededMembers:       row.SucceededMembers,
		TerminalForwardMembers: row.TerminalForwardMembers,
		RevertMembers:          row.RevertMembers,
		RevertedMembers:        row.RevertedMembers,
	}, nil
}

func (s *SQLRolloutLaneStore) AdvanceLane(
	ctx context.Context,
	orgID int64,
	rolloutID uuid.UUID,
	expectedChannelID int64,
	targetChannelID int64,
) error {
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		q := s.GetQueries(txCtx)
		child, err := q.GetFirmwareRollout(txCtx, sqlc.GetFirmwareRolloutParams{
			RolloutID: rolloutID,
			OrgID:     orgID,
		})
		if err != nil {
			return err
		}
		if child.LaneModelID.Valid && child.LaneID.Valid {
			laneModelID := child.LaneModelID.UUID
			return advanceBetweenChannelModel(
				txCtx,
				q,
				rolloutSettlementContext{
					RolloutID:   rolloutID,
					OrgID:       orgID,
					LaneID:      child.LaneID.UUID,
					LaneModelID: &laneModelID,
				},
				expectedChannelID,
				targetChannelID,
			)
		}
		lane, err := q.GetRolloutLaneForRollout(
			txCtx,
			sqlc.GetRolloutLaneForRolloutParams{
				RolloutID: uuid.NullUUID{UUID: rolloutID, Valid: true},
				OrgID:     orgID,
			},
		)
		if err != nil {
			return err
		}
		lane, err = q.LockRolloutLane(
			txCtx,
			sqlc.LockRolloutLaneParams{LaneID: lane.ID, OrgID: lane.OrgID},
		)
		if err != nil {
			return err
		}
		if lane.CurrentChannelID == targetChannelID {
			return nil
		}
		if lane.CurrentChannelID != expectedChannelID {
			return betweenchannel.ErrLaneConflict
		}
		_, err = q.AdvanceRolloutLaneCurrentChannel(
			txCtx,
			sqlc.AdvanceRolloutLaneCurrentChannelParams{
				TargetChannelID:   targetChannelID,
				LaneID:            lane.ID,
				OrgID:             orgID,
				ExpectedChannelID: expectedChannelID,
			},
		)
		if errors.Is(err, sql.ErrNoRows) {
			return betweenchannel.ErrLaneConflict
		}
		return err
	})
}

func (s *SQLRolloutLaneStore) ListFinalizations(
	ctx context.Context,
	limit int32,
) ([]betweenchannel.Finalization, error) {
	rows, err := s.GetQueries(ctx).ListBetweenChannelFinalizations(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list between-channel finalizations: %w", err)
	}
	result := make([]betweenchannel.Finalization, 0, len(rows))
	for _, row := range rows {
		result = append(result, finalizationFromListRow(row))
	}
	return result, nil
}

func (s *SQLRolloutLaneStore) Finalize(
	ctx context.Context,
	input betweenchannel.Finalization,
) (betweenchannel.FinalizationResult, error) {
	result, err := s.transactor.RunInTxWithResult(
		ctx,
		func(txCtx context.Context) (any, error) {
			q := s.GetQueries(txCtx)
			child, lockErr := q.LockFirmwareRollout(
				txCtx,
				sqlc.LockFirmwareRolloutParams{
					RolloutID: input.RolloutID,
					OrgID:     input.OrgID,
				},
			)
			if lockErr != nil {
				return nil, lockErr
			}
			if child.LaneModelID.Valid {
				if _, lockErr = lockModelChildScope(txCtx, q, child); lockErr != nil {
					return nil, lockErr
				}
			} else {
				if _, lockErr = q.LockRolloutLane(
					txCtx,
					sqlc.LockRolloutLaneParams{
						LaneID: input.LaneID,
						OrgID:  input.OrgID,
					},
				); lockErr != nil {
					return nil, lockErr
				}
				lockedChannels, channelErr := q.LockBetweenChannelChannels(
					txCtx,
					sqlc.LockBetweenChannelChannelsParams{
						OrgID: input.OrgID,
						ChannelIds: []int64{
							input.SourceChannelID,
							input.TargetChannelID,
						},
					},
				)
				if channelErr != nil {
					return nil, channelErr
				}
				if len(lockedChannels) != 2 {
					return nil, betweenchannel.ErrLaneConflict
				}
				if _, lockErr = q.LockBetweenChannelDevices(
					txCtx,
					sqlc.LockBetweenChannelDevicesParams{
						OrgID:     input.OrgID,
						DeviceIds: []int64{input.DeviceID},
					},
				); lockErr != nil {
					return nil, lockErr
				}
			}
			currentRow, getErr := q.GetBetweenChannelFinalizationForUpdate(
				txCtx,
				sqlc.GetBetweenChannelFinalizationForUpdateParams{
					MemberID: input.MemberID,
					OrgID:    input.OrgID,
				},
			)
			if getErr != nil {
				return nil, getErr
			}
			current := finalizationFromLockedRow(currentRow)
			if current.EnforcementID != input.EnforcementID {
				return nil, rollout.ErrRevisionConflict
			}
			originalMemberState := current.MemberState
			finalized, finalizeErr := finalizeBetweenChannelMember(txCtx, q, current)
			if finalizeErr != nil {
				return nil, finalizeErr
			}
			if !finalized.ProjectActivity {
				return finalized, nil
			}
			switch originalMemberState {
			case rollout.MemberStateAdmitted:
				if settleErr := settleBetweenChannelForward(txCtx, q, current); settleErr != nil {
					return nil, settleErr
				}
			case rollout.MemberStateReverting:
				if _, settleErr := settleBetweenChannelRevert(
					txCtx,
					q,
					rolloutSettlementFromFinalization(current),
				); settleErr != nil {
					return nil, settleErr
				}
			case rollout.MemberStatePending,
				rollout.MemberStateSucceeded,
				rollout.MemberStateFailed,
				rollout.MemberStateAttentionRequired,
				rollout.MemberStateCancelled,
				rollout.MemberStateReverted:
			}
			child, getErr = q.GetFirmwareRollout(
				txCtx,
				sqlc.GetFirmwareRolloutParams{
					RolloutID: current.RolloutID,
					OrgID:     current.OrgID,
				},
			)
			if getErr != nil {
				return nil, getErr
			}
			if child.GroupID.Valid {
				if _, refreshErr := q.RefreshFirmwareRolloutGroupResult(
					txCtx,
					sqlc.RefreshFirmwareRolloutGroupResultParams{
						GroupID: child.GroupID.UUID,
						OrgID:   current.OrgID,
					},
				); refreshErr != nil {
					return nil, refreshErr
				}
			}
			return finalized, nil
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return betweenchannel.FinalizationResult{}, rollout.ErrRevisionConflict
	}
	if err != nil {
		return betweenchannel.FinalizationResult{}, fmt.Errorf(
			"finalize between-channel member: %w",
			err,
		)
	}
	finalized, ok := result.(*betweenchannel.FinalizationResult)
	if !ok {
		return betweenchannel.FinalizationResult{}, fmt.Errorf(
			"finalize between-channel member: unexpected result %T",
			result,
		)
	}
	return *finalized, nil
}

func createLaneReleaseSet(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	targets []betweenchannel.ReleaseTarget,
) (int64, error) {
	releaseSet, err := q.CreateFirmwareReleaseSet(ctx, orgID)
	if err != nil {
		return 0, err
	}
	for _, target := range targets {
		if _, err = q.CreateFirmwareReleaseTarget(
			ctx,
			sqlc.CreateFirmwareReleaseTargetParams{
				ReleaseSetID:       releaseSet.ID,
				OrgID:              orgID,
				FirmwareFileID:     target.FirmwareFileID,
				TargetManufacturer: target.Manufacturer,
				TargetModel:        target.Model,
				FirmwareVersion:    target.FirmwareVersion,
				Sha256:             target.SHA256,
			},
		); err != nil {
			return 0, err
		}
	}
	return releaseSet.ID, nil
}

func createLanePhysicalChannel(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	laneID uuid.UUID,
	position int32,
	releaseSetID int64,
) (int64, error) {
	channel, err := q.CreateDeviceSet(ctx, sqlc.CreateDeviceSetParams{
		OrgID: orgID,
		Type:  sqlc.DeviceSetTypeChannel,
		Label: fmt.Sprintf("rollout-lane-%s-%d", laneID.String(), position),
	})
	if err != nil {
		return 0, err
	}
	rows, err := q.CreateChannelExtension(ctx, sqlc.CreateChannelExtensionParams{
		ReleaseSetID: releaseSetID,
		DeviceSetID:  channel.ID,
		OrgID:        orgID,
	})
	if err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, betweenchannel.ErrCompatibility
	}
	return channel.ID, nil
}

type modelRolloutStartContext struct {
	GroupID                  uuid.UUID
	LaneModelID              uuid.UUID
	ModelIdentityKey         string
	ModelIdentityValidatedAt time.Time
	SourceReleaseTargetID    int64
	TargetReleaseTargetID    int64
	Manufacturer             string
	Model                    string
}

type betweenChannelRolloutCreate struct {
	Request            betweenchannel.StartRolloutRequest
	SourceChannelID    int64
	TargetChannelID    int64
	Transitions        []betweenchannel.DeviceTransition
	TargetReleaseSetID int64
	ModelContext       *modelRolloutStartContext
}

func createBetweenChannelRollout(
	ctx context.Context,
	q sqlc.Querier,
	input betweenChannelRolloutCreate,
) (sqlc.FirmwareRollout, error) {
	req := input.Request
	sourceChannelID := input.SourceChannelID
	targetChannelID := input.TargetChannelID
	transitions := input.Transitions
	targetReleaseSetID := input.TargetReleaseSetID
	modelContext := input.ModelContext
	sourceInfo, err := q.GetChannelInfo(
		ctx,
		sqlc.GetChannelInfoParams{
			DeviceSetID: sourceChannelID,
			OrgID:       req.OrgID,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	authority, err := q.CreateChannelFirmwareAuthority(
		ctx,
		sqlc.CreateChannelFirmwareAuthorityParams{
			ID:                 uuid.New(),
			OrgID:              req.OrgID,
			AuthorityType:      "rollout",
			AuthorityReference: req.ID.String(),
			CreatedByUserID:    req.ActorUserID,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	idempotencyKey := req.IdempotencyKey
	inputBatches := req.Batches
	targets := req.ReleaseTargets
	hashratePolicy := req.HashratePolicy
	groupID := uuid.NullUUID{}
	laneID := uuid.NullUUID{}
	laneModelID := uuid.NullUUID{}
	modelIdentityKey := sql.NullString{}
	modelIdentityValidatedAt := sql.NullTime{}
	sourceReleaseTargetID := sql.NullInt64{}
	targetReleaseTargetID := sql.NullInt64{}
	snapshot := map[string]any{"lane_id": req.LaneID.String()}
	if modelContext != nil {
		plan := req.ModelPlans[0]
		idempotencyKey = plan.ModelStartKey
		inputBatches = plan.Batches
		targets = []betweenchannel.ReleaseTarget{plan.ReleaseTarget}
		hashratePolicy = plan.HashratePolicy
		groupID = uuid.NullUUID{UUID: modelContext.GroupID, Valid: true}
		laneID = uuid.NullUUID{UUID: req.LaneID, Valid: true}
		laneModelID = uuid.NullUUID{UUID: modelContext.LaneModelID, Valid: true}
		modelIdentityKey = sql.NullString{String: modelContext.ModelIdentityKey, Valid: true}
		modelIdentityValidatedAt = sql.NullTime{
			Time:  modelContext.ModelIdentityValidatedAt,
			Valid: true,
		}
		sourceReleaseTargetID = sql.NullInt64{
			Int64: modelContext.SourceReleaseTargetID,
			Valid: true,
		}
		targetReleaseTargetID = sql.NullInt64{
			Int64: modelContext.TargetReleaseTargetID,
			Valid: true,
		}
		snapshot["lane_model_id"] = modelContext.LaneModelID.String()
		snapshot["model_identity_key"] = modelContext.ModelIdentityKey
		snapshot["manufacturer"] = modelContext.Manufacturer
		snapshot["model"] = modelContext.Model
	}
	rolloutRow, err := q.CreateFirmwareRollout(
		ctx,
		sqlc.CreateFirmwareRolloutParams{
			RolloutID:                req.ID,
			OrgID:                    req.OrgID,
			Name:                     req.Name,
			StrategyKey:              betweenchannel.StrategyKey,
			ForwardAuthorityID:       authority.ID,
			ForwardAuthorityRevision: authority.Revision,
			SourceChannelID:          sql.NullInt64{Int64: sourceChannelID, Valid: true},
			TargetChannelID:          sql.NullInt64{Int64: targetChannelID, Valid: true},
			SourceReleaseSetID:       sql.NullInt64{Int64: sourceInfo, Valid: true},
			TargetReleaseSetID:       sql.NullInt64{Int64: targetReleaseSetID, Valid: true},
			GroupID:                  groupID,
			LaneID:                   laneID,
			LaneModelID:              laneModelID,
			ModelIdentityKey:         modelIdentityKey,
			ModelIdentityValidatedAt: modelIdentityValidatedAt,
			SourceReleaseTargetID:    sourceReleaseTargetID,
			TargetReleaseTargetID:    targetReleaseTargetID,
			SourceSnapshot:           marshalSnapshot(snapshot),
			TargetSnapshot:           marshalSnapshot(snapshot),
			RevertSnapshot:           marshalSnapshot(snapshot),
			HashratePolicyMaxDropBasisPoints: ptrToNullInt32(
				hashratePolicyMaxDrop(hashratePolicy),
			),
			HashratePolicyHealthyDurationSeconds: ptrToNullInt32(
				hashratePolicyHealthyDuration(hashratePolicy),
			),
			IdempotencyKey:    idempotencyKey,
			CreateFingerprint: req.RequestFingerprint,
			Reason:            req.Reason,
			CreatedByUserID:   req.ActorUserID,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	var identityBoundary *time.Time
	if modelContext != nil {
		identityBoundary = &modelContext.ModelIdentityValidatedAt
	}
	batchJSON, memberJSON, err := rolloutInputs(
		inputBatches,
		transitions,
		targets,
		identityBoundary,
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	createdBatches, err := q.CreateFirmwareRolloutBatches(
		ctx,
		sqlc.CreateFirmwareRolloutBatchesParams{
			RolloutID: req.ID,
			OrgID:     req.OrgID,
			Batches:   batchJSON,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	if len(createdBatches) != len(req.Batches) && modelContext == nil {
		return sqlc.FirmwareRollout{}, betweenchannel.ErrMembershipConflict
	}
	if modelContext != nil && len(createdBatches) != len(req.ModelPlans[0].Batches) {
		return sqlc.FirmwareRollout{}, betweenchannel.ErrMembershipConflict
	}
	members, err := q.CreateFirmwareRolloutMembers(
		ctx,
		sqlc.CreateFirmwareRolloutMembersParams{
			RolloutID: req.ID,
			Members:   memberJSON,
			OrgID:     req.OrgID,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	if len(members) != len(transitions) {
		return sqlc.FirmwareRollout{}, betweenchannel.ErrMembershipConflict
	}
	frozenTargets, err := q.FreezeBetweenChannelMemberReleaseTargets(
		ctx,
		sqlc.FreezeBetweenChannelMemberReleaseTargetsParams{
			RolloutID: req.ID,
			OrgID:     req.OrgID,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	if frozenTargets != int64(len(members)) {
		return sqlc.FirmwareRollout{}, betweenchannel.ErrCompatibility
	}
	if _, err = q.CreateFirmwareRolloutCause(
		ctx,
		sqlc.CreateFirmwareRolloutCauseParams{
			RolloutID:         req.ID,
			OrgID:             req.OrgID,
			Operation:         string(rollout.ControlOperationCreate),
			Reason:            req.Reason,
			ActorUserID:       req.ActorUserID,
			ActorType:         persistedActorType(req.ActorType),
			ActorCredentialID: ptrToNullString(req.ActorCredentialID),
			ToState:           string(rollout.StateCreated),
			RolloutRevision:   rolloutRow.Revision,
		},
	); err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	return rolloutRow, nil
}

func rolloutInputs(
	batches []rollout.CreateBatch,
	transitions []betweenchannel.DeviceTransition,
	targets []betweenchannel.ReleaseTarget,
	modelIdentityValidatedAt *time.Time,
) (json.RawMessage, json.RawMessage, error) {
	type batchInput struct {
		Position int32  `json:"position"`
		Label    string `json:"label"`
	}
	type memberInput struct {
		BatchPosition            int32          `json:"batch_position"`
		Position                 int32          `json:"position"`
		DeviceIdentifier         string         `json:"device_identifier"`
		ModelIdentityKey         string         `json:"model_identity_key,omitempty"`
		ModelIdentityValidatedAt *time.Time     `json:"model_identity_validated_at,omitempty"`
		SourceSnapshot           map[string]any `json:"source_snapshot"`
		TargetSnapshot           map[string]any `json:"target_snapshot"`
		RevertSnapshot           map[string]any `json:"revert_snapshot"`
	}
	transitionByIdentifier := make(map[string]betweenchannel.DeviceTransition, len(transitions))
	for _, transition := range transitions {
		transitionByIdentifier[transition.DeviceIdentifier] = transition
	}
	targetByModel := make(map[string]betweenchannel.ReleaseTarget, len(targets))
	for _, target := range targets {
		targetByModel[betweenchannel.ModelKey(target.Manufacturer, target.Model)] = target
	}
	batchInputs := make([]batchInput, 0, len(batches))
	memberInputs := make([]memberInput, 0, len(transitions))
	var position int32
	for batchPosition, batch := range batches {
		batchInputs = append(batchInputs, batchInput{
			Position: int32(batchPosition), //nolint:gosec // API validation limits batches to 1000.
			Label:    batch.Label,
		})
		for _, member := range batch.Members {
			transition := transitionByIdentifier[member.DeviceIdentifier]
			target := targetByModel[betweenchannel.ModelKey(
				transition.Manufacturer,
				transition.Model,
			)]
			sourceSnapshot := map[string]any{
				"release_target_id": transition.SourceReleaseTargetID,
				"firmware_file_id":  transition.SourceFirmwareFileID,
				"firmware_version":  transition.SourceFirmwareVersion,
				"sha256":            transition.SourceSHA256,
			}
			targetSnapshot := map[string]any{
				"firmware_file_id": target.FirmwareFileID,
				"firmware_version": target.FirmwareVersion,
				"sha256":           target.SHA256,
			}
			memberInputs = append(memberInputs, memberInput{
				BatchPosition:            int32(batchPosition), //nolint:gosec // API validation limits batches to 1000.
				Position:                 position,
				DeviceIdentifier:         member.DeviceIdentifier,
				ModelIdentityKey:         transition.ModelIdentityKey,
				ModelIdentityValidatedAt: modelIdentityValidatedAt,
				SourceSnapshot:           sourceSnapshot,
				TargetSnapshot:           targetSnapshot,
				RevertSnapshot:           sourceSnapshot,
			})
			position++
		}
	}
	batchJSON, err := json.Marshal(batchInputs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal rollout lane batches: %w", err)
	}
	memberJSON, err := json.Marshal(memberInputs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal rollout lane members: %w", err)
	}
	return batchJSON, memberJSON, nil
}

func previewMembershipChange(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.PreviewMembershipChangeRequest,
) (betweenchannel.MembershipChangePreview, error) {
	identifiers := append(
		append([]string(nil), req.AddIdentifiers...),
		req.RemoveIdentifiers...,
	)
	candidates, err := q.ListRolloutLaneMembershipCandidates(
		ctx,
		sqlc.ListRolloutLaneMembershipCandidatesParams{
			OrgID:             req.OrgID,
			DeviceIdentifiers: identifiers,
		},
	)
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	if len(candidates) != len(identifiers) {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
	}
	return previewMembershipChangeWithCandidates(ctx, q, req, candidates)
}

func previewLaneWithCandidates(
	req betweenchannel.PreviewLaneRequest,
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
) (betweenchannel.InitialEnforcementPreview, []uuid.UUID, error) {
	candidateByIdentifier := membershipCandidatesByIdentifier(candidates)
	devices := make([]initialLaneDevice, 0, len(req.DeviceIdentifiers))
	reassignments := make([]betweenchannel.MembershipReassignment, 0)
	sourceLaneSet := make(map[uuid.UUID]struct{})
	for _, identifier := range req.DeviceIdentifiers {
		candidate, ok := candidateByIdentifier[identifier]
		if !ok {
			return betweenchannel.InitialEnforcementPreview{}, nil, betweenchannel.ErrMembershipConflict
		}
		if candidate.ChannelID.Valid && !candidate.SourceLaneID.Valid {
			return betweenchannel.InitialEnforcementPreview{}, nil, fmt.Errorf(
				"%w: device %s belongs to a non-lane channel",
				betweenchannel.ErrMembershipConflict,
				identifier,
			)
		}
		if candidate.SourceLaneID.Valid {
			sourceLaneSet[candidate.SourceLaneID.UUID] = struct{}{}
			reassignments = append(reassignments, betweenchannel.MembershipReassignment{
				DeviceIdentifier:      identifier,
				SourceLaneID:          candidate.SourceLaneID.UUID,
				SourceLaneLabel:       candidate.SourceLaneLabel.String,
				SourceChannelID:       candidate.ChannelID.Int64,
				SourceChannelPosition: candidate.SourceChannelPosition.Int32,
				SourceReleaseVersion:  candidate.SourceReleaseVersion,
				SourceLaneRevision:    candidate.SourceLaneRevision.Int64,
			})
		}
		devices = append(devices, initialLaneDevice{
			DeviceID:               candidate.DeviceID,
			DeviceIdentifier:       candidate.DeviceIdentifier,
			Manufacturer:           candidate.Manufacturer,
			Model:                  candidate.Model,
			CurrentFirmwareVersion: candidate.ObservedFirmwareVersion,
		})
	}
	if err := validateInitialLaneTargets(devices, req.ReleaseTargets); err != nil {
		return betweenchannel.InitialEnforcementPreview{}, nil, err
	}
	sort.Slice(reassignments, func(i, j int) bool {
		return reassignments[i].DeviceIdentifier < reassignments[j].DeviceIdentifier
	})
	sourceLaneIDs := make([]uuid.UUID, 0, len(sourceLaneSet))
	for laneID := range sourceLaneSet {
		sourceLaneIDs = append(sourceLaneIDs, laneID)
	}
	sort.Slice(sourceLaneIDs, func(i, j int) bool {
		return sourceLaneIDs[i].String() < sourceLaneIDs[j].String()
	})
	preview := buildInitialEnforcementPreview(devices, req.ReleaseTargets)
	preview.Reassignments = reassignments
	preview.RequiresReassignConfirmation = len(reassignments) > 0
	return preview, sourceLaneIDs, nil
}

func createLanePreviewRequest(
	req betweenchannel.CreateLaneRequest,
) betweenchannel.PreviewLaneRequest {
	return betweenchannel.PreviewLaneRequest{
		OrgID:             req.OrgID,
		ReleaseTargets:    req.ReleaseTargets,
		DeviceIdentifiers: req.DeviceIdentifiers,
	}
}

func previewMembershipChangeWithCandidates(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.PreviewMembershipChangeRequest,
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
) (betweenchannel.MembershipChangePreview, error) {
	targetRows, err := q.ListRolloutLaneCurrentReleaseTargets(
		ctx,
		sqlc.ListRolloutLaneCurrentReleaseTargetsParams{
			LaneID: req.LaneID,
			OrgID:  req.OrgID,
		},
	)
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	if len(targetRows) == 0 {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrLaneNotFound
	}
	targets := make([]betweenchannel.ReleaseTarget, 0, len(targetRows))
	for _, row := range targetRows {
		targets = append(targets, betweenchannel.ReleaseTarget{
			FirmwareFileID:  row.FirmwareFileID,
			Manufacturer:    row.TargetManufacturer,
			Model:           row.TargetModel,
			FirmwareVersion: row.FirmwareVersion,
			SHA256:          row.Sha256,
		})
	}
	candidateByIdentifier := membershipCandidatesByIdentifier(candidates)
	addDevices := make([]initialLaneDevice, 0, len(req.AddIdentifiers))
	reassignments := make([]betweenchannel.MembershipReassignment, 0)
	for _, identifier := range req.AddIdentifiers {
		candidate, ok := candidateByIdentifier[identifier]
		if !ok {
			return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
		}
		if candidate.ChannelID.Valid && !candidate.SourceLaneID.Valid {
			return betweenchannel.MembershipChangePreview{}, fmt.Errorf(
				"%w: device %s belongs to a non-lane channel",
				betweenchannel.ErrMembershipConflict,
				identifier,
			)
		}
		if candidate.SourceLaneID.Valid {
			if candidate.SourceLaneID.UUID == req.LaneID {
				return betweenchannel.MembershipChangePreview{}, fmt.Errorf(
					"%w: device %s already belongs to the target lane",
					betweenchannel.ErrMembershipConflict,
					identifier,
				)
			}
			reassignments = append(reassignments, betweenchannel.MembershipReassignment{
				DeviceIdentifier:      identifier,
				SourceLaneID:          candidate.SourceLaneID.UUID,
				SourceLaneLabel:       candidate.SourceLaneLabel.String,
				SourceChannelID:       candidate.ChannelID.Int64,
				SourceChannelPosition: candidate.SourceChannelPosition.Int32,
				SourceReleaseVersion:  candidate.SourceReleaseVersion,
				SourceLaneRevision:    candidate.SourceLaneRevision.Int64,
			})
		}
		addDevices = append(addDevices, initialLaneDevice{
			DeviceID:               candidate.DeviceID,
			DeviceIdentifier:       candidate.DeviceIdentifier,
			Manufacturer:           candidate.Manufacturer,
			Model:                  candidate.Model,
			CurrentFirmwareVersion: candidate.ObservedFirmwareVersion,
		})
	}
	if err = validateInitialLaneTargets(addDevices, targets); err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	firmwarePreview := buildInitialEnforcementPreview(addDevices, targets)
	firmwarePreview.Targets = targets

	removals, err := laneMembersForIdentifiers(
		ctx,
		q,
		req.OrgID,
		req.LaneID,
		req.RemoveIdentifiers,
	)
	if err != nil {
		return betweenchannel.MembershipChangePreview{}, err
	}
	if len(removals) != len(req.RemoveIdentifiers) {
		return betweenchannel.MembershipChangePreview{}, betweenchannel.ErrMembershipConflict
	}
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

func affectedMembershipLaneIDs(
	targetLaneID uuid.UUID,
	addIdentifiers []string,
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
) ([]uuid.UUID, error) {
	candidateByIdentifier := membershipCandidatesByIdentifier(candidates)
	laneIDs := map[uuid.UUID]struct{}{targetLaneID: {}}
	for _, identifier := range addIdentifiers {
		candidate, ok := candidateByIdentifier[identifier]
		if !ok {
			return nil, betweenchannel.ErrMembershipConflict
		}
		if candidate.ChannelID.Valid && !candidate.SourceLaneID.Valid {
			return nil, betweenchannel.ErrMembershipConflict
		}
		if candidate.SourceLaneID.Valid {
			if candidate.SourceLaneID.UUID == targetLaneID {
				return nil, betweenchannel.ErrMembershipConflict
			}
			laneIDs[candidate.SourceLaneID.UUID] = struct{}{}
		}
	}
	result := make([]uuid.UUID, 0, len(laneIDs))
	for laneID := range laneIDs {
		result = append(result, laneID)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result, nil
}

func membershipCandidatesByIdentifier(
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
) map[string]sqlc.ListRolloutLaneMembershipCandidatesRow {
	result := make(
		map[string]sqlc.ListRolloutLaneMembershipCandidatesRow,
		len(candidates),
	)
	for _, candidate := range candidates {
		result[candidate.DeviceIdentifier] = candidate
	}
	return result
}

func candidateDeviceIDs(
	candidates []sqlc.ListRolloutLaneMembershipCandidatesRow,
) []int64 {
	result := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.DeviceID)
	}
	return result
}

func candidateIDsForIdentifiers(
	candidates map[string]sqlc.ListRolloutLaneMembershipCandidatesRow,
	identifiers []string,
) []int64 {
	result := make([]int64, 0, len(identifiers))
	for _, identifier := range identifiers {
		result = append(result, candidates[identifier].DeviceID)
	}
	return result
}

type membershipAppliedAudit struct {
	Added      []string                      `json:"added"`
	Reassigned []membershipReassignmentAudit `json:"reassigned"`
	Removed    []membershipRemovalAudit      `json:"removed"`
}

type membershipReassignmentAudit struct {
	DeviceIdentifier      string    `json:"device_identifier"`
	SourceLaneID          uuid.UUID `json:"source_lane_id"`
	SourceLaneLabel       string    `json:"source_lane_label"`
	SourceChannelID       int64     `json:"source_channel_id"`
	SourceChannelPosition int32     `json:"source_channel_position"`
	SourceReleaseVersion  string    `json:"source_release_version"`
}

type membershipRemovalAudit struct {
	DeviceIdentifier string    `json:"device_identifier"`
	SourceLaneID     uuid.UUID `json:"source_lane_id"`
	SourceChannelID  int64     `json:"source_channel_id"`
}

func membershipChangeAuditJSON(
	req betweenchannel.UpdateMembershipRequest,
	preview betweenchannel.MembershipChangePreview,
) (json.RawMessage, json.RawMessage, error) {
	requested, err := json.Marshal(struct {
		TargetLaneID      string   `json:"target_lane_id"`
		ExpectedRevision  int64    `json:"expected_revision"`
		AddIdentifiers    []string `json:"add_device_identifiers"`
		RemoveIdentifiers []string `json:"remove_device_identifiers"`
		ConfirmFirmware   bool     `json:"confirm_firmware"`
		ConfirmReassign   bool     `json:"confirm_reassign"`
	}{
		TargetLaneID:      req.LaneID.String(),
		ExpectedRevision:  req.ExpectedRevision,
		AddIdentifiers:    req.AddIdentifiers,
		RemoveIdentifiers: req.RemoveIdentifiers,
		ConfirmFirmware:   req.ConfirmFirmware,
		ConfirmReassign:   req.ConfirmReassign,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal requested membership change: %w", err)
	}
	reassigned := make([]membershipReassignmentAudit, 0, len(preview.Reassignments))
	for _, reassignment := range preview.Reassignments {
		reassigned = append(reassigned, membershipReassignmentAudit{
			DeviceIdentifier:      reassignment.DeviceIdentifier,
			SourceLaneID:          reassignment.SourceLaneID,
			SourceLaneLabel:       reassignment.SourceLaneLabel,
			SourceChannelID:       reassignment.SourceChannelID,
			SourceChannelPosition: reassignment.SourceChannelPosition,
			SourceReleaseVersion:  reassignment.SourceReleaseVersion,
		})
	}
	removed := make([]membershipRemovalAudit, 0, len(preview.Removals))
	for _, member := range preview.Removals {
		removed = append(removed, membershipRemovalAudit{
			DeviceIdentifier: member.DeviceIdentifier,
			SourceLaneID:     req.LaneID,
			SourceChannelID:  member.ChannelID,
		})
	}
	applied, err := json.Marshal(membershipAppliedAudit{
		Added:      req.AddIdentifiers,
		Reassigned: reassigned,
		Removed:    removed,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal applied membership change: %w", err)
	}
	return requested, applied, nil
}

func laneCreateMembershipAuditJSON(
	req betweenchannel.CreateLaneRequest,
	preview betweenchannel.InitialEnforcementPreview,
) (json.RawMessage, json.RawMessage, error) {
	requested, err := json.Marshal(struct {
		TargetLaneID      string   `json:"target_lane_id"`
		DeviceIdentifiers []string `json:"add_device_identifiers"`
		ConfirmFirmware   bool     `json:"confirm_firmware"`
		ConfirmReassign   bool     `json:"confirm_reassign"`
	}{
		TargetLaneID:      req.ID.String(),
		DeviceIdentifiers: req.DeviceIdentifiers,
		ConfirmFirmware:   req.ConfirmInitialEnforcement,
		ConfirmReassign:   req.ConfirmReassignment,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal requested lane creation membership: %w", err)
	}
	reassigned := make([]membershipReassignmentAudit, 0, len(preview.Reassignments))
	for _, reassignment := range preview.Reassignments {
		reassigned = append(reassigned, membershipReassignmentAudit{
			DeviceIdentifier:      reassignment.DeviceIdentifier,
			SourceLaneID:          reassignment.SourceLaneID,
			SourceLaneLabel:       reassignment.SourceLaneLabel,
			SourceChannelID:       reassignment.SourceChannelID,
			SourceChannelPosition: reassignment.SourceChannelPosition,
			SourceReleaseVersion:  reassignment.SourceReleaseVersion,
		})
	}
	applied, err := json.Marshal(membershipAppliedAudit{
		Added:      req.DeviceIdentifiers,
		Reassigned: reassigned,
		Removed:    []membershipRemovalAudit{},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal applied lane creation membership: %w", err)
	}
	return requested, applied, nil
}

func replayRolloutLaneMembershipChange(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.UpdateMembershipRequest,
) (*betweenchannel.UpdateMembershipResult, error) {
	change, err := q.GetRolloutLaneMembershipChangeByIdempotencyKey(
		ctx,
		sqlc.GetRolloutLaneMembershipChangeByIdempotencyKeyParams{
			OrgID:          req.OrgID,
			IdempotencyKey: req.IdempotencyKey,
		},
	)
	if err != nil {
		return nil, err
	}
	if change.TargetLaneID != req.LaneID ||
		change.RequestFingerprint != req.RequestFingerprint {
		return nil, betweenchannel.ErrIdempotencyConflict
	}
	laneRow, err := q.GetRolloutLane(
		ctx,
		sqlc.GetRolloutLaneParams{LaneID: req.LaneID, OrgID: req.OrgID},
	)
	if err != nil {
		return nil, err
	}
	lane, err := loadRolloutLane(ctx, q, laneRow, false, nil)
	if err != nil {
		return nil, err
	}
	var applied membershipAppliedAudit
	if err = json.Unmarshal(change.Applied, &applied); err != nil {
		return nil, fmt.Errorf("decode applied membership change: %w", err)
	}
	addedMembers, err := laneMembersForIdentifiers(
		ctx,
		q,
		req.OrgID,
		req.LaneID,
		applied.Added,
	)
	if err != nil {
		return nil, err
	}
	return &betweenchannel.UpdateMembershipResult{
		Lane:              lane,
		TransitionMembers: addedMembers,
	}, nil
}

func laneMembersForIdentifiers(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
	laneID uuid.UUID,
	identifiers []string,
) ([]betweenchannel.LaneMember, error) {
	if len(identifiers) == 0 {
		return nil, nil
	}
	rows, err := q.ListRolloutLaneMembersByIdentifiers(
		ctx,
		sqlc.ListRolloutLaneMembersByIdentifiersParams{
			LaneID:            laneID,
			OrgID:             orgID,
			DeviceIdentifiers: identifiers,
		},
	)
	if err != nil {
		return nil, err
	}
	return laneMembersFromIdentifierRows(rows), nil
}

type laneMemberRow struct {
	DeviceID                           int64
	DeviceIdentifier                   string
	Manufacturer                       string
	Model                              string
	ObservedFirmwareVersion            string
	ChannelID                          int64
	ChannelPosition                    int32
	OnCurrentChannel                   bool
	PinnedReleaseVersion               string
	EnforcementObservedFirmwareVersion string
	EnforcementTargetFirmwareVersion   string
	EnforcementState                   string
	EnforcementLastError               string
	EnforcementUpdatedAt               time.Time
}

func laneMembersFromListRows(
	rows []sqlc.ListRolloutLaneMembersRow,
) []betweenchannel.LaneMember {
	result := make([]betweenchannel.LaneMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, laneMemberFromRow(laneMemberRow{
			DeviceID:                           row.DeviceID,
			DeviceIdentifier:                   row.DeviceIdentifier,
			Manufacturer:                       row.Manufacturer,
			Model:                              row.Model,
			ObservedFirmwareVersion:            row.ObservedFirmwareVersion,
			ChannelID:                          row.ChannelID,
			ChannelPosition:                    row.ChannelPosition,
			OnCurrentChannel:                   row.OnCurrentChannel,
			PinnedReleaseVersion:               row.PinnedReleaseVersion,
			EnforcementObservedFirmwareVersion: row.EnforcementObservedFirmwareVersion,
			EnforcementTargetFirmwareVersion:   row.EnforcementTargetFirmwareVersion,
			EnforcementState:                   row.EnforcementState,
			EnforcementLastError:               row.EnforcementLastError,
			EnforcementUpdatedAt:               row.EnforcementUpdatedAt,
		}))
	}
	return result
}

func laneMembersFromIdentifierRows(
	rows []sqlc.ListRolloutLaneMembersByIdentifiersRow,
) []betweenchannel.LaneMember {
	result := make([]betweenchannel.LaneMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, laneMemberFromRow(laneMemberRow{
			DeviceID:                           row.DeviceID,
			DeviceIdentifier:                   row.DeviceIdentifier,
			Manufacturer:                       row.Manufacturer,
			Model:                              row.Model,
			ObservedFirmwareVersion:            row.ObservedFirmwareVersion,
			ChannelID:                          row.ChannelID,
			ChannelPosition:                    row.ChannelPosition,
			OnCurrentChannel:                   row.OnCurrentChannel,
			PinnedReleaseVersion:               row.PinnedReleaseVersion,
			EnforcementObservedFirmwareVersion: row.EnforcementObservedFirmwareVersion,
			EnforcementTargetFirmwareVersion:   row.EnforcementTargetFirmwareVersion,
			EnforcementState:                   row.EnforcementState,
			EnforcementLastError:               row.EnforcementLastError,
			EnforcementUpdatedAt:               row.EnforcementUpdatedAt,
		}))
	}
	return result
}

func laneMemberFromRow(row laneMemberRow) betweenchannel.LaneMember {
	member := betweenchannel.LaneMember{
		DeviceID:                row.DeviceID,
		DeviceIdentifier:        row.DeviceIdentifier,
		Manufacturer:            row.Manufacturer,
		Model:                   row.Model,
		ObservedFirmwareVersion: row.ObservedFirmwareVersion,
		ChannelID:               row.ChannelID,
		ChannelPosition:         row.ChannelPosition,
		OnCurrentChannel:        row.OnCurrentChannel,
		PinnedReleaseVersion:    row.PinnedReleaseVersion,
	}
	if row.EnforcementState != "" {
		member.Enforcement = &channel.FirmwareTransitionMiner{
			DeviceIdentifier:              row.DeviceIdentifier,
			Manufacturer:                  row.Manufacturer,
			Model:                         row.Model,
			LatestObservedFirmwareVersion: row.EnforcementObservedFirmwareVersion,
			TargetFirmwareVersion:         row.EnforcementTargetFirmwareVersion,
			State: firmwareTransitionState(
				channel.EnforcementState(row.EnforcementState),
			),
			LastError: row.EnforcementLastError,
			UpdatedAt: row.EnforcementUpdatedAt,
		}
	}
	return member
}

type initialLaneDevice struct {
	DeviceID               int64
	DeviceIdentifier       string
	Manufacturer           string
	Model                  string
	CurrentFirmwareVersion string
}

func initialDevicesFromPreviewRows(
	rows []sqlc.ListBetweenChannelDeviceModelsRow,
) []initialLaneDevice {
	result := make([]initialLaneDevice, 0, len(rows))
	for _, row := range rows {
		result = append(result, initialLaneDevice{
			DeviceID:               row.DeviceID,
			DeviceIdentifier:       row.DeviceIdentifier,
			Manufacturer:           row.Manufacturer,
			Model:                  row.Model,
			CurrentFirmwareVersion: row.CurrentFirmwareVersion,
		})
	}
	return result
}

func initialDevicesFromLockedRows(
	rows []sqlc.LockBetweenChannelInitialDevicesRow,
) []initialLaneDevice {
	result := make([]initialLaneDevice, 0, len(rows))
	for _, row := range rows {
		result = append(result, initialLaneDevice{
			DeviceID:               row.DeviceID,
			DeviceIdentifier:       row.DeviceIdentifier,
			Manufacturer:           row.Manufacturer,
			Model:                  row.Model,
			CurrentFirmwareVersion: row.CurrentFirmwareVersion,
		})
	}
	return result
}

func validateInitialLaneTargets(
	models []initialLaneDevice,
	targets []betweenchannel.ReleaseTarget,
) error {
	targetByModel := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetByModel[betweenchannel.ModelKey(target.Manufacturer, target.Model)] = struct{}{}
	}
	for _, model := range models {
		if model.Manufacturer == "" || model.Model == "" {
			return fmt.Errorf(
				"%w: device %s has no manufacturer or model",
				betweenchannel.ErrCompatibility,
				model.DeviceIdentifier,
			)
		}
		if _, ok := targetByModel[betweenchannel.ModelKey(model.Manufacturer, model.Model)]; !ok {
			return fmt.Errorf(
				"%w: initial release is missing %s %s",
				betweenchannel.ErrCompatibility,
				model.Manufacturer,
				model.Model,
			)
		}
	}
	return nil
}

func buildInitialEnforcementPreview(
	devices []initialLaneDevice,
	targets []betweenchannel.ReleaseTarget,
) betweenchannel.InitialEnforcementPreview {
	targetByModel := make(map[string]betweenchannel.ReleaseTarget, len(targets))
	for _, target := range targets {
		targetByModel[betweenchannel.ModelKey(target.Manufacturer, target.Model)] = target
	}
	result := betweenchannel.InitialEnforcementPreview{
		Targets: append([]betweenchannel.ReleaseTarget(nil), targets...),
		Miners:  make([]betweenchannel.InitialFirmwareMiner, 0, len(devices)),
	}
	for _, device := range devices {
		target := targetByModel[betweenchannel.ModelKey(device.Manufacturer, device.Model)]
		status := betweenchannel.InitialFirmwareUnknown
		switch {
		case strings.TrimSpace(device.CurrentFirmwareVersion) == "":
			result.UnknownCount++
		case strings.TrimSpace(device.CurrentFirmwareVersion) == target.FirmwareVersion:
			status = betweenchannel.InitialFirmwareMatch
			result.MatchingCount++
		default:
			status = betweenchannel.InitialFirmwareMismatch
			result.MismatchedCount++
		}
		result.Miners = append(result.Miners, betweenchannel.InitialFirmwareMiner{
			DeviceID:               device.DeviceID,
			DeviceIdentifier:       device.DeviceIdentifier,
			Manufacturer:           device.Manufacturer,
			Model:                  device.Model,
			CurrentFirmwareVersion: strings.TrimSpace(device.CurrentFirmwareVersion),
			TargetFirmwareVersion:  target.FirmwareVersion,
			TargetFirmwareFileID:   target.FirmwareFileID,
			Status:                 status,
		})
	}
	return result
}

func validateFrozenPopulation(
	transitions []betweenchannel.DeviceTransition,
	batches []rollout.CreateBatch,
) error {
	source := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		source = append(source, transition.DeviceIdentifier)
	}
	requested := make([]string, 0, len(transitions))
	for _, batch := range batches {
		for _, member := range batch.Members {
			requested = append(requested, member.DeviceIdentifier)
		}
	}
	sort.Strings(source)
	sort.Strings(requested)
	if !slices.Equal(source, requested) {
		return betweenchannel.ErrMembershipConflict
	}
	return nil
}

func transitionRows(
	rows []sqlc.ListRolloutLaneChannelTransitionsRow,
) []betweenchannel.DeviceTransition {
	result := make([]betweenchannel.DeviceTransition, 0, len(rows))
	for _, row := range rows {
		result = append(result, betweenchannel.DeviceTransition{
			DeviceID:              row.DeviceID,
			DeviceIdentifier:      row.DeviceIdentifier,
			Manufacturer:          row.Manufacturer,
			Model:                 row.Model,
			SourceReleaseTargetID: row.SourceReleaseTargetID.Int64,
			SourceFirmwareFileID:  row.SourceFirmwareFileID.String,
			SourceFirmwareVersion: row.SourceFirmwareVersion.String,
			SourceSHA256:          row.SourceSha256.String,
		})
	}
	return result
}

func transitionDeviceIDs(transitions []betweenchannel.DeviceTransition) []int64 {
	result := make([]int64, 0, len(transitions))
	for _, transition := range transitions {
		result = append(result, transition.DeviceID)
	}
	return result
}

func loadRolloutLane(
	ctx context.Context,
	q sqlc.Querier,
	row sqlc.RolloutLane,
	includeTransitionMembers bool,
	transitionMembersUpdatedAfter *time.Time,
) (*betweenchannel.Lane, error) {
	status, err := q.GetRolloutLaneFirmwareConvergenceStatus(
		ctx,
		sqlc.GetRolloutLaneFirmwareConvergenceStatusParams{
			OrgID:  row.OrgID,
			LaneID: row.ID,
		},
	)
	if err != nil {
		return nil, err
	}
	convergenceStatus := firmwareConvergenceStatus(firmwareConvergenceCounts{
		Total:     status.TotalCount,
		Pending:   status.PendingCount,
		Updating:  status.UpdatingCount,
		Verifying: status.VerifyingCount,
		Confirmed: status.ConfirmedCount,
		Attention: status.AttentionCount,
	})
	if includeTransitionMembers {
		members, membersErr := q.ListRolloutLaneFirmwareConvergenceMembers(
			ctx,
			sqlc.ListRolloutLaneFirmwareConvergenceMembersParams{
				OrgID:               row.OrgID,
				LaneID:              row.ID,
				MembersUpdatedAfter: ptrToNullTime(transitionMembersUpdatedAfter),
			},
		)
		if membersErr != nil {
			return nil, membersErr
		}
		convergenceStatus.Members = firmwareTransitionMiners(members)
	}
	return loadRolloutLaneWithFirmwareConvergenceStatus(
		ctx,
		q,
		row,
		convergenceStatus,
		nil,
	)
}

type rolloutLaneAggregate struct {
	MemberCount     int64
	Channels        []betweenchannel.LaneChannel
	TopologyEnabled bool
	Models          []betweenchannel.LaneModel
}

func loadRolloutLaneWithFirmwareConvergenceStatus(
	ctx context.Context,
	q sqlc.Querier,
	row sqlc.RolloutLane,
	convergenceStatus betweenchannel.FirmwareConvergenceStatus,
	aggregate *rolloutLaneAggregate,
) (*betweenchannel.Lane, error) {
	var (
		channels        []betweenchannel.LaneChannel
		memberCount     int64
		topologyEnabled bool
		models          []betweenchannel.LaneModel
	)
	if aggregate != nil {
		channels = aggregate.Channels
		memberCount = aggregate.MemberCount
		topologyEnabled = aggregate.TopologyEnabled
		models = aggregate.Models
	} else {
		channelRows, err := q.ListRolloutLaneChannels(
			ctx,
			sqlc.ListRolloutLaneChannelsParams{LaneID: row.ID, OrgID: row.OrgID},
		)
		if err != nil {
			return nil, err
		}
		channels = make([]betweenchannel.LaneChannel, 0, len(channelRows))
		for _, channel := range channelRows {
			var rolloutID *uuid.UUID
			if channel.RolloutID.Valid {
				value := channel.RolloutID.UUID
				rolloutID = &value
			}
			channels = append(channels, betweenchannel.LaneChannel{
				ChannelID: channel.ChannelID, ReleaseSetID: channel.ReleaseSetID,
				Position: channel.Position, RolloutID: rolloutID, CreatedAt: channel.CreatedAt,
			})
		}
		count, countErr := q.CountRolloutLaneMembers(
			ctx,
			sqlc.CountRolloutLaneMembersParams{
				LaneID:      row.ID,
				OrgID:       row.OrgID,
				LaneModelID: uuid.NullUUID{},
			},
		)
		if countErr != nil {
			return nil, countErr
		}
		memberCount = count
		var topologyErr error
		topologyEnabled, topologyErr = rolloutLaneTopologyEnabled(ctx, q, row.OrgID)
		if topologyErr != nil {
			return nil, topologyErr
		}
		if topologyEnabled {
			var modelsErr error
			models, modelsErr = loadRolloutLaneModels(ctx, q, row.ID, row.OrgID)
			if modelsErr != nil {
				return nil, modelsErr
			}
		}
	}
	result := &betweenchannel.Lane{
		ID:                        row.ID,
		OrgID:                     row.OrgID,
		Label:                     row.Label,
		Description:               row.Description,
		CurrentChannelID:          row.CurrentChannelID,
		Revision:                  row.Revision,
		CreatedByUserID:           row.CreatedByUserID,
		CreatedAt:                 row.CreatedAt,
		UpdatedAt:                 row.UpdatedAt,
		Channels:                  channels,
		MemberCount:               int32(memberCount), //nolint:gosec // Lane membership is bounded by API limits.
		FirmwareConvergence:       convergenceStatus,
		ScalarProjectionAvailable: true,
	}
	result.TopologyEnabled = topologyEnabled
	if !topologyEnabled {
		return result, nil
	}
	result.Models = models
	scalarChannelID := sharedModelChannelID(result.Models)
	result.ScalarProjectionAvailable = scalarChannelID != 0
	if result.ScalarProjectionAvailable {
		result.CurrentChannelID = scalarChannelID
	} else {
		result.CurrentChannelID = 0
		result.MemberCount = 0
		result.FirmwareConvergence = betweenchannel.FirmwareConvergenceStatus{}
	}
	return result, nil
}

func rolloutLaneTopologyEnabled(ctx context.Context, q sqlc.Querier, orgID int64) (bool, error) {
	cutover, err := q.GetRolloutLaneTopologyCutover(ctx, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cutover.Enabled, nil
}

func loadRolloutLaneModels(
	ctx context.Context,
	q sqlc.Querier,
	laneID uuid.UUID,
	orgID int64,
) ([]betweenchannel.LaneModel, error) {
	modelsByLane, err := loadRolloutLaneModelsByLaneIDs(ctx, q, []uuid.UUID{laneID}, orgID)
	if err != nil {
		return nil, err
	}
	return modelsByLane[laneID], nil
}

func loadRolloutLaneModelsByLaneIDs(
	ctx context.Context,
	q sqlc.Querier,
	laneIDs []uuid.UUID,
	orgID int64,
) (map[uuid.UUID][]betweenchannel.LaneModel, error) {
	rows, err := q.ListRolloutLaneModelsByLaneIDs(
		ctx,
		sqlc.ListRolloutLaneModelsByLaneIDsParams{LaneIds: laneIDs, OrgID: orgID},
	)
	if err != nil {
		return nil, err
	}
	historyRows, err := q.ListRolloutLaneModelChannelsByLaneIDs(
		ctx,
		sqlc.ListRolloutLaneModelChannelsByLaneIDsParams{LaneIds: laneIDs, OrgID: orgID},
	)
	if err != nil {
		return nil, err
	}
	statusRows, err := q.ListRolloutLaneModelFirmwareConvergenceStatusesByLaneIDs(
		ctx,
		sqlc.ListRolloutLaneModelFirmwareConvergenceStatusesByLaneIDsParams{
			LaneIds: laneIDs,
			OrgID:   orgID,
		},
	)
	if err != nil {
		return nil, err
	}
	historyByModel := make(map[uuid.UUID][]betweenchannel.LaneModelChannel, len(rows))
	for _, history := range historyRows {
		historyByModel[history.LaneModelID] = append(
			historyByModel[history.LaneModelID],
			betweenchannel.LaneModelChannel{
				ChannelID: history.ChannelID,
				Position:  history.Position,
				FirmwareTarget: betweenchannel.LaneModelFirmwareTarget{
					ReleaseTargetID: history.ReleaseTargetID,
					ReleaseSetID:    history.ReleaseSetID,
					FirmwareFileID:  history.FirmwareFileID,
					FirmwareVersion: history.FirmwareVersion,
					SHA256:          history.Sha256,
				},
				CreatedAt: history.CreatedAt,
			},
		)
	}
	statusByModel := make(map[uuid.UUID]betweenchannel.FirmwareConvergenceStatus, len(statusRows))
	for _, status := range statusRows {
		statusByModel[status.LaneModelID] = firmwareConvergenceStatus(firmwareConvergenceCounts{
			Total:     status.TotalCount,
			Pending:   status.PendingCount,
			Updating:  status.UpdatingCount,
			Verifying: status.VerifyingCount,
			Confirmed: status.ConfirmedCount,
			Attention: status.AttentionCount,
		})
	}
	result := make(map[uuid.UUID][]betweenchannel.LaneModel, len(laneIDs))
	for _, laneID := range laneIDs {
		result[laneID] = make([]betweenchannel.LaneModel, 0)
	}
	for _, row := range rows {
		target := &betweenchannel.LaneModelFirmwareTarget{
			ReleaseTargetID: row.CurrentReleaseTargetID,
			ReleaseSetID:    row.CurrentReleaseSetID,
			FirmwareFileID:  row.FirmwareFileID,
			FirmwareVersion: row.FirmwareVersion,
			SHA256:          row.Sha256,
		}
		history := historyByModel[row.ID]
		for index := range history {
			history[index].Current = history[index].ChannelID == row.CurrentChannelID
		}
		result[row.LaneID] = append(result[row.LaneID], betweenchannel.LaneModel{
			ID:                     row.ID,
			LaneID:                 row.LaneID,
			OrgID:                  row.OrgID,
			ModelIdentityKey:       row.ModelIdentityKey,
			NormalizationVersion:   row.NormalizationVersion,
			Manufacturer:           row.Manufacturer,
			Model:                  row.Model,
			CurrentChannelID:       row.CurrentChannelID,
			CurrentReleaseSetID:    row.CurrentReleaseSetID,
			CurrentReleaseTargetID: row.CurrentReleaseTargetID,
			Revision:               row.Revision,
			CreatedAt:              row.CreatedAt,
			UpdatedAt:              row.UpdatedAt,
			CurrentFirmwareTarget:  target,
			MemberCount:            int32(row.ActiveBindingCount), //nolint:gosec // Lane membership is API bounded.
			Bindings: betweenchannel.LaneModelBindingSummary{
				ActiveCount:     row.ActiveBindingCount,
				HistoricalCount: row.HistoricalBindingCount,
			},
			FirmwareConvergence: statusByModel[row.ID],
			Channels:            history,
			Compatibility:       betweenchannel.LaneModelCompatible,
		})
	}
	return result, nil
}

func sharedModelChannelID(models []betweenchannel.LaneModel) int64 {
	if len(models) == 0 {
		return 0
	}
	channelID := models[0].CurrentChannelID
	for _, model := range models[1:] {
		if model.CurrentChannelID != channelID {
			return 0
		}
	}
	return channelID
}

type firmwareConvergenceCounts struct {
	Total     int64
	Pending   int64
	Updating  int64
	Verifying int64
	Confirmed int64
	Attention int64
}

func firmwareConvergenceStatus(counts firmwareConvergenceCounts) betweenchannel.FirmwareConvergenceStatus {
	return betweenchannel.FirmwareConvergenceStatus{
		TotalCount:     int32(counts.Total),     //nolint:gosec // Lane creation caps membership at 10,000.
		PendingCount:   int32(counts.Pending),   //nolint:gosec // Lane creation caps membership at 10,000.
		UpdatingCount:  int32(counts.Updating),  //nolint:gosec // Lane creation caps membership at 10,000.
		VerifyingCount: int32(counts.Verifying), //nolint:gosec // Lane creation caps membership at 10,000.
		ConfirmedCount: int32(counts.Confirmed), //nolint:gosec // Lane creation caps membership at 10,000.
		AttentionCount: int32(counts.Attention), //nolint:gosec // Lane creation caps membership at 10,000.
	}
}

func firmwareTransitionMiners(
	rows []sqlc.ListRolloutLaneFirmwareConvergenceMembersRow,
) []channel.FirmwareTransitionMiner {
	result := make([]channel.FirmwareTransitionMiner, 0, len(rows))
	for _, row := range rows {
		result = append(result, channel.FirmwareTransitionMiner{
			DeviceIdentifier:              row.DeviceIdentifier,
			Manufacturer:                  row.Manufacturer,
			Model:                         row.Model,
			LatestObservedFirmwareVersion: row.LastObservedFirmwareVersion.String,
			TargetFirmwareVersion:         row.TargetFirmwareVersion,
			State:                         firmwareTransitionState(channel.EnforcementState(row.State)),
			LastError:                     row.LastError.String,
			UpdatedAt:                     row.UpdatedAt,
		})
	}
	return result
}

func firmwareTransitionState(state channel.EnforcementState) channel.FirmwareTransitionState {
	switch state {
	case channel.EnforcementStatePending, channel.EnforcementStateHeld:
		return channel.FirmwareTransitionPending
	case channel.EnforcementStateDispatching, channel.EnforcementStateDispatched:
		return channel.FirmwareTransitionUpdating
	case channel.EnforcementStateVerifying:
		return channel.FirmwareTransitionVerifying
	case channel.EnforcementStateConfirmed:
		return channel.FirmwareTransitionConfirmed
	case channel.EnforcementStateAttentionRequired, channel.EnforcementStateCancelled:
		return channel.FirmwareTransitionNeedsAttention
	default:
		return channel.FirmwareTransitionNeedsAttention
	}
}

func finalizationFromListRow(
	row sqlc.ListBetweenChannelFinalizationsRow,
) betweenchannel.Finalization {
	return betweenchannel.Finalization{
		MemberID:                 row.MemberID,
		RolloutID:                row.RolloutID,
		OrgID:                    row.OrgID,
		BatchID:                  row.BatchID,
		DeviceID:                 row.DeviceID,
		DeviceIdentifier:         row.DeviceIdentifier,
		MemberState:              rollout.MemberState(row.MemberState),
		MemberRevision:           row.MemberRevision,
		EnforcementID:            row.EnforcementID,
		EnforcementState:         channel.EnforcementState(row.EnforcementState),
		AuthorityID:              row.AuthorityID,
		LastError:                row.LastError.String,
		RolloutState:             rollout.State(row.RolloutState),
		RolloutRevision:          row.RolloutRevision,
		ForwardAuthorityID:       row.ForwardAuthorityID,
		ForwardAuthorityRevision: row.ForwardAuthorityRevision,
		RevertAuthorityID:        nullUUIDToPtr(row.RevertAuthorityID),
		RevertAuthorityRevision:  nullInt64ToPtr(row.RevertAuthorityRevision),
		CreatedByUserID:          row.CreatedByUserID,
		SourceChannelID:          row.SourceChannelID.Int64,
		TargetChannelID:          row.TargetChannelID.Int64,
		LaneID:                   row.LaneID,
		CurrentChannelID:         row.CurrentChannelID,
		LaneModelID:              nullUUIDToPtr(row.LaneModelID),
		ParentID:                 nullUUIDToPtr(row.GroupID),
		ModelIdentityKey:         row.ModelIdentityKey.String,
		Manufacturer:             row.Manufacturer,
		Model:                    row.Model,
		ModelIdentityValidatedAt: timePtr(row.ModelIdentityValidatedAt),
		CommandCompletedAt:       timePtr(row.CommandCompletedAt),
		ObservedModelIdentityKey: row.ObservedModelIdentityKey,
		ModelIdentityObservedAt:  timePtr(row.ModelIdentityObservedAt),
		ModelCurrentChannelID:    nullInt64ToPtr(row.ModelCurrentChannelID),
	}
}

func finalizationFromLockedRow(
	row sqlc.GetBetweenChannelFinalizationForUpdateRow,
) betweenchannel.Finalization {
	return betweenchannel.Finalization{
		MemberID:                 row.MemberID,
		RolloutID:                row.RolloutID,
		OrgID:                    row.OrgID,
		BatchID:                  row.BatchID,
		DeviceID:                 row.DeviceID,
		DeviceIdentifier:         row.DeviceIdentifier,
		MemberState:              rollout.MemberState(row.MemberState),
		MemberRevision:           row.MemberRevision,
		EnforcementID:            row.EnforcementID,
		EnforcementState:         channel.EnforcementState(row.EnforcementState),
		AuthorityID:              row.AuthorityID,
		LastError:                row.LastError.String,
		RolloutState:             rollout.State(row.RolloutState),
		RolloutRevision:          row.RolloutRevision,
		ForwardAuthorityID:       row.ForwardAuthorityID,
		ForwardAuthorityRevision: row.ForwardAuthorityRevision,
		RevertAuthorityID:        nullUUIDToPtr(row.RevertAuthorityID),
		RevertAuthorityRevision:  nullInt64ToPtr(row.RevertAuthorityRevision),
		CreatedByUserID:          row.CreatedByUserID,
		SourceChannelID:          row.SourceChannelID.Int64,
		TargetChannelID:          row.TargetChannelID.Int64,
		LaneID:                   row.LaneID,
		CurrentChannelID:         row.CurrentChannelID,
		LaneModelID:              nullUUIDToPtr(row.LaneModelID),
		ParentID:                 nullUUIDToPtr(row.GroupID),
		ModelIdentityKey:         row.ModelIdentityKey.String,
		Manufacturer:             row.Manufacturer,
		Model:                    row.Model,
		ModelIdentityValidatedAt: timePtr(row.ModelIdentityValidatedAt),
		CommandCompletedAt:       timePtr(row.CommandCompletedAt),
		ObservedModelIdentityKey: row.ObservedModelIdentityKey,
		ModelIdentityObservedAt:  timePtr(row.ModelIdentityObservedAt),
		ModelCurrentChannelID:    nullInt64ToPtr(row.ModelCurrentChannelID),
	}
}

func finalizeBetweenChannelMember(
	ctx context.Context,
	q sqlc.Querier,
	current betweenchannel.Finalization,
) (*betweenchannel.FinalizationResult, error) {
	switch current.EnforcementState {
	case channel.EnforcementStateAttentionRequired:
		reason := current.LastError
		if reason == "" {
			reason = "firmware enforcement requires operator attention"
		}
		return markFinalizationTerminal(
			ctx,
			q,
			current,
			rollout.MemberStateAttentionRequired,
			reason,
			betweenchannel.FinalizationOutcomeAttention,
		)
	case channel.EnforcementStateCancelled:
		return markFinalizationTerminal(
			ctx,
			q,
			current,
			rollout.MemberStateCancelled,
			current.LastError,
			betweenchannel.FinalizationOutcomeCancelled,
		)
	case channel.EnforcementStatePending, channel.EnforcementStateHeld:
		const reason = "rollout authority was halted before dispatch completed"
		cancelled, err := q.CancelHaltedBetweenChannelEnforcement(
			ctx,
			sqlc.CancelHaltedBetweenChannelEnforcementParams{
				LastError:     sql.NullString{String: reason, Valid: true},
				EnforcementID: current.EnforcementID,
			},
		)
		if err != nil {
			return nil, err
		}
		if cancelled != 1 {
			return nil, rollout.ErrRevisionConflict
		}
		current.EnforcementState = channel.EnforcementStateCancelled
		return markFinalizationTerminal(
			ctx,
			q,
			current,
			rollout.MemberStateCancelled,
			reason,
			betweenchannel.FinalizationOutcomeCancelled,
		)
	case channel.EnforcementStateConfirmed:
	case channel.EnforcementStateDispatching,
		channel.EnforcementStateDispatched,
		channel.EnforcementStateVerifying:
		return nil, rollout.ErrInvalidTransition
	}

	if current.MemberState == rollout.MemberStateAdmitted && current.LaneModelID != nil {
		if current.CommandCompletedAt == nil ||
			current.ModelIdentityObservedAt == nil ||
			!current.ModelIdentityObservedAt.After(*current.CommandCompletedAt) ||
			current.ObservedModelIdentityKey == "" {
			return &betweenchannel.FinalizationResult{Finalization: current}, nil
		}
		if current.ObservedModelIdentityKey != current.ModelIdentityKey {
			return markFinalizationTerminal(
				ctx,
				q,
				current,
				rollout.MemberStateAttentionRequired,
				"model identity changed after firmware command completion",
				betweenchannel.FinalizationOutcomeAttention,
			)
		}
	}

	membership, err := q.GetDeviceChannelMembership(
		ctx,
		sqlc.GetDeviceChannelMembershipParams{
			OrgID:    current.OrgID,
			DeviceID: current.DeviceID,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return markMembershipConflict(ctx, q, current)
	}
	if err != nil {
		return nil, err
	}

	switch current.MemberState {
	case rollout.MemberStateSucceeded:
		if membership == current.TargetChannelID {
			return &betweenchannel.FinalizationResult{
				Finalization: current,
				Outcome:      betweenchannel.FinalizationOutcomeMoved,
			}, nil
		}
		return nil, rollout.ErrRevisionConflict
	case rollout.MemberStateReverted:
		if membership == current.SourceChannelID {
			return &betweenchannel.FinalizationResult{
				Finalization: current,
				Outcome:      betweenchannel.FinalizationOutcomeMoved,
			}, nil
		}
		return nil, rollout.ErrRevisionConflict
	case rollout.MemberStateAdmitted:
		currentPointer := current.CurrentChannelID
		if current.ModelCurrentChannelID != nil {
			currentPointer = *current.ModelCurrentChannelID
		}
		if current.AuthorityID != current.ForwardAuthorityID ||
			currentPointer != current.SourceChannelID {
			return markMembershipConflict(ctx, q, current)
		}
		if membership != current.SourceChannelID {
			return markMembershipConflict(ctx, q, current)
		}
		if current.LaneModelID != nil {
			_, err = q.FinalizeBetweenChannelModelForward(
				ctx,
				sqlc.FinalizeBetweenChannelModelForwardParams{
					MemberID:                current.MemberID,
					RolloutID:               current.RolloutID,
					OrgID:                   current.OrgID,
					ExpectedRevision:        current.MemberRevision,
					LaneModelID:             *current.LaneModelID,
					LaneID:                  current.LaneID,
					DeviceID:                current.DeviceID,
					SourceChannelID:         current.SourceChannelID,
					TargetChannelID:         current.TargetChannelID,
					BindingID:               uuid.New(),
					ModelIdentityKey:        current.ModelIdentityKey,
					ModelIdentityObservedAt: ptrToNullTime(current.ModelIdentityObservedAt),
				},
			)
		} else {
			_, err = q.FinalizeBetweenChannelForward(
				ctx,
				sqlc.FinalizeBetweenChannelForwardParams{
					MemberID:         current.MemberID,
					RolloutID:        current.RolloutID,
					OrgID:            current.OrgID,
					ExpectedRevision: current.MemberRevision,
					DeviceID:         current.DeviceID,
					SourceChannelID:  current.SourceChannelID,
					TargetChannelID:  current.TargetChannelID,
				},
			)
		}
		if err != nil {
			return nil, err
		}
		current.MemberState = rollout.MemberStateSucceeded
		return &betweenchannel.FinalizationResult{
			Finalization:    current,
			Outcome:         betweenchannel.FinalizationOutcomeMoved,
			ProjectActivity: true,
		}, nil
	case rollout.MemberStateReverting:
		if current.RevertAuthorityID == nil ||
			current.AuthorityID != *current.RevertAuthorityID ||
			membership != current.TargetChannelID {
			return markMembershipConflict(ctx, q, current)
		}
		if current.LaneModelID != nil {
			_, err = q.FinalizeBetweenChannelModelRevert(
				ctx,
				sqlc.FinalizeBetweenChannelModelRevertParams{
					MemberID:                current.MemberID,
					RolloutID:               current.RolloutID,
					OrgID:                   current.OrgID,
					ExpectedRevision:        current.MemberRevision,
					LaneModelID:             *current.LaneModelID,
					LaneID:                  current.LaneID,
					DeviceID:                current.DeviceID,
					TargetChannelID:         current.TargetChannelID,
					SourceChannelID:         current.SourceChannelID,
					BindingID:               uuid.New(),
					ModelIdentityKey:        current.ModelIdentityKey,
					ModelIdentityObservedAt: ptrToNullTime(current.ModelIdentityObservedAt),
				},
			)
		} else {
			_, err = q.FinalizeBetweenChannelRevert(
				ctx,
				sqlc.FinalizeBetweenChannelRevertParams{
					MemberID:         current.MemberID,
					RolloutID:        current.RolloutID,
					OrgID:            current.OrgID,
					ExpectedRevision: current.MemberRevision,
					DeviceID:         current.DeviceID,
					TargetChannelID:  current.TargetChannelID,
					SourceChannelID:  current.SourceChannelID,
				},
			)
		}
		if err != nil {
			return nil, err
		}
		current.MemberState = rollout.MemberStateReverted
		return &betweenchannel.FinalizationResult{
			Finalization:    current,
			Outcome:         betweenchannel.FinalizationOutcomeMoved,
			ProjectActivity: true,
		}, nil
	case rollout.MemberStatePending,
		rollout.MemberStateFailed,
		rollout.MemberStateAttentionRequired,
		rollout.MemberStateCancelled:
		return nil, rollout.ErrRevisionConflict
	}
	return nil, rollout.ErrRevisionConflict
}

func markMembershipConflict(
	ctx context.Context,
	q sqlc.Querier,
	current betweenchannel.Finalization,
) (*betweenchannel.FinalizationResult, error) {
	const reason = "device channel membership changed outside the active rollout"
	return markFinalizationTerminal(
		ctx,
		q,
		current,
		rollout.MemberStateAttentionRequired,
		reason,
		betweenchannel.FinalizationOutcomeConflict,
	)
}

func markFinalizationTerminal(
	ctx context.Context,
	q sqlc.Querier,
	current betweenchannel.Finalization,
	state rollout.MemberState,
	reason string,
	outcome betweenchannel.FinalizationOutcome,
) (*betweenchannel.FinalizationResult, error) {
	_, err := q.MarkBetweenChannelMemberTerminal(
		ctx,
		sqlc.MarkBetweenChannelMemberTerminalParams{
			MemberState:      string(state),
			LastError:        sql.NullString{String: reason, Valid: reason != ""},
			MemberID:         current.MemberID,
			RolloutID:        current.RolloutID,
			OrgID:            current.OrgID,
			ExpectedRevision: current.MemberRevision,
			ExpectedState:    string(current.MemberState),
		},
	)
	if err != nil {
		return nil, err
	}
	current.MemberState = state
	current.LastError = reason
	return &betweenchannel.FinalizationResult{
		Finalization:    current,
		Outcome:         outcome,
		ProjectActivity: true,
	}, nil
}

func settleBetweenChannelForward(
	ctx context.Context,
	q sqlc.Querier,
	current betweenchannel.Finalization,
) error {
	completedBatch, err := q.CompleteSettledBetweenChannelBatch(
		ctx,
		sqlc.CompleteSettledBetweenChannelBatchParams{
			BatchID:   current.BatchID,
			RolloutID: current.RolloutID,
			OrgID:     current.OrgID,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.RolloutState == rollout.StateAborted {
		return nil
	}
	if !completedBatch.IsFinalBatch {
		if current.RolloutState != rollout.StateRunning {
			return nil
		}
		_, err = q.MoveBetweenChannelRolloutToReview(
			ctx,
			sqlc.MoveBetweenChannelRolloutToReviewParams{
				RolloutID:        current.RolloutID,
				OrgID:            current.OrgID,
				ExpectedRevision: current.RolloutRevision,
			},
		)
		if errors.Is(err, sql.ErrNoRows) {
			return rollout.ErrRevisionConflict
		}
		return err
	}

	settlement, err := q.GetBetweenChannelForwardSettlement(
		ctx,
		sqlc.GetBetweenChannelForwardSettlementParams{
			RolloutID: current.RolloutID,
			OrgID:     current.OrgID,
		},
	)
	if err != nil {
		return err
	}
	if settlement.TotalMembers == 0 ||
		settlement.TerminalMembers != settlement.TotalMembers ||
		settlement.IncompleteBatches != 0 {
		return nil
	}
	switch current.RolloutState {
	case rollout.StateRunning, rollout.StateReview, rollout.StatePaused:
	case rollout.StateAborted,
		rollout.StateCreated,
		rollout.StateCompleted,
		rollout.StateCompletedWithFailures,
		rollout.StateReverting,
		rollout.StateReverted:
		return nil
	}

	if settlement.FailedMembers > 0 {
		if current.RolloutState == rollout.StateReview {
			return nil
		}
		_, err = q.MoveBetweenChannelRolloutToReview(
			ctx,
			sqlc.MoveBetweenChannelRolloutToReviewParams{
				RolloutID:        current.RolloutID,
				OrgID:            current.OrgID,
				ExpectedRevision: current.RolloutRevision,
			},
		)
		if errors.Is(err, sql.ErrNoRows) {
			return rollout.ErrRevisionConflict
		}
		return err
	}

	targetState := rollout.StateCompleted
	authority, err := q.HaltChannelFirmwareAuthority(
		ctx,
		sqlc.HaltChannelFirmwareAuthorityParams{
			AuthorityID:      current.ForwardAuthorityID,
			OrgID:            current.OrgID,
			ExpectedRevision: current.ForwardAuthorityRevision,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return rollout.ErrRevisionConflict
	}
	if err != nil {
		return err
	}
	completed, err := q.CompleteBetweenChannelRollout(
		ctx,
		sqlc.CompleteBetweenChannelRolloutParams{
			TargetState: string(targetState),
			ForwardAuthorityRevision: sql.NullInt64{
				Int64: authority.Revision,
				Valid: true,
			},
			RolloutID:        current.RolloutID,
			OrgID:            current.OrgID,
			ExpectedRevision: current.RolloutRevision,
			ExpectedState:    string(current.RolloutState),
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return rollout.ErrRevisionConflict
	}
	if err != nil {
		return err
	}
	if _, err = q.ReleaseFirmwareRolloutOwners(
		ctx,
		sqlc.ReleaseFirmwareRolloutOwnersParams{
			RolloutID: current.RolloutID,
			OrgID:     current.OrgID,
		},
	); err != nil {
		return err
	}
	settlementContext := rolloutSettlementFromFinalization(current)
	if current.LaneModelID != nil {
		if err = advanceBetweenChannelModel(
			ctx,
			q,
			settlementContext,
			current.SourceChannelID,
			current.TargetChannelID,
		); err != nil {
			return err
		}
	} else {
		if err = advanceBetweenChannelLane(
			ctx,
			q,
			settlementContext,
			current.SourceChannelID,
			current.TargetChannelID,
		); err != nil {
			return err
		}
	}
	return createAutomaticCompletionCause(ctx, q, settlementContext, completed)
}

type rolloutSettlementContext struct {
	RolloutID                uuid.UUID
	OrgID                    int64
	RolloutState             rollout.State
	RolloutRevision          int64
	ForwardAuthorityID       uuid.UUID
	ForwardAuthorityRevision int64
	RevertAuthorityID        *uuid.UUID
	RevertAuthorityRevision  *int64
	CreatedByUserID          int64
	SourceChannelID          int64
	TargetChannelID          int64
	LaneID                   uuid.UUID
	CurrentChannelID         int64
	LaneModelID              *uuid.UUID
}

func rolloutSettlementFromFinalization(
	current betweenchannel.Finalization,
) rolloutSettlementContext {
	return rolloutSettlementContext{
		RolloutID:                current.RolloutID,
		OrgID:                    current.OrgID,
		RolloutState:             current.RolloutState,
		RolloutRevision:          current.RolloutRevision,
		ForwardAuthorityID:       current.ForwardAuthorityID,
		ForwardAuthorityRevision: current.ForwardAuthorityRevision,
		RevertAuthorityID:        current.RevertAuthorityID,
		RevertAuthorityRevision:  current.RevertAuthorityRevision,
		CreatedByUserID:          current.CreatedByUserID,
		SourceChannelID:          current.SourceChannelID,
		TargetChannelID:          current.TargetChannelID,
		LaneID:                   current.LaneID,
		CurrentChannelID:         current.CurrentChannelID,
		LaneModelID:              current.LaneModelID,
	}
}

func advanceBetweenChannelModel(
	ctx context.Context,
	q sqlc.Querier,
	current rolloutSettlementContext,
	expectedChannelID int64,
	targetChannelID int64,
) error {
	if current.LaneModelID == nil {
		return betweenchannel.ErrLaneConflict
	}
	declaration, err := q.LockRolloutLaneModelForMutation(
		ctx,
		sqlc.LockRolloutLaneModelForMutationParams{
			LaneID:      current.LaneID,
			OrgID:       current.OrgID,
			LaneModelID: uuid.NullUUID{UUID: *current.LaneModelID, Valid: true},
		},
	)
	if err != nil {
		return err
	}
	if declaration.CurrentChannelID == targetChannelID {
		return nil
	}
	if declaration.CurrentChannelID != expectedChannelID {
		return betweenchannel.ErrLaneConflict
	}
	target, err := q.GetChannelInfo(ctx, sqlc.GetChannelInfoParams{
		DeviceSetID: targetChannelID,
		OrgID:       current.OrgID,
	})
	if err != nil {
		return err
	}
	targetRelease, err := q.GetRolloutLaneReleaseTargetByModel(
		ctx,
		sqlc.GetRolloutLaneReleaseTargetByModelParams{
			ReleaseSetID:     target,
			OrgID:            current.OrgID,
			ModelIdentityKey: declaration.ModelIdentityKey,
		},
	)
	if err != nil {
		return err
	}
	_, err = q.AdvanceRolloutLaneModelCurrentTarget(
		ctx,
		sqlc.AdvanceRolloutLaneModelCurrentTargetParams{
			TargetChannelID:       targetChannelID,
			TargetReleaseSetID:    target,
			TargetReleaseTargetID: targetRelease.ID,
			LaneModelID:           declaration.ID,
			LaneID:                declaration.LaneID,
			OrgID:                 declaration.OrgID,
			ExpectedChannelID:     expectedChannelID,
			ExpectedRevision:      declaration.Revision,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return betweenchannel.ErrLaneConflict
	}
	return err
}

func settleBetweenChannelRevert(
	ctx context.Context,
	q sqlc.Querier,
	current rolloutSettlementContext,
) (bool, error) {
	if current.RolloutState != rollout.StateReverting ||
		current.RevertAuthorityID == nil ||
		current.RevertAuthorityRevision == nil {
		return false, nil
	}
	settlement, err := q.GetBetweenChannelRevertSettlement(
		ctx,
		sqlc.GetBetweenChannelRevertSettlementParams{
			RolloutID: current.RolloutID,
			OrgID:     current.OrgID,
		},
	)
	if err != nil {
		return false, err
	}
	if settlement.SelectedMembers == 0 ||
		settlement.TerminalMembers != settlement.SelectedMembers {
		return false, nil
	}

	targetState := rollout.StateReverted
	if settlement.FailedMembers > 0 {
		targetState = rollout.StateCompletedWithFailures
	}
	authority, err := q.HaltChannelFirmwareAuthority(
		ctx,
		sqlc.HaltChannelFirmwareAuthorityParams{
			AuthorityID:      *current.RevertAuthorityID,
			OrgID:            current.OrgID,
			ExpectedRevision: *current.RevertAuthorityRevision,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, rollout.ErrRevisionConflict
	}
	if err != nil {
		return false, err
	}
	completed, err := q.CompleteBetweenChannelRollout(
		ctx,
		sqlc.CompleteBetweenChannelRolloutParams{
			TargetState: string(targetState),
			RevertAuthorityRevision: sql.NullInt64{
				Int64: authority.Revision,
				Valid: true,
			},
			RolloutID:        current.RolloutID,
			OrgID:            current.OrgID,
			ExpectedRevision: current.RolloutRevision,
			ExpectedState:    string(current.RolloutState),
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, rollout.ErrRevisionConflict
	}
	if err != nil {
		return false, err
	}
	if _, err = q.ReleaseFirmwareRolloutOwners(
		ctx,
		sqlc.ReleaseFirmwareRolloutOwnersParams{
			RolloutID: current.RolloutID,
			OrgID:     current.OrgID,
		},
	); err != nil {
		return false, err
	}
	if targetState == rollout.StateReverted {
		if current.LaneModelID != nil {
			declaration, declarationErr := q.LockRolloutLaneModelForMutation(
				ctx,
				sqlc.LockRolloutLaneModelForMutationParams{
					LaneID:      current.LaneID,
					OrgID:       current.OrgID,
					LaneModelID: uuid.NullUUID{UUID: *current.LaneModelID, Valid: true},
				},
			)
			if declarationErr != nil {
				return false, declarationErr
			}
			switch declaration.CurrentChannelID {
			case current.SourceChannelID:
				// Split and aborted child reverts leave the declaration pointer at source.
			case current.TargetChannelID:
				if err = advanceBetweenChannelModel(
					ctx,
					q,
					current,
					current.TargetChannelID,
					current.SourceChannelID,
				); err != nil {
					return false, err
				}
			default:
				return false, betweenchannel.ErrLaneConflict
			}
		} else if err = advanceBetweenChannelLane(
			ctx,
			q,
			current,
			current.TargetChannelID,
			current.SourceChannelID,
		); err != nil {
			return false, err
		}
	}
	if err = createAutomaticCompletionCause(ctx, q, current, completed); err != nil {
		return false, err
	}
	return true, nil
}

func advanceBetweenChannelLane(
	ctx context.Context,
	q sqlc.Querier,
	current rolloutSettlementContext,
	expectedChannelID int64,
	targetChannelID int64,
) error {
	if current.CurrentChannelID == targetChannelID {
		return nil
	}
	if current.CurrentChannelID != expectedChannelID {
		return betweenchannel.ErrLaneConflict
	}
	_, err := q.AdvanceRolloutLaneCurrentChannel(
		ctx,
		sqlc.AdvanceRolloutLaneCurrentChannelParams{
			TargetChannelID:   targetChannelID,
			LaneID:            current.LaneID,
			OrgID:             current.OrgID,
			ExpectedChannelID: expectedChannelID,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return betweenchannel.ErrLaneConflict
	}
	return err
}

func createAutomaticCompletionCause(
	ctx context.Context,
	q sqlc.Querier,
	current rolloutSettlementContext,
	completed sqlc.FirmwareRollout,
) error {
	_, err := q.CreateFirmwareRolloutCause(
		ctx,
		sqlc.CreateFirmwareRolloutCauseParams{
			RolloutID:       current.RolloutID,
			OrgID:           current.OrgID,
			Operation:       string(rollout.ControlOperationComplete),
			Reason:          "rollout lane work settled automatically",
			ActorUserID:     current.CreatedByUserID,
			ActorType:       string(rollout.ActorTypeSystem),
			FromState:       sql.NullString{String: string(current.RolloutState), Valid: true},
			ToState:         completed.State,
			RolloutRevision: completed.Revision,
		},
	)
	return err
}

func lockStartedRolloutControl(
	ctx context.Context,
	q sqlc.Querier,
	rolloutID uuid.UUID,
	orgID int64,
	controlID uuid.UUID,
) error {
	control, err := q.GetFirmwareRolloutControl(
		ctx,
		sqlc.GetFirmwareRolloutControlParams{
			ControlID: controlID,
			RolloutID: rolloutID,
			OrgID:     orgID,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return rollout.ErrRevisionConflict
	}
	if err != nil {
		return err
	}
	if control.Status != string(rollout.ControlStatusStarted) {
		return rollout.ErrRevisionConflict
	}
	return nil
}

func loadTopologyReadiness(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
) (betweenchannel.TopologyReadiness, error) {
	cutover, err := q.GetRolloutLaneTopologyCutover(ctx, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		cutover = sqlc.RolloutLaneTopologyCutover{OrgID: orgID, Revision: 1}
	} else if err != nil {
		return betweenchannel.TopologyReadiness{}, err
	}
	activeLegacyCount, err := q.CountActiveLegacyRolloutLaneWork(ctx, orgID)
	if err != nil {
		return betweenchannel.TopologyReadiness{}, err
	}
	rows, err := q.ListRolloutLaneTopologyAnomalies(ctx, orgID)
	if err != nil {
		return betweenchannel.TopologyReadiness{}, err
	}
	anomalies := make([]betweenchannel.TopologyAnomaly, 0, len(rows))
	for _, row := range rows {
		var details map[string]any
		if unmarshalErr := json.Unmarshal(row.Details, &details); unmarshalErr != nil {
			return betweenchannel.TopologyReadiness{}, fmt.Errorf(
				"decode topology anomaly %s: %w",
				row.AnomalyID,
				unmarshalErr,
			)
		}
		actions := make([]betweenchannel.TopologyRepairAction, 0, len(row.SupportedRepairActions))
		for _, action := range row.SupportedRepairActions {
			actions = append(actions, betweenchannel.TopologyRepairAction(action))
		}
		anomaly := betweenchannel.TopologyAnomaly{
			ID:                     row.AnomalyID,
			LaneID:                 row.LaneID,
			DeviceID:               row.DeviceID,
			DeviceIdentifier:       row.DeviceIdentifier,
			Type:                   betweenchannel.TopologyAnomalyType(row.AnomalyType),
			SupportedRepairActions: actions,
			Details:                details,
		}
		if row.LaneModelID.Valid {
			value := row.LaneModelID.UUID
			anomaly.LaneModelID = &value
		}
		if row.LaneModelRevision.Valid {
			value := row.LaneModelRevision.Int64
			anomaly.LaneModelRevision = &value
		}
		anomalies = append(anomalies, anomaly)
	}
	return betweenchannel.TopologyReadiness{
		OrgID:                    orgID,
		Enabled:                  cutover.Enabled,
		Revision:                 cutover.Revision,
		AnomalyCount:             int64(len(anomalies)),
		ActiveLegacyRolloutCount: activeLegacyCount,
		Anomalies:                anomalies,
		UpdatedAt:                cutover.UpdatedAt,
	}, nil
}

func refreshRolloutLaneTopologyBackfill(
	ctx context.Context,
	q sqlc.Querier,
	orgID int64,
) error {
	if err := q.RunRolloutLaneTopologyBackfill(ctx, orgID); err != nil {
		return fmt.Errorf("refresh rollout lane model topology backfill: %w", err)
	}
	return nil
}

func replayTopologyRepair(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.RepairModelBindingRequest,
) (betweenchannel.RepairModelBindingResult, error) {
	operation, err := q.GetRolloutLaneTopologyAdminOperationByKey(
		ctx,
		sqlc.GetRolloutLaneTopologyAdminOperationByKeyParams{
			OrgID:          req.OrgID,
			IdempotencyKey: req.IdempotencyKey,
		},
	)
	if err != nil {
		return betweenchannel.RepairModelBindingResult{}, err
	}
	if operation.Operation != "repair_binding" ||
		operation.RequestFingerprint != req.RequestFingerprint {
		return betweenchannel.RepairModelBindingResult{}, betweenchannel.ErrIdempotencyConflict
	}
	var applied struct {
		BindingID string `json:"binding_id"`
	}
	if err := json.Unmarshal(operation.Applied, &applied); err != nil {
		return betweenchannel.RepairModelBindingResult{}, fmt.Errorf(
			"decode replayed topology repair result: %w",
			err,
		)
	}
	bindingID, err := uuid.Parse(applied.BindingID)
	if err != nil {
		return betweenchannel.RepairModelBindingResult{}, fmt.Errorf(
			"parse replayed topology repair binding ID: %w",
			err,
		)
	}
	readiness, err := loadTopologyReadiness(ctx, q, req.OrgID)
	if err != nil {
		return betweenchannel.RepairModelBindingResult{}, err
	}
	return betweenchannel.RepairModelBindingResult{
		BindingID:         bindingID,
		ResultingRevision: operation.ResultingRevision,
		Replayed:          true,
		Readiness:         readiness,
	}, nil
}

func replayTopologyEnable(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.EnableTopologyRequest,
) (betweenchannel.EnableTopologyResult, error) {
	operation, err := q.GetRolloutLaneTopologyAdminOperationByKey(
		ctx,
		sqlc.GetRolloutLaneTopologyAdminOperationByKeyParams{
			OrgID:          req.OrgID,
			IdempotencyKey: req.IdempotencyKey,
		},
	)
	if err != nil {
		return betweenchannel.EnableTopologyResult{}, err
	}
	if operation.Operation != "enable" ||
		operation.RequestFingerprint != req.RequestFingerprint {
		return betweenchannel.EnableTopologyResult{}, betweenchannel.ErrIdempotencyConflict
	}
	readiness, err := loadTopologyReadiness(ctx, q, req.OrgID)
	if err != nil {
		return betweenchannel.EnableTopologyResult{}, err
	}
	return betweenchannel.EnableTopologyResult{
		Readiness: readiness,
		Replayed:  true,
	}, nil
}
