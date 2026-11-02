package hostedaccounts_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolRPCOperationHistoryValidatesCursorAndPaginates(t *testing.T) {
	f := newRPCFixture(t)
	ctx := t.Context()
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	create := func(accountID, name string) *pb.Configuration {
		t.Helper()
		result, err := f.configurations.CreateConfiguration(ctx, rpcRequest(&pb.CreateConfigurationRequest{AccountId: accountID, Configuration: &pb.ConfigurationInput{Name: name, Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout"}}}}, f.cookie, accountID))
		require.NoError(t, err)
		return result.Msg.Configuration
	}
	configuration := create("", "Production")
	other := create("", "Other configuration")
	private := create(account.AccountID, "Private configuration")
	var discovery, device int64
	identifier := uuid.NewString()
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, f.org, identifier).Scan(&discovery))
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, f.org, identifier, discovery).Scan(&device))
	// One transaction gives every history entry the same timestamp, exercising
	// the operation-ID tie breaker as well as the 25-entry page boundary.
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	q := sqlc.New(tx)
	seedOperation := func(config *pb.Configuration) string {
		t.Helper()
		operation, err := q.CreatePoolConfigurationOperation(ctx, sqlc.CreatePoolConfigurationOperationParams{OperationID: uuid.NewString(), OrganizationID: f.org, ConfigurationID: config.ConfigurationId, RevisionID: config.RevisionId, ActorID: f.host})
		require.NoError(t, err)
		assignment, err := q.SetPoolConfigurationAssignment(ctx, sqlc.SetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: "device", TargetID: device, DeviceID: device, ConfigurationID: config.ConfigurationId, RevisionID: config.RevisionId, OperationID: operation.OperationID})
		require.NoError(t, err)
		_, err = q.CreatePoolConfigurationOperationTarget(ctx, sqlc.CreatePoolConfigurationOperationTargetParams{OrganizationID: f.org, OperationID: operation.OperationID, TargetKind: "device", TargetID: device, AssignmentVersion: assignment.Version})
		require.NoError(t, err)
		return operation.OperationID
	}
	ids := make([]string, 27)
	for i := range ids {
		ids[i] = seedOperation(configuration)
	}
	otherID, privateID := seedOperation(other), seedOperation(private)
	require.NoError(t, tx.Commit())
	slices.Sort(ids)
	slices.Reverse(ids)
	list := func(token string) (*connect.Response[pb.ListOperationsResponse], error) {
		return f.configurations.ListOperations(ctx, rpcRequest(&pb.ListOperationsRequest{ConfigurationId: configuration.ConfigurationId, PageToken: token}, f.cookie, ""))
	}
	t.Run("valid pages including tied timestamps", func(t *testing.T) {
		first, err := list("")
		require.NoError(t, err)
		require.Len(t, first.Msg.Operations, 25)
		require.Equal(t, ids[24], first.Msg.NextPageToken)
		second, err := list(first.Msg.NextPageToken)
		require.NoError(t, err)
		require.Len(t, second.Msg.Operations, 2)
		require.Empty(t, second.Msg.NextPageToken)
		var actual []string
		for _, page := range []*pb.ListOperationsResponse{first.Msg, second.Msg} {
			for _, operation := range page.Operations {
				actual = append(actual, operation.OperationId)
			}
		}
		require.Equal(t, ids, actual, "pages must not skip or repeat history entries")
		last, err := list(ids[len(ids)-1])
		require.NoError(t, err)
		require.Empty(t, last.Msg.Operations, "a valid cursor at the end is still an empty page")
	})
	for _, test := range []struct{ name, token string }{
		{"malformed", "not-an-operation-id"},
		{"unknown", uuid.NewString()},
		{"another configuration", otherID},
		{"another owner", privateID},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := list(test.token)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}
