package hostedaccounts_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/stretchr/testify/require"
)

func TestArchivedHostPoolCredentialsRetainAccountBoundaries(t *testing.T) {
	f := setup(t)
	pools, _, _, cipher, _ := poolFixture(t, f)
	ctx := t.Context()
	q := sqlc.New(f.db)
	password := "retained-pool-password"
	options := poolconfigurations.RevisionOptions{Name: "Host pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "host.worker", Password: &password}}}
	host, err := q.GetHostPoolAccount(ctx, f.org)
	require.NoError(t, err)
	config := seedLegacyPoolRevision(t, ctx, q, cipher, f.org, host.ID, options)
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
	customerConfig := seedLegacyPoolRevision(t, ctx, q, cipher, f.org, child.ID, options)
	_, err = pools.HostGroupCredentials(ctx, q, f.org, customerConfig.RevisionID)
	require.ErrorIs(t, err, sql.ErrNoRows, "customer credentials cannot be used by host mining groups")
	_, err = f.db.ExecContext(ctx, `UPDATE hosted_account SET suspended=TRUE WHERE id=$1`, host.ID)
	require.NoError(t, err)
	_, err = pools.HostGroupCredentials(ctx, q, f.org, config.RevisionID)
	require.ErrorIs(t, err, sql.ErrNoRows, "archiving must not bypass account suspension")
}
