package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTicketVisibilityMigrationPreservesPrivateAndSharedTickets(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	service := ticketServiceForTest(f.db)
	params := models.CreateParams{OrgID: f.org, Category: models.TicketCategoryModule, Component: "Hashboard", Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}, IdempotencyKey: uuid.NewString()}
	private, _, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	params.ShareWithClients = true
	params.IdempotencyKey = uuid.NewString()
	shared, _, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	snapshot := func() string {
		t.Helper()
		var state string
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
			'tickets',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM repair_ticket r WHERE org_id=$1),
			'audiences',(SELECT jsonb_agg(to_jsonb(a) ORDER BY ticket_id,account_id) FROM repair_ticket_audience a WHERE org_id=$1))::text`, f.org).Scan(&state))
		return state
	}
	before := snapshot()
	migration, err := migrations.Current.ReadFile("current/001048_internal_client_ticketing.down.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(migration))
	require.ErrorContains(t, err, "client ticketing has durable data; preserve it and fix forward")
	require.Equal(t, before, snapshot(), "a refused rollback must preserve private audiences, shared tickets, and their visibility")

	_, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: private.ID})
	require.True(t, fleeterror.IsNotFoundError(err))
	view, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: shared.ID})
	require.NoError(t, err)
	require.True(t, view.Ticket.SharedWithClients)
}
func TestHostNoteVisibilityFollowsTicketAudienceAndCanBeRevoked(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client A", f.user, hostedaccounts.DefaultCapabilities)
	b := f.client(t, "Client B", f.host, hostedaccounts.DefaultCapabilities)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Demo','sharing-demo') RETURNING id", f.org).Scan(&site))
	service := ticketServiceForTest(f.db)
	ticket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, Category: models.TicketCategoryInfrastructure, SiteID: &site, Component: "Power", ClientAccountIDs: []string{a.AccountID, b.AccountID}, IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	note, _, err := service.CreateComment(t.Context(), f.org, ticket.ID, f.host, "", "Host diagnosis", uuid.NewString())
	require.NoError(t, err)
	require.Error(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, false, ticket.UpdatedAt), "an internal ticket cannot publish a note")
	ticket, err = service.ShareRepairTicket(t.Context(), f.org, ticket.ID, ticket.UpdatedAt)
	require.NoError(t, err)
	reviewedVersion := ticket.UpdatedAt
	require.NoError(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, false, reviewedVersion))
	for _, account := range []struct {
		actor int64
		id    string
	}{{f.user, a.AccountID}, {f.host, b.AccountID}} {
		view, err := f.svc.GetAccountTicket(t.Context(), account.actor, f.org, &pb.GetAccountTicketRequest{AccountId: account.id, TicketId: ticket.ID})
		require.NoError(t, err)
		require.Len(t, view.Comments, 1, "a host note inherits the full ticket audience")
		require.Equal(t, note.Text, view.Comments[0].Text)
	}
	current, err := service.GetRepairTicket(t.Context(), f.org, ticket.ID)
	require.NoError(t, err)
	require.NoError(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, true, current.Ticket.UpdatedAt))
	require.Error(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, false, reviewedVersion), "a stale sharing retry cannot republish a hidden note")
	for _, account := range []struct {
		actor int64
		id    string
	}{{f.user, a.AccountID}, {f.host, b.AccountID}} {
		view, err := f.svc.GetAccountTicket(t.Context(), account.actor, f.org, &pb.GetAccountTicketRequest{AccountId: account.id, TicketId: ticket.ID})
		require.NoError(t, err)
		require.Empty(t, view.Comments, "making the note internal removes it from subsequent client reads")
	}
	current, err = service.GetRepairTicket(t.Context(), f.org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, current.Comments, 1, "the host retains the note")
	require.True(t, current.Comments[0].Internal)
	require.NoError(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, false, current.Ticket.UpdatedAt), "a fresh confirmation can share the note again")
	reply, _, err := service.CreateComment(t.Context(), f.org, ticket.ID, f.user, "", "Client A private reply", uuid.NewString(), models.CommentScope{AccountID: a.AccountID})
	require.NoError(t, err)
	current, err = service.GetRepairTicket(t.Context(), f.org, ticket.ID)
	require.NoError(t, err)
	require.Error(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, reply.ID, false, current.Ticket.UpdatedAt), "client replies cannot be broadened to the ticket audience")
	require.Error(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, reply.ID, true, current.Ticket.UpdatedAt), "client replies retain their client conversation")
	other, err := f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: b.AccountID, TicketId: ticket.ID})
	require.NoError(t, err)
	require.Len(t, other.Comments, 1)
}

func TestHostTicketDefaultsPrivateOnClientEquipment(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	service := ticketServiceForTest(f.db)
	diagnosis := "Host-only confirmed fault"
	ticket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, Category: models.TicketCategoryModule, Component: "Hashboard", Description: "Power drops intermittently", Diagnosis: &diagnosis, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}, IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	_, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: ticket.ID})
	require.True(t, fleeterror.IsNotFoundError(err), "host-created tickets must be private by default: %v", err)
	listed, err := f.svc.ListAccountTickets(t.Context(), f.user, f.org, &pb.ListAccountTicketsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Empty(t, listed.Tickets)
	note, _, err := service.CreateComment(t.Context(), f.org, ticket.ID, f.host, "", "Internal investigation", uuid.NewString())
	require.NoError(t, err)
	require.True(t, note.Internal)
	shared, err := service.ShareRepairTicket(t.Context(), f.org, ticket.ID, ticket.UpdatedAt)
	require.NoError(t, err)
	require.True(t, shared.SharedWithClients)
	view, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: ticket.ID})
	require.NoError(t, err)
	require.Empty(t, view.Comments, "sharing a ticket must not publish internal note history")
	require.Equal(t, "Power drops intermittently", view.Ticket.Description)
	require.Empty(t, view.Ticket.Diagnosis, "sharing a ticket must not publish host diagnosis")
	listed, err = f.svc.ListAccountTickets(t.Context(), f.user, f.org, &pb.ListAccountTicketsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Tickets, 1)
	require.Empty(t, listed.Tickets[0].Diagnosis)

	require.NoError(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, false, shared.UpdatedAt))
	require.NoError(t, service.SetTicketCommentVisibility(t.Context(), f.org, ticket.ID, note.ID, false, shared.UpdatedAt), "retry sharing preserves the published note")
	view, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: ticket.ID})
	require.NoError(t, err)
	require.Len(t, view.Comments, 1)
	require.Equal(t, note.Text, view.Comments[0].Text)
	second, _, err := service.CreateComment(t.Context(), f.org, ticket.ID, f.host, "", "Still host-only", uuid.NewString())
	require.NoError(t, err)
	require.True(t, second.Internal, "host notes stay internal even after sharing the ticket")
	view, err = f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: ticket.ID})
	require.NoError(t, err)
	require.Len(t, view.Comments, 1, "the new internal note remains hidden after ticket sharing")

}
