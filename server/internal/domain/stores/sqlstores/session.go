package sqlstores

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

var _ session.Store = &SQLSessionStore{}

// SQLSessionStore implements session.Store using SQL database.
type SQLSessionStore struct {
	SQLConnectionManager
}

// NewSQLSessionStore creates a new SQL-backed session store.
func NewSQLSessionStore(conn *sql.DB) *SQLSessionStore {
	return &SQLSessionStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLSessionStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

// CreateSession creates a new session record in the database.
func (s *SQLSessionStore) CreateSession(ctx context.Context, sess *session.Session) error {
	return s.getQueries(ctx).CreateSession(ctx, sqlc.CreateSessionParams{
		SessionID:      sess.SessionID,
		UserID:         sess.UserID,
		OrganizationID: sess.OrganizationID,
		UserAgent:      toNullString(sess.UserAgent),
		IpAddress:      toNullString(sess.IPAddress),
		CreatedAt:      sess.CreatedAt,
		LastActivity:   sess.LastActivity,
		ExpiresAt:      sess.ExpiresAt,
	})
}

// GetSessionByID retrieves a session by its ID.
func (s *SQLSessionStore) GetSessionByID(ctx context.Context, sessionID string) (*session.Session, error) {
	row, err := s.getQueries(ctx).GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return &session.Session{
		ID:             row.ID,
		SessionID:      row.SessionID,
		UserID:         row.UserID,
		OrganizationID: row.OrganizationID,
		UserAgent:      row.UserAgent.String,
		IPAddress:      row.IpAddress.String,
		CreatedAt:      row.CreatedAt,
		LastActivity:   row.LastActivity,
		ExpiresAt:      row.ExpiresAt,
		RevokedAt:      nullTimeToPtr(row.RevokedAt),
	}, nil
}

// UpdateSessionActivity updates the last activity and expiry time for a session.
func (s *SQLSessionStore) UpdateSessionActivity(ctx context.Context, sessionID string, lastActivity, expiresAt time.Time) error {
	return s.getQueries(ctx).UpdateSessionActivity(ctx, sqlc.UpdateSessionActivityParams{
		SessionID:    sessionID,
		LastActivity: lastActivity,
		ExpiresAt:    expiresAt,
	})
}

// RevokeSession marks a session as revoked.
func (s *SQLSessionStore) RevokeSession(ctx context.Context, sessionID string, revokedAt time.Time) error {
	return s.getQueries(ctx).RevokeSession(ctx, sqlc.RevokeSessionParams{
		SessionID: sessionID,
		RevokedAt: sql.NullTime{Time: revokedAt, Valid: true},
	})
}

// RevokeAllSessionsByUserID revokes all sessions for a user.
func (s *SQLSessionStore) RevokeAllSessionsByUserID(ctx context.Context, userID int64, revokedAt time.Time) error {
	return s.getQueries(ctx).RevokeAllSessionsByUserID(ctx, sqlc.RevokeAllSessionsByUserIDParams{
		RevokedAt: sql.NullTime{Time: revokedAt, Valid: true},
		UserID:    userID,
	})
}

// DeleteExpiredSessions removes expired and revoked sessions from the database.
func (s *SQLSessionStore) DeleteExpiredSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.getQueries(ctx).DeleteExpiredSessions(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rowsAffected, nil
}

func toNullString(s string) sql.NullString {
	return sql.NullString{
		String: s,
		Valid:  s != "",
	}
}

func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}
