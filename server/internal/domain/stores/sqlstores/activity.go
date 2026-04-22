package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// pgErrCodeUniqueViolation is PostgreSQL's SQLSTATE for unique_violation.
const pgErrCodeUniqueViolation = "23505"

// completedBatchUniqueIndex is the partial unique index on
// (batch_id, event_type) scoped to '%.completed' rows. We only swallow
// unique-violation errors coming from this specific index so that a future
// constraint added to activity_log cannot be accidentally swallowed too.
const completedBatchUniqueIndex = "uq_activity_log_batch_completed"

var _ interfaces.ActivityStore = &SQLActivityStore{}

type SQLActivityStore struct {
	SQLConnectionManager
}

func NewSQLActivityStore(conn *sql.DB) *SQLActivityStore {
	return &SQLActivityStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLActivityStore) Insert(ctx context.Context, event *models.Event) error {
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generating activity event ID: %w", err)
	}

	metadata, err := marshalMetadata(event.Metadata)
	if err != nil {
		return err
	}

	err = s.GetQueries(ctx).InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
		EventID:        eventID,
		EventCategory:  string(event.Category),
		EventType:      event.Type,
		Description:    event.Description,
		Result:         string(event.Result),
		ErrorMessage:   nullStringFromPtr(event.ErrorMessage),
		ScopeType:      nullStringFromPtr(event.ScopeType),
		ScopeLabel:     nullStringFromPtr(event.ScopeLabel),
		ScopeCount:     nullInt32FromIntPtr(event.ScopeCount),
		ActorType:      string(event.ActorType),
		UserID:         nullStringFromPtr(event.UserID),
		Username:       nullStringFromPtr(event.Username),
		OrganizationID: nullInt64FromPtr(event.OrganizationID),
		Metadata:       metadata,
		BatchID:        nullStringFromPtr(event.BatchID),
	})
	if err != nil && isCompletedBatchDuplicate(event, err) {
		// A concurrent finalizer retry already wrote this completion row;
		// treat it as success so retries are no-ops.
		return nil
	}
	return err
}

