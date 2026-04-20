package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mocks/mock_session_store.go -package=mocks Store

// Store defines the interface for session persistence operations.
type Store interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByID(ctx context.Context, sessionID string) (*Session, error)
	UpdateSessionActivity(ctx context.Context, sessionID string, lastActivity, expiresAt time.Time) error
	RevokeSession(ctx context.Context, sessionID string, revokedAt time.Time) error
	RevokeAllSessionsByUserID(ctx context.Context, userID int64, revokedAt time.Time) error
	DeleteExpiredSessions(ctx context.Context, cutoff time.Time) (int64, error)
}

// Service provides session management operations.
type Service struct {
	cfg   Config
	store Store
}

// NewService creates a new session service.
func NewService(cfg Config, store Store) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
	}
}

// Create generates a new session for the authenticated user.
func (s *Service) Create(ctx context.Context, userID, orgID int64, userAgent, ipAddress string) (*Session, error) {
	sessionID, err := generateSessionID(s.cfg.IDBytes)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to generate session ID: %v", err)
	}

	now := time.Now()
	session := &Session{
		SessionID:      sessionID,
		UserID:         userID,
		OrganizationID: orgID,
		UserAgent:      userAgent,
		IPAddress:      ipAddress,
		CreatedAt:      now,
		LastActivity:   now,
		ExpiresAt:      now.Add(s.cfg.Duration),
	}

	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create session: %v", err)
	}

	return session, nil
}

// Validate checks if a session is valid and updates last activity (sliding window).
func (s *Service) Validate(ctx context.Context, sessionID string) (*Session, error) {
	session, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("invalid session")
	}

	now := time.Now()

	if now.After(session.ExpiresAt) {
		return nil, fleeterror.NewUnauthenticatedError("session expired")
	}

	if session.RevokedAt != nil {
		return nil, fleeterror.NewUnauthenticatedError("session revoked")
	}

	// Sliding window: extend expiry on activity
	newExpiry := now.Add(s.cfg.Duration)
	if err := s.store.UpdateSessionActivity(ctx, sessionID, now, newExpiry); err != nil {
		// Log but don't fail - session is still valid, it will just expire at the original time
		// Truncate session ID in logs to avoid leaking full identifier if logs are compromised
		truncatedID := sessionID
		if len(sessionID) > 8 {
			truncatedID = sessionID[:8] + "..."
		}
		slog.ErrorContext(ctx, "failed to update session activity", "sessionID", truncatedID, "error", err)
	}

	session.LastActivity = now
	session.ExpiresAt = newExpiry

	return session, nil
}

// Revoke invalidates a specific session (logout).
func (s *Service) Revoke(ctx context.Context, sessionID string) error {
	return s.store.RevokeSession(ctx, sessionID, time.Now())
}

// RevokeAllSessions revokes all active sessions for a user.
func (s *Service) RevokeAllSessions(ctx context.Context, userID int64) error {
	return s.store.RevokeAllSessionsByUserID(ctx, userID, time.Now())
}

// CleanupExpired removes expired and revoked sessions from the database.
func (s *Service) CleanupExpired(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx, time.Now())
}

// CreateCookie creates an HTTP cookie with proper security settings for the session.
func (s *Service) CreateCookie(sessionID string) *http.Cookie {
	return &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: parseSameSite(s.cfg.CookieSameSite),
		MaxAge:   int(s.cfg.Duration.Seconds()),
	}
}

// CreateLogoutCookie creates an expired cookie to clear the session.
func (s *Service) CreateLogoutCookie() *http.Cookie {
	return &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: parseSameSite(s.cfg.CookieSameSite),
		MaxAge:   -1, // Delete immediately
	}
}

// CookieName returns the configured cookie name.
func (s *Service) CookieName() string {
	return s.cfg.CookieName
}

// CleanupInterval returns the configured cleanup interval.
func (s *Service) CleanupInterval() time.Duration {
	return s.cfg.CleanupInterval
}

func generateSessionID(numBytes int) (string, error) {
	bytes := make([]byte, numBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func parseSameSite(value string) http.SameSite {
	switch value {
	case "Lax":
		return http.SameSiteLaxMode
	case "None":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteStrictMode
	}
}
