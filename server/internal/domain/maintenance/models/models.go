// Package models holds the domain types for the maintenance (repair ticketing) domain.
package models

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

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
	TicketResolutionUnspecified    TicketResolution = 0
	TicketResolutionRepaired       TicketResolution = 1
	TicketResolutionReplaced       TicketResolution = 2
	TicketResolutionDeferred       TicketResolution = 3
	TicketResolutionUnrepairable   TicketResolution = 4
	TicketResolutionNoActionNeeded TicketResolution = 5
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
	WarrantyStatusUnspecified   WarrantyStatus = 0
	WarrantyStatusInWarranty    WarrantyStatus = 1
	WarrantyStatusOutOfWarranty WarrantyStatus = 2
	WarrantyStatusExpiringSoon  WarrantyStatus = 3
)

// TicketSortField identifies the stable value used to order ticket pages.
type TicketSortField int16

const (
	TicketSortFieldUnspecified TicketSortField = 0
	TicketSortFieldComponent   TicketSortField = 1
	TicketSortFieldAsset       TicketSortField = 2
	TicketSortFieldLocation    TicketSortField = 3
	TicketSortFieldStatus      TicketSortField = 4
	TicketSortFieldCreatedAt   TicketSortField = 5
)

// SortDirection controls whether the selected sort value increases or decreases.
type SortDirection int16

const (
	SortDirectionUnspecified SortDirection = 0
	SortDirectionAscending   SortDirection = 1
	SortDirectionDescending  SortDirection = 2
)

// TicketCursor is serialized as opaque base64url JSON at the transport edge.
// Value comes directly from the SQL sort expression; ID is the deterministic
// tie-breaker when multiple rows share that value.
type TicketCursor struct {
	SortField     TicketSortField `json:"sort_field"`
	SortDirection SortDirection   `json:"sort_direction"`
	Value         string          `json:"value"`
	ID            int64           `json:"id"`
}

func (c TicketCursor) Encode() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode ticket cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeTicketCursor(token string) (TicketCursor, error) {
	if len(token) == 0 || len(token) > 2048 {
		return TicketCursor{}, fmt.Errorf("invalid ticket cursor length")
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return TicketCursor{}, fmt.Errorf("decode ticket cursor: %w", err)
	}
	var cursor TicketCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return TicketCursor{}, fmt.Errorf("decode ticket cursor JSON: %w", err)
	}
	if cursor.ID <= 0 || cursor.Value == "" {
		return TicketCursor{}, fmt.Errorf("invalid ticket cursor")
	}
	return cursor, nil
}

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
	AlertID         *string
	AssigneeUserID  *int64
	AssigneeName    string
	WarrantyStatus  WarrantyStatus
	Resolution      TicketResolution
	RepairLocation  RepairLocation
	Notes           *string
	DailyImpactUsd  float64
	RMAVendor       *string
	RMATracking     *string
	RMAEta          *time.Time
	SiteID          *int64
	SiteName        string
	BuildingID      *int64
	BuildingName    string
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
	Cursor       TicketCursor
}

// TicketComment is the domain shape for a repair_ticket_comment row.
type TicketComment struct {
	ID               int64
	OrgID            int64
	TicketID         int64
	UserID           int64
	UserName         string
	Text             string
	AuthoredByCaller bool
	CreatedAt        time.Time
	DeletedAt        *time.Time
}

// PartUsage represents a part reserved for or consumed by a repair.
type PartUsage struct {
	InventoryPartID int64
	PartName        string
	Quantity        int32
	ConsumedAt      *time.Time
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
	AlertID         *string
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
	// PartsSelection is nil when the caller omitted the field and points to an
	// empty slice when the caller explicitly removes every active reservation.
	PartsSelection *[]PartUsage
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
	OverdueOnly      bool
	SearchQuery      string
	SortField        TicketSortField
	SortDirection    SortDirection
	Cursor           *TicketCursor
	Limit            int32
}

// CompletedFilter selects which completed tickets to return for the
// history tab.
type CompletedFilter struct {
	OrgID          int64
	Component      *string
	AssigneeUserID *int64
	SortField      TicketSortField
	SortDirection  SortDirection
	Cursor         *TicketCursor
	Limit          int32
}

// Assignee is an active user with a live organization membership.
type Assignee struct {
	UserID   int64
	Username string
	RoleName string
}

// AssetContext is the authoritative location snapshot for a live miner.
type AssetContext struct {
	MinerIdentifier string
	SiteID          *int64
	SiteName        string
	BuildingID      *int64
	BuildingName    string
	Zone            *string
	RackID          *int64
	RackLabel       *string
	GroupLabel      *string
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
