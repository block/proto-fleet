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
)

const baselineWindow = 15 * time.Minute

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
	rows, err := s.GetQueries(ctx).ListBetweenChannelDeviceModels(
		ctx,
		sqlc.ListBetweenChannelDeviceModelsParams{
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
	devices := initialDevicesFromPreviewRows(rows)
	if len(devices) != len(req.DeviceIdentifiers) {
		return betweenchannel.InitialEnforcementPreview{}, betweenchannel.ErrMembershipConflict
	}
	if err = validateInitialLaneTargets(devices, req.ReleaseTargets); err != nil {
		return betweenchannel.InitialEnforcementPreview{}, err
	}
	return buildInitialEnforcementPreview(devices, req.ReleaseTargets), nil
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
				if existing.CreateFingerprint != req.RequestFingerprint {
					return nil, betweenchannel.ErrIdempotencyConflict
				}
				return loadRolloutLane(txCtx, q, existing)
			case !errors.Is(getErr, sql.ErrNoRows):
				return nil, getErr
			}

			models, modelErr := q.LockBetweenChannelInitialDevices(
				txCtx,
				sqlc.LockBetweenChannelInitialDevicesParams{
					OrgID:             req.OrgID,
					DeviceIdentifiers: req.DeviceIdentifiers,
				},
			)
			if modelErr != nil {
				return nil, modelErr
			}
			if len(models) != len(req.DeviceIdentifiers) {
				return nil, betweenchannel.ErrMembershipConflict
			}
			devices := initialDevicesFromLockedRows(models)
			if compatibilityErr := validateInitialLaneTargets(devices, req.ReleaseTargets); compatibilityErr != nil {
				return nil, compatibilityErr
			}
			preview := buildInitialEnforcementPreview(devices, req.ReleaseTargets)
			if preview.RequiresConfirmation() && !req.ConfirmInitialEnforcement {
				return nil, fmt.Errorf(
					"%w: %d mismatched and %d unknown miners",
					betweenchannel.ErrInitialEnforcementConfirmationRequired,
					preview.MismatchedCount,
					preview.UnknownCount,
				)
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
			return loadRolloutLane(txCtx, q, laneRow)
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
	if existing.CreateFingerprint != req.RequestFingerprint {
		return nil, betweenchannel.ErrIdempotencyConflict
	}
	return loadRolloutLane(ctx, q, existing)
}

func (s *SQLRolloutLaneStore) GetLane(
	ctx context.Context,
	orgID int64,
	laneID uuid.UUID,
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
	lane, err := loadRolloutLane(ctx, s.GetQueries(ctx), row)
	if err != nil {
		return nil, fmt.Errorf("load rollout lane: %w", err)
	}
	return lane, nil
}

func (s *SQLRolloutLaneStore) ListLanes(
	ctx context.Context,
	orgID int64,
) ([]betweenchannel.Lane, error) {
	q := s.GetQueries(ctx)
	rows, err := q.ListRolloutLanes(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list rollout lanes: %w", err)
	}
	statusRows, err := q.ListRolloutLaneInitialEnforcementStatuses(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list rollout lane initial enforcement statuses: %w", err)
	}
	initialStatusByLane := make(
		map[uuid.UUID]betweenchannel.InitialEnforcementStatus,
		len(statusRows),
	)
	for _, status := range statusRows {
		initialStatusByLane[status.LaneID] = initialEnforcementStatus(
			status.TotalCount,
			status.PendingCount,
			status.UpdatingCount,
			status.ConfirmedCount,
			status.AttentionCount,
		)
	}
	result := make([]betweenchannel.Lane, 0, len(rows))
	for _, row := range rows {
		lane, loadErr := loadRolloutLaneWithInitialStatus(
			ctx,
			q,
			row,
			initialStatusByLane[row.ID],
		)
		if loadErr != nil {
			return nil, fmt.Errorf("load rollout lane: %w", loadErr)
		}
		result = append(result, *lane)
	}
	return result, nil
}

func (s *SQLRolloutLaneStore) StartRollout(
	ctx context.Context,
	req betweenchannel.StartRolloutRequest,
) (betweenchannel.StartRolloutResult, error) {
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
				loadedLane, laneErr := loadRolloutLane(txCtx, q, laneRow)
				return &betweenchannel.StartRolloutResult{
					Lane:    loadedLane,
					Rollout: loadedRollout,
				}, laneErr
			case !errors.Is(getErr, sql.ErrNoRows):
				return nil, getErr
			}
			activeInitial, countErr := q.CountActiveRolloutLaneInitialEnforcements(
				txCtx,
				sqlc.CountActiveRolloutLaneInitialEnforcementsParams{
					OrgID:  req.OrgID,
					LaneID: req.LaneID,
				},
			)
			if countErr != nil {
				return nil, countErr
			}
			if activeInitial > 0 {
				return nil, fmt.Errorf(
					"%w: %d miners have not settled",
					betweenchannel.ErrInitialEnforcementActive,
					activeInitial,
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
				req,
				laneRow.CurrentChannelID,
				targetChannelID,
				domainTransitions,
				releaseSetID,
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
			loadedLane, loadErr := loadRolloutLane(txCtx, q, laneRow)
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
) error {
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if req.Rollout.SourceChannelID == nil ||
			req.Rollout.TargetChannelID == nil ||
			req.Rollout.TargetReleaseSetID == nil {
			return betweenchannel.ErrCompatibility
		}
		q := s.GetQueries(txCtx)
		lane, err := q.GetRolloutLaneForRollout(
			txCtx,
			sqlc.GetRolloutLaneForRolloutParams{
				RolloutID: uuid.NullUUID{UUID: req.Rollout.ID, Valid: true},
				OrgID:     req.Rollout.OrgID,
			},
		)
		if errors.Is(err, sql.ErrNoRows) {
			return betweenchannel.ErrLaneNotFound
		}
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
		if lane.CurrentChannelID != *req.Rollout.SourceChannelID {
			return betweenchannel.ErrLaneConflict
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
		lane, err = q.LockRolloutLane(
			txCtx,
			sqlc.LockRolloutLaneParams{LaneID: lane.ID, OrgID: lane.OrgID},
		)
		if err != nil {
			return err
		}
		expectedCurrentChannelID := *req.Rollout.TargetChannelID
		if req.Rollout.AbortedAt != nil {
			expectedCurrentChannelID = *req.Rollout.SourceChannelID
		}
		if lane.CurrentChannelID != expectedCurrentChannelID {
			return betweenchannel.ErrLaneConflict
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
			if _, lockErr := q.LockRolloutLane(
				txCtx,
				sqlc.LockRolloutLaneParams{
					LaneID: input.LaneID,
					OrgID:  input.OrgID,
				},
			); lockErr != nil {
				return nil, lockErr
			}
			lockedChannels, lockErr := q.LockBetweenChannelChannels(
				txCtx,
				sqlc.LockBetweenChannelChannelsParams{
					OrgID: input.OrgID,
					ChannelIds: []int64{
						input.SourceChannelID,
						input.TargetChannelID,
					},
				},
			)
			if lockErr != nil {
				return nil, lockErr
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

func createBetweenChannelRollout(
	ctx context.Context,
	q sqlc.Querier,
	req betweenchannel.StartRolloutRequest,
	sourceChannelID int64,
	targetChannelID int64,
	transitions []betweenchannel.DeviceTransition,
	targetReleaseSetID int64,
) (sqlc.FirmwareRollout, error) {
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
			SourceSnapshot:           marshalSnapshot(map[string]any{"lane_id": req.LaneID.String()}),
			TargetSnapshot:           marshalSnapshot(map[string]any{"lane_id": req.LaneID.String()}),
			RevertSnapshot:           marshalSnapshot(map[string]any{"lane_id": req.LaneID.String()}),
			IdempotencyKey:           req.IdempotencyKey,
			CreateFingerprint:        req.RequestFingerprint,
			Reason:                   req.Reason,
			CreatedByUserID:          req.ActorUserID,
		},
	)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	batchJSON, memberJSON, err := rolloutInputs(req.Batches, transitions, req.ReleaseTargets)
	if err != nil {
		return sqlc.FirmwareRollout{}, err
	}
	batches, err := q.CreateFirmwareRolloutBatches(
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
	if len(batches) != len(req.Batches) {
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
) (json.RawMessage, json.RawMessage, error) {
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
				BatchPosition:    int32(batchPosition), //nolint:gosec // API validation limits batches to 1000.
				Position:         position,
				DeviceIdentifier: member.DeviceIdentifier,
				SourceSnapshot:   sourceSnapshot,
				TargetSnapshot:   targetSnapshot,
				RevertSnapshot:   sourceSnapshot,
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
) (*betweenchannel.Lane, error) {
	status, err := q.GetRolloutLaneInitialEnforcementStatus(
		ctx,
		sqlc.GetRolloutLaneInitialEnforcementStatusParams{
			OrgID:  row.OrgID,
			LaneID: row.ID,
		},
	)
	if err != nil {
		return nil, err
	}
	return loadRolloutLaneWithInitialStatus(
		ctx,
		q,
		row,
		initialEnforcementStatus(
			status.TotalCount,
			status.PendingCount,
			status.UpdatingCount,
			status.ConfirmedCount,
			status.AttentionCount,
		),
	)
}

func loadRolloutLaneWithInitialStatus(
	ctx context.Context,
	q sqlc.Querier,
	row sqlc.RolloutLane,
	initialStatus betweenchannel.InitialEnforcementStatus,
) (*betweenchannel.Lane, error) {
	channels, err := q.ListRolloutLaneChannels(
		ctx,
		sqlc.ListRolloutLaneChannelsParams{
			LaneID: row.ID,
			OrgID:  row.OrgID,
		},
	)
	if err != nil {
		return nil, err
	}
	result := &betweenchannel.Lane{
		ID:                 row.ID,
		OrgID:              row.OrgID,
		Label:              row.Label,
		Description:        row.Description,
		CurrentChannelID:   row.CurrentChannelID,
		Revision:           row.Revision,
		CreatedByUserID:    row.CreatedByUserID,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		Channels:           make([]betweenchannel.LaneChannel, 0, len(channels)),
		InitialEnforcement: initialStatus,
	}
	for _, channel := range channels {
		var rolloutID *uuid.UUID
		if channel.RolloutID.Valid {
			value := channel.RolloutID.UUID
			rolloutID = &value
		}
		result.Channels = append(result.Channels, betweenchannel.LaneChannel{
			ChannelID:    channel.ChannelID,
			ReleaseSetID: channel.ReleaseSetID,
			Position:     channel.Position,
			RolloutID:    rolloutID,
			CreatedAt:    channel.CreatedAt,
		})
	}
	return result, nil
}

func initialEnforcementStatus(
	total int64,
	pending int64,
	updating int64,
	confirmed int64,
	attention int64,
) betweenchannel.InitialEnforcementStatus {
	return betweenchannel.InitialEnforcementStatus{
		TotalCount:     int32(total),     //nolint:gosec // Lane creation caps membership at 10,000.
		PendingCount:   int32(pending),   //nolint:gosec // Lane creation caps membership at 10,000.
		UpdatingCount:  int32(updating),  //nolint:gosec // Lane creation caps membership at 10,000.
		ConfirmedCount: int32(confirmed), //nolint:gosec // Lane creation caps membership at 10,000.
		AttentionCount: int32(attention), //nolint:gosec // Lane creation caps membership at 10,000.
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
		if current.AuthorityID != current.ForwardAuthorityID ||
			current.CurrentChannelID != current.SourceChannelID {
			return markMembershipConflict(ctx, q, current)
		}
		if membership != current.SourceChannelID {
			return markMembershipConflict(ctx, q, current)
		}
		if _, err = q.FinalizeBetweenChannelForward(
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
		); err != nil {
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
		if _, err = q.FinalizeBetweenChannelRevert(
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
		); err != nil {
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
	if err = advanceBetweenChannelLane(
		ctx,
		q,
		settlementContext,
		current.SourceChannelID,
		current.TargetChannelID,
	); err != nil {
		return err
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
	}
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
		if err = advanceBetweenChannelLane(
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
