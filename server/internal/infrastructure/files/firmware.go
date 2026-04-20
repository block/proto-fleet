package files

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/google/uuid"
)

// FirmwareFileInfo holds metadata about a stored firmware file.
type FirmwareFileInfo struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploaded_at"`
}

const firmwareDir = "firmware"
const firmwareStagingDir = "firmware/staging"

const defaultMaxFirmwareFileSize int64 = 500 * 1024 * 1024 // 500 MB

// allowedFirmwareExtensions lists file suffixes accepted for firmware uploads.
// .swu is the Proto Rig MDK firmware format, .tar.gz is the standard Antminer format.
// Checked case-insensitively via hasAllowedFirmwareExtension.
var allowedFirmwareExtensions = []string{".swu", ".tar.gz", ".zip"}

// AllowedFirmwareExtensions returns a copy of the allowed firmware file extensions.
func AllowedFirmwareExtensions() []string {
	out := make([]string, len(allowedFirmwareExtensions))
	copy(out, allowedFirmwareExtensions)
	return out
}

func getFirmwareDirPath(fileID string) string {
	return filepath.Join(firmwareDir, fileID)
}

// canonicalizeFirmwareFileID validates and normalizes a firmware file ID.
// uuid.Parse accepts multiple textual forms (uppercase, urn:uuid:, braced),
// so we normalize to the lowercase hyphenated form to ensure consistent
// on-disk paths.
func canonicalizeFirmwareFileID(fileID string) (string, error) {
	parsed, err := uuid.Parse(fileID)
	if err != nil {
		return "", fleeterror.NewInvalidArgumentErrorf("invalid firmware file ID: %s", fileID)
	}
	return parsed.String(), nil
}

// initFirmwareDir creates the firmware root directory if it doesn't exist.
// Existing firmware uploads are preserved across service restarts.
// Callers are responsible for deleting files when they are no longer needed
// via DeleteFirmwareFile.
func initFirmwareDir() error {
	if err := os.MkdirAll(firmwareDir, 0750); err != nil {
		return fleeterror.NewInternalErrorf("failed to create firmware dir: %v", err)
	}
	if err := os.MkdirAll(firmwareStagingDir, 0750); err != nil {
		return fleeterror.NewInternalErrorf("failed to create firmware staging dir: %v", err)
	}
	cleanStagingDir()
	return nil
}

// cleanStagingDir removes leftover temp files from previous runs. Since upload
// sessions are in-memory only, any files in the staging directory at startup
// are orphans from interrupted uploads.
func cleanStagingDir() {
	entries, err := os.ReadDir(firmwareStagingDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(firmwareStagingDir, entry.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("failed to remove orphaned staging file", "path", path, "error", err)
		} else {
			slog.Info("removed orphaned staging file", "path", path)
		}
	}
}

// StagingDir returns the path to the firmware staging directory for chunked uploads.
func StagingDir() string {
	return firmwareStagingDir
}

// ValidateFirmwareFilename checks that the filename is non-empty and has an
// allowed extension. Use this when the file size is not yet known (e.g.,
// streaming multipart uploads).
func (s *Service) ValidateFirmwareFilename(filename string) error {
	if filename == "" {
		return fleeterror.NewInvalidArgumentError("firmware filename is required")
	}
	if !hasAllowedFirmwareExtension(filename) {
		return fleeterror.NewInvalidArgumentErrorf("unsupported firmware file type %q (allowed: %s)",
			filename, allowedExtensionsList())
	}
	return nil
}

// ValidateFirmwareFile checks that the filename has an allowed extension and the
// size does not exceed the configured maximum. It should be called before saving
// when the file size is known upfront.
func (s *Service) ValidateFirmwareFile(filename string, size int64) error {
	if filename == "" {
		return fleeterror.NewInvalidArgumentError("firmware filename is required")
	}

	if !hasAllowedFirmwareExtension(filename) {
		return fleeterror.NewInvalidArgumentErrorf("unsupported firmware file type %q (allowed: %s)",
			filename, allowedExtensionsList())
	}

	if size <= 0 {
		return fleeterror.NewInvalidArgumentError("firmware file size must be greater than zero")
	}

	maxSize := s.maxFirmwareFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFirmwareFileSize
	}
	if size > maxSize {
		return fleeterror.NewInvalidArgumentErrorf("firmware file too large: %d bytes (max: %d bytes)", size, maxSize)
	}

	return nil
}

