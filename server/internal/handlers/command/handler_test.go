package command_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	handler "github.com/block/proto-fleet/server/internal/handlers/command"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// TODO: Rewrite the broader command handler tests using plugin-based test
// infrastructure. The tests below are narrow to GetCommandBatchDeviceResults
// and don't need plugin support.
func TestCommandHandler(t *testing.T) {
	t.Skip("Disabled pending plugin-based test infrastructure")
}

// TestHandler_GetCommandBatchDeviceResults_PassesThroughHappyPath builds the
// handler on top of a thin command.Service and exercises the full
// connect.Request / Response shape end-to-end. The service-level tests in
// results_integration_test.go cover the detailed branches; this asserts the
// handler correctly delegates and propagates.
func TestHandler_GetCommandBatchDeviceResults_PassesThroughHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()
	dev := dbService.CreateDevice(user.OrganizationID, "proto")

	batchUUID := "handler-happy-1"
	ctx := context.Background()
	require.NoError(t, db2.WithTransactionNoResult(ctx, dbService.DB, func(q *sqlc.Queries) error {
		_, e := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:           batchUUID,
			Type:           "REBOOT",
			CreatedBy:      user.DatabaseID,
			CreatedAt:      time.Now(),
			Status:         sqlc.BatchStatusEnumFINISHED,
			DevicesCount:   1,
			Payload:        pqtype.NullRawMessage{Valid: false},
			OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true},
		})
		return e
	}))
	_, err = dbService.DB.ExecContext(ctx,
		`UPDATE command_batch_log SET finished_at = NOW() WHERE uuid = $1`, batchUUID)
	require.NoError(t, err)
	require.NoError(t, db2.WithTransactionNoResult(ctx, dbService.DB, func(q *sqlc.Queries) error {
		return q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid:      batchUUID,
			DeviceID:  dev.DatabaseID,
			Status:    sqlc.DeviceCommandStatusEnumSUCCESS,
			UpdatedAt: time.Now(),
		})
	}))

	svc := command.NewService(
		&command.Config{}, dbService.DB,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	h := handler.NewHandler(svc)

	req := connect.NewRequest(&pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	authCtx := testutil.MockAuthContextForTesting(ctx, user.DatabaseID, user.OrganizationID)

	resp, err := h.GetCommandBatchDeviceResults(authCtx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, batchUUID, resp.Msg.BatchIdentifier)
	assert.Equal(t, int32(1), resp.Msg.SuccessCount)
	assert.Len(t, resp.Msg.DeviceResults, 1)
}

// TestHandler_GetCommandBatchDeviceResults_PropagatesInvalidArgument ensures
// the handler surfaces the service's FleetError unchanged so the interceptor
// can map it to the proper connect.Code. (The interceptor itself is covered
// by its own tests.)
func TestHandler_GetCommandBatchDeviceResults_PropagatesInvalidArgument(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()

	svc := command.NewService(
		&command.Config{}, dbService.DB,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	h := handler.NewHandler(svc)

	req := connect.NewRequest(&pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: "",
	})
	authCtx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	_, err = h.GetCommandBatchDeviceResults(authCtx, req)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError to propagate through the handler")
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}