// isCompletedBatchDuplicate reports whether err is the unique_violation raised
// by uq_activity_log_batch_completed for a '*.completed' insert.
func isCompletedBatchDuplicate(event *models.Event, err error) bool {
	if event == nil || event.BatchID == nil || !strings.HasSuffix(event.Type, models.CompletedEventSuffix) {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrCodeUniqueViolation && pgErr.ConstraintName == completedBatchUniqueIndex
}

func (s *SQLActivityStore) List(ctx context.Context, filter models.Filter) ([]models.Entry, error) {
	rows, err := s.GetQueries(ctx).ListActivityLogs(ctx, toListParams(filter))
	if err != nil {
		return nil, err
	}

	entries := make([]models.Entry, len(rows))
	for i, row := range rows {
		entries[i] = rowToEntry(row)
	}
	return entries, nil
}

func (s *SQLActivityStore) Count(ctx context.Context, filter models.Filter) (int64, error) {
	return s.GetQueries(ctx).CountActivityLogs(ctx, toCountParams(filter))
}

func (s *SQLActivityStore) GetDistinctUsers(ctx context.Context, orgID int64) ([]models.UserInfo, error) {
	rows, err := s.GetQueries(ctx).GetDistinctActivityUsers(ctx, validNullInt64(orgID))
	if err != nil {
		return nil, err
	}

	var users []models.UserInfo
	for _, row := range rows {
		if !row.UserID.Valid {
			continue
		}
		// Fall back to user_id when username is NULL so the user still
		// appears in filter dropdowns. Service.Log warns when this happens.
		username := row.UserID.String
		if row.Username.Valid {
			username = row.Username.String
		}
		users = append(users, models.UserInfo{
			UserID:   row.UserID.String,
			Username: username,
		})
	}
	return users, nil
}

func (s *SQLActivityStore) GetDistinctEventTypes(ctx context.Context, orgID int64) ([]models.EventTypeInfo, error) {
	rows, err := s.GetQueries(ctx).GetDistinctEventTypes(ctx, validNullInt64(orgID))
	if err != nil {
		return nil, err
	}

	result := make([]models.EventTypeInfo, len(rows))
	for i, row := range rows {
		result[i] = models.EventTypeInfo{
			EventType:     row.EventType,
			EventCategory: row.EventCategory,
		}
	}
	return result, nil
}

func (s *SQLActivityStore) GetDistinctScopeTypes(ctx context.Context, orgID int64) ([]string, error) {
	rows, err := s.GetQueries(ctx).GetDistinctScopeTypes(ctx, validNullInt64(orgID))
	if err != nil {
		return nil, err
	}

	var result []string
	for _, row := range rows {
		if row.Valid {
			result = append(result, row.String)
		}
	}
	return result, nil
}

// --- filter mapping ---

func toListParams(f models.Filter) sqlc.ListActivityLogsParams {
	cursorTime := f.CursorTime
	cursorID := f.CursorID
	if cursorTime == nil || cursorID == nil {
		cursorTime = nil
		cursorID = nil
	}

	return sqlc.ListActivityLogsParams{
		OrgID:         validNullInt64(f.OrganizationID),
		Categories:    nilIfEmpty(f.EventCategories),
		EventTypes:    nilIfEmpty(f.EventTypes),
		UserIds:       nilIfEmpty(f.UserIDs),
		ScopeTypes:    nilIfEmpty(f.ScopeTypes),
		SearchPattern: nullStringFromSearch(f.SearchText),
		StartTime:     nullTimeFromPtr(f.StartTime),
		EndTime:       nullTimeFromPtr(f.EndTime),
		CursorTime:    nullTimeFromPtr(cursorTime),
		CursorID:      nullInt64FromPtr(cursorID),
		PageSize:      clampPageSize(f.PageSize),
	}
}

func toCountParams(f models.Filter) sqlc.CountActivityLogsParams {
	return sqlc.CountActivityLogsParams{
		OrgID:         validNullInt64(f.OrganizationID),
		Categories:    nilIfEmpty(f.EventCategories),
		EventTypes:    nilIfEmpty(f.EventTypes),
		UserIds:       nilIfEmpty(f.UserIDs),
		ScopeTypes:    nilIfEmpty(f.ScopeTypes),
		SearchPattern: nullStringFromSearch(f.SearchText),
		StartTime:     nullTimeFromPtr(f.StartTime),
		EndTime:       nullTimeFromPtr(f.EndTime),
	}
}

// --- row conversion ---

func rowToEntry(row sqlc.ListActivityLogsRow) models.Entry {
	var metadata json.RawMessage
	if row.Metadata.Valid {
		metadata = row.Metadata.RawMessage
	}

	return models.Entry{
		ID:           row.ID,
		EventID:      row.EventID.String(),
		Category:     row.EventCategory,
		Type:         row.EventType,
		Description:  row.Description,
		Result:       row.Result,
		ErrorMessage: ptrFromNullString(row.ErrorMessage),
		ScopeType:    ptrFromNullString(row.ScopeType),
		ScopeLabel:   ptrFromNullString(row.ScopeLabel),
		ScopeCount:   intPtrFromNullInt32(row.ScopeCount),
		ActorType:    row.ActorType,
		UserID:       ptrFromNullString(row.UserID),
		Username:     ptrFromNullString(row.Username),
		CreatedAt:    row.CreatedAt,
		Metadata:     metadata,
		BatchID:      ptrFromNullString(row.BatchID),
	}
}

// --- nullable helpers ---

func nullStringFromPtr(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt64FromPtr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullInt32FromIntPtr(v *int) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	n := *v
	if n > math.MaxInt32 {
		n = math.MaxInt32
	}
	if n < math.MinInt32 {
		n = math.MinInt32
	}
	return sql.NullInt32{Int32: int32(n), Valid: true} // #nosec G115 -- bounds checked above
}

func validNullInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullTimeFromPtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func nullStringFromSearch(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: "%" + likeEscaper.Replace(s) + "%", Valid: true}
}

func ptrFromNullString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func intPtrFromNullInt32(n sql.NullInt32) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int32)
	return &v
}

// nilIfEmpty returns nil for empty or nil slices so that pq.Array
// produces a SQL NULL (matches everything) rather than '{}' (matches nothing).
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func clampPageSize(size int) int32 {
	if size < models.MinPageSize {
		return int32(models.DefaultPageSize)
	}
	if size > models.MaxPageSize {
		return int32(models.MaxPageSize)
	}
	return int32(size)
}

func marshalMetadata(m map[string]any) (pqtype.NullRawMessage, error) {
	if len(m) == 0 {
		return pqtype.NullRawMessage{}, nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return pqtype.NullRawMessage{}, fmt.Errorf("marshaling activity metadata: %w", err)
	}
	return pqtype.NullRawMessage{RawMessage: data, Valid: true}, nil
}
