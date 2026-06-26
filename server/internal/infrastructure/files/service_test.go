package files

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/miner/logformat"
)

// setupService creates a Service backed by a temporary directory and restores the
// working directory on cleanup, so tests don't write into the source tree.
func setupService(t *testing.T) *Service {
	t.Helper()
	tmp := t.TempDir()
	t.Chdir(tmp)

	svc, err := NewService(Config{})
	require.NoError(t, err)
	return svc
}

// TestSaveLogs_NormalizesMAC verifies that colons and dashes in the MAC address
// are stripped and the address is lowercased in the output filename.
func TestSaveLogs_NormalizesMAC(t *testing.T) {
	svc := setupService(t)

	filePath, err := svc.SaveLogs("batch-1", "AA:BB:CC:DD:EE:FF", []string{"line1", "line2"})

	require.NoError(t, err)
	name := filepath.Base(filePath)
	assert.True(t, strings.HasPrefix(name, "miner-logs-aabbccddeeff-"), "filename should start with normalized MAC")
	assert.True(t, strings.HasSuffix(name, ".csv"))
}

// TestSaveLogs_WritesLines verifies that every log line is written to the file.
func TestSaveLogs_WritesLines(t *testing.T) {
	svc := setupService(t)

	lines := []string{"header", "row1", "row2"}
	filePath, err := svc.SaveLogs("batch-2", "00:11:22:33:44:55", lines)

	require.NoError(t, err)
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	content := string(data)
	for _, line := range lines {
		assert.Contains(t, content, line)
	}
}

// TestBundleLogs_SingleFile_MovesToTempWithNameSidecar verifies that when only one
// device's log file exists the bundle step moves it directly to the temp directory
// as a CSV (no ZIP), and writes a .name sidecar containing the original filename.
func TestBundleLogs_SingleFile_MovesToTempWithNameSidecar(t *testing.T) {
	svc := setupService(t)

	filePath, err := svc.SaveLogs("batch-single", "aa:bb:cc:dd:ee:ff", []string{"Time,Message", `2026-01-01T00:00:00Z,"hello"`})
	require.NoError(t, err)
	originalName := filepath.Base(filePath)

	bundlePath, err := svc.bundleLogs("batch-single")

	require.NoError(t, err)
	assert.Equal(t, getBatchLogsSingleFilePath("batch-single"), bundlePath)

	_, statErr := os.Stat(bundlePath)
	assert.NoError(t, statErr, "CSV should exist at bundle path")

	sidecar, readErr := os.ReadFile(bundlePath + ".name")
	require.NoError(t, readErr)
	assert.Equal(t, originalName, string(sidecar))
}

func TestSaveCommandArtifactLog_MaterializesAndBundlesSingleCSV(t *testing.T) {
	svc := setupService(t)
	content := "Time,Message\n2026-01-01T00:00:00Z,\"hello\"\n"
	wantContent := "Time,Message\n\"2026-01-01T00:00:00Z\",\"hello\"\n"
	info, err := svc.SaveCommandArtifact("../../remote-miner-logs.csv", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)

	filePath, err := svc.SaveCommandArtifactLog("batch-artifact-single", "AA:BB:CC:DD:EE:FF", info.ID)
	require.NoError(t, err)

	assert.Equal(t, getBatchLogsDirPath("batch-artifact-single"), filepath.Dir(filePath))
	assert.True(t, strings.HasPrefix(filepath.Base(filePath), "miner-logs-aabbccddeeff-"))
	assert.True(t, strings.HasSuffix(filepath.Base(filePath), ".csv"))
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, wantContent, string(data))
	assert.NoDirExists(t, getCommandArtifactDirPath(info.ID))

	bundlePath, err := svc.bundleLogs("batch-artifact-single")
	require.NoError(t, err)
	assert.Equal(t, getBatchLogsSingleFilePath("batch-artifact-single"), bundlePath)

	fsFile, err := svc.GetBatchLogBundleFile("batch-artifact-single")
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(filePath), fsFile.Filename)
	assert.Equal(t, wantContent, string(fsFile.Data))
}

func TestSaveCommandArtifactLog_SanitizesUploadedCSV(t *testing.T) {
	svc := setupService(t)
	content := "Time,Type,Message\n=cmd,INFO,+message\n2026-01-01T00:00:00Z,WARN,\" \t@nested\"\n"
	info, err := svc.SaveCommandArtifact("remote-miner-logs.csv", int64(len(content)), checksumOf(content), strings.NewReader(content))
	require.NoError(t, err)

	filePath, err := svc.SaveCommandArtifactLog("batch-sanitize-artifact", "aa:bb:cc:dd:ee:ff", info.ID)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "Time,Type,Message\n\"'=cmd\",\"INFO\",\"'+message\"\n\"2026-01-01T00:00:00Z\",\"WARN\",\"' \t@nested\"\n", string(data))
	assert.NoDirExists(t, getCommandArtifactDirPath(info.ID))
}

