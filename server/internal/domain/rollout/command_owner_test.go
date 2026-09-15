package rollout

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

// ownerAuditDispatcher keeps the miner transport controlled while exercising
// the real command-batch insert and its user foreign key. Like command.Service,
// it takes created_by from the enforcement session, not from a test-side owner.
type ownerAuditDispatcher struct {
	t *testing.T
	f *fixture
}

func (d *ownerAuditDispatcher) FirmwareUpdateArtifact(ctx context.Context, selector *commandpb.DeviceSelector, checksum string, metadata files.FirmwareMetadata) (*command.CommandResult, error) {
	info, err := session.GetInfo(ctx)
	require.NoError(d.t, err)
	require.Equal(d.t, session.ActorRolloutEnforcement, info.Actor)
	batchID := id.GenerateID()
	_, err = sqlc.New(d.f.conn).CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
		Uuid: batchID, Type: "FirmwareUpdate", CreatedBy: info.UserID,
		CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING,
		DevicesCount:   int32(len(selector.GetIncludeDevices().GetDeviceIdentifiers())), // #nosec G115 -- bounded test fleet
		OrganizationID: sql.NullInt64{Int64: info.OrganizationID, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	result, err := d.f.dispatcher.FirmwareUpdateArtifact(ctx, selector, checksum, metadata)
	if result != nil {
		result.BatchIdentifier = batchID
	}
	return result, err
}

func newCommandOwnerFixture(t *testing.T) (*fixture, Actor) {
	t.Helper()
	f := newFixture(t, 1)
	_, err := f.conn.ExecContext(t.Context(), `INSERT INTO "user" (id, user_id, username, password_hash)
		VALUES (41, 'assignment-owner', 'assignment-owner', 'test'),
		       (42, 'second-owner', 'second-owner', 'test')`)
	require.NoError(t, err)
	f.svc.commands = &ownerAuditDispatcher{t: t, f: f}
	f.channel(t, allAtOnce, "miner-0")
	// A numeric audit identity deliberately outside the user table catches
	// accidental use of the actor ID as a command's owning user.
	return f, Actor{Type: ActorTypeAPIKey, ID: 9001, Name: "deployment key", OwnerUserID: 41}
}

func applyAsOwner(t *testing.T, f *fixture, actor Actor, fileID string) Rollout {
	t.Helper()
	started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, actor, f.channelID, []Assignment{rigAssignment(fileID)}, nil)
	require.NoError(t, err)
	require.Len(t, started, 1)
	return started[0]
}

func commandOwners(t *testing.T, f *fixture) []int64 {
	t.Helper()
	rows, err := f.conn.QueryContext(t.Context(), `SELECT created_by FROM command_batch_log ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var owners []int64
	for rows.Next() {
		var owner int64
		require.NoError(t, rows.Scan(&owner))
		owners = append(owners, owner)
	}
	require.NoError(t, rows.Err())
	return owners
}

func TestEnforcementUsesPersistedAssignmentOwnerAfterReconciliation(t *testing.T) {
	f, actor := newCommandOwnerFixture(t)
	started := applyAsOwner(t, f, actor, "fw-2")
	require.Equal(t, actor.ID, started.StartedBy.ID)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []int64{41}, commandOwners(t, f))
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)

	// Recreate the service before drift correction: its owner must survive in
	// the assignment, even though the next rollout is attributed to system/0.
	queries := sqlstores.NewSQLConnectionManager(f.conn)
	f.svc = NewService(&queries, sqlstores.NewSQLTransactor(f.conn), &ownerAuditDispatcher{t: t, f: f}, f.files, f.activity)
	f.svc.now = func() time.Time { return f.clock }
	f.setReportedVersion(t, "miner-0", "1.0.0")
	f.svc.EnforceTick(t.Context())
	reconciled := f.latestRollout(t)
	require.NotEqual(t, started.ID, reconciled.ID)
	require.Equal(t, SystemActor, reconciled.StartedBy)
	require.Equal(t, []int64{41, 41}, commandOwners(t, f))
	require.Equal(t, int32(1), reconciled.Devices[0].Attempts)
}

func TestRetryKeepsAssignmentOwnerAndRollbackAssignsNewOwner(t *testing.T) {
	f, actor := newCommandOwnerFixture(t)
	first := applyAsOwner(t, f, actor, "fw-1")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "1.5.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompleted, f.rollout(t, first.ID).Status)
	failed := applyAsOwner(t, f, actor, "fw-2")
	for range MaxAttempts {
		f.svc.EnforceTick(t.Context())
		f.backdateSends(t)
	}
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompletedWithFailures, f.rollout(t, failed.ID).Status)

	secondActor := Actor{Type: ActorTypeAPIKey, ID: 9002, Name: "second key", OwnerUserID: 42}
	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, failed.ID, Mutation{Actor: secondActor})
	require.NoError(t, err)
	require.Equal(t, secondActor.ID, retried.StartedBy.ID)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []int64{41, 41, 41, 41, 41}, commandOwners(t, f), "retry enforces the existing assignment under its owner")
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	_, rolledBack, err := f.svc.RollbackFirmware(t.Context(), f.orgID, retried.ID, Mutation{Actor: secondActor})
	require.NoError(t, err)
	require.Len(t, rolledBack, 1)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []int64{41, 41, 41, 41, 41, 42}, commandOwners(t, f), "rollback changes the assignment and its owning user")
}

func TestAssignmentRequiresAPIKeyOwner(t *testing.T) {
	f, actor := newCommandOwnerFixture(t)
	actor.OwnerUserID = 0
	_, err := f.svc.ApplyFirmware(t.Context(), f.orgID, actor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
	require.ErrorContains(t, err, "requires an owning user")
	var assignments int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM release_channel_firmware`).Scan(&assignments))
	require.Zero(t, assignments)
}
