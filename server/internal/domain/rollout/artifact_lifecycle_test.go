package rollout

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func realArtifactFixture(t *testing.T) (*fixture, *files.Service, string) {
	t.Helper()
	f := newFixture(t, 1)
	t.Chdir(t.TempDir())
	fs, err := files.NewService(files.Config{})
	require.NoError(t, err)
	f.svc.files = fs
	fs.SetFirmwareDeletionGuard(func() (files.FirmwareDeletionCheck, func(), error) {
		return PrepareFirmwareDeletion(t.Context(), f.conn)
	})
	fileID, err := fs.SaveFirmwareFile("firmware.swu", strings.NewReader("firmware payload"), files.FirmwareMetadata{
		TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0",
	})
	require.NoError(t, err)
	return f, fs, fileID
}

func TestFirmwareDeletionProtectsActiveAndContinuouslyEnforcedAssignments(t *testing.T) {
	f, fs, fileID := realArtifactFixture(t)
	f.channel(t, allAtOnce, f.allMiners()...)
	started := f.apply(t, fileID)
	err := fs.DeleteFirmwareFile(fileID)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
	require.True(t, fleeterror.IsFailedPreconditionError(fs.DeleteFirmwareFile(fileID)), "a completed update still has an enforced assignment")
	_, err = f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("")}, nil)
	require.NoError(t, err)
	require.NoError(t, fs.DeleteFirmwareFile(fileID), "cleared, settled firmware can be removed normally")
}

func TestFirmwareDeletionProtectsQueuedCommandsWithoutAChannel(t *testing.T) {
	for _, status := range []string{"PENDING", "PROCESSING"} {
		t.Run(status, func(t *testing.T) {
			f, fs, fileID := realArtifactFixture(t)
			f.queueFirmwareCommand(t, "miner-0", fileID, status)
			require.True(t, fleeterror.IsFailedPreconditionError(fs.DeleteFirmwareFile(fileID)))
			_, err := f.conn.ExecContext(t.Context(), `UPDATE queue_message SET status = 'SUCCESS'`)
			require.NoError(t, err)
			require.NoError(t, fs.DeleteFirmwareFile(fileID))
		})
	}
}

func TestDeleteAllFirmwarePreservesInUseFilesAndDeletesUnusedFiles(t *testing.T) {
	f, fs, fileID := realArtifactFixture(t)
	f.channel(t, allAtOnce, f.allMiners()...)
	f.apply(t, fileID)
	unused, err := fs.SaveFirmwareFile("unused.swu", strings.NewReader("unused payload"), files.FirmwareMetadata{
		TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "1.0.0",
	})
	require.NoError(t, err)
	deleted, err := fs.DeleteAllFirmwareFiles()
	require.Equal(t, 1, deleted)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	_, err = fs.ResolveFirmwareArtifact(fileID)
	require.NoError(t, err)
	_, err = fs.ResolveFirmwareArtifact(unused)
	require.Error(t, err)
}

type waitBeforeFirmwareResolve struct {
	FirmwareFiles
	entered chan struct{}
	proceed <-chan struct{}
}

func (f *waitBeforeFirmwareResolve) ResolveFirmwareArtifact(fileID string) (files.FirmwareArtifact, error) {
	close(f.entered)
	<-f.proceed
	return f.FirmwareFiles.ResolveFirmwareArtifact(fileID)
}

func TestFirmwareDeletionDoesNotBlockAssignmentWithSaturatedPool(t *testing.T) {
	f, fs, fileID := realArtifactFixture(t)
	f.channel(t, allAtOnce, f.allMiners()...)
	// The migration adapter retains a connection. Leave exactly one further
	// slot for application work rather than shrinking beneath that baseline.
	retainedConnections := f.conn.Stats().InUse
	f.conn.SetMaxOpenConns(retainedConnections + 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fs.SetFirmwareDeletionGuard(func() (files.FirmwareDeletionCheck, func(), error) {
		return PrepareFirmwareDeletion(ctx, f.conn)
	})
	proceed := make(chan struct{})
	allowResolve := sync.OnceFunc(func() { close(proceed) })
	defer allowResolve()
	blocked := &waitBeforeFirmwareResolve{FirmwareFiles: fs, entered: make(chan struct{}), proceed: proceed}
	f.svc.files = blocked
	applyDone := make(chan error, 1)
	go func() {
		_, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{rigAssignment(fileID)}, nil)
		applyDone <- err
	}()
	select {
	case <-blocked.entered:
	case <-ctx.Done():
		t.Fatal("assignment did not reach artifact resolution")
	}

	// The assignment holds the pool's only connection. Deletion must wait
	// without holding the files lock it needs to finish resolution and pin.
	initialWaits := f.conn.Stats().WaitCount
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- fs.DeleteFirmwareFile(fileID) }()
	require.Eventually(t, func() bool {
		return f.conn.Stats().WaitCount > initialWaits
	}, time.Second, 10*time.Millisecond)
	allowResolve()
	select {
	case err := <-applyDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("deletion held the files lock while waiting for the assignment's connection")
	}
	err := <-deleteDone
	require.True(t, fleeterror.IsFailedPreconditionError(err), "the reserved connection must see the committed assignment: %v", err)
	_, err = fs.ResolveFirmwareArtifact(fileID)
	require.NoError(t, err)
	require.Equal(t, retainedConnections, f.conn.Stats().InUse, "deletion must release its reserved connection after a rejected check")
}
