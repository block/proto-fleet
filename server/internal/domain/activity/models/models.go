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
	ActorCohort      ActorType = "cohort"
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
	case ActorUser, ActorSystem, ActorScheduler, ActorCurtailment, ActorCohort:
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

	// MultiSite marks a multi-device fleet event whose touched device set
	// spans more than one site scope (#538). Such events carry SiteID == nil
	// and instead record their full touched-site set in MemberSiteIDs /
	// TouchesUnassigned, persisted to the activity_log_site side table so the
	// event surfaces under EACH of its sites. MultiSite is the cheap
	// discriminator the read filter checks before probing the side table; it
	// is true exactly when the side table has membership rows.
	MultiSite bool

	// MemberSiteIDs is the distinct set of (non-nil) sites a MultiSite event
	// touched. Empty for single-site / org-level events (those use the scalar
	// SiteID or neither). Each becomes one activity_log_site row.
	MemberSiteIDs []int64

	// TouchesUnassigned records that a MultiSite event also touched site-less
	// devices, so it surfaces in the /unassigned bucket too (via a NULL-site
	// activity_log_site row). Only meaningful when MultiSite is true.
	TouchesUnassigned bool
}

// SiteScope is the resolved site footprint of a multi-device event, produced
// by ResolveSiteScope and applied to an Event via ApplySiteScope. It encodes
// exactly one of three states (see the table in ResolveSiteScope):
// single-site (SiteID set), org/site-less (all zero), or multi-site
// (MultiSite true with the touched-site set).
type SiteScope struct {
	SiteID            *int64
	MultiSite         bool
	MemberSiteIDs     []int64
	TouchesUnassigned bool
}

// ApplySiteScope stamps a resolved SiteScope onto the event.
func (e *Event) ApplySiteScope(s SiteScope) {
	e.SiteID = s.SiteID
	e.MultiSite = s.MultiSite
	e.MemberSiteIDs = s.MemberSiteIDs
	e.TouchesUnassigned = s.TouchesUnassigned
}

// ResolveSiteScope reduces the distinct site_ids touched by a multi-device
// event into the SiteScope stamped on its activity row. The input is the
// distinct set of device site_ids (a nil entry represents a site-less
// device). A "slot" is a distinct real site OR the single "unassigned" slot
// (present iff any device is site-less); the representation is chosen by the
// number of slots:
//
//   - 1 slot, a real site → SiteID set (single-site fast path; /{site}).
//   - 1 slot, unassigned (every device site-less), or 0 slots (empty) →
//     all-zero scope: SiteID nil, not multi-site (stays in /unassigned).
//   - ≥2 slots → MultiSite, with MemberSiteIDs = the real sites and
//     TouchesUnassigned = whether a site-less device was in the set. The
//     event surfaces under each member site, and /unassigned iff
//     TouchesUnassigned.
func ResolveSiteScope(siteIDs []*int64) SiteScope {
	seen := make(map[int64]struct{}, len(siteIDs))
	var realSites []int64
	hasUnassigned := false
	for _, s := range siteIDs {
		if s == nil {
			hasUnassigned = true
			continue
		}
		if _, dup := seen[*s]; dup {
			continue
		}
		seen[*s] = struct{}{}
		realSites = append(realSites, *s)
	}

	slots := len(realSites)
	if hasUnassigned {
		slots++
	}

	switch {
	case slots >= 2:
		return SiteScope{MultiSite: true, MemberSiteIDs: realSites, TouchesUnassigned: hasUnassigned}
	case len(realSites) == 1:
		site := realSites[0]
		return SiteScope{SiteID: &site}
	default:
		// 0 slots (empty input) or the single "unassigned" slot: no single
		// site to stamp and not multi-site — stays in the unassigned bucket.
		return SiteScope{}
	}
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
