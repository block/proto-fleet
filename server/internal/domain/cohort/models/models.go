// Package models holds the domain types for cohorts.
package models

import (
	"encoding/json"
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
	SiteID           *int64
	AddedAt          time.Time
	Display          CohortDeviceDisplay
}

// CohortDeviceDisplay is the human-readable fleet metadata shown alongside
// cohort membership/device rows.
type CohortDeviceDisplay struct {
	Name         string
	WorkerName   string
	Manufacturer string
	Model        string
	IPAddress    string
	SerialNumber string
	SiteLabel    string
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
	SiteID  *int64
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
	Manufacturer   string
	Model          string
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
	SiteID    *int64
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
	Assignments           []CohortDeviceAssignment
	CohortIDs             []int64
	OwnerUserIDs          []int64
	IncludeUnowned        bool
	Manufacturers         []string
	Models                []string
	SiteIDs               []int64
	IncludeUnassignedSite bool
	Search                string
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
	SiteID           *int64
}

// DefaultCohortDevice is a device that currently has no explicit cohort
// membership row and therefore belongs to the default cohort.
type DefaultCohortDevice struct {
	DeviceIdentifier string
	SiteID           *int64
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
	SiteID           *int64
	EffectiveCohort  Cohort
	Display          CohortDeviceDisplay
}
