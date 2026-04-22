package command_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Integration coverage for GetCommandBatchDeviceResults. Exercises the
// authorization, details_pruned, and truncation semantics end-to-end against
// a real Postgres.

// newResultsTestService builds a command.Service wired with the bare minimum
// dependencies the RPC actually touches (conn + config). All other services
// are nil on purpose so a test failure that reaches them shows up loudly.
func newResultsTestService(conn *sql.DB) *command.Service {
	return command.NewService(
		&command.Config{},
		conn,
		nil, // executionService
		nil, // messageQueue
		nil, // statusService
		nil, // encryptService
		nil, // filesService
		nil, // deviceStore
		nil, // userStore
		nil, // credentialsVerifier
		nil, // telemetryListener
		nil, // capabilitiesProvider
		nil, // activitySvc
	)
}

// seedBatchInState creates a command_batch_log in the given state + org. Used
// by the results-RPC tests instead of seedFinishedBatch because several tests
// need PENDING / PROCESSING.
func seedBatchInState(t *testing.T, conn *sql.DB, batchUUID string, userID, orgID int64, deviceCount int32, status sqlc.BatchStatusEnum) {
	t.Helper()
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:           batchUUID,
			Type:           "REBOOT",
			CreatedBy:      userID,
			CreatedAt:      time.Now(),
			Status:         status,
			DevicesCount:   deviceCount,
			Payload:        pqtype.NullRawMessage{Valid: false},
			OrganizationID: sql.NullInt64{Int64: orgID, Valid: orgID != 0},
		})
		return err
	})
	require.NoError(t, err)
	if status == sqlc.BatchStatusEnumFINISHED {
		_, err := conn.ExecContext(ctx,
			`UPDATE command_batch_log SET finished_at = NOW() WHERE uuid = $1`, batchUUID)
		require.NoError(t, err)
	}
}

func TestGetCommandBatchDeviceResults_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, user := setupRetentionTest(t)
	dev1 := dbService.CreateDevice(user.OrganizationID, "proto")
	dev2 := dbService.CreateDevice(user.OrganizationID, "proto")

	batchUUID := "results-happy-1"
	seedBatchInState(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 2, sqlc.BatchStatusEnumFINISHED)
	seedDeviceLog(t, conn, batchUUID, dev1.DatabaseID, sqlc.DeviceCommandStatusEnumSUCCESS, time.Now())
	seedDeviceLog(t, conn, batchUUID, dev2.DatabaseID, sqlc.DeviceCommandStatusEnumFAILED, time.Now())
	_, err := conn.ExecContext(context.Background(),
		`UPDATE command_on_device_log SET error_info = 'plugin exploded' WHERE device_id = $1`, dev2.DatabaseID)
	require.NoError(t, err)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err)
	assert.Equal(t, batchUUID, resp.BatchIdentifier)
	assert.Equal(t, "REBOOT", resp.CommandType)
	assert.Equal(t, string(sqlc.BatchStatusEnumFINISHED), resp.Status)
	assert.Equal(t, int32(2), resp.TotalCount)
	assert.Equal(t, int32(1), resp.SuccessCount)
	assert.Equal(t, int32(1), resp.FailureCount)
	assert.Len(t, resp.DeviceResults, 2)
	assert.False(t, resp.DetailsPruned, "FINISHED with rows must not be pruned")
	assert.False(t, resp.Truncated, "2 rows must not trigger the 5000 cap")

	// Confirm the FAILED row carries its error_info through to the RPC.
	var failure *pb.CommandBatchDeviceResult
	for _, r := range resp.DeviceResults {
		if r.Status == "failed" {
			failure = r
		}
	}
	require.NotNil(t, failure)
	require.NotNil(t, failure.ErrorMessage)
	assert.Equal(t, "plugin exploded", *failure.ErrorMessage)
}

