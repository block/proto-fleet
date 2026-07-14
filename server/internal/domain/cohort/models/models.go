// Package models holds the domain types for cohorts.
package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// CohortState is the persisted cohort lifecycle state.
type CohortState string

const (
	CohortStateActive   CohortState = "active"
	CohortStateReleased CohortState = "released"
)

// SourceActorType identifies who created or mutated a cohort.
type SourceActorType string

const (
	SourceActorUser      SourceActorType = "user"
	SourceActorAPIKey    SourceActorType = "api_key"
	SourceActorScheduler SourceActorType = "scheduler"
	SourceActorCohort    SourceActorType = "cohort"
)

// CohortDesiredConfig is the typed desired configuration persisted as JSONB.
// It contains references to organization-owned resources, never credentials.
type CohortDesiredConfig struct {
	Pools *CohortPoolDesiredConfig `json:"pools,omitempty"`
}

// CohortPoolDesiredConfig describes the complete ordered pool set for miners.
type CohortPoolDesiredConfig struct {
	PrimaryPoolID int64  `json:"primary_pool_id"`
	Backup1PoolID *int64 `json:"backup_1_pool_id,omitempty"`
	Backup2PoolID *int64 `json:"backup_2_pool_id,omitempty"`
}

type CohortPoolReference struct {
	ID        int64
	URL       string
	Username  string
	UpdatedAt time.Time
}

// MarshalJSON returns nil for an empty desired configuration.
func (c *CohortDesiredConfig) MarshalJSON() ([]byte, error) {
	type alias CohortDesiredConfig
	if c == nil || c.Pools == nil {
		return nil, nil
	}
	raw, err := json.Marshal((*alias)(c))
	if err != nil {
		return nil, fmt.Errorf("marshal cohort desired config: %w", err)
	}
	return raw, nil
}

// ParseCohortDesiredConfig decodes the persisted typed JSON representation.
func ParseCohortDesiredConfig(raw json.RawMessage) (*CohortDesiredConfig, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil, nil
	}
	var config CohortDesiredConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("unmarshal cohort desired config: %w", err)
	}
	return &config, nil
}

// Cohort is the canonical domain shape for a cohort row.
type Cohort struct {
	ID                    int64
	OrgID                 int64
	Label                 string
	IsDefault             bool
	OwnerUserID           *int64
	OwnerUsername         *string
	ExpiresAt             *time.Time
	DesiredFirmwareFileID *string
	DesiredConfig         *CohortDesiredConfig
	DesiredConfigJSON     json.RawMessage
	State                 CohortState
	Purpose               string
	SourceActorType       SourceActorType
	SourceActorID         *string
	IdempotencyKey        *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ExplicitMemberCount   int64
	Members               []CohortMember
	FirmwareTargets       []CohortFirmwareTarget
	FirmwareStatuses      []CohortFirmwareStatus
	FirmwareProgress      CohortFirmwareProgress
	ConfigProgress        []CohortConfigProgress
}

