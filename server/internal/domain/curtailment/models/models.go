// Package models defines the curtailment domain types, kept independent of
// sqlc-generated types so the selector / modes / handler don't import them.
package models

import (
	"time"

	"github.com/google/uuid"
)

// OrgConfig is the per-org default duration and candidate-power floor.
type OrgConfig struct {
	OrgID                 int64
	MaxDurationDefaultSec int32
	CandidateMinPowerW    int32
}

// ResponseProfile is reusable curtailment response behavior. Automation rules
// bind source triggers to these profiles; profiles themselves do not execute.
type ResponseProfile struct {
	ID                          int64
	OrgID                       int64
	ProfileName                 string
	SiteID                      *int64
	ScopeJSON                   []byte
	Mode                        Mode
	Strategy                    Strategy
	Level                       Level
	Priority                    Priority
	TargetKW                    *float64
	ToleranceKW                 *float64
	CurtailBatchSize            *int32
	CurtailBatchIntervalSec     int32
	RestoreBatchSize            int32
	RestoreBatchIntervalSec     int32
	IncludeMaintenance          bool
	ForceIncludeMaintenance     bool
	ForceIncludeAllPairedMiners bool
	PostEventCooldownSec        int32
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

// AutomationTriggerType identifies the kind of signal an automation rule
// listens to. MQTT is the only supported v1 trigger.
type AutomationTriggerType string

const (
	AutomationTriggerTypeMQTT AutomationTriggerType = "MQTT"
)

// AutomationSignal is the latest normalized trigger state for a rule.
type AutomationSignal string

const (
	AutomationSignalOff AutomationSignal = "OFF"
	AutomationSignalOn  AutomationSignal = "ON"
)

// AutomationRule binds an external trigger source to a response profile.
type AutomationRule struct {
	ID                       int64
	OrgID                    int64
	RuleName                 string
	TriggerType              AutomationTriggerType
	MQTTSourceID             int64
	MQTTSourceName           string
	ResponseProfileID        int64
	ResponseProfileName      string
	ResponseProfileSiteID    *int64
	ResponseProfileScopeJSON []byte
	Enabled                  bool
	LastSignal               *AutomationSignal
	LastSignalAt             *time.Time
	ActiveEventUUID          *uuid.UUID
	LastStartedAt            *time.Time
	LastRestoredAt           *time.Time
	LastError                *string
	LastErrorAt              *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// MQTTSourceActiveCurtailment identifies an MQTT source (enabled or not)
// whose automation has a non-terminal curtailment event.
type MQTTSourceActiveCurtailment struct {
	SourceID       int64
	OrganizationID int64
	SourceName     string
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

// DesiredState values for `curtailment_target.desired_state`. The active
// phase requests "curtailed"; Stop flips non-terminal targets to "active"
// (restore phase). Plain strings rather than a typed alias to match the
// existing column shape; constants give callers a single source of truth.
const (
	DesiredStateCurtailed = "curtailed"
	DesiredStateActive    = "active"
)

// TargetState is a typed wrapper for `curtailment_target.state`.
type TargetState string

const (
	TargetStatePending TargetState = "pending"
	// TargetStateDispatching is the pre-command transient. AdminTerminate's
	// in-flight gate counts DISPATCHING+desired_state='curtailed' rows so a
	// terminate cannot race past an outstanding Curtail.
	TargetStateDispatching   TargetState = "dispatching"
	TargetStateDispatched    TargetState = "dispatched"
	TargetStateConfirmed     TargetState = "confirmed"
	TargetStateDrifted       TargetState = "drifted"
	TargetStateUnavailable   TargetState = "unavailable"
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
	ScopeTypeSite       ScopeType = "site"
	ScopeTypeDeviceSets ScopeType = "device_sets"
	ScopeTypeDeviceList ScopeType = "device_list"
	ScopeTypeMixed      ScopeType = "mixed"
)

// SourceActorType identifies who triggered an event, for audit attribution.
type SourceActorType string

const (
	SourceActorUser       SourceActorType = "user"
	SourceActorAPIKey     SourceActorType = "api_key"
	SourceActorWebhook    SourceActorType = "webhook"
	SourceActorScheduler  SourceActorType = "scheduler"
	SourceActorAutomation SourceActorType = "automation"
)

// Mode is the curtailment dispatch mode. FIXED_KW (select until a kW target)
// and FULL_FLEET (curtail every eligible candidate in scope); reserved values
// are rejected by the service validator.
type Mode string

const (
	ModeFixedKw   Mode = "FIXED_KW"
	ModeFullFleet Mode = "FULL_FLEET"
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

// Priority controls curtailment urgency. HIGH is proto-reserved but rejected
// by the validator.
type Priority string

const (
	PriorityNormal    Priority = "NORMAL"
	PriorityEmergency Priority = "EMERGENCY"
	PriorityHigh      Priority = "HIGH"
)

// Event represents a `curtailment_event` row; JSON columns are raw bytes.
type Event struct {
	ID                          int64
	EventUUID                   uuid.UUID
	OrgID                       int64
	State                       EventState
	Mode                        Mode
	Strategy                    Strategy
	Level                       Level
	Priority                    Priority
	LoopType                    LoopType
	ScopeType                   ScopeType
	ScopeJSON                   []byte
	ModeParamsJSON              []byte
	CurtailBatchSize            *int32
	CurtailBatchIntervalSec     int32
	RestoreBatchSize            int32
	RestoreBatchIntervalSec     int32
	EffectiveBatchSize          *int32
	MinCurtailedDurationSec     int32
	MaxDurationSeconds          *int32
	AllowUnbounded              bool
	IncludeMaintenance          bool
	ForceIncludeMaintenance     bool
	ForceIncludeAllPairedMiners bool
	DecisionSnapshotJSON        []byte
	SourceActorType             SourceActorType
	SourceActorID               *string
	ExternalSource              *string
	ExternalReference           *string
	IdempotencyKey              *string
	SupersedesEventID           *int64
	Reason                      string
	ScheduledStartAt            *time.Time
	StartedAt                   *time.Time
	EndedAt                     *time.Time
	CreatedByUserID             int64
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	TargetRollup                *TargetRollup
	TargetSiteCoverage          *TargetSiteCoverage
}

// TargetRollup summarizes all target rows for an event. Counts stay int64 at
// the domain boundary because SQL COUNT returns int64; handlers clamp to the
// proto int32 fields when rendering.
type TargetRollup struct {
	Pending            int64
	Dispatched         int64
	Confirmed          int64
	Drifted            int64
	Resolved           int64
	Released           int64
	RestoreFailed      int64
	Unavailable        int64
	Total              int64
	UnavailableReasons []TargetUnavailableReasonCount
}

// TargetUnavailableReasonCount summarizes unavailable target reasons.
type TargetUnavailableReasonCount struct {
	Reason string
	Count  int64
}

// TargetSiteCoverage summarizes how persisted target rows map back to current
// device site assignments. Incomplete coverage means at least one target no
// longer resolves to a live device with a site.
type TargetSiteCoverage struct {
	SiteIDs           []int64
	Complete          bool
	TargetCount       int64
	MappedTargetCount int64
}

func (c TargetSiteCoverage) UnknownTargetCount() int64 {
	unknown := c.TargetCount - c.MappedTargetCount
	if unknown < 0 {
		return 0
	}
	return unknown
}

// InsertEventParams is the caller-supplied fields; id / created_at /
// updated_at come from the DB. EffectiveBatchSize is computed at Start
// from the selected-target count and stamped here.
type InsertEventParams struct {
	EventUUID                   uuid.UUID
	OrgID                       int64
	State                       EventState
	Mode                        Mode
	Strategy                    Strategy
	Level                       Level
	Priority                    Priority
	LoopType                    LoopType
	ScopeType                   ScopeType
	ScopeJSON                   []byte
	ModeParamsJSON              []byte
	CurtailBatchSize            *int32
	CurtailBatchIntervalSec     int32
	RestoreBatchSize            int32
	RestoreBatchIntervalSec     int32
	MinCurtailedDurationSec     int32
	MaxDurationSeconds          *int32
	AllowUnbounded              bool
	IncludeMaintenance          bool
	ForceIncludeMaintenance     bool
	ForceIncludeAllPairedMiners bool
	DecisionSnapshotJSON        []byte
	SourceActorType             SourceActorType
	SourceActorID               *string
	ExternalSource              *string
	ExternalReference           *string
	IdempotencyKey              *string
	Reason                      string
	ScheduledStartAt            *time.Time
	StartedAt                   *time.Time
	// EndedAt is set only when an event is inserted already terminal — a
	// vacuously-COMPLETED FULL_FLEET start with no eligible targets — so the
	// completion time is recorded; the reconciler/restorer set it otherwise.
	EndedAt            *time.Time
	CreatedByUserID    int64
	EffectiveBatchSize int32
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
	CurtailPhase          TargetPhaseSummary
	RestorePhase          *TargetPhaseSummary
}

// TargetPhase identifies the durable per-phase target summary that backs
// historical activity expansion.
type TargetPhase string

const (
	TargetPhaseCurtail TargetPhase = "curtail"
	TargetPhaseRestore TargetPhase = "restore"
)

// TargetPhaseSummary preserves the outcome of a curtail or restore phase even
// when the rolling target state/cursor columns are reset for the next phase.
type TargetPhaseSummary struct {
	Phase        TargetPhase
	State        TargetState
	StartedAt    *time.Time
	DispatchedAt *time.Time
	BatchUUID    *string
	CompletedAt  *time.Time
	RetryCount   int32
	FailureCount int32
	LastError    *string
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
	LastError             *string
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

// Candidate is per-device state assembled by the curtailment store from
// a cross-table join (device + telemetry + pairing + status). nil-pointer
// fields mean "no row joined" and map to the natural skip reason (e.g.,
// absent telemetry → stale).
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