// SaveFirmwareFile streams a firmware file to disk and returns a unique file ID.
// Each call always creates a new copy on disk — deduplication is handled at the
// upload layer via FindFirmwareFileByChecksum (Ticket 3's check endpoint lets
// clients skip redundant uploads). This ensures each batch owns its file and
// can safely delete it on completion without affecting other batches.
//
// Callers should call ValidateFirmwareFile or ValidateFirmwareFilename before
// saving to ensure the filename extension is acceptable.
func (s *Service) SaveFirmwareFile(filename string, reader io.Reader) (string, error) {
	fileID := id.GenerateID()
	dir := getFirmwareDirPath(fileID)

	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create firmware file dir: %v", err)
	}

	sanitized := sanitizeFirmwareFilename(filename)
	filePath := filepath.Join(dir, sanitized)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInternalErrorf("failed to create firmware file: %v", err)
	}
	defer file.Close()

	maxSize := s.maxFirmwareFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFirmwareFileSize
	}
	limitedReader := io.LimitReader(reader, maxSize+1)

	hasher := sha256.New()
	teeReader := io.TeeReader(limitedReader, hasher)

	written, err := io.Copy(file, teeReader)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInternalErrorf("failed to write firmware file: %v", err)
	}
	if written > maxSize {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInvalidArgumentErrorf("firmware file too large: exceeded %d byte limit during upload", maxSize)
	}
	if written == 0 {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInvalidArgumentError("firmware file is empty")
	}

	if err := file.Sync(); err != nil {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInternalErrorf("failed to sync firmware file to disk: %v", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	s.mu.Lock()
	s.checksumIndex[checksum] = append(s.checksumIndex[checksum], fileID)
	s.mu.Unlock()

	slog.Info("firmware file saved", "file_id", fileID, "filename", sanitized, "checksum", checksum)
	return fileID, nil
}

// SaveFirmwareFileFromPath moves an existing file (e.g. from the staging directory)
// into the standard firmware directory, computes its SHA-256 checksum, and registers
// it in the checksum index. Uses os.Rename for efficiency — both paths must be on
// the same filesystem. Used by the chunked upload complete handler.
func (s *Service) SaveFirmwareFileFromPath(filename string, srcPath string) (string, error) {
	fileID := id.GenerateID()
	dir := getFirmwareDirPath(fileID)

	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create firmware file dir: %v", err)
	}

	sanitized := sanitizeFirmwareFilename(filename)
	destPath := filepath.Join(dir, sanitized)

	if err := os.Rename(srcPath, destPath); err != nil {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInternalErrorf("failed to move firmware file: %v", err)
	}

	checksum, err := computeFileChecksum(destPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInternalErrorf("failed to compute checksum after move: %v", err)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInternalErrorf("failed to stat firmware file: %v", err)
	}
	if info.Size() == 0 {
		_ = os.RemoveAll(dir)
		return "", fleeterror.NewInvalidArgumentError("firmware file is empty")
	}

	s.mu.Lock()
	s.checksumIndex[checksum] = append(s.checksumIndex[checksum], fileID)
	s.mu.Unlock()

	slog.Info("firmware file saved from path", "file_id", fileID, "filename", sanitized, "checksum", checksum)
	return fileID, nil
}

// GetFirmwareFilePath returns the on-disk path for a firmware file ID.
// Returns an error if the file does not exist.
func (s *Service) GetFirmwareFilePath(fileID string) (string, error) {
	canonical, err := canonicalizeFirmwareFileID(fileID)
	if err != nil {
		return "", err
	}

	dir := getFirmwareDirPath(canonical)
	path, err := findSingleFileInDir(dir)
	if err != nil {
		return "", fleeterror.NewNotFoundErrorf("firmware file not found: %s", canonical)
	}
	return path, nil
}

// OpenFirmwareFile opens the firmware file for reading and returns the reader,
// original filename, and file size. The caller is responsible for closing the reader.
func (s *Service) OpenFirmwareFile(fileID string) (io.ReadCloser, string, int64, error) {
	filePath, err := s.GetFirmwareFilePath(fileID)
	if err != nil {
		return nil, "", 0, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to open firmware file: %v", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to stat firmware file: %v", err)
	}

	return file, filepath.Base(filePath), info.Size(), nil
}

// FindFirmwareFileByChecksum looks up a firmware file by its SHA-256 hex digest.
// Returns the file ID and true if found, or empty string and false otherwise.
// Used by the pre-upload check endpoint to let clients skip redundant uploads.
func (s *Service) FindFirmwareFileByChecksum(sha256Hex string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.checksumIndex[sha256Hex]
	if len(ids) == 0 {
		return "", false
	}
	return ids[0], true
}

