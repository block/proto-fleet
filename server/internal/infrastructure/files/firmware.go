package files

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
)

// FirmwareFileInfo holds metadata about a stored firmware file.
type FirmwareFileInfo struct {
	ID                 string    `json:"id"`
	Filename           string    `json:"filename"`
	Size               int64     `json:"size"`
	SHA256             string    `json:"sha256,omitempty"`
	FilePath           string    `json:"-"`
	UploadedAt         time.Time `json:"uploaded_at"`
	TargetManufacturer string    `json:"target_manufacturer"`
	TargetModel        string    `json:"target_model"`
	FirmwareVersion    string    `json:"firmware_version,omitempty"`
}

// FirmwareMetadata describes the single miner type a firmware file targets.
type FirmwareMetadata struct {
	TargetManufacturer string `json:"target_manufacturer"`
	TargetModel        string `json:"target_model"`
	FirmwareVersion    string `json:"firmware_version,omitempty"`
}

// FirmwareUploadSaveResult describes the stored or reused firmware file after
// upload metadata has been resolved.
type FirmwareUploadSaveResult struct {
	FirmwareFileID string
	Reused         bool
	Metadata       FirmwareMetadata
}

const firmwareDir = "firmware"
const firmwareStagingDir = "firmware/staging"
const firmwareMetadataFilename = "metadata.json"

var errFirmwareMetadataNotFound = errors.New("firmware metadata not found")

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

func (m FirmwareMetadata) normalized() FirmwareMetadata {
	return FirmwareMetadata{
		TargetManufacturer: strings.TrimSpace(m.TargetManufacturer),
		TargetModel:        strings.TrimSpace(m.TargetModel),
		FirmwareVersion:    strings.TrimSpace(m.FirmwareVersion),
	}
}

func (m FirmwareMetadata) matches(other FirmwareMetadata) bool {
	m = m.normalized()
	other = other.normalized()
	return m.TargetManufacturer == other.TargetManufacturer &&
		m.TargetModel == other.TargetModel &&
		m.FirmwareVersion == other.FirmwareVersion
}

// MatchesTarget reports whether the firmware applies to a device with the
// given manufacturer and model. Unlike matches (exact comparison, used for
// upload dedup), deployment compatibility is case-insensitive, and a device
// with unknown manufacturer or model never matches.
func (m FirmwareMetadata) MatchesTarget(manufacturer, model string) bool {
	m = m.normalized()
	manufacturer = strings.TrimSpace(manufacturer)
	model = strings.TrimSpace(model)
	return manufacturer != "" && model != "" &&
		strings.EqualFold(manufacturer, m.TargetManufacturer) &&
		strings.EqualFold(model, m.TargetModel)
}

// ValidateFirmwareMetadata checks that target metadata is complete.
func ValidateFirmwareMetadata(metadata FirmwareMetadata) error {
	metadata = metadata.normalized()
	if metadata.TargetManufacturer == "" {
		return fleeterror.NewInvalidArgumentError("target_manufacturer is required")
	}
	if metadata.TargetModel == "" {
		return fleeterror.NewInvalidArgumentError("target_model is required")
	}
	return nil
}

// ValidateFirmwareUploadMetadata checks metadata required for new uploads and
// checksum reuse. Existing files may predate firmware_version metadata.
func ValidateFirmwareUploadMetadata(metadata FirmwareMetadata) error {
	metadata = metadata.normalized()
	if err := ValidateFirmwareMetadata(metadata); err != nil {
		return err
	}
	if metadata.FirmwareVersion == "" {
		return fleeterror.NewInvalidArgumentError("firmware_version is required")
	}
	return nil
}

