package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountTicketCommentsPageWithoutExposingHiddenAnchors(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Paged conversation", f.user, hostedaccounts.AllCapabilities)
	created := createClientIssue(t, f, f.user, account.AccountID, "Equipment issue")
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO repair_ticket_comment(org_id,ticket_id,user_id,user_name,text,created_at,internal,account_id)
 SELECT $1,$2,$3,'Host','visible-'||lpad(n::text,3,'0'),'2026-01-01'::timestamptz,FALSE,NULL FROM generate_series(1,101) n`, f.org, created.Ticket.Id, f.host)
	require.NoError(t, err)
	var hidden int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO repair_ticket_comment(org_id,ticket_id,user_id,user_name,text,internal) VALUES($1,$2,$3,'Host','Internal secret',TRUE) RETURNING id`, f.org, created.Ticket.Id, f.host).Scan(&hidden))
	page, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.Len(t, page.Comments, 50)
	require.Equal(t, "visible-052", page.Comments[0].Text)
	require.Equal(t, "visible-101", page.Comments[49].Text)
	require.Equal(t, page.Comments[0].Id, page.NextBeforeCommentId)
	older, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, BeforeCommentId: page.NextBeforeCommentId})
	require.NoError(t, err)
	require.Len(t, older.Comments, 50)
	require.Equal(t, "visible-002", older.Comments[0].Text)
	require.Equal(t, "visible-051", older.Comments[49].Text)
	oldest, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, BeforeCommentId: older.NextBeforeCommentId})
	require.NoError(t, err)
	require.Len(t, oldest.Comments, 1)
	require.Equal(t, "visible-001", oldest.Comments[0].Text)
	require.Zero(t, oldest.NextBeforeCommentId)
	_, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, BeforeCommentId: hidden})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "hidden comments cannot serve as page anchors: %v", err)

	for _, change := range []string{"UPDATE repair_ticket_comment SET internal=TRUE WHERE id=$1", "UPDATE repair_ticket_comment SET internal=FALSE,deleted_at=clock_timestamp() WHERE id=$1"} {
		_, err = f.db.ExecContext(t.Context(), change, page.NextBeforeCommentId)
		require.NoError(t, err)
		_, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, BeforeCommentId: page.NextBeforeCommentId})
		require.True(t, fleeterror.IsFailedPreconditionError(err), "stale page anchor must require refresh rather than appear to end history: %v", err)
	}
	_, err = f.db.ExecContext(t.Context(), "UPDATE repair_ticket_comment SET internal=FALSE,deleted_at=NULL WHERE id=$1", page.NextBeforeCommentId)
	require.NoError(t, err)
	_, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, BeforeCommentId: -1})
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	commented, err := f.svc.CommentAccountTicket(t.Context(), f.user, f.org, &pb.CommentAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, Text: "Newest reply", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	require.Len(t, commented.Comments, 50)
	require.Equal(t, "Newest reply", commented.Comments[49].Text)
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Resolved", ExpectedUpdatedAt: commented.Ticket.UpdatedAt})
	require.NoError(t, err)
	require.Len(t, closed.Comments, 50)
	require.NotZero(t, closed.NextBeforeCommentId)
}
