package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// pgErrCodeForeignKeyViolation is PostgreSQL's SQLSTATE for foreign_key_violation.
const pgErrCodeForeignKeyViolation = "23503"

// Partial-unique-index names used to map a unique-violation into a typed
// sentinel (replay path or AlreadyExists) instead of leaking Internal.
const nonTerminalEventPerOrgUniqueIndex = "uq_curtailment_event_one_non_terminal_per_org"

const (
	idempotencyKeyUniqueIndex    = "uq_curtailment_event_idempotency"
	externalReferenceUniqueIndex = "uq_curtailment_event_external_ref"
)

func mapOrgConfigError(err error, orgID int64) error {
	if err == nil {
		return nil
	}
	// EnsureCurtailmentOrgConfig requires organization.deleted_at IS NULL;
	// ErrNoRows means soft-deleted/unknown.
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundErrorf("organization %d not found", orgID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeForeignKeyViolation {
		return fleeterror.NewNotFoundErrorf("organization %d not found", orgID)
	}
	return fleeterror.NewInternalErrorf("failed to get curtailment org config: %v", err)
}

var _ interfaces.CurtailmentStore = &SQLCurtailmentStore{}

type SQLCurtailmentStore struct {
	SQLConnectionManager
}

func NewSQLCurtailmentStore(conn *sql.DB) *SQLCurtailmentStore {
	return &SQLCurtailmentStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLCurtailmentStore) GetOrgConfig(ctx context.Context, orgID int64) (*models.OrgConfig, error) {
	// Ensure-then-read seeds post-migration tenants. One retry covers a
	// READ COMMITTED race where the loser's snapshot missed the winner's
	// INSERT; the deletion case maps to NotFound via mapOrgConfigError.
	row, err := s.GetQueries(ctx).EnsureCurtailmentOrgConfig(ctx, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		row, err = s.GetQueries(ctx).EnsureCurtailmentOrgConfig(ctx, orgID)
	}
	if err != nil {
		return nil, mapOrgConfigError(err, orgID)
	}
	return &models.OrgConfig{
		OrgID:                 row.OrgID,
		MaxDurationDefaultSec: row.MaxDurationDefaultSec,
		CandidateMinPowerW:    row.CandidateMinPowerW,
		PostEventCooldownSec:  row.PostEventCooldownSec,
	}, nil
}

func (s *SQLCurtailmentStore) ListActiveCurtailedDevices(ctx context.Context, orgID int64) ([]string, error) {
	devices, err := s.GetQueries(ctx).ListActiveCurtailedDevicesByOrg(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list active curtailed devices: %v", err)
	}
	return devices, nil
}

func (s *SQLCurtailmentStore) ListRecentlyResolvedCurtailedDevices(ctx context.Context, orgID int64, cooldownSec int32) ([]string, error) {
	devices, err := s.GetQueries(ctx).ListRecentlyResolvedCurtailedDevicesByOrg(ctx, sqlc.ListRecentlyResolvedCurtailedDevicesByOrgParams{
		OrgID:       orgID,
		CooldownSec: cooldownSec,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list recently resolved curtailed devices: %v", err)
	}
	return devices, nil
}

// InsertEventWithTargets writes event + targets in one transaction.
func (s *SQLCurtailmentStore) InsertEventWithTargets(
	ctx context.Context,
	event models.InsertEventParams,
	targets []models.InsertTargetParams,
) (*models.InsertEventResult, error) {
	if len(targets) == 0 {
		return nil, fleeterror.NewInvalidArgumentError(
			"InsertEventWithTargets requires a non-empty targets slice",
		)
	}
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.InsertEventResult, error) {
		row, err := q.InsertCurtailmentEvent(ctx, sqlc.InsertCurtailmentEventParams{
			EventUuid:               event.EventUUID,
			OrgID:                   event.OrgID,
			State:                   string(event.State),
			Mode:                    string(event.Mode),
			Strategy:                string(event.Strategy),
			Level:                   string(event.Level),
			Priority:                string(event.Priority),
			LoopType:                string(event.LoopType),
			ScopeType:               string(event.ScopeType),
			ScopeJsonb:              event.ScopeJSON,
			ModeParamsJsonb:         event.ModeParamsJSON,
			RestoreBatchSize:        event.RestoreBatchSize,
			RestoreBatchIntervalSec: event.RestoreBatchIntervalSec,
			MinCurtailedDurationSec: event.MinCurtailedDurationSec,
			MaxDurationSeconds:      ptrToNullInt32(event.MaxDurationSeconds),
			AllowUnbounded:          event.AllowUnbounded,
			IncludeMaintenance:      event.IncludeMaintenance,
			ForceIncludeMaintenance: event.ForceIncludeMaintenance,
			DecisionSnapshotJsonb:   event.DecisionSnapshotJSON,
			SourceActorType:         string(event.SourceActorType),
			SourceActorID:           ptrToNullString(event.SourceActorID),
			ExternalSource:          ptrToNullString(event.ExternalSource),
			ExternalReference:       ptrToNullString(event.ExternalReference),
			IdempotencyKey:          ptrToNullString(event.IdempotencyKey),
			Reason:                  event.Reason,
			ScheduledStartAt:        ptrToNullTime(event.ScheduledStartAt),
			CreatedByUserID:         event.CreatedByUserID,
			EffectiveBatchSize:      sql.NullInt32{Int32: event.EffectiveBatchSize, Valid: true},
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUniqueViolation {
				switch pgErr.ConstraintName {
				case nonTerminalEventPerOrgUniqueIndex:
					return nil, interfaces.ErrCurtailmentNonTerminalEventExists
				case idempotencyKeyUniqueIndex, externalReferenceUniqueIndex:
					// Replay path: caller re-issues the matching lookup.
					return nil, interfaces.ErrCurtailmentReplayRaceLoss
				}
				// Unknown constraint: sanitize the response and log the
				// name server-side so it doesn't leak through %v.
				slog.Error("curtailment_event insert hit unknown unique constraint",
					"constraint", pgErr.ConstraintName, "org_id", event.OrgID, "event_uuid", event.EventUUID)
				return nil, fleeterror.NewAlreadyExistsError("curtailment event already exists")
			}
			return nil, fleeterror.NewInternalErrorf("failed to insert curtailment event: %v", err)
		}
		payload, err := buildBulkTargetPayload(targets)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf(
				"failed to encode curtailment target payload: %v", err,
			)
		}
		inserted, err := q.BulkInsertCurtailmentTargets(ctx, sqlc.BulkInsertCurtailmentTargetsParams{
			CurtailmentEventID: row.ID,
			TargetsJsonb:       payload,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to bulk insert curtailment targets: %v", err)
		}
		if inserted != int64(len(targets)) {
			// jsonb_to_recordset silently drops rows that fail column-type
			// cast; bail so the tx rolls back instead of partial fanout.
			return nil, fleeterror.NewInternalErrorf(
				"bulk insert wrote %d targets, expected %d", inserted, len(targets),
			)
		}
		return &models.InsertEventResult{
			ID:        row.ID,
			EventUUID: row.EventUuid,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}, nil
	})
}

func (s *SQLCurtailmentStore) GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
		EventUuid: eventUUID,
		OrgID:     orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
	}
	return convertEventRow(row), nil
}

