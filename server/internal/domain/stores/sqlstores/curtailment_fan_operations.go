package sqlstores

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
)

// Fan commands retain session advisory locks, never event row locks. Lifecycle
// writes use the same advisory key; new owners use the same per-fan keys. The
// pinned connection is discarded if cleanup cannot prove its locks released.
func withCurtailmentFanSession[T any](ctx context.Context, s *SQLCurtailmentStore, eventID int64, fanIDs []int64, action func() (T, error)) (T, error) {
	var zero T
	release, err := db.AcquireDispatchBudget(ctx, s.conn.DB)
	if err != nil {
		return zero, err
	}
	defer release()
	conn, err := s.conn.DB.Conn(ctx)
	if err != nil {
		return zero, fmt.Errorf("acquire facility fan connection: %w", err)
	}
	defer conn.Close()
	q := sqlc.New(conn)
	var lockedFans []int64
	lifecycleLocked := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var unlockErr error
		if lifecycleLocked {
			unlockErr = q.UnlockCurtailmentLifecycleSession(cleanup, strconv.FormatInt(eventID, 10))
		}
		for i := len(lockedFans) - 1; i >= 0; i-- {
			unlockErr = errors.Join(unlockErr, q.UnlockCurtailmentFanSession(cleanup, strconv.FormatInt(lockedFans[i], 10)))
		}
		if unlockErr != nil {
			_ = conn.Raw(func(any) error { return sqldriver.ErrBadConn })
		}
	}()
	for _, fanID := range uniqueSortedInt64s(fanIDs) {
		if err := q.LockCurtailmentFanSession(ctx, strconv.FormatInt(fanID, 10)); err != nil {
			// A lost acknowledgement can hide a successful session lock. Do
			// not return a connection with unknown lock ownership to the pool.
			_ = conn.Raw(func(any) error { return sqldriver.ErrBadConn })
			return zero, err
		}
		lockedFans = append(lockedFans, fanID)
	}
	if err := q.LockCurtailmentLifecycleSession(ctx, strconv.FormatInt(eventID, 10)); err != nil {
		_ = conn.Raw(func(any) error { return sqldriver.ErrBadConn })
		return zero, err
	}
	lifecycleLocked = true
	// Fan endpoints must not move or be deleted during physical I/O. These
	// narrow infrastructure locks do not block event reads or metadata edits.
	current, err := q.GetCurtailmentEventForFanOperation(ctx, eventID)
	if err != nil {
		return zero, err
	}
	if len(current.FacilityFanDeviceIds) > 0 {
		if len(current.FacilityFanDeviceIds) != len(current.FacilityFanSiteIds) {
			return zero, fleeterror.NewFailedPreconditionError("facility fan authorization snapshot is invalid")
		}
		// Retain endpoint locks until the physical adapter returns even when
		// the caller cancels; lock acquisition and I/O still use ctx below.
		//nolint:forbidigo // Canonical fan-I/O boundary; no event row locks or external-I/O replay.
		tx, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
		if err != nil {
			return zero, fmt.Errorf("begin facility fan endpoint locks: %w", err)
		}
		defer tx.Rollback()
		locked := sqlc.New(tx)
		siteByFan := make(map[int64]int64, len(current.FacilityFanDeviceIds))
		for i, fanID := range current.FacilityFanDeviceIds {
			siteByFan[fanID] = current.FacilityFanSiteIds[i]
		}
		// Match profile and Start acquisition order to avoid deadlocks with
		// concurrent changes that lock more than one infrastructure device.
		for _, fanID := range uniqueSortedInt64s(current.FacilityFanDeviceIds) {
			_, err := locked.LockInfrastructureDeviceForWrite(ctx, sqlc.LockInfrastructureDeviceForWriteParams{
				ID: fanID, OrgID: current.OrgID, ExpectedSiteID: siteByFan[fanID],
			})
			if errors.Is(err, sql.ErrNoRows) {
				return zero, fleeterror.NewFailedPreconditionErrorf("facility fan %d moved or was deleted after curtailment started", fanID)
			}
			if err != nil {
				return zero, err
			}
		}
	}
	return action()
}

