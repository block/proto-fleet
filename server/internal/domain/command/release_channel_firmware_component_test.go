package command

import (
	"bytes"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

// ct1CapsuleCommands is a command service over one paired CT1 container, with
// a PSU capsule uploaded for it.
type ct1CapsuleCommands struct {
	svc        *Service
	conn       *sql.DB
	files      *files.Service
	identifier string
	capsuleID  string
}

func newCT1CapsuleCommands(t *testing.T) *ct1CapsuleCommands {
	t.Helper()
	_, conn, _, _, identifier := setupPasswordCommandDevice(t, "proto", false)
	ctx := manualSessionCtx(1)
	_, err := conn.ExecContext(ctx, `INSERT INTO "user" (id, user_id, username, password_hash) VALUES (42, 'user-1', 'test-user', 'unused')`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET manufacturer = 'Proto', model = 'CT1' WHERE device_identifier = $1`, "discovered-"+identifier)
	require.NoError(t, err)

	t.Chdir(t.TempDir())
	fs, err := files.NewService(files.Config{})
	require.NoError(t, err)
	capsule, err := psucaptest.Build(psucaptest.Options{})
	require.NoError(t, err)
	capsuleID, err := fs.SaveFirmwareFile("boco.psucap", bytes.NewReader(capsule), files.FirmwareMetadata{
		TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "app=1410;bl=1110", Component: files.FirmwareComponentPSU,
	})
	require.NoError(t, err)
	run := newExecutionRun(t.Context())
	t.Cleanup(run.cancelAdmission)
	t.Cleanup(run.cancelWork)
	svc := &Service{
		config: &Config{}, conn: conn, executionService: &ExecutionService{run: run},
		filesService: fs, deviceStore: sqlstores.NewSQLDeviceStore(conn),
		messageQueue:                     queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
		startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
	}
	svc.RegisterFilter(NewReleaseChannelFirmwareFilter(conn))
	return &ct1CapsuleCommands{svc: svc, conn: conn, files: fs, identifier: identifier, capsuleID: capsuleID}
}

// payloadComponents returns the component a batch's queued command and its
// log entry name. The rollout queries read the first; a finished command reads
// the second.
func (c *ct1CapsuleCommands) payloadComponents(t *testing.T, batch string) (queued, logged string) {
	t.Helper()
	ctx := manualSessionCtx(1)
	require.NoError(t, c.conn.QueryRowContext(ctx, `SELECT payload->>'firmware_component' FROM queue_message WHERE command_batch_log_uuid = $1`, batch).Scan(&queued))
	require.NoError(t, c.conn.QueryRowContext(ctx, `SELECT payload->>'firmware_component' FROM command_batch_log WHERE uuid = $1`, batch).Scan(&logged))
	return queued, logged
}

// A release channel owns a container's firmware per component. A channel that
// assigns only hashboard firmware does not own the container's PSU firmware,
// so a direct PSU update is admitted, and its queued command names the
// component so that its completion invalidates PSU provenance only.
func TestManualPSUUpdateIsAdmittedBesideAHashboardAssignment(t *testing.T) {
	c := newCT1CapsuleCommands(t)
	svc, conn, identifier, capsuleID := c.svc, c.conn, c.identifier, c.capsuleID
	ctx := manualSessionCtx(1)
	var channelID int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO release_channel (org_id, name, created_by) VALUES (1, 'Stable', 42) RETURNING id`).Scan(&channelID))
	_, err := conn.ExecContext(ctx, `INSERT INTO release_channel_target (channel_id, target_type, device_identifier) VALUES ($1, 'miner', $2)`, channelID, identifier)
	require.NoError(t, err)
	assign := func(component string) {
		t.Helper()
		_, err := conn.ExecContext(ctx, `INSERT INTO release_channel_firmware (channel_id, manufacturer, model, component, firmware_checksum, assigned_by)
			VALUES ($1, 'Proto', 'CT1', $2, 'assigned', 42)`, channelID, component)
		require.NoError(t, err)
	}
	assign("")

	result, err := svc.FirmwareUpdate(ctx, includeSelector(identifier), capsuleID)
	require.NoError(t, err, "a hashboard assignment does not own the container's PSU firmware")
	require.Equal(t, 1, result.DispatchedCount)
	queued, logged := c.payloadComponents(t, result.BatchIdentifier)
	require.Equal(t, files.FirmwareComponentPSU, queued)
	require.Equal(t, files.FirmwareComponentPSU, logged, "completion reads the component from the batch")

	// Once the channel assigns PSU firmware too, it owns that component.
	assign(files.FirmwareComponentPSU)
	_, err = svc.FirmwareUpdate(ctx, includeSelector(identifier), capsuleID)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "managed PSU firmware must fail before enqueue: %v", err)
	require.Contains(t, err.Error(), "Stable")
	var commands int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&commands))
	require.Equal(t, 1, commands)
}

// A release channel sends a capsule with the same command as a direct update.
// The command names the component of the assignment's metadata snapshot, so
// the PSU rollout finds its own command and a finished one clears PSU
// provenance only.
func TestRolloutDispatchNamesThePSUComponent(t *testing.T) {
	c := newCT1CapsuleCommands(t)
	artifact, err := c.files.ResolveFirmwareArtifact(c.capsuleID)
	require.NoError(t, err)
	require.Equal(t, files.FirmwareComponentPSU, artifact.Metadata.ComponentKey())

	result, err := c.svc.FirmwareUpdateArtifact(manualSessionCtx(1), includeSelector(c.identifier), artifact.Checksum, artifact.Metadata)
	require.NoError(t, err)
	require.Equal(t, 1, result.DispatchedCount)
	queued, logged := c.payloadComponents(t, result.BatchIdentifier)
	require.Equal(t, files.FirmwareComponentPSU, queued)
	require.Equal(t, files.FirmwareComponentPSU, logged)
}
