// Package models holds the domain types for buildings.
package models

import "time"

// RackOrderIndex mirrors the proto enum and the SMALLINT stored in
// device_set_rack.order_index / building.default_rack_order_index. We
// re-declare it as a typed constant set so the domain layer is
// independent of the proto package.
type RackOrderIndex int16

const (
	RackOrderIndexUnspecified RackOrderIndex = 0
	RackOrderIndexBottomLeft  RackOrderIndex = 1
	RackOrderIndexTopLeft     RackOrderIndex = 2
	RackOrderIndexBottomRight RackOrderIndex = 3
	RackOrderIndexTopRight    RackOrderIndex = 4
)

// Valid reports whether the value matches one of the defined enum
// members. Used to reject malformed proto inputs at the service edge.
func (r RackOrderIndex) Valid() bool {
	return r >= RackOrderIndexUnspecified && r <= RackOrderIndexTopRight
}

// Building is the canonical domain shape for a building row.
type Building struct {
	ID                    int64
	OrgID                 int64
	SiteID                *int64 // nil = unassigned
	SiteLabel             string
	Name                  string
	Description           string
	PowerKw               float64
	OverheadKw            float64
	Aisles                int32
	PhysicalRackCount     int32
	RacksPerAisle         int32
	DefaultRackRows       int32
	DefaultRackColumns    int32
	DefaultRackOrderIndex RackOrderIndex
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// BuildingWithCounts pairs a Building with its rack_count for the
// list/delete-confirm flows.
type BuildingWithCounts struct {
	Building    Building
	RackCount   int64
	DeviceCount int64
	ListStats   *FleetListStats
}

// CreateParams is the input shape for the building create flow.
type CreateParams struct {
	OrgID                 int64
	SiteID                *int64 // nil = unassigned
	Name                  string
	Description           string
	PowerKw               float64
	OverheadKw            float64
	Aisles                int32
	PhysicalRackCount     int32
	RacksPerAisle         int32
	DefaultRackRows       int32
	DefaultRackColumns    int32
	DefaultRackOrderIndex RackOrderIndex
}

// UpdateParams is the input shape for building updates. SiteID is
// intentionally NOT updated here; that flow lives on
// SiteService.AssignBuildingsToSite, which carries the cross-collection
// invariant check.
type UpdateParams struct {
	OrgID                 int64
	ID                    int64
	Name                  string
	Description           string
	PowerKw               float64
	OverheadKw            float64
	Aisles                int32
	PhysicalRackCount     int32
	RacksPerAisle         int32
	DefaultRackRows       int32
	DefaultRackColumns    int32
	DefaultRackOrderIndex RackOrderIndex
}

// ListFilter selects which buildings to return. SiteIDs is an OR
// across sites; IncludeUnassigned additionally lets through buildings
// with site_id IS NULL. Both empty + IncludeUnassigned false means no
// filter (every live building in the org). Mirrors the miner-list
// filter shape from MinerListFilter (#197).
type ListFilter struct {
	OrgID             int64
	SiteIDs           []int64
	IncludeUnassigned bool
	IncludeStats      bool
}

// DeleteResult carries the cascade-unassign rack count for the
// activity-log row written on building delete.
type DeleteResult struct {
	UnassignedRackCount int64
}

// BuildingRack is the rack-in-building read shape used by
// ManageBuildingModal. Position fields are nil when the rack is a
// building member without a chosen grid cell.
type BuildingRack struct {
	RackID          int64
	RackLabel       string
	AisleIndex      *int32
	PositionInAisle *int32
}

// RackPlacementParam carries one rack's identity plus its optional
// grid placement inside the target building. Used by
// AssignRacksToBuilding for bulk updates.
type RackPlacementParam struct {
	RackID int64
	// AisleIndex / PositionInAisle are nil when the caller is not
	// positioning the rack at a specific cell. Must be paired (both
	// nil or both set); enforced at the service edge.
	AisleIndex      *int32
	PositionInAisle *int32
}

// AssignRacksToBuildingParams is the input shape for the bulk
// rack→building assignment flow. TargetBuildingID is nil when
// unassigning every rack in the batch from any building. Each entry
// in Racks may carry its own grid placement (or leave it nil to clear
// the cell).
type AssignRacksToBuildingParams struct {
	OrgID            int64
	Racks            []RackPlacementParam
	TargetBuildingID *int64
}

// AssignRacksToBuildingResult is the aggregate response carrying the
// total cascade impact count across every rack in the batch.
type AssignRacksToBuildingResult struct {
	SiteReassignedDeviceCount int64
}

// PerDeviceBuildingConflictReason enumerates why a device was rejected
// by AssignDevicesToBuilding.
type PerDeviceBuildingConflictReason int

const (
	// ReasonBuildingUnspecified — default zero value, should never
	// appear in emitted conflicts.
	ReasonBuildingUnspecified PerDeviceBuildingConflictReason = 0
	// ReasonBuildingDeviceNotFound — identifier doesn't match a live
	// device in the org.
	ReasonBuildingDeviceNotFound PerDeviceBuildingConflictReason = 1
	// ReasonBuildingDeviceInRackAtOtherBuilding — device is in a rack
	// whose building_id differs from the requested target.
	ReasonBuildingDeviceInRackAtOtherBuilding PerDeviceBuildingConflictReason = 2
	// ReasonBuildingDeviceInRackAtOtherSite — device is in a rack
	// whose site_id differs from the target building's site. Covers
	// the cross-site rack-without-building case the building-only
	// conflict check misses. Cleared by the same force flag as
	// IN_RACK_AT_OTHER_BUILDING.
	ReasonBuildingDeviceInRackAtOtherSite PerDeviceBuildingConflictReason = 3
)

// PerDeviceBuildingConflict explains why a device was rejected by
// AssignDevicesToBuilding. Mirrors the proto shape so the handler is a
// thin translator.
type PerDeviceBuildingConflict struct {
	DeviceIdentifier      string
	Reason                PerDeviceBuildingConflictReason
	ConflictingBuildingID int64
}

// AssignDevicesToBuildingParams is the input shape for the bulk assign
// flow. TargetBuildingID == nil means "Unassigned".
//
// When ForceClearConflictingRackMembership is true the service, inside
// the same transaction as the building write, drops any existing rack
// membership for devices whose rack is at a different building before
// applying the building update. Mirrors AssignDevicesToSite's force-
// clear semantic. When false (default), any device in a rack at a
// different building rejects the whole batch with conflicts.
type AssignDevicesToBuildingParams struct {
	OrgID                               int64
	TargetBuildingID                    *int64
	DeviceIdentifiers                   []string
	ForceClearConflictingRackMembership bool
}

// AssignDevicesToBuildingResult carries the rows touched + the count of
// devices whose site_id was cascaded to the target building's site.
type AssignDevicesToBuildingResult struct {
	ReassignedCount           int64
	SiteReassignedDeviceCount int64
}

// BuildingStats is the rollup returned by GetBuildingStats. Scope is
// every device whose rack lives in the building.
type BuildingStats struct {
	BuildingID                int64
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
	RackHealth                []BuildingRackHealth
	// DeviceIdentifiers is the set of devices the rollup was computed
	// over. Returned so FE telemetry consumers can scope themselves
	// without a separate ListMinerStateSnapshots pagination.
	DeviceIdentifiers []string
}

// FleetListStats is the lightweight rollup attached to list rows. It
// intentionally excludes BuildingStats detail fields such as RackHealth
// and DeviceIdentifiers.
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

// BuildingRackHealth is the per-rack rollup returned alongside
// BuildingStats. State counts use the same DeviceSetStats buckets; the
// FE owns the priority rule that collapses them into a visual state.
type BuildingRackHealth struct {
	RackID          int64
	RackLabel       string
	AisleIndex      *int32
	PositionInAisle *int32
	HashingCount    int32
	BrokenCount     int32
	OfflineCount    int32
	SleepingCount   int32
}
