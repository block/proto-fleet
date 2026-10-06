package hostedaccounts_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"

	"connectrpc.com/authn"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
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

func poolOptions(password *string) hostedaccounts.PoolRevisionOptions {
	return hostedaccounts.PoolRevisionOptions{Name: "  Customer choice  ", Slots: []hostedaccounts.PoolSlotOptions{
		{URL: "stratum+tcp://freely-chosen.example:3333", Username: "payout.with.periods", Password: password, WorkerTemplate: "rig-{miner_id}"},
		{URL: "stratum+ssl://backup.example:443", Username: "backup.payout"},
	}}
}

func TestPoolRevisionsEncryptSecretsAndReplaceBackupsWithoutMutatingHistory(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	pools, _, _, cipher, ctx := poolFixture(t, f)
	password := "pool-only-secret"
	first, err := pools.Create(ctx, f.user, f.org, a.AccountID, poolOptions(&password))
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Number)
	require.Equal(t, "Customer choice", first.Name)
	require.Len(t, first.Slots, 2)
	require.True(t, first.Slots[0].HasPassword)
	require.False(t, first.Slots[1].HasPassword)
	require.Equal(t, int32(1), first.Slots[1].Priority)
	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)
	require.NotContains(t, string(encoded), "ciphertext")
	var slots []byte
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT slots FROM hosted_account_pool_revision WHERE revision_id=$1`, first.RevisionID).Scan(&slots))
	require.NotContains(t, string(slots), password)
	var stored []struct {
		PasswordCiphertext string `json:"password_ciphertext"`
	}
	require.NoError(t, json.Unmarshal(slots, &stored))
	plaintext, err := cipher.Decrypt(stored[0].PasswordCiphertext)
	require.NoError(t, err)
	require.Equal(t, password, string(plaintext))
	plaintext, err = cipher.Decrypt(stored[1].PasswordCiphertext)
	require.NoError(t, err)
	require.Empty(t, plaintext, "empty passwords are encrypted too")
	options := poolOptions(nil)
	options.Slots = options.Slots[:1]
	second, err := pools.Revise(ctx, f.user, f.org, a.AccountID, first.PoolID, first.RevisionID, options)
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Number)
	require.Len(t, second.Slots, 1)
	require.True(t, second.Slots[0].HasPassword)
	var revisedSlots []byte
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT slots FROM hosted_account_pool_revision WHERE revision_id=$1`, second.RevisionID).Scan(&revisedSlots))
	var revised []struct {
		PasswordCiphertext string `json:"password_ciphertext"`
	}
	require.NoError(t, json.Unmarshal(revisedSlots, &revised))
	require.Equal(t, stored[0].PasswordCiphertext, revised[0].PasswordCiphertext)
	old, err := pools.Get(ctx, f.user, f.org, a.AccountID, first.PoolID, first.RevisionID)
	require.NoError(t, err)
	require.Len(t, old.Slots, 2)
	_, err = pools.Revise(ctx, f.user, f.org, a.AccountID, first.PoolID, first.RevisionID, options)
	require.Error(t, err, "stale revisions cannot replace a newer revision")
	empty := ""
	options.Slots[0].Password = &empty
	third, err := pools.Revise(ctx, f.user, f.org, a.AccountID, first.PoolID, second.RevisionID, options)
	require.NoError(t, err)
	require.False(t, third.Slots[0].HasPassword)
	_, err = f.db.ExecContext(ctx, `UPDATE hosted_account_pool_revision SET name='tampered' WHERE revision_id=$1`, first.RevisionID)
	require.Error(t, err, "stored revisions are immutable")
	var auditText string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(metadata::text,' '),'') FROM activity_log WHERE organization_id=$1`, f.org).Scan(&auditText))
	require.NotContains(t, auditText, password)
	require.NotContains(t, auditText, "freely-chosen.example")
}

func TestPoolProfilesRequireRealStepUpAndProtectSelectedConfigurations(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	pools, _, _, _, ctx := poolFixture(t, f)
	first, err := pools.Create(ctx, f.user, f.org, a.AccountID, poolOptions(nil))
	require.NoError(t, err)
	profile, err := pools.IncomingProfile(ctx, f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.Empty(t, profile.RevisionID)
	require.Error(t, pools.SetIncomingProfile(ctx, f.user, f.org, a.AccountID, first.RevisionID, "wrong-password"))
	require.NoError(t, pools.SetIncomingProfile(ctx, f.user, f.org, a.AccountID, first.RevisionID, "actor-password"))
	profile, err = pools.IncomingProfile(ctx, f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.Equal(t, first.RevisionID, profile.RevisionID)
	require.Equal(t, int64(1), profile.Revision)
	require.Error(t, pools.Archive(ctx, f.user, f.org, a.AccountID, first.PoolID))
	require.Error(t, pools.SetIncomingProfile(ctx, f.host, f.org, a.AccountID, "", "actor-password"), "actor must match authenticated identity")
	require.NoError(t, pools.SetIncomingProfile(ctx, f.user, f.org, a.AccountID, "", "actor-password"))
	profile, err = pools.IncomingProfile(ctx, f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.Empty(t, profile.RevisionID)
	require.Equal(t, int64(2), profile.Revision)
	require.NoError(t, pools.Archive(ctx, f.user, f.org, a.AccountID, first.PoolID))
	_, err = pools.Get(ctx, f.user, f.org, a.AccountID, first.PoolID, first.RevisionID)
	require.NoError(t, err)
	_, err = pools.Revise(ctx, f.user, f.org, a.AccountID, first.PoolID, first.RevisionID, poolOptions(nil))
	require.Error(t, err)
	require.Error(t, pools.SetIncomingProfile(ctx, f.user, f.org, a.AccountID, first.RevisionID, "actor-password"))
	page, err := pools.List(ctx, f.user, f.org, a.AccountID, "", 0, false)
	require.NoError(t, err)
	require.Empty(t, page.Configurations)
	page, err = pools.List(ctx, f.user, f.org, a.AccountID, "", 0, true)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 1)
	_, err = f.db.ExecContext(ctx, `DELETE FROM hosted_account WHERE id=$1`, a.ID)
	require.NoError(t, err, "immutable revisions must permit account cleanup cascades")
}

func TestPoolOwnershipPaginationAndLiveKeyRevocation(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Sibling", f.host, hostedaccounts.AllCapabilities)
	pools, keys, credentials, _, ctx := poolFixture(t, f)
	var createdIDs []string
	for range 3 {
		created, err := pools.Create(ctx, f.user, f.org, a.AccountID, poolOptions(nil))
		require.NoError(t, err)
		createdIDs = append(createdIDs, created.PoolID)
	}
	page, err := pools.List(ctx, f.user, f.org, a.AccountID, "", 2, false)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 2)
	require.NotEmpty(t, page.NextPageToken)
	next, err := pools.List(ctx, f.user, f.org, a.AccountID, page.NextPageToken, 2, false)
	require.NoError(t, err)
	require.Len(t, next.Configurations, 1)
	require.ElementsMatch(t, createdIDs, []string{page.Configurations[0].PoolID, page.Configurations[1].PoolID, next.Configurations[0].PoolID})
	require.Empty(t, next.NextPageToken)
	_, err = pools.List(ctx, f.host, f.org, b.AccountID, page.NextPageToken, 2, false)
	require.Error(t, err)
	_, err = pools.List(ctx, f.user, f.org, a.AccountID, page.NextPageToken, 2, true)
	require.Error(t, err)
	_, err = pools.Get(ctx, f.host, f.org, b.AccountID, page.Configurations[0].PoolID, "")
	require.Error(t, err)
	_, err = pools.Create(ctx, f.user, f.org, b.AccountID, poolOptions(nil))
	require.Error(t, err)
	issued, err := keys.Create(ctx, f.user, f.org, a.AccountID, hostedaccounts.KeyOptions{Name: "Pool writer", Capabilities: hostedaccounts.MachineCapabilities})
	require.NoError(t, err)
	record, err := credentials.Validate(ctx, issued.Secret)
	require.NoError(t, err)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	machineInfo := *info
	machineInfo.AuthMethod, machineInfo.AccountKey, machineInfo.APIKeyDatabaseID = session.AuthMethodAPIKey, true, record.ID
	machineCtx, err := hostedaccounts.WithMachineScope(authn.SetInfo(ctx, &machineInfo), a.AccountID, hostedaccounts.MachineCapabilities)
	require.NoError(t, err)
	_, err = pools.Create(machineCtx, f.user, f.org, a.AccountID, poolOptions(nil))
	require.NoError(t, err)
	require.NoError(t, keys.Revoke(ctx, f.user, f.org, a.AccountID, issued.Key.KeyID))
	_, err = pools.Create(machineCtx, f.user, f.org, a.AccountID, poolOptions(nil))
	require.Error(t, err, "captured scope does not survive live credential revocation")
	require.Error(t, pools.Archive(machineCtx, f.user, f.org, a.AccountID, page.Configurations[0].PoolID))
	require.Error(t, pools.SetIncomingProfile(machineCtx, f.user, f.org, a.AccountID, page.Configurations[0].RevisionID, "actor-password"))
	require.NoError(t, f.svc.SetMembership(ctx, f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleViewer))
	_, err = pools.Create(ctx, f.host, f.org, a.AccountID, poolOptions(nil))
	require.Error(t, err)
	_, err = pools.Get(ctx, f.host, f.org, a.AccountID, page.Configurations[0].PoolID, "")
	require.NoError(t, err)
}

func TestPoolRevisionRaceHasOneWinner(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	pools, _, _, _, ctx := poolFixture(t, f)
	first, err := pools.Create(ctx, f.user, f.org, a.AccountID, poolOptions(nil))
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := pools.Revise(ctx, f.user, f.org, a.AccountID, first.PoolID, first.RevisionID, poolOptions(nil))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		} else {
			require.True(t, fleeterror.IsFailedPreconditionError(err), "the losing revision must report an optimistic conflict: %v", err)
		}
	}
	require.Equal(t, 1, winners)
	latest, err := pools.Get(ctx, f.user, f.org, a.AccountID, first.PoolID, "")
	require.NoError(t, err)
	require.Equal(t, int64(2), latest.Number)
}

func TestPoolValidationRejectsCredentialsAndInvalidDestinationsWithoutEchoingSecrets(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	pools, _, _, _, ctx := poolFixture(t, f)
	for _, url := range []string{"https://pool.example", "stratum+tcp://pool.example:0", "stratum+tcp://pool.example:65536", "stratum+tcp://embedded-secret@pool.example", "stratum+tcp://pool.example/path", "stratum+tcp://pool.example?secret=embedded-secret", "stratum+tcp://pool.example#embedded-secret", "stratum2+tcp://pool.example:3333"} {
		t.Run(url, func(t *testing.T) {
			options := poolOptions(nil)
			options.Slots[0].URL = url
			_, err := pools.Create(ctx, f.user, f.org, a.AccountID, options)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "embedded-secret")
		})
	}
	for _, template := range []string{"bad {unknown}", "{miner_id", "miner}", "space worker"} {
		options := poolOptions(nil)
		options.Slots[0].WorkerTemplate = template
		_, err := pools.Create(ctx, f.user, f.org, a.AccountID, options)
		require.Error(t, err)
	}
	options := poolOptions(nil)
	options.Slots[0].Username = "payout with space"
	_, err := pools.Create(ctx, f.user, f.org, a.AccountID, options)
	require.Error(t, err)
	secret := "embedded-secret\n"
	options = poolOptions(&secret)
	_, err = pools.Create(ctx, f.user, f.org, a.AccountID, options)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "embedded-secret")
}

func TestPoolStorageRejectsCrossAccountOwnershipAndDirectRevisionDeletion(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Other", f.user, hostedaccounts.AllCapabilities)
	pools, _, _, _, ctx := poolFixture(t, f)
	first, err := pools.Create(ctx, f.user, f.org, a.AccountID, poolOptions(nil))
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) SELECT $1,organization_id,$2,pool_id,2,name,slots FROM hosted_account_pool_revision WHERE revision_id=$3`, uuid.NewString(), b.ID, first.RevisionID)
	require.Error(t, err, "revision owner must match configuration owner")
	_, err = f.db.ExecContext(ctx, `INSERT INTO hosted_account_pool_profile(organization_id,account_id,revision_id) VALUES($1,$2,$3)`, f.org, b.ID, first.RevisionID)
	require.Error(t, err, "profile revision must belong to the same account")
	_, err = f.db.ExecContext(ctx, `DELETE FROM hosted_account_pool_revision WHERE revision_id=$1`, first.RevisionID)
	require.Error(t, err, "a live parent protects direct revision deletion")
	var foreignOrg int64
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Foreign') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	_, err = f.db.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, uuid.NewString(), foreignOrg, a.ID)
	require.Error(t, err, "configuration owner must belong to the same organization")
	_, err = pools.Get(ctx, f.user, foreignOrg, a.AccountID, first.PoolID, "")
	require.Error(t, err)
}

