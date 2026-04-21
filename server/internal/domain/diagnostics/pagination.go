package diagnostics

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
)

// Re-export pagination constants from models for backward compatibility.
const (
	DefaultPageSize = models.DefaultPageSize
	MaxPageSize     = models.MaxPageSize
	MinPageSize     = models.MinPageSize
)

// NormalizePageSize re-exports from models for backward compatibility.
var NormalizePageSize = models.NormalizePageSize

// DecodeCursor re-exports from models for backward compatibility.
var DecodeCursor = models.DecodeCursor

// DecodeDeviceCursor re-exports from models for backward compatibility.
var DecodeDeviceCursor = models.DecodeDeviceCursor

// DecodeComponentCursor re-exports from models for backward compatibility.
var DecodeComponentCursor = models.DecodeComponentCursor

// ============================================================================
// Encode Functions (not in models - only needed by service layer)
// ============================================================================

// cursorData holds the serializable cursor state for encoding.
// Field names are short to minimize token size.
// Note: This type is intentionally duplicated from models/pagination.go to maintain
// layer separation - models package handles decoding (used by sqlstores), while
// this package handles encoding (used by service). Both must remain in sync.
type cursorData struct {
	Severity   int       `json:"s"`
	LastSeenAt time.Time `json:"t"`
	ErrorID    string    `json:"e"`
}

// EncodeCursor creates a base64 page token from cursor data.
// Returns empty string if encoding fails (logged at error level since this indicates
// the response will appear to have no more pages, potentially causing data loss for clients).
func EncodeCursor(severity models.Severity, lastSeenAt time.Time, errorID string) string {
	data := cursorData{
		Severity:   int(severity), // #nosec G115 -- Severity enum bounded (max 4), safe for int
		LastSeenAt: lastSeenAt,
		ErrorID:    errorID,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to encode cursor", "error", err, "errorID", errorID)
		return ""
	}

	return base64.URLEncoding.EncodeToString(jsonBytes)
}

// BuildNextPageToken creates a page token from the last error in the result set.
// Returns empty string if errors slice is empty or if the page is not full.
func BuildNextPageToken(errors []models.ErrorMessage, pageSize int) string {
	if len(errors) == 0 || len(errors) < pageSize {
		return ""
	}

	lastError := errors[len(errors)-1]
	return EncodeCursor(lastError.Severity, lastError.LastSeenAt, lastError.ErrorID)
}

// ============================================================================
// Device Cursor Functions
// ============================================================================

// deviceCursorData holds the serializable cursor state for device pagination.
// Includes severity to preserve sort order (worst_severity ASC, device_id ASC).
type deviceCursorData struct {
	Severity int   `json:"s"` // worst_severity for keyset pagination
	DeviceID int64 `json:"d"`
}

// EncodeDeviceCursor creates a base64 page token from severity and device ID.
// Both fields are required for correct keyset pagination with ORDER BY worst_severity ASC, device_id ASC.
func EncodeDeviceCursor(severity models.Severity, deviceID int64) string {
	data := deviceCursorData{
		Severity: int(severity), // #nosec G115 -- Severity enum bounded (max 4), safe for int
		DeviceID: deviceID,
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to encode device cursor", "error", err, "deviceID", deviceID)
		return ""
	}
	return base64.URLEncoding.EncodeToString(jsonBytes)
}

// BuildNextDevicePageToken creates a cursor from SQL-ordered device keys.
// This preserves the SQL sort order (worst_severity ASC, device_id ASC) for pagination.
// Returns empty string if devices slice is empty or if the page is not full.
func BuildNextDevicePageToken(devices []models.DeviceKey, pageSize int) string {
	if len(devices) == 0 || len(devices) < pageSize {
		return ""
	}
	lastDevice := devices[len(devices)-1]
	return EncodeDeviceCursor(lastDevice.WorstSeverity, lastDevice.DeviceID)
}

// ============================================================================
// Component Cursor Functions
// ============================================================================

// componentCursorData holds the serializable cursor state for component pagination.
// Includes severity to preserve sort order (worst_severity ASC, device_id ASC, component_type ASC, component_id ASC).
type componentCursorData struct {
	Severity      int                  `json:"s"` // worst_severity for keyset pagination
	DeviceID      int64                `json:"d"`
	ComponentType models.ComponentType `json:"t"`
	ComponentID   *string              `json:"c,omitempty"` // nil = NULL/device-level, non-nil = component-specific
}

// EncodeComponentCursor creates a base64 page token from severity, device ID, component type and component ID.
// Pass nil for componentID when the component is NULL (device-level errors).
// All fields are required for correct keyset pagination.
func EncodeComponentCursor(severity models.Severity, deviceID int64, componentType models.ComponentType, componentID *string) string {
	data := componentCursorData{
		Severity:      int(severity), // #nosec G115 -- Severity enum bounded (max 4), safe for int
		DeviceID:      deviceID,
		ComponentType: componentType,
		ComponentID:   componentID,
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to encode component cursor", "error", err, "deviceID", deviceID, "componentType", componentType, "componentID", componentID)
		return ""
	}
	return base64.URLEncoding.EncodeToString(jsonBytes)
}

// BuildNextComponentPageToken creates a cursor from SQL-ordered component keys.
// This preserves the SQL sort order (worst_severity ASC, device_id ASC, component_type ASC, component_id ASC).
// Returns empty string if components slice is empty or if the page is not full.
func BuildNextComponentPageToken(components []models.ComponentKey, pageSize int) string {
	if len(components) == 0 || len(components) < pageSize {
		return ""
	}
	lastComponent := components[len(components)-1]
	return EncodeComponentCursor(lastComponent.WorstSeverity, lastComponent.DeviceID, lastComponent.ComponentType, lastComponent.ComponentID)
}