func (s *SQLCurtailmentStore) GetActiveEvent(ctx context.Context, orgID int64) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetActiveCurtailmentEvent(ctx, orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to get active curtailment event for org %d: %v", orgID, err)
	}
	return convertEventRow(row), nil
}

func (s *SQLCurtailmentStore) GetEventByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventByIdempotencyKey(ctx, sqlc.GetCurtailmentEventByIdempotencyKeyParams{
		OrgID:          orgID,
		IdempotencyKey: sql.NullString{String: idempotencyKey, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to look up curtailment event by idempotency_key: %v", err)
	}
	return convertEventRow(row), nil
}

func (s *SQLCurtailmentStore) GetEventByExternalReference(ctx context.Context, orgID int64, externalSource, externalReference string) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventByExternalReference(ctx, sqlc.GetCurtailmentEventByExternalReferenceParams{
		OrgID:             orgID,
		ExternalSource:    sql.NullString{String: externalSource, Valid: true},
		ExternalReference: sql.NullString{String: externalReference, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to look up curtailment event by (external_source, external_reference): %v", err)
	}
	return convertEventRow(row), nil
}

const (
	curtailmentEventsDefaultPageSize int32 = 50
	curtailmentEventsMaxPageSize     int32 = 200
)

func (s *SQLCurtailmentStore) ListEvents(ctx context.Context, params interfaces.ListEventsParams) ([]*models.Event, string, error) {
	cursor, err := decodeCurtailmentEventCursor(params.PageToken)
	if err != nil {
		return nil, "", err
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = curtailmentEventsDefaultPageSize
	}
	if pageSize > curtailmentEventsMaxPageSize {
		pageSize = curtailmentEventsMaxPageSize
	}

	var cursorID int64
	if cursor != nil {
		if cursor.OrgID != params.OrgID || cursor.StateFilter != params.StateFilter {
			return nil, "", fleeterror.NewInvalidArgumentError("page_token does not match org_id or state_filter")
		}
		cursorID = cursor.ID
	}

	rows, err := s.GetQueries(ctx).ListCurtailmentEventsForOrg(ctx, sqlc.ListCurtailmentEventsForOrgParams{
		OrgID:       params.OrgID,
		CursorID:    cursorID,
		StateFilter: string(params.StateFilter),
		// Over-fetch by one so the caller knows whether another page remains.
		RowLimit: int64(pageSize) + 1,
	})
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("failed to list curtailment events: %v", err)
	}

	var nextToken string
	if int64(len(rows)) > int64(pageSize) {
		// Trim the over-fetched row; cursor points at the last id.
		rows = rows[:pageSize]
		nextToken = encodeCurtailmentEventCursor(&curtailmentEventCursor{
			ID:          rows[len(rows)-1].ID,
			OrgID:       params.OrgID,
			StateFilter: params.StateFilter,
		})
	}

	out := make([]*models.Event, len(rows))
	for i, row := range rows {
		// ListCurtailmentEventsForOrgRow's field layout matches
		// sqlc.CurtailmentEvent (different name only because the query
		// projects a derived snapshot expression); sqlc regen fails the
		// build if these ever drift.
		out[i] = convertEventRow(sqlc.CurtailmentEvent(row))
	}
	return out, nextToken, nil
}

func (s *SQLCurtailmentStore) UpdateOperatorFields(ctx context.Context, eventID, orgID int64, params interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	row, err := s.GetQueries(ctx).UpdateCurtailmentEventOperatorFields(ctx, sqlc.UpdateCurtailmentEventOperatorFieldsParams{
		ID:                      eventID,
		OrgID:                   orgID,
		Reason:                  nullStringFromPtr(params.Reason),
		RestoreBatchSize:        nullInt32FromPtr(params.RestoreBatchSize),
		RestoreBatchIntervalSec: nullInt32FromPtr(params.RestoreBatchIntervalSec),
		MaxDurationSeconds:      nullInt32FromPtr(params.MaxDurationSeconds),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, interfaces.ErrCurtailmentEventStateRaceLoss
		}
		return nil, fleeterror.NewInternalErrorf("failed to update curtailment event: %v", err)
	}
	return convertEventRow(row), nil
}

func nullInt32FromPtr(p *int32) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *p, Valid: true}
}