// TestBundleLogs_MultipleFiles_CreatesZIPWithNameSidecar verifies that when logs from
// multiple devices are present they are bundled into a ZIP, and a .name sidecar is
// written with a human-readable filename matching the miner-logs-{timestamp}.zip pattern.
func TestBundleLogs_MultipleFiles_CreatesZIPWithNameSidecar(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveLogs("batch-multi", "aa:bb:cc:dd:ee:01", []string{"line1"})
	require.NoError(t, err)
	_, err = svc.SaveLogs("batch-multi", "aa:bb:cc:dd:ee:02", []string{"line2"})
	require.NoError(t, err)

	bundlePath, err := svc.bundleLogs("batch-multi")

	require.NoError(t, err)
	assert.Equal(t, getBatchLogsZipFilePath("batch-multi"), bundlePath)

	_, statErr := os.Stat(bundlePath)
	assert.NoError(t, statErr, "ZIP should exist at bundle path")

	sidecar, readErr := os.ReadFile(bundlePath + ".name")
	require.NoError(t, readErr)
	zipName := string(sidecar)
	assert.True(t, strings.HasPrefix(zipName, "miner-logs-"), "ZIP name should start with miner-logs-")
	assert.True(t, strings.HasSuffix(zipName, ".zip"), "ZIP name should end with .zip")
}

// TestBundleLogs_MultipleFiles_ZIPContainsAllFiles verifies that every per-device CSV
// is included in the produced ZIP archive.
func TestBundleLogs_MultipleFiles_ZIPContainsAllFiles(t *testing.T) {
	svc := setupService(t)

	file1, err := svc.SaveLogs("batch-zip-contents", "aa:bb:cc:dd:ee:01", []string{"a"})
	require.NoError(t, err)
	file2, err := svc.SaveLogs("batch-zip-contents", "aa:bb:cc:dd:ee:02", []string{"b"})
	require.NoError(t, err)

	bundlePath, err := svc.bundleLogs("batch-zip-contents")
	require.NoError(t, err)

	zr, err := zip.OpenReader(bundlePath)
	require.NoError(t, err)
	defer zr.Close()

	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, filepath.Base(file1))
	assert.Contains(t, names, filepath.Base(file2))
}

func TestSaveCommandArtifactLog_BundlesMixedDirectAndRemoteLogsAsZIP(t *testing.T) {
	svc := setupService(t)
	directPath, err := svc.SaveLogs("batch-mixed", "aa:bb:cc:dd:ee:01", []string{"direct-line"})
	require.NoError(t, err)
	remoteContent := "Time,Message\n2026-01-01T00:00:00Z,remote-line\n"
	wantRemoteContent := "Time,Message\n\"2026-01-01T00:00:00Z\",\"remote-line\"\n"
	info, err := svc.SaveCommandArtifact("remote-miner-logs.csv", int64(len(remoteContent)), checksumOf(remoteContent), strings.NewReader(remoteContent))
	require.NoError(t, err)
	remotePath, err := svc.SaveCommandArtifactLog("batch-mixed", "aa:bb:cc:dd:ee:02", info.ID)
	require.NoError(t, err)

	bundlePath, err := svc.bundleLogs("batch-mixed")
	require.NoError(t, err)
	assert.Equal(t, getBatchLogsZipFilePath("batch-mixed"), bundlePath)

	contents := readZipFileContents(t, bundlePath)
	assert.Equal(t, "direct-line\n", contents[filepath.Base(directPath)])
	assert.Equal(t, wantRemoteContent, contents[filepath.Base(remotePath)])
}

// TestBundleLogs_NoFiles_ReturnsEmpty verifies that bundling a batch with no log files
// returns an empty path without error (all devices may have failed).
func TestBundleLogs_NoFiles_ReturnsEmpty(t *testing.T) {
	svc := setupService(t)

	bundlePath, err := svc.bundleLogs("batch-empty")

	require.NoError(t, err)
	assert.Empty(t, bundlePath)
}

// TestGetBatchLogBundleFile_UsesNameSidecar verifies that when a .name sidecar exists
// the returned FSFile carries the sidecar filename rather than the temp path basename.
func TestGetBatchLogBundleFile_UsesNameSidecar(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveLogs("batch-sidecar", "cc:dd:ee:ff:00:11", []string{"Time,Message", `2026-01-01T00:00:00Z,"data"`})
	require.NoError(t, err)
	_, err = svc.bundleLogs("batch-sidecar")
	require.NoError(t, err)

	fsFile, err := svc.GetBatchLogBundleFile("batch-sidecar")

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(fsFile.Filename, "miner-logs-"), "filename should use sidecar name")
	assert.True(t, strings.HasSuffix(fsFile.Filename, ".csv"))
	assert.NotEmpty(t, fsFile.Data)
}

// TestGetBatchLogBundleFile_NotReady returns an error when the bundle does not exist yet.
func TestGetBatchLogBundleFile_NotReady(t *testing.T) {
	svc := setupService(t)

	_, err := svc.GetBatchLogBundleFile("batch-missing")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available yet")
}

