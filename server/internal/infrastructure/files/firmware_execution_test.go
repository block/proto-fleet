package files

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirmwareExecutionPin_DownloadWhileDeletionWriterIsPending(t *testing.T) {
	svc := setupService(t)
	const content = "assigned firmware payload"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	executionReader, info, err := svc.OpenFirmwareFileForExecution(fileID, checksumOf(content))
	require.NoError(t, err)
	t.Cleanup(func() { _ = executionReader.Close() })

	// Queue a real deletion writer behind an independent metadata reader, then
	// begin the exact-ID open used by the gateway. Holding an execution RLock
	// across delivery would leave the writer and the gateway waiting forever.
	svc.firmwareMetadataReuseMu.RLock()
	locked := true
	defer func() {
		if locked {
			svc.firmwareMetadataReuseMu.RUnlock()
		}
	}()
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- svc.DeleteFirmwareFile(fileID) }()
	require.Eventually(t, func() bool {
		if svc.firmwareMetadataReuseMu.TryRLock() {
			svc.firmwareMetadataReuseMu.RUnlock()
			return false
		}
		return true
	}, time.Second, time.Millisecond, "deletion must be queued as a writer")

	type downloadResult struct {
		content string
		err     error
	}
	downloadDone := make(chan downloadResult, 1)
	go func() {
		reader, _, err := svc.OpenFirmwareArtifact(info.ID, info.SHA256)
		if err != nil {
			downloadDone <- downloadResult{err: err}
			return
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err == nil {
			err = closeErr
		}
		downloadDone <- downloadResult{content: string(data), err: err}
	}()
	svc.firmwareMetadataReuseMu.RUnlock()
	locked = false

	select {
	case err := <-deleteDone:
		requireFleetCode(t, err, connect.CodeFailedPrecondition)
	case <-time.After(time.Second):
		t.Fatal("deletion blocked on an active command")
	}
	select {
	case got := <-downloadDone:
		require.NoError(t, got.err)
		assert.Equal(t, content, got.content)
	case <-time.After(time.Second):
		t.Fatal("gateway open deadlocked behind the deletion writer")
	}
	require.NoError(t, executionReader.Close())
	require.NoError(t, svc.DeleteFirmwareFile(fileID))
}

func TestFirmwareExecutionPin_ProtectsOnlyItsFileUntilEveryCommandFinishes(t *testing.T) {
	svc := setupService(t)
	const content = "firmware shared by two commands"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	unrelatedID, err := svc.SaveFirmwareFile("other.swu", strings.NewReader("different firmware"), testFirmwareMetadata())
	require.NoError(t, err)

	manualReader, info, err := svc.OpenFirmwareFileForExecution(fileID, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manualReader.Close() })
	artifactReader, _, err := svc.OpenFirmwareFileForExecution(fileID, info.SHA256)
	require.NoError(t, err)
	t.Cleanup(func() { _ = artifactReader.Close() })
	requireFleetCode(t, svc.DeleteFirmwareFile(fileID), connect.CodeFailedPrecondition)
	require.NoError(t, svc.DeleteFirmwareFile(unrelatedID), "another payload must remain deletable")

	// Payload identity is immutable, but editing a sidecar during delivery is
	// safe: release channel commands already carry their assignment snapshot.
	metadata := testFirmwareMetadata()
	metadata.FirmwareVersion = "9.9.9"
	_, err = svc.UpdateFirmwareMetadata(fileID, metadata)
	require.NoError(t, err)
	pluginBytes, err := os.ReadFile(info.FilePath)
	require.NoError(t, err, "the plugin process reopens this path")
	assert.Equal(t, content, string(pluginBytes))

	require.NoError(t, manualReader.Close())
	require.NoError(t, manualReader.Close(), "closing twice must not release the other command's pin")
	requireFleetCode(t, svc.DeleteFirmwareFile(fileID), connect.CodeFailedPrecondition)
	require.NoError(t, artifactReader.Close())
	require.NoError(t, svc.DeleteFirmwareFile(fileID))
	assert.Empty(t, svc.firmwareExecutionPins)
}

func TestFirmwareExecutionPin_FailedVerificationDoesNotPreventDeletion(t *testing.T) {
	svc := setupService(t)
	const content = "firmware before corruption"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)
	path, err := svc.GetFirmwareFilePath(fileID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", len(content))), 0600))

	reader, _, err := svc.OpenFirmwareFileForExecution(fileID, checksumOf(content))
	requireFleetCode(t, err, connect.CodeFailedPrecondition)
	require.Nil(t, reader)
	require.NoError(t, svc.DeleteFirmwareFile(fileID))
	assert.Empty(t, svc.firmwareExecutionPins)
}
