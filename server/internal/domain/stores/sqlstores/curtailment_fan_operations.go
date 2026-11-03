package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

func (s *SQLCurtailmentStore) CommandFanState(
	ctx context.Context,
	eventID int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (*string, error) {
	if params.ExpectedEventState.IsTerminal() {
		return nil, fleeterror.NewInvalidArgumentError("terminal fan state must use serialized terminal recovery")
	}
	if command == nil {
		return nil, fleeterror.NewInvalidArgumentError("facility fan command is required")
	}
	return s.commandFanState(ctx, eventID, params, command)
}

func (s *SQLCurtailmentStore) commandFanState(
	ctx context.Context,
	eventID int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (*string, error) {
	return db.WithTransactionNoRetry(ctx, s.conn.DB, func(q sqlc.Querier) (*string, error) {
		return s.commandFanStateWithQueries(ctx, q, eventID, params, command)
	})
}

func (s *SQLCurtailmentStore) commandFanStateWithQueries(
	ctx context.Context,
	q sqlc.Querier,
	eventID int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (*string, error) {
	_, err := q.LockCurtailmentEventForFanCommand(ctx, sqlc.LockCurtailmentEventForFanCommandParams{
		ID:            eventID,
		ExpectedState: string(params.ExpectedEventState),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, interfaces.ErrCurtailmentEventStateRaceLoss
	}
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to lock curtailment event %d for fan command: %v", eventID, err)
	}

	lastError := command(ctx)
	fanAirflowReopenedAt := params.FanAirflowReopenedAt
	if lastError == nil && params.FanAirflowReopenedAtOnSuccess != nil {
		fanAirflowReopenedAt = params.FanAirflowReopenedAtOnSuccess
		// A successful command replaces an old phase's marker rather than
		// clearing the newly confirmed airflow timestamp.
		params.ClearFanAirflowReopenedAt = false
	}
	rows, err := q.UpdateCurtailmentEventFanState(ctx, sqlc.UpdateCurtailmentEventFanStateParams{
		ID:                        eventID,
		ExpectedState:             string(params.ExpectedEventState),
		FanOffSentAt:              ptrToNullTime(params.FanOffSentAt),
		FanOnSentAt:               ptrToNullTime(params.FanOnSentAt),
		FanAirflowReopenedAt:      ptrToNullTime(fanAirflowReopenedAt),
		ClearFanAirflowReopenedAt: params.ClearFanAirflowReopenedAt,
		FanLastError:              ptrToNullString(lastError),
	})
	if err != nil {
		return lastError, fleeterror.NewInternalErrorf("failed to update curtailment event %d fan state: %v", eventID, err)
	}
	if rows == 0 {
		return lastError, interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return lastError, nil
}

func (s *SQLCurtailmentStore) ForceReleaseEventWithFanRecovery(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
	reason string,
	eventID int64,
	facilityFanDeviceIDs []int64,
	facilityFanSiteIDs []int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (interfaces.ForceReleaseEventResult, error) {
	if params.ExpectedEventState != models.EventStateCancelled {
		return interfaces.ForceReleaseEventResult{}, fleeterror.NewInvalidArgumentError(
			"force-release fan recovery requires cancelled expected event state",
		)
	}
	fanIDs := uniqueSortedInt64s(facilityFanDeviceIDs)
	if len(fanIDs) == 0 || len(fanIDs) != len(facilityFanDeviceIDs) || len(facilityFanSiteIDs) != len(facilityFanDeviceIDs) {
		return interfaces.ForceReleaseEventResult{}, fleeterror.NewInvalidArgumentError(
			"force-release fan recovery requires positive unique facility fan IDs",
		)
	}
	expectedSiteByFanID := make(map[int64]int64, len(facilityFanDeviceIDs))
	for index, fanID := range facilityFanDeviceIDs {
		if facilityFanSiteIDs[index] <= 0 {
			return interfaces.ForceReleaseEventResult{}, fleeterror.NewInvalidArgumentError(
				"force-release fan recovery requires positive aligned facility fan site IDs",
			)
		}
		expectedSiteByFanID[fanID] = facilityFanSiteIDs[index]
	}
	if command == nil {
		return interfaces.ForceReleaseEventResult{}, fleeterror.NewInvalidArgumentError(
			"force-release fan recovery command is required",
		)
	}

	return db.WithTransactionNoRetry(ctx, s.conn.DB, func(q sqlc.Querier) (interfaces.ForceReleaseEventResult, error) {
		for _, fanID := range fanIDs {
			if err := q.LockCurtailmentFanDeviceForWrite(ctx, strconv.FormatInt(fanID, 10)); err != nil {
				return interfaces.ForceReleaseEventResult{}, fleeterror.NewInternalErrorf("failed to lock force-release facility fan claim: %v", err)
			}
		}
		for _, fanID := range fanIDs {
			_, lockErr := q.LockInfrastructureDeviceForWrite(ctx, sqlc.LockInfrastructureDeviceForWriteParams{
				ID:             fanID,
				OrgID:          orgID,
				ExpectedSiteID: expectedSiteByFanID[fanID],
			})
			if errors.Is(lockErr, sql.ErrNoRows) {
				return interfaces.ForceReleaseEventResult{}, fleeterror.NewFailedPreconditionErrorf(
					"facility fan %d moved or was deleted after curtailment started", fanID,
				)
			}
			if lockErr != nil {
				return interfaces.ForceReleaseEventResult{}, fleeterror.NewInternalErrorf("failed to lock force-release facility fan device %d: %v", fanID, lockErr)
			}
		}

		result, releaseErr := s.forceReleaseEvent(ctx, q, orgID, eventUUID, reason)
		if releaseErr != nil {
			return interfaces.ForceReleaseEventResult{}, releaseErr
		}
		if !result.OwnershipReleased {
			if result.Event == nil || !result.Event.State.IsTerminal() || result.Event.ID != eventID {
				return interfaces.ForceReleaseEventResult{}, interfaces.ErrCurtailmentEventStateRaceLoss
			}
			claims, claimErr := q.CountCurtailmentFanClaims(ctx, sqlc.CountCurtailmentFanClaimsParams{
				IncludeTerminalErrors: true,
				OrgID:                 orgID,
				ExcludedEventID:       sql.NullInt64{Int64: eventID, Valid: true},
				FacilityFanDeviceIds:  fanIDs,
			})
			if claimErr != nil {
				return interfaces.ForceReleaseEventResult{}, fleeterror.NewInternalErrorf(
					"failed to check newer facility fan claims after force-release race: %v", claimErr,
				)
			}
			if claims > 0 {
				return interfaces.ForceReleaseEventResult{}, fleeterror.NewFailedPreconditionError(
					"facility fans are claimed by a newer active curtailment event; recover that event instead",
				)
			}
			// The reconciler won the terminal transition after the service's
			// preload. The fan locks still exclude a new owner, so recover the
			// returned terminal row instead of silently skipping the promised ON.
			params.ExpectedEventState = result.Event.State
		}

		lastError, commandErr := s.commandFanStateWithQueries(ctx, q, eventID, params, command)
		if commandErr != nil {
			return interfaces.ForceReleaseEventResult{}, commandErr
		}
		if result.Event != nil {
			if params.FanOnSentAt != nil {
				result.Event.FanOnSentAt = params.FanOnSentAt
			}
			result.Event.FanLastError = lastError
		}
		return result, nil
	})
}

func (s *SQLCurtailmentStore) RecoverTerminalFanState(
	ctx context.Context,
	eventID, orgID int64,
	facilityFanDeviceIDs []int64,
	facilityFanSiteIDs []int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) error {
	if !params.ExpectedEventState.IsTerminal() {
		return fleeterror.NewInvalidArgumentError("terminal fan recovery requires a terminal expected event state")
	}
	fanIDs := uniqueSortedInt64s(facilityFanDeviceIDs)
	if len(fanIDs) == 0 || len(fanIDs) != len(facilityFanDeviceIDs) || len(facilityFanSiteIDs) != len(facilityFanDeviceIDs) {
		return fleeterror.NewInvalidArgumentError("terminal fan recovery requires positive unique facility fan IDs")
	}
	expectedSiteByFanID := make(map[int64]int64, len(facilityFanDeviceIDs))
	for index, fanID := range facilityFanDeviceIDs {
		if facilityFanSiteIDs[index] <= 0 {
			return fleeterror.NewInvalidArgumentError("terminal fan recovery requires positive aligned facility fan site IDs")
		}
		expectedSiteByFanID[fanID] = facilityFanSiteIDs[index]
	}
	if command == nil {
		return fleeterror.NewInvalidArgumentError("terminal fan recovery command is required")
	}

	_, err := db.WithTransactionNoRetry(ctx, s.conn.DB, func(q sqlc.Querier) (struct{}, error) {
		for _, fanID := range fanIDs {
			if err := q.LockCurtailmentFanDeviceForWrite(ctx, strconv.FormatInt(fanID, 10)); err != nil {
				return struct{}{}, fleeterror.NewInternalErrorf("failed to lock terminal facility fan recovery: %v", err)
			}
			_, err := q.LockInfrastructureDeviceForWrite(ctx, sqlc.LockInfrastructureDeviceForWriteParams{
				ID:             fanID,
				OrgID:          orgID,
				ExpectedSiteID: expectedSiteByFanID[fanID],
			})
			if errors.Is(err, sql.ErrNoRows) {
				return struct{}{}, fleeterror.NewFailedPreconditionErrorf(
					"facility fan %d moved or was deleted after curtailment started", fanID,
				)
			}
			if err != nil {
				return struct{}{}, fleeterror.NewInternalErrorf("failed to lock terminal facility fan device %d: %v", fanID, err)
			}
		}
		claims, err := q.CountCurtailmentFanClaims(ctx, sqlc.CountCurtailmentFanClaimsParams{
			IncludeTerminalErrors: true,
			OrgID:                 orgID,
			ExcludedEventID:       sql.NullInt64{Int64: eventID, Valid: true},
			FacilityFanDeviceIds:  fanIDs,
		})
		if err != nil {
			return struct{}{}, fleeterror.NewInternalErrorf("failed to check newer facility fan claims: %v", err)
		}
		if claims > 0 {
			return struct{}{}, fleeterror.NewFailedPreconditionError(
				"facility fans are claimed by a newer active curtailment event; recover that event instead",
			)
		}

		lastError := command(ctx)
		rows, err := q.UpdateCurtailmentEventFanState(ctx, sqlc.UpdateCurtailmentEventFanStateParams{
			ID:                        eventID,
			ExpectedState:             string(params.ExpectedEventState),
			FanOffSentAt:              ptrToNullTime(params.FanOffSentAt),
			FanOnSentAt:               ptrToNullTime(params.FanOnSentAt),
			FanAirflowReopenedAt:      ptrToNullTime(params.FanAirflowReopenedAt),
			ClearFanAirflowReopenedAt: params.ClearFanAirflowReopenedAt,
			FanLastError:              ptrToNullString(lastError),
		})
		if err != nil {
			return struct{}{}, fleeterror.NewInternalErrorf("failed to update terminal curtailment event %d fan state: %v", eventID, err)
		}
		if rows == 0 {
			return struct{}{}, interfaces.ErrCurtailmentEventStateRaceLoss
		}
		return struct{}{}, nil
	})
	return err
}
