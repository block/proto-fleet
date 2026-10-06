package command

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

func TestFirmwareUpdateArtifact_QueuesHealthyDuplicateAfterCachedCopyCorrupts(t *testing.T) {
	_, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	// The shared device fixture owns organization 1; match manualSessionCtx's
	// user so the real command-batch foreign key is satisfied.
	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO "user" (id, user_id, username, password_hash)
		VALUES (42, 'user-1', 'test-user', 'unused')
	`)
	require.NoError(t, err)

	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	const content = "assigned firmware payload"
	metadata := files.FirmwareMetadata{TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0"}
	assignedID, err := filesService.SaveFirmwareFile("assigned.swu", strings.NewReader(content), metadata)
	require.NoError(t, err)
	assignment, err := filesService.ResolveFirmwareArtifact(assignedID)
	require.NoError(t, err)
	_, err = filesService.SaveFirmwareFile("duplicate.swu", strings.NewReader(content), metadata)
	require.NoError(t, err)
	candidates := filesService.FirmwareFileIDsByChecksum(assignment.Checksum)
	require.Len(t, candidates, 2)
	corruptID, healthyID := candidates[0], candidates[1]
	corruptPath, err := filesService.GetFirmwareFilePath(corruptID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(corruptPath, []byte(strings.Repeat("x", len(content))), 0600))

	// The healthy duplicate's current sidecar does not describe this miner;
	// dispatch must continue to validate against the saved assignment snapshot.
	_, err = filesService.UpdateFirmwareMetadata(healthyID, files.FirmwareMetadata{
		TargetManufacturer: "Bitmain", TargetModel: "S19", FirmwareVersion: "9.0.0",
	})
	require.NoError(t, err)

	run := newExecutionRun(t.Context())
	t.Cleanup(run.cancelAdmission)
	t.Cleanup(run.cancelWork)
	svc := &Service{
		config:           &Config{},
		conn:             conn,
		executionService: &ExecutionService{run: run},
		messageQueue:     queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
		filesService:     filesService,
		deviceStore:      sqlstores.NewSQLDeviceStore(conn),
		// Keep queued rows available to inspect without starting background
		// status tracking or device workers. All dispatch DB access stays real.
		startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
	}
	ctx := manualSessionCtx(1)
	for range 2 {
		result, err := svc.FirmwareUpdateArtifact(ctx, includeSelector(identifier), assignment.Checksum, assignment.Metadata)
		require.NoError(t, err)
		require.Equal(t, 1, result.DispatchedCount)
		var rawPayload []byte
		require.NoError(t, conn.QueryRowContext(ctx, `
			SELECT payload FROM queue_message WHERE command_batch_log_uuid = $1 AND device_id = $2
		`, result.BatchIdentifier, deviceID).Scan(&rawPayload))
		var payload dto.FirmwareUpdatePayload
		require.NoError(t, json.Unmarshal(rawPayload, &payload))
		assert.Equal(t, healthyID, payload.FirmwareFileID)
		assert.Equal(t, assignment.Checksum, payload.FirmwareChecksum)
		reader, _, err := filesService.OpenFirmwareArtifact(payload.FirmwareFileID, payload.FirmwareChecksum)
		require.NoError(t, err)
		delivered, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		require.NoError(t, readErr)
		require.NoError(t, closeErr)
		assert.Equal(t, content, string(delivered))
	}
}
