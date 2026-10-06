package hostedaccounts_test

import (
	"bytes"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func keyRequest[T any](message *T, secret, accountID string) *connect.Request[T] {
	request := rpcRequest(message, "", accountID)
	request.Header().Set("Authorization", "Bearer "+secret)
	return request
}

func TestAccountKeyRPCScopeCannotBorrowIssuerHostOrSiblingAccess(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	sibling := f.account(t, "Sibling", f.host, domain.DefaultCapabilities)
	created, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Read only", Capabilities: 1}, f.cookie, account.AccountID))
	require.NoError(t, err)
	secret := created.Msg.Secret
	result, err := f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, secret, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Msg.Account.EffectiveCapabilities)
	_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, secret, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: sibling.AccountID}, secret, sibling.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: sibling.AccountID}, secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.ListMembers(t.Context(), keyRequest(&pb.ListMembersRequest{AccountId: account.AccountID}, secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.CreateAccountKey(t.Context(), keyRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Replicated", Capabilities: 1}, secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.ListAccounts(t.Context(), keyRequest(&pb.ListAccountsRequest{}, secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	organizationKey, _, err := f.keys.Create(t.Context(), f.host, f.org, "host", "host", "Organization", nil)
	require.NoError(t, err)
	for _, selected := range []string{"", account.AccountID} {
		_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, organizationKey, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	_, err = f.client.RevokeAccountKey(t.Context(), rpcRequest(&pb.RevokeAccountKeyRequest{AccountId: account.AccountID, KeyId: created.Msg.Key.KeyId}, f.cookie, account.AccountID))
	require.NoError(t, err)
	_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, secret, account.AccountID))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestAccountKeySecretIsShownOnceAndRedactedFromLogsAndActivity(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	created, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Automation", Capabilities: 7}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.NotEmpty(t, created.Msg.Secret)
	listed, err := f.client.ListAccountKeys(t.Context(), rpcRequest(&pb.ListAccountKeysRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, listed.Msg.Keys, 1)
	require.NotContains(t, string(mustProtoJSON(t, listed.Msg)), created.Msg.Secret)
	var hash, activity string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT key_hash FROM api_key WHERE key_id=$1`, created.Msg.Key.KeyId).Scan(&hash))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(string_agg(row_to_json(a)::text,E'\n'),'') FROM activity_log a WHERE organization_id=$1`, f.org).Scan(&activity))
	require.Contains(t, output.String(), "CreateAccountKey")
	require.NotContains(t, output.String(), created.Msg.Secret)
	require.NotContains(t, output.String(), hash)
	require.Contains(t, activity, "create_hosted_account_key")
	require.NotContains(t, activity, created.Msg.Secret)
	require.NotContains(t, activity, hash)
}

func TestCustomerOnlyKeyLosesAccessImmediatelyWithMembership(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	invitation := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	accepted := f.register(t, invitation.Token, "customer-key-user")
	cookie := accepted.Header().Get("Set-Cookie")
	issued, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Personal", Capabilities: 1}, cookie, account.AccountID))
	require.NoError(t, err)
	_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, issued.Msg.Secret, account.AccountID))
	require.NoError(t, err)
	var userID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM "user" WHERE username='customer-key-user'`).Scan(&userID))
	require.NoError(t, f.accounts.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, userID))
	_, err = f.client.GetAccount(t.Context(), keyRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, issued.Msg.Secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
