package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedClientManagementSummaryRPC(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Client", f.host, domain.AllCapabilities)
	viewer := f.addUser(t, "client-summary-viewer", false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, account.AccountID, viewer, domain.RoleViewer))
	response, err := f.client.ListChildren(t.Context(), rpcRequest(&pb.ListChildrenRequest{AccountId: f.root}, f.cookie, f.root))
	require.NoError(t, err)
	require.Len(t, response.Msg.Accounts, 1)
	counts := response.Msg.Accounts[0].ManagementSummary
	require.NotNil(t, counts)
	require.EqualValues(t, 2, counts.LoginCount)
	require.Zero(t, counts.MinerCount)
	require.Zero(t, counts.ModuleCount)
	require.Zero(t, counts.ContainerCount)
	cookie := f.login(t, "client-summary-viewer").Header().Get("Set-Cookie")
	_, err = f.client.ListChildren(t.Context(), rpcRequest(&pb.ListChildrenRequest{AccountId: f.root}, cookie, f.root))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	directory, err := f.client.ListAccounts(t.Context(), rpcRequest(&pb.ListAccountsRequest{}, cookie, ""))
	require.NoError(t, err)
	require.Len(t, directory.Msg.Accounts, 1)
	require.Nil(t, directory.Msg.Accounts[0].ManagementSummary)
}
