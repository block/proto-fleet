package hostedaccounts_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	activitypb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedActivityRPCScopeExportAndLiveAccess(t *testing.T) {
	f := newRPCFixture(t)
	a := f.account(t, "Customer A", f.host, domain.AllCapabilities)
	b := f.account(t, "Sibling secret", f.host, domain.AllCapabilities)
	viewer := f.addUser(t, "activity-viewer", false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, a.AccountID, viewer, domain.RoleViewer))
	cookie := f.login(t, "activity-viewer").Header().Get("Set-Cookie")
	request := &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 1}
	for _, selected := range []string{"", b.AccountID} {
		_, err := f.client.ListAccountActivities(t.Context(), rpcRequest(request, cookie, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	page, err := f.client.ListAccountActivities(t.Context(), rpcRequest(request, cookie, a.AccountID))
	require.NoError(t, err)
	require.NotEmpty(t, page.Msg.Activities)
	request.Filter = &activitypb.ActivityFilter{SearchText: "Sibling secret"}
	page, err = f.client.ListAccountActivities(t.Context(), rpcRequest(request, cookie, a.AccountID))
	require.NoError(t, err)
	require.Zero(t, page.Msg.TotalCount, "search cannot inspect excluded metadata or sibling rows")
	request.Filter = &activitypb.ActivityFilter{UserIds: []string{"hidden-actor"}}
	_, err = f.client.ListAccountActivities(t.Context(), rpcRequest(request, cookie, a.AccountID))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	stream, err := f.client.ExportAccountActivities(t.Context(), rpcRequest(&pb.ExportAccountActivitiesRequest{AccountId: a.AccountID}, cookie, a.AccountID))
	require.NoError(t, err)
	var exported strings.Builder
	for stream.Receive() {
		exported.Write(stream.Msg().Chunk)
	}
	require.NoError(t, stream.Err())
	require.Contains(t, exported.String(), "Created account")
	require.NotContains(t, exported.String(), "Sibling secret")
	require.NotContains(t, exported.String(), "activity-viewer")
	assertExportDenied := func(account string) {
		t.Helper()
		denied, err := f.client.ExportAccountActivities(t.Context(), rpcRequest(&pb.ExportAccountActivitiesRequest{AccountId: account}, cookie, account))
		require.NoError(t, err)
		require.False(t, denied.Receive())
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(denied.Err()))
	}
	assertExportDenied(b.AccountID)
	require.NoError(t, f.accounts.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, viewer))
	request.Filter = nil
	_, err = f.client.ListAccountActivities(t.Context(), rpcRequest(request, cookie, a.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assertExportDenied(a.AccountID)
}
