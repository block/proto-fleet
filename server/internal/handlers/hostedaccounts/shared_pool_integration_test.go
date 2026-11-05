package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/pools"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type rpcPoolProbe struct{ calls int }

func (p *rpcPoolProbe) ValidateConnection(context.Context, string, string, *secrets.Text, *time.Duration) (bool, error) {
	p.calls++
	return true, nil
}

func TestSharedPoolRPCConnectionTestValidatesSelectionAndAuthority(t *testing.T) {
	f := newRPCFixture(t)
	probe := &rpcPoolProbe{}
	f.configurationService.WithConnectionTester(probe)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	for _, accountID := range []string{"", account.AccountID} {
		t.Run(accountID, func(t *testing.T) {
			request := &pb.TestConnectionRequest{AccountId: accountID, Pool: &pb.PoolInput{Url: "stratum+tcp://pool.example", Username: "payout"}}
			tested, err := f.configurations.TestConnection(t.Context(), rpcRequest(request, f.cookie, accountID))
			require.NoError(t, err)
			require.True(t, tested.Msg.Connected)
			calls := probe.calls
			request.ExpectedVersion = 1
			_, err = f.configurations.TestConnection(t.Context(), rpcRequest(request, f.cookie, accountID))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "draft versions cannot be silently ignored")
			request.ExpectedVersion = 0
			request.Pool.SlotId = uuid.NewString()
			_, err = f.configurations.TestConnection(t.Context(), rpcRequest(request, f.cookie, accountID))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "saved slot identity requires its configuration")
			require.Equal(t, calls, probe.calls)
		})
	}
	require.NoError(t, f.accounts.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, domain.DefaultCapabilities&^domain.ManagePools, false))
	calls := probe.calls
	_, err := f.configurations.TestConnection(t.Context(), rpcRequest(&pb.TestConnectionRequest{AccountId: account.AccountID, Pool: &pb.PoolInput{Url: "stratum+tcp://pool.example", Username: "payout"}}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.Equal(t, calls, probe.calls)
}

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
			for _, password := range []string{"incorrect", rpcPassword} {
				confirmation.Password = password
				_, err = f.configurations.ConfirmReview(t.Context(), rpcRequest(confirmation, f.cookie, accountID))
				require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "settings-only saves reject any supplied confirmation password")
			}
			unchanged, err = f.configurations.GetConfiguration(t.Context(), rpcRequest(&pb.GetConfigurationRequest{AccountId: accountID, ConfigurationId: c.ConfigurationId}, f.cookie, accountID))
			require.NoError(t, err)
			require.Equal(t, c.Version, unchanged.Msg.Configuration.Version, "rejected confirmations must not save")
			confirmation.Password = ""
			saved, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(confirmation, f.cookie, accountID))
			require.NoError(t, err)
			require.NotEqual(t, c.RevisionId, saved.Msg.Configuration.RevisionId)
			require.Equal(t, newSlotID, saved.Msg.Configuration.Pools[2].SlotId, "confirmation accepts the new server-issued identity")
			require.True(t, saved.Msg.Configuration.Pools[1].HasPassword)
			require.Empty(t, saved.Msg.OperationId, "unassigned configurations send no equipment work")
			again, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(confirmation, f.cookie, accountID))
			require.NoError(t, err)
			require.Equal(t, saved.Msg.Configuration.RevisionId, again.Msg.Configuration.RevisionId)
			confirmation.Password = rpcPassword
			_, err = f.configurations.ConfirmReview(t.Context(), rpcRequest(confirmation, f.cookie, accountID))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "settings-only replays also reject confirmation passwords")
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

