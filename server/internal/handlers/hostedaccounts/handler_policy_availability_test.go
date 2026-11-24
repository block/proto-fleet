package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestHostedRPCSitePolicySelectionDoesNotDependOnOperation(t *testing.T) {
	f := newRPCFixture(t)
	username := "parent-" + uuid.NewString()
	actor := f.addUser(t, username, false)
	parent := f.account(t, "Parent", actor, domain.AllCapabilities)
	child, err := f.accounts.CreateChild(t.Context(), actor, f.org, parent.AccountID, "Child", f.host, domain.AllCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, child.AccountID, actor, domain.RoleViewer))
	cookie := f.login(t, username).Header().Get("Set-Cookie")
	read := func(selected string) *pb.GetSitePolicyResponse {
		t.Helper()
		response, err := f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: child.AccountID}, cookie, selected))
		require.NoError(t, err)
		return response.Msg
	}
	before := read("")
	require.False(t, before.SitePolicy.Restricted)
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: child.AccountID, SitePolicy: &pb.SitePolicy{Restricted: true}}, cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	after := read("")
	require.False(t, after.SitePolicy.Restricted)
	require.False(t, after.EffectiveRestricted)
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: proto.String(child.AccountID), SitePolicy: &pb.SitePolicy{Restricted: true}}, cookie, parent.AccountID))
	require.NoError(t, err)
	response, err := f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: proto.String(child.AccountID)}, cookie, parent.AccountID))
	require.NoError(t, err)
	require.True(t, response.Msg.SitePolicy.Restricted)
	own := read("")
	require.False(t, own.SitePolicy.Restricted)
	require.True(t, own.EffectiveRestricted)
}

func TestHostedRPCInvitationListingReportsEffectiveAvailability(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.AllCapabilities)
	issued := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	require.True(t, issued.Invitation.Available)
	list := func() *pb.Invitation {
		t.Helper()
		response, err := f.client.ListInvitations(t.Context(), rpcRequest(&pb.ListInvitationsRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
		require.NoError(t, err)
		require.Len(t, response.Msg.Invitations, 1)
		return response.Msg.Invitations[0]
	}
	require.True(t, list().Available)
	require.NoError(t, f.accounts.ConfigureSites(t.Context(), f.host, f.org, f.root, false, nil))
	row := list()
	require.False(t, row.Available)
	require.Nil(t, row.RevokedAt)
	require.Nil(t, row.AcceptedAt)
}
