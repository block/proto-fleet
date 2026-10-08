package hostedaccounts_test

import (
	"bytes"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func mustProtoJSON(t *testing.T, message proto.Message) []byte {
	t.Helper()
	encoded, err := protojson.Marshal(message)
	require.NoError(t, err)
	return encoded
}

func TestHostedRPCInvitationSecretsStayOutOfLogsAndLaterResponses(t *testing.T) {
	// No parallel execution: the production request logger uses slog.Default.
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	issued := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	preview, err := f.client.PreviewInvitation(t.Context(), connect.NewRequest(&pb.PreviewInvitationRequest{Token: issued.Token}))
	require.NoError(t, err)
	registered := f.register(t, issued.Token, "redaction-customer")
	listed, err := f.client.ListInvitations(t.Context(), rpcRequest(&pb.ListInvitationsRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	var tokenHash string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT token_hash FROM hosted_account_invitation WHERE invitation_id=$1`, issued.Invitation.InvitationId).Scan(&tokenHash))
	require.NotEqual(t, issued.Token, tokenHash)
	for _, response := range []struct {
		name string
		json []byte
	}{
		{"preview", mustProtoJSON(t, preview.Msg)},
		{"registration", mustProtoJSON(t, registered.Msg)},
		{"invitation list", mustProtoJSON(t, listed.Msg)},
	} {
		require.NotContains(t, string(response.json), issued.Token, response.name)
		require.NotContains(t, string(response.json), tokenHash, response.name)
		require.NotContains(t, string(response.json), rpcPassword, response.name)
	}
	_, err = f.client.PreviewInvitation(t.Context(), connect.NewRequest(&pb.PreviewInvitationRequest{Token: issued.Token}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	logs := output.String()
	require.Contains(t, logs, hostedv1connect.HostedAccountServiceCreateInvitationProcedure, "debug logging must actually be active")
	require.Contains(t, logs, hostedv1connect.HostedAccountServiceRegisterInvitedUserProcedure)
	require.Contains(t, logs, "incoming unary request failed", "failed invitation requests exercise the error log path")
	require.NotContains(t, logs, issued.Token)
	require.NotContains(t, logs, tokenHash)
	require.NotContains(t, logs, rpcPassword)
	var activity string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(string_agg(row_to_json(a)::text,E'\n'),'') FROM activity_log a WHERE organization_id=$1`, f.org).Scan(&activity))
	require.Contains(t, activity, "create_hosted_account_invitation")
	require.Contains(t, activity, "accept_hosted_account_invitation")
	require.NotContains(t, activity, issued.Token)
	require.NotContains(t, activity, tokenHash)
	require.NotContains(t, activity, rpcPassword)
}