func TestSharedPoolRPCAppliesWithoutManagePermissionAndPersistsResults(t *testing.T) {
	for _, hosted := range []bool{false, true} {
		name := "operator"
		if hosted {
			name = "hosted"
		}
		t.Run(name, func(t *testing.T) {
			f := newRPCFixture(t)
			accountID := ""
			var account domain.Account
			if hosted {
				account = f.account(t, "Customer", f.host, domain.DefaultCapabilities)
				accountID = account.AccountID
			}
			input := &pb.ConfigurationInput{Name: "Production", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout"}}}
			created, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{AccountId: accountID, Configuration: input}, f.cookie, accountID))
			require.NoError(t, err)
			config := created.Msg.Configuration
			var discovery, device int64
			identifier := uuid.NewString()
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, f.org, identifier).Scan(&discovery))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, f.org, identifier, discovery).Scan(&device))
			if hosted {
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, f.org, device, account.ID)
				require.NoError(t, err)
				require.NoError(t, f.accounts.ConfigureChild(t.Context(), f.host, f.org, accountID, domain.DefaultCapabilities&^domain.ManagePools, false))
			} else {
				role, err := authz.NewService(f.db, nil).CreateCustomRole(t.Context(), f.host, f.org, "Apply pools", "", []string{authz.PermPoolRead, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools})
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `DELETE FROM user_organization_role WHERE user_id=$1 AND organization_id=$2`, f.host, f.org)
				require.NoError(t, err)
				_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.host, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
				require.NoError(t, err)
			}
			_, err = f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{AccountId: accountID, ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version, Configuration: input}, f.cookie, accountID))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "editing still requires Manage")
			selection := &pb.ReviewAssignmentRequest{AccountId: accountID, ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version, DeviceIdentifiers: []string{identifier}}
			if hosted {
				selection.DeviceIdentifiers = nil
				selection.DeviceIds = []int64{device}
			}
			review, err := f.configurations.ReviewAssignment(t.Context(), rpcRequest(selection, f.cookie, accountID))
			require.NoError(t, err)
			require.EqualValues(t, 1, review.Msg.AffectedMiners)
			require.Len(t, review.Msg.Equipment, 1)
			require.Equal(t, device, review.Msg.Equipment[0].TargetId)
			require.NotEmpty(t, review.Msg.Equipment[0].Label)
			require.NotContains(t, string(mustProtoJSON(t, review.Msg)), "reviewed_binding")
			_, err = f.configurations.ConfirmReview(t.Context(), rpcRequest(&pb.ConfirmReviewRequest{AccountId: accountID, ReviewId: review.Msg.ReviewId}, f.cookie, accountID))
			require.Error(t, err, "hardware changes require a password confirmation")
			saved, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(&pb.ConfirmReviewRequest{AccountId: accountID, ReviewId: review.Msg.ReviewId, Password: rpcPassword}, f.cookie, accountID))
			require.NoError(t, err)
			require.NotEmpty(t, saved.Msg.OperationId)
			result, err := f.configurations.GetOperation(t.Context(), rpcRequest(&pb.GetOperationRequest{AccountId: accountID, OperationId: saved.Msg.OperationId}, f.cookie, accountID))
			require.NoError(t, err)
			require.Len(t, result.Msg.Targets, 1)
			require.Equal(t, "pending", result.Msg.Targets[0].State, "saving does not claim equipment application")
			require.NotContains(t, string(mustProtoJSON(t, result.Msg)), rpcPassword)
			require.NotEmpty(t, result.Msg.Targets[0].Label)
			history, err := f.configurations.ListOperations(t.Context(), rpcRequest(&pb.ListOperationsRequest{AccountId: accountID, ConfigurationId: config.ConfigurationId}, f.cookie, accountID))
			require.NoError(t, err)
			require.Len(t, history.Msg.Operations, 1)
			require.Equal(t, saved.Msg.OperationId, history.Msg.Operations[0].OperationId)
			require.EqualValues(t, 1, history.Msg.Operations[0].PendingCount)
			_, err = f.configurations.ReviewRetry(t.Context(), rpcRequest(&pb.ReviewRetryRequest{AccountId: accountID, OperationId: saved.Msg.OperationId, Targets: []*pb.RetryTarget{{Kind: "device", TargetId: device}}}, f.cookie, accountID))
			require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "pending work cannot be duplicated by retry")
			current, err := f.configurations.GetConfiguration(t.Context(), rpcRequest(&pb.GetConfigurationRequest{AccountId: accountID, ConfigurationId: config.ConfigurationId}, f.cookie, accountID))
			require.NoError(t, err)
			require.EqualValues(t, 1, current.Msg.Configuration.AssignedCount)
			t.Run("assignment review retains assigned count", func(t *testing.T) {
				review, err := f.configurations.ReviewAssignment(t.Context(), rpcRequest(&pb.ReviewAssignmentRequest{AccountId: accountID, ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version, DeviceIdentifiers: []string{identifier}}, f.cookie, accountID))
				require.NoError(t, err)
				require.Equal(t, current.Msg.Configuration.AssignedCount, review.Msg.Configuration.AssignedCount)
			})
			t.Run("retry review retains assigned count", func(t *testing.T) {
				_, err := f.db.ExecContext(t.Context(), `UPDATE pool_configuration_operation_target SET state='failed',result_code='unavailable',attempts=1 WHERE operation_id=$1`, saved.Msg.OperationId)
				require.NoError(t, err)
				review, err := f.configurations.ReviewRetry(t.Context(), rpcRequest(&pb.ReviewRetryRequest{AccountId: accountID, OperationId: saved.Msg.OperationId, Targets: []*pb.RetryTarget{{Kind: "device", TargetId: device}}}, f.cookie, accountID))
				require.NoError(t, err)
				require.Equal(t, current.Msg.Configuration.AssignedCount, review.Msg.Configuration.AssignedCount)
			})
		})
	}
}