func TestPoolCeilingsAndExplicitAncestorGrantsStayLive(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Private child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	pools, _, _, _, ctx := poolFixture(t, f)
	_, err = pools.Create(ctx, f.user, f.org, child.AccountID, poolOptions(nil))
	require.Error(t, err, "ancestor administration is not private account access")
	require.NoError(t, f.svc.SetAncestorGrant(ctx, f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.View|hostedaccounts.ManagePools))
	created, err := pools.Create(ctx, f.user, f.org, child.AccountID, poolOptions(nil))
	require.NoError(t, err)
	require.Error(t, pools.SetIncomingProfile(ctx, f.user, f.org, child.AccountID, created.RevisionID, "actor-password"), "manage pools does not imply change pools")
	require.NoError(t, f.svc.ConfigureChild(ctx, f.host, f.org, parent.AccountID, hostedaccounts.View|hostedaccounts.ManageMembers|hostedaccounts.Delegate, false))
	_, err = pools.Revise(ctx, f.user, f.org, child.AccountID, created.PoolID, created.RevisionID, poolOptions(nil))
	require.Error(t, err, "live ancestor ceiling removes pool writing")
	_, err = pools.Get(ctx, f.user, f.org, child.AccountID, created.PoolID, "")
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureChild(ctx, f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities, true))
	_, err = pools.List(ctx, f.user, f.org, child.AccountID, "", 0, false)
	require.Error(t, err, "suspension blocks reads")
}

