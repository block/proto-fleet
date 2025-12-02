package session

import "time"

// Session represents a user session stored in the database.
type Session struct {
	ID             int64
	SessionID      string
	UserID         int64
	OrganizationID int64
	UserAgent      string
	IPAddress      string
	CreatedAt      time.Time
	LastActivity   time.Time
	ExpiresAt      time.Time
	RevokedAt      *time.Time
}

// Info contains session information passed to handlers via context.
// This replaces the JWT-based ClientAuthClaims for user authentication.
type Info struct {
	SessionID      string
	UserID         int64
	OrganizationID int64
}