func TestGetCommandBatchDeviceResults_NotFoundForCrossOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, orgAUser := setupRetentionTest(t)
	orgBUser := dbService.CreateSuperAdminUser2()

	batchUUID := "results-cross-org-1"
	seedBatchInState(t, conn, batchUUID, orgAUser.DatabaseID, orgAUser.OrganizationID, 1, sqlc.BatchStatusEnumFINISHED)

	svc := newResultsTestService(conn)
	// Caller is in Org B; the batch was recorded under Org A.
	ctx := testutil.MockAuthContextForTesting(context.Background(), orgBUser.DatabaseID, orgBUser.OrganizationID)

	_, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.Error(t, err)
	// Whether wrapped or surfaced directly, the error maps to connect.CodeNotFound.
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestGetCommandBatchDeviceResults_DetailsPrunedWhenFinishedWithNoRows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	batchUUID := "results-pruned-1"
	seedBatchInState(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 3, sqlc.BatchStatusEnumFINISHED)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err)
	assert.True(t, resp.DetailsPruned, "FINISHED with devices_count>0 and no codl rows must be pruned")
	assert.Empty(t, resp.DeviceResults)
	assert.Equal(t, int32(3), resp.TotalCount)
}

func TestGetCommandBatchDeviceResults_NotPrunedWhilePending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	batchUUID := "results-pending-1"
	seedBatchInState(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 2, sqlc.BatchStatusEnumPENDING)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err)
	assert.False(t, resp.DetailsPruned, "mid-run batches must not report pruned")
}

func TestGetCommandBatchDeviceResults_NotPrunedForEmptySelector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	batchUUID := "results-empty-selector-1"
	// devices_count=0 -- a FINISHED batch that matched no miners. We must not
	// claim its details are pruned.
	seedBatchInState(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 0, sqlc.BatchStatusEnumFINISHED)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err)
	assert.False(t, resp.DetailsPruned, "devices_count=0 batches never had details to prune")
	assert.Equal(t, int32(0), resp.TotalCount)
}

// TestGetCommandBatchDeviceResults_TruncatesLargeBatchesWithConsistentCounts
// exercises the M1 SQL-enforced LIMIT: the query reads at most
// maxBatchDeviceResults+1 rows, so truncation is detected server-side (via
// `len(rows) > cap`) without materializing the full list in driver memory.
// Aggregate counts come from the separate GetBatchDeviceCounts query and are
// therefore always accurate regardless of truncation.
func TestGetCommandBatchDeviceResults_TruncatesLargeBatchesWithConsistentCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, user := setupRetentionTest(t)
	const deviceCount = 5100 // over the 5000 cap; SQL reads 5001 and Go slices to 5000

	batchUUID := "results-truncate-1"
	seedBatchInState(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, int32(deviceCount), sqlc.BatchStatusEnumFINISHED)

	devs := make([]testutil.DeviceIdentification, 0, deviceCount)
	for i := 0; i < deviceCount; i++ {
		devs = append(devs, dbService.CreateDevice(user.OrganizationID, "proto"))
	}

	// Bulk-insert codl rows in chunks so the test doesn't hammer sqlc one-by-one.
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		for _, dev := range devs {
			if err := q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
				Uuid:      batchUUID,
				DeviceID:  dev.DatabaseID,
				Status:    sqlc.DeviceCommandStatusEnumSUCCESS,
				UpdatedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("upserting codl for device %d: %w", dev.DatabaseID, err)
			}
		}
		return nil
	})
	require.NoError(t, err)

	svc := newResultsTestService(conn)
	rpcCtx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(rpcCtx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err)
	assert.True(t, resp.Truncated)
	assert.Len(t, resp.DeviceResults, 5000, "device_results must be capped at maxBatchDeviceResults")
	assert.Equal(t, int32(deviceCount), resp.TotalCount)
	assert.Equal(t, int32(deviceCount), resp.SuccessCount)
	assert.Equal(t, int32(0), resp.FailureCount)
	assert.Equal(t, resp.TotalCount, resp.SuccessCount+resp.FailureCount,
		"counts must sum to TotalCount regardless of truncation")
}

func TestGetCommandBatchDeviceResults_InvalidArgumentOnEmptyIdentifier(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	_, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: "   ",
	})
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}