// AdminTerminateEvent transactionally flips the event to targetState and
// sweeps non-terminal targets to RESTORE_FAILED with reason as last_error.
// Routes: same target_state → idempotent echo; different terminal state →
// StateConflict; any in-flight target → ActiveEvent (caller must Stop first).
//
// transitioned=false marks the idempotent-echo paths (initial-read or
// race-loss re-read) so the caller can suppress side effects.
type adminTerminateResult struct {
	event        *models.Event
	transitioned bool
}

func (s *SQLCurtailmentStore) AdminTerminateEvent(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
	targetState models.EventState,
	reason string,
) (*models.Event, bool, error) {
	result, err := db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (adminTerminateResult, error) {
		current, err := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
			EventUuid: eventUUID,
			OrgID:     orgID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return adminTerminateResult{}, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		if err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
		}

		currentState := models.EventState(current.State)
		if currentState == targetState {
			// Idempotent echo: event already in the requested terminal state.
			return adminTerminateResult{event: convertEventRow(current), transitioned: false}, nil
		}
		if currentState.IsTerminal() {
			return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateStateConflict
		}

		// In-flight gate: reject if any target still has an outstanding
		// Curtail. Subsumes the ACTIVE check and catches mid-dispatch
		// PENDING events.
		hasInFlight, err := q.CurtailmentEventHasInFlightTargets(ctx, current.ID)
		if err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to check in-flight targets: %v", err)
		}
		if hasInFlight {
			return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
		}

		updated, err := q.AdminTerminateCurtailmentEvent(ctx, sqlc.AdminTerminateCurtailmentEventParams{
			ID:          current.ID,
			OrgID:       orgID,
			TargetState: string(targetState),
		})
		if errors.Is(err, sql.ErrNoRows) {
			// Race: UPDATE matched 0 rows under the state guard. Re-read
			// and route by latest state for idempotent echo.
			latest, getErr := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
				EventUuid: eventUUID,
				OrgID:     orgID,
			})
			if getErr != nil {
				return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to re-read curtailment event after concurrent state change: %v", getErr)
			}
			latestState := models.EventState(latest.State)
			if latestState == targetState {
				// Idempotent echo: concurrent terminate landed first.
				return adminTerminateResult{event: convertEventRow(latest), transitioned: false}, nil
			}
			hasInFlight, gateErr := q.CurtailmentEventHasInFlightTargets(ctx, current.ID)
			if gateErr != nil {
				return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to check in-flight targets after terminate race: %v", gateErr)
			}
			if hasInFlight {
				return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
			}
			if latestState == models.EventStateActive {
				return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
			}
			return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateStateConflict
		}
		if err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to terminate curtailment event: %v", err)
		}

		if err := q.SweepCurtailmentTargetsToRestoreFailed(ctx, sqlc.SweepCurtailmentTargetsToRestoreFailedParams{
			CurtailmentEventID: current.ID,
			LastError:          reason,
		}); err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to sweep curtailment targets: %v", err)
		}

		return adminTerminateResult{event: convertEventRow(updated), transitioned: true}, nil
	})
	if err != nil {
		return nil, false, err
	}
	return result.event, result.transitioned, nil
}

