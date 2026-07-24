package files

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/logformat"
)

const logsDir = "logs"
const tempDir = logsDir + string(filepath.Separator) + "tmp"
const grpcSizeLimit = 4 * 1024 * 1024
const unknownMACPlaceholder = "unknown"

// macSafeCharsRe matches characters that are NOT lowercase hex digits.
// Used to whitelist-sanitize MAC addresses for use in filenames.
var macSafeCharsRe = regexp.MustCompile(`[^0-9a-f]`)

var batchLogTimestamp = func() string {
	return time.Now().Format("2006-01-02_15-04-05")
}

// sanitizeMACForFilename strips separators from a MAC address and retains only lowercase
// hex characters. If the result is empty (malformed input), it falls back to a safe placeholder.
func sanitizeMACForFilename(mac string) string {
	normalized := macSafeCharsRe.ReplaceAllString(strings.ToLower(mac), "")
	if normalized == "" {
		return unknownMACPlaceholder
	}
	return normalized
}

type FSFile struct {
	Filename string
	Data     []byte
}

func getBatchLogsZipFilePath(batchLogUUID string) string {
	return filepath.Join(tempDir, fmt.Sprintf("logs_batch_%s.zip", batchLogUUID))
}

func getBatchLogsSingleFilePath(batchLogUUID string) string {
	return filepath.Join(tempDir, fmt.Sprintf("logs_batch_%s.csv", batchLogUUID))
}

// findBatchBundlePath returns the path of the ready bundle (zip or single csv) for a batch.
// Returns "" if neither exists.
func findBatchBundlePath(batchLogUUID string) string {
	zipPath := getBatchLogsZipFilePath(batchLogUUID)
	if _, err := os.Stat(zipPath); err == nil {
		return zipPath
	}
	csvPath := getBatchLogsSingleFilePath(batchLogUUID)
	if _, err := os.Stat(csvPath); err == nil {
		return csvPath
	}
	return ""
}

// dir where all the logs for batch reside
func getBatchLogsDirPath(batchLogUUID string) string {
	return filepath.Join(logsDir, batchLogUUID)
}

type Service struct {
	maxFirmwareFileSize            int64
	maxCommandArtifactSize         int64
	commandArtifactRetentionTTL    time.Duration
	commandArtifactCleanupInterval time.Duration

	mu                   sync.Mutex
	checksumIndex        map[string][]string // SHA-256 hex -> reuse-eligible file IDs
	firmwareChecksumByID map[string]string   // fileID -> SHA-256 hex
}

// MaxFirmwareFileSize returns the configured maximum firmware file size in bytes.
func (s *Service) MaxFirmwareFileSize() int64 {
	if s.maxFirmwareFileSize <= 0 {
		return defaultMaxFirmwareFileSize
	}
	return s.maxFirmwareFileSize
}

// MaxCommandArtifactSize returns the configured maximum command artifact size in bytes.
func (s *Service) MaxCommandArtifactSize() int64 {
	if s.maxCommandArtifactSize <= 0 {
		return defaultMaxCommandArtifactSize
	}
	return s.maxCommandArtifactSize
}

// CommandArtifactRetentionTTL returns how long finalized command artifacts are retained.
func (s *Service) CommandArtifactRetentionTTL() time.Duration {
	if s.commandArtifactRetentionTTL <= 0 {
		return defaultCommandArtifactRetentionTTL
	}
	return s.commandArtifactRetentionTTL
}

// CommandArtifactCleanupInterval returns how often finalized command artifacts are swept.
func (s *Service) CommandArtifactCleanupInterval() time.Duration {
	if s.commandArtifactCleanupInterval <= 0 {
		return defaultCommandArtifactCleanupInterval
	}
	return s.commandArtifactCleanupInterval
}