// canonicalizeFirmwareFileID validates and normalizes a firmware file ID.
// uuid.Parse accepts multiple textual forms (uppercase, urn:uuid:, braced),
// so we normalize to the lowercase hyphenated form to ensure consistent
// on-disk paths.
func canonicalizeFirmwareFileID(fileID string) (string, error) {
	canonical, err := canonicalizeStorageUUID("firmware file", fileID)
	if err != nil {
		return "", fleeterror.NewInvalidArgumentError(err.Error())
	}
	return canonical, nil
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

// cleanStagingDir removes leftover temp files and publish dirs from previous
// runs. Since upload sessions are in-memory only, anything in the staging
// directory at startup is an orphan from an interrupted upload.
func cleanStagingDir() {
	cleanStorageStagingDir(firmwareStagingDir,
		"failed to remove orphaned staging entry",
		"removed orphaned staging entry")
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
func (s *Service) SaveFirmwareFile(filename string, reader io.Reader, metadata FirmwareMetadata) (string, error) {
	result, err := s.SaveFirmwareUpload(filename, reader, metadata, true)
	if err != nil {
		return "", err
	}
	return result.FirmwareFileID, nil
}

// SaveFirmwareUpload stages a streamed upload and reuses an existing file with
// the same checksum plus metadata unless force is true.
func (s *Service) SaveFirmwareUpload(filename string, reader io.Reader, manualMetadata FirmwareMetadata, force bool) (FirmwareUploadSaveResult, error) {
	var result FirmwareUploadSaveResult
	metadata := manualMetadata.normalized()
	if err := ValidateFirmwareUploadMetadata(metadata); err != nil {
		return result, err
	}

	tempFile, err := os.CreateTemp(firmwareStagingDir, "direct-*")
	if err != nil {
		return result, fleeterror.NewInternalErrorf("failed to create firmware staging file: %v", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	maxSize := s.maxFirmwareFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFirmwareFileSize
	}
	limitedReader := io.LimitReader(reader, maxSize+1)
	hasher := sha256.New()
	written, err := io.Copy(tempFile, io.TeeReader(limitedReader, hasher))
	if err != nil {
		return result, fleeterror.NewInternalErrorf("failed to write firmware staging file: %v", err)
	}
	if written > maxSize {
		return result, fleeterror.NewInvalidArgumentErrorf("firmware file too large: exceeded %d byte limit during upload", maxSize)
	}
	if written == 0 {
		return result, fleeterror.NewInvalidArgumentError("firmware file is empty")
	}
	// No fsync here: the staging file is throwaway, and the publish path syncs
	// the payload before the rename that makes it visible.
	if err := tempFile.Close(); err != nil {
		return result, fleeterror.NewInternalErrorf("failed to close firmware staging file: %v", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	result, err = s.SaveFirmwareUploadFromPath(filename, tempPath, metadata, force, checksum)
	if err != nil {
		return FirmwareUploadSaveResult{}, err
	}
	removeTemp = false
	return result, nil
}

// SaveFirmwareUploadFromPath consumes a staged upload path. On success it either
// removes the staged file and returns an existing matching firmware_file_id, or
// moves the staged file into permanent firmware storage.
func (s *Service) SaveFirmwareUploadFromPath(filename string, srcPath string, manualMetadata FirmwareMetadata, force bool, checksum string) (FirmwareUploadSaveResult, error) {
	var result FirmwareUploadSaveResult

	metadata := manualMetadata.normalized()
	if err := ValidateFirmwareUploadMetadata(metadata); err != nil {
		return result, err
	}

	if checksum == "" {
		var err error
		checksum, err = computeFileChecksum(srcPath)
		if err != nil {
			return result, fleeterror.NewInternalErrorf("failed to compute firmware checksum: %v", err)
		}
	}

	if !force {
		if existingID, ok := s.FindFirmwareFileByChecksum(checksum, metadata); ok {
			if err := os.Remove(srcPath); err != nil && !os.IsNotExist(err) {
				return result, fleeterror.NewInternalErrorf("failed to remove reused firmware staging file: %v", err)
			}
			return FirmwareUploadSaveResult{
				FirmwareFileID: existingID,
				Reused:         true,
				Metadata:       metadata,
			}, nil
		}
	}

	fileID, err := s.saveFirmwareFileFromPathWithChecksum(filename, srcPath, metadata, checksum)
	if err != nil {
		return result, err
	}
	return FirmwareUploadSaveResult{
		FirmwareFileID: fileID,
		Metadata:       metadata,
	}, nil
}

func (s *Service) saveFirmwareFileFromPathWithChecksum(filename string, srcPath string, metadata FirmwareMetadata, checksum string) (string, error) {
	fileID := id.GenerateID()
	finalDir := getFirmwareDirPath(fileID)
	stagingDir, err := os.MkdirTemp(firmwareStagingDir, "publish-*")
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create firmware publish staging dir: %v", err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	sanitized := sanitizeFirmwareFilename(filename)
	destPath := filepath.Join(stagingDir, sanitized)

	if err := os.Rename(srcPath, destPath); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to move firmware file: %v", err)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to stat firmware file: %v", err)
	}
	if info.Size() == 0 {
		return "", fleeterror.NewInvalidArgumentError("firmware file is empty")
	}

	payload, err := os.OpenFile(destPath, os.O_RDWR, 0)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to open staged firmware file for sync: %v", err)
	}
	if err := payload.Sync(); err != nil {
		_ = payload.Close()
		return "", fleeterror.NewInternalErrorf("failed to sync staged firmware file: %v", err)
	}
	if err := payload.Close(); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to close staged firmware file: %v", err)
	}

	if err := writeFirmwareMetadata(stagingDir, metadata); err != nil {
		return "", err
	}
	if err := syncFirmwareDirectory(stagingDir); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to sync firmware staging directory: %v", err)
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to publish firmware directory: %v", err)
	}
	removeStaging = false
	if err := syncFirmwareDirectory(firmwareDir); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to sync firmware directory: %v", err)
	}

	s.rememberFirmwareChecksum(checksum, fileID)

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
	return getFirmwareFilePathForCanonicalID(canonical)
}

