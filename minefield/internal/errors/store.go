package errors

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store manages injected errors in memory
type Store struct {
	errors map[string]*InjectedError
	mu     sync.RWMutex
}

// NewStore creates a new error store
func NewStore() *Store {
	return &Store{
		errors: make(map[string]*InjectedError),
	}
}

// InjectedError represents an error injected into the system
type InjectedError struct {
	ID             string `json:"id"`
	ErrorCode      string `json:"error_code"`
	Source         string `json:"source"` // "rig" | "fan" | "psu" | "hashboard"
	ComponentIndex *int   `json:"component_index,omitempty"`
	Message        string `json:"message"`
	Timestamp      int64  `json:"timestamp"` // Unix timestamp
	ExpiredAt      *int64 `json:"expired_at,omitempty"` // Internal tracking only
	TTLSeconds     *int   `json:"ttl_seconds,omitempty"` // Internal tracking only
}

// ToAPIFormat converts the error to the miner API format
// Following the simplified NotificationError type
func (e *InjectedError) ToAPIFormat() map[string]interface{} {
	apiError := map[string]interface{}{
		"error_code": e.ErrorCode,
		"source":     e.Source,
		"message":    e.Message,
		"timestamp":  e.Timestamp,
	}

	// Add component_index if set
	if e.ComponentIndex != nil {
		apiError["component_index"] = *e.ComponentIndex
	}

	return apiError
}

// TriggerError adds a new error to the store
func (s *Store) TriggerError(errorCode, source, message string, componentIndex *int, ttlSeconds *int) *InjectedError {
	s.mu.Lock()
	defer s.mu.Unlock()

	error := &InjectedError{
		ID:             uuid.New().String(),
		ErrorCode:      errorCode,
		Source:         source,
		Message:        message,
		ComponentIndex: componentIndex,
		Timestamp:      time.Now().Unix(),
		TTLSeconds:     ttlSeconds,
	}

	// Set expiration if TTL is specified
	if ttlSeconds != nil && *ttlSeconds > 0 {
		expiresAt := time.Now().Add(time.Duration(*ttlSeconds) * time.Second).Unix()
		error.ExpiredAt = &expiresAt
	}

	s.errors[error.ID] = error
	return error
}

// GetActiveErrors returns all active (non-expired) errors
func (s *Store) GetActiveErrors() []*InjectedError {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().Unix()
	active := make([]*InjectedError, 0)

	for _, err := range s.errors {
		// Skip expired errors
		if err.ExpiredAt != nil && *err.ExpiredAt <= now {
			continue
		}
		active = append(active, err)
	}

	return active
}

// GetAllErrors returns all errors including expired ones
func (s *Store) GetAllErrors() []*InjectedError {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]*InjectedError, 0, len(s.errors))
	for _, err := range s.errors {
		all = append(all, err)
	}
	return all
}

// ClearError marks an error as cleared (expired)
func (s *Store) ClearError(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err, ok := s.errors[id]; ok {
		now := time.Now().Unix()
		err.ExpiredAt = &now
		return nil
	}
	return nil // Silently succeed even if not found
}

// ClearAllErrors clears all active errors
func (s *Store) ClearAllErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	for _, err := range s.errors {
		if err.ExpiredAt == nil || *err.ExpiredAt > now {
			err.ExpiredAt = &now
		}
	}
}

// DeleteError removes an error completely from the store
func (s *Store) DeleteError(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.errors, id)
}

// CleanupExpired removes expired errors older than the given duration
func (s *Store) CleanupExpired(olderThan time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan).Unix()
	removed := 0

	for id, err := range s.errors {
		if err.ExpiredAt != nil && *err.ExpiredAt < cutoff {
			delete(s.errors, id)
			removed++
		}
	}

	return removed
}