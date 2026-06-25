package files

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

const (
	commandArtifactsDir        = "command-artifacts"
	commandArtifactsStagingDir = "command-artifacts/staging"

	defaultMaxCommandArtifactSize int64 = 500 * 1024 * 1024 // 500 MB
)

var sha256HexRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

// CommandArtifactInfo holds metadata about a stored fleet node command artifact.
type CommandArtifactInfo struct {
	ID        string
	Filename  string
	Size      int64
	SHA256    string
	CreatedAt time.Time
}

func initCommandArtifactDir() error {
	if err := os.MkdirAll(commandArtifactsDir, 0750); err != nil {
		return fleeterror.NewInternalErrorf("failed to create command artifacts dir: %v", err)
	}
	if err := os.MkdirAll(commandArtifactsStagingDir, 0750); err != nil {
		return fleeterror.NewInternalErrorf("failed to create command artifacts staging dir: %v", err)
	}
	cleanCommandArtifactStagingDir()
	return nil
}

func cleanCommandArtifactStagingDir() {
	entries, err := os.ReadDir(commandArtifactsStagingDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(commandArtifactsStagingDir, entry.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("failed to remove orphaned command artifact staging file", "path", path, "error", err)
		}
	}
}

func getCommandArtifactDirPath(artifactID string) string {
	return filepath.Join(commandArtifactsDir, artifactID)
}

func canonicalizeCommandArtifactID(artifactID string) (string, error) {
	parsed, err := uuid.Parse(artifactID)
	if err != nil {
		return "", fleeterror.NewInvalidArgumentErrorf("invalid command artifact ID: %s", artifactID)
	}
	return parsed.String(), nil
}

func sanitizeCommandArtifactFilename(filename string) string {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "artifact.bin"
	}
	return name
}

func validateCommandArtifactSHA256(sha256Hex string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(sha256Hex))
	if !sha256HexRe.MatchString(normalized) {
		return "", fleeterror.NewInvalidArgumentError("command artifact sha256 must be 64 lowercase hexadecimal characters")
	}
	return normalized, nil
}

// SaveCommandArtifact streams a command artifact to disk, validating declared size
// and SHA-256 as it writes. The artifact ID is generated server-side.
func (s *Service) SaveCommandArtifact(filename string, sizeBytes int64, sha256Hex string, reader io.Reader) (*CommandArtifactInfo, error) {
	if filename == "" {
		return nil, fleeterror.NewInvalidArgumentError("command artifact filename is required")
	}
	if sizeBytes <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("command artifact size must be greater than zero")
	}
	maxSize := s.MaxCommandArtifactSize()
	if sizeBytes > maxSize {
		return nil, fleeterror.NewPlainError(fmt.Sprintf("command artifact too large: %d bytes (max: %d bytes)", sizeBytes, maxSize), connect.CodeResourceExhausted)
	}
	expectedSHA, err := validateCommandArtifactSHA256(sha256Hex)
	if err != nil {
		return nil, err
	}

	artifactID := id.GenerateID()
	dir := getCommandArtifactDirPath(artifactID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create command artifact dir: %v", err)
	}

	sanitized := sanitizeCommandArtifactFilename(filename)
	filePath := filepath.Join(dir, sanitized)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fleeterror.NewInternalErrorf("failed to create command artifact: %v", err)
	}
	defer file.Close()

	hasher := sha256.New()
	limitedReader := io.LimitReader(reader, maxSize+1)
	written, err := io.Copy(file, io.TeeReader(limitedReader, hasher))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fleeterror.NewInternalErrorf("failed to write command artifact: %v", err)
	}
	if written > maxSize {
		_ = os.RemoveAll(dir)
		return nil, fleeterror.NewPlainError(fmt.Sprintf("command artifact too large: exceeded %d byte limit during upload", maxSize), connect.CodeResourceExhausted)
	}
	if written != sizeBytes {
		_ = os.RemoveAll(dir)
		return nil, fleeterror.NewInvalidArgumentErrorf("command artifact size mismatch: declared %d bytes, received %d bytes", sizeBytes, written)
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA != expectedSHA {
		_ = os.RemoveAll(dir)
		return nil, fleeterror.NewInvalidArgumentError("command artifact sha256 mismatch")
	}
	if err := file.Sync(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fleeterror.NewInternalErrorf("failed to sync command artifact: %v", err)
	}

	return &CommandArtifactInfo{
		ID:        artifactID,
		Filename:  sanitized,
		Size:      written,
		SHA256:    actualSHA,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// OpenCommandArtifact opens a stored command artifact and returns its metadata.
func (s *Service) OpenCommandArtifact(artifactID string) (io.ReadCloser, CommandArtifactInfo, error) {
	canonical, err := canonicalizeCommandArtifactID(artifactID)
	if err != nil {
		return nil, CommandArtifactInfo{}, err
	}
	dir := getCommandArtifactDirPath(canonical)
	filePath, err := findSingleFileInDir(dir)
	if err != nil {
		return nil, CommandArtifactInfo{}, fleeterror.NewNotFoundErrorf("command artifact not found: %s", canonical)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, CommandArtifactInfo{}, fleeterror.NewInternalErrorf("failed to open command artifact: %v", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, CommandArtifactInfo{}, fleeterror.NewInternalErrorf("failed to stat command artifact: %v", err)
	}
	sha, err := computeFileChecksum(filePath)
	if err != nil {
		file.Close()
		return nil, CommandArtifactInfo{}, fleeterror.NewInternalErrorf("failed to compute command artifact checksum: %v", err)
	}
	return file, CommandArtifactInfo{
		ID:        canonical,
		Filename:  filepath.Base(filePath),
		Size:      info.Size(),
		SHA256:    sha,
		CreatedAt: info.ModTime(),
	}, nil
}
