// Package maintenance is the domain layer for the MaintenanceService RPC
// surface. Repair ticket CRUD, bulk operations, comments, parts, and
// aggregate stats.
package maintenance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

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

type commentCreateResult struct {
	comment *models.TicketComment
	siteID  *int64
}

type commentDeleteResult struct {
	siteID *int64
}

// Service is the domain entry point for repair ticket operations.
type Service struct {
	store       interfaces.MaintenanceStore
	refs        interfaces.MaintenanceReferenceStore
	inventory   interfaces.InventoryStore
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService wires a MaintenanceStore, Transactor (for multi-step
// mutations like create + number generation, bulk close + parts), and
// the activity Service used for fire-and-forget audit logs. activitySvc
// may be nil in tests or environments where activity logging is disabled.
func NewService(
	store interfaces.MaintenanceStore,
	refs interfaces.MaintenanceReferenceStore,
	inventory interfaces.InventoryStore,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:       store,
		refs:        refs,
		inventory:   inventory,
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
	params.Component = strings.TrimSpace(params.Component)
	if params.Component == "" {
		return nil, fleeterror.NewInvalidArgumentError("component is required")
	}
	if params.Category != models.TicketCategoryMiner && params.Category != models.TicketCategoryInfrastructure {
		return nil, fleeterror.NewInvalidArgumentError("invalid ticket category")
	}

	var ticket *models.RepairTicket
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if params.AssigneeUserID != nil {
			if _, err := s.refs.ResolveAssignee(txCtx, params.OrgID, *params.AssigneeUserID); err != nil {
				return err
			}
		}
		switch params.Category {
		case models.TicketCategoryMiner:
			if params.MinerIdentifier == nil || strings.TrimSpace(*params.MinerIdentifier) == "" {
				return fleeterror.NewInvalidArgumentError("miner_identifier is required for miner tickets")
			}
			identifier := strings.TrimSpace(*params.MinerIdentifier)
			asset, err := s.refs.ResolveMinerContext(txCtx, params.OrgID, identifier)
			if err != nil {
				return err
			}
			params.MinerIdentifier = &identifier
			params.SiteID, params.BuildingID = asset.SiteID, asset.BuildingID
			params.Zone, params.RackID = asset.Zone, asset.RackID
			params.RackLabel, params.GroupLabel = asset.RackLabel, asset.GroupLabel
		case models.TicketCategoryInfrastructure:
			if params.SiteID == nil {
				return fleeterror.NewInvalidArgumentError("site_id is required for infrastructure tickets")
			}
			location, err := s.refs.ResolveLocationContext(txCtx, params.OrgID, params.SiteID, params.BuildingID)
			if err != nil {
				return err
			}
			params.SiteID, params.BuildingID = location.SiteID, location.BuildingID
			params.MinerIdentifier, params.RackID, params.RackLabel, params.GroupLabel = nil, nil, nil, nil
		case models.TicketCategoryUnspecified:
			return fleeterror.NewInvalidArgumentError("invalid ticket category")
		}

		if params.SiteID != nil {
			if err := s.refs.LockSiteForTicket(txCtx, params.OrgID, *params.SiteID); err != nil {
				return err
			}
		}
		if params.BuildingID != nil {
			if err := s.refs.LockBuildingForTicket(txCtx, params.OrgID, *params.BuildingID); err != nil {
				return err
			}
		}

		nextID, err := s.store.NextTicketNumber(txCtx, params.OrgID)
		if err != nil {
			return err
		}
		created, err := s.store.CreateRepairTicket(txCtx, params, fmt.Sprintf("TK-%04d", nextID))
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
func (s *Service) ListRepairTickets(ctx context.Context, filter models.ListFilter) ([]models.RepairTicketSummary, int32, bool, error) {
	pageSize := clampLimit(filter.Limit)
	filter.Limit = pageSize
	countFilter := filter
	filter.Limit++

	tickets, err := s.store.ListRepairTickets(ctx, filter)
	if err != nil {
		return nil, 0, false, err
	}

	totalCount, err := s.store.CountRepairTickets(ctx, countFilter)
	if err != nil {
		return nil, 0, false, err
	}

	hasNext := len(tickets) > int(pageSize)
	if hasNext {
		tickets = tickets[:pageSize]
	}
	return tickets, totalCount, hasNext, nil
}

// UpdateRepairTicket validates lifecycle and reference policy and mutates the
// ticket, reservations, and stock in one transaction.
func (s *Service) UpdateRepairTicket(ctx context.Context, params models.UpdateParams) (*models.RepairTicket, error) {
	if params.Component != nil {
		trimmed := strings.TrimSpace(*params.Component)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("component must not be empty")
		}
		params.Component = &trimmed
	}
	if params.ClearAssignee && params.AssigneeUserID != nil {
		return nil, fleeterror.NewInvalidArgumentError("assignee_user_id and clear_assignee cannot both be set")
	}
	if params.ClearRMAEta && params.RMAEta != nil {
		return nil, fleeterror.NewInvalidArgumentError("rma_eta and clear_rma_eta cannot both be set")
	}
	if params.Status != nil {
		if err := validateStatusTransition(*params.Status); err != nil {
			return nil, err
		}
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		current, err := s.store.GetRepairTicketForUpdate(txCtx, params.OrgID, params.ID)
		if err != nil {
			return nil, err
		}
		targetStatus := current.Status
		if params.Status != nil {
			targetStatus = *params.Status
		}
		if current.Status == models.TicketStatusCompleted {
			if targetStatus != models.TicketStatusCompleted {
				return nil, fleeterror.NewFailedPreconditionError("completed tickets are terminal")
			}
			return current, nil
		}
		if !statusTransitionAllowed(current.Status, targetStatus) {
			return nil, fleeterror.NewFailedPreconditionErrorf("ticket status transition %d to %d is not allowed", current.Status, targetStatus)
		}
		if params.Resolution != nil && (*params.Resolution < models.TicketResolutionRepaired || *params.Resolution > models.TicketResolutionNoActionNeeded) {
			return nil, fleeterror.NewInvalidArgumentError("invalid ticket resolution")
		}
		if params.RepairLocation != nil && (*params.RepairLocation < models.RepairLocationUnspecified || *params.RepairLocation > models.RepairLocationRepairBench) {
			return nil, fleeterror.NewInvalidArgumentError("invalid repair location")
		}
		if params.AssigneeUserID != nil {
			if _, err := s.refs.ResolveAssignee(txCtx, params.OrgID, *params.AssigneeUserID); err != nil {
				return nil, err
			}
		}
		if targetStatus == models.TicketStatusSentToVendor {
			vendor := current.RMAVendor
			if params.RMAVendor != nil {
				vendor = params.RMAVendor
			}
			if vendor == nil || strings.TrimSpace(*vendor) == "" {
				return nil, fleeterror.NewInvalidArgumentError("rma_vendor is required when sending a ticket to a vendor")
			}
		}
		if targetStatus == models.TicketStatusCompleted {
			resolution := current.Resolution
			if params.Resolution != nil {
				resolution = *params.Resolution
			}
			location := current.RepairLocation
			if params.RepairLocation != nil {
				location = *params.RepairLocation
			}
			if err := validateCompletion(current.Category, resolution, location); err != nil {
				return nil, err
			}
		}

		var parts []models.PartUsage
		if params.PartsSelection != nil || targetStatus == models.TicketStatusCompleted {
			parts, err = s.reconcileParts(txCtx, current, params.PartsSelection)
			if err != nil {
				return nil, err
			}
		}
		if targetStatus == models.TicketStatusCompleted {
			for _, part := range parts {
				if err := s.inventory.ConsumeReserved(txCtx, params.OrgID, part.InventoryPartID, part.Quantity); err != nil {
					return nil, err
				}
			}
			if err := s.store.MarkTicketPartsConsumed(txCtx, params.OrgID, params.ID); err != nil {
				return nil, err
			}
		}
		return s.store.UpdateRepairTicket(txCtx, params)
	})
	if err != nil {
		return nil, err
	}
	ticket, ok := result.(*models.RepairTicket)
	if !ok {
		return nil, fleeterror.NewInternalError("maintenance transaction returned an unexpected result")
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

// DeleteRepairTicket releases active reservations and soft-deletes the ticket
// in one transaction.
func (s *Service) DeleteRepairTicket(ctx context.Context, orgID, id int64) error {
	var ticket *models.RepairTicket
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		var err error
		ticket, err = s.store.GetRepairTicketForUpdate(txCtx, orgID, id)
		if err != nil {
			return err
		}
		parts, err := s.store.ListTicketParts(txCtx, orgID, id)
		if err != nil {
			return err
		}
		for _, part := range activeParts(parts) {
			if err := s.inventory.Release(txCtx, orgID, part.InventoryPartID, part.Quantity); err != nil {
				return err
			}
		}
		rows, err := s.store.SoftDeleteRepairTicket(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if rows != 1 {
			return fleeterror.NewNotFoundErrorf("ticket %d not found", id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventTicketDeleted,
			OrganizationID: &orgID,
			SiteID:         ticket.SiteID,
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
	if newStatus == models.TicketStatusCompleted {
		return 0, fleeterror.NewInvalidArgumentError("use bulk close to complete tickets")
	}
	if newStatus == models.TicketStatusSentToVendor {
		return 0, fleeterror.NewInvalidArgumentError("tickets must be sent to a vendor individually with RMA details")
	}

	ids, err := normalizeBulkIDs(ticketIDs)
	if err != nil {
		return 0, err
	}
	var affected int64
	var scope activitymodels.SiteScope
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		attemptSiteIDs := make([]*int64, 0, len(ids))
		for _, id := range ids {
			ticket, err := s.store.GetRepairTicketForUpdate(txCtx, orgID, id)
			if err != nil {
				return err
			}
			if ticket.Status == models.TicketStatusCompleted || !statusTransitionAllowed(ticket.Status, newStatus) {
				return fleeterror.NewFailedPreconditionErrorf("ticket %d cannot transition to status %d", id, newStatus)
			}
			attemptSiteIDs = append(attemptSiteIDs, ticket.SiteID)
		}
		rows, err := s.store.BulkUpdateTicketStatus(txCtx, orgID, ids, int16(newStatus))
		if err != nil {
			return err
		}
		affected = rows
		scope = activitymodels.ResolveSiteScope(attemptSiteIDs)
		return nil
	})
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
		event.ApplySiteScope(scope)
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// BulkAssign sets the assignee on multiple tickets. Pass nil
// assigneeUserID to unassign. Returns affected row count.
func (s *Service) BulkAssign(ctx context.Context, orgID int64, ticketIDs []int64, assigneeUserID *int64) (int64, error) {
	ids, err := normalizeBulkIDs(ticketIDs)
	if err != nil {
		return 0, err
	}

	var affected int64
	var scope activitymodels.SiteScope
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if assigneeUserID != nil {
			if _, err := s.refs.ResolveAssignee(txCtx, orgID, *assigneeUserID); err != nil {
				return err
			}
		}
		siteIDs, err := s.requireMutableTickets(txCtx, orgID, ids)
		if err != nil {
			return err
		}
		rows, err := s.store.BulkAssignTickets(txCtx, orgID, ids, assigneeUserID)
		if err != nil {
			return err
		}
		affected = rows
		scope = activitymodels.ResolveSiteScope(siteIDs)
		return nil
	})
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
		event.ApplySiteScope(scope)
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// BulkMarkUrgent flags multiple tickets as urgent. Returns affected row
// count.
func (s *Service) BulkMarkUrgent(ctx context.Context, orgID int64, ticketIDs []int64) (int64, error) {
	ids, err := normalizeBulkIDs(ticketIDs)
	if err != nil {
		return 0, err
	}

	var affected int64
	var scope activitymodels.SiteScope
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		siteIDs, err := s.requireMutableTickets(txCtx, orgID, ids)
		if err != nil {
			return err
		}
		rows, err := s.store.BulkMarkUrgent(txCtx, orgID, ids)
		if err != nil {
			return err
		}
		affected = rows
		scope = activitymodels.ResolveSiteScope(siteIDs)
		return nil
	})
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
		event.ApplySiteScope(scope)
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return affected, nil
}

// BulkClose completes the exact selected ticket set and consumes each
// ticket's existing reservations in one transaction.
func (s *Service) BulkClose(ctx context.Context, params models.BulkCloseParams) (int64, error) {
	ids, err := normalizeBulkIDs(params.TicketIDs)
	if err != nil {
		return 0, err
	}
	if len(params.PartsUsed) > 0 {
		return 0, fleeterror.NewInvalidArgumentError("bulk close cannot apply one parts list to multiple tickets")
	}
	if params.Resolution < models.TicketResolutionRepaired || params.Resolution > models.TicketResolutionNoActionNeeded {
		return 0, fleeterror.NewInvalidArgumentError("resolution is required when completing tickets")
	}

	var affected int64
	var scope activitymodels.SiteScope
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		var attemptAffected int64
		attemptSiteIDs := make([]*int64, 0, len(ids))
		minerIDs := make([]int64, 0, len(ids))
		infrastructureIDs := make([]int64, 0, len(ids))
		for _, id := range ids {
			ticket, err := s.store.GetRepairTicketForUpdate(txCtx, params.OrgID, id)
			if err != nil {
				return err
			}
			if ticket.Status == models.TicketStatusCompleted {
				continue
			}
			if !statusTransitionAllowed(ticket.Status, models.TicketStatusCompleted) {
				return fleeterror.NewFailedPreconditionErrorf("ticket %d cannot be completed", id)
			}
			attemptSiteIDs = append(attemptSiteIDs, ticket.SiteID)
			completionLocation := params.RepairLocation
			if ticket.Category == models.TicketCategoryInfrastructure {
				completionLocation = models.RepairLocationUnspecified
			}
			if err := validateCompletion(ticket.Category, params.Resolution, completionLocation); err != nil {
				return err
			}
			parts, err := s.store.ListTicketParts(txCtx, params.OrgID, id)
			if err != nil {
				return err
			}
			for _, part := range activeParts(parts) {
				if err := s.inventory.ConsumeReserved(txCtx, params.OrgID, part.InventoryPartID, part.Quantity); err != nil {
					return err
				}
			}
			if err := s.store.MarkTicketPartsConsumed(txCtx, params.OrgID, id); err != nil {
				return err
			}
			if ticket.Category == models.TicketCategoryMiner {
				minerIDs = append(minerIDs, id)
			} else {
				infrastructureIDs = append(infrastructureIDs, id)
			}
		}
		if len(minerIDs) > 0 {
			rows, err := s.store.BulkCloseTickets(txCtx, params.OrgID, minerIDs, int16(params.Resolution), int16(params.RepairLocation), params.Notes)
			if err != nil {
				return err
			}
			attemptAffected += rows
		}
		if len(infrastructureIDs) > 0 {
			rows, err := s.store.BulkCloseTickets(txCtx, params.OrgID, infrastructureIDs, int16(params.Resolution), int16(models.RepairLocationUnspecified), params.Notes)
			if err != nil {
				return err
			}
			attemptAffected += rows
		}
		affected = attemptAffected
		scope = activitymodels.ResolveSiteScope(attemptSiteIDs)
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
		event.ApplySiteScope(scope)
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
func (s *Service) GetTicketStats(ctx context.Context, filter models.ListFilter) (*models.TicketStats, error) {
	return s.store.GetTicketStats(ctx, filter)
}

// ---------------------------------------------------------------
// Comments
// ---------------------------------------------------------------

// CreateComment adds a comment to a ticket. Validates the ticket exists
// in the org before inserting.
func (s *Service) CreateComment(ctx context.Context, orgID, ticketID, userID int64, userName, text string) (*models.TicketComment, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fleeterror.NewInvalidArgumentError("comment text is required")
	}
	if utf8.RuneCountInString(text) > 4096 {
		return nil, fleeterror.NewInvalidArgumentError("comment text must not exceed 4096 characters")
	}

	if s.transactor == nil {
		return nil, fleeterror.NewInternalError("maintenance transactor is not configured")
	}
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		ticket, err := s.store.GetRepairTicketForUpdate(txCtx, orgID, ticketID)
		if err != nil {
			return nil, err
		}
		comment, err := s.store.CreateTicketComment(txCtx, orgID, ticketID, userID, text)
		if err != nil {
			return nil, err
		}
		return &commentCreateResult{comment: comment, siteID: ticket.SiteID}, nil
	})
	if err != nil {
		return nil, err
	}
	created, ok := result.(*commentCreateResult)
	if !ok || created == nil || created.comment == nil {
		return nil, fleeterror.NewInternalError("create comment transaction returned an invalid result")
	}
	comment := created.comment
	comment.AuthoredByCaller = true

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventCommentCreated,
			OrganizationID: &orgID,
			SiteID:         created.siteID,
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

// ListTicketComments returns all live comments for a ticket after
// confirming that the ticket belongs to the caller's organization.
func (s *Service) ListTicketComments(ctx context.Context, orgID, ticketID int64) ([]models.TicketComment, error) {
	if _, err := s.store.GetRepairTicket(ctx, orgID, ticketID); err != nil {
		return nil, err
	}
	return s.store.ListTicketComments(ctx, orgID, ticketID)
}

// DeleteComment soft-deletes a comment.
func (s *Service) DeleteComment(ctx context.Context, orgID, callerUserID, commentID int64) error {
	if s.transactor == nil {
		return fleeterror.NewInternalError("maintenance transactor is not configured")
	}
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		siteID, err := s.store.GetTicketCommentSiteForUpdate(txCtx, orgID, callerUserID, commentID)
		if err != nil {
			return nil, err
		}
		rowsAffected, err := s.store.SoftDeleteTicketComment(txCtx, orgID, callerUserID, commentID)
		if err != nil {
			return nil, err
		}
		if rowsAffected == 0 {
			return nil, fleeterror.NewNotFoundErrorf("comment %d not found", commentID)
		}
		return &commentDeleteResult{siteID: siteID}, nil
	})
	if err != nil {
		return err
	}
	deleted, ok := result.(*commentDeleteResult)
	if !ok || deleted == nil {
		return fleeterror.NewInternalError("delete comment transaction returned an invalid result")
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventCommentDeleted,
			OrganizationID: &orgID,
			SiteID:         deleted.siteID,
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
func (s *Service) ListCompletedTickets(ctx context.Context, filter models.CompletedFilter) ([]models.RepairTicketSummary, int32, bool, error) {
	pageSize := clampLimit(filter.Limit)
	filter.Limit = pageSize
	countFilter := filter
	filter.Limit++
	tickets, err := s.store.ListCompletedTickets(ctx, filter)
	if err != nil {
		return nil, 0, false, err
	}
	total, err := s.store.CountCompletedTickets(ctx, countFilter)
	if err != nil {
		return nil, 0, false, err
	}
	hasNext := len(tickets) > int(pageSize)
	if hasNext {
		tickets = tickets[:pageSize]
	}
	return tickets, total, hasNext, nil
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

// ListAssignees returns every active user with a live membership in the
// organization. Role-based routing is intentionally outside V1.
func (s *Service) ListAssignees(ctx context.Context, orgID int64) ([]models.Assignee, error) {
	return s.refs.ListAssignees(ctx, orgID)
}

// ---------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------

func (s *Service) reconcileParts(ctx context.Context, ticket *models.RepairTicket, requested *[]models.PartUsage) ([]models.PartUsage, error) {
	existing, err := s.store.ListTicketParts(ctx, ticket.OrgID, ticket.ID)
	if err != nil {
		return nil, err
	}
	current := activeParts(existing)
	if requested == nil {
		return current, nil
	}
	next, err := normalizeParts(*requested)
	if err != nil {
		return nil, err
	}
	for index := range next {
		part, err := s.inventory.GetForUpdate(ctx, ticket.OrgID, next[index].InventoryPartID)
		if err != nil {
			return nil, err
		}
		if ticket.SiteID == nil || part.SiteID == nil || *ticket.SiteID != *part.SiteID {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"inventory part %d is not stocked at the ticket site",
				next[index].InventoryPartID,
			)
		}
		next[index].PartName = part.Name
	}
	currentByID := partsByID(current)
	nextByID := partsByID(next)
	ids := make([]int64, 0, len(currentByID)+len(nextByID))
	seen := make(map[int64]struct{}, len(currentByID)+len(nextByID))
	for id := range currentByID {
		seen[id] = struct{}{}
	}
	for id := range nextByID {
		seen[id] = struct{}{}
	}
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		delta := nextByID[id].Quantity - currentByID[id].Quantity
		switch {
		case delta > 0:
			if err := s.inventory.Reserve(ctx, ticket.OrgID, id, delta); err != nil {
				return nil, err
			}
		case delta < 0:
			if err := s.inventory.Release(ctx, ticket.OrgID, id, -delta); err != nil {
				return nil, err
			}
		}
	}
	if err := s.store.SetTicketParts(ctx, ticket.OrgID, ticket.ID); err != nil {
		return nil, err
	}
	for _, part := range next {
		if err := s.store.InsertTicketPart(ctx, ticket.OrgID, ticket.ID, part.InventoryPartID, part.PartName, part.Quantity); err != nil {
			return nil, err
		}
	}
	return next, nil
}

