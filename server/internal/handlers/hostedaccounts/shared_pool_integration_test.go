package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolRPCScopesReviewsAndPreservesCredentials(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	for _, accountID := range []string{"", account.AccountID} {
		t.Run(accountID, func(t *testing.T) {
			secret := "private-pool-password"
			input := &pb.ConfigurationInput{Name: "Production", Pools: []*pb.PoolInput{
				{Url: "stratum+tcp://primary.example:3333", Username: "payout", Password: &secret},
				{Url: "stratum+tcp://backup.example:3333", Username: "backup"},
			}}
			created, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{AccountId: accountID, Configuration: input}, f.cookie, accountID))
			require.NoError(t, err)
			c := created.Msg.Configuration
			require.NotContains(t, string(mustProtoJSON(t, created.Msg)), secret)
			input.Pools[0].SlotId, input.Pools[1].SlotId = c.Pools[0].SlotId, c.Pools[1].SlotId
			input.Pools[0].Password = nil
			input.Pools[0], input.Pools[1] = input.Pools[1], input.Pools[0]
			empty := ""
			input.Pools = append(input.Pools, &pb.PoolInput{Url: "stratum+tcp://new-backup.example:3333", Username: "new-backup", Password: &empty})
			review, err := f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{AccountId: accountID, ConfigurationId: c.ConfigurationId, ExpectedVersion: c.Version, Configuration: input}, f.cookie, accountID))
			require.NoError(t, err)
			require.True(t, review.Msg.SettingsChanged)
			require.Zero(t, review.Msg.AffectedMiners)
			require.True(t, review.Msg.Configuration.Pools[1].HasPassword)
			require.Equal(t, c.Pools[1].SlotId, review.Msg.Configuration.Pools[0].SlotId)
			require.Equal(t, c.Pools[0].SlotId, review.Msg.Configuration.Pools[1].SlotId)
			newSlotID := review.Msg.Configuration.Pools[2].SlotId
			require.NotEmpty(t, newSlotID)
			require.NotContains(t, []string{c.Pools[0].SlotId, c.Pools[1].SlotId}, newSlotID)
			require.NotContains(t, string(mustProtoJSON(t, review.Msg)), secret)
			unchanged, err := f.configurations.GetConfiguration(t.Context(), rpcRequest(&pb.GetConfigurationRequest{AccountId: accountID, ConfigurationId: c.ConfigurationId}, f.cookie, accountID))
			require.NoError(t, err)
			require.Equal(t, c.RevisionId, unchanged.Msg.Configuration.RevisionId, "review does not save")
			confirmation := &pb.ConfirmReviewRequest{AccountId: accountID, ReviewId: review.Msg.ReviewId}
			saved, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(confirmation, f.cookie, accountID))
			require.NoError(t, err)
			require.NotEqual(t, c.RevisionId, saved.Msg.Configuration.RevisionId)
			require.Equal(t, newSlotID, saved.Msg.Configuration.Pools[2].SlotId, "confirmation accepts the new server-issued identity")
			require.True(t, saved.Msg.Configuration.Pools[1].HasPassword)
			require.Empty(t, saved.Msg.OperationId, "unassigned configurations send no equipment work")
			again, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(confirmation, f.cookie, accountID))
			require.NoError(t, err)
			require.Equal(t, saved.Msg.Configuration.RevisionId, again.Msg.Configuration.RevisionId)
			var events int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='update_pool_configuration' AND metadata->>'configuration_id'=$2`, f.org, c.ConfigurationId).Scan(&events))
			require.Equal(t, 1, events, "confirmation replay does not duplicate audit")
			_, err = f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{AccountId: accountID, ConfigurationId: c.ConfigurationId, ExpectedVersion: c.Version, Configuration: input}, f.cookie, accountID))
			require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
			other := account.AccountID
			if other == accountID {
				other = ""
			}
			_, err = f.configurations.GetConfiguration(t.Context(), rpcRequest(&pb.GetConfigurationRequest{AccountId: other, ConfigurationId: c.ConfigurationId}, f.cookie, other))
			require.Error(t, err, "configuration ownership is enforced even for an actor authorized in both scopes")
		})
	}
	_, err := f.configurations.ListConfigurations(t.Context(), rpcRequest(&pb.ListConfigurationsRequest{}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "account header cannot select operator library")
	_, err = f.configurations.ListConfigurations(t.Context(), rpcRequest(&pb.ListConfigurationsRequest{AccountId: account.AccountID}, f.cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "account body requires selected workspace")
}

func TestSharedPoolRPCCreationRequiresNewSlotIdentities(t *testing.T) {
	f := newRPCFixture(t)
	password := "first-password"
	input := &pb.ConfigurationInput{Name: "Independent", Pools: []*pb.PoolInput{
		{Url: "stratum+tcp://primary.example", Username: "payout", Password: &password},
		{Url: "stratum+tcp://backup.example", Username: "backup"},
	}}
	first, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	password = "different-password"
	second, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err, "identical destinations with different passwords remain independent configurations")
	for i := range input.Pools {
		require.NotEmpty(t, first.Msg.Configuration.Pools[i].SlotId)
		require.NotEqual(t, first.Msg.Configuration.Pools[i].SlotId, second.Msg.Configuration.Pools[i].SlotId)
		t.Run(input.Pools[i].Username, func(t *testing.T) {
			input.Pools[i].SlotId = first.Msg.Configuration.Pools[i].SlotId
			defer func() { input.Pools[i].SlotId = "" }()
			_, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_configuration WHERE organization_id=$1`, f.org).Scan(&count))
	require.Equal(t, 2, count, "rejected creation must not persist a configuration")
}