func (s *SQLCurtailmentStore) ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentTargetsByEvent(ctx, sqlc.ListCurtailmentTargetsByEventParams{
		OrgID:     orgID,
		EventUuid: eventUUID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment targets: %v", err)
	}
	targets := make([]*models.Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, convertTargetRow(row))
	}
	return targets, nil
}

func (s *SQLCurtailmentStore) ListCandidates(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]*models.Candidate, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentCandidatesByOrg(ctx, sqlc.ListCurtailmentCandidatesByOrgParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment candidates: %v", err)
	}
	out := make([]*models.Candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, &models.Candidate{
			DeviceIdentifier: row.DeviceIdentifier,
			DriverName:       nullStringToPtr(row.DriverName),
			Model:            row.Model,
			DeviceStatus:     row.DeviceStatus,
			PairingStatus:    row.PairingStatus,
			LatestMetricsAt:  nullTimeToPtr(row.LatestMetricsAt),
			LatestPowerW:     nullFloat64ToPtr(row.LatestPowerW),
			LatestHashRateHS: nullFloat64ToPtr(row.LatestHashRateHs),
			AvgEfficiencyJH:  nullFloat64ToPtr(row.AvgEfficiency),
		})
	}
	return out, nil
}

func (s *SQLCurtailmentStore) ListNonTerminalEvents(ctx context.Context) ([]*models.Event, error) {
	rows, err := s.GetQueries(ctx).ListNonTerminalCurtailmentEvents(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list non-terminal curtailment events: %v", err)
	}
	out := make([]*models.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, convertEventRow(row))
	}
	return out, nil
}