// DeleteFirmwareFile removes a firmware file from disk and the checksum index.
// Returns a NotFoundError if no file with the given ID exists.
func (s *Service) DeleteFirmwareFile(fileID string) error {
	canonical, err := canonicalizeFirmwareFileID(fileID)
	if err != nil {
		return err
	}

	dir := getFirmwareDirPath(canonical)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fleeterror.NewNotFoundErrorf("firmware file not found: %s", canonical)
		}
		return fleeterror.NewInternalErrorf("failed to stat firmware dir %s: %v", canonical, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fleeterror.NewInternalErrorf("failed to remove firmware dir %s: %v", canonical, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for checksum, ids := range s.checksumIndex {
		for i, fid := range ids {
			if fid == canonical {
				ids = append(ids[:i], ids[i+1:]...)
				if len(ids) == 0 {
					delete(s.checksumIndex, checksum)
				} else {
					s.checksumIndex[checksum] = ids
				}
				goto indexDone
			}
		}
	}
indexDone:

	slog.Info("firmware file deleted", "file_id", canonical)
	return nil
}

// ListFirmwareFiles returns metadata for all stored firmware files, sorted by
// upload time (newest first). Returns an empty slice when no files exist.
func (s *Service) ListFirmwareFiles() ([]FirmwareFileInfo, error) {
	entries, err := os.ReadDir(firmwareDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read firmware dir: %w", err)
	}

	result := make([]FirmwareFileInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "staging" {
			continue
		}
		fileID, err := canonicalizeFirmwareFileID(entry.Name())
		if err != nil {
			continue
		}

		dir := getFirmwareDirPath(fileID)
		filePath, err := findSingleFileInDir(dir)
		if err != nil {
			slog.Warn("skipping firmware dir during list", "file_id", fileID, "error", err)
			continue
		}

		fileInfo, err := os.Stat(filePath)
		if err != nil {
			slog.Warn("failed to stat firmware file during list", "file_id", fileID, "error", err)
			continue
		}

		dirInfo, err := os.Stat(dir)
		if err != nil {
			slog.Warn("failed to stat firmware dir during list", "file_id", fileID, "error", err)
			continue
		}

		result = append(result, FirmwareFileInfo{
			ID:         fileID,
			Filename:   filepath.Base(filePath),
			Size:       fileInfo.Size(),
			UploadedAt: dirInfo.ModTime(),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UploadedAt.After(result[j].UploadedAt)
	})

	return result, nil
}

// DeleteAllFirmwareFiles removes all firmware files from disk and the checksum
// index. Best-effort: continues on individual errors and returns the first error
// encountered along with the count of successfully deleted files.
func (s *Service) DeleteAllFirmwareFiles() (int, error) {
	entries, err := os.ReadDir(firmwareDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read firmware dir: %w", err)
	}

	deleted := 0
	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "staging" {
			continue
		}
		fileID, err := canonicalizeFirmwareFileID(entry.Name())
		if err != nil {
			continue
		}

		if err := s.DeleteFirmwareFile(fileID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("failed to delete firmware file during delete-all", "file_id", fileID, "error", err)
			continue
		}
		deleted++
	}

	if deleted > 0 {
		slog.Info("deleted all firmware files", "count", deleted)
	}
	return deleted, firstErr
}

// initChecksumIndex scans the firmware directory on startup and rebuilds the
// in-memory checksum index from any firmware files on disk.
func (s *Service) initChecksumIndex() error {
	entries, err := os.ReadDir(firmwareDir)
	if err != nil {
		return fmt.Errorf("failed to read firmware dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fileID, err := canonicalizeFirmwareFileID(entry.Name())
		if err != nil {
			continue
		}
		dir := getFirmwareDirPath(fileID)
		filePath, err := findSingleFileInDir(dir)
		if err != nil {
			continue
		}
		checksum, err := computeFileChecksum(filePath)
		if err != nil {
			slog.Warn("failed to compute checksum for existing firmware file", "file_id", fileID, "error", err)
			continue
		}

		s.checksumIndex[checksum] = append(s.checksumIndex[checksum], fileID)
	}

	count := 0
	for _, ids := range s.checksumIndex {
		count += len(ids)
	}
	if count > 0 {
		slog.Info("rebuilt firmware checksum index from disk", "files", count)
	}
	return nil
}

func computeFileChecksum(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for checksum: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("failed to compute checksum: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// findSingleFileInDir returns the path to the single non-directory entry inside
// a directory. Returns an error if zero or more than one file exists, so callers
// fail fast on corrupted firmware directories.
func findSingleFileInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read firmware dir %s: %w", dir, err)
	}
	var foundPath string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if foundPath != "" {
			return "", fmt.Errorf("multiple files found in %s", dir)
		}
		foundPath = filepath.Join(dir, e.Name())
	}
	if foundPath == "" {
		return "", fmt.Errorf("no file found in %s", dir)
	}
	return foundPath, nil
}

func hasAllowedFirmwareExtension(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range allowedFirmwareExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func allowedExtensionsList() string {
	sorted := make([]string, len(allowedFirmwareExtensions))
	copy(sorted, allowedFirmwareExtensions)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}

// sanitizeFirmwareFilename strips directory components from the filename,
// keeping only the base name to prevent path traversal.
func sanitizeFirmwareFilename(filename string) string {
	return filepath.Base(filename)
}
