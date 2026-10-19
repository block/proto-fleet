package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedRPCCreateAccountRollsBackWhenCreatorAccessCannotResolve(t *testing.T) {
	f := newRPCFixture(t)
	// Make the authority lookup fail after the creation writes, while keeping
	// those writes valid. The RPC must roll back the account and its audit event.
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION suspend_new_account() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN
	 UPDATE hosted_account SET suspended=true WHERE account_id=NEW.metadata->>'account_id';
	 RETURN NEW;
	END $$;
	CREATE TRIGGER suspend_new_account AFTER INSERT ON activity_log
	FOR EACH ROW WHEN (NEW.event_type='create_hosted_account') EXECUTE FUNCTION suspend_new_account()`)
	require.NoError(t, err)
	request := &pb.CreateAccountRequest{AccountId: f.root, Name: "Atomic client", Capabilities: uint64(domain.AllCapabilities)}
	_, err = f.client.CreateAccount(t.Context(), rpcRequest(request, f.cookie, f.root))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	var accounts, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account WHERE organization_id=$1 AND name=$2`, f.org, request.Name).Scan(&accounts))
	require.Zero(t, accounts, "a failed creation must not leave an account that a retry duplicates")
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='create_hosted_account' AND organization_id=$1`, f.org).Scan(&audits))
	require.Zero(t, audits)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER suspend_new_account ON activity_log; DROP FUNCTION suspend_new_account()`)
	require.NoError(t, err)
	created, err := f.client.CreateAccount(t.Context(), rpcRequest(request, f.cookie, f.root))
	require.NoError(t, err)
	require.Equal(t, uint64(domain.AllCapabilities), created.Msg.Account.EffectiveCapabilities)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account WHERE organization_id=$1 AND name=$2`, f.org, request.Name).Scan(&accounts))
	require.Equal(t, 1, accounts)
}