func (s *SQLCurtailmentStore) UpdateEventState(ctx context.Context, eventID int64, expectedState models.EventState, state models.EventState, startedAt *time.Time, endedAt *time.Time) error {
	rows, err := s.GetQueries(ctx).UpdateCurtailmentEventState(ctx, sqlc.UpdateCurtailmentEventStateParams{
		ID:            eventID,
		ExpectedState: string(expectedState),
		State:         string(state),
		StartedAt:     ptrToNullTime(startedAt),
		EndedAt:       ptrToNullTime(endedAt),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update curtailment event %d state: %v", eventID, err)
	}
	if rows == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	rows, err := s.GetQueries(ctx).UpdateCurtailmentTargetState(ctx, sqlc.UpdateCurtailmentTargetStateParams{
		CurtailmentEventID:   eventID,
		DeviceIdentifier:     deviceIdentifier,
		State:                string(params.State),
		LastDispatchedAt:     ptrToNullTime(params.LastDispatchedAt),
		LastBatchUuid:        ptrToNullString(params.LastBatchUUID),
		ObservedPowerW:       ptrFloat64ToNullString(params.ObservedPowerW),
		ObservedAt:           ptrToNullTime(params.ObservedAt),
		ConfirmedAt:          ptrToNullTime(params.ConfirmedAt),
		RetryCount:           ptrToNullInt32(params.RetryCount),
		LastError:            ptrToNullString(params.LastError),
		ExpectedEventState:   ptrEventStateToNullString(params.ExpectedEventState),
		ExpectedDesiredState: ptrToNullString(params.ExpectedDesiredState),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update curtailment target (%d, %s) state: %v", eventID, deviceIdentifier, err)
	}
	if rows == 0 {
		// Zero rows: either the parent event advanced to terminal (EXISTS
		// guard) or expected_desired_state lost the race against a Stop.
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) BumpTargetRetry(ctx context.Context, eventID int64, deviceIdentifier string) error {
	rows, err := s.GetQueries(ctx).BumpCurtailmentTargetRetry(ctx, sqlc.BumpCurtailmentTargetRetryParams{
		CurtailmentEventID: eventID,
		DeviceIdentifier:   deviceIdentifier,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to bump curtailment target retry (%d, %s): %v", eventID, deviceIdentifier, err)
	}
	if rows == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) UpsertHeartbeat(ctx context.Context, params interfaces.UpsertCurtailmentHeartbeatParams) error {
	if err := s.GetQueries(ctx).UpsertCurtailmentReconcilerHeartbeat(ctx, sqlc.UpsertCurtailmentReconcilerHeartbeatParams{
		LastTickAt:         params.LastTickAt,
		LastTickUuid:       params.LastTickUUID,
		LastTickDurationMs: ptrToNullInt32(params.LastTickDurationMS),
		ActiveEventCount:   params.ActiveEventCount,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert curtailment heartbeat: %v", err)
	}
	return nil
}

// BeginRestoreTransition runs the event-state flip + target reset in one tx.
// Pre-reads the event to distinguish "already restoring" (idempotent
// return) from "already terminal" (FailedPrecondition); the UPDATE's
// state guard catches concurrent transitions between pre-read and write.
func (s *SQLCurtailmentStore) BeginRestoreTransition(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
) (*models.Event, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Event, error) {
		current, err := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
			EventUuid: eventUUID,
			OrgID:     orgID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
		}

		state := models.EventState(current.State)
		if state == models.EventStateRestoring {
			// Idempotent re-Stop: leave targets alone.
			return convertEventRow(current), nil
		}
		if state.IsTerminal() {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"cannot stop curtailment event %s in terminal state %q",
				eventUUID, current.State,
			)
		}

		updated, err := q.BeginCurtailmentRestoration(ctx, current.ID)
		if errors.Is(err, sql.ErrNoRows) {
			// Concurrent transition between pre-read and update: re-read and
			// route by the latest state so terminal races don't silently echo
			// success.
			latest, getErr := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
				EventUuid: eventUUID,
				OrgID:     orgID,
			})
			if getErr != nil {
				return nil, fleeterror.NewInternalErrorf("failed to re-read curtailment event after concurrent state change: %v", getErr)
			}
			latestState := models.EventState(latest.State)
			if latestState.IsTerminal() {
				return nil, fleeterror.NewFailedPreconditionErrorf(
					"cannot stop curtailment event %s in terminal state %q",
					eventUUID, latest.State,
				)
			}
			if latestState == models.EventStateRestoring {
				// Idempotent re-Stop: first call's sizing wins.
				return convertEventRow(latest), nil
			}
			return nil, fleeterror.NewInternalErrorf(
				"unexpected event state after concurrent transition: %q", latest.State,
			)
		}
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to begin curtailment restoration: %v", err)
		}

		if err := q.ResetCurtailmentTargetsForRestore(ctx, current.ID); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to reset curtailment targets for restore: %v", err)
		}

		return convertEventRow(updated), nil
	})
}

