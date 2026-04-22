package command_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
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

// insertCompletionActivity is a thin test seed for a '*.completed'
// activity_log row. Used by the header-aged-out tests to simulate the
// state where command_batch_log has been retention-pruned but the activity
// row is still live (default BatchLogRetention=180d < ActivityLogRetention=365d).
func insertCompletionActivity(
	t *testing.T,
	conn *sql.DB,
	batchID string,
	orgID int64,
	eventType string,
	result string,
	totalCount int32,
	metadata map[string]any,
) {
	t.Helper()
	ctx := context.Background()
	eventID, err := uuid.NewV7()
	require.NoError(t, err)

	rawMeta, err := json.Marshal(metadata)
	require.NoError(t, err)

	err = db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		return q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
			EventID:        eventID,
			EventCategory:  string(activitymodels.CategoryDeviceCommand),
			EventType:      eventType,
			Description:    "seed activity row",
			Result:         result,
			ErrorMessage:   sql.NullString{Valid: false},
			ScopeType:      sql.NullString{Valid: false},
			ScopeLabel:     sql.NullString{Valid: false},
			ScopeCount:     sql.NullInt32{Int32: totalCount, Valid: true},
			ActorType:      string(activitymodels.ActorUser),
			UserID:         sql.NullString{Valid: false},
			Username:       sql.NullString{Valid: false},
			OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
			Metadata:       pqtype.NullRawMessage{RawMessage: rawMeta, Valid: true},
			BatchID:        sql.NullString{String: batchID, Valid: true},
		})
	})
	require.NoError(t, err)
}

// TestGetCommandBatchDeviceResults_BatchHeaderAgedOut verifies the S3 / R15
// path: when command_batch_log has been retention-pruned but the
// '*.completed' activity row is still present in the same org, the RPC
// synthesizes a details_pruned=true response from the activity metadata
// instead of returning NotFound. The user sees a graceful "detail aged
// out" state rather than a 404 on an entry that's still listed in the
// activity timeline.
func TestGetCommandBatchDeviceResults_BatchHeaderAgedOut(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	batchUUID := "results-aged-out-finalizer"
	// Simulate the reconciler/finalizer path (R14/M4 non-pruned case):
	// known counts in the metadata.
	insertCompletionActivity(t, conn, batchUUID, user.OrganizationID,
		"reboot"+activitymodels.CompletedEventSuffix,
		string(activitymodels.ResultFailure), // at least one device failed
		3,
		map[string]any{
			"batch_id":      batchUUID,
			"total_count":   3,
			"success_count": 2,
			"failure_count": 1,
		},
	)

	// No batch_log or codl rows seeded -- the header is genuinely absent.

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err, "header-aged-out path must not 404 when activity row exists")
	assert.Equal(t, batchUUID, resp.BatchIdentifier)
	assert.Equal(t, "reboot", resp.CommandType, "command_type must be stripped from event_type")
	assert.Equal(t, string(sqlc.BatchStatusEnumFINISHED), resp.Status,
		"a completion row means the batch was FINISHED")
	assert.Equal(t, int32(3), resp.TotalCount)
	assert.Equal(t, int32(2), resp.SuccessCount)
	assert.Equal(t, int32(1), resp.FailureCount)
	assert.True(t, resp.DetailsPruned, "details must be marked pruned when header is gone")
	assert.False(t, resp.Truncated)
	assert.Empty(t, resp.DeviceResults, "device_results must be empty without a header")
}

// TestGetCommandBatchDeviceResults_BatchHeaderAgedOut_ResultUnknown covers
// the R14/M4 reconciler path: if the reconciler backfilled a ResultUnknown
// completion row (because codl was already pruned at backfill time) and
// then the batch header itself ages out, the RPC surfaces zeros for
// success/failure instead of asserting a best-case outcome the server no
// longer knows.
func TestGetCommandBatchDeviceResults_BatchHeaderAgedOut_ResultUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	batchUUID := "results-aged-out-unknown"
	insertCompletionActivity(t, conn, batchUUID, user.OrganizationID,
		"reboot"+activitymodels.CompletedEventSuffix,
		string(activitymodels.ResultUnknown),
		3,
		map[string]any{
			"batch_id":           batchUUID,
			"total_count":        3,
			"device_logs_pruned": true,
			"reconciled":         true,
			// success_count / failure_count intentionally omitted -- the
			// reconciler cannot substantiate them once codl is gone.
		},
	)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), user.DatabaseID, user.OrganizationID)

	resp, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.NoError(t, err)
	assert.Equal(t, batchUUID, resp.BatchIdentifier)
	assert.Equal(t, int32(3), resp.TotalCount)
	assert.Equal(t, int32(0), resp.SuccessCount,
		"ResultUnknown metadata omits success_count; RPC must not invent one")
	assert.Equal(t, int32(0), resp.FailureCount,
		"ResultUnknown metadata omits failure_count; RPC must not invent one")
	assert.True(t, resp.DetailsPruned)
	assert.Empty(t, resp.DeviceResults)
}

// TestGetCommandBatchDeviceResults_BatchHeaderAgedOut_CrossOrgStill404
// confirms the aged-out fallback is org-scoped: an attacker in Org B
// holding the UUID of a batch whose header and all codl rows are gone
// cannot learn that it ever existed just because the activity row in
// Org A is still live.
func TestGetCommandBatchDeviceResults_BatchHeaderAgedOut_CrossOrgStill404(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, orgAUser := setupRetentionTest(t)
	orgBUser := dbService.CreateSuperAdminUser2()

	batchUUID := "results-aged-out-crossorg"
	insertCompletionActivity(t, conn, batchUUID, orgAUser.OrganizationID,
		"reboot"+activitymodels.CompletedEventSuffix,
		string(activitymodels.ResultSuccess),
		1,
		map[string]any{
			"batch_id":      batchUUID,
			"total_count":   1,
			"success_count": 1,
			"failure_count": 0,
		},
	)

	svc := newResultsTestService(conn)
	ctx := testutil.MockAuthContextForTesting(context.Background(), orgBUser.DatabaseID, orgBUser.OrganizationID)

	_, err := svc.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{
		BatchIdentifier: batchUUID,
	})
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr))
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode,
		"cross-org caller must still see NotFound, even on the fallback path")
}
