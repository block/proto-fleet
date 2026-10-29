package hostedaccounts_test

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestArchivedHostPoolCredentialsRetainAccountBoundaries(t *testing.T) {
	f := setup(t)
	pools, _, _, _, _ := poolFixture(t, f)
	ctx := t.Context()
	q := sqlc.New(f.db)
	password := "retained-pool-password"
	options := poolconfigurations.RevisionOptions{Name: "Host pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "host.worker", Password: &password}}}
	config, err := pools.CreateHost(ctx, f.host, f.org, options)
	require.NoError(t, err)
	host, err := q.GetHostPoolAccount(ctx, f.org)
	require.NoError(t, err)
	rows, err := q.ArchiveHostedAccountPool(ctx, sqlc.ArchiveHostedAccountPoolParams{OrganizationID: f.org, AccountID: host.ID, PoolID: config.PoolID})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	credentials, err := pools.HostGroupCredentials(ctx, q, f.org, config.RevisionID)
	require.NoError(t, err, "archiving preserves the credentials needed by accepted workflows")
	require.Len(t, credentials, 1)
	require.Equal(t, password, credentials[0].Password)
	_, err = q.GetHostMiningGroupPoolRevision(ctx, sqlc.GetHostMiningGroupPoolRevisionParams{OrganizationID: f.org, RevisionID: config.RevisionID})
	require.ErrorIs(t, err, sql.ErrNoRows, "new assignments must still reject the archived configuration")
	_, err = pools.HostGroupCredentials(ctx, q, f.org+1, config.RevisionID)
	require.ErrorIs(t, err, sql.ErrNoRows, "archived revisions remain organization scoped")
	child := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	customerConfig, err := pools.Create(ctx, f.user, f.org, child.AccountID, options)
	require.NoError(t, err)
	_, err = pools.HostGroupCredentials(ctx, q, f.org, customerConfig.RevisionID)
	require.ErrorIs(t, err, sql.ErrNoRows, "customer credentials cannot be used by host mining groups")
	_, err = f.db.ExecContext(ctx, `UPDATE hosted_account SET suspended=TRUE WHERE id=$1`, host.ID)
	require.NoError(t, err)
	_, err = pools.HostGroupCredentials(ctx, q, f.org, config.RevisionID)
	require.ErrorIs(t, err, sql.ErrNoRows, "archiving must not bypass account suspension")
}

func TestHostPoolCreationRecordsCredentialFreeActivity(t *testing.T) {
	f := setup(t)
	pools, _, _, _, ctx := poolFixture(t, f)
	password := uuid.NewString()
	options := poolconfigurations.RevisionOptions{Name: "Host pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://private-pool.example:3333", Username: "private.payout", Password: &password}}}
	config, err := pools.CreateHost(ctx, f.host, f.org, options)
	require.NoError(t, err)
	var actor, username string
	var metadata []byte
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT user_id,username,metadata FROM activity_log WHERE organization_id=$1 AND event_type='create_hosted_account_pool'`, f.org).Scan(&actor, &username, &metadata))
	user, err := sqlc.New(f.db).GetUserById(ctx, f.host)
	require.NoError(t, err)
	require.Equal(t, user.UserID, actor)
	require.Equal(t, user.Username, username)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(metadata, &fields))
	require.Equal(t, config.PoolID, fields["pool_id"])
	require.Equal(t, config.RevisionID, fields["revision_id"])
	require.Equal(t, f.root, fields["account_id"])
	for _, secret := range []string{password, options.Slots[0].URL, options.Slots[0].Username, "ciphertext"} {
		require.NotContains(t, string(metadata), secret)
	}
	_, err = pools.CreateHost(ctx, f.user, f.org, options)
	require.Error(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='create_hosted_account_pool'`, f.org).Scan(&count))
	require.Equal(t, 1, count, "denied creation must not record success")
}

func TestHostPoolCreationRollsBackWhenAuditFails(t *testing.T) {
	f := setup(t)
	pools, _, _, _, ctx := poolFixture(t, f)
	_, err := f.db.ExecContext(ctx, `CREATE FUNCTION reject_host_pool_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='create_hosted_account_pool' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_host_pool_audit BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION reject_host_pool_audit()`)
	require.NoError(t, err)
	_, err = pools.CreateHost(ctx, f.host, f.org, poolconfigurations.RevisionOptions{Name: "Host pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
	require.ErrorContains(t, err, "audit unavailable")
	var poolsCount, revisionsCount int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_pool WHERE organization_id=$1`, f.org).Scan(&poolsCount))
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_pool_revision WHERE organization_id=$1`, f.org).Scan(&revisionsCount))
	require.Zero(t, poolsCount)
	require.Zero(t, revisionsCount)
}
