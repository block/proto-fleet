package models

import (
	"time"
)

// ============================================================================
// Filter Types
// ============================================================================

// FilterLogic determines how filter criteria are combined.
// TODO: Re-enable OR logic support in SQL queries and service layer.
type FilterLogic uint

const (
	FilterLogicUnspecified FilterLogic = 0
	FilterLogicAND         FilterLogic = 1 // All criteria must match (currently the only supported mode)
	FilterLogicOR          FilterLogic = 2 // Any criterion can match (not yet implemented)
)

// QueryFilter represents filter criteria for error queries.
type QueryFilter struct {
	DeviceIdentifiers []string        // Filter by device identifiers (e.g., "proto-12345")
	DeviceTypes       []string        // Filter by model names (e.g., "R2", "S19")
	ComponentIDs      []string        // Filter by component identifiers
	ComponentTypes    []ComponentType // Filter by component type
	MinerErrors       []MinerError    // Filter by canonical error codes
	Severities        []Severity      // Filter by severity levels
	TimeFrom          *time.Time      // Filter errors with last_seen_at >= TimeFrom
	TimeTo            *time.Time      // Filter errors with last_seen_at <= TimeTo
	IncludeClosed     bool            // If true, include closed/expired errors
	Logic             FilterLogic     // How to combine filter criteria (AND/OR)
}

// ============================================================================
// Query Request Types
// ============================================================================

// ResultView determines how errors are aggregated in the response.
type ResultView uint

const (
	ResultViewUnspecified ResultView = 0
	ResultViewError       ResultView = 1 // Flat list of individual errors
	ResultViewComponent   ResultView = 2 // Errors grouped by component
	ResultViewDevice      ResultView = 3 // Errors grouped by device
)

// QueryOptions contains all parameters for an error query.
type QueryOptions struct {
	OrgID      int64        // Required: organization scope
	Filter     *QueryFilter // Optional: filter criteria
	ResultView ResultView   // How to aggregate results
	PageSize   int          // Number of items per page (1-1000)
	PageToken  string       // Cursor for pagination
	OrderBy    string       // Sort order (default: "severity DESC, last_seen_at DESC, error_id DESC")
}

// ============================================================================
// Query Response Types
// ============================================================================

// QueryResult represents the result of an error query.
type QueryResult struct {
	Errors        []ErrorMessage     // Populated for ResultViewError
	ComponentErrs []ComponentErrors  // Populated for ResultViewComponent
	DeviceErrs    []DeviceErrorGroup // Populated for ResultViewDevice
	NextPageToken string             // Cursor for fetching the next page
	TotalCount    int64              // Total number of matching items across all pages
}

// Status represents aggregated health status based on error severity.
// Calculated using waterfall logic: Critical→Error, Major/Minor/Info→Warning, else OK.
type Status uint

const (
	StatusUnspecified Status = 0
	StatusOK          Status = 1 // No open errors
	StatusWarning     Status = 2 // Major, minor, or informational errors present
	StatusError       Status = 3 // Critical errors present
)

// Summary provides human-readable error summaries for grouped views.
type Summary struct {
	Title     string // One-liner summary (e.g., "3 critical PSU errors")
	Details   string // Full multi-line description
	Condensed string // Ultra-short for UI badges (e.g., "3 PSU")
}

// ComponentErrors groups errors by component for ResultViewComponent.
type ComponentErrors struct {
	ComponentID      string
	ComponentType    ComponentType
	DeviceID         int64
	DeviceType       string // Model name (e.g., "S19", "R2")
	Status           Status
	Summary          Summary
	Errors           []ErrorMessage
	CountsBySeverity map[Severity]int32
}

// DeviceErrorGroup groups errors by device for ResultViewDevice.
// Named differently from DeviceErrors in miner_errors.go (used for plugin returns).
type DeviceErrorGroup struct {
	DeviceID         int64
	DeviceType       string // Model name (e.g., "S19", "R2")
	Status           Status
	Summary          Summary
	Errors           []ErrorMessage
	CountsBySeverity map[Severity]int32
}

// ============================================================================
// Pagination Types
// ============================================================================

// PageCursor holds pagination state for cursor-based pagination.
// Cursor is based on the sort order: (severity, last_seen_at, error_id).
type PageCursor struct {
	Severity   Severity
	LastSeenAt time.Time
	ErrorID    string
}

// DeviceKey identifies a unique device with its worst severity for cursor-based pagination.
// Used by ResultViewDevice to paginate by device rather than by error.
type DeviceKey struct {
	DeviceID         int64    // Internal database ID (for keyset pagination)
	DeviceIdentifier string   // External identifier (for filtering)
	WorstSeverity    Severity // For keyset pagination ordering
}

// ComponentKey identifies a unique component with its worst severity for cursor-based pagination.
// Used by ResultViewComponent to paginate by component rather than by error.
type ComponentKey struct {
	DeviceID         int64         // Internal database ID (for keyset pagination)
	DeviceIdentifier string        // External identifier (for filtering)
	ComponentType    ComponentType // Component type (PSU, hashboard, fan, control board, etc.)
	ComponentID      *string       // nil = NULL/device-level errors, non-nil = component-specific
	WorstSeverity    Severity      // For keyset pagination ordering
}
