package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClientTicketRPCMutationIsolationHasNoSideEffects(t *testing.T) {
	f := newRPCFixture(t)
	a := f.account(t, "Account A", f.host, domain.AllCapabilities)
	b := f.account(t, "Account B", f.host, domain.AllCapabilities)
	invitation := f.invite(t, a.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	cookieA := f.register(t, invitation.Token, "isolated-a").Header().Get("Set-Cookie")
	invitation = f.invite(t, b.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	cookieB := f.register(t, invitation.Token, "isolated-b").Header().Get("Set-Cookie")
	created, err := f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: a.AccountID, Description: "Account A private issue", IdempotencyKey: uuid.NewString()}, cookieA, a.AccountID))
	require.NoError(t, err)
	closed, err := f.client.ChangeAccountTicketStatus(t.Context(), rpcRequest(&pb.ChangeAccountTicketStatusRequest{AccountId: a.AccountID, TicketId: created.Msg.Detail.Ticket.Id, Close: true, ResolutionNote: "Resolved", ExpectedUpdatedAt: created.Msg.Detail.Ticket.UpdatedAt}, cookieA, a.AccountID))
	require.NoError(t, err)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Private host site','private-host') RETURNING id`, f.org).Scan(&site))
	private, err := f.maintenance.CreateRepairTicket(t.Context(), rpcRequest(&mpb.CreateRepairTicketRequest{Category: mpb.TicketCategory_TICKET_CATEGORY_INFRASTRUCTURE, Component: "Power", Description: "Host investigation", SiteId: &site, ClientAccountIds: []string{a.AccountID, b.AccountID}, IdempotencyKey: uuid.NewString()}, f.cookie, ""))
	require.NoError(t, err)
	type state struct {
		tickets, comments, audits int
		rows                      string
	}
	snapshot := func() state {
		var s state
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM repair_ticket WHERE org_id=$1),(SELECT count(*) FROM repair_ticket_comment WHERE org_id=$1),(SELECT count(*) FROM activity_log WHERE organization_id=$1),COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM repair_ticket r WHERE org_id=$1),'[]')`, f.org).Scan(&s.tickets, &s.comments, &s.audits, &s.rows))
		return s
	}
	mutations := func(t *testing.T, cookie, account string, ticket *mpb.RepairTicket, code connect.Code) {
		t.Helper()
		before := snapshot()
		_, err := f.client.CommentAccountTicket(t.Context(), rpcRequest(&pb.CommentAccountTicketRequest{AccountId: account, TicketId: ticket.Id, Text: "Unauthorized reply", IdempotencyKey: uuid.NewString()}, cookie, account))
		require.Equal(t, code, connect.CodeOf(err))
		for _, close := range []bool{true, false} {
			note := ""
			if close {
				note = "Unauthorized close"
			}
			_, err = f.client.ChangeAccountTicketStatus(t.Context(), rpcRequest(&pb.ChangeAccountTicketStatusRequest{AccountId: account, TicketId: ticket.Id, Close: close, ResolutionNote: note, ExpectedUpdatedAt: ticket.UpdatedAt}, cookie, account))
			require.Equal(t, code, connect.CodeOf(err))
		}
		require.Equal(t, before, snapshot(), "denied requests cannot write comments, ticket state, or audits")
	}
	t.Run("sibling account", func(t *testing.T) { mutations(t, cookieB, b.AccountID, closed.Msg.Detail.Ticket, connect.CodeNotFound) })
	t.Run("sibling reads", func(t *testing.T) {
		before := snapshot()
		_, err := f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: b.AccountID, TicketId: closed.Msg.Detail.Ticket.Id}, cookieB, b.AccountID))
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
		listed, err := f.client.ListAccountTickets(t.Context(), rpcRequest(&pb.ListAccountTicketsRequest{AccountId: b.AccountID}, cookieB, b.AccountID))
		require.NoError(t, err)
		require.Empty(t, listed.Msg.Tickets, "neither sibling-created nor internal host tickets may enter the account list")
		require.Equal(t, before, snapshot())
	})

	t.Run("unshared host ticket", func(t *testing.T) { mutations(t, cookieB, b.AccountID, private.Msg.Ticket, connect.CodeNotFound) })
	var userID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE username='isolated-a'`).Scan(&userID))
	_, err = f.client.RevokeMember(t.Context(), rpcRequest(&pb.RevokeMemberRequest{AccountId: a.AccountID, UserId: userID}, f.cookie, a.AccountID))
	require.NoError(t, err)
	t.Run("revoked teammate", func(t *testing.T) {
		mutations(t, cookieA, a.AccountID, closed.Msg.Detail.Ticket, connect.CodePermissionDenied)
		_, err := f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: a.AccountID, TicketId: closed.Msg.Detail.Ticket.Id}, cookieA, a.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		_, err = f.client.ListAccountTickets(t.Context(), rpcRequest(&pb.ListAccountTicketsRequest{AccountId: a.AccountID}, cookieA, a.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		_, err = f.client.ListAccountTicketAssets(t.Context(), rpcRequest(&pb.ListAccountTicketAssetsRequest{AccountId: a.AccountID}, cookieA, a.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		before := snapshot()
		_, err = f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: a.AccountID, Description: "Unauthorized issue", IdempotencyKey: uuid.NewString()}, cookieA, a.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.Equal(t, before, snapshot())
	})
}

func TestClientTicketRPCRequiresDirectMembershipBeyondAncestorGrants(t *testing.T) {
	f := newRPCFixture(t)
	parent := f.account(t, "Parent", f.host, domain.AllCapabilities)
	childAdmin := f.addUser(t, "child-admin", false)
	child, err := f.accounts.CreateChild(t.Context(), f.host, f.org, parent.AccountID, "Child", childAdmin, domain.AllCapabilities)
	require.NoError(t, err)
	created, err := f.accounts.CreateAccountTicket(t.Context(), childAdmin, f.org, &pb.CreateAccountTicketRequest{AccountId: child.Account.AccountID, Description: "Child private issue", IdempotencyKey: uuid.NewString()})
	require.NoError(t, err)
	account := child.Account.AccountID
	before := func() []int {
		counts := make([]int, 3)
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM repair_ticket WHERE org_id=$1),(SELECT count(*) FROM repair_ticket_comment WHERE org_id=$1),(SELECT count(*) FROM activity_log WHERE organization_id=$1)`, f.org).Scan(&counts[0], &counts[1], &counts[2]))
		return counts
	}
	original := before()
	calls := []struct {
		name string
		call func() error
	}{
		{"get", func() error {
			_, err := f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: account, TicketId: created.Ticket.Id}, f.cookie, account))
			return err
		}},
		{"list", func() error {
			_, err := f.client.ListAccountTickets(t.Context(), rpcRequest(&pb.ListAccountTicketsRequest{AccountId: account}, f.cookie, account))
			return err
		}},
		{"assets", func() error {
			_, err := f.client.ListAccountTicketAssets(t.Context(), rpcRequest(&pb.ListAccountTicketAssetsRequest{AccountId: account}, f.cookie, account))
			return err
		}},
		{"create", func() error {
			_, err := f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: account, Description: "Ancestor issue", IdempotencyKey: uuid.NewString()}, f.cookie, account))
			return err
		}},
		{"comment", func() error {
			_, err := f.client.CommentAccountTicket(t.Context(), rpcRequest(&pb.CommentAccountTicketRequest{AccountId: account, TicketId: created.Ticket.Id, Text: "Ancestor reply", IdempotencyKey: uuid.NewString()}, f.cookie, account))
			return err
		}},
		{"close", func() error {
			_, err := f.client.ChangeAccountTicketStatus(t.Context(), rpcRequest(&pb.ChangeAccountTicketStatusRequest{AccountId: account, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Closed", ExpectedUpdatedAt: created.Ticket.UpdatedAt}, f.cookie, account))
			return err
		}},
		{"reopen", func() error {
			_, err := f.client.ChangeAccountTicketStatus(t.Context(), rpcRequest(&pb.ChangeAccountTicketStatusRequest{AccountId: account, TicketId: created.Ticket.Id, ExpectedUpdatedAt: created.Ticket.UpdatedAt}, f.cookie, account))
			return err
		}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) { require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(call.call())) })
	}
	require.Equal(t, original, before())
}
