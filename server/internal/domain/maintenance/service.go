// Package maintenance is the domain layer for the MaintenanceService RPC
// surface. Repair ticket CRUD, bulk operations, comments, parts, and
// aggregate stats.
package maintenance

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for maintenance activity logs.
const (
	eventTicketCreated  = "maintenance.ticket_created"
	eventTicketUpdated  = "maintenance.ticket_updated"
	eventTicketDeleted  = "maintenance.ticket_deleted"
	eventTicketBulk     = "maintenance.ticket_bulk_update"
	eventCommentCreated = "maintenance.comment_created"
	eventCommentDeleted = "maintenance.comment_deleted"
)

// Pagination defaults / caps.
const (
	DefaultListLimit = int32(50)
	MaxListLimit     = int32(200)
)

// Service is the domain entry point for repair ticket operations.
type Service struct {
	store       interfaces.MaintenanceStore
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService wires a MaintenanceStore, Transactor (for multi-step
// mutations like create + number generation, bulk close + parts), and
// the activity Service used for fire-and-forget audit logs. activitySvc
// may be nil in tests or environments where activity logging is disabled.
func NewService(
	store interfaces.MaintenanceStore,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:       store,
		transactor:  transactor,
		activitySvc: activitySvc,
	}
}

// clampLimit applies default and max clamping to the pagination limit.
func clampLimit(limit int32) int32 {
	if limit <= 0 {
		return DefaultListLimit
	}
	if limit > MaxListLimit {
		return MaxListLimit
	}
	return limit
}

// ---------------------------------------------------------------
// Ticket CRUD
// ---------------------------------------------------------------

// CreateRepairTicket generates a TK-XXXX ticket number and inserts the
// ticket row inside a single transaction.
func (s *Service) CreateRepairTicket(ctx context.Context, params models.CreateParams) (*models.RepairTicket, error) {
	if params.Component == "" {
		return nil, fleeterror.NewInvalidArgumentError("component is required")
	}

	var ticket *models.RepairTicket
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		nextID, err := s.store.NextTicketNumber(txCtx, params.OrgID)
		if err != nil {
			return err
		}
		ticketNumber := fmt.Sprintf("TK-%04d", nextID)

		created, err := s.store.CreateRepairTicket(txCtx, params, ticketNumber)
		if err != nil {
			return err
		}
		ticket = created
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER tx commits.
	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketCreated,
			OrganizationID: &orgID,
			SiteID:         ticket.SiteID,
			Description: fmt.Sprintf(
				"Created repair ticket %s (id=%d, component=%s)",
				ticket.TicketNumber, ticket.ID, ticket.Component,
			),
			Metadata: map[string]any{
				"ticket_id":     ticket.ID,
				"ticket_number": ticket.TicketNumber,
				"category":      int16(ticket.Category),
				"component":     ticket.Component,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return ticket, nil
}

// GetRepairTicket returns the full ticket detail including comments and
// parts.
func (s *Service) GetRepairTicket(ctx context.Context, orgID, id int64) (*models.TicketDetail, error) {
	ticket, err := s.store.GetRepairTicket(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	comments, err := s.store.ListTicketComments(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	parts, err := s.store.ListTicketParts(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	return &models.TicketDetail{
		Ticket:    *ticket,
		Comments:  comments,
		PartsUsed: parts,
	}, nil
}

// ListRepairTickets returns tickets matching the filter with pagination.
func (s *Service) ListRepairTickets(ctx context.Context, filter models.ListFilter) ([]models.RepairTicketSummary, int32, error) {
	filter.Limit = clampLimit(filter.Limit)

	tickets, err := s.store.ListRepairTickets(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	totalCount, err := s.store.CountRepairTickets(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return tickets, totalCount, nil
}

// UpdateRepairTicket mutates the ticket's mutable fields. Validates
// status transitions when a status change is requested.
func (s *Service) UpdateRepairTicket(ctx context.Context, params models.UpdateParams) (*models.RepairTicket, error) {
	if params.Status != nil {
		if err := validateStatusTransition(*params.Status); err != nil {
			return nil, err
		}
	}

	ticket, err := s.store.UpdateRepairTicket(ctx, params)
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER the write.
	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketUpdated,
			OrganizationID: &orgID,
			SiteID:         ticket.SiteID,
			Description: fmt.Sprintf(
				"Updated repair ticket %s (id=%d)",
				ticket.TicketNumber, ticket.ID,
			),
			Metadata: map[string]any{
				"ticket_id":     ticket.ID,
				"ticket_number": ticket.TicketNumber,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return ticket, nil
}

// DeleteRepairTicket soft-deletes the ticket.
func (s *Service) DeleteRepairTicket(ctx context.Context, orgID, id int64) error {
	rowsAffected, err := s.store.SoftDeleteRepairTicket(ctx, orgID, id)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fleeterror.NewNotFoundErrorf("ticket %d not found", id)
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketDeleted,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Deleted repair ticket %d", id),
			Metadata: map[string]any{
				"ticket_id": id,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return nil
}

// ---------------------------------------------------------------
// Bulk operations
// ---------------------------------------------------------------

// BulkUpdateStatus sets the status on multiple tickets. Returns
// affected row count.
func (s *Service) BulkUpdateStatus(ctx context.Context, orgID int64, ticketIDs []int64, newStatus models.TicketStatus) (int64, error) {
	if len(ticketIDs) == 0 {
		return 0, fleeterror.NewInvalidArgumentError("ticket_ids must not be empty")
	}
	if err := validateStatusTransition(newStatus); err != nil {
		return 0, err
	}

	affected, err := s.store.BulkUpdateTicketStatus(ctx, orgID, ticketIDs, int16(newStatus))
	if err != nil {
		return 0, err
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketBulk,
			OrganizationID: &orgID,
			Description: fmt.Sprintf(
				"Bulk status update: %d ticket(s) → status %d",
				affected, int16(newStatus),
			),
			Metadata: map[string]any{
				"ticket_ids": ticketIDs,
				"new_status": int16(newStatus),
				"affected":   affected,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// BulkAssign sets the assignee on multiple tickets. Pass nil
// assigneeUserID to unassign. Returns affected row count.
func (s *Service) BulkAssign(ctx context.Context, orgID int64, ticketIDs []int64, assigneeUserID *int64) (int64, error) {
	if len(ticketIDs) == 0 {
		return 0, fleeterror.NewInvalidArgumentError("ticket_ids must not be empty")
	}

	affected, err := s.store.BulkAssignTickets(ctx, orgID, ticketIDs, assigneeUserID)
	if err != nil {
		return 0, err
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketBulk,
			OrganizationID: &orgID,
			Description: fmt.Sprintf(
				"Bulk assign: %d ticket(s) → user %v",
				affected, derefInt64(assigneeUserID),
			),
			Metadata: map[string]any{
				"ticket_ids":       ticketIDs,
				"assignee_user_id": assigneeUserID,
				"affected":         affected,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// BulkMarkUrgent flags multiple tickets as urgent. Returns affected row
// count.
func (s *Service) BulkMarkUrgent(ctx context.Context, orgID int64, ticketIDs []int64) (int64, error) {
	if len(ticketIDs) == 0 {
		return 0, fleeterror.NewInvalidArgumentError("ticket_ids must not be empty")
	}

	affected, err := s.store.BulkMarkUrgent(ctx, orgID, ticketIDs)
	if err != nil {
		return 0, err
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketBulk,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Bulk mark urgent: %d ticket(s)", affected),
			Metadata: map[string]any{
				"ticket_ids": ticketIDs,
				"affected":   affected,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// BulkClose closes multiple tickets with a shared resolution and repair
// location, optionally recording parts used on each. Runs in a
// transaction when parts are supplied.
func (s *Service) BulkClose(ctx context.Context, params models.BulkCloseParams) (int64, error) {
	if len(params.TicketIDs) == 0 {
		return 0, fleeterror.NewInvalidArgumentError("ticket_ids must not be empty")
	}

	var affected int64
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		rows, err := s.store.BulkCloseTickets(
			txCtx,
			params.OrgID,
			params.TicketIDs,
			int16(params.Resolution),
			int16(params.RepairLocation),
			params.Notes,
		)
		if err != nil {
			return err
		}
		affected = rows

		// Record parts on every closed ticket.
		if len(params.PartsUsed) > 0 {
			for _, ticketID := range params.TicketIDs {
				// Clear existing parts first.
				if err := s.store.SetTicketParts(txCtx, params.OrgID, ticketID); err != nil {
					return err
				}
				for _, part := range params.PartsUsed {
					if err := s.store.InsertTicketPart(txCtx, params.OrgID, ticketID, part.PartName, part.Quantity); err != nil {
						return err
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketBulk,
			OrganizationID: &orgID,
			Description: fmt.Sprintf(
				"Bulk close: %d ticket(s), resolution=%d",
				affected, int16(params.Resolution),
			),
			Metadata: map[string]any{
				"ticket_ids": params.TicketIDs,
				"resolution": int16(params.Resolution),
				"affected":   affected,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// ---------------------------------------------------------------
// Stats
// ---------------------------------------------------------------

// GetTicketStats aggregates multiple count queries into a single
// TicketStats snapshot.
func (s *Service) GetTicketStats(ctx context.Context, orgID int64) (*models.TicketStats, error) {
	countByStatus, err := s.store.CountTicketsByStatus(ctx, orgID)
	if err != nil {
		return nil, err
	}

	unassigned, err := s.store.CountUnassignedTickets(ctx, orgID)
	if err != nil {
		return nil, err
	}

	urgent, err := s.store.CountUrgentTickets(ctx, orgID)
	if err != nil {
		return nil, err
	}

	overdue, err := s.store.CountOverdueTickets(ctx, orgID)
	if err != nil {
		return nil, err
	}

	avgAge, err := s.store.AvgTicketAgeHours(ctx, orgID)
	if err != nil {
		return nil, err
	}

	// Convert int16 keys from store to typed TicketStatus keys.
	typedCounts := make(map[models.TicketStatus]int32, len(countByStatus))
	for status, count := range countByStatus {
		typedCounts[models.TicketStatus(status)] = count
	}

	return &models.TicketStats{
		CountByStatus: typedCounts,
		Unassigned:    unassigned,
		Urgent:        urgent,
		Overdue:       overdue,
		AvgAgeHours:   avgAge,
	}, nil
}

// ---------------------------------------------------------------
// Comments
// ---------------------------------------------------------------

// CreateComment adds a comment to a ticket. Validates the ticket exists
// in the org before inserting.
func (s *Service) CreateComment(ctx context.Context, orgID, ticketID, userID int64, userName, text string) (*models.TicketComment, error) {
	if text == "" {
		return nil, fleeterror.NewInvalidArgumentError("comment text is required")
	}

	// Verify ticket exists in org.
	if _, err := s.store.GetRepairTicket(ctx, orgID, ticketID); err != nil {
		return nil, err
	}

	comment, err := s.store.CreateTicketComment(ctx, orgID, ticketID, userID, userName, text)
	if err != nil {
		return nil, err
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventCommentCreated,
			OrganizationID: &orgID,
			Description: fmt.Sprintf(
				"Added comment on ticket %d by %s",
				ticketID, userName,
			),
			Metadata: map[string]any{
				"ticket_id":  ticketID,
				"comment_id": comment.ID,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return comment, nil
}

// DeleteComment soft-deletes a comment.
func (s *Service) DeleteComment(ctx context.Context, orgID, commentID int64) error {
	rowsAffected, err := s.store.SoftDeleteTicketComment(ctx, orgID, commentID)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fleeterror.NewNotFoundErrorf("comment %d not found", commentID)
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventCommentDeleted,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Deleted comment %d", commentID),
			Metadata: map[string]any{
				"comment_id": commentID,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return nil
}

// ---------------------------------------------------------------
// History
// ---------------------------------------------------------------

// ListCompletedTickets returns completed tickets for the history tab.
func (s *Service) ListCompletedTickets(ctx context.Context, filter models.CompletedFilter) ([]models.RepairTicketSummary, error) {
	filter.Limit = clampLimit(filter.Limit)
	return s.store.ListCompletedTickets(ctx, filter)
}

// ---------------------------------------------------------------
// Miner / Rack scoped
// ---------------------------------------------------------------

// ListTicketsByMiner returns tickets associated with a specific miner.
func (s *Service) ListTicketsByMiner(ctx context.Context, orgID int64, minerIdentifier string) ([]models.RepairTicket, error) {
	if minerIdentifier == "" {
		return nil, fleeterror.NewInvalidArgumentError("miner_identifier is required")
	}
	return s.store.ListTicketsByMiner(ctx, orgID, minerIdentifier)
}

// ListTicketsByRack returns non-completed tickets for a specific rack.
func (s *Service) ListTicketsByRack(ctx context.Context, orgID, rackID int64) ([]models.RepairTicket, error) {
	return s.store.ListTicketsByRack(ctx, orgID, rackID)
}

// ---------------------------------------------------------------
// Parts (ticket-scoped, called from handler after update)
// ---------------------------------------------------------------

// SetTicketParts replaces all parts for a ticket with the supplied list.
func (s *Service) SetTicketParts(ctx context.Context, orgID, ticketID int64, parts []models.PartUsage) error {
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.store.SetTicketParts(txCtx, orgID, ticketID); err != nil {
			return err
		}
		for _, part := range parts {
			if err := s.store.InsertTicketPart(txCtx, orgID, ticketID, part.PartName, part.Quantity); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------

// validateStatusTransition rejects invalid status values. The full
// state-machine enforcement can be expanded here; for now we only
// reject the Unspecified sentinel.
func validateStatusTransition(status models.TicketStatus) error {
	if status < models.TicketStatusOpen || status > models.TicketStatusCompleted {
		return fleeterror.NewInvalidArgumentErrorf("invalid ticket status: %d", int16(status))
	}
	return nil
}

func derefInt64(v *int64) any {
	if v == nil {
		return "(none)"
	}
	return *v
}
