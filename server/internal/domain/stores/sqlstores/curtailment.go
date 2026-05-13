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

func mapOrgConfigError(err error, orgID int64) error {
	if err == nil {
		return nil
	}
	// EnsureCurtailmentOrgConfig gates both branches on
	// organization.deleted_at IS NULL, so soft-deleted/unknown orgs return
	// ErrNoRows. Map to NotFound so deleted tenants can't be revived.
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
	// Ensure-then-read: post-migration tenants don't have a seeded row.
	// EnsureCurtailmentOrgConfig is INSERT ... ON CONFLICT DO NOTHING with
	// a fallback SELECT in one CTE; both branches require the org to be
	// active. ErrNoRows means soft-deleted/unknown OR a READ COMMITTED race
	// (loser's snapshot missed the winner's INSERT) — retry resolves the
	// race; if it's the deletion case, mapOrgConfigError returns NotFound.
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

// InsertEventWithTargets writes event + targets in one transaction so a
// partial Start can't leave a pending event without its target set.
func (s *SQLCurtailmentStore) InsertEventWithTargets(
	ctx context.Context,
	event models.InsertEventParams,
	targets []models.InsertTargetParams,
) (*models.InsertEventResult, error) {
	if len(targets) == 0 {
		// Defense-in-depth; service rejects empty plans upstream.
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
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to insert curtailment event: %v", err)
		}
		for _, t := range targets {
			err := q.InsertCurtailmentTarget(ctx, sqlc.InsertCurtailmentTargetParams{
				CurtailmentEventID:     row.ID,
				DeviceIdentifier:       t.DeviceIdentifier,
				TargetType:             t.TargetType,
				State:                  string(t.State),
				DesiredState:           t.DesiredState,
				BaselinePowerW:         ptrFloat64ToNullString(t.BaselinePowerW),
				SelectorRationaleJsonb: rawMessageOrNullable(t.SelectorRationaleJSON),
			})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf(
					"failed to insert curtailment target %s: %v", t.DeviceIdentifier, err,
				)
			}
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

func (s *SQLCurtailmentStore) UpdateEventState(ctx context.Context, eventID int64, state models.EventState, startedAt *time.Time, endedAt *time.Time) error {
	if err := s.GetQueries(ctx).UpdateCurtailmentEventState(ctx, sqlc.UpdateCurtailmentEventStateParams{
		ID:        eventID,
		State:     string(state),
		StartedAt: ptrToNullTime(startedAt),
		EndedAt:   ptrToNullTime(endedAt),
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to update curtailment event %d state: %v", eventID, err)
	}
	return nil
}

func (s *SQLCurtailmentStore) UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	if err := s.GetQueries(ctx).UpdateCurtailmentTargetState(ctx, sqlc.UpdateCurtailmentTargetStateParams{
		CurtailmentEventID: eventID,
		DeviceIdentifier:   deviceIdentifier,
		State:              string(params.State),
		LastDispatchedAt:   ptrToNullTime(params.LastDispatchedAt),
		LastBatchUuid:      ptrToNullString(params.LastBatchUUID),
		ObservedPowerW:     ptrFloat64ToNullString(params.ObservedPowerW),
		ObservedAt:         ptrToNullTime(params.ObservedAt),
		ConfirmedAt:        ptrToNullTime(params.ConfirmedAt),
		RetryCount:         ptrToNullInt32(params.RetryCount),
		LastError:          ptrToNullString(params.LastError),
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to update curtailment target (%d, %s) state: %v", eventID, deviceIdentifier, err)
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

// convertTargetRow maps a sqlc target row (sql.NullString for NUMERIC
// baseline_power_w / observed_power_w) to the domain Target with *float64.
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

func nullFloat64ToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

// ptrFloat64ToNullString formats a *float64 for a NUMERIC column. NUMERIC
// values arrive at the database/sql boundary as strings; sqlc maps them to
// sql.NullString. NULL maps to !Valid; non-NULL formats with full precision
// so a 12.3 round-trip preserves three decimal places.
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
		// A non-NULL NUMERIC column that doesn't parse signals real data
		// corruption or a schema/driver mismatch. Surface it via the same
		// slog.Warn pattern other sqlstores use; keep returning nil so the
		// read path stays tolerant of one-off corruption (the selector
		// treats this as "unknown efficiency" and ranks it last).
		slog.Warn("failed to parse NUMERIC string", "value", n.String, "err", err)
		return nil
	}
	return &v
}

// rawMessageOrNullable wraps a raw JSON byte slice into pqtype.NullRawMessage,
// treating nil/empty as NULL so the JSONB column receives SQL NULL rather than
// the literal "null" or empty string.
func rawMessageOrNullable(b []byte) pqtype.NullRawMessage {
	if len(b) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: json.RawMessage(b), Valid: true}
}

func nullRawMessageToBytes(n pqtype.NullRawMessage) []byte {
	if !n.Valid {
		return nil
	}
	return []byte(n.RawMessage)
}