// GetFirmwareMetadata returns the target metadata for a stored firmware file.
func (s *Service) GetFirmwareMetadata(fileID string) (FirmwareMetadata, error) {
	canonical, err := canonicalizeFirmwareFileID(fileID)
	if err != nil {
		return FirmwareMetadata{}, err
	}
	dir := getFirmwareDirPath(canonical)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return FirmwareMetadata{}, fleeterror.NewNotFoundErrorf("firmware file not found: %s", canonical)
		}
		return FirmwareMetadata{}, fleeterror.NewInternalErrorf("failed to stat firmware dir %s: %v", canonical, err)
	}
	metadata, err := readFirmwareMetadata(dir)
	if err != nil {
		if errors.Is(err, errFirmwareMetadataNotFound) {
			return FirmwareMetadata{}, nil
		}
		return FirmwareMetadata{}, fleeterror.NewInternalErrorf("failed to read firmware metadata: %v", err)
	}
	return metadata, nil
}

// UpdateFirmwareMetadata atomically replaces the target metadata for a stored
// firmware file and refreshes its checksum reuse entry. This also allows a
// legacy payload without a sidecar to become eligible for new deployments once
// complete metadata has been supplied.
func (s *Service) UpdateFirmwareMetadata(fileID string, metadata FirmwareMetadata) error {
	canonical, err := canonicalizeFirmwareFileID(fileID)
	if err != nil {
		return err
	}
	metadata = metadata.normalized()
	if err := ValidateFirmwareUploadMetadata(metadata); err != nil {
		return err
	}

	dir := getFirmwareDirPath(canonical)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fleeterror.NewNotFoundErrorf("firmware file not found: %s", canonical)
		}
		return fleeterror.NewInternalErrorf("failed to stat firmware dir %s: %v", canonical, err)
	}
	filePath, err := findSingleFileInDir(dir, firmwareMetadataFilename)
	if err != nil {
		return fleeterror.NewNotFoundErrorf("firmware file not found: %s", canonical)
	}
	// The payload is immutable after publish, so a cached checksum is trusted;
	// only legacy files that were never indexed need a fresh hash.
	checksum, ok := s.lookupFirmwareChecksum(canonical)
	if !ok {
		checksum, err = computeFileChecksum(filePath)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to compute firmware checksum: %v", err)
		}
	}

	if err := writeFirmwareMetadata(dir, metadata); err != nil {
		return err
	}
	s.rememberFirmwareChecksum(checksum, canonical)
	if err := syncFirmwareDirectory(dir); err != nil {
		return fleeterror.NewInternalErrorf("failed to sync firmware directory: %v", err)
	}
	return nil
}

func getFirmwareFilePathForCanonicalID(canonical string) (string, error) {
	dir := getFirmwareDirPath(canonical)
	path, err := findSingleFileInDir(dir, firmwareMetadataFilename)
	if err != nil {
		return "", fleeterror.NewNotFoundErrorf("firmware file not found: %s", canonical)
	}
	return path, nil
}

// OpenFirmwareFile opens the firmware file for reading and returns the reader,
// original filename, and file size. The caller is responsible for closing the reader.
func (s *Service) OpenFirmwareFile(fileID string) (io.ReadCloser, string, int64, error) {
	reader, info, err := s.OpenFirmwareFileWithInfo(fileID)
	if err != nil {
		return nil, "", 0, err
	}
	return reader, info.Filename, info.Size, nil
}