func TestSharedPoolRPCReviewRejectsForeignSlotIdentities(t *testing.T) {
	f := newRPCFixture(t)
	password := "explicit-password"
	input := &pb.ConfigurationInput{Name: "Independent", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout", Password: &password}}}
	first, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	other, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	for _, test := range []struct{ name, id, url string }{
		{"invented", uuid.NewString(), "stratum+tcp://changed.example"},
		{"another configuration", other.Msg.Configuration.Pools[0].SlotId, "stratum+tcp://changed.example"},
		{"unchanged settings", uuid.NewString(), input.Pools[0].Url},
	} {
		t.Run(test.name, func(t *testing.T) {
			input.Pools[0].SlotId, input.Pools[0].Url = test.id, test.url
			_, err := f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{
				ConfigurationId: first.Msg.Configuration.ConfigurationId, ExpectedVersion: first.Msg.Configuration.Version, Configuration: input,
			}, f.cookie, ""))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "an explicit password does not authorize a caller-issued identity")
		})
	}
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_configuration_review WHERE organization_id=$1`, f.org).Scan(&count))
	require.Zero(t, count, "invalid identities must not produce a confirmable review")
}

func TestSharedPoolRPCAccountKeyCannotEscapeOwner(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	key, err := f.client.CreateAccountKey(t.Context(), rpcRequest(&hostedpb.CreateAccountKeyRequest{AccountId: account.AccountID, Name: "Pool automation", Capabilities: uint64(domain.MachineCapabilities)}, f.cookie, account.AccountID))
	require.NoError(t, err)
	input := &pb.ConfigurationInput{Name: "Private", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example:3333", Username: "payout"}}}
	_, err = f.configurations.CreateConfiguration(t.Context(), keyRequest(&pb.CreateConfigurationRequest{AccountId: account.AccountID, Configuration: input}, key.Msg.Secret, account.AccountID))
	require.NoError(t, err)
	_, err = f.configurations.CreateConfiguration(t.Context(), keyRequest(&pb.CreateConfigurationRequest{Configuration: input}, key.Msg.Secret, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.configurations.ListConfigurations(t.Context(), keyRequest(&pb.ListConfigurationsRequest{}, key.Msg.Secret, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestSharedPoolRPCRenameRetainsAssignmentCount(t *testing.T) {
	f := newRPCFixture(t)
	input := &pb.ConfigurationInput{Name: "Production", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example:3333", Username: "payout"}}}
	created, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	c := created.Msg.Configuration
	var discovery, deviceID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name) VALUES($1,'assigned','192.0.2.1','80','http','proto') RETURNING id`, f.org).Scan(&discovery))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,'assigned','02:00:00:00:00:01',$2) RETURNING id`, f.org, discovery).Scan(&deviceID))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO pool_configuration_operation(operation_id,organization_id,configuration_id,revision_id,actor_id) VALUES('00000000-0000-0000-0000-000000000001',$1,$2,$3,$4)`, f.org, c.ConfigurationId, c.RevisionId, f.host)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,operation_id) VALUES($1,'device',$2,$2,$3,$4,1,'00000000-0000-0000-0000-000000000001')`, f.org, deviceID, c.ConfigurationId, c.RevisionId)
	require.NoError(t, err)
	input.Name = "Renamed"
	input.Pools[0].SlotId = c.Pools[0].SlotId
	review, err := f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{ConfigurationId: c.ConfigurationId, ExpectedVersion: c.Version, Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	require.EqualValues(t, 1, review.Msg.Configuration.AssignedCount)
	require.Zero(t, review.Msg.AffectedMiners)
	saved, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(&pb.ConfirmReviewRequest{ReviewId: review.Msg.ReviewId}, f.cookie, ""))
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Msg.Configuration.AssignedCount)
	require.Equal(t, c.RevisionId, saved.Msg.Configuration.RevisionId)
	require.Empty(t, saved.Msg.OperationId)
	input.Pools[0].SlotId = "not-a-slot-identity"
	_, err = f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{ConfigurationId: c.ConfigurationId, ExpectedVersion: saved.Msg.Configuration.Version, Configuration: input}, f.cookie, ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
