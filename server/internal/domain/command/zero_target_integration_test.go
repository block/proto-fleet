package command_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func newZeroTargetDispatchTestService(t *testing.T, conn *sql.DB) *command.Service {
	t.Helper()

	commandConfig := &command.Config{
		MaxWorkers:                       1,
		MasterPollingInterval:            10 * time.Millisecond,
		WorkerExecutionTimeout:           time.Second,
		BatchStatusUpdatePollingInterval: 10 * time.Millisecond,
		DequeueRetries:                   0,
		StuckMessageTimeout:              time.Hour,
		ReaperInterval:                   time.Hour,
	}
	queueConfig := &queue.Config{
		DequeLimit:        10,
		MaxFailureRetries: 1,
	}
	messageQueue := queue.NewDatabaseMessageQueue(queueConfig, conn)
	executionCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	executionService := command.NewExecutionService(
		executionCtx,
		commandConfig,
		conn,
		messageQueue,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, executionService.Start(executionCtx))

	return command.NewService(
		commandConfig,
		conn,
		executionService,
		messageQueue,
		command.NewStatusService(conn, messageQueue),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func countCommandBatches(t *testing.T, conn *sql.DB, orgID int64) int {
	t.Helper()
	var count int
	err := conn.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM command_batch_log WHERE organization_id = $1`,
		orgID,
	).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestSetPowerTarget_UnresolvedIncludeSelector_ReturnsInvalidArgumentBeforeBatchCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, user := setupRetentionTest(t)
	svc := newZeroTargetDispatchTestService(t, conn)

	deletedDevice := dbService.CreateDevice(user.OrganizationID, "proto")
	_, err := conn.ExecContext(
		context.Background(),
		`UPDATE device SET deleted_at = NOW() WHERE id = $1`,
		deletedDevice.DatabaseID,
	)
	require.NoError(t, err)

	before := countCommandBatches(t, conn, user.OrganizationID)
	authCtx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)
	resp, err := svc.SetPowerTarget(
		authCtx,
		&pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonpb.DeviceIdentifierList{
					DeviceIdentifiers: []string{"missing-device", deletedDevice.ID},
				},
			},
		},
		pb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY,
	)

	require.Nil(t, resp)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "no devices matched selector")
	assert.Equal(t, before, countCommandBatches(t, conn, user.OrganizationID))
}

func TestSetPowerTarget_SchedulerStaleTargets_ReturnsNoOpBeforeBatchCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)
	svc := newZeroTargetDispatchTestService(t, conn)

	before := countCommandBatches(t, conn, user.OrganizationID)
	schedulerCtx := authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "scheduler",
		UserID:         user.DatabaseID,
		OrganizationID: user.OrganizationID,
		ExternalUserID: "scheduler",
		Username:       "scheduler",
		Actor:          session.ActorScheduler,
		Source:         session.Source{ScheduleID: 99, SchedulePriority: 5},
	})
	resp, err := svc.SetPowerTarget(
		schedulerCtx,
		&pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonpb.DeviceIdentifierList{
					DeviceIdentifiers: []string{"missing-device"},
				},
			},
		},
		pb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY,
	)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.BatchIdentifier)
	assert.Zero(t, resp.DispatchedCount)
	assert.Empty(t, resp.Skipped)
	assert.Equal(t, before, countCommandBatches(t, conn, user.OrganizationID))
}
