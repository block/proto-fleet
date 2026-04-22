package command_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	activityDomain "github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

// TestCompletionReconciler_BackfillsMissingCompletion end-to-end: seed a
// FINISHED batch with an initiated activity row but no completion row, run the
// reconciler once, and assert the completion row exists with correct counts
// and attribution copied from the initiated row.
func TestCompletionReconciler_BackfillsMissingCompletion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, user := setupRetentionTest(t)
	dev1 := dbService.CreateDevice(user.OrganizationID, "proto")
	dev2 := dbService.CreateDevice(user.OrganizationID, "proto")

	batchUUID := "reconciler-e2e-1"
	// Finish "long enough ago" that we clear the reconciler's grace period.
	finishedAt := time.Now().Add(-10 * time.Minute)
	seedFinishedBatch(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 2, finishedAt)

	seedDeviceLog(t, conn, batchUUID, dev1.DatabaseID, sqlc.DeviceCommandStatusEnumSUCCESS, finishedAt)
	seedDeviceLog(t, conn, batchUUID, dev2.DatabaseID, sqlc.DeviceCommandStatusEnumFAILED, finishedAt)
	// Persist an error_info for the failed row so the reconciler's counts
	// query still classifies it as failed (sanity check only; reconciler
	// doesn't read error_info directly).
	_, err := conn.ExecContext(context.Background(),
		`UPDATE command_on_device_log SET error_info = 'boom' WHERE device_id = $1`,
		dev2.DatabaseID)
	require.NoError(t, err)

	// Seed the initiated activity row the reconciler keys off.
	activityStore := sqlstores.NewSQLActivityStore(conn)
	activitySvc := activityDomain.NewService(activityStore)
	batchIDCopy := batchUUID
	userID := user.ExternalUserID
	username := user.Username
	orgID := user.OrganizationID
	scope := 2
	activitySvc.Log(context.Background(), activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           "reboot",
		Description:    "Reboot",
		ScopeCount:     &scope,
		UserID:         &userID,
		Username:       &username,
		OrganizationID: &orgID,
		BatchID:        &batchIDCopy,
		Metadata:       map[string]any{"batch_id": batchUUID},
	})

	// Confirm no completion row exists yet.
	assert.Zero(t, countWhere(t, conn,
		`SELECT COUNT(*) FROM activity_log WHERE batch_id = $1 AND event_type = 'reboot.completed'`,
		batchUUID))

	cfg := &command.Config{
		ReconcilerInterval:    time.Hour,
		ReconcilerGracePeriod: time.Minute,
		ReconcilerMaxBatches:  10,
	}
	reconciler := command.NewCompletionReconciler(conn, cfg, activitySvc)
	require.NoError(t, reconciler.RunOnceForTest(context.Background()))

	// Completion row should now exist with the right counts and metadata.
	var (
		eventType   string
		result      string
		description string
		orgIDRow    sql.NullInt64
		userIDRow   sql.NullString
		metadata    pqtype.NullRawMessage
	)
	err = conn.QueryRowContext(context.Background(),
		`SELECT event_type, result, description, organization_id, user_id, metadata
		 FROM activity_log
		 WHERE batch_id = $1 AND event_type = 'reboot.completed'`,
		batchUUID).Scan(&eventType, &result, &description, &orgIDRow, &userIDRow, &metadata)
	require.NoError(t, err)
	assert.Equal(t, "reboot.completed", eventType)
	assert.Equal(t, "failure", result, "any failure forces overall result=failure")
	assert.Contains(t, description, "Reboot completed: 1 succeeded, 1 failed")
	require.True(t, orgIDRow.Valid)
	assert.Equal(t, orgID, orgIDRow.Int64)
	require.True(t, userIDRow.Valid)
	assert.Equal(t, userID, userIDRow.String)
	require.True(t, metadata.Valid, "completion row must carry metadata")

	// Second pass is a no-op thanks to the partial unique index + store swallow.
	require.NoError(t, reconciler.RunOnceForTest(context.Background()))
	assert.Equal(t, 1, countWhere(t, conn,
		`SELECT COUNT(*) FROM activity_log WHERE batch_id = $1 AND event_type = 'reboot.completed'`,
		batchUUID))
}