func (s *SQLCurtailmentStore) CommandFanState(ctx context.Context, eventID int64, params interfaces.UpdateCurtailmentFanStateParams, command func(context.Context) *string) (*string, error) {
	if params.ExpectedEventState.IsTerminal() {
		return nil, fleeterror.NewInvalidArgumentError("terminal fan state must use serialized terminal recovery")
	}
	if command == nil {
		return nil, fleeterror.NewInvalidArgumentError("facility fan command is required")
	}
	event, err := s.GetQueries(ctx).GetCurtailmentEventForFanOperation(ctx, eventID)
	if err != nil {
		return nil, err
	}
	return withCurtailmentFanSession(ctx, s, eventID, event.FacilityFanDeviceIds, func() (*string, error) {
		current, err := s.GetQueries(ctx).GetCurtailmentEventForFanOperation(ctx, eventID)
		if err != nil {
			return nil, err
		}
		if models.EventState(current.State) != params.ExpectedEventState {
			return nil, interfaces.ErrCurtailmentEventStateRaceLoss
		}
		// Another caller may have completed this same ON phase while this
		// invocation waited for the session lock. Preserve its durable result.
		if fanOnCommandAlreadyConfirmed(current, params) {
			return nil, nil
		}
		if current.FanOnSentAt.Valid {
			params.FanOnSentAt = nil
		}
		lastError := command(ctx)
		return lastError, s.persistFanCommandResult(ctx, eventID, params, lastError)
	})
}

// FanOnSentAt belongs to the restoring/terminal ON phase; the airflow
// marker records an active-phase reopening. FanLastError distinguishes a
// first attempt from a successfully persisted physical command.
func fanOnAlreadyConfirmed(current sqlc.CurtailmentEvent) bool {
	return !current.FanLastError.Valid && (current.FanOnSentAt.Valid || current.FanAirflowReopenedAt.Valid)
}

func fanOnCommandAlreadyConfirmed(current sqlc.CurtailmentEvent, params interfaces.UpdateCurtailmentFanStateParams) bool {
	if current.FanLastError.Valid {
		return false
	}
	if params.ExpectedEventState == models.EventStateRestoring {
		return current.FanOnSentAt.Valid
	}
	return (params.FanAirflowReopenedAt != nil || params.FanAirflowReopenedAtOnSuccess != nil) && current.FanAirflowReopenedAt.Valid
}

