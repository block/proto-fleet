// Package models holds the domain types for miner channels.
package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// MinerChannelState is the persisted miner channel lifecycle state.
type MinerChannelState string

const (
	MinerChannelStateActive   MinerChannelState = "active"
	MinerChannelStateReleased MinerChannelState = "released"
)

// SourceActorType identifies who created or mutated a miner channel.
type SourceActorType string

const (
	SourceActorUser         SourceActorType = "user"
	SourceActorAPIKey       SourceActorType = "api_key"
	SourceActorScheduler    SourceActorType = "scheduler"
	SourceActorMinerChannel SourceActorType = "miner_channel"
)

// MinerChannelDesiredConfig is the typed desired configuration persisted as JSONB.
// It contains references to organization-owned resources, never credentials.
type MinerChannelDesiredConfig struct {
	Pools *MinerChannelPoolDesiredConfig `json:"pools,omitempty"`
}

// MinerChannelPoolDesiredConfig describes the complete ordered pool set for miners.
type MinerChannelPoolDesiredConfig struct {
	PrimaryPoolID int64  `json:"primary_pool_id"`
	Backup1PoolID *int64 `json:"backup_1_pool_id,omitempty"`
	Backup2PoolID *int64 `json:"backup_2_pool_id,omitempty"`
}

type MinerChannelPoolReference struct {
	ID        int64
	URL       string
	Username  string
	UpdatedAt time.Time
}

// MarshalJSON returns nil for an empty desired configuration.
func (c *MinerChannelDesiredConfig) MarshalJSON() ([]byte, error) {
	type alias MinerChannelDesiredConfig
	if c == nil || c.Pools == nil {
		return nil, nil
	}
	raw, err := json.Marshal((*alias)(c))
	if err != nil {
		return nil, fmt.Errorf("marshal miner channel desired config: %w", err)
	}
	return raw, nil
}

// ParseMinerChannelDesiredConfig decodes the persisted typed JSON representation.
func ParseMinerChannelDesiredConfig(raw json.RawMessage) (*MinerChannelDesiredConfig, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil, nil
	}
	var config MinerChannelDesiredConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("unmarshal miner channel desired config: %w", err)
	}
	return &config, nil
}

// MinerChannel is the canonical domain shape for a miner channel row.
type MinerChannel struct {
	ID                  int64
	OrgID               int64
	Label               string
	IsDefault           bool
	OwnerUserID         *int64
	OwnerUsername       *string
	ExpiresAt           *time.Time
	DesiredConfig       *MinerChannelDesiredConfig
	DesiredConfigJSON   json.RawMessage
	State               MinerChannelState
	Purpose             string
	SourceActorType     SourceActorType
	SourceActorID       *string
	IdempotencyKey      *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ExplicitMemberCount int64
	Members             []MinerChannelMember
	FirmwareTargets     []MinerChannelFirmwareTarget
	FirmwareStatuses    []MinerChannelFirmwareStatus
	FirmwareProgress    MinerChannelFirmwareProgress
	ConfigProgress      []MinerChannelConfigProgress
}

