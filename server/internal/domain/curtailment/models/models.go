// Package models defines the curtailment domain types, kept independent of
// sqlc-generated types so the selector / modes / handler don't import them.
package models

import (
	"time"

	"github.com/google/uuid"
)

// OrgConfig is the per-org tunable triple: max-duration default,
// candidate-power floor, and cooldown window.
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
	case EventStatePending, EventStateActive, EventStateRestoring:
		return false
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
// closed-loop modes that re-evaluate desired targets each tick.
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

// Mode is the curtailment dispatch mode. Currently FIXED_KW only;
// reserved values are rejected by the service validator.
type Mode string

const (
	ModeFixedKw Mode = "FIXED_KW"
)

// Strategy is the candidate-ranking strategy. Currently
// LEAST_EFFICIENT_FIRST only; reserved values are rejected by the validator.
type Strategy string

const (
	StrategyLeastEfficientFirst Strategy = "LEAST_EFFICIENT_FIRST"
)

// Level is the curtailment depth. The Fleet event layer dispatches FULL
// only; EFFICIENCY is plumbed at the SDK/plugin layer.
type Level string

const (
	LevelFull Level = "FULL"
)

// Priority controls cooldown / hysteresis bypass. EMERGENCY skips both;
// HIGH is proto-reserved but rejected by the validator.
type Priority string

const (
	PriorityNormal    Priority = "NORMAL"
	PriorityEmergency Priority = "EMERGENCY"
	PriorityHigh      Priority = "HIGH"
)

// Event represents a `curtailment_event` row; JSON columns are raw bytes.
type Event struct {
	ID                      int64
	EventUUID               uuid.UUID
	OrgID                   int64
	State                   EventState
	Mode                    Mode
	Strategy                Strategy
	Level                   Level
	Priority                Priority
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
	CreatedByUserID         int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// InsertEventParams is the caller-supplied fields; id / created_at /
// updated_at / effective_batch_size come from the DB.
type InsertEventParams struct {
	EventUUID               uuid.UUID
	OrgID                   int64
	State                   EventState
	Mode                    Mode
	Strategy                Strategy
	Level                   Level
	Priority                Priority
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
	CreatedByUserID         int64
}

// InsertEventResult is what InsertEventWithTargets returns to the caller.
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
