package files

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func TestFirmwareAssignmentPinsProtectThroughCommitWithoutHoldingLifecycleLock(t *testing.T) {
	svc := setupService(t)
	const payload = "firmware for an assignment transaction"
	fileID, err := svc.SaveFirmwareFile("update.swu", strings.NewReader(payload), testFirmwareMetadata())
	require.NoError(t, err)
	releaseFirst, err := svc.PinFirmwareArtifact(checksumOf(payload))
	require.NoError(t, err)
	t.Cleanup(releaseFirst)
	releaseSecond, err := svc.PinFirmwareArtifact(checksumOf(payload))
	require.NoError(t, err)
	t.Cleanup(releaseSecond)
	requireFleetCode(t, svc.DeleteFirmwareFile(fileID), connect.CodeFailedPrecondition)

	// An assignment transaction builds views and can resolve additional files.
	// A pin must not deadlock those reads behind a waiting metadata writer.
	updated := make(chan error, 1)
	go func() {
		_, err := svc.UpdateFirmwareMetadata(fileID, testFirmwareMetadata())
		updated <- err
	}()
	select {
	case err := <-updated:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("assignment pin held the lifecycle lock")
	}
	_, err = svc.ResolveFirmwareArtifact(fileID)
	require.NoError(t, err)
	releaseFirst()
	releaseFirst()
	requireFleetCode(t, svc.DeleteFirmwareFile(fileID), connect.CodeFailedPrecondition)
	releaseSecond()
	require.NoError(t, svc.DeleteFirmwareFile(fileID))
}

func TestFirmwareAssignmentPinRejectsMissingOrCorruptPayload(t *testing.T) {
	svc := setupService(t)
	const payload = "firmware"
	_, err := svc.PinFirmwareArtifact(checksumOf(payload))
	require.Error(t, err)
	fileID, err := svc.SaveFirmwareFile("update.swu", strings.NewReader(payload), testFirmwareMetadata())
	require.NoError(t, err)
	path, err := svc.GetFirmwareFilePath(fileID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("changed"), 0600))
	_, err = svc.PinFirmwareArtifact(checksumOf(payload))
	require.Error(t, err)
	require.Empty(t, svc.firmwareExecutionPins)
}

func TestFirmwareDeletionFailsClosedWhenUsageCheckFails(t *testing.T) {
	for _, failsPreparation := range []bool{true, false} {
		t.Run(fmt.Sprintf("preparation=%t", failsPreparation), func(t *testing.T) {
			svc := setupService(t)
			fileID, err := svc.SaveFirmwareFile("update.swu", strings.NewReader("firmware"), testFirmwareMetadata())
			require.NoError(t, err)
			want := errors.New("usage check unavailable")
			released := false
			svc.SetFirmwareDeletionGuard(func() (FirmwareDeletionCheck, func(), error) {
				if failsPreparation {
					return nil, nil, want
				}
				return func(id, checksum string) error {
					require.Equal(t, fileID, id)
					require.Equal(t, checksumOf("firmware"), checksum)
					return want
				}, func() { released = true }, nil
			})
			require.ErrorIs(t, svc.DeleteFirmwareFile(fileID), want)
			require.Equal(t, !failsPreparation, released)
			_, err = svc.ResolveFirmwareArtifact(fileID)
			require.NoError(t, err, "a failed usage check must leave payload and identity intact")
		})
	}
}