// OpenFirmwareFileWithInfo opens the firmware file for reading and returns
// metadata required to address it as a command artifact payload.
func (s *Service) OpenFirmwareFileWithInfo(fileID string) (io.ReadCloser, FirmwareFileInfo, error) {
	canonical, err := canonicalizeFirmwareFileID(fileID)
	if err != nil {
		return nil, FirmwareFileInfo{}, err
	}
	filePath, err := getFirmwareFilePathForCanonicalID(canonical)
	if err != nil {
		return nil, FirmwareFileInfo{}, err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, FirmwareFileInfo{}, fleeterror.NewInternalErrorf("failed to open firmware file: %v", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, FirmwareFileInfo{}, fleeterror.NewInternalErrorf("failed to stat firmware file: %v", err)
	}

	metadata, hasMetadata := FirmwareMetadata{}, true
	if m, err := readFirmwareMetadata(getFirmwareDirPath(canonical)); err != nil {
		if !errors.Is(err, errFirmwareMetadataNotFound) {
			file.Close()
			return nil, FirmwareFileInfo{}, fleeterror.NewInternalErrorf("failed to read firmware metadata: %v", err)
		}
		hasMetadata = false
	} else {
		metadata = m
	}

	checksum, err := s.firmwareChecksum(canonical, filePath, hasMetadata)
	if err != nil {
		file.Close()
		return nil, FirmwareFileInfo{}, err
	}

	return file, FirmwareFileInfo{
		ID:                 canonical,
		Filename:           filepath.Base(filePath),
		Size:               info.Size(),
		SHA256:             checksum,
		FilePath:           filePath,
		TargetManufacturer: metadata.TargetManufacturer,
		TargetModel:        metadata.TargetModel,
		FirmwareVersion:    metadata.FirmwareVersion,
	}, nil
}

// firmwareChecksum returns the file's cached checksum, computing it on a miss.
// hasValidMetadata decides how a computed checksum is cached: only files with a
// valid metadata sidecar become eligible for checksum-reuse lookups.
func (s *Service) firmwareChecksum(canonicalID, filePath string, hasValidMetadata bool) (string, error) {
	if checksum, ok := s.lookupFirmwareChecksum(canonicalID); ok {
		return checksum, nil
	}
	checksum, err := computeFileChecksum(filePath)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to compute firmware checksum: %v", err)
	}
	if hasValidMetadata {
		s.rememberFirmwareChecksum(checksum, canonicalID)
	} else {
		s.rememberFirmwareChecksumByID(checksum, canonicalID)
	}
	return checksum, nil
}

func (s *Service) lookupFirmwareChecksum(canonicalID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checksum, ok := s.firmwareChecksumByID[canonicalID]
	return checksum, ok
}

// rememberFirmwareChecksum caches a file's checksum and makes it eligible for
// checksum reuse. Files without valid metadata must use
// rememberFirmwareChecksumByID instead so they never satisfy a reuse lookup.
func (s *Service) rememberFirmwareChecksum(checksum, canonicalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firmwareChecksumByID[canonicalID] = checksum
	for _, id := range s.checksumIndex[checksum] {
		if id == canonicalID {
			return
		}
	}
	s.checksumIndex[checksum] = append(s.checksumIndex[checksum], canonicalID)
}

func (s *Service) rememberFirmwareChecksumByID(checksum, canonicalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firmwareChecksumByID[canonicalID] = checksum
}

// FindFirmwareFileByChecksum looks up a firmware file by its SHA-256 hex digest.
// Returns the file ID and true if found, or empty string and false otherwise.
// Used by the pre-upload check endpoint to let clients skip redundant uploads.
func (s *Service) FindFirmwareFileByChecksum(sha256Hex string, metadata FirmwareMetadata) (string, bool) {
	s.mu.Lock()
	fileIDs := append([]string(nil), s.checksumIndex[sha256Hex]...)
	s.mu.Unlock()

	metadata = metadata.normalized()
	for _, fileID := range fileIDs {
		storedMetadata, err := readFirmwareMetadata(getFirmwareDirPath(fileID))
		if err != nil {
			if errors.Is(err, errFirmwareMetadataNotFound) {
				continue
			}
			slog.Warn("skipping firmware with invalid metadata during checksum lookup", "file_id", fileID, "error", err)
			s.mu.Lock()
			s.removeFirmwareChecksumLocked(sha256Hex, fileID)
			s.mu.Unlock()
			continue
		}
		if storedMetadata.matches(metadata) {
			return fileID, true
		}
	}
	return "", false
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

	checksum, ok := s.firmwareChecksumByID[canonical]
	if ok {
		s.removeFirmwareChecksumLocked(checksum, canonical)
	} else {
		s.removeFirmwareChecksumByScanLocked(canonical)
	}

	slog.Info("firmware file deleted", "file_id", canonical)
	return nil
}