type rotatingPoolVerifier struct {
	hostedaccounts.CredentialsVerifier
	rotate func() error
}

func (v rotatingPoolVerifier) VerifySessionCredentials(ctx context.Context, username, password string) error {
	if err := v.CredentialsVerifier.VerifySessionCredentials(ctx, username, password); err != nil {
		return err
	}
	return v.rotate()
}

func TestPoolProfileRejectsPasswordResetAfterStepUpVerification(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	pools, keys, _, cipher, ctx := poolFixture(t, f)
	first, err := pools.Create(ctx, f.user, f.org, a.AccountID, poolOptions(nil))
	require.NoError(t, err)
	store := sqlstores.NewSQLUserStore(f.db)
	verifier := domainauth.NewService(store, store, sqlstores.NewSQLTransactor(f.db), nil, nil, nil, nil, authz.NewPermissionResolver(f.db))
	newHash, err := bcrypt.GenerateFromPassword([]byte("replacement-password"), bcrypt.MinCost)
	require.NoError(t, err)
	rotating := rotatingPoolVerifier{verifier, func() error {
		_, err := f.db.ExecContext(ctx, `UPDATE "user" SET password_hash=$1,password_updated_at=clock_timestamp() WHERE id=$2`, string(newHash), f.user)
		require.NoError(t, err)
		return nil
	}}
	pools = hostedaccounts.NewPoolService(f.svc, cipher, keys, rotating)
	require.Error(t, pools.SetIncomingProfile(ctx, f.user, f.org, a.AccountID, first.RevisionID, "actor-password"), "a reset after successful verification invalidates its proof")
	profile, err := pools.IncomingProfile(ctx, f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.Zero(t, profile.Revision, "rejected step-up must not change the profile witness")
	var successful int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_log WHERE organization_id=$1 AND event_type='set_hosted_account_pool_profile'`, f.org).Scan(&successful))
	require.Zero(t, successful)
}
