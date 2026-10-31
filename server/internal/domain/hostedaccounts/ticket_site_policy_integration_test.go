package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTicketLiveSitePolicyRedactsPhysicalContextAndPreservesConversation(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Historical customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Original site','original-ticket-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module SET model='Private model' WHERE id=$1`, module)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Retained repair conversation", Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MODULE, Id: module}}, IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	require.NotEmpty(t, created.Ticket.Targets[0].Label)
	require.NotNil(t, created.Ticket.Targets[0].SiteId)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, nil))
	view, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	listed, err := f.svc.ListAccountTickets(t.Context(), f.user, f.org, &pb.ListAccountTicketsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Tickets, 1)
	for _, ticket := range []*mpb.RepairTicket{view.Ticket, listed.Tickets[0]} {
		require.Equal(t, created.Ticket.Description, ticket.Description)
		require.Len(t, ticket.Targets, 1)
		target := ticket.Targets[0]
		require.Equal(t, module, target.Id)
		require.Equal(t, mpb.TicketCategory_TICKET_CATEGORY_MODULE, target.Kind)
		require.True(t, target.OutsidePolicy)
		require.Empty(t, target.Label)
		require.Empty(t, target.Model)
		require.Nil(t, target.SiteId)
		require.Nil(t, target.ContainerId)
		require.Nil(t, target.ModuleId)
		require.Nil(t, target.InstallationId)
		require.Nil(t, target.Position)
	}
	commented, err := f.svc.CommentAccountTicket(t.Context(), f.user, f.org, &pb.CommentAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, Text: "Historical follow-up", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	require.Len(t, commented.Comments, 1)
	require.Empty(t, commented.Ticket.Targets[0].Model)
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "No longer needed", ExpectedUpdatedAt: commented.Ticket.UpdatedAt})
	require.NoError(t, err)
	reopened, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, ExpectedUpdatedAt: closed.Ticket.UpdatedAt})
	require.NoError(t, err)
	require.Equal(t, mpb.TicketStatus_TICKET_STATUS_OPEN, reopened.Ticket.Status)
	require.Empty(t, reopened.Ticket.Targets[0].Model)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, false, nil))
	restored, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.False(t, restored.Ticket.Targets[0].OutsidePolicy)
	require.Equal(t, "Private model", restored.Ticket.Targets[0].Model)
	require.Equal(t, site, restored.Ticket.Targets[0].GetSiteId())
}
