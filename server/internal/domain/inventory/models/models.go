// Package models holds the domain types for inventory parts.
package models

import "time"

// AdjustmentReason mirrors the proto enum and the SMALLINT stored
// alongside inventory adjustments. Re-declared as a typed constant
// set so the domain layer is independent of the proto package.
type AdjustmentReason int16

const (
	AdjustmentReasonUnspecified       AdjustmentReason = 0
	AdjustmentReasonReceivedShipment  AdjustmentReason = 1
	AdjustmentReasonCycleCount        AdjustmentReason = 2
	AdjustmentReasonDamagedScrapped   AdjustmentReason = 3
	AdjustmentReasonReturnedFromRepair AdjustmentReason = 4
	AdjustmentReasonOther             AdjustmentReason = 5
)

// Valid reports whether the value matches one of the defined enum
// members. Used to reject malformed proto inputs at the service edge.
func (r AdjustmentReason) Valid() bool {
	return r >= AdjustmentReasonUnspecified && r <= AdjustmentReasonOther
}

// InventoryPart is the canonical domain shape for an inventory_part row.
type InventoryPart struct {
	ID           int64
	OrgID        int64
	Name         string
	Type         string
	Manufacturer *string
	PartNumber   *string
	SiteID       *int64 // nil = not site-scoped
	SiteName     string
	OnHand       int32
	Allocated    int32
	ReorderPoint int32
	BinLocation  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// InventoryInsights is the aggregate stats shape for the inventory
// insights row displayed above the parts table.
type InventoryInsights struct {
	TotalOnHand    int32
	TotalAllocated int32
	LowStockCount  int32
	SitesCount     int32
}

// CreateParams is the input shape for the inventory part create flow.
type CreateParams struct {
	OrgID        int64
	Name         string
	Type         string
	Manufacturer *string
	PartNumber   *string
	SiteID       *int64
	OnHand       int32
	ReorderPoint int32
	BinLocation  *string
}

// UpdateParams is the input shape for inventory part updates. Only
// non-nil fields are written; the store COALESCE pattern preserves
// existing values for omitted fields.
type UpdateParams struct {
	ID           int64
	OrgID        int64
	OnHand       *int32
	ReorderPoint *int32
	BinLocation  *string
	Reason       AdjustmentReason
	Notes        *string
}

// ListFilter selects which inventory parts to return.
type ListFilter struct {
	OrgID        int64
	SiteIDs      []int64
	Types        []string
	LowStockOnly bool
	CursorID     *int64
	Limit        int32
}

// CsvPreviewRow is a single parsed row from a CSV import before
// the user confirms. The service validates each row and attaches
// any per-row error so the UI can display a preview table with
// inline warnings.
type CsvPreviewRow struct {
	RowNumber    int
	Name         string
	Type         string
	Manufacturer string
	PartNumber   string
	SiteName     string
	OnHand       int32
	ReorderPoint int32
	BinLocation  string
	Error        string // empty when valid
}
