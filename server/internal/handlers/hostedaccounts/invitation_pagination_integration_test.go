package hostedaccounts_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestListInvitationsRPCPagesAndValidatesContinuation(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	for range 3 {
		_, err := f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: account.AccountID, Role: pb.AccountRole_ACCOUNT_ROLE_VIEWER}, f.cookie, account.AccountID))
		require.NoError(t, err)
	}
	first, err := f.client.ListInvitations(t.Context(), rpcRequest(&pb.ListInvitationsRequest{AccountId: account.AccountID, PageSize: 2}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, first.Msg.Invitations, 2)
	require.NotEmpty(t, first.Msg.NextPageToken)
	last, err := f.client.ListInvitations(t.Context(), rpcRequest(&pb.ListInvitationsRequest{AccountId: account.AccountID, PageSize: 2, PageToken: first.Msg.NextPageToken}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, last.Msg.Invitations, 1)
	require.Empty(t, last.Msg.NextPageToken)
	for _, row := range first.Msg.Invitations {
		require.NotEqual(t, row.InvitationId, last.Msg.Invitations[0].InvitationId)
	}
	other := f.account(t, "Other", f.host, hostedaccounts.DefaultCapabilities)
	for _, request := range []*pb.ListInvitationsRequest{
		{AccountId: other.AccountID, PageToken: first.Msg.NextPageToken},
		{AccountId: account.AccountID, PageSize: 101},
		{AccountId: account.AccountID, PageSize: -1},
		{AccountId: account.AccountID, PageToken: "invalid token!"},
		{AccountId: account.AccountID, PageToken: strings.Repeat("a", 513)},
	} {
		_, err := f.client.ListInvitations(t.Context(), rpcRequest(request, f.cookie, request.AccountId))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
}
