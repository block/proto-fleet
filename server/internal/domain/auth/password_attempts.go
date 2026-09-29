package auth

import (
	"crypto/sha256"
	"errors"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
)

const passwordAttemptWindow = time.Minute

type passwordAttemptWindowState struct {
	started time.Time
	count   int
}

// passwordAttempts is shared by every password-verification operation in a Service.
// Fixed windows count successes too, reset on process restart, and never evict live entries.
type passwordAttempts struct {
	mu         sync.Mutex
	entries    map[[sha256.Size]byte]passwordAttemptWindowState
	nextExpiry time.Time
}

func (l *passwordAttempts) allow(username string, now time.Time) bool {
	// Fixed-size keys bound memory even for oversized names from internal callers.
	key := sha256.Sum256([]byte(username))
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[[sha256.Size]byte]passwordAttemptWindowState)
	}
	entry, exists := l.entries[key]
	if exists && now.Before(entry.started.Add(passwordAttemptWindow)) {
		if entry.count >= 10 {
			return false
		}
		entry.count++
		l.entries[key] = entry
		return true
	}
	if !exists && len(l.entries) >= 10000 {
		if now.Before(l.nextExpiry) {
			return false
		}
		l.nextExpiry = time.Time{}
		for key, entry := range l.entries {
			expires := entry.started.Add(passwordAttemptWindow)
			if !now.Before(expires) {
				delete(l.entries, key)
			} else if l.nextExpiry.IsZero() || expires.Before(l.nextExpiry) {
				l.nextExpiry = expires
			}
		}
		if len(l.entries) >= 10000 {
			return false
		}
	}
	l.entries[key] = passwordAttemptWindowState{started: now, count: 1}
	if expires := now.Add(passwordAttemptWindow); l.nextExpiry.IsZero() || expires.Before(l.nextExpiry) {
		l.nextExpiry = expires
	}
	return true
}

func (s *Service) checkPasswordAttempt(username string) error {
	if s.passwordAttempts.allow(username, time.Now()) {
		return nil
	}
	slog.Warn("password verification throttled", "event", "auth_password_throttled")
	return connect.NewError(connect.CodeResourceExhausted, errors.New("Too many password attempts. Try again in one minute."))
}
