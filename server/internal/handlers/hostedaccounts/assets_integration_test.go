package hostedaccounts_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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

func TestReplaceHashboardRPCRequiresHostSelection(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.AllCapabilities)
	request := &pb.ReplaceAccountHashboardRequest{AccountId: f.root, OldBoard: &pb.HashboardAllocationSelection{Id: 1, ExpectedRevision: 1}, ReplacementBoard: &pb.HashboardAllocationSelection{Id: 2}}
	for _, selected := range []string{"", account.AccountID} {
		_, err := f.client.ReplaceAccountHashboard(t.Context(), rpcRequest(request, f.cookie, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	_, err := f.client.ReplaceAccountHashboard(t.Context(), rpcRequest(request, f.cookie, f.root))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "the authorized request must reach the board revision checks")
	request.AccountId = account.AccountID
	_, err = f.client.ReplaceAccountHashboard(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "client membership cannot perform an operator replacement")
}

func TestHostedMiningRPCRejectsUnsupportedAssetFiltersAndSort(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.AllCapabilities)
	for _, request := range []*pb.ListAccountAssetsRequest{
		{Sort: &commonpb.SortConfig{Field: commonpb.SortField_SORT_FIELD_IP_ADDRESS}},
		{SiteId: proto.Int64(0)}, {Model: proto.String("")},
		{Model: proto.String("S21"), SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: 1}}},
		{SiteId: proto.Int64(1), SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: 1}}},
		{ReplacementFor: &pb.HashboardAllocationSelection{Id: 1}},
		{HashboardsOnly: true, ReplacementFor: &pb.HashboardAllocationSelection{Id: 1}},
		{HashboardsOnly: true, OwnerAccountId: &account.AccountID, ReplacementFor: &pb.HashboardAllocationSelection{Id: 0}},
		{HashboardsOnly: true, OwnerAccountId: &account.AccountID, ReplacementFor: &pb.HashboardAllocationSelection{Id: 1, ExpectedRevision: -1}},
		{HashboardsOnly: true, OwnerAccountId: &account.AccountID, ReplacementFor: &pb.HashboardAllocationSelection{Id: 1}, SelectedAssets: []*pb.MiningAssetReference{{Kind: 7, Id: 1}}},
	} {
		request.AccountId = account.AccountID
		_, err := f.client.ListAccountAssets(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
}

func TestHostedDashboardAndDetailRPCRequireSelectedAccount(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.AllCapabilities)
	sibling := f.account(t, "Sibling", f.host, domain.AllCapabilities)
	end := time.Now()
	request := &pb.GetAccountDashboardRequest{AccountId: account.AccountID, StartTime: timestamppb.New(end.Add(-time.Hour)), EndTime: timestamppb.New(end), BucketSeconds: 3600}
	for _, selected := range []string{"", sibling.AccountID} {
		_, err := f.client.GetAccountDashboard(t.Context(), rpcRequest(request, f.cookie, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		_, err = f.client.GetAccountAssetDetail(t.Context(), rpcRequest(&pb.GetAccountAssetDetailRequest{AccountId: account.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: 1}}, f.cookie, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	result, err := f.client.GetAccountDashboard(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Zero(t, result.Msg.Totals.AssetCount)
	require.Len(t, result.Msg.Readings, 1)
	_, err = f.client.GetAccountAssetDetail(t.Context(), rpcRequest(&pb.GetAccountAssetDetailRequest{AccountId: account.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: 1}}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	require.NoError(t, f.accounts.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, domain.AllCapabilities, true))
	_, err = f.client.GetAccountDashboard(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
