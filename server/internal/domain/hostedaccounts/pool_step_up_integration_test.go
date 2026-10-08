package hostedaccounts_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

type recordingPoolVerifier struct {
	real              *domainauth.Service
	calls             int
	afterVerification func() error
}

func (v *recordingPoolVerifier) VerifySessionCredentials(ctx context.Context, username, password string) error {
	v.calls++
	if err := v.real.VerifySessionCredentials(ctx, username, password); err != nil {
		return fmt.Errorf("verify pool fixture credentials: %w", err)
	}
	if v.afterVerification != nil {
		return v.afterVerification()
	}
	return nil
}

type poolStepUpFixture struct {
	fixture
	account     hostedaccounts.Account
	pools       *hostedaccounts.PoolService
	keys        *hostedaccounts.KeyService
	credentials *apikey.Service
	verifier    *recordingPoolVerifier
	info        session.Info
}

func newPoolStepUpFixture(t *testing.T) poolStepUpFixture {
	t.Helper()
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	_, keys, credentials, cipher, ctx := poolFixture(t, f)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	store := sqlstores.NewSQLUserStore(f.db)
	verifier := &recordingPoolVerifier{real: domainauth.NewService(store, store, sqlstores.NewSQLTransactor(f.db), nil, nil, nil, nil, authz.NewPermissionResolver(f.db))}
	return poolStepUpFixture{f, account, hostedaccounts.NewPoolService(f.svc, cipher, keys, verifier), keys, credentials, verifier, *info}
}
func (f poolStepUpFixture) sessionContext(t *testing.T) context.Context {
	t.Helper()
	return authn.SetInfo(t.Context(), &f.info)
}
func (f poolStepUpFixture) keyContext(t *testing.T, capabilities hostedaccounts.Capabilities) (context.Context, hostedaccounts.IssuedAccountKey, int64) {
	t.Helper()
	ctx := f.sessionContext(t)
	issued, err := f.keys.Create(ctx, f.user, f.org, f.account.AccountID, hostedaccounts.KeyOptions{Name: "Step-up fixture", Capabilities: capabilities})
	require.NoError(t, err)
	record, err := f.credentials.Validate(ctx, issued.Secret)
	require.NoError(t, err)
	info := f.info
	info.AuthMethod, info.AccountKey, info.APIKeyDatabaseID = session.AuthMethodAPIKey, true, record.ID
	machineCtx, err := hostedaccounts.WithMachineScope(authn.SetInfo(t.Context(), &info), f.account.AccountID, capabilities)
	require.NoError(t, err)
	return machineCtx, issued, record.ID
}
func (f poolStepUpFixture) requireUnchangedProfile(t *testing.T) {
	t.Helper()
	var profiles, events int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_pool_profile WHERE organization_id=$1 AND account_id=$2`, f.org, f.account.ID).Scan(&profiles))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='set_hosted_account_pool_profile'`, f.org).Scan(&events))
	require.Zero(t, profiles)
	require.Zero(t, events, "denied step-up must not mutate or audit a successful profile change")
}

func TestViewOnlyPoolKeyCannotSpendIssuerStepUpBudget(t *testing.T) {
	f := newPoolStepUpFixture(t)
	ctx, _, _ := f.keyContext(t, hostedaccounts.View)
	for range 12 {
		require.Error(t, f.pools.SetIncomingProfile(ctx, f.user, f.org, f.account.AccountID, "", "wrong-password"))
	}
	assert.Zero(t, f.verifier.calls, "read-only keys must never reach credential verification")
	require.NoError(t, f.verifier.real.VerifySessionCredentials(f.sessionContext(t), f.info.Username, "actor-password"), "denied key traffic must leave the real issuer's shared step-up budget available")
	f.requireUnchangedProfile(t)
}