// TestFindBatchBundlePath_PrefersZIPOverCSV verifies that when both a ZIP and a CSV
// happen to exist for the same batch UUID the ZIP path is returned.
func TestFindBatchBundlePath_PrefersZIPOverCSV(t *testing.T) {
	setupService(t)

	uuid := "batch-prefer-zip"
	zipPath := getBatchLogsZipFilePath(uuid)
	csvPath := getBatchLogsSingleFilePath(uuid)

	require.NoError(t, os.MkdirAll(tempDir, 0750))
	require.NoError(t, os.WriteFile(zipPath, []byte("zip"), 0600))
	require.NoError(t, os.WriteFile(csvPath, []byte("csv"), 0600))

	assert.Equal(t, zipPath, findBatchBundlePath(uuid))
}

// TestFindBatchBundlePath_ReturnsEmptyWhenMissing returns "" when neither bundle exists.
func TestFindBatchBundlePath_ReturnsEmptyWhenMissing(t *testing.T) {
	setupService(t)
	assert.Empty(t, findBatchBundlePath("batch-nonexistent"))
}

// TestBatchLogCleanup_RemovesAllFiles verifies that cleanup removes the batch directory,
// the bundle file, and the .name sidecar — leaving no trace behind.
func TestBatchLogCleanup_RemovesAllFiles(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveLogs("batch-cleanup", "ff:ee:dd:cc:bb:aa", []string{"log line"})
	require.NoError(t, err)
	_, err = svc.bundleLogs("batch-cleanup")
	require.NoError(t, err)

	err = svc.batchLogCleanup("batch-cleanup")
	require.NoError(t, err)

	assert.NoDirExists(t, getBatchLogsDirPath("batch-cleanup"))
	assert.NoFileExists(t, getBatchLogsSingleFilePath("batch-cleanup"))
	assert.NoFileExists(t, getBatchLogsSingleFilePath("batch-cleanup")+".name")
	assert.NoFileExists(t, getBatchLogsZipFilePath("batch-cleanup"))
	assert.NoFileExists(t, getBatchLogsZipFilePath("batch-cleanup")+".name")
}

func TestSaveCommandArtifactLog_RejectsMissingAndCorruptArtifacts(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		svc := setupService(t)

		_, err := svc.SaveCommandArtifactLog("batch-missing-artifact", "aa:bb:cc:dd:ee:ff", "00000000-0000-0000-0000-000000000000")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "command artifact not found")
		assert.NoDirExists(t, getBatchLogsDirPath("batch-missing-artifact"))
	})

	t.Run("corrupt", func(t *testing.T) {
		svc := setupService(t)
		content := "Time,Message\n,aaaa\n"
		info, err := svc.SaveCommandArtifact("remote-miner-logs.csv", int64(len(content)), checksumOf(content), strings.NewReader(content))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(getCommandArtifactDirPath(info.ID), info.Filename), []byte("Time,Message\n,bbbb\n"), 0600))

		filePath, err := svc.SaveCommandArtifactLog("batch-corrupt-artifact", "aa:bb:cc:dd:ee:ff", info.ID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sha256 mismatch")
		assert.Empty(t, filePath)
		entries, readErr := os.ReadDir(getBatchLogsDirPath("batch-corrupt-artifact"))
		if !os.IsNotExist(readErr) {
			require.NoError(t, readErr)
			assert.Empty(t, entries)
		}
	})
}

func TestSaveCommandArtifactLog_RejectsMalformedCSV(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "unexpected header",
			content: "Timestamp,Message\n2026-01-01T00:00:00Z,hello\n",
			wantErr: "unexpected miner log csv header",
		},
		{
			name:    "malformed row",
			content: "Time,Message\n\"unterminated\n",
			wantErr: "read csv row",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := setupService(t)
			info, err := svc.SaveCommandArtifact("remote-miner-logs.csv", int64(len(tc.content)), checksumOf(tc.content), strings.NewReader(tc.content))
			require.NoError(t, err)

			filePath, err := svc.SaveCommandArtifactLog("batch-malformed-artifact", "aa:bb:cc:dd:ee:ff", info.ID)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Empty(t, filePath)
			entries, readErr := os.ReadDir(getBatchLogsDirPath("batch-malformed-artifact"))
			if !os.IsNotExist(readErr) {
				require.NoError(t, readErr)
				assert.Empty(t, entries)
			}
			assert.DirExists(t, getCommandArtifactDirPath(info.ID))
		})
	}
}

func TestSaveCommandArtifactLog_RejectsOversizedMinerLogs(t *testing.T) {
	svc := setupService(t)
	content := bytes.Repeat([]byte("x"), int(logformat.MaxArtifactBytes)+1)
	info, err := svc.SaveCommandArtifact("remote-miner-logs.csv", int64(len(content)), checksumOf(string(content)), bytes.NewReader(content))
	require.NoError(t, err)

	filePath, err := svc.SaveCommandArtifactLog("batch-oversized-artifact", "aa:bb:cc:dd:ee:ff", info.ID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "miner log artifact too large")
	assert.Empty(t, filePath)
	assert.NoDirExists(t, getBatchLogsDirPath("batch-oversized-artifact"))
}

func readZipFileContents(t *testing.T, zipPath string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	defer zr.Close()

	contents := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		reader, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		require.NoError(t, err)
		require.NoError(t, closeErr)
		contents[f.Name] = string(data)
	}
	return contents
}
