package files

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
)

const logsDir = "logs"
const tempDir = logsDir + string(filepath.Separator) + "tmp"
const grpcSizeLimit = 4 * 1024 * 1024

type FSFile struct {
	Filename string
	Data     []byte
}

// zip containing all the logs for batch
func getBatchLogsZipFilePath(batchLogUUID string) string {
	zipFilename := fmt.Sprintf("logs_batch_%s.zip", batchLogUUID)
	return filepath.Join(tempDir, zipFilename)
}

// dir where all the logs for batch reside
func getBatchLogsDirPath(batchLogUUID string) string {
	return filepath.Join(logsDir, batchLogUUID)
}

type Service struct {
}

func NewService() (*Service, error) {
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create logs dir: %v", err)
	}
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create temp logs dir: %v", err)
	}
	return &Service{}, nil
}

func (s *Service) CreateBatchDirIfNotExists(batchLogUUID string) (string, error) {
	batchDir := getBatchLogsDirPath(batchLogUUID)
	err := os.MkdirAll(batchDir, 0750)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create batch dir: %v", err)
	}

	return batchDir, nil
}

func (s *Service) SaveLogs(batchLogUUID string, deviceIdentifier *miner.DeviceIdentifier, logLines []string) (string, error) {
	batchDir, err := s.CreateBatchDirIfNotExists(batchLogUUID)
	if err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s.csv", deviceIdentifier, timestamp)
	filePath := filepath.Join(batchDir, filename)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create log file: %v", err)
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

func (s *Service) bundleLogsIntoZIP(batchLogUUID string) (string, error) {
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to create temp directory: %v", err)
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

	batchDir := getBatchLogsDirPath(batchLogUUID)
	files, err := os.ReadDir(batchDir)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to read batch directory: %v", err)
	}

	for _, file := range files {
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
	zipPath := getBatchLogsZipFilePath(batchLogUUID)
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		return "", fleeterror.NewInternalErrorf("log bundle is not available yet, please try again later")
	}

	return zipPath, nil
}

func (s *Service) GetBatchLogBundleFile(batchLogUUID string) (*FSFile, error) {
	downloadableFilePath := getBatchLogsZipFilePath(batchLogUUID)
	if _, err := os.Stat(downloadableFilePath); os.IsNotExist(err) {
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

	data, err := io.ReadAll(file)
	if err != nil {
		slog.Error("Error reading all from file", "path", downloadableFilePath, "error", err)
		return nil, fleeterror.NewInternalErrorf("Failed to process request!")
	}

	return &FSFile{Filename: filename, Data: data}, nil
}

func (s *Service) DownloadLogsOnFinishedCallback(batchLogUUID string) func() error {
	return func() error {
		_, err := s.bundleLogsIntoZIP(batchLogUUID)
		if err != nil {
			return fleeterror.NewInternalErrorf("error bundling logs into ZIP: %v", err)
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

	zipPath := getBatchLogsZipFilePath(batchLogUUID)

	if err := os.RemoveAll(batchLogsDir); err != nil {
		return fleeterror.NewInternalErrorf("failed to remove batch directory: %v", err)
	}

	if err := os.Remove(zipPath); err != nil {
		if !os.IsNotExist(err) {
			return fleeterror.NewInternalErrorf("failed to remove batch ZIP file: %v", err)
		}
	}

	return nil
}
