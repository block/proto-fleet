package models

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Pagination constants for error queries.
const (
	DefaultPageSize = 50
	MaxPageSize     = 1000
	MinPageSize     = 1
)

// cursorData holds the serializable cursor state.
// Field names are short to minimize token size.
type cursorData struct {
	Severity   int       `json:"s"`
	LastSeenAt time.Time `json:"t"`
	ErrorID    string    `json:"e"`
}

// DecodeCursor parses a base64 page token into cursor components.
// Returns nil if token is empty. Returns error for invalid tokens.
func DecodeCursor(token string) (*PageCursor, error) {
	if token == "" {
		return nil, nil
	}

	jsonBytes, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	var data cursorData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, fmt.Errorf("invalid cursor format: %w", err)
	}

	return &PageCursor{
		Severity:   Severity(data.Severity), // #nosec G115 -- Severity comes from our own encoded cursor
		LastSeenAt: data.LastSeenAt,
		ErrorID:    data.ErrorID,
	}, nil
}

// NormalizePageSize ensures page size is within valid bounds.
func NormalizePageSize(pageSize int) int {
	if pageSize < MinPageSize {
		return DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return MaxPageSize
	}
	return pageSize
}

// deviceCursorData holds the serializable cursor state for device pagination.
type deviceCursorData struct {
	Severity int   `json:"s"`
	DeviceID int64 `json:"d"`
}

// DecodeDeviceCursor parses a base64 page token into severity and device ID.
// Returns (0, 0, nil) if token is empty. Returns error for invalid tokens.
func DecodeDeviceCursor(token string) (Severity, int64, error) {
	if token == "" {
		return 0, 0, nil
	}

	jsonBytes, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid device cursor encoding: %w", err)
	}

	var data deviceCursorData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return 0, 0, fmt.Errorf("invalid device cursor format: %w", err)
	}

	return Severity(data.Severity), data.DeviceID, nil // #nosec G115 -- Severity comes from our own encoded cursor
}

// componentCursorData holds the serializable cursor state for component pagination.
type componentCursorData struct {
	Severity      int           `json:"s"`
	DeviceID      int64         `json:"d"`
	ComponentType ComponentType `json:"t"`
	ComponentID   *string       `json:"c,omitempty"`
}

// DecodeComponentCursor parses a base64 page token into severity, device ID, component type and component ID.
// Returns (0, 0, 0, nil, nil) if token is empty. Returns error for invalid tokens.
func DecodeComponentCursor(token string) (Severity, int64, ComponentType, *string, error) {
	if token == "" {
		return 0, 0, 0, nil, nil
	}

	jsonBytes, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("invalid component cursor encoding: %w", err)
	}

	var data componentCursorData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("invalid component cursor format: %w", err)
	}

	return Severity(data.Severity), data.DeviceID, data.ComponentType, data.ComponentID, nil // #nosec G115 -- Severity and ComponentType come from our own encoded cursor
}
