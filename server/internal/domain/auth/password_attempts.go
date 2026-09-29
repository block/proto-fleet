package auth

import (
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

// passwordAttempts bounds checks per immutable account ID. Public login and
// session-bound verification use separate budgets so login abuse cannot block step-up.
// Fixed windows count successes too, reset on process restart, and never evict live entries.
type passwordAttempts struct {
	mu         sync.Mutex
	entries    map[int64]passwordAttemptWindowState
	nextExpiry time.Time
}

func (l *passwordAttempts) allow(userID int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[int64]passwordAttemptWindowState)
	}
	entry, exists := l.entries[userID]
	if exists && now.Before(entry.started.Add(passwordAttemptWindow)) {
		if entry.count >= 10 {
			return false
		}
		entry.count++
		l.entries[userID] = entry
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
	l.entries[userID] = passwordAttemptWindowState{started: now, count: 1}
	if expires := now.Add(passwordAttemptWindow); l.nextExpiry.IsZero() || expires.Before(l.nextExpiry) {
		l.nextExpiry = expires
	}
	return true
}

func (l *passwordAttempts) check(userID int64) error {
	if l.allow(userID, time.Now()) {
		return nil
	}
	slog.Warn("password verification throttled", "event", "auth_password_throttled")
	return connect.NewError(connect.CodeResourceExhausted, errors.New("Too many password attempts. Try again in one minute."))
}
