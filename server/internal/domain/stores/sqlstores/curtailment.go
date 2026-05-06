package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

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
	row, err := s.GetQueries(ctx).GetCurtailmentOrgConfig(ctx, orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment org config not found for org %d", orgID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment org config: %v", err)
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

func (s *SQLCurtailmentStore) InsertEvent(ctx context.Context, params models.InsertEventParams) (*models.InsertEventResult, error) {
	row, err := s.GetQueries(ctx).InsertCurtailmentEvent(ctx, sqlc.InsertCurtailmentEventParams{
		EventUuid:               params.EventUUID,
		OrgID:                   params.OrgID,
		State:                   string(params.State),
		Mode:                    params.Mode,
		Strategy:                params.Strategy,
		Level:                   params.Level,
		Priority:                params.Priority,
		LoopType:                string(params.LoopType),
		ScopeType:               string(params.ScopeType),
		ScopeJsonb:              params.ScopeJSON,
		ModeParamsJsonb:         params.ModeParamsJSON,
		RestoreBatchSize:        params.RestoreBatchSize,
		RestoreBatchIntervalSec: params.RestoreBatchIntervalSec,
		MinCurtailedDurationSec: params.MinCurtailedDurationSec,
		MaxDurationSeconds:      ptrToNullInt32(params.MaxDurationSeconds),
		AllowUnbounded:          params.AllowUnbounded,
		IncludeMaintenance:      params.IncludeMaintenance,
		ForceIncludeMaintenance: params.ForceIncludeMaintenance,
		DecisionSnapshotJsonb:   params.DecisionSnapshotJSON,
		SourceActorType:         string(params.SourceActorType),
		SourceActorID:           ptrToNullString(params.SourceActorID),
		ExternalSource:          ptrToNullString(params.ExternalSource),
		ExternalReference:       ptrToNullString(params.ExternalReference),
		IdempotencyKey:          ptrToNullString(params.IdempotencyKey),
		Reason:                  params.Reason,
		ScheduledStartAt:        ptrToNullTime(params.ScheduledStartAt),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to insert curtailment event: %v", err)
	}
	return &models.InsertEventResult{
		ID:        row.ID,
		EventUUID: row.EventUuid,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
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

func (s *SQLCurtailmentStore) InsertTarget(ctx context.Context, params models.InsertTargetParams) error {
	err := s.GetQueries(ctx).InsertCurtailmentTarget(ctx, sqlc.InsertCurtailmentTargetParams{
		CurtailmentEventID:     params.CurtailmentEventID,
		DeviceIdentifier:       params.DeviceIdentifier,
		TargetType:             params.TargetType,
		State:                  string(params.State),
		DesiredState:           params.DesiredState,
		BaselinePowerW:         ptrFloat64ToNullString(params.BaselinePowerW),
		SelectorRationaleJsonb: rawMessageOrNullable(params.SelectorRationaleJSON),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to insert curtailment target: %v", err)
	}
	return nil
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

// convertEventRow maps a sqlc-generated event row to the domain Event type so
// the rest of the curtailment domain (selector, modes, handler) does not
// import sqlc-generated code.
func convertEventRow(row sqlc.CurtailmentEvent) *models.Event {
	return &models.Event{
		ID:                      row.ID,
		EventUUID:               row.EventUuid,
		OrgID:                   row.OrgID,
		State:                   models.EventState(row.State),
		Mode:                    row.Mode,
		Strategy:                row.Strategy,
		Level:                   row.Level,
		Priority:                row.Priority,
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
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

// convertTargetRow maps a sqlc-generated target row (which uses sql.NullString
// for the NUMERIC baseline_power_w / observed_power_w columns) to the domain
// Target type with explicit *float64.
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

// --- conversion helpers (curtailment-scoped; lift to a shared file when a
// second store needs the same shapes) ---

func ptrToNullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func ptrToNullInt32(p *int32) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *p, Valid: true}
}

func ptrToNullTime(p *time.Time) sql.NullTime {
	if p == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *p, Valid: true}
}

func nullInt32ToPtr(n sql.NullInt32) *int32 {
	if !n.Valid {
		return nil
	}
	v := n.Int32
	return &v
}

func nullInt64ToPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
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