func TestSharedPoolRPCSelectedAccountAssignmentReadsAndChecks(t *testing.T) {
	f := newRPCFixture(t)
	ctx := t.Context()
	username := "customer-" + uuid.NewString()
	user := f.addUser(t, username, false)
	account := f.account(t, "Customer", user, domain.DefaultCapabilities)
	other := f.account(t, "Other customer", f.host, domain.DefaultCapabilities)
	cookie := f.login(t, username).Header().Get("Set-Cookie")
	var discovery, device int64
	identifier := uuid.NewString()
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, f.org, identifier).Scan(&discovery))
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, f.org, identifier, discovery).Scan(&device))
	_, err := f.db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, f.org, device, account.ID)
	require.NoError(t, err)
	require.NoError(t, sqlc.New(f.db).EnsureDeviceWriteFences(ctx, []int64{device}))

	assignments, err := f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{AccountId: account.AccountID, DeviceIds: []int64{device}}, cookie, account.AccountID))
	require.NoError(t, err)
	require.EqualValues(t, 1, assignments.Msg.SelectedCount)
	require.Len(t, assignments.Msg.Assignments, 1)
	require.Equal(t, device, assignments.Msg.Assignments[0].DeviceId)
	checked, err := f.configurations.CheckConfiguration(ctx, rpcRequest(&pb.CheckConfigurationRequest{AccountId: account.AccountID, Target: &pb.RetryTarget{Kind: "device", TargetId: device}}, cookie, account.AccountID))
	require.NoError(t, err)
	require.False(t, checked.Msg.ReadbackAvailable)
	require.Equal(t, "check_unavailable", checked.Msg.ResultCode, "authorized check reaches the offline miner")

	batch, err := f.configurations.ReadDevicePools(ctx, rpcRequest(&pb.ReadDevicePoolsRequest{AccountId: account.AccountID, Targets: []*pb.RetryTarget{{Kind: "device", TargetId: device}}}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, batch.Msg.Readings, 1)
	require.Equal(t, device, batch.Msg.Readings[0].Target.TargetId)
	require.Equal(t, "unavailable", batch.Msg.Readings[0].Reading.State)
	require.Empty(t, batch.Msg.Readings[0].Reading.Reported)

	for _, tc := range []struct {
		name, body, header, cookie string
		code                       connect.Code
	}{
		{"missing selection", account.AccountID, "", cookie, connect.CodePermissionDenied},
		{"mismatched selection", account.AccountID, other.AccountID, cookie, connect.CodePermissionDenied},
		{"operator fallback", "", account.AccountID, cookie, connect.CodePermissionDenied},
		{"inaccessible account", other.AccountID, other.AccountID, cookie, connect.CodePermissionDenied},
		{"unauthenticated", account.AccountID, account.AccountID, "", connect.CodeUnauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{AccountId: tc.body, DeviceIds: []int64{device}}, tc.cookie, tc.header))
			require.Equal(t, tc.code, connect.CodeOf(err))
			_, err = f.configurations.ReadDevicePools(ctx, rpcRequest(&pb.ReadDevicePoolsRequest{AccountId: tc.body, Targets: []*pb.RetryTarget{{Kind: "device", TargetId: device}}}, tc.cookie, tc.header))
			require.Equal(t, tc.code, connect.CodeOf(err))

			_, err = f.configurations.CheckConfiguration(ctx, rpcRequest(&pb.CheckConfigurationRequest{AccountId: tc.body, Target: &pb.RetryTarget{Kind: "device", TargetId: device}}, tc.cookie, tc.header))
			require.Equal(t, tc.code, connect.CodeOf(err))
		})
	}
	// Even an operator authorized in both accounts cannot read another account's equipment.
	assignments, err = f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{AccountId: other.AccountID, DeviceIds: []int64{device}}, f.cookie, other.AccountID))
	require.NoError(t, err)
	require.Empty(t, assignments.Msg.Assignments)
	_, err = f.configurations.CheckConfiguration(ctx, rpcRequest(&pb.CheckConfigurationRequest{AccountId: other.AccountID, Target: &pb.RetryTarget{Kind: "device", TargetId: device}}, f.cookie, other.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	require.NoError(t, f.accounts.ConfigureChild(ctx, f.host, f.org, account.AccountID, domain.DefaultCapabilities&^domain.ChangePools, false))
	_, err = f.configurations.CheckConfiguration(ctx, rpcRequest(&pb.CheckConfigurationRequest{AccountId: account.AccountID, Target: &pb.RetryTarget{Kind: "device", TargetId: device}}, cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "readback still requires equipment authority")
	assignments, err = f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{AccountId: account.AccountID, DeviceIds: []int64{device}}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, assignments.Msg.Assignments, 1, "reading saved intent does not require ChangePools")
}

func TestSharedPoolRPCConnectionTestRestrictsOnlyCustomerDestinations(t *testing.T) {
	f := newRPCFixture(t)
	f.configurationService.WithConnectionTester(pools.NewService(pools.Config{Timeout: time.Second}))
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			var request json.RawMessage
			if json.NewDecoder(conn).Decode(&request) == nil {
				_, _ = conn.Write([]byte("{\"id\":1,\"result\":true}\n"))
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-finished })
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	input := &pb.PoolInput{Url: "stratum+tcp://" + net.JoinHostPort("127.0.0.1", port), Username: "payout"}
	tested, err := f.configurations.TestConnection(t.Context(), rpcRequest(&pb.TestConnectionRequest{AccountId: account.AccountID, Pool: input}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.False(t, tested.Msg.Connected, "customer tests must not reach a local pool, even when it is a valid pool server")
	tested, err = f.configurations.TestConnection(t.Context(), rpcRequest(&pb.TestConnectionRequest{Pool: input}, f.cookie, ""))
	require.NoError(t, err)
	require.True(t, tested.Msg.Connected, "operator testing retains private-pool support")
	// Destination restrictions belong to the probe, not configuration storage.
	saved, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{AccountId: account.AccountID, Configuration: &pb.ConfigurationInput{Name: "Private pool", Pools: []*pb.PoolInput{input}}}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, input.Url, saved.Msg.Configuration.Pools[0].Url)
}

func TestSharedPoolRPCExpiredUnacceptedReviewDetails(t *testing.T) {
	f := newRPCFixture(t)
	input := &pb.ConfigurationInput{Name: "Production", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout"}}}
	created, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	c := created.Msg.Configuration
	input.Pools[0].SlotId = c.Pools[0].SlotId
	input.Name = "Updated production"
	reviewed, err := f.configurations.ReviewEdit(t.Context(), rpcRequest(&pb.ReviewEditRequest{ConfigurationId: c.ConfigurationId, ExpectedVersion: c.Version, Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	expired := uuid.NewString()
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO pool_configuration_review(review_id,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,expires_at)
 SELECT $1,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,clock_timestamp()-interval '1 second' FROM pool_configuration_review WHERE review_id=$2`, expired, reviewed.Msg.ReviewId)
	require.NoError(t, err)
	_, err = f.configurations.ConfirmReview(t.Context(), rpcRequest(&pb.ConfirmReviewRequest{ReviewId: expired}, f.cookie, ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	var rpcError *connect.Error
	require.True(t, errors.As(err, &rpcError))
	require.Len(t, rpcError.Details(), 1)
	detail, err := rpcError.Details()[0].Value()
	require.NoError(t, err)
	fleetDetail, ok := detail.(*commonpb.FleetErrorDetails)
	require.True(t, ok)
	require.EqualValues(t, pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_EXPIRED_UNACCEPTED, fleetDetail.GetEndpoint())
}

func TestSharedPoolRPCWorkerBatchAuthorizationAndConfirmation(t *testing.T) {
	f := newRPCFixture(t)
	created, err := f.configurations.CreateConfiguration(t.Context(), rpcRequest(&pb.CreateConfigurationRequest{Configuration: &pb.ConfigurationInput{Name: "Worker config", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout.with.periods", WorkerTemplate: "{miner_name}"}}}}, f.cookie, ""))
	require.NoError(t, err)
	config := created.Msg.Configuration
	identifier := uuid.NewString()
	var discovery, device int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, f.org, identifier).Scan(&discovery))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, f.org, identifier, discovery).Scan(&device))
	_, err = sqlc.New(f.db).AdoptPoolConfigurationAssignment(t.Context(), sqlc.AdoptPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: "device", TargetID: device, DeviceID: device, ConfigurationID: config.ConfigurationId, RevisionID: config.RevisionId})
	require.NoError(t, err)
	ctx := authn.SetInfo(t.Context(), &session.Info{AuthMethod: session.AuthMethodSession, UserID: f.host, OrganizationID: f.org})
	reviews, err := f.configurationService.ReviewWorkerNames(ctx, map[string]string{identifier: "new-worker"})
	require.NoError(t, err)
	ids := []string{reviews[0].ID}
	_, err = f.configurations.AuthorizeWorkerNameReviews(t.Context(), rpcRequest(&pb.AuthorizeWorkerNameReviewsRequest{ReviewIds: ids, Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	_, err = f.configurations.AuthorizeWorkerNameReviews(t.Context(), rpcRequest(&pb.AuthorizeWorkerNameReviewsRequest{ReviewIds: []string{ids[0], ids[0]}, Password: rpcPassword}, f.cookie, ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = f.configurations.AuthorizeWorkerNameReviews(t.Context(), rpcRequest(&pb.AuthorizeWorkerNameReviewsRequest{ReviewIds: ids, Password: rpcPassword}, f.cookie, ""))
	require.NoError(t, err)
	var operations int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_configuration_operation`).Scan(&operations))
	require.Zero(t, operations, "authorization does not submit equipment updates")
	saved, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(&pb.ConfirmReviewRequest{ReviewId: ids[0]}, f.cookie, ""))
	require.NoError(t, err)
	require.NotEmpty(t, saved.Msg.OperationId)
	var worker string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT worker_name FROM device WHERE id=$1`, device).Scan(&worker))
	require.Equal(t, "new-worker", worker)
	replay, err := f.configurations.ConfirmReview(t.Context(), rpcRequest(&pb.ConfirmReviewRequest{ReviewId: ids[0]}, f.cookie, ""))
	require.NoError(t, err)
	require.Equal(t, saved.Msg.OperationId, replay.Msg.OperationId)
}
