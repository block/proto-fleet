package files

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func checksumOf(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func firmwareFileEntries(t *testing.T) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(firmwareDir)
	require.NoError(t, err)
	var filtered []os.DirEntry
	for _, e := range entries {
		if e.Name() != "staging" {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func TestValidateFirmwareFile_AcceptsAllowedExtensions(t *testing.T) {
	svc := setupService(t)

	assert.NoError(t, svc.ValidateFirmwareFile("firmware-v2.0.swu", 1024))
	assert.NoError(t, svc.ValidateFirmwareFile("upgrade.tar.gz", 1024))
	assert.NoError(t, svc.ValidateFirmwareFile("firmware.zip", 1024))
	assert.NoError(t, svc.ValidateFirmwareFile("FIRMWARE.SWU", 1024))
	assert.NoError(t, svc.ValidateFirmwareFile("upgrade.TAR.GZ", 1024))
	assert.NoError(t, svc.ValidateFirmwareFile("FIRMWARE.ZIP", 1024))
}

func TestValidateFirmwareFile_RejectsInvalidExtensions(t *testing.T) {
	svc := setupService(t)

	err := svc.ValidateFirmwareFile("firmware.bin", 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported firmware file type")

	err = svc.ValidateFirmwareFile("firmware.exe", 1024)
	require.Error(t, err)
}

func TestValidateFirmwareFile_RejectsEmptyFilename(t *testing.T) {
	svc := setupService(t)

	err := svc.ValidateFirmwareFile("", 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filename is required")
}

func TestValidateFirmwareFile_RejectsZeroSize(t *testing.T) {
	svc := setupService(t)

	err := svc.ValidateFirmwareFile("firmware.swu", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")
}

func TestValidateFirmwareFile_RejectsNegativeSize(t *testing.T) {
	svc := setupService(t)

	err := svc.ValidateFirmwareFile("firmware.swu", -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")
}

func TestValidateFirmwareFile_RejectsOversizedFile(t *testing.T) {
	svc := setupService(t)
	svc.maxFirmwareFileSize = 1000

	err := svc.ValidateFirmwareFile("firmware.swu", 2000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestValidateFirmwareFile_AcceptsExactMaxSize(t *testing.T) {
	svc := setupService(t)
	svc.maxFirmwareFileSize = 1000

	assert.NoError(t, svc.ValidateFirmwareFile("firmware.swu", 1000))
}

func TestSaveFirmwareFile_StreamsToDisk(t *testing.T) {
	svc := setupService(t)

	content := "fake firmware content"
	reader := strings.NewReader(content)

	fileID, err := svc.SaveFirmwareFile("firmware-v2.0.swu", reader)

	require.NoError(t, err)
	assert.NotEmpty(t, fileID)

	filePath, err := svc.GetFirmwareFilePath(fileID)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
	assert.Equal(t, "firmware-v2.0.swu", filepath.Base(filePath))
}

func TestSaveFirmwareFile_SanitizesFilename(t *testing.T) {
	svc := setupService(t)

	fileID, err := svc.SaveFirmwareFile("../../etc/passwd", strings.NewReader("data"))

	require.NoError(t, err)

	filePath, err := svc.GetFirmwareFilePath(fileID)
	require.NoError(t, err)
	assert.Equal(t, "passwd", filepath.Base(filePath))
}

func TestSaveFirmwareFile_RejectsOversizedStream(t *testing.T) {
	svc := setupService(t)
	svc.maxFirmwareFileSize = 10

	oversizedData := strings.Repeat("x", 20)
	_, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(oversizedData))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")

	assert.Empty(t, firmwareFileEntries(t), "oversized upload should not leave files behind")
}

func TestSaveFirmwareFile_IdenticalContentGetsDifferentIDs(t *testing.T) {
	svc := setupService(t)

	content := "identical firmware content"
	id1, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	id2, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2, "each save should produce a unique fileID even for identical content")
}

func TestSaveFirmwareFile_DifferentContentGetsDifferentID(t *testing.T) {
	svc := setupService(t)

	id1, err := svc.SaveFirmwareFile("firmware-a.swu", strings.NewReader("content A"))
	require.NoError(t, err)

	id2, err := svc.SaveFirmwareFile("firmware-b.swu", strings.NewReader("content B"))
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2, "different content should produce different fileIDs")
}

func TestSaveFirmwareFile_EachSaveCreatesOwnDirectory(t *testing.T) {
	svc := setupService(t)

	content := "same content twice"
	_, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	_, err = svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	assert.Len(t, firmwareFileEntries(t), 2, "each save should create its own directory on disk")
}

func TestGetFirmwareFilePath_ReturnsErrorForMissing(t *testing.T) {
	svc := setupService(t)

	_, err := svc.GetFirmwareFilePath("00000000-0000-0000-0000-000000000000")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware file not found")
}

func TestGetFirmwareFilePath_RejectsPathTraversalInFileID(t *testing.T) {
	svc := setupService(t)

	_, err := svc.GetFirmwareFilePath("../logs/tmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid firmware file ID")

	_, err = svc.GetFirmwareFilePath("not-a-uuid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid firmware file ID")

	_, err = svc.GetFirmwareFilePath("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid firmware file ID")
}

func TestOpenFirmwareFile_ReturnsReaderAndMetadata(t *testing.T) {
	svc := setupService(t)

	content := "firmware binary data here"
	fileID, err := svc.SaveFirmwareFile("update.swu", strings.NewReader(content))
	require.NoError(t, err)

	reader, filename, size, err := svc.OpenFirmwareFile(fileID)
	require.NoError(t, err)
	defer reader.Close()

	assert.Equal(t, "update.swu", filename)
	assert.Equal(t, int64(len(content)), size)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestOpenFirmwareFile_ReturnsErrorForMissing(t *testing.T) {
	svc := setupService(t)

	_, _, _, err := svc.OpenFirmwareFile("00000000-0000-0000-0000-000000000000")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware file not found")
}

func TestOpenFirmwareFile_RejectsPathTraversal(t *testing.T) {
	svc := setupService(t)

	_, _, _, err := svc.OpenFirmwareFile("../logs/tmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid firmware file ID")
}

func TestFindFirmwareFileByChecksum_ReturnsTrueForExistingFile(t *testing.T) {
	svc := setupService(t)

	content := "findable firmware content"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	foundID, ok := svc.FindFirmwareFileByChecksum(checksumOf(content))
	assert.True(t, ok)
	assert.Equal(t, fileID, foundID)
}

func TestFindFirmwareFileByChecksum_ReturnsFalseForUnknownChecksum(t *testing.T) {
	svc := setupService(t)

	foundID, ok := svc.FindFirmwareFileByChecksum("0000000000000000000000000000000000000000000000000000000000000000")
	assert.False(t, ok)
	assert.Empty(t, foundID)
}

func TestFindFirmwareFileByChecksum_ReturnsFalseAfterDelete(t *testing.T) {
	svc := setupService(t)

	content := "firmware to find then delete"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	err = svc.DeleteFirmwareFile(fileID)
	require.NoError(t, err)

	_, ok := svc.FindFirmwareFileByChecksum(checksumOf(content))
	assert.False(t, ok)
}

func TestDeleteFirmwareFile_RemovesDirectory(t *testing.T) {
	svc := setupService(t)

	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader("data"))
	require.NoError(t, err)

	dir := getFirmwareDirPath(fileID)
	assert.DirExists(t, dir)

	err = svc.DeleteFirmwareFile(fileID)
	require.NoError(t, err)

	assert.NoDirExists(t, dir)
}

func TestDeleteFirmwareFile_RemovesFromChecksumIndex(t *testing.T) {
	svc := setupService(t)

	content := "firmware to delete and re-upload"
	fileID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	err = svc.DeleteFirmwareFile(fileID)
	require.NoError(t, err)

	assert.Empty(t, svc.checksumIndex, "checksumIndex should be empty after delete")

	newID, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)
	assert.NotEqual(t, fileID, newID, "re-upload after delete should get a new fileID")
}

func TestDeleteFirmwareFile_ReturnsNotFoundForMissingFile(t *testing.T) {
	svc := setupService(t)

	err := svc.DeleteFirmwareFile("00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware file not found")
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestDeleteFirmwareFile_RejectsInvalidFileID(t *testing.T) {
	svc := setupService(t)

	err := svc.DeleteFirmwareFile("../logs/tmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid firmware file ID")
}

func TestNewService_CreatesFirmwareDir(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	_, err := NewService(Config{})
	require.NoError(t, err)

	assert.DirExists(t, filepath.Join(tmp, firmwareDir))
}

func TestConfig_MaxFirmwareFileSizeOverridesDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	customMax := int64(100 * 1024 * 1024)
	svc, err := NewService(Config{MaxFirmwareFileSize: customMax})
	require.NoError(t, err)

	assert.Equal(t, customMax, svc.maxFirmwareFileSize)
}

func TestNewService_PreservesExistingFirmwareFilesAcrossRestart(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	svc, err := NewService(Config{})
	require.NoError(t, err)

	content := "persisted firmware data"
	fileID, err := svc.SaveFirmwareFile("persisted.swu", strings.NewReader(content))
	require.NoError(t, err)

	restartedSvc, err := NewService(Config{})
	require.NoError(t, err)

	reader, filename, size, err := restartedSvc.OpenFirmwareFile(fileID)
	require.NoError(t, err)
	defer reader.Close()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, "persisted.swu", filename)
	assert.Equal(t, int64(len(content)), size)
	assert.Equal(t, content, string(data))
}

func TestInitChecksumIndex_RebuildsOnRestart(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	svc1, err := NewService(Config{})
	require.NoError(t, err)

	content := "firmware for restart test"
	fileID, err := svc1.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	svc2, err := NewService(Config{})
	require.NoError(t, err)

	foundID, ok := svc2.FindFirmwareFileByChecksum(checksumOf(content))
	assert.True(t, ok, "after restart, checksum index should contain the existing file")
	assert.Equal(t, fileID, foundID)
}

func TestSaveFirmwareFile_RejectsEmptyStream(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(""))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	assert.Empty(t, firmwareFileEntries(t), "empty upload should not leave files behind")
}

func TestSaveFirmwareFile_IndependentDeletion(t *testing.T) {
	svc := setupService(t)

	content := "shared firmware content"
	id1, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	id2, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	err = svc.DeleteFirmwareFile(id1)
	require.NoError(t, err)

	reader, filename, size, err := svc.OpenFirmwareFile(id2)
	require.NoError(t, err, "deleting one copy should not affect the other")
	defer reader.Close()

	assert.Equal(t, "firmware.swu", filename)
	assert.Equal(t, int64(len(content)), size)
}

func TestFindFirmwareFileByChecksum_SurvivesPartialDeletion(t *testing.T) {
	svc := setupService(t)

	content := "firmware uploaded by two batches"
	id1, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	id2, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	err = svc.DeleteFirmwareFile(id2)
	require.NoError(t, err)

	foundID, ok := svc.FindFirmwareFileByChecksum(checksumOf(content))
	assert.True(t, ok, "checksum lookup should still succeed after deleting one of two identical uploads")
	assert.Equal(t, id1, foundID)
}

func TestValidateFirmwareFilename_AcceptsAllowedExtensions(t *testing.T) {
	svc := setupService(t)

	assert.NoError(t, svc.ValidateFirmwareFilename("firmware-v2.0.swu"))
	assert.NoError(t, svc.ValidateFirmwareFilename("upgrade.tar.gz"))
	assert.NoError(t, svc.ValidateFirmwareFilename("firmware.zip"))
	assert.NoError(t, svc.ValidateFirmwareFilename("FIRMWARE.SWU"))
	assert.NoError(t, svc.ValidateFirmwareFilename("upgrade.TAR.GZ"))
	assert.NoError(t, svc.ValidateFirmwareFilename("FIRMWARE.ZIP"))
}

func TestValidateFirmwareFilename_RejectsInvalidExtensions(t *testing.T) {
	svc := setupService(t)

	err := svc.ValidateFirmwareFilename("firmware.bin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported firmware file type")

	err = svc.ValidateFirmwareFilename("firmware.exe")
	require.Error(t, err)
}

func TestValidateFirmwareFilename_RejectsEmptyFilename(t *testing.T) {
	svc := setupService(t)

	err := svc.ValidateFirmwareFilename("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filename is required")
}

func TestValidateFirmwareFilename_DoesNotCheckSize(t *testing.T) {
	svc := setupService(t)

	assert.NoError(t, svc.ValidateFirmwareFilename("firmware.swu"),
		"ValidateFirmwareFilename should not reject based on size")
}

func TestAllowedExtensionsList_IsDeterministic(t *testing.T) {
	first := allowedExtensionsList()
	for range 10 {
		assert.Equal(t, first, allowedExtensionsList())
	}
}

func TestSaveFirmwareFileFromPath_MovesAndRegistersChecksum(t *testing.T) {
	svc := setupService(t)

	content := "firmware via chunked upload"
	srcPath := filepath.Join(firmwareStagingDir, "test-upload")
	require.NoError(t, os.WriteFile(srcPath, []byte(content), 0600))

	fileID, err := svc.SaveFirmwareFileFromPath("chunked.swu", srcPath)
	require.NoError(t, err)
	assert.NotEmpty(t, fileID)

	filePath, err := svc.GetFirmwareFilePath(fileID)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
	assert.Equal(t, "chunked.swu", filepath.Base(filePath))

	foundID, ok := svc.FindFirmwareFileByChecksum(checksumOf(content))
	assert.True(t, ok, "checksum index should contain the file after SaveFirmwareFileFromPath")
	assert.Equal(t, fileID, foundID)

	_, statErr := os.Stat(srcPath)
	assert.True(t, os.IsNotExist(statErr), "source file should be removed after rename")
}

func TestSaveFirmwareFileFromPath_RejectsEmptyFile(t *testing.T) {
	svc := setupService(t)

	srcPath := filepath.Join(firmwareStagingDir, "empty-upload")
	require.NoError(t, os.WriteFile(srcPath, []byte{}, 0600))

	_, err := svc.SaveFirmwareFileFromPath("empty.swu", srcPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestListFirmwareFiles_EmptyReturnsEmptySlice(t *testing.T) {
	svc := setupService(t)

	files, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	assert.NotNil(t, files, "should return empty slice, not nil")
	assert.Empty(t, files)
}

func TestListFirmwareFiles_ReturnsSavedFiles(t *testing.T) {
	svc := setupService(t)

	id1, err := svc.SaveFirmwareFile("alpha.swu", strings.NewReader("alpha content"))
	require.NoError(t, err)

	id2, err := svc.SaveFirmwareFile("beta.tar.gz", strings.NewReader("beta content here"))
	require.NoError(t, err)

	files, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	assert.Len(t, files, 2)

	ids := map[string]bool{files[0].ID: true, files[1].ID: true}
	assert.True(t, ids[id1], "should contain first file ID")
	assert.True(t, ids[id2], "should contain second file ID")

	for _, f := range files {
		if f.ID == id1 {
			assert.Equal(t, "alpha.swu", f.Filename)
			assert.Equal(t, int64(len("alpha content")), f.Size)
		} else {
			assert.Equal(t, "beta.tar.gz", f.Filename)
			assert.Equal(t, int64(len("beta content here")), f.Size)
		}
		assert.False(t, f.UploadedAt.IsZero(), "upload time should be set")
	}
}

func TestListFirmwareFiles_SkipsStagingDir(t *testing.T) {
	svc := setupService(t)

	// Place a file in the staging dir
	require.NoError(t, os.WriteFile(filepath.Join(firmwareStagingDir, "orphan"), []byte("data"), 0600))

	_, err := svc.SaveFirmwareFile("real.swu", strings.NewReader("real content"))
	require.NoError(t, err)

	files, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	assert.Len(t, files, 1, "staging files should not appear in the list")
	assert.Equal(t, "real.swu", files[0].Filename)
}

func TestListFirmwareFiles_SortedByUploadTimeDescending(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveFirmwareFile("first.swu", strings.NewReader("first"))
	require.NoError(t, err)

	_, err = svc.SaveFirmwareFile("second.swu", strings.NewReader("second"))
	require.NoError(t, err)

	files, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.True(t, !files[0].UploadedAt.Before(files[1].UploadedAt),
		"first entry should have the most recent upload time")
}

func TestDeleteAllFirmwareFiles_RemovesAllFiles(t *testing.T) {
	svc := setupService(t)

	_, err := svc.SaveFirmwareFile("one.swu", strings.NewReader("one"))
	require.NoError(t, err)
	_, err = svc.SaveFirmwareFile("two.swu", strings.NewReader("two"))
	require.NoError(t, err)

	deleted, err := svc.DeleteAllFirmwareFiles()
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	files, err := svc.ListFirmwareFiles()
	require.NoError(t, err)
	assert.Empty(t, files, "no files should remain after delete-all")
}

func TestDeleteAllFirmwareFiles_EmptyReturnsZero(t *testing.T) {
	svc := setupService(t)

	deleted, err := svc.DeleteAllFirmwareFiles()
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)
}

func TestDeleteAllFirmwareFiles_CleansChecksumIndex(t *testing.T) {
	svc := setupService(t)

	content := "firmware for checksum cleanup test"
	_, err := svc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	_, ok := svc.FindFirmwareFileByChecksum(checksumOf(content))
	require.True(t, ok, "checksum should be found before delete-all")

	_, err = svc.DeleteAllFirmwareFiles()
	require.NoError(t, err)

	_, ok = svc.FindFirmwareFileByChecksum(checksumOf(content))
	assert.False(t, ok, "checksum should not be found after delete-all")
}
