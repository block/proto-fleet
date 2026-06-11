//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFleetCLIFirmwareWorkflow drives the firmware file lifecycle through the
// fleetcli binary: config -> check -> upload (direct and chunked) -> list ->
// delete -> delete-all.
//
// Prerequisites: the docker-compose stack must be running with fleet-api on
// localhost:4000 (e.g. `just dev`). The final delete-all step removes every
// firmware file stored on the target fleet.
func TestFleetCLIFirmwareWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx := context.Background()

	t.Log("Waiting for fleet-api to be ready...")
	waitForFleetAPIHealth(t, ctx, 60*time.Second)

	t.Log("Building fleetcli test binary...")
	buildFleetCLIBinary(t)

	env := []string{
		"FLEET_SERVER=" + fleetAPIURL + "/",
		"FLEET_USERNAME=" + testUsername,
		"FLEET_PASSWORD=" + testPassword,
		"NO_COLOR=1",
	}

	// Firmware commands need session credentials, which only work once the
	// fleet has an admin; accept already-onboarded failures so the test is
	// rerunnable.
	if _, err := runFleetCLI(ctx, env,
		"onboarding", "create-admin",
		"--username", testUsername,
		"--password", testPassword,
	); err != nil {
		require.Truef(t, isAlreadyOnboardedError(err),
			"create-admin failed for a reason other than existing onboarding: %v", err)
	}

	workDir := t.TempDir()
	// Random content guarantees the checksum pre-check cannot collide with
	// files uploaded by earlier runs or other users of the stack.
	smallPath := writeRandomFirmwareFile(t, workDir, "e2e-firmware-small.swu", 64*1024)

	var chunkSizeBytes, maxFileSizeBytes int64

	t.Run("Config", func(t *testing.T) {
		output, err := runFleetCLI(ctx, env, "firmware", "config")
		require.NoError(t, err, "firmware config should succeed")

		var cfg struct {
			AllowedExtensions []string `json:"allowed_extensions"`
			MaxFileSizeBytes  int64    `json:"max_file_size_bytes"`
			ChunkSizeBytes    int64    `json:"chunk_size_bytes"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &cfg), "config output should be JSON: %s", output)
		require.Contains(t, cfg.AllowedExtensions, ".swu", "firmware config should allow .swu uploads")
		require.Positive(t, cfg.ChunkSizeBytes, "chunk size should be positive")
		require.Greater(t, cfg.MaxFileSizeBytes, cfg.ChunkSizeBytes, "max file size should exceed the chunk size")

		chunkSizeBytes = cfg.ChunkSizeBytes
		maxFileSizeBytes = cfg.MaxFileSizeBytes
		t.Logf("✓ Config: chunk_size=%d max_file_size=%d extensions=%v",
			cfg.ChunkSizeBytes, cfg.MaxFileSizeBytes, cfg.AllowedExtensions)
	})

	t.Run("CheckBeforeUpload", func(t *testing.T) {
		check := checkFirmwareFile(t, ctx, env, smallPath)
		require.False(t, check.Exists, "fresh random content must not already exist on the server")
		t.Log("✓ Check reports unknown checksum before upload")
	})

	var smallFileID string

	t.Run("UploadDirect", func(t *testing.T) {
		output, err := runFleetCLI(ctx, env, "firmware", "upload", "--quiet", smallPath)
		require.NoError(t, err, "direct upload should succeed")
		smallFileID = decodeFirmwareFileID(t, output)
		t.Logf("✓ Direct upload stored firmware file %s", smallFileID)
	})

	t.Run("CheckAfterUpload", func(t *testing.T) {
		require.NotEmpty(t, smallFileID, "smallFileID must be set from the direct upload step")

		check := checkFirmwareFile(t, ctx, env, smallPath)
		require.True(t, check.Exists, "uploaded checksum should be known to the server")
		assert.Equal(t, smallFileID, check.FirmwareFileID, "check should return the uploaded file id")
		t.Log("✓ Check finds the uploaded file by checksum")
	})

	t.Run("UploadReusesExisting", func(t *testing.T) {
		require.NotEmpty(t, smallFileID, "smallFileID must be set from the direct upload step")

		output, err := runFleetCLI(ctx, env, "firmware", "upload", "--quiet", smallPath)
		require.NoError(t, err, "re-upload should succeed")
		assert.Equal(t, smallFileID, decodeFirmwareFileID(t, output),
			"re-uploading identical content should return the existing file id")
		t.Log("✓ Re-upload reused the existing file")
	})

	var chunkedFileID string
	var chunkedSize int64

	t.Run("UploadChunked", func(t *testing.T) {
		require.Positive(t, chunkSizeBytes, "chunk size must be known from the Config step")

		chunkedSize = chunkSizeBytes + 1<<20
		require.LessOrEqual(t, chunkedSize, maxFileSizeBytes,
			"chunked test file must fit within the server's max file size")
		chunkedPath := writeRandomFirmwareFile(t, workDir, "e2e-firmware-chunked.swu", chunkedSize)

		output, err := runFleetCLI(ctx, env, "firmware", "upload", "--quiet", chunkedPath)
		require.NoError(t, err, "chunked upload should succeed")
		chunkedFileID = decodeFirmwareFileID(t, output)
		assert.NotEqual(t, smallFileID, chunkedFileID, "chunked upload should store a distinct file")

		// The server hashes the assembled file when the chunked upload
		// completes, so a checksum hit for the local file proves every chunk
		// arrived intact and in order.
		check := checkFirmwareFile(t, ctx, env, chunkedPath)
		require.True(t, check.Exists, "server should know the chunked upload's checksum")
		assert.Equal(t, chunkedFileID, check.FirmwareFileID, "checksum should map to the chunked file id")
		t.Logf("✓ Chunked upload stored firmware file %s (%d bytes)", chunkedFileID, chunkedSize)
	})

	t.Run("List", func(t *testing.T) {
		require.NotEmpty(t, smallFileID, "smallFileID must be set from the direct upload step")
		require.NotEmpty(t, chunkedFileID, "chunkedFileID must be set from the chunked upload step")

		files := listFirmwareFiles(t, ctx, env)

		small := findFirmwareFile(files, smallFileID)
		require.NotNil(t, small, "list should include the direct upload")
		assert.Equal(t, "e2e-firmware-small.swu", small.Filename)
		assert.Equal(t, int64(64*1024), small.Size)

		chunked := findFirmwareFile(files, chunkedFileID)
		require.NotNil(t, chunked, "list should include the chunked upload")
		assert.Equal(t, "e2e-firmware-chunked.swu", chunked.Filename)
		assert.Equal(t, chunkedSize, chunked.Size, "stored size should match the full chunked file")
		t.Logf("✓ List contains both uploads (%d file(s) total)", len(files))
	})

	t.Run("Delete", func(t *testing.T) {
		require.NotEmpty(t, smallFileID, "smallFileID must be set from the direct upload step")

		output, err := runFleetCLI(ctx, env, "firmware", "delete", smallFileID)
		require.NoError(t, err, "firmware delete should succeed")

		var resp struct {
			DeletedFileID string `json:"deleted_file_id"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &resp), "delete output should be JSON: %s", output)
		assert.Equal(t, smallFileID, resp.DeletedFileID, "delete should echo the removed file id")

		require.Nil(t, findFirmwareFile(listFirmwareFiles(t, ctx, env), smallFileID),
			"deleted file should no longer be listed")
		t.Logf("✓ Deleted firmware file %s", smallFileID)
	})

	t.Run("DeleteAll", func(t *testing.T) {
		require.NotEmpty(t, chunkedFileID, "chunkedFileID must be set from the chunked upload step")

		output, err := runFleetCLI(ctx, env, "firmware", "delete-all")
		require.NoError(t, err, "firmware delete-all should succeed")

		var resp struct {
			DeletedCount int `json:"deleted_count"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &resp), "delete-all output should be JSON: %s", output)
		assert.GreaterOrEqual(t, resp.DeletedCount, 1, "delete-all should remove at least the chunked upload")

		assert.Empty(t, listFirmwareFiles(t, ctx, env), "no firmware files should remain after delete-all")
		t.Logf("✓ Delete-all removed %d file(s)", resp.DeletedCount)
	})
}

type firmwareE2EFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type firmwareE2ECheck struct {
	Exists         bool   `json:"exists"`
	FirmwareFileID string `json:"firmware_file_id"`
}

// writeRandomFirmwareFile creates a file of the given size filled with random
// bytes so its checksum is unique to this test run.
func writeRandomFirmwareFile(t *testing.T, dir, name string, size int64) string {
	t.Helper()

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	require.NoError(t, err, "create %s", name)
	_, err = io.CopyN(f, rand.Reader, size)
	require.NoError(t, f.Close(), "close %s", name)
	require.NoError(t, err, "fill %s with %d random bytes", name, size)
	return path
}

func checkFirmwareFile(t *testing.T, ctx context.Context, env []string, path string) firmwareE2ECheck {
	t.Helper()

	output, err := runFleetCLI(ctx, env, "firmware", "check", path)
	require.NoError(t, err, "firmware check should succeed")

	var resp firmwareE2ECheck
	require.NoError(t, json.Unmarshal([]byte(output), &resp), "check output should be JSON: %s", output)
	return resp
}

func decodeFirmwareFileID(t *testing.T, output string) string {
	t.Helper()

	var resp struct {
		FirmwareFileID string `json:"firmware_file_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &resp), "output should be firmware_file_id JSON: %s", output)
	require.NotEmpty(t, resp.FirmwareFileID, "firmware_file_id should not be empty")
	return resp.FirmwareFileID
}

func listFirmwareFiles(t *testing.T, ctx context.Context, env []string) []firmwareE2EFile {
	t.Helper()

	output, err := runFleetCLI(ctx, env, "firmware", "list")
	require.NoError(t, err, "firmware list should succeed")

	var resp struct {
		Files []firmwareE2EFile `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &resp), "list output should be JSON: %s", output)
	return resp.Files
}

func findFirmwareFile(files []firmwareE2EFile, id string) *firmwareE2EFile {
	for i := range files {
		if files[i].ID == id {
			return &files[i]
		}
	}
	return nil
}
