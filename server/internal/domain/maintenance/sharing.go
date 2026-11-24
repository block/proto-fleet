package maintenance

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
)

func verifyCommentAudience(ticket *models.RepairTicket, accountID string) error {
	if accountID == "" {
		return nil
	}
	for _, a := range ticket.Audiences {
		if a.AccountID == accountID {
			return nil
		}
	}
	return fleeterror.NewInvalidArgumentError("client is not an affected account on this ticket")
}

// ShareRepairTicket exposes the opening recipients and client-facing fields.
// Internal notes retain their own visibility.
func (s *Service) ShareRepairTicket(ctx context.Context, orgID, ticketID int64, expected time.Time) (*models.RepairTicket, error) {
	if expected.IsZero() {
		return nil, fleeterror.NewInvalidArgumentError("expected_updated_at is required")
	}
	var ticket *models.RepairTicket
	err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		current, err := s.store.GetRepairTicketForUpdate(ctx, orgID, ticketID)
		if err != nil {
			return err
		}
		if err := verifyAuthorizedSite(ctx, current.SiteID); err != nil {
			return err
		}
		if current.SharedWithClients {
			ticket = current
			return nil
		}
		if err := verifyTicketVersion(current, expected); err != nil {
			return err
		}
		if len(current.Audiences) == 0 {
			return fleeterror.NewInvalidArgumentError("ticket has no affected clients")
		}
		if err := s.store.ShareRepairTicket(ctx, orgID, ticketID); err != nil {
			return err
		}
		ticket, err = s.store.GetRepairTicket(ctx, orgID, ticketID)
		return err
	})
	return ticket, err
}

// SetTicketCommentVisibility changes a host note's visibility for the ticket audience.
// Account-scoped client replies remain in their original conversation.
func (s *Service) SetTicketCommentVisibility(ctx context.Context, orgID, ticketID, commentID int64, internal bool, expected time.Time) error {
	if expected.IsZero() {
		return fleeterror.NewInvalidArgumentError("expected_updated_at is required")
	}
	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		ticket, err := s.store.GetRepairTicketForUpdate(ctx, orgID, ticketID)
		if err != nil {
			return err
		}
		if err := verifyAuthorizedSite(ctx, ticket.SiteID); err != nil {
			return err
		}
		comment, err := s.store.GetTicketCommentForVisibility(ctx, orgID, ticketID, commentID)
		if err != nil {
			return err
		}
		if comment.AccountID != "" {
			return fleeterror.NewInvalidArgumentError("client replies keep their account conversation")
		}
		if !internal && (!ticket.SharedWithClients || len(ticket.Audiences) == 0) {
			return fleeterror.NewInvalidArgumentError("share the ticket before sharing a note")
		}
		if comment.Internal == internal {
			return nil
		}
		if err := verifyTicketVersion(ticket, expected); err != nil {
			return err
		}
		return s.store.SetTicketCommentVisibility(ctx, orgID, ticketID, commentID, internal)
	})
}
