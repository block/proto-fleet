package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestResolveHostFleetPoolsPreservesOrderCredentialsAndRevisionIdentity(t *testing.T) {
	f := setup(t)
	pools, _, _, cipher, _ := poolFixture(t, f)
	q := sqlc.New(f.db)
	ctx := t.Context()
	password, err := cipher.Encrypt([]byte("pool-secret"))
	require.NoError(t, err)
	makePool := func(name, url string, orgID int64) int64 {
		t.Helper()
		id, err := q.CreatePool(ctx, sqlc.CreatePoolParams{OrgID: orgID, PoolName: name, Url: url, Username: "worker", PasswordEnc: password, CreatedAt: time.Now()})
		require.NoError(t, err)
		return id
	}
	primary := makePool("Primary", "stratum+tcp://primary.example:3333", f.org)
	backup := makePool("Backup", "stratum+tcp://backup.example:3333", f.org)
	first, err := pools.ResolveHostFleetPools(ctx, f.host, f.org, []int64{primary, backup})
	require.NoError(t, err)
	credentials, err := pools.HostGroupCredentials(ctx, q, f.org, first.RevisionID)
	require.NoError(t, err)
	require.Len(t, credentials, 2)
	require.Equal(t, "stratum+tcp://primary.example:3333", credentials[0].URL)
	require.Equal(t, "stratum+tcp://backup.example:3333", credentials[1].URL)
	require.Equal(t, "pool-secret", credentials[0].Password)
	repeated, err := pools.ResolveHostFleetPools(ctx, f.host, f.org, []int64{primary, backup})
	require.NoError(t, err)
	require.Equal(t, first.RevisionID, repeated.RevisionID)
	reversed, err := pools.ResolveHostFleetPools(ctx, f.host, f.org, []int64{backup, primary})
	require.NoError(t, err)
	require.NotEqual(t, first.RevisionID, reversed.RevisionID)
	newPassword, err := cipher.Encrypt([]byte("new-secret"))
	require.NoError(t, err)
	require.NoError(t, q.UpdatePool(ctx, sqlc.UpdatePoolParams{OrgID: f.org, ID: primary, PoolName: "Primary", Url: credentials[0].URL, Username: "worker", PasswordEnc: newPassword, UpdatedAt: time.Now()}))
	revised, err := pools.ResolveHostFleetPools(ctx, f.host, f.org, []int64{primary, backup})
	require.NoError(t, err)
	require.NotEqual(t, first.RevisionID, revised.RevisionID)
	original, err := pools.HostGroupCredentials(ctx, q, f.org, first.RevisionID)
	require.NoError(t, err)
	require.Equal(t, "pool-secret", original[0].Password)
	current, err := pools.HostGroupCredentials(ctx, q, f.org, revised.RevisionID)
	require.NoError(t, err)
	require.Equal(t, "new-secret", current[0].Password)
	for _, ids := range [][]int64{nil, {primary, primary}, {primary, backup, primary, backup}, {primary, -1}, {primary, 999999999}} {
		_, err := pools.ResolveHostFleetPools(ctx, f.host, f.org, ids)
		require.Error(t, err)
	}
	_, err = pools.ResolveHostFleetPools(ctx, f.user, f.org, []int64{primary})
	require.Error(t, err, "customer account membership does not grant host pool read access")
	unsupported := makePool("TLS", "stratum+ssl://tls.example:3333", f.org)
	_, err = pools.ResolveHostFleetPools(ctx, f.host, f.org, []int64{unsupported})
	require.Error(t, err)
	require.NoError(t, q.SoftDeletePool(ctx, sqlc.SoftDeletePoolParams{OrgID: f.org, ID: primary}))
	_, err = pools.ResolveHostFleetPools(ctx, f.host, f.org, []int64{primary})
	require.Error(t, err, "deleted pools cannot be selected for a new assignment")
}