func (s *SQLCurtailmentStore) GetHeartbeat(ctx context.Context) (*models.Heartbeat, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentReconcilerHeartbeat(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("curtailment reconciler heartbeat row missing (migration seed should have created it)")
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment heartbeat: %v", err)
	}
	return &models.Heartbeat{
		ID:                 row.ID,
		LastTickAt:         row.LastTickAt,
		LastTickUUID:       row.LastTickUuid,
		LastTickDurationMS: nullInt32ToPtr(row.LastTickDurationMs),
		ActiveEventCount:   row.ActiveEventCount,
	}, nil
}

// convertEventRow maps a sqlc row to the domain Event so callers outside
// the store don't import sqlc-generated code.
func convertEventRow(row sqlc.CurtailmentEvent) *models.Event {
	return &models.Event{
		ID:                      row.ID,
		EventUUID:               row.EventUuid,
		OrgID:                   row.OrgID,
		State:                   models.EventState(row.State),
		Mode:                    models.Mode(row.Mode),
		Strategy:                models.Strategy(row.Strategy),
		Level:                   models.Level(row.Level),
		Priority:                models.Priority(row.Priority),
		LoopType:                models.LoopType(row.LoopType),
		ScopeType:               models.ScopeType(row.ScopeType),
		ScopeJSON:               row.ScopeJsonb,
		ModeParamsJSON:          row.ModeParamsJsonb,
		RestoreBatchSize:        row.RestoreBatchSize,
		RestoreBatchIntervalSec: row.RestoreBatchIntervalSec,
		EffectiveBatchSize:      nullInt32ToPtr(row.EffectiveBatchSize),
		MinCurtailedDurationSec: row.MinCurtailedDurationSec,
		MaxDurationSeconds:      nullInt32ToPtr(row.MaxDurationSeconds),
		AllowUnbounded:          row.AllowUnbounded,
		IncludeMaintenance:      row.IncludeMaintenance,
		ForceIncludeMaintenance: row.ForceIncludeMaintenance,
		DecisionSnapshotJSON:    row.DecisionSnapshotJsonb,
		SourceActorType:         models.SourceActorType(row.SourceActorType),
		SourceActorID:           nullStringToPtr(row.SourceActorID),
		ExternalSource:          nullStringToPtr(row.ExternalSource),
		ExternalReference:       nullStringToPtr(row.ExternalReference),
		IdempotencyKey:          nullStringToPtr(row.IdempotencyKey),
		SupersedesEventID:       nullInt64ToPtr(row.SupersedesEventID),
		Reason:                  row.Reason,
		ScheduledStartAt:        nullTimeToPtr(row.ScheduledStartAt),
		StartedAt:               nullTimeToPtr(row.StartedAt),
		EndedAt:                 nullTimeToPtr(row.EndedAt),
		CreatedByUserID:         row.CreatedByUserID,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

// convertTargetRow maps a sqlc target row to the domain Target.
func convertTargetRow(row sqlc.CurtailmentTarget) *models.Target {
	return &models.Target{
		CurtailmentEventID:    row.CurtailmentEventID,
		DeviceIdentifier:      row.DeviceIdentifier,
		TargetType:            row.TargetType,
		State:                 models.TargetState(row.State),
		DesiredState:          row.DesiredState,
		BaselinePowerW:        nullStringToFloat64Ptr(row.BaselinePowerW),
		AddedAt:               row.AddedAt,
		ReleasedAt:            nullTimeToPtr(row.ReleasedAt),
		LastDispatchedAt:      nullTimeToPtr(row.LastDispatchedAt),
		LastBatchUUID:         nullStringToPtr(row.LastBatchUuid),
		ObservedPowerW:        nullStringToFloat64Ptr(row.ObservedPowerW),
		ObservedAt:            nullTimeToPtr(row.ObservedAt),
		ConfirmedAt:           nullTimeToPtr(row.ConfirmedAt),
		RetryCount:            row.RetryCount,
		LastError:             nullStringToPtr(row.LastError),
		SelectorRationaleJSON: nullRawMessageToBytes(row.SelectorRationaleJsonb),
	}
}

// --- curtailment-specific conversion helpers ---
// (generic helpers moved to helpers.go so site/building/curtailment
// stores share one canonical implementation)

func nullInt32ToPtr(n sql.NullInt32) *int32 {
	if !n.Valid {
		return nil
	}
	v := n.Int32
	return &v
}

func ptrEventStateToNullString(p *models.EventState) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*p), Valid: true}
}