func TestPoolStepUpRequiresLiveAuthorityBeforeVerifier(t *testing.T) {
	for _, failure := range []string{"view-key", "revoked-key", "lowered-key", "expired-key", "missing-key-scope", "missing-session-permission", "hierarchy-ceiling", "suspended-account"} {
		t.Run(failure, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			ctx := f.sessionContext(t)
			if failure == "view-key" {
				ctx, _, _ = f.keyContext(t, hostedaccounts.View)
			} else if failure == "missing-key-scope" {
				machineCtx, _, _ := f.keyContext(t, hostedaccounts.MachineCapabilities)
				info, err := session.GetInfo(machineCtx)
				require.NoError(t, err)
				ctx = authn.SetInfo(t.Context(), info)
			} else if failure == "revoked-key" || failure == "lowered-key" || failure == "expired-key" {
				machineCtx, issued, keyID := f.keyContext(t, hostedaccounts.MachineCapabilities)
				ctx = machineCtx
				switch failure {
				case "revoked-key":
					require.NoError(t, f.keys.Revoke(f.sessionContext(t), f.user, f.org, f.account.AccountID, issued.Key.KeyID))
				case "lowered-key":
					_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_key SET capabilities=$1 WHERE api_key_id=$2`, int64(hostedaccounts.View), keyID)
					require.NoError(t, err)
				case "expired-key":
					_, err := f.db.ExecContext(t.Context(), `UPDATE api_key SET expires_at=now()-interval '1 second' WHERE id=$1`, keyID)
					require.NoError(t, err)
				}
			} else {
				switch failure {
				case "missing-session-permission":
					require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, f.account.AccountID, f.host, hostedaccounts.RoleAdministrator))
					require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, f.account.AccountID, f.user, hostedaccounts.RoleViewer))
				case "hierarchy-ceiling":
					require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
				case "suspended-account":
					require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities, true))
				}
			}
			require.Error(t, f.pools.SetIncomingProfile(ctx, f.user, f.org, f.account.AccountID, "", "wrong-password"))
			require.Zero(t, f.verifier.calls, "live denied authority must precede credential loading and hashing")
			f.requireUnchangedProfile(t)
		})
	}
}

func TestAuthorizedPoolStepUpStillVerifiesCredentials(t *testing.T) {
	for _, principal := range []string{"session", "account-key"} {
		t.Run(principal, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			ctx := f.sessionContext(t)
			if principal == "account-key" {
				ctx, _, _ = f.keyContext(t, hostedaccounts.View|hostedaccounts.ChangePools)
			}
			require.Error(t, f.pools.SetIncomingProfile(ctx, f.user, f.org, f.account.AccountID, "", "wrong-password"))
			require.Equal(t, 1, f.verifier.calls)
			f.requireUnchangedProfile(t)
			require.NoError(t, f.pools.SetIncomingProfile(ctx, f.user, f.org, f.account.AccountID, "", "actor-password"))
			require.Equal(t, 2, f.verifier.calls)
			profile, err := f.pools.IncomingProfile(f.sessionContext(t), f.user, f.org, f.account.AccountID)
			require.NoError(t, err)
			require.Equal(t, int64(1), profile.Revision)
		})
	}
}

func TestPoolStepUpRechecksAuthorityAfterVerification(t *testing.T) {
	for _, change := range []string{"membership", "key-revocation", "key-capabilities", "hierarchy-ceiling"} {
		t.Run(change, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			// Provide another administrator so a live membership removal is valid.
			require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, f.account.AccountID, f.host, hostedaccounts.RoleAdministrator))
			source := f.sessionContext(t)
			var keyID int64
			var issued hostedaccounts.IssuedAccountKey
			if change == "key-revocation" || change == "key-capabilities" {
				source, issued, keyID = f.keyContext(t, hostedaccounts.MachineCapabilities)
			}
			ctx, cancel := context.WithTimeout(source, 10*time.Second)
			defer cancel()
			changed := false
			f.verifier.afterVerification = func() error {
				// These real mutations can acquire the organization lock while the
				// verifier is still active; admission must not retain that lock.
				mutationCtx, mutationCancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer mutationCancel()
				var changeErr error
				switch change {
				case "membership":
					changeErr = f.svc.SetMembership(mutationCtx, f.host, f.org, f.account.AccountID, f.user, hostedaccounts.RoleViewer)
				case "key-revocation":
					changeErr = f.keys.Revoke(mutationCtx, f.user, f.org, f.account.AccountID, issued.Key.KeyID)
				case "key-capabilities":
					_, changeErr = f.db.ExecContext(mutationCtx, `UPDATE hosted_account_key SET capabilities=$1 WHERE api_key_id=$2`, int64(hostedaccounts.View), keyID)
				case "hierarchy-ceiling":
					changeErr = f.svc.ConfigureChild(mutationCtx, f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false)
				}
				require.NoError(t, changeErr, "the authority change itself must commit before final reauthorization")
				changed = true
				return nil
			}
			require.Error(t, f.pools.SetIncomingProfile(ctx, f.user, f.org, f.account.AccountID, "", "actor-password"), "final transactional authority must reject changes during verification")
			require.Equal(t, 1, f.verifier.calls)
			require.True(t, changed)
			require.NoError(t, ctx.Err(), "live authority mutation must not wait behind a lock held during verification")
			f.requireUnchangedProfile(t)
		})
	}
}

func TestPoolStepUpPreservesCredentialLookupDatabaseFailure(t *testing.T) {
	f := newPoolStepUpFixture(t)
	// Keep SET ROLE and the service queries on the same real PostgreSQL connection.
	f.db.SetMaxOpenConns(1)
	f.db.SetMaxIdleConns(1)
	role := "pool_credential_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := f.db.ExecContext(t.Context(), "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" NOLOGIN")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		_, cleanupErr := f.db.ExecContext(ctx, "RESET ROLE")
		require.NoError(t, cleanupErr)
		_, cleanupErr = f.db.ExecContext(ctx, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		require.NoError(t, cleanupErr)
		_, cleanupErr = f.db.ExecContext(ctx, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
		require.NoError(t, cleanupErr)
	})
	_, err = f.db.ExecContext(t.Context(), `SELECT set_config('pool_test.credential_role', $1, false)`, role)
	require.NoError(t, err)
	// Bind the role name as data and quote identifiers with PostgreSQL's %I.
	// Live authority needs only id/deleted_at; the full credential projection
	// must fail with a real insufficient-privilege error.
	_, err = f.db.ExecContext(t.Context(), `DO $$ BEGIN
		EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', current_setting('pool_test.credential_role'));
		EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', current_setting('pool_test.credential_role'));
		EXECUTE format('REVOKE SELECT ON "user" FROM %I', current_setting('pool_test.credential_role'));
		EXECUTE format('GRANT SELECT (id, deleted_at) ON "user" TO %I', current_setting('pool_test.credential_role'));
	END $$`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "SET ROLE "+pgx.Identifier{role}.Sanitize())
	require.NoError(t, err)
	_, err = f.svc.Resolve(f.sessionContext(t), f.user, f.org, f.account.AccountID)
	require.NoError(t, err, "live account admission must remain authorized")
	err = f.pools.SetIncomingProfile(f.sessionContext(t), f.user, f.org, f.account.AccountID, "", "actor-password")
	_, resetErr := f.db.ExecContext(t.Context(), "RESET ROLE")
	require.NoError(t, resetErr)
	require.Zero(t, f.verifier.calls, "credential loading must fail before password verification")
	f.requireUnchangedProfile(t)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode, "database failure must not become an authorization denial")
	var postgresErr *pgconn.PgError
	require.ErrorAs(t, err, &postgresErr, "the database cause must remain available to callers")
	require.Equal(t, "42501", postgresErr.Code)
}