func (s *Service) removeFirmwareChecksumLocked(checksum, canonicalID string) {
	delete(s.firmwareChecksumByID, canonicalID)
	ids := s.checksumIndex[checksum]
	for i, id := range ids {
		if id != canonicalID {
			continue
		}
		ids = append(ids[:i], ids[i+1:]...)
		if len(ids) == 0 {
			delete(s.checksumIndex, checksum)
		} else {
			s.checksumIndex[checksum] = ids
		}
		return
	}
}

func (s *Service) removeFirmwareChecksumByScanLocked(canonicalID string) {
	for checksum, ids := range s.checksumIndex {
		for i, id := range ids {
			if id != canonicalID {
				continue
			}
			ids = append(ids[:i], ids[i+1:]...)
			if len(ids) == 0 {
				delete(s.checksumIndex, checksum)
			} else {
				s.checksumIndex[checksum] = ids
			}
			return
		}
	}
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
		metadata, err := readFirmwareMetadata(dir)
		if err != nil {
			if errors.Is(err, errFirmwareMetadataNotFound) {
				metadata = FirmwareMetadata{}
			} else {
				slog.Warn("skipping firmware with invalid metadata during list", "file_id", fileID, "error", err)
				continue
			}
		}

		filePath, err := findSingleFileInDir(dir, firmwareMetadataFilename)
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
			ID:                 fileID,
			Filename:           filepath.Base(filePath),
			Size:               fileInfo.Size(),
			UploadedAt:         dirInfo.ModTime(),
			TargetManufacturer: metadata.TargetManufacturer,
			TargetModel:        metadata.TargetModel,
			FirmwareVersion:    metadata.FirmwareVersion,
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
		if _, err := readFirmwareMetadata(dir); err != nil {
			if !errors.Is(err, errFirmwareMetadataNotFound) {
				slog.Warn("skipping firmware with invalid metadata during checksum rebuild", "file_id", fileID, "error", err)
			}
			continue
		}
		filePath, err := findSingleFileInDir(dir, firmwareMetadataFilename)
		if err != nil {
			continue
		}
		checksum, err := computeFileChecksum(filePath)
		if err != nil {
			slog.Warn("failed to compute checksum for existing firmware file", "file_id", fileID, "error", err)
			continue
		}

		s.rememberFirmwareChecksum(checksum, fileID)
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

// writeFirmwareMetadata atomically publishes the metadata sidecar via a
// temp-file-and-rename, so readers never observe a partially written sidecar.
func writeFirmwareMetadata(dir string, metadata FirmwareMetadata) error {
	tempFile, err := os.CreateTemp(dir, ".metadata-*.tmp")
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create firmware metadata staging file: %v", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0600); err != nil {
		return fleeterror.NewInternalErrorf("failed to set firmware metadata permissions: %v", err)
	}
	if err := json.NewEncoder(tempFile).Encode(metadata.normalized()); err != nil {
		return fleeterror.NewInternalErrorf("failed to write firmware metadata: %v", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fleeterror.NewInternalErrorf("failed to sync firmware metadata to disk: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		return fleeterror.NewInternalErrorf("failed to close firmware metadata staging file: %v", err)
	}
	if err := os.Rename(tempPath, filepath.Join(dir, firmwareMetadataFilename)); err != nil {
		return fleeterror.NewInternalErrorf("failed to publish firmware metadata: %v", err)
	}
	removeTemp = false
	return nil
}

func readFirmwareMetadata(dir string) (FirmwareMetadata, error) {
	file, err := os.Open(filepath.Join(dir, firmwareMetadataFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return FirmwareMetadata{}, fmt.Errorf("%w: %s", errFirmwareMetadataNotFound, dir)
		}
		return FirmwareMetadata{}, fmt.Errorf("open firmware metadata: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var metadata FirmwareMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return FirmwareMetadata{}, fmt.Errorf("decode firmware metadata: %w", err)
	}
	metadata = metadata.normalized()
	if err := ValidateFirmwareMetadata(metadata); err != nil {
		return FirmwareMetadata{}, err
	}
	return metadata, nil
}

func syncFirmwareDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open firmware directory for sync: %w", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync firmware directory: %w", err)
	}
	return nil
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
