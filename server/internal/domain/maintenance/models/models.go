// Package models holds the domain types for the maintenance (repair ticketing) domain.
package models

import "time"

// TicketCategory mirrors the proto enum and the SMALLINT stored in
// repair_ticket.category. Typed constant set so the domain layer is
// independent of the proto package.
type TicketCategory int16

const (
	TicketCategoryUnspecified    TicketCategory = 0
	TicketCategoryMiner          TicketCategory = 1
	TicketCategoryInfrastructure TicketCategory = 2
)

// TicketStatus mirrors repair_ticket.status.
type TicketStatus int16

const (
	TicketStatusUnspecified  TicketStatus = 0
	TicketStatusOpen         TicketStatus = 1
	TicketStatusInProgress   TicketStatus = 2
	TicketStatusOnHold       TicketStatus = 3
	TicketStatusSentToVendor TicketStatus = 4
	TicketStatusCompleted    TicketStatus = 5
)

// TicketResolution mirrors repair_ticket.resolution.
type TicketResolution int16

const (
	TicketResolutionUnspecified  TicketResolution = 0
	TicketResolutionRepaired     TicketResolution = 1
	TicketResolutionReplaced     TicketResolution = 2
	TicketResolutionDeferred     TicketResolution = 3
	TicketResolutionUnrepairable TicketResolution = 4
)

// RepairLocation mirrors repair_ticket.repair_location.
type RepairLocation int16

const (
	RepairLocationUnspecified RepairLocation = 0
	RepairLocationOnRack      RepairLocation = 1
	RepairLocationRepairBench RepairLocation = 2
)

// WarrantyStatus mirrors repair_ticket.warranty_status.
type WarrantyStatus int16

const (
	WarrantyStatusUnspecified  WarrantyStatus = 0
	WarrantyStatusInWarranty   WarrantyStatus = 1
	WarrantyStatusOutOfWarranty WarrantyStatus = 2
	WarrantyStatusExpiringSoon WarrantyStatus = 3
)

// RepairTicket is the canonical domain shape for a repair_ticket row.
type RepairTicket struct {
	ID              int64
	OrgID           int64
	TicketNumber    string
	Category        TicketCategory
	Status          TicketStatus
	Urgent          bool
	Component       string
	Diagnosis       *string
	MinerIdentifier *string
	AlertID         *int64
	AssigneeUserID  *int64
	WarrantyStatus  WarrantyStatus
	Resolution      TicketResolution
	RepairLocation  RepairLocation
	Notes           *string
	DailyImpactUsd  float64
	RMAVendor       *string
	RMATracking     *string
	RMAEta          *time.Time
	SiteID          *int64
	BuildingID      *int64
	Zone            *string
	RackID          *int64
	RackLabel       *string
	GroupLabel      *string
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// RepairTicketSummary pairs a RepairTicket with rolled-up comment and
// parts counts for the list view.
type RepairTicketSummary struct {
	RepairTicket
	CommentCount int32
	PartsCount   int32
}

// TicketComment is the domain shape for a repair_ticket_comment row.
type TicketComment struct {
	ID        int64
	OrgID     int64
	TicketID  int64
	UserID    int64
	UserName  string
	Text      string
	CreatedAt time.Time
	DeletedAt *time.Time
}

// PartUsage represents a single part consumed during a repair.
type PartUsage struct {
	PartName string
	Quantity int32
}

// TicketDetail is the full read model returned by GetRepairTicket,
// combining the ticket row with its comments and parts.
type TicketDetail struct {
	Ticket    RepairTicket
	Comments  []TicketComment
	PartsUsed []PartUsage
}

// CreateParams is the input shape for creating a new repair ticket.
type CreateParams struct {
	OrgID           int64
	Category        TicketCategory
	Urgent          bool
	Component       string
	Diagnosis       *string
	MinerIdentifier *string
	AlertID         *int64
	AssigneeUserID  *int64
	WarrantyStatus  WarrantyStatus
	DailyImpactUsd  float64
	SiteID          *int64
	BuildingID      *int64
	Zone            *string
	RackID          *int64
	RackLabel       *string
	GroupLabel      *string
	Notes           *string
}

// UpdateParams is the input shape for updating a repair ticket. Pointer
// fields are optional; when nil the column is left unchanged. The
// ClearAssignee flag unsets assignee_user_id even if AssigneeUserID is
// nil (CASE branch in the SQL UPDATE).
type UpdateParams struct {
	OrgID          int64
	ID             int64
	Status         *TicketStatus
	Urgent         *bool
	AssigneeUserID *int64
	ClearAssignee  bool
	Component      *string
	Diagnosis      *string
	WarrantyStatus *WarrantyStatus
	Resolution     *TicketResolution
	RepairLocation *RepairLocation
	Notes          *string
	RMAVendor      *string
	RMATracking    *string
	RMAEta         *time.Time
}

// BulkCloseParams is the input shape for closing multiple tickets at
// once with a shared resolution, repair location, and optional notes.
type BulkCloseParams struct {
	OrgID          int64
	TicketIDs      []int64
	Resolution     TicketResolution
	RepairLocation RepairLocation
	Notes          *string
	PartsUsed      []PartUsage
}

// ListFilter selects which tickets to return. All slice/pointer fields
// are optional; when zero-valued that dimension is not filtered.
type ListFilter struct {
	OrgID            int64
	Statuses         []int16
	Categories       []int16
	SiteIDs          []int64
	BuildingIDs      []int64
	RackIDs          []int64
	GroupLabels      []string
	AssigneeUserID   *int64
	UrgentOnly       bool
	ExcludeCompleted bool
	SearchQuery      string
	CursorID         *int64
	Limit            int32
}

// CompletedFilter selects which completed tickets to return for the
// history tab.
type CompletedFilter struct {
	OrgID          int64
	Component      *string
	AssigneeUserID *int64
	CursorID       *int64
	Limit          int32
}

// TicketStats is the aggregate snapshot returned by GetTicketStats.
type TicketStats struct {
	// CountByStatus maps TicketStatus → count.
	CountByStatus map[TicketStatus]int32
	Unassigned    int32
	Urgent        int32
	Overdue       int32
	AvgAgeHours   float64
}