func NewService(cfg Config) (*Service, error) {
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create logs dir: %v", err)
	}
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create temp logs dir: %v", err)
	}
	if err := initFirmwareDir(); err != nil {
		return nil, err
	}
	if err := initCommandArtifactDir(); err != nil {
		return nil, err
	}

	maxSize := cfg.MaxFirmwareFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFirmwareFileSize
	}
	maxArtifactSize := cfg.MaxCommandArtifactSize
	if maxArtifactSize <= 0 {
		maxArtifactSize = defaultMaxCommandArtifactSize
	}
	retentionTTL := cfg.CommandArtifactRetentionTTL
	if retentionTTL <= 0 {
		retentionTTL = defaultCommandArtifactRetentionTTL
	}
	cleanupInterval := cfg.CommandArtifactCleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = defaultCommandArtifactCleanupInterval
	}

	svc := &Service{
		maxFirmwareFileSize:            maxSize,
		maxCommandArtifactSize:         maxArtifactSize,
		commandArtifactRetentionTTL:    retentionTTL,
		commandArtifactCleanupInterval: cleanupInterval,
		checksumIndex:                  make(map[string][]string),
		firmwareChecksumByID:           make(map[string]string),
	}

	if err := svc.initChecksumIndex(); err != nil {
		slog.Warn("failed to rebuild firmware checksum index from disk", "error", err)
	}

	return svc, nil
}

func (s *Service) CreateBatchDirIfNotExists(batchLogUUID string) (string, error) {
	batchDir := getBatchLogsDirPath(batchLogUUID)
	err := os.MkdirAll(batchDir, 0750)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create batch dir: %v", err)
	}

	return batchDir, nil
}

func (s *Service) batchLogFileName(macAddress string, attempt int) string {
	normalizedMAC := sanitizeMACForFilename(macAddress)
	timestamp := batchLogTimestamp()
	if attempt == 0 {
		return fmt.Sprintf("miner-logs-%s-%s.csv", normalizedMAC, timestamp)
	}
	return fmt.Sprintf("miner-logs-%s-%s-%d.csv", normalizedMAC, timestamp, attempt)
}

func (s *Service) openBatchLogFile(batchLogUUID string, macAddress string) (string, *os.File, error) {
	batchDir, err := s.CreateBatchDirIfNotExists(batchLogUUID)
	if err != nil {
		return "", nil, err
	}

	for attempt := range 1000 {
		filePath := filepath.Join(batchDir, s.batchLogFileName(macAddress, attempt))
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			return filePath, file, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", nil, fleeterror.NewInternalErrorf("failed to create log file: %v", err)
	}
	return "", nil, fleeterror.NewInternalErrorf("failed to create unique log file for batch %s", batchLogUUID)
}

func (s *Service) SaveLogs(batchLogUUID string, macAddress string, logLines []string) (string, error) {
	filePath, file, err := s.openBatchLogFile(batchLogUUID, macAddress)
	if err != nil {
		return "", err
	}
	defer file.Close()

	bufWriter := bufio.NewWriter(file)
	defer func() {
		if err := bufWriter.Flush(); err != nil {
			slog.Error("failed to flush buffer", "error", err)
		}
	}()

	for _, line := range logLines {
		if _, err := fmt.Fprintln(bufWriter, line); err != nil {
			return "", fleeterror.NewInternalErrorf("failed to write log data to file: %v", err)
		}
	}

	if err := bufWriter.Flush(); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to flush log data to file: %v", err)
	}

	return filePath, nil
}

func (s *Service) SaveCommandArtifactLog(batchLogUUID string, macAddress string, artifactID string) (string, error) {
	reader, info, err := s.OpenCommandArtifact(artifactID)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	if info.Size > logformat.MaxArtifactBytes {
		return "", fleeterror.NewFailedPreconditionErrorf("miner log artifact too large: %d bytes (max: %d bytes)", info.Size, logformat.MaxArtifactBytes)
	}

	filePath, file, err := s.openBatchLogFile(batchLogUUID, macAddress)
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if file != nil {
			if closeErr := file.Close(); closeErr != nil {
				slog.Warn("failed to close command artifact log file", "path", filePath, "error", closeErr)
			}
		}
		if !keep {
			if removeErr := os.Remove(filePath); removeErr != nil && !os.IsNotExist(removeErr) {
				slog.Warn("failed to remove partial command artifact log file", "path", filePath, "error", removeErr)
			}
		}
	}()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to read command artifact log: %v", err)
	}
	if int64(len(data)) != info.Size {
		return "", fleeterror.NewFailedPreconditionErrorf("corrupt command artifact %s: metadata size %d does not match copied size %d", info.ID, info.Size, len(data))
	}
	if actualSHA := sha256.Sum256(data); hex.EncodeToString(actualSHA[:]) != info.SHA256 {
		return "", fleeterror.NewFailedPreconditionErrorf("corrupt command artifact %s: sha256 mismatch", info.ID)
	}

	var sanitized bytes.Buffer
	if err := logformat.WriteSanitizedCSV(&sanitized, bytes.NewReader(data)); err != nil {
		return "", fleeterror.NewFailedPreconditionErrorf("failed to sanitize command artifact log csv: %v", err)
	}
	if int64(sanitized.Len()) > logformat.MaxArtifactBytes {
		return "", fleeterror.NewFailedPreconditionErrorf("sanitized miner log artifact too large: %d bytes (max: %d bytes)", sanitized.Len(), logformat.MaxArtifactBytes)
	}
	if _, err := file.Write(sanitized.Bytes()); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to write sanitized command artifact log: %v", err)
	}
	if err := file.Sync(); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to sync command artifact log: %v", err)
	}
	if err := file.Close(); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to close command artifact log: %v", err)
	}
	file = nil
	keep = true

	if err := s.DeleteCommandArtifact(info.ID); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to delete materialized command artifact %s: %v", info.ID, err)
	}

	return filePath, nil
}

