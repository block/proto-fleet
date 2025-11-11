package errors

import (
	"encoding/json"
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
	ID         string                 `json:"id"`
	ErrorCode  string                 `json:"error_code"`
	ErrorLevel string                 `json:"error_level"` // "Error" | "Warning"
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details"`
	// Component indices for error location
	ComponentIndex *int   `json:"component_index,omitempty"`
	HashboardIndex *int   `json:"hashboard_index,omitempty"`
	AsicIndex      *int   `json:"asic_index,omitempty"`
	InsertedAt     int64  `json:"inserted_at"` // Unix timestamp
	ExpiredAt      *int64 `json:"expired_at,omitempty"`
	TTLSeconds     *int   `json:"ttl_seconds,omitempty"`
}

// ToAPIFormat converts the error to the miner API format
// Following the NotificationError type from MDK_API.json
func (e *InjectedError) ToAPIFormat() map[string]interface{} {
	apiError := map[string]interface{}{
		"error_code":  e.ErrorCode,
		"error_level": e.ErrorLevel,
		"message":     e.Message,
		"inserted_at": e.InsertedAt,
	}

	// Determine source based on which indices are present
	source := "Miner" // Default source
	if e.AsicIndex != nil {
		source = "ASIC"
		apiError["asic_index"] = *e.AsicIndex
	}
	if e.HashboardIndex != nil {
		if source != "ASIC" {
			source = "Hashboard"
		}
		apiError["hashboard_index"] = *e.HashboardIndex
	}
	if e.ComponentIndex != nil {
		apiError["component_index"] = *e.ComponentIndex
	}
	apiError["source"] = source

	// Add expired_at if set
	if e.ExpiredAt != nil {
		apiError["expired_at"] = *e.ExpiredAt
	} else {
		apiError["expired_at"] = 0 // Active errors have expired_at = 0
	}

	// Format details as a JSON string matching the Rust ErrorDetails enum
	// The details field is a JSON string with the error variant as the key
	if e.Details != nil && len(e.Details) > 0 {
		// Build the details object based on the error code
		var detailsObj interface{}

		switch e.ErrorCode {
		case "FanSlow", "FanNotSpinning":
			// Match Rust struct: {fan_bay_index, fan_id, fan_pwm_target_pct, fan_rpm_tach}
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{
					"fan_bay_index":      e.Details["fan_bay_index"],
					"fan_id":            e.Details["fan_id"],
					"fan_pwm_target_pct": e.Details["fan_pwm_target_pct"],
					"fan_rpm_tach":      e.Details["fan_rpm_tach"],
				},
			}
		case "HashboardOverheat":
			// Match Rust struct: {hb_slot, hb_sn, temperature}
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{
					"hb_slot":     e.Details["hb_slot"],
					"hb_sn":       e.Details["hb_sn"],
					"temperature": e.Details["temperature"],
				},
			}
		case "AsicOverTemp":
			// Match Rust struct: {hb_slot, hb_sn, asic_index, temperature}
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{
					"hb_slot":     e.Details["hb_slot"],
					"hb_sn":       e.Details["hb_sn"],
					"asic_index":  e.Details["asic_index"],
					"temperature": e.Details["temperature"],
				},
			}
		case "PoolConnectionLost":
			// Match Rust struct: {pool_id, pool_url}
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{
					"pool_id":  e.Details["pool_id"],
					"pool_url": e.Details["pool_url"],
				},
			}
		case "NoPoolConfigured":
			// Empty struct
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{},
			}
		case "HashboardPowerLost", "HashboardUsbConnectionLost":
			// Match Rust struct: {hb_slot, hb_sn}
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{
					"hb_slot": e.Details["hb_slot"],
					"hb_sn":   e.Details["hb_sn"],
				},
			}
		case "InsufficientCooling":
			// Match Rust struct: {bay_index, num_operational_fans, num_expected_fans, failed_fans, required_fans}
			detailsObj = map[string]interface{}{
				e.ErrorCode: map[string]interface{}{
					"bay_index":            e.Details["bay_index"],
					"num_operational_fans": e.Details["num_operational_fans"],
					"num_expected_fans":    e.Details["num_expected_fans"],
					"failed_fans":         e.Details["failed_fans"],
					"required_fans":       e.Details["required_fans"],
				},
			}
		default:
			// For any other error types, just wrap all details
			detailsObj = map[string]interface{}{
				e.ErrorCode: e.Details,
			}
		}

		if detailsJSON, err := json.Marshal(detailsObj); err == nil {
			apiError["details"] = string(detailsJSON)
		}
	}

	return apiError
}

// TriggerError adds a new error to the store
func (s *Store) TriggerError(errorCode, errorLevel, message string, details map[string]interface{}, ttlSeconds *int) *InjectedError {
	s.mu.Lock()
	defer s.mu.Unlock()

	error := &InjectedError{
		ID:         uuid.New().String(),
		ErrorCode:  errorCode,
		ErrorLevel: errorLevel,
		Message:    message,
		Details:    details,
		InsertedAt: time.Now().Unix(),
		TTLSeconds: ttlSeconds,
	}

	// Extract component indices from details if present
	if compIdx, ok := details["component_index"].(float64); ok {
		idx := int(compIdx)
		error.ComponentIndex = &idx
	}
	if hbIdx, ok := details["hashboard_index"].(float64); ok {
		idx := int(hbIdx)
		error.HashboardIndex = &idx
	}
	if asicIdx, ok := details["asic_index"].(float64); ok {
		idx := int(asicIdx)
		error.AsicIndex = &idx
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