func (s *Service) requireMutableTickets(ctx context.Context, orgID int64, ids []int64) ([]*int64, error) {
	siteIDs := make([]*int64, 0, len(ids))
	for _, id := range ids {
		ticket, err := s.store.GetRepairTicketForUpdate(ctx, orgID, id)
		if err != nil {
			return nil, err
		}
		if ticket.Status == models.TicketStatusCompleted {
			return nil, fleeterror.NewFailedPreconditionErrorf("completed ticket %d is terminal", id)
		}
		siteIDs = append(siteIDs, ticket.SiteID)
	}
	return siteIDs, nil
}

func normalizeBulkIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("ticket_ids must not be empty")
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("ticket_ids must contain only positive IDs")
		}
		seen[id] = struct{}{}
	}
	result := make([]int64, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func normalizeParts(parts []models.PartUsage) ([]models.PartUsage, error) {
	byID := make(map[int64]models.PartUsage, len(parts))
	for _, part := range parts {
		if part.InventoryPartID <= 0 || part.Quantity <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("part IDs and quantities must be greater than zero")
		}
		combined := int64(byID[part.InventoryPartID].Quantity) + int64(part.Quantity)
		if combined > int64(1<<31-1) {
			return nil, fleeterror.NewInvalidArgumentError("combined part quantity is too large")
		}
		current := byID[part.InventoryPartID]
		if current.PartName == "" {
			current.PartName = strings.TrimSpace(part.PartName)
		}
		current.InventoryPartID = part.InventoryPartID
		current.Quantity = int32(combined) //nolint:gosec // explicit MaxInt32 bound above
		byID[part.InventoryPartID] = current
	}
	ids := make([]int64, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]models.PartUsage, 0, len(ids))
	for _, id := range ids {
		result = append(result, byID[id])
	}
	return result, nil
}