func (s *Service) bundleLogs(batchLogUUID string) (string, error) {
	batchDir := getBatchLogsDirPath(batchLogUUID)
	logFiles, err := os.ReadDir(batchDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("no log files to bundle — all devices may have failed", "batch_uuid", batchLogUUID)
			return "", nil
		}
		return "", fleeterror.NewInternalErrorf("failed to read batch directory: %v", err)
	}

	if len(logFiles) == 0 {
		slog.Warn("batch directory exists but contains no log files — all devices may have failed", "batch_uuid", batchLogUUID)
		return "", nil
	}

	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create temp directory: %v", err)
	}

	if len(logFiles) == 1 && !logFiles[0].IsDir() {
		srcPath := filepath.Join(batchDir, logFiles[0].Name())
		destPath := getBatchLogsSingleFilePath(batchLogUUID)
		if err := os.Rename(srcPath, destPath); err != nil {
			return "", fleeterror.NewInternalErrorf("failed to move single log file: %v", err)
		}
		// Store the original per-device filename so it can be used as the download name.
		if writeErr := os.WriteFile(destPath+".name", []byte(logFiles[0].Name()), 0600); writeErr != nil {
			slog.Warn("failed to write filename sidecar", "error", writeErr)
		}
		return destPath, nil
	}

	finalZipPath := getBatchLogsZipFilePath(batchLogUUID)
	tempZipPath := finalZipPath + ".tmp"

	zipFile, err := os.Create(tempZipPath)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create zip file: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for _, file := range logFiles {
		if file.IsDir() {
			return "", fleeterror.NewInternalErrorf("dir found in the logs dir of batchLogUUID: %s", batchLogUUID)
		}

		filePath := filepath.Join(batchDir, file.Name())
		if insideErr := addFileToZIP(zipWriter, filePath); insideErr != nil {
			removalErr := os.Remove(tempZipPath)
			if removalErr != nil {
				return "", fleeterror.NewInternalErrorf("failed to add file to zip: %v and also to remove the temp file: %v", insideErr, removalErr)
			}
			return "", fleeterror.NewInternalErrorf("failed to add file to zip: %v", insideErr)
		}
	}

	err = zipWriter.Close()
	if err != nil {
		return "", fleeterror.NewInternalErrorf("zipWrite close error: %v", err)
	}

	err = zipFile.Close()
	if err != nil {
		return "", fleeterror.NewInternalErrorf("zipFile close error: %v", err)
	}

	if err := os.Rename(tempZipPath, finalZipPath); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to finalize zip file: %v", err)
	}

	zipName := fmt.Sprintf("miner-logs-%s.zip", time.Now().Format("2006-01-02_15-04-05"))
	if writeErr := os.WriteFile(finalZipPath+".name", []byte(zipName), 0600); writeErr != nil {
		slog.Warn("failed to write zip filename sidecar", "error", writeErr)
	}

	return finalZipPath, nil
}

