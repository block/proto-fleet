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

// Actor marks sessions synthesized by internal orchestrators (e.g. the
// schedule processor) so the command service can attribute activity rows
// correctly. Empty for user/API-key traffic.
type Actor string

const (
	// ActorScheduler marks sessions synthesized by the schedule processor.
	ActorScheduler Actor = "scheduler"
)

// Source carries optional caller policy context used by command preflight
// filters to make priority/conflict decisions. All fields are zero by default.
// Filters that don't require a particular field should ignore it.
type Source struct {
	// ScheduleID + SchedulePriority are populated by the schedule processor
	// for scheduler-origin commands. Manual callers (and any caller without
	// schedule context) leave them zero. ScheduleID == 0 is the convention
	// for "no source schedule" — the schedule_conflict filter uses it to
	// decide whether to apply scheduler priority semantics or treat every
	// running power-target schedule as a blocker.
	ScheduleID       int64
	SchedulePriority int32
}

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

	// Actor is set by internal orchestrators that synthesize a session.Info
	// (e.g. scheduler). Empty for user/API-key traffic.
	Actor Actor

	// Source is populated by orchestrators that have policy context filters
	// can act on (priority, schedule ID, etc.). Zero-valued for user/API-key
	// traffic.
	Source Source
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
