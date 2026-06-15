// Package models holds the domain types for sites.
package models

import "time"

// Site is the canonical domain shape for a site row.
type Site struct {
	ID              int64
	OrgID           int64
	Name            string
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
}

// CreateSiteParams is the input shape for the Create flow.
type CreateSiteParams struct {
	OrgID           int64
	Name            string
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
	OrgID           int64
	ID              int64
	Name            string
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
	UnassignedDeviceCount       int64
	UnassignedRackCount         int64
	DeletedBuildingCount        int64
	DeletedResponseProfileCount int64
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

// ReassignDevicesToSiteParams is the input shape for the bulk reassign
// flow. TargetSiteID == nil means "Unassigned".
type ReassignDevicesToSiteParams struct {
	OrgID             int64
	TargetSiteID      *int64
	DeviceIdentifiers []string
}

// AssignBuildingToSiteParams is the input shape for the building site
// reassignment flow. TargetSiteID == nil means "Unassigned".
type AssignBuildingToSiteParams struct {
	OrgID        int64
	BuildingID   int64
	TargetSiteID *int64
}

// AssignBuildingToSiteResult is the cascade-impact tally for the
// building → site move.
type AssignBuildingToSiteResult struct {
	ReassignedRackCount   int64
	ReassignedDeviceCount int64
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
	SiteID                   int64
	BuildingCount            int32
	DeviceCount              int32
	ReportingCount           int32
	HashrateReportingCount   int32
	EfficiencyReportingCount int32
	PowerReportingCount      int32
	TotalHashrateThs         float64
	AvgEfficiencyJth         float64
	TotalPowerKw             float64
	HashingCount             int32
	BrokenCount              int32
	OfflineCount             int32
	SleepingCount            int32
}
