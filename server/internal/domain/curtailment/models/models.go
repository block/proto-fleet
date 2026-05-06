// Package models defines the domain shapes the curtailment store boundary
// returns, kept independent of sqlc-generated types so the rest of the
// curtailment domain (selector, modes, handler) does not import generated code.
package models

import (
	"time"

	"github.com/google/uuid"
)

// OrgConfig holds the per-org tunables read at handler entry: the default
// max-duration cap (used to normalize max_duration_seconds=0), the candidate
// power floor (the per-org tier of the two-tier resolution; admin-supplied
// override on the request takes precedence), and the cooldown window applied
// by the selector to recently-resolved miners.
type OrgConfig struct {
	OrgID                 int64
	MaxDurationDefaultSec int32
	CandidateMinPowerW    int32
	PostEventCooldownSec  int32
}

// EventState is a typed wrapper for `curtailment_event.state` to keep the
// pending/active/restoring/terminal lifecycle visible in Go.
type EventState string

const (
	EventStatePending               EventState = "pending"
	EventStateActive                EventState = "active"
	EventStateRestoring             EventState = "restoring"
	EventStateCompleted             EventState = "completed"
	EventStateCompletedWithFailures EventState = "completed_with_failures"
	EventStateCancelled             EventState = "cancelled"
	EventStateFailed                EventState = "failed"
)

// IsTerminal reports whether the event has reached a final state.
func (s EventState) IsTerminal() bool {
	switch s {
	case EventStateCompleted, EventStateCompletedWithFailures,
		EventStateCancelled, EventStateFailed:
		return true
	}
	return false
}

// TargetState is a typed wrapper for `curtailment_target.state`.
type TargetState string

const (
	TargetStatePending       TargetState = "pending"
	TargetStateDispatched    TargetState = "dispatched"
	TargetStateConfirmed     TargetState = "confirmed"
	TargetStateDrifted       TargetState = "drifted"
	TargetStateResolved      TargetState = "resolved"
	TargetStateReleased      TargetState = "released"
	TargetStateRestoreFailed TargetState = "restore_failed"
)

// LoopType distinguishes open-loop modes (frozen target set) from
// closed-loop modes that re-evaluate desired targets each tick (v3+).
type LoopType string

const (
	LoopTypeOpen   LoopType = "open"
	LoopTypeClosed LoopType = "closed"
)

// ScopeType identifies how a curtailment request expressed its target set.
type ScopeType string

const (
	ScopeTypeWholeOrg   ScopeType = "whole_org"
	ScopeTypeDeviceSets ScopeType = "device_sets"
	ScopeTypeDeviceList ScopeType = "device_list"
)

// SourceActorType identifies who triggered an event, for audit attribution.
type SourceActorType string

const (
	SourceActorUser      SourceActorType = "user"
	SourceActorAPIKey    SourceActorType = "api_key"
	SourceActorWebhook   SourceActorType = "webhook"
	SourceActorScheduler SourceActorType = "scheduler"
)

