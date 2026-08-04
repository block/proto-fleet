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
	stateFilename            = "state.json"
	maxChecksumBytes         = 4096
	maxDownloadBytes         = int64(8 << 30) // 8 GiB hard stop for a corrupt or hostile response.
	maxExtractedBytes        = int64(16 << 30)
	maxArchiveEntries        = 100_000
	defaultHTTPTimeout       = 30 * time.Minute
	defaultPreflightTimeout  = 2 * time.Hour
	defaultActivationTimeout = 45 * time.Minute
	defaultCleanupTimeout    = 2 * time.Minute
	maxCommandLogBytes       = int64(64 << 20)
	canonicalDownloadBaseURL = "https://github.com/block/proto-fleet/releases/download"
	processLockFilename      = "updater.lock"
	activationMarkerFilename = "activation-swap.json"
	activationMarkerTempName = ".activation-swap.json.tmp"
	operationArtifactPrefix  = ".proto-fleet-upgrade-"
	stateTempPrefix          = ".state-"
)

var (
	canonicalRelease         = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-rc\.\d+)?$`)
	staleStateArtifactName   = regexp.MustCompile(`^\.proto-fleet-upgrade-[a-f0-9]{64}\.(?:tar\.gz|sha256|updater)$`)
	staleStagingArtifactName = regexp.MustCompile(`^\.proto-fleet-upgrade-[a-f0-9]{64}$`)
)

var releaseImageRepositories = [...]string{
	"proto-fleet-api",
	"proto-fleet-client",
	"proto-fleet-timescaledb",
	"proto-fleet-timescaledb-ha",
}

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Commands run in their own process group so a timed-out shell cannot
		// leave Docker clients or other descendants running without supervision.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return fmt.Errorf("kill command process group: %w", err)
		}
		return nil
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

type Config struct {
	InstallRoot       string
	StateDir          string
	DownloadBaseURL   string
	HTTPClient        *http.Client
	Runner            CommandRunner
	Now               func() time.Time
	NewID             func() string
	GOARCH            string
	SelfUpdatePath    string
	PreflightTimeout  time.Duration
	ActivationTimeout time.Duration
	CleanupTimeout    time.Duration

	// Tests inject an httptest TLS endpoint without opening a production
	// configuration path for alternate release mirrors.
	allowTestDownloadBaseURL bool
}

type activationMarker struct {
	OperationID   string `json:"operation_id"`
	TargetVersion string `json:"target_version"`
}

type Manager struct {
	cfg Config

	mu               sync.RWMutex
	operation        *updaterapi.Operation
	closing          bool
	operationRunning bool
	cancelOperation  context.CancelFunc
	operationWG      sync.WaitGroup
	selfUpdateReady  chan struct{}
	processLock      *os.File
	closeOnce        sync.Once
	closeErr         error
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
	if cfg.DownloadBaseURL == "" {
		cfg.DownloadBaseURL = canonicalDownloadBaseURL
	} else if cfg.DownloadBaseURL != canonicalDownloadBaseURL && !cfg.allowTestDownloadBaseURL {
		return nil, fmt.Errorf("download base URL must use the official GitHub Releases URL")
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
	if cfg.PreflightTimeout == 0 {
		cfg.PreflightTimeout = defaultPreflightTimeout
	}
	if cfg.ActivationTimeout == 0 {
		cfg.ActivationTimeout = defaultActivationTimeout
	}
	if cfg.CleanupTimeout == 0 {
		cfg.CleanupTimeout = defaultCleanupTimeout
	}
	if cfg.PreflightTimeout < 0 || cfg.ActivationTimeout < 0 || cfg.CleanupTimeout < 0 {
		return nil, fmt.Errorf("updater phase timeouts must be positive")
	}
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, "logs"), 0o700); err != nil {
		return nil, fmt.Errorf("create updater state directory: %w", err)
	}
	processLock, err := acquireProcessLock(cfg.StateDir)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		cfg:             cfg,
		selfUpdateReady: make(chan struct{}, 1),
		processLock:     processLock,
	}
	if err := m.loadState(); err != nil {
		_ = processLock.Close()
		return nil, err
	}
	if err := m.cleanupStaleArtifacts(); err != nil {
		_ = processLock.Close()
		return nil, err
	}
	return m, nil
}

func (m *Manager) Close() error {
	return m.Shutdown(context.Background())
}

// SelfUpdateReady is signaled once the replacement executable and successful
// terminal state are both durable. The command process owns listener draining,
// lock release, and exec so the manager remains independent of any supervisor.
func (m *Manager) SelfUpdateReady() <-chan struct{} {
	return m.selfUpdateReady
}

// Shutdown rejects future triggers, cancels work that has not crossed the
// activation boundary, waits for the operation to persist a terminal state,
// and only then releases the daemon lock. An active forward migration is not
// canceled; its own bounded activation deadline remains the safety limit.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closing = true
	cancelOperation := m.operation == nil || m.operation.Phase != updaterapi.PhaseActivating
	cancel := m.cancelOperation
	m.mu.Unlock()
	if cancelOperation && cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		m.operationWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		if cancel != nil {
			cancel()
		}
		m.closeOnce.Do(func() {
			if m.processLock != nil {
				m.closeErr = m.processLock.Close()
			}
		})
		return m.closeErr
	case <-ctx.Done():
		return fmt.Errorf("wait for updater operation shutdown: %w", ctx.Err())
	}
}

func acquireProcessLock(stateDir string) (*os.File, error) {
	lockPath := filepath.Join(stateDir, processLockFilename)
	fd, err := syscall.Open(
		lockPath,
		syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open updater process lock: %w", err)
	}
	lockFile := os.NewFile(uintptr(fd), lockPath)
	if lockFile == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("open updater process lock")
	}
	closeWithError := func(err error) (*os.File, error) {
		_ = lockFile.Close()
		return nil, err
	}
	info, err := lockFile.Stat()
	if err != nil {
		return closeWithError(fmt.Errorf("inspect updater process lock: %w", err))
	}
	if !info.Mode().IsRegular() {
		return closeWithError(fmt.Errorf("updater process lock is not a regular file"))
	}
	if err := lockFile.Chmod(0o600); err != nil {
		return closeWithError(fmt.Errorf("secure updater process lock: %w", err))
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return closeWithError(fmt.Errorf("another updater process is already running"))
		}
		return closeWithError(fmt.Errorf("acquire updater process lock: %w", err))
	}
	// PID metadata is diagnostic only; the open-file-description lock is the
	// authority. Failure to refresh the hint must not weaken serialization.
	if err := writeProcessLockPID(lockFile); err != nil {
		log.Printf("write updater process lock owner: %v", err)
	}
	return lockFile, nil
}

func writeProcessLockPID(lockFile *os.File) error {
	if _, err := lockFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek updater process lock: %w", err)
	}
	if err := lockFile.Truncate(0); err != nil {
		return fmt.Errorf("truncate updater process lock: %w", err)
	}
	if _, err := fmt.Fprintf(lockFile, "%d\n", os.Getpid()); err != nil {
		return fmt.Errorf("write updater process lock: %w", err)
	}
	if err := lockFile.Sync(); err != nil {
		return fmt.Errorf("sync updater process lock: %w", err)
	}
	return nil
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
	marker, err := m.readActivationMarker()
	if err != nil {
		return updaterapi.Operation{}, fmt.Errorf("inspect pending activation recovery: %w", err)
	}
	if marker != nil {
		return updaterapi.Operation{}, fmt.Errorf(
			"activation recovery for operation %s is pending; restart the updater after completing recovery",
			marker.OperationID,
		)
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
	if m.closing {
		return updaterapi.Operation{}, fmt.Errorf("updater is shutting down")
	}
	if m.operation != nil && !m.operation.Phase.Terminal() {
		return updaterapi.Operation{}, fmt.Errorf("an upgrade to %s is already in progress", m.operation.TargetVersion)
	}
	if m.operationRunning {
		return updaterapi.Operation{}, fmt.Errorf("the previous upgrade is still completing cleanup")
	}
	if err := m.cleanupStaleArtifacts(); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("clean stale upgrade artifacts: %w", err)
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
	operationCtx, cancelOperation := context.WithCancel(context.Background())
	m.cancelOperation = cancelOperation
	m.operationRunning = true
	m.operationWG.Add(1)
	go func() {
		defer m.operationWG.Done()
		defer m.finishOperation()
		m.run(operationCtx, operationCopy.ID, targetVersion)
	}()
	return operationCopy, nil
}

func (m *Manager) finishOperation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operationRunning = false
	m.cancelOperation = nil
}

func (m *Manager) run(ctx context.Context, operationID, targetVersion string) {
	logPath := filepath.Join(m.cfg.StateDir, "logs", operationID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		m.fail(operationID, fmt.Errorf("open upgrade log: %w", err), "")
		return
	}
	defer logFile.Close()
	defer func() {
		operation := m.Status().Operation
		if operation == nil || operation.ID != operationID || !operation.Phase.Terminal() {
			return
		}
		_, _ = fmt.Fprintf(
			logFile,
			"[%s] terminal phase=%s error=%q\n",
			m.cfg.Now().UTC().Format(time.RFC3339),
			operation.Phase,
			operation.Error,
		)
	}()
	commandOutput := newCappedWriter(logFile, maxCommandLogBytes)

	recovery := ""
	if err := m.setLogPath(operationID, logPath, recovery); err != nil {
		m.fail(operationID, fmt.Errorf("persist upgrade log location: %w", err), recovery)
		return
	}
	_, _ = fmt.Fprintf(logFile, "[%s] starting upgrade to %s\n", m.cfg.Now().UTC().Format(time.RFC3339), targetVersion)

	archiveName := fmt.Sprintf("proto-fleet-%s-%s.tar.gz", targetVersion, m.cfg.GOARCH)
	archiveURL := strings.TrimSuffix(m.cfg.DownloadBaseURL, "/") + "/" + targetVersion + "/" + archiveName
	artifactBase := operationArtifactBase(operationID)
	archivePath := filepath.Join(m.cfg.StateDir, artifactBase+".tar.gz")
	checksumPath := filepath.Join(m.cfg.StateDir, artifactBase+".sha256")
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

	if err := m.advance(operationID, updaterapi.PhaseVerifying, "Verifying release checksum"); err != nil {
		m.fail(operationID, fmt.Errorf("persist verification phase: %w", err), recovery)
		return
	}
	// The fixed GitHub Releases origin is the publisher trust anchor. This
	// sidecar detects transfer/storage corruption; it is not represented as an
	// independent publisher signature.
	if err := verifyChecksum(ctx, archivePath, checksumPath, archiveName); err != nil {
		m.fail(operationID, err, recovery)
		return
	}

	stageRoot := filepath.Join(m.cfg.InstallRoot, artifactBase)
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		m.fail(operationID, fmt.Errorf("create staging directory: %w", err), recovery)
		return
	}
	defer os.RemoveAll(stageRoot)

	if err := m.advance(operationID, updaterapi.PhaseStaging, "Staging release and preserving configuration"); err != nil {
		m.fail(operationID, fmt.Errorf("persist staging phase: %w", err), recovery)
		return
	}
	var preparedUpdater *os.File
	updaterCopied := false
	if m.cfg.SelfUpdatePath != "" {
		preparedUpdaterPath := filepath.Join(m.cfg.StateDir, artifactBase+".updater")
		preparedUpdater, err = os.OpenFile(preparedUpdaterPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			m.fail(operationID, fmt.Errorf("create protected updater copy: %w", err), recovery)
			return
		}
		defer os.Remove(preparedUpdaterPath)
		defer func() {
			if preparedUpdater != nil {
				_ = preparedUpdater.Close()
			}
		}()
	}
	if err := extractArchiveWithUpdaterCopy(
		ctx,
		archivePath,
		stageRoot,
		preparedUpdater,
		&updaterCopied,
	); err != nil {
		m.fail(operationID, fmt.Errorf("extract release bundle: %w", err), recovery)
		return
	}
	if preparedUpdater != nil {
		if !updaterCopied {
			m.fail(operationID, fmt.Errorf("release bundle is missing required updater binary"), recovery)
			return
		}
		if err := preparedUpdater.Chmod(0o700); err != nil {
			m.fail(operationID, fmt.Errorf("secure protected updater copy: %w", err), recovery)
			return
		}
		if err := preparedUpdater.Sync(); err != nil {
			m.fail(operationID, fmt.Errorf("sync protected updater copy: %w", err), recovery)
			return
		}
		if _, err := preparedUpdater.Seek(0, io.SeekStart); err != nil {
			m.fail(operationID, fmt.Errorf("rewind protected updater copy: %w", err), recovery)
			return
		}
	}
	stageDeployment := filepath.Join(stageRoot, "deployment")
	currentDeployment := filepath.Join(m.cfg.InstallRoot, "deployment")
	if err := validateStagedRelease(stageDeployment, targetVersion); err != nil {
		m.fail(operationID, err, recovery)
		return
	}
	if err := preserveDeploymentState(ctx, currentDeployment, stageDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("preserve deployment configuration: %w", err), recovery)
		return
	}
	if err := preserveDeploymentOwnership(ctx, currentDeployment, stageDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("preserve deployment ownership: %w", err), recovery)
		return
	}

	if err := m.advance(operationID, updaterapi.PhasePreflight, "Building and validating the new stack while Fleet stays online"); err != nil {
		m.fail(operationID, fmt.Errorf("persist preflight phase: %w", err), recovery)
		return
	}
	if err := m.runCommand(ctx, m.cfg.PreflightTimeout, stageDeployment, commandOutput, "/bin/bash", "./run-fleet.sh", "--non-interactive", "--preflight-only"); err != nil {
		if ctx.Err() == nil {
			m.removeFailedPreflightImages(ctx, stageDeployment, commandOutput, logFile, targetVersion)
		}
		m.fail(operationID, fmt.Errorf("upgrade preflight failed: %w", err), recovery)
		return
	}

	if err := m.beginActivation(operationID, "Restarting Fleet; the client may disconnect for several minutes"); err != nil {
		m.fail(operationID, fmt.Errorf("persist activation phase: %w", err), recovery)
		return
	}
	backupDeployment := filepath.Join(m.cfg.InstallRoot, "deployment.previous")
	if err := activateDeployment(stageDeployment, currentDeployment, backupDeployment); err != nil {
		m.failActivation(operationID, targetVersion, err, logFile)
		return
	}
	recovery = activationRecoveryCommand(currentDeployment)
	if err := m.setRecoveryCommand(operationID, recovery); err != nil {
		m.fail(operationID, fmt.Errorf("persist activation recovery command: %w", err), recovery)
		return
	}
	if err := m.clearActivationMarker(); err != nil {
		m.failActivation(operationID, targetVersion, fmt.Errorf("complete activation swap: %w", err), logFile)
		return
	}

	if err := m.runCommand(ctx, m.cfg.ActivationTimeout, currentDeployment, commandOutput, "/bin/bash", "./run-fleet.sh", "--non-interactive", "--skip-build"); err != nil {
		// run-fleet may have applied forward-only migrations before returning an
		// error. Keep the new deployment active for forward recovery; restoring
		// the previous binaries could make the schema and application incompatible.
		m.fail(operationID, fmt.Errorf("new stack failed to start: %w", err), recovery)
		return
	}
	successMessage := fmt.Sprintf("Fleet %s is running", targetVersion)
	selfUpdateSucceeded := false
	if preparedUpdater != nil {
		if err := atomicReplaceExecutableFromFile(preparedUpdater, m.cfg.SelfUpdatePath); err != nil {
			_, _ = fmt.Fprintf(logFile, "[%s] warning: Fleet is healthy, but the host updater binary was not refreshed: %v\n", m.cfg.Now().UTC().Format(time.RFC3339), err)
			successMessage += "; host updater refresh needs attention (see upgrade log)"
		} else {
			selfUpdateSucceeded = true
		}
	}

	if err := m.succeed(operationID, successMessage, selfUpdateSucceeded); err != nil {
		_, _ = fmt.Fprintf(logFile, "[%s] warning: persist successful terminal state: %v\n", m.cfg.Now().UTC().Format(time.RFC3339), err)
		log.Printf("persist successful upgrade state: %v", err)
	} else if selfUpdateSucceeded {
		select {
		case m.selfUpdateReady <- struct{}{}:
		default:
		}
	}
	_, _ = fmt.Fprintf(logFile, "[%s] upgrade completed\n", m.cfg.Now().UTC().Format(time.RFC3339))
}

func operationArtifactBase(operationID string) string {
	digest := sha256.Sum256([]byte(operationID))
	return operationArtifactPrefix + hex.EncodeToString(digest[:])
}

func (m *Manager) writeActivationMarker(marker activationMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("encode activation marker: %w", err)
	}
	path := filepath.Join(m.cfg.StateDir, activationMarkerFilename)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("create activation marker: marker already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect activation marker destination: %w", err)
	}
	tempPath := filepath.Join(m.cfg.StateDir, activationMarkerTempName)
	fd, err := syscall.Open(
		tempPath,
		syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create activation marker temp file: %w", err)
	}
	file := os.NewFile(uintptr(fd), tempPath)
	if file == nil {
		_ = syscall.Close(fd)
		return fmt.Errorf("create activation marker temp file")
	}
	tempInstalled := false
	defer func() {
		if !tempInstalled {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write activation marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync activation marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close activation marker: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install activation marker: %w", err)
	}
	tempInstalled = true
	if err := syncDirectory(m.cfg.StateDir); err != nil {
		return fmt.Errorf("persist activation marker: %w", err)
	}
	return nil
}

func (m *Manager) readActivationMarker() (*activationMarker, error) {
	path := filepath.Join(m.cfg.StateDir, activationMarkerFilename)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect activation marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("activation marker is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read activation marker: %w", err)
	}
	var marker activationMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, fmt.Errorf("decode activation marker: %w", err)
	}
	if marker.OperationID == "" || !canonicalRelease.MatchString(marker.TargetVersion) || !semver.IsValid(marker.TargetVersion) {
		return nil, fmt.Errorf("activation marker is invalid")
	}
	return &marker, nil
}

func (m *Manager) clearActivationMarker() error {
	path := filepath.Join(m.cfg.StateDir, activationMarkerFilename)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("remove activation marker: %w", err)
	}
	if err := syncDirectory(m.cfg.StateDir); err != nil {
		return fmt.Errorf("persist activation marker removal: %w", err)
	}
	return nil
}

func (m *Manager) failActivation(
	operationID string,
	targetVersion string,
	activationErr error,
	logOutput io.Writer,
) {
	layout := &updaterapi.Operation{
		TargetVersion: targetVersion,
		Phase:         updaterapi.PhaseActivating,
	}
	restoredPrevious, reconcileErr := m.reconcileDeploymentLayout(layout, true, true, true)
	if restoredPrevious {
		_, _ = fmt.Fprintf(logOutput, "[%s] activation failed before Fleet stopped; restored the previous deployment\n", m.cfg.Now().UTC().Format(time.RFC3339))
	}
	if reconcileErr == nil {
		reconcileErr = m.clearActivationMarker()
	}
	if reconcileErr != nil {
		activationErr = errors.Join(
			activationErr,
			fmt.Errorf("reconcile failed activation layout: %w", reconcileErr),
		)
	}
	m.fail(operationID, activationErr, layout.RecoveryCommand)
}

// activateDeployment makes the extracted release durable before entering the
// two-rename activation window. The two renames cannot be committed as one
// portable filesystem operation, so every completed metadata step is fsynced
// and every pre-command failure attempts a checked restoration. Startup
// reconciliation covers a process or power loss between the renames.
func activateDeployment(staged, current, previous string) error {
	installRoot := filepath.Dir(current)
	stageRoot := filepath.Dir(staged)
	if err := syncTree(staged); err != nil {
		return fmt.Errorf("sync staged deployment before activation: %w", err)
	}
	if err := os.RemoveAll(previous); err != nil {
		return fmt.Errorf("remove previous deployment backup: %w", err)
	}
	if err := syncDirectory(installRoot); err != nil {
		return fmt.Errorf("persist previous deployment backup removal: %w", err)
	}
	if err := os.Rename(current, previous); err != nil {
		return fmt.Errorf("back up current deployment: %w", err)
	}
	if err := syncDirectory(installRoot); err != nil {
		restoreErr := restorePreviousDeployment(current, previous, installRoot)
		return errors.Join(
			fmt.Errorf("persist current deployment backup: %w", err),
			restoreErr,
		)
	}
	if err := os.Rename(staged, current); err != nil {
		restoreErr := restorePreviousDeployment(current, previous, installRoot)
		return errors.Join(
			fmt.Errorf("activate staged deployment: %w", err),
			restoreErr,
		)
	}
	if err := errors.Join(syncDirectory(stageRoot), syncDirectory(installRoot)); err != nil {
		restoreErr := rollbackPreparedDeployment(current, staged, previous, installRoot, stageRoot)
		return errors.Join(
			fmt.Errorf("persist activated deployment swap: %w", err),
			restoreErr,
		)
	}
	return nil
}

func restorePreviousDeployment(current, previous, installRoot string) error {
	if err := os.Rename(previous, current); err != nil {
		return fmt.Errorf("restore previous deployment: %w", err)
	}
	if err := syncDirectory(installRoot); err != nil {
		return fmt.Errorf("persist restored previous deployment: %w", err)
	}
	return nil
}

func rollbackPreparedDeployment(current, staged, previous, installRoot, stageRoot string) error {
	if err := os.Rename(current, staged); err != nil {
		return fmt.Errorf("move uncommitted deployment back to staging: %w", err)
	}
	if err := os.Rename(previous, current); err != nil {
		return fmt.Errorf("restore previous deployment after durability failure: %w", err)
	}
	if err := errors.Join(syncDirectory(stageRoot), syncDirectory(installRoot)); err != nil {
		return fmt.Errorf("persist deployment restoration: %w", err)
	}
	return nil
}

func syncTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk tree for sync: %w", walkErr)
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect tree entry for sync: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to sync non-regular staged entry %s", path)
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open staged file for sync: %w", err)
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if syncErr != nil || closeErr != nil {
			var fileErrs []error
			if syncErr != nil {
				fileErrs = append(fileErrs, fmt.Errorf("sync staged file %s: %w", path, syncErr))
			}
			if closeErr != nil {
				fileErrs = append(fileErrs, fmt.Errorf("close staged file %s: %w", path, closeErr))
			}
			return errors.Join(fileErrs...)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sync staged tree: %w", err)
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := syncDirectory(directories[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) runCommand(
	parent context.Context,
	timeout time.Duration,
	dir string,
	output io.Writer,
	name string,
	args ...string,
) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := m.cfg.Runner.Run(ctx, dir, output, name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("command did not complete within its phase deadline: %w", ctxErr)
	}
	return err
}

type cappedWriter struct {
	mu        sync.Mutex
	output    io.Writer
	remaining int64
	truncated bool
}

func newCappedWriter(output io.Writer, limit int64) *cappedWriter {
	return &cappedWriter{output: output, remaining: limit}
}

func (w *cappedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	originalLength := len(data)
	allowed := originalLength
	if int64(allowed) > w.remaining {
		allowed = int(w.remaining)
	}
	if allowed > 0 {
		written, err := w.output.Write(data[:allowed])
		w.remaining -= int64(written)
		if err != nil {
			return written, fmt.Errorf("write bounded command output: %w", err)
		}
		if written != allowed {
			return written, io.ErrShortWrite
		}
	}
	if allowed < originalLength && !w.truncated {
		w.truncated = true
		if _, err := io.WriteString(w.output, "\n[command output truncated at configured limit]\n"); err != nil {
			return allowed, fmt.Errorf("write command output truncation marker: %w", err)
		}
	}
	// Discarded bytes are reported as consumed so verbose child commands keep
	// running while manager-owned terminal messages remain writable directly to
	// the underlying log file.
	return originalLength, nil
}

// removeFailedPreflightImages releases only the immutable target tags from a
// preflight that did not complete. The manager has already proved the target
// is newer than the active release, and Docker refuses to remove an image used
// by any running or stopped container. Cleanup is best-effort so it can never
// hide the original preflight failure or make recovery less reliable.
func (m *Manager) removeFailedPreflightImages(
	parent context.Context,
	dir string,
	commandOutput io.Writer,
	logOutput io.Writer,
	targetVersion string,
) {
	ctx, cancel := context.WithTimeout(parent, m.cfg.CleanupTimeout)
	defer cancel()
	for _, repository := range releaseImageRepositories {
		image := repository + ":" + targetVersion
		if err := m.cfg.Runner.Run(ctx, dir, commandOutput, "docker", "image", "rm", image); err != nil {
			_, _ = fmt.Fprintf(logOutput, "warning: could not remove failed preflight image %s: %v\n", image, err)
		}
	}
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

func (m *Manager) beginActivation(id, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
	if m.closing {
		return fmt.Errorf("updater is shutting down before activation")
	}
	marker := activationMarker{
		OperationID:   m.operation.ID,
		TargetVersion: m.operation.TargetVersion,
	}
	if err := m.writeActivationMarker(marker); err != nil {
		return fmt.Errorf("persist activation swap marker: %w", err)
	}
	m.operation.Phase = updaterapi.PhaseActivating
	m.operation.Message = message
	m.operation.UpdatedAt = m.cfg.Now().UTC()
	if err := m.persistLocked(); err != nil {
		return errors.Join(
			fmt.Errorf("persist activating operation: %w", err),
			m.clearActivationMarker(),
		)
	}
	return nil
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

func (m *Manager) setRecoveryCommand(id, recovery string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
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
	m.operation.RecoveryCommand = recovery
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	if persistErr := m.persistLocked(); persistErr != nil {
		log.Printf("persist failed upgrade state: %v", persistErr)
	}
}

func (m *Manager) succeed(id, message string, closeForSelfUpdate bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
	now := m.cfg.Now().UTC()
	m.operation.Phase = updaterapi.PhaseSucceeded
	m.operation.Message = message
	m.operation.RecoveryCommand = ""
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	if err := m.persistLocked(); err != nil {
		return err
	}
	if closeForSelfUpdate {
		// Bar a second trigger before the restart notification is observable.
		// The command process will drain this manager and exec the replacement.
		m.closing = true
	}
	return nil
}

func (m *Manager) loadState() error {
	marker, err := m.readActivationMarker()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(m.cfg.StateDir, stateFilename))
	if errors.Is(err, os.ErrNotExist) {
		if marker != nil {
			return fmt.Errorf("activation marker exists without persisted operation state")
		}
		_, reconcileErr := m.reconcileDeploymentLayout(&updaterapi.Operation{}, false, false, false)
		return reconcileErr
	}
	if err != nil {
		return fmt.Errorf("read updater state: %w", err)
	}
	var op updaterapi.Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return fmt.Errorf("decode updater state: %w", err)
	}
	if marker != nil && (marker.OperationID != op.ID || marker.TargetVersion != op.TargetVersion) {
		return fmt.Errorf("activation marker does not match persisted operation")
	}
	if marker != nil && op.Phase == updaterapi.PhaseSucceeded {
		return fmt.Errorf("successful operation retains an unexpected activation marker")
	}
	wasTerminal := op.Phase.Terminal()
	deriveForwardRecovery := op.Phase == updaterapi.PhaseActivating || marker != nil
	if !wasTerminal && !deriveForwardRecovery {
		op.RecoveryCommand = ""
	}
	restoredPrevious, err := m.reconcileDeploymentLayout(
		&op,
		deriveForwardRecovery,
		marker != nil,
		marker != nil,
	)
	if err != nil {
		if marker != nil && op.RecoveryCommand != "" {
			now := m.cfg.Now().UTC()
			op.Phase = updaterapi.PhaseFailed
			op.Message = "Activation layout requires manual recovery"
			recoveryError := "Activation layout recovery did not complete: " + err.Error()
			if !strings.Contains(op.Error, recoveryError) {
				if op.Error != "" {
					op.Error += " "
				}
				op.Error += recoveryError
			}
			op.UpdatedAt = now
			if op.CompletedAt == nil {
				op.CompletedAt = &now
			}
			m.operation = &op
			if persistErr := m.persistLocked(); persistErr != nil {
				return errors.Join(err, fmt.Errorf("persist activation recovery instructions: %w", persistErr))
			}
		}
		return err
	}
	if marker != nil {
		if err := m.clearActivationMarker(); err != nil {
			return err
		}
	}
	if !wasTerminal {
		now := m.cfg.Now().UTC()
		op.Phase = updaterapi.PhaseFailed
		if restoredPrevious {
			op.Message = "Upgrade interrupted; previous deployment restored"
			op.Error = "The updater restarted during the activation swap before Fleet was stopped. The previous deployment was restored safely."
		} else {
			op.Message = "Upgrade interrupted"
			op.Error = "The updater restarted before the operation completed; inspect the host log and recovery details before retrying."
		}
		op.UpdatedAt = now
		op.CompletedAt = &now
	} else if restoredPrevious {
		now := m.cfg.Now().UTC()
		op.Phase = updaterapi.PhaseFailed
		op.Message = "Previous deployment restored during updater startup"
		if op.Error == "" {
			op.Error = "The active deployment was missing; the updater restored the validated previous deployment."
		} else {
			op.Error += " The active deployment was missing during startup; the updater restored the validated previous deployment."
		}
		op.RecoveryCommand = ""
		op.UpdatedAt = now
		if op.CompletedAt == nil {
			op.CompletedAt = &now
		}
	}
	m.operation = &op
	return m.persistLocked()
}

// reconcileDeploymentLayout validates the active layout and repairs the
// two-rename crash gap only when a durable activation marker proves the
// operation had not yet crossed into command execution. A present forward
// deployment is never rolled back because migrations may already be applied.
func (m *Manager) reconcileDeploymentLayout(
	op *updaterapi.Operation,
	deriveForwardRecovery bool,
	allowPreviousRestore bool,
	ensureDurable bool,
) (bool, error) {
	current := filepath.Join(m.cfg.InstallRoot, "deployment")
	previous := filepath.Join(m.cfg.InstallRoot, "deployment.previous")
	currentInfo, err := os.Lstat(current)
	if err == nil {
		if !currentInfo.IsDir() {
			return false, fmt.Errorf("reconcile deployment layout: active deployment is not a directory")
		}
		version, versionErr := readInstalledVersion(filepath.Join(current, "version.txt"))
		if versionErr != nil {
			return false, fmt.Errorf("validate active deployment during reconciliation: %w", versionErr)
		}
		if deriveForwardRecovery {
			op.RecoveryCommand = ""
			if version == op.TargetVersion {
				markerInfo, markerErr := os.Lstat(filepath.Join(current, ".update-preflight-complete"))
				if markerErr == nil {
					if !markerInfo.Mode().IsRegular() {
						return false, fmt.Errorf("activation preflight proof is not a regular file")
					}
					op.RecoveryCommand = activationRecoveryCommand(current)
				} else if !errors.Is(markerErr, os.ErrNotExist) {
					return false, fmt.Errorf("inspect activation preflight proof: %w", markerErr)
				}
			}
		}
		if ensureDurable {
			if err := syncDirectory(m.cfg.InstallRoot); err != nil {
				return false, fmt.Errorf("persist reconciled active deployment: %w", err)
			}
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect active deployment during reconciliation: %w", err)
	}
	if !allowPreviousRestore {
		return false, fmt.Errorf("active deployment is missing without a pending activation swap; refusing automatic rollback")
	}
	previousInfo, err := os.Lstat(previous)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("reconcile deployment layout: both deployment and deployment.previous are missing")
		}
		return false, fmt.Errorf("inspect previous deployment during reconciliation: %w", err)
	}
	if !previousInfo.IsDir() {
		return false, fmt.Errorf("reconcile deployment layout: deployment.previous is not a directory")
	}
	if _, err := readInstalledVersion(filepath.Join(previous, "version.txt")); err != nil {
		return false, fmt.Errorf("validate previous deployment during reconciliation: %w", err)
	}
	op.RecoveryCommand = activationRestoreCommand(current, previous)
	if err := os.Rename(previous, current); err != nil {
		return false, fmt.Errorf("restore previous deployment during reconciliation: %w", err)
	}
	if err := syncDirectory(m.cfg.InstallRoot); err != nil {
		return false, fmt.Errorf("persist restored deployment during reconciliation: %w", err)
	}
	op.RecoveryCommand = ""
	return true, nil
}

// cleanupStaleArtifacts runs only after the daemon-lifetime lock is held and
// any interrupted deployment swap has been reconciled. Exact SHA-derived
// names keep cleanup inside reserved direct-child slots. Non-directories are
// unlinked without following them; directories in state-file slots fail closed.
func (m *Manager) cleanupStaleArtifacts() error {
	removedState, cleanupStateErr := cleanupStaleStateArtifacts(m.cfg.StateDir)
	if removedState {
		if err := syncDirectory(m.cfg.StateDir); err != nil {
			return fmt.Errorf("persist stale updater state cleanup: %w", err)
		}
	}
	if cleanupStateErr != nil {
		return cleanupStateErr
	}

	removedStages, cleanupStagesErr := cleanupStaleStageDirectories(m.cfg.InstallRoot)
	if removedStages {
		if err := syncDirectory(m.cfg.InstallRoot); err != nil {
			return fmt.Errorf("persist stale updater staging cleanup: %w", err)
		}
	}
	if cleanupStagesErr != nil {
		return cleanupStagesErr
	}
	return nil
}

func cleanupStaleStateArtifacts(stateDir string) (bool, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return false, fmt.Errorf("list updater state for stale artifacts: %w", err)
	}
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if !staleStateArtifactName.MatchString(name) &&
			!strings.HasPrefix(name, stateTempPrefix) &&
			name != activationMarkerTempName {
			continue
		}
		path := filepath.Join(stateDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return removed, fmt.Errorf("inspect stale updater state artifact %s: %w", name, err)
		}
		if info.IsDir() {
			return removed, fmt.Errorf("refusing to remove stale updater state directory from file slot %s", name)
		}
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("remove stale updater state artifact %s: %w", name, err)
		}
		removed = true
	}
	return removed, nil
}

func cleanupStaleStageDirectories(installRoot string) (bool, error) {
	entries, err := os.ReadDir(installRoot)
	if err != nil {
		return false, fmt.Errorf("list install root for stale updater staging: %w", err)
	}
	removed := false
	for _, entry := range entries {
		if !staleStagingArtifactName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(installRoot, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return removed, fmt.Errorf("inspect stale updater staging directory %s: %w", entry.Name(), err)
		}
		remove := os.Remove
		if info.IsDir() {
			remove = os.RemoveAll
		}
		if err := remove(path); err != nil {
			return removed, fmt.Errorf("remove stale updater staging directory %s: %w", entry.Name(), err)
		}
		removed = true
	}
	return removed, nil
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
	temp, err := os.CreateTemp(m.cfg.StateDir, stateTempPrefix)
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

func verifyChecksum(ctx context.Context, archivePath, checksumPath, archiveName string) error {
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
	if _, err := io.Copy(hash, readerWithContext(ctx, file)); err != nil {
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
	return extractArchiveWithUpdaterCopy(context.Background(), archivePath, destination, nil, nil)
}

// extractArchiveWithUpdaterCopy streams the updater payload directly from the
// checksum-verified archive into root-only state while extracting the ordinary
// deployment tree. The protected copy is therefore never read back through an
// operator-writable installation path before it replaces the host updater.
func extractArchiveWithUpdaterCopy(
	ctx context.Context,
	archivePath string,
	destination string,
	updaterCopy *os.File,
	updaterCopied *bool,
) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(readerWithContext(ctx, file))
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
	var extractedBytes int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extract release archive: %w", err)
		}
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
			writer := io.Writer(out)
			copyUpdater := updaterCopy != nil && filepath.ToSlash(name) == "deployment/updater/proto-fleet-updater"
			if copyUpdater {
				writer = io.MultiWriter(out, updaterCopy)
			}
			// tar.Reader already bounds reads to header.Size; the additional
			// LimitReader documents that bound for static analysis.
			if _, err := io.Copy(writer, io.LimitReader(tr, header.Size)); err != nil { // #nosec G110
				out.Close()
				return fmt.Errorf("extract archive file %s: %w", header.Name, err)
			}
			if copyUpdater && updaterCopied != nil {
				*updaterCopied = true
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

func preserveDeploymentState(ctx context.Context, current, staged string) error {
	for _, path := range []string{".env", "server/influx_config/.env"} {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preserve deployment configuration: %w", err)
		}
		source := filepath.Join(current, path)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect preserved file %s: %w", path, err)
		}
		if err := copyFile(ctx, source, filepath.Join(staged, path)); err != nil {
			return err
		}
	}
	sourceSSL := filepath.Join(current, "ssl")
	if _, err := os.Stat(sourceSSL); err == nil {
		if err := copyTree(ctx, sourceSSL, filepath.Join(staged, "ssl")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect preserved TLS directory: %w", err)
	}
	return nil
}

// The host updater runs as root so it can manage rootful Docker and its own
// systemd unit. The deployment owner is inside that same host-administrator
// trust boundary: supported non-root installs enable one-click only when the
// owner can control the same rootful Docker daemon, and root-run installs are
// root-owned. Preserve that owner so one successful upgrade does not make
// later manual maintenance unexpectedly require a different account.
func preserveDeploymentOwnership(ctx context.Context, current, staged string) error {
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
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preserve staged deployment ownership: %w", err)
		}
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

func copyFile(ctx context.Context, source, destination string) error {
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
	if _, err := io.Copy(out, readerWithContext(ctx, in)); err != nil {
		out.Close()
		return fmt.Errorf("copy file contents: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close destination file: %w", err)
	}
	return nil
}

func atomicReplaceExecutable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open staged updater: %w", err)
	}
	defer in.Close()
	return atomicReplaceExecutableFromFile(in, destination)
}

func atomicReplaceExecutableFromFile(in *os.File, destination string) error {
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind staged updater: %w", err)
	}
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("inspect staged updater: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("staged updater is not executable")
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".proto-fleet-updater-")
	if err != nil {
		return fmt.Errorf("create updater executable temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, in); err != nil {
		temp.Close()
		return fmt.Errorf("copy staged updater: %w", err)
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

func copyTree(ctx context.Context, source, destination string) error {
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preserve deployment tree: %w", err)
		}
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
		return copyFile(ctx, path, target)
	})
	if err != nil {
		return fmt.Errorf("copy preserved tree: %w", err)
	}
	return nil
}

type readerFunc func(data []byte) (int, error)

func (read readerFunc) Read(data []byte) (int, error) {
	return read(data)
}

func readerWithContext(ctx context.Context, reader io.Reader) io.Reader {
	return readerFunc(func(data []byte) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("read canceled: %w", err)
		}
		read, err := reader.Read(data)
		if errors.Is(err, io.EOF) {
			return read, io.EOF
		}
		if err != nil {
			return read, fmt.Errorf("read source: %w", err)
		}
		return read, nil
	})
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func activationRecoveryCommand(deploymentPath string) string {
	return fmt.Sprintf("cd %s && ./run-fleet.sh --non-interactive --skip-build", shellQuote(deploymentPath))
}

func activationRestoreCommand(current, previous string) string {
	quotedCurrent := shellQuote(current)
	quotedPrevious := shellQuote(previous)
	return fmt.Sprintf(
		"test ! -e %s && test ! -L %s && test -d %s && mv -- %s %s && sync",
		quotedCurrent,
		quotedCurrent,
		quotedPrevious,
		quotedPrevious,
		quotedCurrent,
	)
}