// TestCompletionReconciler_PrunedDeviceLogsYieldResultUnknown verifies the
// reconciler handles the retention-gap case honestly: a FINISHED batch whose
// header still exists but whose per-device rows have been aged out by the
// command retention cleaner (90d default codl retention vs 180d cbl retention
// leaves a window where this is possible after a reconciler outage).
//
// Without this guard the reconciler would write a completion row with
// result=success and "0 succeeded, 0 failed", which is a lie -- the batch did
// have devices, we just no longer know how they fared. The expected behavior
// is a row with result=unknown, a descriptive message, and a
// device_logs_pruned metadata flag so operators can tell the reconciled state
// apart from a live 0/0 batch.
func TestCompletionReconciler_PrunedDeviceLogsYieldResultUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, _, user := setupRetentionTest(t)

	batchUUID := "reconciler-pruned-1"
	finishedAt := time.Now().Add(-10 * time.Minute)
	// devices_count=3 but we deliberately seed NO command_on_device_log rows
	// to simulate a batch whose codl rows have been retention-pruned.
	seedFinishedBatch(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 3, finishedAt)

	activityStore := sqlstores.NewSQLActivityStore(conn)
	activitySvc := activityDomain.NewService(activityStore)
	batchIDCopy := batchUUID
	userID := user.ExternalUserID
	username := user.Username
	orgID := user.OrganizationID
	scope := 3
	activitySvc.Log(context.Background(), activitymodels.Event{
		Category:       activitymodels.CategoryDeviceCommand,
		Type:           "reboot",
		Description:    "Reboot",
		ScopeCount:     &scope,
		UserID:         &userID,
		Username:       &username,
		OrganizationID: &orgID,
		BatchID:        &batchIDCopy,
		Metadata:       map[string]any{"batch_id": batchUUID},
	})

	cfg := &command.Config{
		ReconcilerInterval:    time.Hour,
		ReconcilerGracePeriod: time.Minute,
		ReconcilerMaxBatches:  10,
	}
	reconciler := command.NewCompletionReconciler(conn, cfg, activitySvc)
	require.NoError(t, reconciler.RunOnceForTest(context.Background()))

	var (
		eventType   string
		result      string
		description string
		metadata    pqtype.NullRawMessage
	)
	err := conn.QueryRowContext(context.Background(),
		`SELECT event_type, result, description, metadata
		 FROM activity_log
		 WHERE batch_id = $1 AND event_type = 'reboot.completed'`,
		batchUUID).Scan(&eventType, &result, &description, &metadata)
	require.NoError(t, err, "reconciler must still write a completion row so the batch is closed")
	assert.Equal(t, "reboot.completed", eventType)
	assert.Equal(t, "unknown", result,
		"pruned batch cannot honestly be marked success or failure")
	assert.Contains(t, description, "per-device detail no longer available",
		"description must explain why the outcome is unknown")

	require.True(t, metadata.Valid, "metadata must be present")
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metadata.RawMessage, &meta))
	assert.Equal(t, true, meta["device_logs_pruned"],
		"metadata must carry device_logs_pruned=true so operators can distinguish")
	assert.Equal(t, true, meta["reconciled"])
	assert.NotContains(t, meta, "success_count",
		"pruned rows must not claim counts they cannot substantiate")
	assert.NotContains(t, meta, "failure_count")

	// Second pass is a no-op thanks to the partial unique index; the
	// reconciler must not revisit this batch every tick forever.
	require.NoError(t, reconciler.RunOnceForTest(context.Background()))
	assert.Equal(t, 1, countWhere(t, conn,
		`SELECT COUNT(*) FROM activity_log WHERE batch_id = $1 AND event_type = 'reboot.completed'`,
		batchUUID))
}

// TestCompletionReconciler_SkipsInternallyTriggeredBatches confirms the
// reconciler does not invent a completion row for batches that have no
// initiated activity row (e.g. worker-name reapply).
func TestCompletionReconciler_SkipsInternallyTriggeredBatches(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, user := setupRetentionTest(t)
	dev := dbService.CreateDevice(user.OrganizationID, "proto")

	batchUUID := "reconciler-no-initiated-1"
	seedFinishedBatch(t, conn, batchUUID, user.DatabaseID, user.OrganizationID, 1, time.Now().Add(-10*time.Minute))
	seedDeviceLog(t, conn, batchUUID, dev.DatabaseID, sqlc.DeviceCommandStatusEnumSUCCESS, time.Now().Add(-10*time.Minute))

	activityStore := sqlstores.NewSQLActivityStore(conn)
	activitySvc := activityDomain.NewService(activityStore)

	cfg := &command.Config{
		ReconcilerInterval:    time.Hour,
		ReconcilerGracePeriod: time.Minute,
		ReconcilerMaxBatches:  10,
	}
	reconciler := command.NewCompletionReconciler(conn, cfg, activitySvc)
	require.NoError(t, reconciler.RunOnceForTest(context.Background()))

	assert.Zero(t, countWhere(t, conn,
		`SELECT COUNT(*) FROM activity_log WHERE batch_id = $1`, batchUUID),
		"no initiated row -> reconciler must not invent a completion row")
}
