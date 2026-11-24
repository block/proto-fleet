package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostCommentRPCDefaultsInternalAndRequiresExplicitPublicReply(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	created, err := f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Equipment issue", IdempotencyKey: uuid.NewString()}, f.cookie, account.AccountID))
	require.NoError(t, err)
	internal, public := true, false
	for _, test := range []struct {
		name         string
		visibility   *bool
		wantInternal bool
	}{
		{"omitted", nil, true}, {"internal", &internal, true}, {"client reply", &public, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := f.maintenance.CreateTicketComment(t.Context(), rpcRequest(&mpb.CreateTicketCommentRequest{TicketId: created.Msg.Detail.Ticket.Id, Text: test.name, IdempotencyKey: uuid.NewString(), Internal: test.visibility}, f.cookie, ""))
			require.NoError(t, err)
			require.Equal(t, test.wantInternal, response.Msg.Comment.Internal)
		})
	}
	visible, err := f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Msg.Detail.Ticket.Id}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, visible.Msg.Detail.Comments, 1)
	require.Equal(t, "client reply", visible.Msg.Detail.Comments[0].Text)
}

func TestHostNoteVisibilityRPCIsReversibleVersionedAndHostScoped(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	created, err := f.client.CreateAccountTicket(t.Context(), rpcRequest(&pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Equipment issue", IdempotencyKey: uuid.NewString()}, f.cookie, account.AccountID))
	require.NoError(t, err)
	ticketID := created.Msg.Detail.Ticket.Id
	note, err := f.maintenance.CreateTicketComment(t.Context(), rpcRequest(&mpb.CreateTicketCommentRequest{TicketId: ticketID, Text: "Host investigation", IdempotencyKey: uuid.NewString()}, f.cookie, ""))
	require.NoError(t, err)
	internal := false
	request := &mpb.SetTicketCommentVisibilityRequest{TicketId: ticketID, CommentId: note.Msg.Comment.Id, Internal: &internal, ExpectedUpdatedAt: created.Msg.Detail.Ticket.UpdatedAt}
	_, err = f.maintenance.SetTicketCommentVisibility(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.Error(t, err, "a selected client workspace cannot mutate host visibility")
	missingInternal := &mpb.SetTicketCommentVisibilityRequest{TicketId: ticketID, CommentId: note.Msg.Comment.Id, ExpectedUpdatedAt: created.Msg.Detail.Ticket.UpdatedAt}
	_, err = f.maintenance.SetTicketCommentVisibility(t.Context(), rpcRequest(missingInternal, f.cookie, ""))
	require.Error(t, err, "visibility must be explicit")
	_, err = f.maintenance.SetTicketCommentVisibility(t.Context(), rpcRequest(request, f.cookie, ""))
	require.NoError(t, err)
	visible, err := f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: ticketID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, visible.Msg.Detail.Comments, 1)
	internal = true
	_, err = f.maintenance.SetTicketCommentVisibility(t.Context(), rpcRequest(&mpb.SetTicketCommentVisibilityRequest{TicketId: ticketID, CommentId: note.Msg.Comment.Id, Internal: &internal, ExpectedUpdatedAt: visible.Msg.Detail.Ticket.UpdatedAt}, f.cookie, ""))
	require.NoError(t, err)
	internal = false
	_, err = f.maintenance.SetTicketCommentVisibility(t.Context(), rpcRequest(request, f.cookie, ""))
	require.Error(t, err, "the old share request cannot undo a later privacy change")
	hidden, err := f.client.GetAccountTicket(t.Context(), rpcRequest(&pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: ticketID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Empty(t, hidden.Msg.Detail.Comments)
}
