package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This exercises real HTTP sessions, password confirmation, protobuf validation
// and SQL persistence; firmware execution is covered by the reconciler tests.
func TestSharedPoolRPCBoardSelectionAndPersistentAssignment(t *testing.T) {
	f := newRPCFixture(t)
	ctx := t.Context()
	identifier := uuid.NewString()
	var discovery, device, tank, module, board int64
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','9095','http','proto',2) RETURNING id`, f.org, identifier).Scan(&discovery))
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,discovered_device_id,mac_address) VALUES($1,$2,$3,'02:00:00:00:00:99') RETURNING id`, f.org, identifier, discovery).Scan(&device))
	_, err := f.db.ExecContext(ctx, `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,now())`, device, f.org)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device).Scan(&tank))
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'module',now(),now()) RETURNING id`, tank).Scan(&module))
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.org).Scan(&board))
	_, err = f.db.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,1,now(),now(),'{"macAddress":"02:00:00:00:00:01"}')`, board, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,1,'occupied',now()),($1,NULL,2,'empty',now()),($1,NULL,3,'empty',now())`, module, board)
	require.NoError(t, err)
	for _, statement := range []string{
		`UPDATE container_module SET last_seen_at=(SELECT observed_at FROM container_inventory WHERE device_id=$1) WHERE id=$2`,
		`UPDATE hashboard_slot SET observed_at=(SELECT observed_at FROM container_inventory WHERE device_id=$1) WHERE module_id=$2`,
		`INSERT INTO hashboard_observation(hashboard_id,device_id,last_seen_at) SELECT hashboard_id,$1,now() FROM hashboard_installation WHERE module_id=$2 AND removed_at IS NULL`,
	} {
		_, err := f.db.ExecContext(ctx, statement, device, module)
		require.NoError(t, err)
	}
	require.NoError(t, sqlc.New(f.db).EnsureDeviceWriteFences(ctx, []int64{device}))
	_, err = f.db.ExecContext(ctx, `INSERT INTO container_mining_group_state(device_id,organization_id,observed,observed_at) VALUES($1,$2,'{"supported":true,"enabled":true,"max_groups":4}',now())`, device, f.org)
	require.NoError(t, err)
	input := &pb.ConfigurationInput{Name: "Shared boards", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example:3333", Username: "payout", WorkerTemplate: "{miner_name}"}}}
	created, err := f.configurations.CreateConfiguration(ctx, rpcRequest(&pb.CreateConfigurationRequest{Configuration: input}, f.cookie, ""))
	require.NoError(t, err)
	config := created.Msg.Configuration
	selection := &pb.BoardSelection{ModuleIds: []int64{module}, HashboardIds: []int64{board}}
	review, err := f.configurations.ReviewAssignment(ctx, rpcRequest(&pb.ReviewAssignmentRequest{ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version, Boards: selection}, f.cookie, ""))
	require.NoError(t, err)
	require.EqualValues(t, 1, review.Msg.AffectedHashboards)
	require.Zero(t, review.Msg.AffectedMiners)
	_, err = f.configurations.ConfirmReview(ctx, rpcRequest(&pb.ConfirmReviewRequest{ReviewId: review.Msg.ReviewId}, f.cookie, ""))
	require.Error(t, err)
	saved, err := f.configurations.ConfirmReview(ctx, rpcRequest(&pb.ConfirmReviewRequest{ReviewId: review.Msg.ReviewId, Password: rpcPassword}, f.cookie, ""))
	require.NoError(t, err)
	current, err := f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{Boards: selection}, f.cookie, ""))
	require.NoError(t, err)
	require.EqualValues(t, 1, current.Msg.SelectedCount)
	require.Len(t, current.Msg.Assignments, 1)
	assignment := current.Msg.Assignments[0]
	require.Equal(t, config.ConfigurationId, assignment.ConfigurationId)
	require.Equal(t, saved.Msg.OperationId, assignment.OperationId)
	require.Equal(t, "pending", assignment.State)
	require.Equal(t, board, assignment.HashboardId)
	require.NotContains(t, string(mustProtoJSON(t, current.Msg)), rpcPassword)
	_, err = f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{Boards: selection, DeviceIdentifiers: []string{identifier}}, f.cookie, ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = f.configurations.GetAssignments(ctx, rpcRequest(&pb.GetAssignmentsRequest{Boards: selection}, "", ""))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}
