package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedRPCPoolRevisionsStayOwnedAndSecretFree(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	sibling := f.account(t, "Sibling", f.host, domain.DefaultCapabilities)
	secret := "pool-password-that-must-stay-private"
	input := &pb.PoolRevisionInput{Name: "Customer pools", Slots: []*pb.PoolSlotInput{{Url: "stratum+tcp://pool.customer.example:3333", Username: "customer.wallet", WorkerTemplate: "{miner_id}", Password: &secret}, {Url: "stratum+ssl://backup.customer.example:443", Username: "customer.backup"}}}
	created, err := f.client.CreatePoolConfiguration(t.Context(), rpcRequest(&pb.CreatePoolConfigurationRequest{AccountId: account.AccountID, Configuration: input}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, created.Msg.Revision.Slots, 2)
	require.Equal(t, "customer.wallet", created.Msg.Revision.Slots[0].Username)
	require.True(t, created.Msg.Revision.Slots[0].HasPassword)
	require.NotContains(t, string(mustProtoJSON(t, created.Msg)), secret)
	got, err := f.client.GetPoolRevision(t.Context(), rpcRequest(&pb.GetPoolRevisionRequest{AccountId: account.AccountID, PoolId: created.Msg.Revision.PoolId}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, created.Msg.Revision.RevisionId, got.Msg.Revision.RevisionId)
	_, err = f.client.GetPoolRevision(t.Context(), rpcRequest(&pb.GetPoolRevisionRequest{AccountId: sibling.AccountID, PoolId: created.Msg.Revision.PoolId}, f.cookie, sibling.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	input.Slots = input.Slots[:1]
	input.Slots[0].Password = nil
	changed, err := f.client.RevisePoolConfiguration(t.Context(), rpcRequest(&pb.RevisePoolConfigurationRequest{AccountId: account.AccountID, PoolId: created.Msg.Revision.PoolId, ExpectedRevisionId: created.Msg.Revision.RevisionId, Configuration: input}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.NotEqual(t, created.Msg.Revision.RevisionId, changed.Msg.Revision.RevisionId)
	require.Equal(t, int64(2), changed.Msg.Revision.Number)
	require.Len(t, changed.Msg.Revision.Slots, 1)
	require.True(t, changed.Msg.Revision.Slots[0].HasPassword)
	oldID := created.Msg.Revision.RevisionId
	old, err := f.client.GetPoolRevision(t.Context(), rpcRequest(&pb.GetPoolRevisionRequest{AccountId: account.AccountID, PoolId: created.Msg.Revision.PoolId, RevisionId: &oldID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, old.Msg.Revision.Slots, 2, "editing preserves the previous complete configuration")
	_, err = f.client.RevisePoolConfiguration(t.Context(), rpcRequest(&pb.RevisePoolConfigurationRequest{AccountId: account.AccountID, PoolId: created.Msg.Revision.PoolId, ExpectedRevisionId: oldID, Configuration: input}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestHostedRPCIncomingPoolProfileRequiresServerStepUp(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	created, err := f.client.CreatePoolConfiguration(t.Context(), rpcRequest(&pb.CreatePoolConfigurationRequest{AccountId: account.AccountID, Configuration: &pb.PoolRevisionInput{Name: "Default", Slots: []*pb.PoolSlotInput{{Url: "stratum+tcp://chosen.customer.example:3333", Username: "wallet.payout"}}}}, f.cookie, account.AccountID))
	require.NoError(t, err)
	revisionID := created.Msg.Revision.RevisionId
	_, err = f.client.SetIncomingPoolProfile(t.Context(), rpcRequest(&pb.SetIncomingPoolProfileRequest{AccountId: account.AccountID, RevisionId: &revisionID, Password: "incorrect-password"}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	profile, err := f.client.GetIncomingPoolProfile(t.Context(), rpcRequest(&pb.GetIncomingPoolProfileRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Nil(t, profile.Msg.Profile)
	_, err = f.client.SetIncomingPoolProfile(t.Context(), rpcRequest(&pb.SetIncomingPoolProfileRequest{AccountId: account.AccountID, RevisionId: &revisionID, Password: rpcPassword}, f.cookie, account.AccountID))
	require.NoError(t, err)
	profile, err = f.client.GetIncomingPoolProfile(t.Context(), rpcRequest(&pb.GetIncomingPoolProfileRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, revisionID, profile.Msg.Profile.RevisionId)

	require.Equal(t, created.Msg.Revision.PoolId, profile.Msg.Profile.PoolId)
	changed, err := f.client.RevisePoolConfiguration(t.Context(), rpcRequest(&pb.RevisePoolConfigurationRequest{AccountId: account.AccountID, PoolId: created.Msg.Revision.PoolId, ExpectedRevisionId: revisionID, Configuration: &pb.PoolRevisionInput{Name: "Edited", Slots: []*pb.PoolSlotInput{{Url: "stratum+tcp://new.customer.example:3333", Username: "new.wallet"}}}}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.NotEqual(t, revisionID, changed.Msg.Revision.RevisionId)
	profile, err = f.client.GetIncomingPoolProfile(t.Context(), rpcRequest(&pb.GetIncomingPoolProfileRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	pinned, err := f.client.GetPoolRevision(t.Context(), rpcRequest(&pb.GetPoolRevisionRequest{AccountId: account.AccountID, PoolId: profile.Msg.Profile.PoolId, RevisionId: &profile.Msg.Profile.RevisionId}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, revisionID, pinned.Msg.Revision.RevisionId)
	require.Equal(t, "Default", pinned.Msg.Revision.Name)
	_, err = f.client.ArchivePoolConfiguration(t.Context(), rpcRequest(&pb.ArchivePoolConfigurationRequest{AccountId: account.AccountID, PoolId: created.Msg.Revision.PoolId}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	_, err = f.client.SetIncomingPoolProfile(t.Context(), rpcRequest(&pb.SetIncomingPoolProfileRequest{AccountId: account.AccountID, Password: rpcPassword}, f.cookie, account.AccountID))
	require.NoError(t, err)
	profile, err = f.client.GetIncomingPoolProfile(t.Context(), rpcRequest(&pb.GetIncomingPoolProfileRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.NotNil(t, profile.Msg.Profile, "clearing retains a monotonic witness for future assignments")
	require.Empty(t, profile.Msg.Profile.RevisionId)
	require.Empty(t, profile.Msg.Profile.PoolId)
	require.Equal(t, int64(2), profile.Msg.Profile.Revision)
}

func TestHostedRPCPoolKeysRespectCapabilitiesAndRevocation(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	sibling := f.account(t, "Sibling", f.host, domain.DefaultCapabilities)
	readKey, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Read", Capabilities: 1}, f.cookie, account.AccountID))
	require.NoError(t, err)
	writeKey, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&pb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Manage pools", Capabilities: 3}, f.cookie, account.AccountID))
	require.NoError(t, err)
	input := &pb.CreatePoolConfigurationRequest{AccountId: account.AccountID, Configuration: &pb.PoolRevisionInput{Name: "Chosen", Slots: []*pb.PoolSlotInput{{Url: "stratum+tcp://chosen.customer.example:3333", Username: "my.wallet"}}}}
	_, err = f.client.CreatePoolConfiguration(t.Context(), keyRequest(input, readKey.Msg.Secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	created, err := f.client.CreatePoolConfiguration(t.Context(), keyRequest(input, writeKey.Msg.Secret, account.AccountID))
	require.NoError(t, err)
	list, err := f.client.ListPoolConfigurations(t.Context(), keyRequest(&pb.ListPoolConfigurationsRequest{AccountId: account.AccountID}, readKey.Msg.Secret, account.AccountID))
	require.NoError(t, err)
	require.Len(t, list.Msg.Configurations, 1)
	require.Equal(t, created.Msg.Revision.PoolId, list.Msg.Configurations[0].PoolId)
	_, err = f.client.ListPoolConfigurations(t.Context(), keyRequest(&pb.ListPoolConfigurationsRequest{AccountId: sibling.AccountID}, readKey.Msg.Secret, sibling.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	organizationKey, _, err := f.keys.Create(t.Context(), f.host, f.org, "host", "host", "Organization", nil)
	require.NoError(t, err)
	for _, selected := range []string{"", account.AccountID} {
		_, err = f.client.ListPoolConfigurations(t.Context(), keyRequest(&pb.ListPoolConfigurationsRequest{AccountId: account.AccountID}, organizationKey, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	_, err = f.client.RevokeAccountKey(t.Context(), rpcRequest(&pb.RevokeAccountKeyRequest{AccountId: account.AccountID, KeyId: writeKey.Msg.Key.KeyId}, f.cookie, account.AccountID))
	require.NoError(t, err)
	_, err = f.client.CreatePoolConfiguration(t.Context(), keyRequest(input, writeKey.Msg.Secret, account.AccountID))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}
