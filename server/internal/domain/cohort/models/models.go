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
)

// Cohort is the canonical domain shape for a cohort row.
type Cohort struct {
	ID                     int64
	OrgID                  int64
	Label                  string
	IsDefault              bool
	OwnerUserID            *int64
	OwnerUsername          *string
	ExpiresAt              *time.Time
	DesiredFirmwareChannel *string
	DesiredFirmwareFileID  *string
	DesiredConfigJSON      json.RawMessage
	State                  CohortState
	Purpose                string
	SourceActorType        SourceActorType
	SourceActorID          *string
	IdempotencyKey         *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ExplicitMemberCount    int64
	Members                []CohortMember
}

// CohortMember is one explicit non-default membership row.
type CohortMember struct {
	CohortID         int64
	OrgID            int64
	DeviceIdentifier string
	SiteID           *int64
	AddedAt          time.Time
}

// CreateCohortParams is the input shape for cohort creation.
type CreateCohortParams struct {
	OrgID                  int64
	Label                  string
	OwnerUserID            *int64
	OwnerUsername          *string
	ExpiresAt              *time.Time
	DesiredFirmwareChannel *string
	DesiredFirmwareFileID  *string
	DesiredConfigJSON      json.RawMessage
	Purpose                string
	SourceActorType        SourceActorType
	SourceActorID          *string
	IdempotencyKey         *string
	DeviceIdentifiers      []string
}

// ListCohortsParams controls cohort list filtering.
type ListCohortsParams struct {
	OrgID           int64
	IncludeReleased bool
}

// ListCohortsByOwnerParams controls owner-scoped cohort list filtering.
type ListCohortsByOwnerParams struct {
	OrgID           int64
	OwnerUserID     int64
	IncludeReleased bool
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
