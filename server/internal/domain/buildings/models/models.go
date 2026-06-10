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
	Building  Building
	RackCount int64
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

// ListFilter selects which buildings to return. SiteID is nil when
// the caller is not filtering by site; UnassignedOnly is true to
// request the "site_id IS NULL" bucket. SiteID != nil and
// UnassignedOnly are mutually exclusive (enforced by the proto oneof).
type ListFilter struct {
	OrgID          int64
	SiteID         *int64
	UnassignedOnly bool
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

// AssignRackToBuildingParams is the input shape for assigning (or
// unassigning) a rack to a building, optionally with a grid cell.
type AssignRackToBuildingParams struct {
	OrgID  int64
	RackID int64
	// BuildingID is nil when unassigning the rack from any building.
	BuildingID *int64
	// AisleIndex / PositionInAisle are nil when the caller is not
	// positioning the rack at a specific cell. Must be paired (both
	// nil or both set); enforced at the service edge.
	AisleIndex      *int32
	PositionInAisle *int32
}

// AssignRackToBuildingResult is the response shape carrying the
// cascade impact count for the activity log + UI confirmation.
type AssignRackToBuildingResult struct {
	SiteReassignedDeviceCount int64
}

// BuildingStats is the rollup returned by GetBuildingStats. Scope is
// every device whose rack lives in the building.
type BuildingStats struct {
	BuildingID               int64
	RackCount                int32
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
	RackHealth               []BuildingRackHealth
	// DeviceIdentifiers is the set of devices the rollup was computed
	// over. Returned so FE telemetry consumers can scope themselves
	// without a separate ListMinerStateSnapshots pagination.
	DeviceIdentifiers []string
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
