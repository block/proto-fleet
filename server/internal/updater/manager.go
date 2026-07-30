package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

const (
	stateFilename      = "state.json"
	maxChecksumBytes   = 4096
	maxDownloadBytes   = int64(8 << 30) // 8 GiB hard stop for a corrupt or hostile response.
	maxExtractedBytes  = int64(16 << 30)
	maxArchiveEntries  = 100_000
	defaultHTTPTimeout = 30 * time.Minute
)

var canonicalRelease = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-rc\.\d+)?$`)

type CommandRunner interface {
	Run(ctx context.Context, dir string, output io.Writer, name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, output io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.Env = append(os.Environ(), "CI=1", "TERM=dumb")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

type Config struct {
	InstallRoot     string
	StateDir        string
	DownloadBaseURL string
	HTTPClient      *http.Client
	Runner          CommandRunner
	Now             func() time.Time
	NewID           func() string
	GOARCH          string
	SelfUpdatePath  string
}

type Manager struct {
	cfg Config

	mu        sync.RWMutex
	operation *updaterapi.Operation
}

func NewManager(cfg Config) (*Manager, error) {
	if !filepath.IsAbs(cfg.InstallRoot) {
		return nil, fmt.Errorf("install root must be absolute")
	}
	if !filepath.IsAbs(cfg.StateDir) {
		return nil, fmt.Errorf("state directory must be absolute")
	}
	if cfg.SelfUpdatePath != "" && !filepath.IsAbs(cfg.SelfUpdatePath) {
		return nil, fmt.Errorf("self-update path must be absolute")
	}
	downloadBase, err := url.Parse(cfg.DownloadBaseURL)
	if err != nil || downloadBase.Scheme != "https" || downloadBase.Host == "" ||
		downloadBase.User != nil || downloadBase.RawQuery != "" || downloadBase.Fragment != "" {
		return nil, fmt.Errorf("download base URL must use https")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			Timeout: defaultHTTPTimeout,
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				if req.URL.Scheme != "https" {
					return fmt.Errorf("refusing non-HTTPS release redirect")
				}
				return nil
			},
		}
	}
	if cfg.Runner == nil {
		cfg.Runner = execRunner{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = uuid.NewString
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.GOARCH != "amd64" && cfg.GOARCH != "arm64" {
		return nil, fmt.Errorf("unsupported architecture %q", cfg.GOARCH)
	}
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, "logs"), 0o700); err != nil {
		return nil, fmt.Errorf("create updater state directory: %w", err)
	}

	m := &Manager{cfg: cfg}
	if err := m.loadState(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Status() updaterapi.StatusResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.operation == nil {
		return updaterapi.StatusResponse{}
	}
	snapshot := *m.operation
	return updaterapi.StatusResponse{Operation: &snapshot}
}

func (m *Manager) Trigger(targetVersion string) (updaterapi.Operation, error) {
	if !canonicalRelease.MatchString(targetVersion) || !semver.IsValid(targetVersion) {
		return updaterapi.Operation{}, fmt.Errorf("target version must be a stable or RC release tag")
	}
	currentVersion, err := readInstalledVersion(filepath.Join(m.cfg.InstallRoot, "deployment", "version.txt"))
	if err != nil {
		return updaterapi.Operation{}, err
	}
	if !semver.IsValid(currentVersion) {
		return updaterapi.Operation{}, fmt.Errorf("installed version %q is not upgradeable", currentVersion)
	}
	if semver.Compare(targetVersion, currentVersion) <= 0 {
		return updaterapi.Operation{}, fmt.Errorf("target version %s must be newer than installed version %s", targetVersion, currentVersion)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation != nil && !m.operation.Phase.Terminal() {
		return updaterapi.Operation{}, fmt.Errorf("an upgrade to %s is already in progress", m.operation.TargetVersion)
	}
	now := m.cfg.Now().UTC()
	op := &updaterapi.Operation{
		ID:            m.cfg.NewID(),
		TargetVersion: targetVersion,
		Phase:         updaterapi.PhaseQueued,
		Message:       "Upgrade queued",
		StartedAt:     now,
		UpdatedAt:     now,
	}
	m.operation = op
	if err := m.persistLocked(); err != nil {
		m.operation = nil
		return updaterapi.Operation{}, err
	}
	operationCopy := *op
	go m.run(operationCopy.ID, targetVersion)
	return operationCopy, nil
}

func (m *Manager) run(operationID, targetVersion string) {
	logPath := filepath.Join(m.cfg.StateDir, "logs", operationID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		m.fail(operationID, fmt.Errorf("open upgrade log: %w", err), "")
		return
	}
	defer logFile.Close()

	ctx := context.Background()
	recovery := fmt.Sprintf("cd %s && ./run-fleet.sh --non-interactive --skip-build", shellQuote(filepath.Join(m.cfg.InstallRoot, "deployment")))
	if err := m.setLogPath(operationID, logPath, recovery); err != nil {
		m.fail(operationID, fmt.Errorf("persist upgrade log location: %w", err), recovery)
		return
	}
	_, _ = fmt.Fprintf(logFile, "[%s] starting upgrade to %s\n", m.cfg.Now().UTC().Format(time.RFC3339), targetVersion)

	archiveName := fmt.Sprintf("proto-fleet-%s-%s.tar.gz", targetVersion, m.cfg.GOARCH)
	archiveURL := strings.TrimSuffix(m.cfg.DownloadBaseURL, "/") + "/" + targetVersion + "/" + archiveName
	archivePath := filepath.Join(m.cfg.StateDir, operationID+"-"+archiveName)
	checksumPath := archivePath + ".sha256"
	defer os.Remove(archivePath)
	defer os.Remove(checksumPath)

	if err := m.advance(operationID, updaterapi.PhaseDownloading, "Downloading release bundle"); err != nil {
		m.fail(operationID, fmt.Errorf("persist download phase: %w", err), recovery)
		return
	}
	if err := m.download(ctx, archiveURL, archivePath, maxDownloadBytes); err != nil {
		m.fail(operationID, fmt.Errorf("download release bundle: %w", err), recovery)
		return
	}
	if err := m.download(ctx, archiveURL+".sha256", checksumPath, maxChecksumBytes); err != nil {
		m.fail(operationID, fmt.Errorf("download release checksum: %w", err), recovery)
		return
	}

	if err := m.advance(operationID, updaterapi.PhaseVerifying, "Verifying release integrity"); err != nil {
		m.fail(operationID, fmt.Errorf("persist verification phase: %w", err), recovery)
		return
	}
	if err := verifyChecksum(archivePath, checksumPath, archiveName); err != nil {
		m.fail(operationID, err, recovery)
		return
	}

	stageRoot, err := os.MkdirTemp(m.cfg.InstallRoot, ".proto-fleet-upgrade-")
	if err != nil {
		m.fail(operationID, fmt.Errorf("create staging directory: %w", err), recovery)
		return
	}
	defer os.RemoveAll(stageRoot)

	if err := m.advance(operationID, updaterapi.PhaseStaging, "Staging release and preserving configuration"); err != nil {
		m.fail(operationID, fmt.Errorf("persist staging phase: %w", err), recovery)
		return
	}
	if err := extractArchive(archivePath, stageRoot); err != nil {
		m.fail(operationID, fmt.Errorf("extract release bundle: %w", err), recovery)
		return
	}
	stageDeployment := filepath.Join(stageRoot, "deployment")
	currentDeployment := filepath.Join(m.cfg.InstallRoot, "deployment")
	if err := validateStagedRelease(stageDeployment, targetVersion); err != nil {
		m.fail(operationID, err, recovery)
		return
	}
	if err := preserveDeploymentState(currentDeployment, stageDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("preserve deployment configuration: %w", err), recovery)
		return
	}
	if err := preserveDeploymentOwnership(currentDeployment, stageDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("preserve deployment ownership: %w", err), recovery)
		return
	}

	if err := m.advance(operationID, updaterapi.PhasePreflight, "Building and validating the new stack while Fleet stays online"); err != nil {
		m.fail(operationID, fmt.Errorf("persist preflight phase: %w", err), recovery)
		return
	}
	if err := m.cfg.Runner.Run(ctx, stageDeployment, logFile, "/bin/bash", "./run-fleet.sh", "--non-interactive", "--preflight-only"); err != nil {
		m.fail(operationID, fmt.Errorf("upgrade preflight failed: %w", err), recovery)
		return
	}
	if m.cfg.SelfUpdatePath != "" {
		stagedUpdater := filepath.Join(stageDeployment, "updater", "proto-fleet-updater")
		if err := atomicReplaceExecutable(stagedUpdater, m.cfg.SelfUpdatePath); err != nil {
			m.fail(operationID, fmt.Errorf("update host updater binary: %w", err), recovery)
			return
		}
	}

	if err := m.advance(operationID, updaterapi.PhaseActivating, "Restarting Fleet; the client may disconnect for several minutes"); err != nil {
		m.fail(operationID, fmt.Errorf("persist activation phase: %w", err), recovery)
		return
	}
	backupDeployment := filepath.Join(m.cfg.InstallRoot, "deployment.previous")
	if err := os.RemoveAll(backupDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("remove previous deployment backup: %w", err), recovery)
		return
	}
	if err := os.Rename(currentDeployment, backupDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("back up current deployment: %w", err), recovery)
		return
	}
	if err := os.Rename(stageDeployment, currentDeployment); err != nil {
		// This is still pre-teardown and therefore safe to restore.
		_ = os.Rename(backupDeployment, currentDeployment)
		m.fail(operationID, fmt.Errorf("activate staged deployment: %w", err), recovery)
		return
	}

	if err := m.cfg.Runner.Run(ctx, currentDeployment, logFile, "/bin/bash", "./run-fleet.sh", "--non-interactive", "--skip-build"); err != nil {
		m.fail(operationID, fmt.Errorf("new stack failed to start: %w", err), recovery)
		return
	}

	if err := m.succeed(operationID, fmt.Sprintf("Fleet %s is running", targetVersion)); err != nil {
		_, _ = fmt.Fprintf(logFile, "[%s] warning: persist successful terminal state: %v\n", m.cfg.Now().UTC().Format(time.RFC3339), err)
		log.Printf("persist successful upgrade state: %v", err)
	}
	_, _ = fmt.Fprintf(logFile, "[%s] upgrade completed\n", m.cfg.Now().UTC().Format(time.RFC3339))
}

func (m *Manager) download(ctx context.Context, rawURL, path string, maxBytes int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := m.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s", rawURL, resp.Status)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("response is larger than %d bytes", maxBytes)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("write download file: %w", err)
	}
	if written > maxBytes {
		return fmt.Errorf("response exceeded %d bytes", maxBytes)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync download file: %w", err)
	}
	return nil
}

func (m *Manager) advance(id string, phase updaterapi.Phase, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
	m.operation.Phase = phase
	m.operation.Message = message
	m.operation.UpdatedAt = m.cfg.Now().UTC()
	return m.persistLocked()
}

func (m *Manager) setLogPath(id, logPath, recovery string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
	m.operation.LogPath = logPath
	m.operation.RecoveryCommand = recovery
	m.operation.UpdatedAt = m.cfg.Now().UTC()
	return m.persistLocked()
}

func (m *Manager) fail(id string, err error, recovery string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return
	}
	now := m.cfg.Now().UTC()
	m.operation.Phase = updaterapi.PhaseFailed
	m.operation.Message = "Upgrade failed"
	m.operation.Error = err.Error()
	if recovery != "" {
		m.operation.RecoveryCommand = recovery
	}
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	if persistErr := m.persistLocked(); persistErr != nil {
		log.Printf("persist failed upgrade state: %v", persistErr)
	}
}

func (m *Manager) succeed(id, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
	now := m.cfg.Now().UTC()
	m.operation.Phase = updaterapi.PhaseSucceeded
	m.operation.Message = message
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	return m.persistLocked()
}

func (m *Manager) loadState() error {
	data, err := os.ReadFile(filepath.Join(m.cfg.StateDir, stateFilename))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read updater state: %w", err)
	}
	var op updaterapi.Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return fmt.Errorf("decode updater state: %w", err)
	}
	if !op.Phase.Terminal() {
		now := m.cfg.Now().UTC()
		op.Phase = updaterapi.PhaseFailed
		op.Message = "Upgrade interrupted"
		op.Error = "The updater restarted before the operation completed; inspect the host log before retrying."
		op.UpdatedAt = now
		op.CompletedAt = &now
	}
	m.operation = &op
	return m.persistLocked()
}

func (m *Manager) persistLocked() error {
	if m.operation == nil {
		return nil
	}
	data, err := json.MarshalIndent(m.operation, "", "  ")
	if err != nil {
		return fmt.Errorf("encode updater state: %w", err)
	}
	path := filepath.Join(m.cfg.StateDir, stateFilename)
	temp, err := os.CreateTemp(m.cfg.StateDir, ".state-")
	if err != nil {
		return fmt.Errorf("create updater state temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure updater state temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write updater state temp file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync updater state temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close updater state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace updater state: %w", err)
	}
	return syncDirectory(m.cfg.StateDir)
}

func readInstalledVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read installed version: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "version:"); ok {
			version := strings.TrimSpace(value)
			if version != "" {
				return version, nil
			}
		}
	}
	return "", fmt.Errorf("installed version file has no version")
}

func verifyChecksum(archivePath, checksumPath, archiveName string) error {
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("read release checksum: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
		return fmt.Errorf("release checksum does not describe %s", archiveName)
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("release checksum is not a valid SHA-256 digest")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open release bundle: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash release bundle: %w", err)
	}
	if !equalBytes(hash.Sum(nil), expected) {
		return fmt.Errorf("release checksum verification failed")
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func extractArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
	var extractedBytes int64
	entries := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("archive contains more than %d entries", maxArchiveEntries)
		}
		if header.Size < 0 || header.Size > maxExtractedBytes-extractedBytes {
			return fmt.Errorf("archive expands beyond %d bytes", maxExtractedBytes)
		}
		extractedBytes += header.Size
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(name) || name == "." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("archive contains unsafe path %q", header.Name)
		}
		target := filepath.Join(destination, name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDestination) {
			return fmt.Errorf("archive path escapes staging directory: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			mode := os.FileMode(uint32(header.Mode & 0o755)) // #nosec G115 -- masked to nine permission bits
			if err := os.MkdirAll(target, mode); err != nil {
				return fmt.Errorf("create archive directory %s: %w", header.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			// #nosec G301 -- extracted deployment parents must be traversable
			// by the account that owns and manually maintains the install.
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create archive parent for %s: %w", header.Name, err)
			}
			mode := os.FileMode(uint32(header.Mode & 0o755)) // #nosec G115 -- masked to nine permission bits
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create archive file %s: %w", header.Name, err)
			}
			// tar.Reader already bounds reads to header.Size; the additional
			// LimitReader documents that bound for static analysis.
			if _, err := io.Copy(out, io.LimitReader(tr, header.Size)); err != nil { // #nosec G110
				out.Close()
				return fmt.Errorf("extract archive file %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close archive file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("archive contains unsupported entry %q", header.Name)
		}
	}
}

func validateStagedRelease(deploymentPath, targetVersion string) error {
	required := []string{
		"docker-compose.yaml",
		"run-fleet.sh",
		"server/fleetd",
		"server/proto-plugin",
		"server/antminer-plugin",
		"server/asicrs-plugin",
	}
	for _, path := range required {
		info, err := os.Stat(filepath.Join(deploymentPath, path))
		if err != nil || info.IsDir() {
			return fmt.Errorf("release bundle is missing required file %s", path)
		}
	}
	version, err := readInstalledVersion(filepath.Join(deploymentPath, "version.txt"))
	if err != nil {
		return fmt.Errorf("validate staged version: %w", err)
	}
	if version != targetVersion {
		return fmt.Errorf("release bundle version %s does not match target %s", version, targetVersion)
	}
	return nil
}

func preserveDeploymentState(current, staged string) error {
	for _, path := range []string{".env", "server/influx_config/.env"} {
		source := filepath.Join(current, path)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect preserved file %s: %w", path, err)
		}
		if err := copyFile(source, filepath.Join(staged, path)); err != nil {
			return err
		}
	}
	sourceSSL := filepath.Join(current, "ssl")
	if _, err := os.Stat(sourceSSL); err == nil {
		if err := copyTree(sourceSSL, filepath.Join(staged, "ssl")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect preserved TLS directory: %w", err)
	}
	return nil
}

// The host updater runs as root so it can manage rootful Docker and its own
// systemd unit. Keep the extracted deployment owned by the same account as
// the previous deployment, otherwise one successful one-click upgrade would
// make later manual maintenance unexpectedly require root.
func preserveDeploymentOwnership(current, staged string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(current)
	if err != nil {
		return fmt.Errorf("inspect current deployment owner: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("read deployment owner")
	}
	err = filepath.WalkDir(staged, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk staged deployment: %w", walkErr)
		}
		if err := os.Chown(path, int(stat.Uid), int(stat.Gid)); err != nil {
			return fmt.Errorf("set staged deployment owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preserve staged deployment ownership: %w", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect source file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to copy non-regular file %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("open destination file: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy file contents: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close destination file: %w", err)
	}
	return nil
}

func atomicReplaceExecutable(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect staged updater: %w", err)
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("staged updater is not executable")
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".proto-fleet-updater-")
	if err != nil {
		return fmt.Errorf("create updater executable temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	in, err := os.Open(source)
	if err != nil {
		temp.Close()
		return fmt.Errorf("open staged updater: %w", err)
	}
	_, copyErr := io.Copy(temp, in)
	closeInErr := in.Close()
	if copyErr != nil {
		temp.Close()
		return fmt.Errorf("copy staged updater: %w", copyErr)
	}
	if closeInErr != nil {
		temp.Close()
		return fmt.Errorf("close staged updater: %w", closeInErr)
	}
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return fmt.Errorf("make updater executable: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync updater executable: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close updater executable: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("replace updater executable: %w", err)
	}
	return syncDirectory(filepath.Dir(destination))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func copyTree(source, destination string) error {
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk preserved tree: %w", walkErr)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("resolve preserved tree path: %w", err)
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect preserved tree entry: %w", err)
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create preserved directory: %w", err)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to preserve symlink %s", path)
		}
		return copyFile(path, target)
	})
	if err != nil {
		return fmt.Errorf("copy preserved tree: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
