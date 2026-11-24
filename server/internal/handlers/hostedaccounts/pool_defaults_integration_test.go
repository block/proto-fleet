package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolRPCDefaultRequiresAccountManagementAuthority(t *testing.T) {
	f := newRPCFixture(t)
	ctx := t.Context()
	admin := f.addUser(t, "default-admin", false)
	parent := f.account(t, "Customer", admin, domain.AllCapabilities)
	child, err := f.accounts.CreateChild(ctx, admin, f.org, parent.AccountID, "Subclient", admin, domain.AllCapabilities)
	require.NoError(t, err)
	cookie := f.login(t, "default-admin").Header().Get("Set-Cookie")
	viewer := f.addUser(t, "default-viewer", false)
	viewerCookie := f.login(t, "default-viewer").Header().Get("Set-Cookie")
	for _, accountID := range []string{parent.AccountID, child.Account.AccountID} {
		t.Run(accountID, func(t *testing.T) {
			createConfig := func(name string) *pb.Configuration {
				t.Helper()
				created, err := f.configurations.CreateConfiguration(ctx, rpcRequest(&pb.CreateConfigurationRequest{AccountId: accountID, Configuration: &pb.ConfigurationInput{Name: name, Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout"}}}}, cookie, accountID))
				require.NoError(t, err)
				return created.Msg.Configuration
			}
			first, second := createConfig("First"), createConfig("Second")
			require.True(t, first.IsDefault)
			require.False(t, second.IsDefault)
			request := &pb.SetDefaultConfigurationRequest{AccountId: accountID, ConfigurationId: second.ConfigurationId, ExpectedVersion: second.Version}
			require.NoError(t, f.accounts.SetMembership(ctx, admin, f.org, accountID, viewer, domain.RoleViewer))
			_, err := f.configurations.SetDefaultConfiguration(ctx, rpcRequest(request, viewerCookie, accountID))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			_, err = f.configurations.SetDefaultConfiguration(ctx, rpcRequest(request, cookie, accountID))
			require.NoError(t, err)
			listed, err := f.configurations.ListConfigurations(ctx, rpcRequest(&pb.ListConfigurationsRequest{AccountId: accountID}, cookie, accountID))
			require.NoError(t, err)
			require.Len(t, listed.Msg.Configurations, 2)
			for _, config := range listed.Msg.Configurations {
				require.Equal(t, config.ConfigurationId == second.ConfigurationId, config.IsDefault)
			}
		})
	}
	listed, err := f.configurations.ListConfigurations(ctx, rpcRequest(&pb.ListConfigurationsRequest{AccountId: child.AccountID}, cookie, child.AccountID))
	require.NoError(t, err)
	config := listed.Msg.Configurations[0]
	require.NoError(t, f.accounts.ConfigureChild(ctx, admin, f.org, child.AccountID, domain.AllCapabilities&^domain.ManagePools, false))
	_, err = f.configurations.SetDefaultConfiguration(ctx, rpcRequest(&pb.SetDefaultConfigurationRequest{AccountId: child.AccountID, ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version}, cookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "account administrators still require the delegated ManagePools capability")
}
