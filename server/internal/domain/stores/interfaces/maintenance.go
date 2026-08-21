package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=maintenance.go -destination=mocks/mock_maintenance_store.go -package=mocks MaintenanceStore

// MaintenanceStore is the persistence boundary for the maintenance
// (repair ticketing) domain. All methods are org-scoped.
//
//nolint:interfacebloat // complete CRUD for tickets + comments + parts + stats
type MaintenanceStore interface {
	// ---------------------------------------------------------------
	// Ticket CRUD
	// ---------------------------------------------------------------

	// NextTicketNumber returns the next sequential ticket id for the
	// org. Must be called inside a transaction to prevent duplicates.
	NextTicketNumber(ctx context.Context, orgID int64) (int64, error)

	// CreateRepairTicket inserts a new repair_ticket row and returns
	// the created row.
	CreateRepairTicket(ctx context.Context, params models.CreateParams, ticketNumber string) (*models.RepairTicket, error)

	// GetRepairTicket returns the live ticket or NotFound.
	GetRepairTicket(ctx context.Context, orgID, id int64) (*models.RepairTicket, error)

	// ListRepairTickets returns tickets matching the supplied filters,
	// paginated by descending id cursor.
	ListRepairTickets(ctx context.Context, filter models.ListFilter) ([]models.RepairTicketSummary, error)

	// CountRepairTickets returns the total count matching the same
	// filters (for pagination).
	CountRepairTickets(ctx context.Context, filter models.ListFilter) (int32, error)

	// UpdateRepairTicket mutates the row's mutable fields. Returns the
	// updated row or NotFound.
	UpdateRepairTicket(ctx context.Context, params models.UpdateParams) (*models.RepairTicket, error)

	// SoftDeleteRepairTicket sets deleted_at. Returns rows affected
	// (0 = not found).
	SoftDeleteRepairTicket(ctx context.Context, orgID, id int64) (int64, error)

	// ---------------------------------------------------------------
	// Bulk operations
	// ---------------------------------------------------------------

	// BulkUpdateTicketStatus sets status on multiple tickets. Returns
	// rows affected.
	BulkUpdateTicketStatus(ctx context.Context, orgID int64, ticketIDs []int64, newStatus int16) (int64, error)

	// BulkAssignTickets sets assignee_user_id on multiple tickets.
	// Pass nil to unassign. Returns rows affected.
	BulkAssignTickets(ctx context.Context, orgID int64, ticketIDs []int64, assigneeUserID *int64) (int64, error)

	// BulkMarkUrgent sets urgent=true on multiple tickets. Returns
	// rows affected.
	BulkMarkUrgent(ctx context.Context, orgID int64, ticketIDs []int64) (int64, error)

	// BulkCloseTickets closes multiple tickets with the supplied
	// resolution and repair location. Returns rows affected.
	BulkCloseTickets(ctx context.Context, orgID int64, ticketIDs []int64, resolution int16, repairLocation int16, notes *string) (int64, error)

	// ---------------------------------------------------------------
	// Stats
	// ---------------------------------------------------------------

	// CountTicketsByStatus returns per-status counts for the queue
	// stats and kanban headers.
	CountTicketsByStatus(ctx context.Context, orgID int64) (map[int16]int32, error)

	// CountUnassignedTickets returns the count of non-completed
	// tickets with no assignee.
	CountUnassignedTickets(ctx context.Context, orgID int64) (int32, error)

	// CountUrgentTickets returns the count of non-completed urgent
	// tickets.
	CountUrgentTickets(ctx context.Context, orgID int64) (int32, error)

	// CountOverdueTickets returns the count of non-completed tickets
	// older than 72 hours.
	CountOverdueTickets(ctx context.Context, orgID int64) (int32, error)

	// AvgTicketAgeHours returns the average age in hours for
	// non-completed tickets.
	AvgTicketAgeHours(ctx context.Context, orgID int64) (float64, error)

	// ---------------------------------------------------------------
	// History
	// ---------------------------------------------------------------

	// ListCompletedTickets returns completed tickets with optional
	// component and assignee filters, paginated by descending
	// completed_at.
	ListCompletedTickets(ctx context.Context, filter models.CompletedFilter) ([]models.RepairTicketSummary, error)

	// ---------------------------------------------------------------
	// Miner / Rack scoped queries
	// ---------------------------------------------------------------

	// ListTicketsByMiner returns all live tickets for a specific
	// miner, open first then completed.
	ListTicketsByMiner(ctx context.Context, orgID int64, minerIdentifier string) ([]models.RepairTicket, error)

	// ListTicketsByRack returns non-completed tickets for miners in a
	// specific rack.
	ListTicketsByRack(ctx context.Context, orgID int64, rackID int64) ([]models.RepairTicket, error)

	// ---------------------------------------------------------------
	// Comments
	// ---------------------------------------------------------------

	// CreateTicketComment inserts a new comment and returns the
	// created row.
	CreateTicketComment(ctx context.Context, orgID, ticketID, userID int64, userName, text string) (*models.TicketComment, error)

	// ListTicketComments returns live comments for a ticket ordered
	// by created_at ascending.
	ListTicketComments(ctx context.Context, orgID, ticketID int64) ([]models.TicketComment, error)

	// SoftDeleteTicketComment sets deleted_at on a comment. Returns
	// rows affected (0 = not found).
	SoftDeleteTicketComment(ctx context.Context, orgID, id int64) (int64, error)

	// ---------------------------------------------------------------
	// Parts
	// ---------------------------------------------------------------

	// SetTicketParts replaces all parts for a ticket (delete + insert
	// in caller's transaction).
	SetTicketParts(ctx context.Context, orgID, ticketID int64) error

	// InsertTicketPart inserts a single part usage row.
	InsertTicketPart(ctx context.Context, orgID, ticketID int64, partName string, quantity int32) error

	// ListTicketParts returns all parts for a ticket.
	ListTicketParts(ctx context.Context, orgID, ticketID int64) ([]models.PartUsage, error)
}