func activeParts(parts []models.PartUsage) []models.PartUsage {
	active := make([]models.PartUsage, 0, len(parts))
	for _, part := range parts {
		if part.ConsumedAt == nil {
			active = append(active, part)
		}
	}
	return active
}

func partsByID(parts []models.PartUsage) map[int64]models.PartUsage {
	result := make(map[int64]models.PartUsage, len(parts))
	for _, part := range parts {
		result[part.InventoryPartID] = part
	}
	return result
}

func validateStatusTransition(status models.TicketStatus) error {
	if status < models.TicketStatusOpen || status > models.TicketStatusCompleted {
		return fleeterror.NewInvalidArgumentErrorf("invalid ticket status: %d", int16(status))
	}
	return nil
}

func statusTransitionAllowed(from, to models.TicketStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case models.TicketStatusOpen:
		return to == models.TicketStatusInProgress || to == models.TicketStatusOnHold || to == models.TicketStatusSentToVendor || to == models.TicketStatusCompleted
	case models.TicketStatusInProgress:
		return to == models.TicketStatusOpen || to == models.TicketStatusOnHold || to == models.TicketStatusSentToVendor || to == models.TicketStatusCompleted
	case models.TicketStatusOnHold:
		return to == models.TicketStatusOpen || to == models.TicketStatusInProgress || to == models.TicketStatusSentToVendor || to == models.TicketStatusCompleted
	case models.TicketStatusSentToVendor:
		return to == models.TicketStatusInProgress || to == models.TicketStatusCompleted
	case models.TicketStatusCompleted, models.TicketStatusUnspecified:
		return false
	}
	return false
}

func validateCompletion(category models.TicketCategory, resolution models.TicketResolution, location models.RepairLocation) error {
	if resolution < models.TicketResolutionRepaired || resolution > models.TicketResolutionNoActionNeeded {
		return fleeterror.NewInvalidArgumentError("resolution is required when completing a ticket")
	}
	requiresLocation := category == models.TicketCategoryMiner &&
		(resolution == models.TicketResolutionRepaired || resolution == models.TicketResolutionReplaced)
	if requiresLocation {
		if location != models.RepairLocationOnRack && location != models.RepairLocationRepairBench {
			return fleeterror.NewInvalidArgumentError("repair_location is required for repaired or replaced miner tickets")
		}
		return nil
	}
	if location != models.RepairLocationUnspecified {
		return fleeterror.NewInvalidArgumentError("repair_location is only valid for repaired or replaced miner tickets")
	}
	return nil
}

func derefInt64(v *int64) any {
	if v == nil {
		return "(none)"
	}
	return *v
}
