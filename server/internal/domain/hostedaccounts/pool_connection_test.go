package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type poolConnectionProbe struct {
	calls                   int
	url, username, password string
	err                     error
}

func (p *poolConnectionProbe) ValidateConnection(_ context.Context, url, username string, password *secrets.Text, timeout *time.Duration) (bool, error) {
	p.calls++
	p.url, p.username, p.password = url, username, password.Value()
	return timeout != nil && *timeout > 0 && *timeout <= 10*time.Second, p.err
}

func TestSharedPoolConnectionUsesOnlyAuthorizedMatchingSlotCredentials(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	probe := &poolConnectionProbe{}
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, nil).WithConnectionTester(probe)
	password := "private-pool-password"
	slot := poolconfigurations.SlotOptions{URL: "stratum+tcp://pool.example", Username: "payout", WorkerTemplate: "{miner_name}", Password: &password}
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{slot}})
	require.NoError(t, err)
	slot.ID, slot.Password = config.Slots[0].ID, nil
	ok, err := service.TestConnection(ctx, f.account.AccountID, config.ID, config.Version, slot)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, password, probe.password)
	require.Equal(t, "payout.Miner-123", probe.username)

	for _, change := range []string{"destination", "username", "slot", "version", "scope"} {
		t.Run(change, func(t *testing.T) {
			candidate, account, version := slot, f.account.AccountID, config.Version
			switch change {
			case "destination":
				candidate.URL = "stratum+tcp://other.example"
			case "username":
				candidate.Username = "other"
			case "slot":
				candidate.ID = uuid.NewString()
			case "version":
				version++
			case "scope":
				account = ""
			}
			calls := probe.calls
			_, err := service.TestConnection(ctx, account, config.ID, version, candidate)
			require.Error(t, err)
			require.Equal(t, calls, probe.calls, "rejection must precede network I/O")
		})
	}
	emptyPassword := ""
	slot.Password = &emptyPassword
	ok, err = service.TestConnection(ctx, f.account.AccountID, config.ID, config.Version, slot)
	require.NoError(t, err)
	require.True(t, ok)
	require.Empty(t, probe.password)
	slot.Password = nil
	probe.err = errors.New("driver echoed " + password)
	ok, err = service.TestConnection(ctx, f.account.AccountID, config.ID, config.Version, slot)
	require.NoError(t, err, "probe failures return a boolean, never raw credential-bearing errors")
	require.False(t, ok)

	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ManagePools, false))
	calls := probe.calls
	_, err = service.TestConnection(ctx, f.account.AccountID, config.ID, config.Version, slot)
	require.Error(t, err)
	require.Equal(t, calls, probe.calls, "Change pools alone does not authorize library network probes")
	var operations int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&operations))
	require.Zero(t, operations, "connection tests never apply settings to equipment")
}
