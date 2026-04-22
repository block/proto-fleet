package session

import "time"

// AuthMethod identifies how a request was authenticated.
type AuthMethod string

const (
	// AuthMethodSession indicates the request was authenticated via a session cookie.
	AuthMethodSession AuthMethod = "session"
	// AuthMethodAPIKey indicates the request was authenticated via an API key.
	AuthMethodAPIKey AuthMethod = "api_key"
)

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

// Actor identifies the kind of principal acting on behalf of a request.
// The typed alias + exported constants prevent magic-string drift between
// the internal orchestrators that set it (e.g. the schedule processor) and
// the consumers that switch on it (e.g. the command service's activity
// logger). Keeping it a bare string alias avoids pulling domain types into
// the session package.
type Actor string

const (
	// ActorScheduler marks sessions synthesized by the schedule processor.
	ActorScheduler Actor = "scheduler"
	// ActorSystem marks sessions synthesized by internal maintenance or
	// reconciler code paths that have no human principal.
	ActorSystem Actor = "system"
)

// Info contains authenticated request context passed to handlers.
// Populated by the auth interceptor for both session and API key authentication.
type Info struct {
	// AuthMethod identifies how this request was authenticated.
	AuthMethod AuthMethod

	// SessionID is only populated when AuthMethod == AuthMethodSession.
	SessionID string

	// APIKeyID is only populated when AuthMethod == AuthMethodAPIKey.
	APIKeyID string

	// Common fields, always populated regardless of auth method.
	UserID         int64
	OrganizationID int64
	ExternalUserID string
	Username       string
	Role           string

	// Actor identifies the kind of principal acting on behalf of the request.
	// Empty for user / API-key traffic (callers default to the user actor
	// type). Set by internal orchestrators that synthesize a session.Info;
	// downstream code translates it into the correct activity actor type.
	Actor Actor
}

// CredentialID returns a stable identifier for the authenticated credential.
// For sessions this is the session ID; for API keys this is "apikey:<key_id>".
// Use this for deduplication, audit trails, and logging instead of raw SessionID.
func (i *Info) CredentialID() string {
	if i.AuthMethod == AuthMethodAPIKey {
		return "apikey:" + i.APIKeyID
	}
	return i.SessionID
}
