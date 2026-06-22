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
	CategoryCurtailment     EventCategory = "curtailment"
	CategorySystem          EventCategory = "system"
)

type ActorType string

const (
	ActorUser        ActorType = "user"
	ActorSystem      ActorType = "system"
	ActorScheduler   ActorType = "scheduler"
	ActorCurtailment ActorType = "curtailment"
)

type ResultType string

const (
	ResultSuccess ResultType = "success"
	ResultFailure ResultType = "failure"
)

// orgLevelCategories are the event categories with no single-site concept for
// their DIRECT (non-batch) rows: login/auth, system events, mining-pool config,
// schedules, curtailment, and device-command audits. They are the single source
// of truth for the "unassigned" activity bucket: a direct (batch_id IS NULL) row
// with site_id IS NULL only belongs in /{unassigned}/activity if its category is
// NOT one of these, so org-level events surface only in the all-sites feed and
// never pollute a site bucket.
//
// Note this only governs the direct-event branch. device_command BATCH rows
// carry a batch_id and are scoped via command_on_device_log (the EXISTS branch),
// independent of this list; the only direct device_command rows are the
// preflight-blocked / filter-skip audits, which span the requested device set
// (no single site) and so are org-level. Site-scoped curtailment rows stamp a
// site_id and thus never reach the unassigned sub-condition; only whole-org /
// device curtailments stay NULL and lean on this list.
//
// Backed by an array (not a slice) so the source can't be mutated; callers get
// a fresh copy via OrgLevelCategories().
var orgLevelCategories = [...]EventCategory{
	CategoryAuth,
	CategorySystem,
	CategoryPool,
	CategorySchedule,
	CategoryCurtailment,
	CategoryDeviceCommand,
}

// OrgLevelCategories returns the org-level categories as a fresh string slice
// (the read queries take []string). A new slice per call keeps the package-level
// source immutable from the caller's side.
func OrgLevelCategories() []string {
	out := make([]string, len(orgLevelCategories))
	for i, c := range orgLevelCategories {
		out[i] = string(c)
	}
	return out
}

func (c EventCategory) Valid() bool {
	switch c {
	case CategoryAuth, CategoryDeviceCommand, CategoryFleetManagement,
		CategoryCollection, CategoryPool, CategorySchedule,
		CategoryCurtailment, CategorySystem:
		return true
	}
	return false
}

func (a ActorType) Valid() bool {
	switch a {
	case ActorUser, ActorSystem, ActorScheduler, ActorCurtailment:
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

	// SiteID is row-stamped at write time so per-site activity feeds
	// don't shift when the device or scope is later reassigned. Callers
	// emitting site-scoped events (site/building CRUD, device reassign,
	// device-driven actions) populate it from the row's authoritative
	// site at event time. Nil for org-scoped events that don't tie to
	// a specific site.
	SiteID *int64
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

	// SiteIDs / IncludeUnassigned form the additive site scope, identical in
	// shape to the buildings/racks/miners filters. Empty SiteIDs + false →
	// no site filter (org-wide feed). See OrgLevelCategories for how the
	// unassigned bucket excludes org-level events.
	SiteIDs           []int64
	IncludeUnassigned bool
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