// CohortFirmwareTarget is desired firmware for a single miner manufacturer/model.
type CohortFirmwareTarget struct {
	CohortID       int64
	OrgID          int64
	Manufacturer   string
	Model          string
	FirmwareFileID *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CohortMember is one explicit non-default membership row.
type CohortMember struct {
	CohortID         int64
	OrgID            int64
	DeviceIdentifier string
	AddedAt          time.Time
	Display          CohortDeviceDisplay
	FirmwareStatus   *CohortFirmwareStatus
	ConfigStatuses   []CohortConfigStatus
}

// FirmwareVersionEvent is one observed firmware-version transition.
type FirmwareVersionEvent struct {
	DeviceIdentifier string
	FirmwareVersion  string
	ObservedAt       time.Time
}

// CohortFirmwareVersionHistoryParams controls a historical version-mix query.
type CohortFirmwareVersionHistoryParams struct {
	OrgID       int64
	CohortID    int64
	StartTime   time.Time
	EndTime     time.Time
	Granularity time.Duration
}

// CohortFirmwareVersionCount is the number of current cohort members on one version.
// An empty version represents a member without an observation at that point.
type CohortFirmwareVersionCount struct {
	FirmwareVersion string
	DeviceCount     int32
}

// CohortFirmwareVersionHistoryPoint is a version distribution at one time bucket.
type CohortFirmwareVersionHistoryPoint struct {
	Timestamp time.Time
	Versions  []CohortFirmwareVersionCount
}

// CohortFirmwareVersionHistory is the bucketed version mix for current members.
type CohortFirmwareVersionHistory struct {
	MemberCount int32
	Points      []CohortFirmwareVersionHistoryPoint
}

// CohortDeviceDisplay is the human-readable fleet metadata shown alongside
// cohort membership/device rows.
type CohortDeviceDisplay struct {
	Name            string
	WorkerName      string
	Manufacturer    string
	Model           string
	IPAddress       string
	SerialNumber    string
	FirmwareVersion string
}

// CreateCohortParams is the input shape for cohort creation.
type CreateCohortParams struct {
	OrgID                 int64
	Label                 string
	OwnerUserID           *int64
	OwnerUsername         *string
	ExpiresAt             *time.Time
	DesiredFirmwareFileID *string
	// DesiredFirmwareTargetManufacturer/Model are resolved by the service from
	// DesiredFirmwareFileID so stores can validate transaction-selected members
	// before committing a create.
	DesiredFirmwareTargetManufacturer string
	DesiredFirmwareTargetModel        string
	DesiredConfigJSON                 json.RawMessage
	DesiredConfig                     *CohortDesiredConfig
	Purpose                           string
	SourceActorType                   SourceActorType
	SourceActorID                     *string
	IdempotencyKey                    *string
	DeviceIdentifiers                 []string
	SourceDeviceSetID                 *int64
	DeviceSelector                    *CohortDeviceSelector
}

// CohortDeviceSelector selects available default-cohort devices server-side.
type CohortDeviceSelector struct {
	Count   int32
	Product *string
	Model   *string
}

// UpdateCohortParams is the patch shape for cohort metadata and desired state.
type UpdateCohortParams struct {
	OrgID                    int64
	CohortID                 int64
	Label                    *string
	Purpose                  *string
	ExpiresAt                *time.Time
	ClearExpiresAt           bool
	DesiredFirmwareFileID    *string
	DesiredConfigJSON        json.RawMessage
	DesiredConfig            *CohortDesiredConfig
	ClearDesiredConfig       bool
	DesiredFirmwareFileIDSet bool
	DesiredConfigJSONSet     bool
}

// SetCohortFirmwareTargetParams sets or clears desired firmware for a miner type.
type SetCohortFirmwareTargetParams struct {
	OrgID          int64
	CohortID       int64
	ActorUserID    int64
	ActorRole      string
	Manufacturer   *string
	Model          *string
	FirmwareFileID *string
}

// ListCohortsParams controls cohort list filtering.
type ListCohortsParams struct {
	OrgID           int64
	IncludeReleased bool
	PageSize        int32
	PageToken       string
	Search          string
}

// ListCohortsByOwnerParams controls owner-scoped cohort list filtering.
type ListCohortsByOwnerParams struct {
	OrgID           int64
	OwnerUserID     int64
	IncludeReleased bool
	PageSize        int32
	PageToken       string
	Search          string
}

type PagedCohorts struct {
	Cohorts       []*Cohort
	NextPageToken string
	TotalCount    int32
}

// MembershipMutationParams captures membership move/remove ownership checks.
type MembershipMutationParams struct {
	OrgID             int64
	CohortID          int64
	ActorUserID       int64
	ActorRole         string
	DeviceIdentifiers []string
	// DesiredFirmwareTargetManufacturer/Model are resolved by the service from
	// the target cohort firmware so stores can validate transaction-selected
	// members before committing a move.
	DesiredFirmwareTargetManufacturer string
	DesiredFirmwareTargetModel        string
}

// ListDevicesParams controls effective cohort device visibility.
type ListDevicesParams struct {
	OrgID     int64
	PageSize  int32
	PageToken string
	Filter    CohortDeviceFilter
}

type CohortDeviceAssignment string

const (
	CohortDeviceAssignmentAvailable CohortDeviceAssignment = "available"
	CohortDeviceAssignmentReserved  CohortDeviceAssignment = "reserved"
)

type CohortDeviceFilter struct {
	Assignments    []CohortDeviceAssignment
	CohortIDs      []int64
	OwnerUserIDs   []int64
	IncludeUnowned bool
	Manufacturers  []string
	Models         []string
	Search         string
}

type PagedCohortDevices struct {
	Devices        []CohortDevice
	NextPageToken  string
	TotalCount     int32
	AvailableCount int32
	ReservedCount  int32
}

// InsertCohortMemberParams inserts a single explicit membership.
type InsertCohortMemberParams struct {
	CohortID         int64
	OrgID            int64
	DeviceIdentifier string
}

// DefaultCohortDevice is a device that currently has no explicit cohort
// membership row and therefore belongs to the default cohort.
type DefaultCohortDevice struct {
	DeviceIdentifier string
}

// CohortDeviceOwnership describes a device's current explicit cohort owner.
type CohortDeviceOwnership struct {
	DeviceIdentifier string
	CohortID         int64
	OwnerUserID      *int64
	OwnerUsername    *string
}

// CohortDevice is a fleet device decorated with its effective cohort.
type CohortDevice struct {
	DeviceIdentifier string
	EffectiveCohort  Cohort
	Display          CohortDeviceDisplay
	FirmwareStatus   *CohortFirmwareStatus
	ConfigStatuses   []CohortConfigStatus
}

type CohortConfigDimension string

const CohortConfigDimensionPools CohortConfigDimension = "pools"

type CohortConfigLifecycleState string

const (
	CohortConfigStateUnsupported           CohortConfigLifecycleState = "unsupported"
	CohortConfigStateWaitingForObservation CohortConfigLifecycleState = "waiting_for_observation"
	CohortConfigStateApplying              CohortConfigLifecycleState = "applying"
	CohortConfigStateVerifying             CohortConfigLifecycleState = "verifying"
	CohortConfigStateConverged             CohortConfigLifecycleState = "converged"
	CohortConfigStateHeld                  CohortConfigLifecycleState = "held"
	CohortConfigStateFailed                CohortConfigLifecycleState = "failed"
)

type CohortConfigStatus struct {
	Dimension        CohortConfigDimension
	Supported        bool
	State            CohortConfigLifecycleState
	RetryCount       int32
	LastError        *string
	LastDispatchedAt *time.Time
	ConfirmedAt      *time.Time
	ObservedAt       *time.Time
}

type CohortConfigProgress struct {
	Dimension        CohortConfigDimension
	TargetedCount    int32
	UnsupportedCount int32
	WaitingCount     int32
	ApplyingCount    int32
	VerifyingCount   int32
	ConvergedCount   int32
	HeldCount        int32
	FailedCount      int32
}

type CohortFirmwareRolloutState string

const (
	CohortFirmwareRolloutStateNoTarget       CohortFirmwareRolloutState = "no_target"
	CohortFirmwareRolloutStateQueued         CohortFirmwareRolloutState = "queued"
	CohortFirmwareRolloutStateUpdating       CohortFirmwareRolloutState = "updating"
	CohortFirmwareRolloutStateVerifying      CohortFirmwareRolloutState = "verifying"
	CohortFirmwareRolloutStateComplete       CohortFirmwareRolloutState = "complete"
	CohortFirmwareRolloutStateNeedsAttention CohortFirmwareRolloutState = "needs_attention"
	CohortFirmwareRolloutStateUnknown        CohortFirmwareRolloutState = "unknown"
)

type CohortFirmwareStatus struct {
	DeviceIdentifier       string
	TargetFirmwareFileID   string
	TargetFirmwareVersion  string
	CurrentFirmwareVersion string
	State                  CohortFirmwareRolloutState
	RetryCount             int32
	LastError              *string
	LastDispatchedAt       *time.Time
	ConfirmedAt            *time.Time
	ObservedAt             *time.Time
	EnforcementState       *EnforcementState
	DeviceStatus           string
}

type CohortFirmwareProgress struct {
	TargetedCount       int32
	CompleteCount       int32
	QueuedCount         int32
	UpdatingCount       int32
	VerifyingCount      int32
	NeedsAttentionCount int32
	UnknownCount        int32
}

type EnforcementState string

const (
	EnforcementStatePending     EnforcementState = "pending"
	EnforcementStateDispatching EnforcementState = "dispatching"
	EnforcementStateDispatched  EnforcementState = "dispatched"
	EnforcementStateConfirmed   EnforcementState = "confirmed"
	EnforcementStateDrifted     EnforcementState = "drifted"
	EnforcementStateFailed      EnforcementState = "failed"
	EnforcementStateHeld        EnforcementState = "held"
)

type ConfigEnforcementCandidate struct {
	OrgID               int64
	DeviceIdentifier    string
	DriverName          string
	Manufacturer        string
	Model               string
	WorkerName          string
	CohortID            int64
	ActorUserID         int64
	ActorExternalUserID string
	ActorUsername       string
	DesiredConfig       *CohortDesiredConfig
	Dimension           CohortConfigDimension
	ObservedStateJSON   json.RawMessage
	ObservedStateHash   *string
	ConfigObservedAt    *time.Time
	DesiredStateHash    *string
	Supported           *bool
	State               *EnforcementState
	RetryCount          int32
	LastBatchUUID       *string
	LastDispatchedAt    *time.Time
	ConfirmedAt         *time.Time
	LastError           *string
}

type UpsertDeviceConfigStateParams struct {
	OrgID             int64
	DeviceIdentifier  string
	Dimension         CohortConfigDimension
	ObservedStateJSON json.RawMessage
	ObservedStateHash string
	ObservedAt        time.Time
}

type ConfigEnforcementMutationParams struct {
	OrgID             int64
	DeviceIdentifier  string
	Dimension         CohortConfigDimension
	DesiredStateHash  string
	State             EnforcementState
	LastBatchUUID     string
	LastDispatchedAt  time.Time
	ConfirmedAt       time.Time
	ObservedAt        time.Time
	LastError         string
	MaxRetries        int32
	DispatchingBefore time.Time
	Supported         bool
}

type FirmwareEnforcementCandidate struct {
	OrgID                       int64
	DeviceIdentifier            string
	Manufacturer                string
	Model                       string
	CohortID                    int64
	OwnerUserID                 *int64
	OwnerUsername               *string
	ActorUserID                 int64
	ActorExternalUserID         string
	ActorUsername               string
	FirmwareFileID              string
	StateDesiredFirmwareFileID  *string
	StateDesiredFirmwareVersion *string
	DesiredFirmwareVersion      string
	ObservedFirmwareVersion     *string
	FirmwareObservedAt          *time.Time
	State                       *EnforcementState
	RetryCount                  int32
	LastBatchUUID               *string
	LastDispatchedAt            *time.Time
	ConfirmedAt                 *time.Time
	LastError                   *string
}

type ClaimFirmwareDispatchParams struct {
	OrgID                  int64
	DeviceIdentifier       string
	DesiredFirmwareFileID  string
	DesiredFirmwareVersion string
	DispatchingBefore      time.Time
}

type MarkFirmwareDispatchedParams struct {
	OrgID                  int64
	DeviceIdentifier       string
	DesiredFirmwareFileID  string
	DesiredFirmwareVersion string
	LastBatchUUID          string
	LastDispatchedAt       time.Time
}

type MarkFirmwareConfirmedParams struct {
	OrgID                  int64
	DeviceIdentifier       string
	DesiredFirmwareFileID  string
	DesiredFirmwareVersion string
	ConfirmedAt            time.Time
	ObservedAt             time.Time
}

type MarkFirmwareDriftedParams struct {
	OrgID            int64
	DeviceIdentifier string
	ObservedAt       time.Time
}

type MarkFirmwareDispatchFailureParams struct {
	OrgID                  int64
	DeviceIdentifier       string
	DesiredFirmwareFileID  string
	DesiredFirmwareVersion string
	RetryState             EnforcementState
	LastError              string
	MaxRetries             int32
}

type MarkFirmwareDispatchHeldParams struct {
	OrgID                  int64
	DeviceIdentifier       string
	DesiredFirmwareFileID  string
	DesiredFirmwareVersion string
	RetryState             EnforcementState
	LastError              string
}
