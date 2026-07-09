// Package models holds the domain types for sites.
package models

import (
	"errors"
	"time"
)

// ErrSiteSlugCollision is returned by stores when the generated slug
// lost a race against another live site in the same org. The service
// handles it by generating the next suffix candidate and retrying.
var ErrSiteSlugCollision = errors.New("site slug collision")

// Site is the canonical domain shape for a site row.
type Site struct {
	ID              int64
	OrgID           int64
	Name            string
	Slug            string
	LocationCity    string
	LocationState   string
	Timezone        string
	PowerCapacityMw float64
	NetworkConfig   string
	Address         string
	PostalCode      string
	Country         string
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SiteWithCounts pairs a site with attachment counts. Used by ListSites
// so the delete-confirm dialog has impact numbers without a second
// round trip.
type SiteWithCounts struct {
	Site          Site
	DeviceCount   int64
	BuildingCount int64
	RackCount     int64
	ListStats     *FleetListStats
}

// CreateSiteParams is the input shape for the Create flow.
type CreateSiteParams struct {
	OrgID           int64
	Name            string
	Slug            string
	LocationCity    string
	LocationState   string
	Timezone        string
	PowerCapacityMw float64
	NetworkConfig   string
	Address         string
	PostalCode      string
	Country         string
	Notes           string
}

// UpdateSiteParams is the input shape for the Update flow.
type UpdateSiteParams struct {
	OrgID int64
	ID    int64
	Name  string
	// Slug is populated by the service layer (regenerated from Name on a
	// rename, carried through unchanged otherwise); it is never taken from
	// the API request.
	Slug            string
	LocationCity    string
	LocationState   string
	Timezone        string
	PowerCapacityMw float64
	NetworkConfig   string
	Address         string
	PostalCode      string
	Country         string
	Notes           string
}

// DeleteSiteResult carries the cascade impact for the delete activity log.
type DeleteSiteResult struct {
	UnassignedDeviceCount            int64
	UnassignedRackCount              int64
	DeletedBuildingCount             int64
	DeletedResponseProfileCount      int64
	DeletedInfrastructureDeviceCount int64
}

// PerDeviceConflictReason enumerates why a device was rejected by a
// bulk site-assignment write.
type PerDeviceConflictReason int

const (
	// ReasonUnspecified — default zero value, should never appear in
	// emitted conflicts.
	ReasonUnspecified PerDeviceConflictReason = 0
	// ReasonDeviceNotFound — identifier doesn't match a live device in
	// the org.
	ReasonDeviceNotFound PerDeviceConflictReason = 1
	// ReasonDeviceInRackAtOtherSite — device is in a rack whose site_id
	// differs from the requested target.
	ReasonDeviceInRackAtOtherSite PerDeviceConflictReason = 2
)

// PerDeviceConflict explains why a device was rejected by a bulk
// site-assignment write. Mirrors the proto shape so the handler is a
// thin translator.
type PerDeviceConflict struct {
	DeviceIdentifier  string
	Reason            PerDeviceConflictReason
	ConflictingSiteID int64
}

// AssignDevicesToSiteParams is the input shape for the bulk assign
// flow. TargetSiteID == nil means "Unassigned".
//
// When ForceClearConflictingRackMembership is true the service, inside
// the same transaction as the site write, drops any existing rack
// membership for the listed devices before applying the site update.
// This closes the cross-site reparent orphan window the client-side
// remove-then-reassign loop in MinerReparentPicker had. When false
// (default), a device sitting in a rack at a different site rejects
// the whole batch with PerDeviceConflict[].
type AssignDevicesToSiteParams struct {
	OrgID                               int64
	TargetSiteID                        *int64
	DeviceIdentifiers                   []string
	ForceClearConflictingRackMembership bool
}

// AssignBuildingsToSiteParams is the input shape for the bulk
// building→site assignment flow. TargetSiteID == nil means "Unassigned";
// the entire batch is applied in one transaction.
type AssignBuildingsToSiteParams struct {
	OrgID        int64
	BuildingIDs  []int64
	TargetSiteID *int64
}

// AssignBuildingsToSiteResult is the aggregate cascade-impact tally
// across every building in the batch.
type AssignBuildingsToSiteResult struct {
	ReassignedRackCount   int64
	ReassignedDeviceCount int64
}

// AssignRacksToSiteParams is the input shape for the bulk rack→site
// partial-update flow. TargetSiteID == nil means "Unassigned"; the
// entire batch applies in one transaction.
type AssignRacksToSiteParams struct {
	OrgID        int64
	RackIDs      []int64
	TargetSiteID *int64
}

// AssignRacksToSiteResult carries cascade impact + the count of racks
// whose building_id was cleared on the site transition.
type AssignRacksToSiteResult struct {
	ReassignedDeviceCount int64
	ClearedBuildingCount  int64
}

// SiteNetworkConfigEntry is a (name, network_config) tuple used by the
// service when computing cross-site overlap warnings on save.
type SiteNetworkConfigEntry struct {
	ID            int64
	Name          string
	NetworkConfig string
}

// SiteStats is the rollup returned by GetSiteStats. Scope is every
// live device with site_id matching the requested site, racked or not.
type SiteStats struct {
	SiteID                    int64
	BuildingCount             int32
	RackCount                 int32
	DeviceCount               int32
	ReportingCount            int32
	HashrateReportingCount    int32
	EfficiencyReportingCount  int32
	PowerReportingCount       int32
	TemperatureReportingCount int32
	TotalHashrateThs          float64
	AvgEfficiencyJth          float64
	TotalPowerKw              float64
	MinTemperatureC           float64
	MaxTemperatureC           float64
	HashingCount              int32
	BrokenCount               int32
	OfflineCount              int32
	SleepingCount             int32
	ControlBoardIssueCount    int32
	FanIssueCount             int32
	HashBoardIssueCount       int32
	PsuIssueCount             int32
}

// FleetListStats is the lightweight rollup attached to list rows.
type FleetListStats struct {
	BuildingCount             int32
	RackCount                 int32
	DeviceCount               int32
	ReportingCount            int32
	HashrateReportingCount    int32
	EfficiencyReportingCount  int32
	PowerReportingCount       int32
	TemperatureReportingCount int32
	TotalHashrateThs          float64
	AvgEfficiencyJth          float64
	TotalPowerKw              float64
	MinTemperatureC           float64
	MaxTemperatureC           float64
	HashingCount              int32
	BrokenCount               int32
	OfflineCount              int32
	SleepingCount             int32
	ControlBoardIssueCount    int32
	FanIssueCount             int32
	HashBoardIssueCount       int32
	PsuIssueCount             int32
}