// MinerChannelFirmwareTarget is desired firmware for a single miner manufacturer/model.
type MinerChannelFirmwareTarget struct {
	MinerChannelID int64
	OrgID          int64
	Manufacturer   string
	Model          string
	FirmwareFileID *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MinerChannelMember is one explicit non-default membership row.
type MinerChannelMember struct {
	MinerChannelID   int64
	OrgID            int64
	DeviceIdentifier string
	AddedAt          time.Time
	Display          MinerChannelDeviceDisplay
	FirmwareStatus   *MinerChannelFirmwareStatus
	ConfigStatuses   []MinerChannelConfigStatus
}

// MinerChannelDeviceDisplay is the human-readable fleet metadata shown alongside
// miner channel membership/device rows.
type MinerChannelDeviceDisplay struct {
	Name            string
	WorkerName      string
	Manufacturer    string
	Model           string
	IPAddress       string
	SerialNumber    string
	FirmwareVersion string
}

// CreateMinerChannelParams is the input shape for miner channel creation.
type CreateMinerChannelParams struct {
	OrgID             int64
	Label             string
	OwnerUserID       *int64
	OwnerUsername     *string
	ExpiresAt         *time.Time
	DesiredConfigJSON json.RawMessage
	DesiredConfig     *MinerChannelDesiredConfig
	Purpose           string
	SourceActorType   SourceActorType
	SourceActorID     *string
	IdempotencyKey    *string
	DeviceIdentifiers []string
	SourceDeviceSetID *int64
	DeviceSelector    *MinerChannelDeviceSelector
}

// MinerChannelDeviceSelector selects available default-miner channel devices server-side.
type MinerChannelDeviceSelector struct {
	Count   int32
	Product *string
	Model   *string
}

// UpdateMinerChannelParams is the patch shape for miner channel metadata and desired state.
type UpdateMinerChannelParams struct {
	OrgID                int64
	MinerChannelID       int64
	Label                *string
	Purpose              *string
	ExpiresAt            *time.Time
	ClearExpiresAt       bool
	DesiredConfigJSON    json.RawMessage
	DesiredConfig        *MinerChannelDesiredConfig
	ClearDesiredConfig   bool
	DesiredConfigJSONSet bool
}

// SetMinerChannelFirmwareTargetParams sets or clears desired firmware for a miner type.
type SetMinerChannelFirmwareTargetParams struct {
	OrgID          int64
	MinerChannelID int64
	ActorUserID    int64
	ActorRole      string
	Manufacturer   *string
	Model          *string
	FirmwareFileID *string
}

// ListMinerChannelsParams controls miner channel list filtering.
type ListMinerChannelsParams struct {
	OrgID           int64
	IncludeReleased bool
	PageSize        int32
	PageToken       string
	Search          string
}

// ListMinerChannelsByOwnerParams controls owner-scoped miner channel list filtering.
type ListMinerChannelsByOwnerParams struct {
	OrgID           int64
	OwnerUserID     int64
	IncludeReleased bool
	PageSize        int32
	PageToken       string
	Search          string
}

type PagedMinerChannels struct {
	MinerChannels []*MinerChannel
	NextPageToken string
	TotalCount    int32
}

// MembershipMutationParams captures membership move/remove ownership checks.
type MembershipMutationParams struct {
	OrgID             int64
	MinerChannelID    int64
	ActorUserID       int64
	ActorRole         string
	DeviceIdentifiers []string
}

// ListDevicesParams controls effective miner channel device visibility.
type ListDevicesParams struct {
	OrgID     int64
	PageSize  int32
	PageToken string
	Filter    MinerChannelDeviceFilter
}

type MinerChannelDeviceAssignment string

const (
	MinerChannelDeviceAssignmentAvailable MinerChannelDeviceAssignment = "available"
	MinerChannelDeviceAssignmentReserved  MinerChannelDeviceAssignment = "reserved"
)

type MinerChannelDeviceFilter struct {
	Assignments     []MinerChannelDeviceAssignment
	MinerChannelIDs []int64
	OwnerUserIDs    []int64
	IncludeUnowned  bool
	Manufacturers   []string
	Models          []string
	Search          string
}

type PagedMinerChannelDevices struct {
	Devices        []MinerChannelDevice
	NextPageToken  string
	TotalCount     int32
	AvailableCount int32
	ReservedCount  int32
}

// InsertMinerChannelMemberParams inserts a single explicit membership.
type InsertMinerChannelMemberParams struct {
	MinerChannelID   int64
	OrgID            int64
	DeviceIdentifier string
}

// DefaultMinerChannelDevice is a device that currently has no explicit miner channel
// membership row and therefore belongs to the default miner channel.
type DefaultMinerChannelDevice struct {
	DeviceIdentifier string
}

// MinerChannelDeviceOwnership describes a device's current explicit miner channel owner.
type MinerChannelDeviceOwnership struct {
	DeviceIdentifier string
	MinerChannelID   int64
	OwnerUserID      *int64
	OwnerUsername    *string
}

// MinerChannelDevice is a fleet device decorated with its effective miner channel.
type MinerChannelDevice struct {
	DeviceIdentifier      string
	EffectiveMinerChannel MinerChannel
	Display               MinerChannelDeviceDisplay
	FirmwareStatus        *MinerChannelFirmwareStatus
	ConfigStatuses        []MinerChannelConfigStatus
}

type MinerChannelConfigDimension string

const MinerChannelConfigDimensionPools MinerChannelConfigDimension = "pools"

type MinerChannelConfigLifecycleState string

const (
	MinerChannelConfigStateUnsupported           MinerChannelConfigLifecycleState = "unsupported"
	MinerChannelConfigStateWaitingForObservation MinerChannelConfigLifecycleState = "waiting_for_observation"
	MinerChannelConfigStateApplying              MinerChannelConfigLifecycleState = "applying"
	MinerChannelConfigStateVerifying             MinerChannelConfigLifecycleState = "verifying"
	MinerChannelConfigStateConverged             MinerChannelConfigLifecycleState = "converged"
	MinerChannelConfigStateHeld                  MinerChannelConfigLifecycleState = "held"
	MinerChannelConfigStateFailed                MinerChannelConfigLifecycleState = "failed"
)

type MinerChannelConfigStatus struct {
	Dimension        MinerChannelConfigDimension
	Supported        bool
	State            MinerChannelConfigLifecycleState
	RetryCount       int32
	LastError        *string
	LastDispatchedAt *time.Time
	ConfirmedAt      *time.Time
	ObservedAt       *time.Time
}

type MinerChannelConfigProgress struct {
	Dimension        MinerChannelConfigDimension
	TargetedCount    int32
	UnsupportedCount int32
	WaitingCount     int32
	ApplyingCount    int32
	VerifyingCount   int32
	ConvergedCount   int32
	HeldCount        int32
	FailedCount      int32
}

type MinerChannelFirmwareRolloutState string

const (
	MinerChannelFirmwareRolloutStateNoTarget       MinerChannelFirmwareRolloutState = "no_target"
	MinerChannelFirmwareRolloutStateQueued         MinerChannelFirmwareRolloutState = "queued"
	MinerChannelFirmwareRolloutStateUpdating       MinerChannelFirmwareRolloutState = "updating"
	MinerChannelFirmwareRolloutStateVerifying      MinerChannelFirmwareRolloutState = "verifying"
	MinerChannelFirmwareRolloutStateComplete       MinerChannelFirmwareRolloutState = "complete"
	MinerChannelFirmwareRolloutStateNeedsAttention MinerChannelFirmwareRolloutState = "needs_attention"
	MinerChannelFirmwareRolloutStateUnknown        MinerChannelFirmwareRolloutState = "unknown"
)

type MinerChannelFirmwareStatus struct {
	DeviceIdentifier       string
	TargetFirmwareFileID   string
	TargetFirmwareVersion  string
	CurrentFirmwareVersion string
	State                  MinerChannelFirmwareRolloutState
	RetryCount             int32
	LastError              *string
	LastDispatchedAt       *time.Time
	ConfirmedAt            *time.Time
	ObservedAt             *time.Time
	EnforcementState       *EnforcementState
	DeviceStatus           string
}

type MinerChannelFirmwareProgress struct {
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
	MinerChannelID      int64
	ActorUserID         int64
	ActorExternalUserID string
	ActorUsername       string
	DesiredConfig       *MinerChannelDesiredConfig
	Dimension           MinerChannelConfigDimension
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
	Dimension         MinerChannelConfigDimension
	ObservedStateJSON json.RawMessage
	ObservedStateHash string
	ObservedAt        time.Time
}

type ConfigEnforcementMutationParams struct {
	OrgID             int64
	DeviceIdentifier  string
	Dimension         MinerChannelConfigDimension
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
	MinerChannelID              int64
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
