package models

import (
	"encoding/json"
	"time"
)

type EventCategory string

const (
	CategoryAuth            EventCategory = "auth"
	CategoryDeviceCommand   EventCategory = "device_command"
	CategoryFleetManagement EventCategory = "fleet_management"
	CategoryCollection      EventCategory = "collection"
	CategoryPool            EventCategory = "pool"
	CategorySchedule        EventCategory = "schedule"
	CategorySystem          EventCategory = "system"
)

type ActorType string

const (
	ActorUser      ActorType = "user"
	ActorSystem    ActorType = "system"
	ActorScheduler ActorType = "scheduler"
)

type ResultType string

const (
	ResultSuccess ResultType = "success"
	ResultFailure ResultType = "failure"
	// ResultUnknown marks an outcome the server can no longer determine. The
	// completion reconciler writes this when a batch reached FINISHED but its
	// per-device rows in command_on_device_log have already been retention-pruned,
	// so we know the batch completed but cannot tell whether any device failed.
	// The activity_log.result column is plain TEXT so no DB schema change is
	// required; frontend should render unknown as a neutral state.
	ResultUnknown ResultType = "unknown"
)

func (c EventCategory) Valid() bool {
	switch c {
	case CategoryAuth, CategoryDeviceCommand, CategoryFleetManagement,
		CategoryCollection, CategoryPool, CategorySchedule, CategorySystem:
		return true
	}
	return false
}

func (a ActorType) Valid() bool {
	switch a {
	case ActorUser, ActorSystem, ActorScheduler:
		return true
	}
	return false
}

func (r ResultType) Valid() bool {
	switch r {
	case ResultSuccess, ResultFailure, ResultUnknown:
		return true
	}
	return false
}

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
	MinPageSize     = 1
)

// CompletedEventSuffix is appended to a command event type to mark the
// terminal row emitted by the batch finalizer. The combined pair
// (batch_id, event_type) is guarded by a partial unique index in the
// activity_log table so the finalizer (and its crash-recovery reconciler)
// can re-run idempotently.
const CompletedEventSuffix = ".completed"

// Event is the write model used by callers of Service.Log().
type Event struct {
	Category       EventCategory
	Type           string
	Description    string
	Result         ResultType
	ErrorMessage   *string
	ScopeType      *string
	ScopeLabel     *string
	ScopeCount     *int
	ActorType      ActorType
	UserID         *string
	Username       *string
	OrganizationID *int64
	Metadata       map[string]any

	// BatchID links the activity row to a command_batch_log.uuid. For
	// '<event_type>.completed' events this field is used by the unique partial
	// index to guarantee at most one completion row per batch, so the finalizer
	// can be re-run safely after crashes.
	BatchID *string
}

// Filter defines query parameters for listing activity entries.
type Filter struct {
	OrganizationID  int64
	EventCategories []string
	EventTypes      []string
	UserIDs         []string
	ScopeTypes      []string
	SearchText      string
	StartTime       *time.Time
	EndTime         *time.Time
	PageSize        int
	CursorTime      *time.Time
	CursorID        *int64
}

// Entry is the read model returned by Service.List().
type Entry struct {
	ID           int64
	EventID      string
	Category     string
	Type         string
	Description  string
	Result       string
	ErrorMessage *string
	ScopeType    *string
	ScopeLabel   *string
	ScopeCount   *int
	ActorType    string
	UserID       *string
	Username     *string
	CreatedAt    time.Time
	Metadata     json.RawMessage
	BatchID      *string
}

type UserInfo struct {
	UserID   string
	Username string
}

type EventTypeInfo struct {
	EventType     string
	EventCategory string
}

type FilterOptions struct {
	EventTypes []EventTypeInfo
	ScopeTypes []string
	Users      []UserInfo
}
