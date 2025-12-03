package session

import "time"

// Config holds session-related configuration.
// Note: Environment variables are prefixed with SESSION_ by the parent config (envprefix:"SESSION_")
type Config struct {
	// Duration is the session lifetime with sliding window (extends on each request)
	Duration time.Duration `help:"Session lifetime duration" env:"DURATION" default:"8h"`
	// IDBytes is the number of random bytes for session ID (32 = 256 bits)
	IDBytes int `help:"Session ID entropy bytes" env:"ID_BYTES" default:"32"`
	// CookieName is the name of the session cookie
	CookieName string `help:"Session cookie name" env:"COOKIE_NAME" default:"fleet_session"`
	// CookieSecure enables the Secure flag (should be true in production)
	CookieSecure bool `help:"Require HTTPS for cookies" env:"COOKIE_SECURE" default:"true"`
	// CookieSameSite sets the SameSite attribute (Strict, Lax, or None)
	CookieSameSite string `help:"Cookie SameSite policy" env:"COOKIE_SAMESITE" default:"Strict"`
	// CleanupInterval is how often to run expired session cleanup
	CleanupInterval time.Duration `help:"Expired session cleanup interval" env:"CLEANUP_INTERVAL" default:"1h"`
}
