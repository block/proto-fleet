package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedMiningRPCRequiresSelectedMatchingAccountAndSession(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.AllCapabilities)
	sibling := f.account(t, "Sibling", f.host, domain.AllCapabilities)
	// The interceptor requires active selection before the handler runs.
	_, err := f.client.GetAccountMiningSummary(t.Context(), rpcRequest(&pb.GetAccountMiningSummaryRequest{AccountId: account.AccountID}, f.cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.GetAccountMiningSummary(t.Context(), rpcRequest(&pb.GetAccountMiningSummaryRequest{AccountId: account.AccountID}, f.cookie, sibling.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.ListAccountAssets(t.Context(), rpcRequest(&pb.ListAccountAssetsRequest{AccountId: account.AccountID}, f.cookie, sibling.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.AssignAccountAssets(t.Context(), rpcRequest(&pb.AssignAccountAssetsRequest{AccountId: account.AccountID, ChildAccountId: sibling.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{{Kind: 5, Id: 1}}}, f.cookie, sibling.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	response, err := f.client.GetAccountMiningSummary(t.Context(), rpcRequest(&pb.GetAccountMiningSummaryRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Zero(t, response.Msg.Totals.AssetCount)
	// Membership revocation applies on the next call; host status does not restore it.
	user := f.addUser(t, "viewer-mining", false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, account.AccountID, user, domain.RoleViewer))
	cookie := f.login(t, "viewer-mining").Header().Get("Set-Cookie")
	response, err = f.client.GetAccountMiningSummary(t.Context(), rpcRequest(&pb.GetAccountMiningSummaryRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Nil(t, response.Msg.Direct)
	require.Empty(t, response.Msg.Children)
	require.NoError(t, f.accounts.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, user))
	_, err = f.client.GetAccountMiningSummary(t.Context(), rpcRequest(&pb.GetAccountMiningSummaryRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