// Event mirrors a `curtailment_event` row at the domain boundary. Fields
// whose values are JSON in the DB are exposed as raw bytes; callers that
// need to deserialize them own the schema.
type Event struct {
	ID                      int64
	EventUUID               uuid.UUID
	OrgID                   int64
	State                   EventState
	Mode                    string
	Strategy                string
	Level                   string
	Priority                string
	LoopType                LoopType
	ScopeType               ScopeType
	ScopeJSON               []byte
	ModeParamsJSON          []byte
	RestoreBatchSize        int32
	RestoreBatchIntervalSec int32
	EffectiveBatchSize      *int32
	MinCurtailedDurationSec int32
	MaxDurationSeconds      *int32
	AllowUnbounded          bool
	IncludeMaintenance      bool
	ForceIncludeMaintenance bool
	DecisionSnapshotJSON    []byte
	SourceActorType         SourceActorType
	SourceActorID           *string
	ExternalSource          *string
	ExternalReference       *string
	IdempotencyKey          *string
	SupersedesEventID       *int64
	Reason                  string
	ScheduledStartAt        *time.Time
	StartedAt               *time.Time
	EndedAt                 *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// InsertEventParams captures the fields a caller must supply when inserting
// a new event. Computed fields (id, created_at, updated_at, effective_batch_size)
// are produced by the DB.
type InsertEventParams struct {
	EventUUID               uuid.UUID
	OrgID                   int64
	State                   EventState
	Mode                    string
	Strategy                string
	Level                   string
	Priority                string
	LoopType                LoopType
	ScopeType               ScopeType
	ScopeJSON               []byte
	ModeParamsJSON          []byte
	RestoreBatchSize        int32
	RestoreBatchIntervalSec int32
	MinCurtailedDurationSec int32
	MaxDurationSeconds      *int32
	AllowUnbounded          bool
	IncludeMaintenance      bool
	ForceIncludeMaintenance bool
	DecisionSnapshotJSON    []byte
	SourceActorType         SourceActorType
	SourceActorID           *string
	ExternalSource          *string
	ExternalReference       *string
	IdempotencyKey          *string
	Reason                  string
	ScheduledStartAt        *time.Time
}

// InsertEventResult is what InsertEvent returns to the caller.
type InsertEventResult struct {
	ID        int64
	EventUUID uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Target mirrors a `curtailment_target` row at the domain boundary.
type Target struct {
	CurtailmentEventID    int64
	DeviceIdentifier      string
	TargetType            string
	State                 TargetState
	DesiredState          string
	BaselinePowerW        *float64
	AddedAt               time.Time
	ReleasedAt            *time.Time
	LastDispatchedAt      *time.Time
	LastBatchUUID         *string
	ObservedPowerW        *float64
	ObservedAt            *time.Time
	ConfirmedAt           *time.Time
	RetryCount            int32
	LastError             *string
	SelectorRationaleJSON []byte
}

// InsertTargetParams captures the fields a caller supplies when inserting a
// per-event target row. Many fields default to NULL/zero at the DB level and
// are populated by later reconciler/restorer ticks.
type InsertTargetParams struct {
	CurtailmentEventID    int64
	DeviceIdentifier      string
	TargetType            string
	State                 TargetState
	DesiredState          string
	BaselinePowerW        *float64
	SelectorRationaleJSON []byte
}

// Heartbeat mirrors the singleton liveness row.
type Heartbeat struct {
	ID                 int16
	LastTickAt         time.Time
	LastTickUUID       uuid.UUID
	LastTickDurationMS *int32
	ActiveEventCount   int32
}

// Candidate is per-device state assembled by the curtailment store from a
// cross-table join (device + latest device_metrics + latest
// device_metrics_hourly + device_pairing + device_status). The service layer
// inspects each Candidate to attribute skip reasons (stale telemetry,
// unpaired, wrong device_status, etc.) before handing the survivors to the
// selector. nil-pointer fields mean "no row joined" — the service interprets
// those as their natural skip-reason variant (e.g., absent telemetry → stale).
type Candidate struct {
	DeviceIdentifier string
	DriverName       *string
	Model            string

	// DeviceStatus is the current device_status_enum value as a string
	// (e.g., "ACTIVE", "OFFLINE", "MAINTENANCE", "UPDATING",
	// "REBOOT_REQUIRED"). The empty string means no device_status row.
	DeviceStatus string

	// PairingStatus is the current pairing_status_enum value as a string
	// (e.g., "PAIRED", "UNPAIRED", "PENDING", "FAILED",
	// "AUTHENTICATION_NEEDED"). The store substitutes "UNPAIRED" when no
	// pairing row exists, matching the existing miner-state convention.
	PairingStatus string

	// LatestMetricsAt is the timestamp of the most recent telemetry sample
	// within the staleness window (15 min). nil means no recent sample.
	LatestMetricsAt  *time.Time
	LatestPowerW     *float64
	LatestHashRateHS *float64

	// AvgEfficiencyJH is the latest device_metrics_hourly avg_efficiency
	// value. nil means the continuous aggregate has no row for this
	// device — the selector ranks unknown-efficiency miners last.
	AvgEfficiencyJH *float64
}
