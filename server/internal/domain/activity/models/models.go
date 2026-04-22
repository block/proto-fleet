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
	case ResultSuccess, ResultFailure:
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
// terminal row emitted by the batch finalizer. The partial unique index on
// (batch_id, event_type) for '*.completed' rows keeps finalizer retries
// idempotent.
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

	// BatchID links the activity row to a command_batch_log.uuid. The
	// partial unique index on (batch_id, event_type) for '%.completed'
	// event types guarantees at most one completion row per batch.
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
