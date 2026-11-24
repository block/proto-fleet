package hostedaccounts_test

import (
	"context"
	"encoding/base64"
	"testing"

	"connectrpc.com/authn"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

func poolFixture(t *testing.T, f fixture) (*hostedaccounts.PoolService, *hostedaccounts.KeyService, *apikey.Service, *encrypt.Service, context.Context) {
	t.Helper()
	cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	store := sqlstores.NewSQLUserStore(f.db)
	verifier := domainauth.NewService(store, store, sqlstores.NewSQLTransactor(f.db), nil, nil, nil, nil, authz.NewPermissionResolver(f.db))
	credentials := apikey.NewService(sqlstores.NewSQLApiKeyStore(f.db), nil)
	keys := hostedaccounts.NewKeyService(f.svc, credentials)
	hash, err := bcrypt.GenerateFromPassword([]byte("actor-password"), bcrypt.MinCost)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE "user" SET password_hash=$1 WHERE id=$2`, string(hash), f.user)
	require.NoError(t, err)
	var username, external string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT username,user_id FROM "user" WHERE id=$1`, f.user).Scan(&username, &external))
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.user, OrganizationID: f.org, Username: username, ExternalUserID: external, AuthMethod: session.AuthMethodSession})
	return hostedaccounts.NewPoolService(f.svc, cipher, keys, verifier), keys, credentials, cipher, ctx
}

func poolOptions(password *string) poolconfigurations.RevisionOptions {
	return poolconfigurations.RevisionOptions{Name: "  Customer choice  ", Slots: []poolconfigurations.SlotOptions{
		{URL: "stratum+tcp://freely-chosen.example:3333", Username: "payout.with.periods", Password: password, WorkerTemplate: "rig-{miner_id}"},
		{URL: "stratum+ssl://backup.example:443", Username: "backup.payout"},
	}}
}

// seedLegacyPoolRevision represents durable pre-cutover records used by recovery
// tests. New configurations are created through poolconfigurations.Service.
func seedLegacyPoolRevision(t *testing.T, ctx context.Context, q sqlc.Querier, cipher *encrypt.Service, orgID, accountID int64, options poolconfigurations.RevisionOptions) sqlc.HostedAccountPoolRevision {
	t.Helper()
	pool, err := q.CreateHostedAccountPool(ctx, sqlc.CreateHostedAccountPoolParams{PoolID: uuid.NewString(), OrganizationID: orgID, AccountID: accountID})
	require.NoError(t, err)
	slots, err := poolconfigurations.EncryptSlots(cipher, options.Slots, nil)
	require.NoError(t, err)
	revision, err := q.CreateHostedAccountPoolRevision(ctx, sqlc.CreateHostedAccountPoolRevisionParams{RevisionID: uuid.NewString(), OrganizationID: orgID, AccountID: accountID, PoolID: pool.PoolID, Number: 1, Name: options.Name, Slots: slots})
	require.NoError(t, err)
	return revision
}

func TestPoolStorageRejectsCrossAccountOwnershipAndDirectRevisionDeletion(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Other", f.user, hostedaccounts.AllCapabilities)
	_, _, _, cipher, ctx := poolFixture(t, f)
	first := seedLegacyPoolRevision(t, ctx, sqlc.New(f.db), cipher, f.org, a.ID, poolOptions(nil))
	_, err := f.db.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) SELECT $1,organization_id,$2,pool_id,2,name,slots FROM hosted_account_pool_revision WHERE revision_id=$3`, uuid.NewString(), b.ID, first.RevisionID)
	require.Error(t, err, "revision owner must match configuration owner")
	_, err = f.db.ExecContext(ctx, `INSERT INTO hosted_account_pool_profile(organization_id,account_id,revision_id) VALUES($1,$2,$3)`, f.org, b.ID, first.RevisionID)
	require.Error(t, err, "profile revision must belong to the same account")
	_, err = f.db.ExecContext(ctx, `DELETE FROM hosted_account_pool_revision WHERE revision_id=$1`, first.RevisionID)
	require.Error(t, err, "a live parent protects direct revision deletion")
	var foreignOrg int64
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Foreign') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	_, err = f.db.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, uuid.NewString(), foreignOrg, a.ID)
	require.Error(t, err, "configuration owner must belong to the same organization")
}
