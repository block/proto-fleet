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

func TestClientTicketRPCUsesLiveAccountAuthorityAndSession(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Ticket customer", f.host, domain.DefaultCapabilities)
	invitation := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	registered := f.register(t, invitation.Token, "ticket-customer")
	cookie := registered.Header().Get("Set-Cookie")
	submitted, err := f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "My entire fleet appears offline", IdempotencyKey: uuid.NewString()}, cookie, account.AccountID))
	require.NoError(t, err, "a customer needs no host maintenance permissions to report a problem")
	require.Equal(t, mpb.TicketCategory_TICKET_CATEGORY_ACCOUNT, submitted.Msg.Detail.Ticket.Category)
	get := &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: submitted.Msg.Detail.Ticket.Id}
	metadata, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err)
	require.True(t, metadata.Msg.Account.TicketAccess, "viewer teammates receive account ticketing automatically")
	_, err = f.client.GetAccountTicket(t.Context(), rpcRequest(get, cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "ticket reads require the selected account")
	_, err = f.client.GetAccountTicket(t.Context(), rpcRequest(get, cookie, uuid.NewString()))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "request account and selected workspace must agree")
	key, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Customer automation", Capabilities: 1}, cookie, account.AccountID))
	require.NoError(t, err)
	_, err = f.client.GetAccountTicket(t.Context(), keyRequest(get, key.Msg.Secret, account.AccountID))
	require.Error(t, err, "account keys do not gain access to session-only ticket conversations")
	var externalUser string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE username='ticket-customer'`).Scan(&externalUser))
	_, err = f.client.RevokeMember(t.Context(), rpcRequest(&pb.RevokeMemberRequest{AccountId: account.AccountID, UserId: externalUser}, f.cookie, account.AccountID))
	require.NoError(t, err)
	_, err = f.client.GetAccountTicket(t.Context(), rpcRequest(get, cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestClientTicketRPCAccountDisableHidesExistingTickets(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Configurable customer", f.host, domain.DefaultCapabilities)
	created, err := f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Missing asset assignments", IdempotencyKey: uuid.NewString()}, f.cookie, account.AccountID))
	require.NoError(t, err)
	disabled := false
	_, err = f.client.ConfigureAccount(t.Context(), rpcRequest(&pb.ConfigureAccountRequest{AccountId: f.root, ChildAccountId: account.AccountID, Capabilities: 31, TicketingEnabled: &disabled}, f.cookie, f.root))
	require.NoError(t, err)
	_, err = f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Msg.Detail.Ticket.Id}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Another issue", IdempotencyKey: uuid.NewString()}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	var retained int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM repair_ticket WHERE id=$1", created.Msg.Detail.Ticket.Id).Scan(&retained))
	require.Equal(t, 1, retained)
}
