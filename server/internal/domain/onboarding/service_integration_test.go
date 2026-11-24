package onboarding_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/onboarding"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type noOnboardedDevices struct{ interfaces.DeviceStore }

func (noOnboardedDevices) GetTotalPairedDevices(context.Context, int64, *interfaces.MinerFilter) (int64, error) {
	return 0, nil
}
func (noOnboardedDevices) GetTotalDevicesPendingAuth(context.Context, int64) (int64, error) {
	return 0, nil
}

func TestFleetOnboardingCountsOnlyActiveOperatorConfigurations(t *testing.T) {
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	user := database.CreateSuperAdminUser()
	other := database.CreateSuperAdminUser2()
	q := sqlc.New(database.DB)
	cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: config.ServiceMasterKey})
	require.NoError(t, err)
	repository := poolconfigurations.NewRepository(cipher)
	service := onboarding.NewService(noOnboardedDevices{}, q, nil)
	ctx := t.Context()
	requireConfigured := func(t *testing.T, orgID int64, want bool) {
		t.Helper()
		selected := authn.SetInfo(ctx, &session.Info{OrganizationID: orgID, AuthMethod: session.AuthMethodSession})
		status, err := service.GetFleetOnboardingStatus(selected)
		require.NoError(t, err)
		require.Equal(t, want, status.PoolConfigured)
		require.False(t, status.DevicePaired)
	}
	createConfiguration := func(scope poolconfigurations.Scope) poolconfigurations.Configuration {
		t.Helper()
		created, err := db.WithTransaction(ctx, database.DB, func(tx sqlc.Querier) (poolconfigurations.Configuration, error) {
			return repository.Create(ctx, tx, scope, poolconfigurations.RevisionOptions{
				Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "payout"}},
			})
		})
		require.NoError(t, err)
		return created
	}

	t.Run("legacy pools alone do not complete setup", func(t *testing.T) {
		_, err := q.CreatePool(ctx, sqlc.CreatePoolParams{
			OrgID: user.OrganizationID, PoolName: "Retired pool", Url: "stratum+tcp://legacy.example:3333", Username: "payout", CreatedAt: time.Now(),
		})
		require.NoError(t, err)
		requireConfigured(t, user.OrganizationID, false)
	})
	t.Run("hosted account configurations do not complete operator setup", func(t *testing.T) {
		host, err := q.GetHostPoolAccount(ctx, user.OrganizationID)
		require.NoError(t, err)
		account, err := q.CreateHostedAccount(ctx, sqlc.CreateHostedAccountParams{
			AccountID: uuid.NewString(), OrganizationID: user.OrganizationID,
			ParentID: sql.NullInt64{Int64: host.ID, Valid: true}, Depth: 1, Name: "Customer", Capabilities: 31,
		})
		require.NoError(t, err)
		createConfiguration(poolconfigurations.Scope{OrganizationID: user.OrganizationID, AccountID: account.ID})
		requireConfigured(t, user.OrganizationID, false)
	})
	t.Run("another organization's operator configuration is isolated", func(t *testing.T) {
		createConfiguration(poolconfigurations.Scope{OrganizationID: other.OrganizationID})
		requireConfigured(t, user.OrganizationID, false)
		requireConfigured(t, other.OrganizationID, true)
	})
	t.Run("archived operator configurations do not complete setup", func(t *testing.T) {
		scope := poolconfigurations.Scope{OrganizationID: user.OrganizationID}
		configuration := createConfiguration(scope)
		require.NoError(t, db.WithTransactionNoResult(ctx, database.DB, func(tx sqlc.Querier) error {
			return repository.Archive(ctx, tx, scope, configuration.ID, configuration.Version)
		}))
		requireConfigured(t, user.OrganizationID, false)
	})
	t.Run("an active operator configuration completes setup", func(t *testing.T) {
		createConfiguration(poolconfigurations.Scope{OrganizationID: user.OrganizationID})
		requireConfigured(t, user.OrganizationID, true)
	})
}