func (s *SQLCurtailmentStore) persistFanCommandResult(ctx context.Context, eventID int64, params interfaces.UpdateCurtailmentFanStateParams, lastError *string) error {
	airflow := params.FanAirflowReopenedAt
	if lastError == nil && params.FanAirflowReopenedAtOnSuccess != nil {
		airflow = params.FanAirflowReopenedAtOnSuccess
		params.ClearFanAirflowReopenedAt = false
	}
	rows, err := s.GetQueries(ctx).UpdateCurtailmentEventFanState(ctx, sqlc.UpdateCurtailmentEventFanStateParams{
		ID: eventID, ExpectedState: string(params.ExpectedEventState),
		FanOffSentAt: ptrToNullTime(params.FanOffSentAt), FanOnSentAt: ptrToNullTime(params.FanOnSentAt),
		FanAirflowReopenedAt: ptrToNullTime(airflow), ClearFanAirflowReopenedAt: params.ClearFanAirflowReopenedAt,
		FanLastError: ptrToNullString(lastError),
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

const fanRecoveryPending = "facility fan recovery is pending"

func markFanRecoveryPending(ctx context.Context, q sqlc.Querier, eventID int64, state models.EventState) error {
	rows, err := q.UpdateCurtailmentEventFanState(ctx, sqlc.UpdateCurtailmentEventFanStateParams{
		ID: eventID, ExpectedState: string(state), FanLastError: sql.NullString{String: fanRecoveryPending, Valid: true},
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) adminTerminateEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID, targetState models.EventState, reason string, command func(context.Context, *models.Event) *string) (*models.Event, bool, error) {
	initial, err := s.GetQueries(ctx).GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{EventUuid: eventUUID, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	if err != nil {
		return nil, false, err
	}
	result, err := withCurtailmentFanSession(ctx, s, initial.ID, initial.FacilityFanDeviceIds, func() (adminTerminateResult, error) {
		result, err := db.WithTransaction(ctx, s.conn.DB, func(q sqlc.Querier) (adminTerminateResult, error) {
			current, err := q.LockCurtailmentEventByUUIDForWrite(ctx, sqlc.LockCurtailmentEventByUUIDForWriteParams{EventUuid: eventUUID, OrgID: orgID})
			if err != nil {
				return adminTerminateResult{}, err
			}
			state := models.EventState(current.State)
			if state == targetState {
				return adminTerminateResult{event: convertEventRow(current)}, nil
			}
			if state.IsTerminal() {
				return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateStateConflict
			}
			inFlight, err := q.CurtailmentEventHasInFlightTargets(ctx, current.ID)
			if err != nil {
				return adminTerminateResult{}, err
			}
			if inFlight || state == models.EventStateActive {
				return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
			}
			if len(current.FacilityFanDeviceIds) > 0 && command == nil {
				return adminTerminateResult{}, fleeterror.NewFailedPreconditionError("facility fan recovery is required before admin termination")
			}
			updated, err := q.AdminTerminateCurtailmentEvent(ctx, sqlc.AdminTerminateCurtailmentEventParams{ID: current.ID, OrgID: orgID, TargetState: string(targetState)})
			if errors.Is(err, sql.ErrNoRows) {
				return adminTerminateResult{}, interfaces.ErrCurtailmentEventStateRaceLoss
			}
			if err != nil {
				return adminTerminateResult{}, err
			}
			if err := q.SweepCurtailmentTargetsToRestoreFailed(ctx, sqlc.SweepCurtailmentTargetsToRestoreFailedParams{CurtailmentEventID: current.ID, LastError: reason}); err != nil {
				return adminTerminateResult{}, err
			}
			if len(current.FacilityFanDeviceIds) > 0 {
				if err := markFanRecoveryPending(ctx, q, current.ID, targetState); err != nil {
					return adminTerminateResult{}, err
				}
				updated.FanLastError = sql.NullString{String: fanRecoveryPending, Valid: true}
			}
			return adminTerminateResult{event: convertEventRow(updated), transitioned: true}, nil
		})
		if err != nil {
			return result, err
		}
		if command == nil || len(result.event.FacilityFanDeviceIDs) == 0 || result.event.FanLastError == nil {
			return result, nil
		}
		// Terminal error evidence keeps the fan claim if the process dies before
		// the command or its acknowledgement is persisted.
		if err := s.checkFanRecoveryClaims(ctx, result.event.ID, orgID, result.event.FacilityFanDeviceIDs, result.event.FacilityFanSiteIDs); err != nil {
			return result, err
		}
		now := time.Now().UTC()
		params := interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: targetState}
		if result.event.FanOnSentAt == nil {
			params.FanOnSentAt = &now
		}
		lastError := command(ctx, result.event)
		if err := s.persistFanCommandResult(ctx, result.event.ID, params, lastError); err != nil {
			return result, err
		}
		result.event.FanLastError = lastError
		if params.FanOnSentAt != nil {
			result.event.FanOnSentAt = params.FanOnSentAt
		}
		return result, nil
	})
	return result.event, result.transitioned, err
}

func (s *SQLCurtailmentStore) checkFanRecoveryClaims(ctx context.Context, eventID, orgID int64, fanIDs, siteIDs []int64) error {
	if len(fanIDs) == 0 || len(uniqueSortedInt64s(fanIDs)) != len(fanIDs) || len(siteIDs) != len(fanIDs) {
		return fleeterror.NewInvalidArgumentError("fan recovery requires positive unique fan IDs and aligned site IDs")
	}
	return db.WithTransactionNoResult(ctx, s.conn.DB, func(q sqlc.Querier) error {
		current, err := q.GetCurtailmentEventForFanOperation(ctx, eventID)
		if err != nil {
			return err
		}
		if current.OrgID != orgID || len(current.FacilityFanDeviceIds) != len(fanIDs) || len(current.FacilityFanSiteIds) != len(siteIDs) {
			return fleeterror.NewFailedPreconditionError("facility fan recovery snapshot changed")
		}
		expectedSites := make(map[int64]int64, len(fanIDs))
		for i, fanID := range current.FacilityFanDeviceIds {
			expectedSites[fanID] = current.FacilityFanSiteIds[i]
		}
		for i, fanID := range fanIDs {
			if siteIDs[i] <= 0 || expectedSites[fanID] != siteIDs[i] {
				return fleeterror.NewFailedPreconditionError("facility fan recovery snapshot changed")
			}
		}

		claims, err := q.CountCurtailmentFanClaims(ctx, sqlc.CountCurtailmentFanClaimsParams{IncludeTerminalErrors: true, OrgID: orgID, ExcludedEventID: sql.NullInt64{Int64: eventID, Valid: true}, FacilityFanDeviceIds: fanIDs})
		if err != nil {
			return err
		}
		if claims > 0 {
			return fleeterror.NewFailedPreconditionError("facility fans are claimed by a newer active curtailment event; recover that event instead")
		}
		return nil
	})
}

func (s *SQLCurtailmentStore) ForceReleaseEventWithFanRecovery(ctx context.Context, orgID int64, eventUUID uuid.UUID, reason string, eventID int64, fanIDs, siteIDs []int64, params interfaces.UpdateCurtailmentFanStateParams, command func(context.Context) *string) (interfaces.ForceReleaseEventResult, error) {
	if params.ExpectedEventState != models.EventStateCancelled || command == nil {
		return interfaces.ForceReleaseEventResult{}, fleeterror.NewInvalidArgumentError("force-release fan recovery requires a command and cancelled expected state")
	}
	return withCurtailmentFanSession(ctx, s, eventID, fanIDs, func() (interfaces.ForceReleaseEventResult, error) {
		current, err := s.GetQueries(ctx).GetCurtailmentEventForFanOperation(ctx, eventID)
		if err != nil {
			return interfaces.ForceReleaseEventResult{}, err
		}
		if current.OrgID != orgID || current.EventUuid != eventUUID {
			return interfaces.ForceReleaseEventResult{}, interfaces.ErrCurtailmentEventStateRaceLoss
		}
		alreadyOn := fanOnAlreadyConfirmed(current)
		if models.EventState(current.State).IsTerminal() && alreadyOn {
			return interfaces.ForceReleaseEventResult{Event: convertEventRow(current)}, nil
		}
		if current.FanOnSentAt.Valid {
			params.FanOnSentAt = nil
		} else if params.FanOnSentAt == nil {
			now := time.Now().UTC()
			params.FanOnSentAt = &now
		}
		if err := s.checkFanRecoveryClaims(ctx, eventID, orgID, fanIDs, siteIDs); err != nil {
			return interfaces.ForceReleaseEventResult{}, err
		}
		result, err := db.WithTransaction(ctx, s.conn.DB, func(q sqlc.Querier) (interfaces.ForceReleaseEventResult, error) {
			result, err := s.forceReleaseEvent(ctx, q, orgID, eventUUID, reason)
			if err != nil {
				return result, err
			}
			if result.Event == nil || result.Event.ID != eventID || !result.Event.State.IsTerminal() {
				return result, interfaces.ErrCurtailmentEventStateRaceLoss
			}
			params.ExpectedEventState = result.Event.State
			if !alreadyOn {
				if err := markFanRecoveryPending(ctx, q, eventID, params.ExpectedEventState); err != nil {
					return interfaces.ForceReleaseEventResult{}, err
				}
			}
			return result, nil
		})
		if err != nil || alreadyOn {
			return result, err
		}
		lastError := command(ctx)
		if err := s.persistFanCommandResult(ctx, eventID, params, lastError); err != nil {
			return result, err
		}
		result.Event.FanLastError = lastError
		if params.FanOnSentAt != nil {
			result.Event.FanOnSentAt = params.FanOnSentAt
		}
		return result, nil
	})
}

func (s *SQLCurtailmentStore) RecoverTerminalFanState(ctx context.Context, eventID, orgID int64, fanIDs, siteIDs []int64, params interfaces.UpdateCurtailmentFanStateParams, command func(context.Context) *string) error {
	if !params.ExpectedEventState.IsTerminal() || command == nil {
		return fleeterror.NewInvalidArgumentError("terminal fan recovery requires a command and terminal expected state")
	}
	_, err := withCurtailmentFanSession(ctx, s, eventID, fanIDs, func() (struct{}, error) {
		current, err := s.GetQueries(ctx).GetCurtailmentEventForFanOperation(ctx, eventID)
		if err != nil {
			return struct{}{}, err
		}
		if current.OrgID != orgID || models.EventState(current.State) != params.ExpectedEventState {
			return struct{}{}, interfaces.ErrCurtailmentEventStateRaceLoss
		}
		if fanOnAlreadyConfirmed(current) {
			return struct{}{}, nil
		}
		if current.FanOnSentAt.Valid {
			params.FanOnSentAt = nil
		} else if params.FanOnSentAt == nil {
			now := time.Now().UTC()
			params.FanOnSentAt = &now
		}
		if err := s.checkFanRecoveryClaims(ctx, eventID, orgID, fanIDs, siteIDs); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, s.persistFanCommandResult(ctx, eventID, params, command(ctx))
	})
	return err
}