func addFileToZIP(zipWrite *zip.Writer, filename string) error {
	fileToZIP, err := os.Open(filename)
	if err != nil {
		return fleeterror.NewInternalErrorf("error opening file to ZIP: %v", err)
	}
	defer fileToZIP.Close()

	info, err := fileToZIP.Stat()
	if err != nil {
		return fleeterror.NewInternalErrorf("error calling stat on file: %v", err)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting file info header: %v", err)
	}

	header.Name = filepath.Base(filename)

	writer, err := zipWrite.CreateHeader(header)
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating header: %v", err)
	}

	_, err = io.Copy(writer, fileToZIP)
	if err != nil {
		return fleeterror.NewInternalErrorf("error copying file: %v", err)
	}

	return nil
}

func (s *Service) getCommandBatchLogBundle(batchLogUUID string) (string, error) {
	bundlePath := findBatchBundlePath(batchLogUUID)
	if bundlePath == "" {
		return "", fleeterror.NewInternalErrorf("log bundle is not available yet, please try again later")
	}

	return bundlePath, nil
}

func (s *Service) GetBatchLogBundleFile(batchLogUUID string) (*FSFile, error) {
	downloadableFilePath := findBatchBundlePath(batchLogUUID)
	if downloadableFilePath == "" {
		return nil, fleeterror.NewInternalErrorf("log bundle is not available yet, please try again later")
	}

	file, err := os.Open(downloadableFilePath)
	if err != nil {
		slog.Error("Error opening file", "path", downloadableFilePath, "error", err)
		return nil, fleeterror.NewInternalErrorf("Failed to process request!")
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		slog.Error("error getting file stats", "path", downloadableFilePath, "error", err)
		return nil, fleeterror.NewInternalErrorf("Failed to process request!")
	}

	if fileInfo.Size() > grpcSizeLimit {
		slog.Error("File too large for gRPC", "path", downloadableFilePath, "size", fileInfo.Size(), "limit", grpcSizeLimit)

		s.ScheduleBatchLogCleanup(batchLogUUID, 30*time.Second)

		return nil, fleeterror.NewInternalErrorf("Log bundle too large to download!")
	}

	filename := filepath.Base(downloadableFilePath)
	if origName, readErr := os.ReadFile(downloadableFilePath + ".name"); readErr == nil {
		filename = strings.TrimSpace(string(origName))
	}

	data, err := io.ReadAll(file)
	if err != nil {
		slog.Error("Error reading all from file", "path", downloadableFilePath, "error", err)
		return nil, fleeterror.NewInternalErrorf("Failed to process request!")
	}

	return &FSFile{Filename: filename, Data: data}, nil
}

func (s *Service) DownloadLogsOnFinishedCallback(batchLogUUID string) func() error {
	return func() error {
		_, err := s.bundleLogs(batchLogUUID)
		if err != nil {
			return fleeterror.NewInternalErrorf("error bundling logs: %v", err)
		}

		s.ScheduleBatchLogCleanup(batchLogUUID, 24*time.Hour)

		return nil
	}
}

func (s *Service) ScheduleBatchLogCleanup(batchLogUUID string, delay time.Duration) {
	cleanupCtx := context.Background()

	time.AfterFunc(delay, func() {
		_, cancel := context.WithTimeout(cleanupCtx, 1*time.Minute)
		defer cancel()

		if err := s.batchLogCleanup(batchLogUUID); err != nil {
			slog.Error("error cleaning up batch files", "batchLogUUID", batchLogUUID, "error", err)
		}
	})
}

func (s *Service) batchLogCleanup(batchLogUUID string) error {
	batchLogsDir := getBatchLogsDirPath(batchLogUUID)

	if err := os.RemoveAll(batchLogsDir); err != nil {
		return fleeterror.NewInternalErrorf("failed to remove batch directory: %v", err)
	}

	zipPath := getBatchLogsZipFilePath(batchLogUUID)
	singleCSVPath := getBatchLogsSingleFilePath(batchLogUUID)
	for _, bundlePath := range []string{zipPath, zipPath + ".name", singleCSVPath, singleCSVPath + ".name"} {
		if err := os.Remove(bundlePath); err != nil && !os.IsNotExist(err) {
			return fleeterror.NewInternalErrorf("failed to remove bundle file %s: %v", bundlePath, err)
		}
	}

	return nil
}
