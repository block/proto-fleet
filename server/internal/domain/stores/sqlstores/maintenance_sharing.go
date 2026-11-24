package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func (s *SQLMaintenanceStore) ShareRepairTicket(ctx context.Context, orgID, ticketID int64) error {
	if err := s.GetQueries(ctx).ShareRepairTicket(ctx, sqlc.ShareRepairTicketParams{OrgID: orgID, ID: ticketID}); err != nil {
		return maintenanceWriteError("share ticket", err)
	}
	return nil
}
func (s *SQLMaintenanceStore) SetTicketCommentVisibility(ctx context.Context, orgID, ticketID, commentID int64, internal bool) error {
	n, err := s.GetQueries(ctx).SetRepairTicketCommentVisibility(ctx, sqlc.SetRepairTicketCommentVisibilityParams{OrgID: orgID, TicketID: ticketID, CommentID: commentID, Internal: internal})
	if err != nil {
		return maintenanceWriteError("set ticket note visibility", err)
	}
	if n == 0 {
		return fleeterror.NewNotFoundError("ticket note not found")
	}
	return nil
}

func (s *SQLMaintenanceStore) GetTicketCommentForVisibility(ctx context.Context, orgID, ticketID, commentID int64) (*models.TicketComment, error) {
	row, err := s.GetQueries(ctx).GetRepairTicketCommentForVisibility(ctx, sqlc.GetRepairTicketCommentForVisibilityParams{OrgID: orgID, TicketID: ticketID, ID: commentID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fleeterror.NewNotFoundError("ticket note not found")
	}
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("load ticket note: %w", err)
	}
	return &models.TicketComment{ID: commentID, TicketID: ticketID, Internal: row.Internal, AccountID: row.AccountUuid}, nil
}