func nullFloat64ToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

// ptrFloat64ToNullString formats a *float64 for a NUMERIC column.
// database/sql sends NUMERIC values as strings; full precision preserves
// the three-decimal round-trip.
func ptrFloat64ToNullString(p *float64) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{
		String: strconv.FormatFloat(*p, 'f', -1, 64),
		Valid:  true,
	}
}

func nullStringToFloat64Ptr(n sql.NullString) *float64 {
	if !n.Valid {
		return nil
	}
	v, err := strconv.ParseFloat(n.String, 64)
	if err != nil {
		// Corruption or driver mismatch: log, return nil so the selector
		// treats it as unknown and ranks it last.
		slog.Warn("failed to parse NUMERIC string", "value", n.String, "err", err)
		return nil
	}
	return &v
}

// bulkInsertTargetRow is the per-target JSON shape consumed by
// BulkInsertCurtailmentTargets via jsonb_to_recordset. Field names match
// the recordset column definitions.
type bulkInsertTargetRow struct {
	DeviceIdentifier       string          `json:"device_identifier"`
	TargetType             string          `json:"target_type"`
	State                  string          `json:"state"`
	DesiredState           string          `json:"desired_state"`
	BaselinePowerW         *float64        `json:"baseline_power_w"`
	SelectorRationaleJsonb json.RawMessage `json:"selector_rationale_jsonb,omitempty"`
}

// buildBulkTargetPayload serializes targets into the JSONB array for
// BulkInsertCurtailmentTargets. baseline_power_w rides as JSON number;
// NUMERIC(12,3) holds float64 precision losslessly at fleet scale.
func buildBulkTargetPayload(targets []models.InsertTargetParams) ([]byte, error) {
	rows := make([]bulkInsertTargetRow, len(targets))
	for i, t := range targets {
		var rationale json.RawMessage
		if len(t.SelectorRationaleJSON) > 0 {
			rationale = json.RawMessage(t.SelectorRationaleJSON)
		}
		rows[i] = bulkInsertTargetRow{
			DeviceIdentifier:       t.DeviceIdentifier,
			TargetType:             t.TargetType,
			State:                  string(t.State),
			DesiredState:           t.DesiredState,
			BaselinePowerW:         t.BaselinePowerW,
			SelectorRationaleJsonb: rationale,
		}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("encode bulk target payload: %v", err)
	}
	return payload, nil
}

func nullRawMessageToBytes(n pqtype.NullRawMessage) []byte {
	if !n.Valid {
		return nil
	}
	return []byte(n.RawMessage)
}
