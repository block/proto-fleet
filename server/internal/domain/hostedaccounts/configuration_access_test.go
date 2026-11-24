package hostedaccounts_test

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/stretchr/testify/require"
)

func TestSharedConfigurationServiceSeparatesOperatorAndSelectedAccountLibraries(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	legacy, _, _, cipher, customerCtx := poolFixture(t, f)
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), legacy, nil)
	customerCtx, err := hostedaccounts.WithRequestAccountID(customerCtx, account.AccountID)
	require.NoError(t, err)
	hostCtx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
	create := func(ctx context.Context, accountID, name string) (poolconfigurations.Configuration, error) {
		return service.Create(ctx, accountID, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	}
	operator, err := create(hostCtx, "", "Operator")
	require.NoError(t, err)
	private, err := create(customerCtx, account.AccountID, "Customer")
	require.NoError(t, err)
	page, err := service.List(hostCtx, "", "", 50, false)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 1)
	require.Equal(t, operator.ID, page.Configurations[0].ID)
	page, err = service.List(customerCtx, account.AccountID, "", 50, false)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 1)
	require.Equal(t, private.ID, page.Configurations[0].ID)
	_, err = create(customerCtx, "", "Scope escape")
	require.True(t, fleeterror.IsForbiddenError(err))
	_, err = service.Get(customerCtx, account.AccountID, operator.ID)
	require.Error(t, err)
	_, err = service.Get(hostCtx, "", private.ID)
	require.Error(t, err, "operator permissions do not reveal private account configurations")
	_, err = service.Get(hostCtx, account.AccountID, private.ID)
	require.True(t, fleeterror.IsForbiddenError(err), "customer access requires explicit selected account and live account authority")
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account SET suspended=TRUE WHERE id=$1`, account.ID)
	require.NoError(t, err)
	_, err = create(customerCtx, account.AccountID, "Suspended")
	require.True(t, fleeterror.IsForbiddenError(err))
	var events int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='create_pool_configuration'`, f.org).Scan(&events))
	require.Equal(t, 2, events, "creation and audit commit together for both owner types")
}
