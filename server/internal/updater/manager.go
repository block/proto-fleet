package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

const (
	stateFilename     = "state.json"
	maxChecksumBytes  = 4096
	maxDownloadBytes  = int64(1 << 30) // Larger bundles require an explicit updater contract change.
	maxExtractedBytes = int64(16 << 30)
	maxArchiveEntries = 10_000 // Larger bundles require an explicit packaging contract change.
	// The request deadline still permits release downloads over slow mine-site
	// links; shutdown cancellation interrupts it immediately before activation.
	defaultHTTPTimeout = 30 * time.Minute
	// Fleet remains online while preflight pulls, loads, and builds images on
	// supported ARM and SD-card-class hosts, so false timeouts cost more than
	// the generous cancellable bound.
	defaultPreflightTimeout = 2 * time.Hour
	// Activation includes migrations and multiple readiness windows after the
	// old stack is stopped. Timing out requires forward manual recovery, making
	// this a minimum liveness bound rather than spare retry time.
	defaultActivationTimeout               = 45 * time.Minute
	defaultCleanupTimeout                  = 2 * time.Minute
	defaultCandidateTimeout                = 10 * time.Second
	maxCommandLogBytes                     = int64(64 << 20)
	maxCandidateVersionBytes               = int64(4096)
	maxRetainedOperationLogs               = 8
	maxRetainedLogBytes                    = int64(256 << 20)
	canonicalDownloadBaseURL               = "https://github.com/block/proto-fleet/releases/download"
	processLockFilename                    = "updater.lock"
	activationMarkerFilename               = "activation-swap.json"
	activationMarkerTempName               = ".activation-swap.json.tmp"
	qualificationBeforeStopBarrierName     = "qualification-pause-before-ha-stop"
	qualificationAfterStopBarrierName      = "qualification-pause-after-ha-stop"
	qualificationBetweenRenamesBarrierName = "qualification-pause-between-deployment-renames"
	preflightProofFilename                 = ".update-preflight-complete"
	startupProofFilename                   = ".fleet-startup-complete"
	operationArtifactPrefix                = ".proto-fleet-upgrade-"
	selfUpdateBackupSuffix                 = ".previous"
	stateTempPrefix                        = ".state-"
	haNodeEnvPath                          = "/etc/proto-fleet/ha/node.env"
)

var (
	staleStateArtifactName          = regexp.MustCompile(`^\.proto-fleet-upgrade-[a-f0-9]{64}\.(?:tar\.gz|sha256|updater)$`)
	staleStagingArtifactName        = regexp.MustCompile(`^\.proto-fleet-upgrade-[a-f0-9]{64}$`)
	operationLogNamePattern         = regexp.MustCompile(`^\.proto-fleet-upgrade-[a-f0-9]{64}\.log$`)
	errTriggerInvalid               = errors.New("invalid updater trigger")
	errTriggerPrecondition          = errors.New("updater trigger precondition failed")
	errTriggerBusy                  = errors.New("updater is busy")
	errTriggerClosing               = errors.New("updater is shutting down")
	errAcknowledgeUnknown           = errors.New("no matching updater operation")
	errAcknowledgeActive            = errors.New("updater operation is not terminal")
	errAcknowledgeStartedAtRequired = errors.New("expected updater operation start time is required")
	errAcknowledgeStartedAtMismatch = errors.New("updater operation start time does not match")
	errAcknowledgeRevisionRequired  = errors.New("expected updater outcome revision is required")
	errAcknowledgeRevisionMismatch  = errors.New("updater outcome revision does not match")
)

type classifiedTriggerError struct {
	kind    error
	message string
}

func (e *classifiedTriggerError) Error() string {
	return e.message
}

func (e *classifiedTriggerError) Unwrap() error {
	return e.kind
}

func newTriggerError(kind error, message string) error {
	return &classifiedTriggerError{kind: kind, message: message}
}

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
	cmd.Env = append(
		os.Environ(),
		"CI=1",
		"TERM=dumb",
		// Coordination marker for updater-owned run-fleet children; not auth.
		"PROTO_FLEET_UPDATER_MANAGED_RUN=1",
	)
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
	DeploymentMode    DeploymentMode

	// Tests inject an httptest TLS endpoint without opening a production
	// configuration path for alternate release mirrors.
	allowTestDownloadBaseURL bool
	// Tests inject deterministic state-write failures without weakening the
	// production state-directory trust boundary.
	beforePersistState func(updaterapi.Operation) error
	// Tests pause staged-tree durability to exercise shutdown and deadline
	// behavior before the non-cancelable activation boundary.
	syncStagedDeployment func(context.Context, string) error
	// Tests observe ownership restoration ordering around preflight-generated
	// artifacts without requiring the test process to run as root.
	preserveDeploymentOwnership func(context.Context, string, string) error
	// Tests pause a trigger after unlocked prechecks but before serialized
	// admission so a completed concurrent upgrade can change the installed version.
	beforeTriggerAdmission func(string)
}

type DeploymentMode string

const (
	DeploymentModeStandalone DeploymentMode = "standalone"
	DeploymentModeHA         DeploymentMode = "ha"
)

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
	selfUpdateReady  chan string
	processLock      *os.File
	logRoot          *os.Root
	closeOnce        sync.Once
	closeErr         error
}

// ensureUpdaterStateDirectory establishes the trust boundary that permits the
// updater to reopen state and downloaded artifacts by pathname. Every path
// component must be owned by root or the current effective user. Group- and
// world-writable components are rejected except for sticky shared ancestors
// (for example /tmp), where an untrusted user cannot replace a trusted-owned
// child. The state directory itself is never allowed to be group- or
// world-writable.
func ensureUpdaterStateDirectory(stateDir string) (string, error) {
	stateDir = filepath.Clean(stateDir)

	existingPath, err := nearestExistingPath(stateDir)
	if err != nil {
		return "", err
	}
	existingIsStateDir := existingPath == stateDir
	if err := validateTrustedUpdaterPath(existingPath, existingIsStateDir); err != nil {
		return "", err
	}
	existingCanonical, err := filepath.EvalSymlinks(existingPath)
	if err != nil {
		return "", fmt.Errorf("resolve existing updater state ancestor: %w", err)
	}
	if err := validateTrustedUpdaterPath(existingCanonical, existingIsStateDir); err != nil {
		return "", err
	}

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create updater state directory: %w", err)
	}
	if err := validateTrustedUpdaterPath(stateDir, true); err != nil {
		return "", err
	}
	canonicalStateDir, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return "", fmt.Errorf("resolve updater state directory: %w", err)
	}
	if err := validateTrustedUpdaterPath(canonicalStateDir, true); err != nil {
		return "", err
	}

	stateInfo, err := os.Lstat(stateDir)
	if err != nil {
		return "", fmt.Errorf("inspect updater state directory: %w", err)
	}
	canonicalInfo, err := os.Lstat(canonicalStateDir)
	if err != nil {
		return "", fmt.Errorf("inspect canonical updater state directory: %w", err)
	}
	if !os.SameFile(stateInfo, canonicalInfo) {
		return "", fmt.Errorf("updater state directory changed while it was validated")
	}
	return canonicalStateDir, nil
}

func nearestExistingPath(path string) (string, error) {
	for candidate := path; ; candidate = filepath.Dir(candidate) {
		_, err := os.Lstat(candidate)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect updater state ancestor %q: %w", candidate, err)
		}
		if parent := filepath.Dir(candidate); parent == candidate {
			return "", fmt.Errorf("find existing updater state ancestor for %q", path)
		}
	}
}

func validateTrustedUpdaterPath(path string, finalIsStateDir bool) error {
	return validateTrustedDirectoryChain(
		path,
		"updater state",
		"updater state directory",
		finalIsStateDir,
		validateTrustedUpdaterOwner,
	)
}

func validateTrustedUpdaterOwner(path string, info os.FileInfo) error {
	uid, err := updaterPathOwnerUID(path, "updater state", info)
	if err != nil {
		return err
	}
	euid := uint32(os.Geteuid()) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	return validateRootOrDaemonPathUID("updater state", path, uid, euid)
}

func pathComponents(path string) []string {
	var reversed []string
	for component := filepath.Clean(path); ; component = filepath.Dir(component) {
		reversed = append(reversed, component)
		parent := filepath.Dir(component)
		if parent == component {
			break
		}
	}
	components := make([]string, len(reversed))
	for index := range reversed {
		components[index] = reversed[len(reversed)-1-index]
	}
	return components
}

type installRootTrust struct {
	adminUID uint32
}

func (trust *installRootTrust) validateOwner(path string, info os.FileInfo) error {
	uid, err := updaterPathOwnerUID(path, "install root", info)
	if err != nil {
		return err
	}
	return trust.validateUID(path, uid)
}

func (trust *installRootTrust) validateUID(path string, uid uint32) error {
	if uid == 0 {
		return nil
	}
	if trust.adminUID == 0 {
		trust.adminUID = uid
		return nil
	}
	if trust.adminUID != uid {
		return fmt.Errorf("install root path component %q is owned by UID %d, but trusted install-admin UID is %d", path, uid, trust.adminUID)
	}
	return nil
}

// ensureTrustedInstallRoot makes the installer-selected deployment owner an
// explicit trust principal. The installer enables this root daemon only when
// that one owner controls the same rootful Docker daemon and is therefore
// already host-administrator-equivalent. Root and at most that single UID may
// own path components; unrelated owners and writable paths fail closed.
func ensureTrustedInstallRoot(installRoot string) (string, error) {
	installRoot = filepath.Clean(installRoot)
	trust := &installRootTrust{}
	if err := validateTrustedDirectoryChain(installRoot, "install root", "install root", true, trust.validateOwner); err != nil {
		return "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(installRoot)
	if err != nil {
		return "", fmt.Errorf("resolve install root: %w", err)
	}
	if err := validateTrustedDirectoryChain(canonicalRoot, "install root", "install root", true, trust.validateOwner); err != nil {
		return "", err
	}
	configuredInfo, err := os.Lstat(installRoot)
	if err != nil {
		return "", fmt.Errorf("inspect install root: %w", err)
	}
	canonicalInfo, err := os.Lstat(canonicalRoot)
	if err != nil {
		return "", fmt.Errorf("inspect canonical install root: %w", err)
	}
	if !os.SameFile(configuredInfo, canonicalInfo) {
		return "", fmt.Errorf("install root changed while it was validated")
	}
	for _, name := range []string{"deployment", "deployment.previous"} {
		deploymentPath := filepath.Join(canonicalRoot, name)
		if _, err := os.Lstat(deploymentPath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("inspect %s directory: %w", name, err)
		}
		label := name + " directory"
		if err := validateTrustedDirectoryChain(deploymentPath, label, label, true, trust.validateOwner); err != nil {
			return "", err
		}
	}
	return canonicalRoot, nil
}

// ensureTrustedSelfUpdatePath is intentionally stricter than InstallRoot. The
// service executable and its path chain must be controlled by root or the
// daemon UID, and later mutations use the validated canonical path.
func ensureTrustedSelfUpdatePath(selfUpdatePath string) (string, error) {
	selfUpdatePath = filepath.Clean(selfUpdatePath)
	configuredInfo, err := os.Lstat(selfUpdatePath)
	if err != nil {
		return "", fmt.Errorf("inspect self-update path: %w", err)
	}
	if err := validateSelfUpdateExecutable(selfUpdatePath, configuredInfo); err != nil {
		return "", err
	}
	canonicalPath, err := ensureTrustedSelfUpdateLocation(selfUpdatePath)
	if err != nil {
		return "", err
	}
	canonicalInfo, err := os.Lstat(canonicalPath)
	if err != nil {
		return "", fmt.Errorf("inspect canonical self-update path: %w", err)
	}
	if err := validateSelfUpdateExecutable(canonicalPath, canonicalInfo); err != nil {
		return "", err
	}
	if !os.SameFile(configuredInfo, canonicalInfo) {
		return "", fmt.Errorf("self-update path changed while it was validated")
	}
	return canonicalPath, nil
}

// ensureTrustedSelfUpdateLocation validates and canonicalizes the protected
// parent chain without requiring the final executable entry to exist. Durable
// handoff recovery uses this narrower primitive to restore a missing entry;
// ordinary startup still uses ensureTrustedSelfUpdatePath above.
func ensureTrustedSelfUpdateLocation(selfUpdatePath string) (string, error) {
	selfUpdatePath = filepath.Clean(selfUpdatePath)
	configuredParent := filepath.Dir(selfUpdatePath)
	if err := validateTrustedDirectoryChain(configuredParent, "self-update path", "", false, validateDaemonPathOwner); err != nil {
		return "", err
	}
	canonicalParent, err := filepath.EvalSymlinks(configuredParent)
	if err != nil {
		return "", fmt.Errorf("resolve self-update parent: %w", err)
	}
	if err := validateTrustedDirectoryChain(canonicalParent, "self-update path", "", false, validateDaemonPathOwner); err != nil {
		return "", err
	}
	return filepath.Join(canonicalParent, filepath.Base(selfUpdatePath)), nil
}

func validateTrustedDirectoryChain(
	path string,
	pathName string,
	finalName string,
	protectFinal bool,
	validateOwner func(string, os.FileInfo) error,
) error {
	components := pathComponents(path)
	for index, component := range components {
		info, err := os.Lstat(component)
		if err != nil {
			return fmt.Errorf("inspect %s path component %q: %w", pathName, component, err)
		}
		isFinal := index == len(components)-1
		if info.Mode()&os.ModeSymlink != 0 {
			if protectFinal && isFinal {
				return fmt.Errorf("%s must not be a symlink: %q", finalName, component)
			}
			if err := validateOwner(component, info); err != nil {
				return err
			}
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf("%s path component is not a directory: %q", pathName, component)
		}
		if err := validateOwner(component, info); err != nil {
			return err
		}
		if info.Mode().Perm()&0o022 == 0 {
			continue
		}
		if protectFinal && isFinal {
			return fmt.Errorf("%s must not be group- or world-writable: %q", finalName, component)
		}
		if info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("%s ancestor is group- or world-writable without the sticky bit: %q", pathName, component)
		}
	}
	return nil
}

func updaterPathOwnerUID(path, name string, info os.FileInfo) (uint32, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("inspect owner of %s path component %q", name, path)
	}
	return stat.Uid, nil
}

func validateDaemonPathUID(path string, ownerUID, daemonUID uint32) error {
	return validateRootOrDaemonPathUID("self-update", path, ownerUID, daemonUID)
}

func validateRootOrDaemonPathUID(pathName, path string, ownerUID, daemonUID uint32) error {
	if ownerUID != 0 && ownerUID != daemonUID {
		return fmt.Errorf("%s path component %q is owned by untrusted UID %d", pathName, path, ownerUID)
	}
	return nil
}

func validateDaemonPathOwner(path string, info os.FileInfo) error {
	uid, err := updaterPathOwnerUID(path, "self-update path", info)
	if err != nil {
		return err
	}
	return validateDaemonPathUID(path, uid, uint32(os.Geteuid())) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
}

func validateSelfUpdateExecutable(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("self-update path must not be a symlink: %q", path)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("self-update path must be a non-empty executable regular file: %q", path)
	}
	if err := validateDaemonPathOwner(path, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("self-update path must not be group- or world-writable: %q", path)
	}
	return nil
}

func openOperationLogRoot(stateDir string) (*os.Root, error) {
	stateRoot, err := os.OpenRoot(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open updater state root: %w", err)
	}
	defer stateRoot.Close()

	logInfo, err := stateRoot.Lstat("logs")
	if errors.Is(err, os.ErrNotExist) {
		if err := stateRoot.Mkdir("logs", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create updater log directory: %w", err)
		}
		logInfo, err = stateRoot.Lstat("logs")
	}
	if err != nil {
		return nil, fmt.Errorf("inspect updater log directory: %w", err)
	}
	if !logInfo.IsDir() {
		return nil, fmt.Errorf("updater log path is not a directory")
	}
	logRoot, err := stateRoot.OpenRoot("logs")
	if err != nil {
		return nil, fmt.Errorf("open confined updater log directory: %w", err)
	}
	logDirectory, err := logRoot.Open(".")
	if err != nil {
		_ = logRoot.Close()
		return nil, fmt.Errorf("open updater log directory handle: %w", err)
	}
	openedInfo, statErr := logDirectory.Stat()
	chmodErr := logDirectory.Chmod(0o700)
	closeErr := logDirectory.Close()
	if statErr != nil || chmodErr != nil || closeErr != nil {
		_ = logRoot.Close()
		return nil, errors.Join(
			wrapIfError("inspect opened updater log directory", statErr),
			wrapIfError("secure updater log directory", chmodErr),
			wrapIfError("close updater log directory handle", closeErr),
		)
	}
	if !os.SameFile(logInfo, openedInfo) {
		_ = logRoot.Close()
		return nil, fmt.Errorf("updater log directory changed while it was opened")
	}
	return logRoot, nil
}

func wrapIfError(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

func NewManager(cfg Config) (*Manager, error) {
	return newManager(cfg)
}

// RepairStartup restores crash-interrupted updater and deployment state before HA starts.
func RepairStartup(cfg Config) error {
	canonicalStateDir, err := ensureUpdaterStateDirectory(cfg.StateDir)
	if err != nil {
		return err
	}
	processLock, err := acquireProcessLock(canonicalStateDir)
	if err != nil {
		return err
	}
	prepareErr := prepareSelfUpdateRepair(cfg.SelfUpdatePath)
	interruptedSelfUpdate := errors.Is(prepareErr, ErrInterruptedSelfUpdateRestored)
	if prepareErr != nil && !interruptedSelfUpdate && !errors.Is(prepareErr, errRetriedSelfUpdateRestored) {
		return errors.Join(prepareErr, processLock.Close())
	}
	if err := processLock.Close(); err != nil {
		return fmt.Errorf("release updater repair lock: %w", err)
	}
	cfg.StateDir = canonicalStateDir
	manager, err := newManager(cfg)
	if err != nil {
		return err
	}
	convergeUpdater := interruptedSelfUpdate || (prepareErr == nil && manager.cfg.DeploymentMode == DeploymentModeHA)
	if manager.cfg.SelfUpdatePath != "" && convergeUpdater {
		matches, matchErr := manager.updaterMatchesInstalledDeployment()
		if matchErr != nil {
			err = matchErr
		} else if !matches {
			err = manager.restoreUpdaterFromInstalledDeployment()
		}
	}
	return errors.Join(err, manager.Close())
}

func newManager(cfg Config) (*Manager, error) {
	if !filepath.IsAbs(cfg.InstallRoot) {
		return nil, fmt.Errorf("install root must be absolute")
	}
	if !filepath.IsAbs(cfg.StateDir) {
		return nil, fmt.Errorf("state directory must be absolute")
	}
	if cfg.SelfUpdatePath != "" && !filepath.IsAbs(cfg.SelfUpdatePath) {
		return nil, fmt.Errorf("self-update path must be absolute")
	}
	if cfg.DeploymentMode == "" {
		cfg.DeploymentMode = DeploymentModeStandalone
	}
	if cfg.DeploymentMode != DeploymentModeStandalone && cfg.DeploymentMode != DeploymentModeHA {
		return nil, fmt.Errorf("deployment mode must be standalone or ha")
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
	if cfg.syncStagedDeployment == nil {
		cfg.syncStagedDeployment = syncStagedDeployment
	}
	if cfg.preserveDeploymentOwnership == nil {
		cfg.preserveDeploymentOwnership = preserveDeploymentOwnership
	}
	if cfg.PreflightTimeout < 0 || cfg.ActivationTimeout < 0 || cfg.CleanupTimeout < 0 {
		return nil, fmt.Errorf("updater phase timeouts must be positive")
	}
	canonicalInstallRoot, err := ensureTrustedInstallRoot(cfg.InstallRoot)
	if err != nil {
		return nil, err
	}
	cfg.InstallRoot = canonicalInstallRoot
	if cfg.SelfUpdatePath != "" {
		canonicalSelfUpdatePath, err := ensureTrustedSelfUpdatePath(cfg.SelfUpdatePath)
		if err != nil {
			return nil, err
		}
		cfg.SelfUpdatePath = canonicalSelfUpdatePath
	}
	canonicalStateDir, err := ensureUpdaterStateDirectory(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	cfg.StateDir = canonicalStateDir
	processLock, err := acquireProcessLock(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	logRoot, err := openOperationLogRoot(cfg.StateDir)
	if err != nil {
		_ = processLock.Close()
		return nil, err
	}

	m := &Manager{
		cfg:             cfg,
		selfUpdateReady: make(chan string, 1),
		processLock:     processLock,
		logRoot:         logRoot,
	}
	if err := m.loadState(); err != nil {
		_ = processLock.Close()
		_ = logRoot.Close()
		return nil, err
	}
	if err := m.cleanupStaleArtifacts(); err != nil {
		_ = processLock.Close()
		_ = logRoot.Close()
		return nil, err
	}
	protectedLogName := ""
	if m.operation != nil {
		protectedLogName = operationLogFilename(m.operation.ID, m.operation.StartedAt)
	}
	if err := m.pruneOperationLogs(protectedLogName, 0, 0); err != nil {
		_ = processLock.Close()
		_ = logRoot.Close()
		return nil, fmt.Errorf("prune updater operation logs: %w", err)
	}
	return m, nil
}

func (m *Manager) updaterMatchesInstalledDeployment() (bool, error) {
	running, err := os.ReadFile(m.cfg.SelfUpdatePath)
	if err != nil {
		return false, fmt.Errorf("read supervised updater for recovery: %w", err)
	}
	installed, err := os.ReadFile(filepath.Join(m.cfg.InstallRoot, "deployment", "updater", "proto-fleet-updater"))
	if err != nil {
		return false, fmt.Errorf("read installed updater for recovery: %w", err)
	}
	return bytes.Equal(running, installed), nil
}

func (m *Manager) restoreUpdaterFromInstalledDeployment() error {
	targetVersion, err := readInstalledVersion(filepath.Join(m.cfg.InstallRoot, "deployment", "version.txt"))
	if err != nil {
		return fmt.Errorf("read installed version for updater recovery: %w", err)
	}
	updaterPath := filepath.Join(m.cfg.InstallRoot, "deployment", "updater", "proto-fleet-updater")
	fd, err := syscall.Open(updaterPath, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open installed updater for recovery: %w", err)
	}
	installedUpdater := os.NewFile(uintptr(fd), updaterPath)
	if installedUpdater == nil {
		_ = syscall.Close(fd)
		return fmt.Errorf("open installed updater for recovery")
	}
	defer installedUpdater.Close()

	if err := m.refreshSelfUpdater(context.Background(), installedUpdater, m.cfg.SelfUpdatePath, targetVersion); err != nil {
		return fmt.Errorf("restore updater from installed deployment: %w", err)
	}
	// The daemon gets one supervised retry and retains rollback eligibility
	// until it binds the production socket.
	if err := authorizeSelfUpdateRestart(m.cfg.SelfUpdatePath); err != nil {
		return fmt.Errorf("authorize recovered updater startup: %w", err)
	}
	return nil
}

// RecoverApplication restarts an application left stopped by an interrupted HA update.
func (m *Manager) RecoverApplication() error {
	if m.operation == nil || !m.operation.RecoveryPending || m.operation.RecoveryCommand == "" {
		return nil
	}
	deployment := filepath.Join(m.cfg.InstallRoot, "deployment")
	version, err := readInstalledVersion(filepath.Join(deployment, "version.txt"))
	if err != nil {
		return fmt.Errorf("read interrupted HA deployment version: %w", err)
	}
	if err := m.runHACommand(context.Background(), m.cfg.ActivationTimeout, deployment, io.Discard, "app-start", version, "any"); err != nil {
		recoveryErr := fmt.Errorf("restart interrupted HA application: %w", err)
		now := m.terminalOutcomeCutoff(*m.operation)
		advanceOutcomeRevision(m.operation)
		m.operation.Phase = updaterapi.PhaseFailed
		m.operation.Message = "HA application recovery failed"
		m.operation.Error = recoveryErr.Error()
		m.operation.UpdatedAt = now
		m.operation.CompletedAt = &now
		if persistErr := m.persistLocked(); persistErr != nil {
			return errors.Join(recoveryErr, fmt.Errorf("persist HA application recovery failure: %w", persistErr))
		}
		return recoveryErr
	}
	now := m.terminalOutcomeCutoff(*m.operation)
	advanceOutcomeRevision(m.operation)
	m.operation.RecoveryCommand = ""
	m.operation.RecoveryPending = false
	m.operation.Message += "; HA application restarted"
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	return m.persistLocked()
}

func (m *Manager) Close() error {
	return m.Shutdown(context.Background())
}

// SelfUpdateReady yields the validated canonical executable path once the
// replacement and successful terminal state are both durable. The command
// process owns listener draining, lock release, and exec so the manager remains
// independent of any supervisor.
func (m *Manager) SelfUpdateReady() <-chan string {
	return m.selfUpdateReady
}

// RollbackSelfUpdate atomically restores the executable that was retained
// before the latest self-refresh. The command process calls this only when
// exec of the already smoke-tested replacement unexpectedly fails.
func (m *Manager) RollbackSelfUpdate() error {
	if m.cfg.SelfUpdatePath == "" {
		return fmt.Errorf("self-update path is not configured")
	}
	return rollbackPendingSelfUpdate(m.cfg.SelfUpdatePath)
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
			var closeErrors []error
			if m.logRoot != nil {
				closeErrors = append(closeErrors, m.logRoot.Close())
			}
			if m.processLock != nil {
				closeErrors = append(closeErrors, m.processLock.Close())
			}
			m.closeErr = errors.Join(closeErrors...)
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
	if m.operation == nil {
		m.mu.RUnlock()
		return updaterapi.StatusResponse{}
	}
	snapshot := *m.operation
	m.mu.RUnlock()
	if m.failureWasRemediated(snapshot) {
		// The deployment proof is already durable, so derive the effective
		// acknowledgement on the snapshot rather than turning a status read into
		// a state mutation. The proof is re-evaluated after daemon restarts.
		snapshot.Acknowledged = true
	}
	return updaterapi.StatusResponse{Operation: &snapshot}
}

// failureWasRemediated auto-dismisses a failed operation once the
// deployment has reached the failed target version (or newer) by other means:
// a manual install, a successful run of the recovery command, or a later
// upgrade. Without this, a failure the operator already fixed keeps
// resurfacing until someone dismisses it by hand.
//
// Remediation must be proven positively: run-fleet.sh writes the startup
// proof only at the end of a run that brought Fleet up, so a proof newer
// than the failure means the deployment at the target version actually
// started. Mere absence of failure artifacts is not enough — a failed
// recovery rerun can consume the preflight marker while the deployment still
// names the failed target, and the failure is then still live. This check is
// deliberately read-only because both HTTP and Connect expose status as a
// no-side-effect operation.
func (m *Manager) failureWasRemediated(operation updaterapi.Operation) bool {
	if operation.Phase != updaterapi.PhaseFailed || operation.Acknowledged || operation.CompletedAt == nil {
		return false
	}
	deployment := filepath.Join(m.cfg.InstallRoot, "deployment")
	installed, err := readInstalledVersion(filepath.Join(deployment, "version.txt"))
	if err != nil || !semver.IsValid(installed) || semver.Compare(installed, operation.TargetVersion) < 0 {
		return false
	}
	proof, err := os.Lstat(filepath.Join(deployment, startupProofFilename))
	return err == nil && proof.Mode().IsRegular() && proof.ModTime().After(*operation.CompletedAt)
}

// Acknowledge durably records that an operator dismissed a terminal operation
// outcome. It is idempotent for the same operation incarnation and outcome
// revision and fails when any identity is stale or the operation is still
// running, so a stale dismissal can never suppress a later operation that
// deliberately reuses the caller-supplied ID or rewritten recovery guidance.
// The boolean reports whether the dismissal was already in place, letting
// callers retry after ambiguous transport results without double-recording the
// action.
func (m *Manager) Acknowledge(
	operationID string,
	expectedStartedAt time.Time,
	expectedOutcomeRevision uint64,
) (updaterapi.Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if expectedStartedAt.IsZero() {
		return updaterapi.Operation{}, false, newTriggerError(
			errAcknowledgeStartedAtRequired,
			"expected operation start time is required",
		)
	}
	if expectedOutcomeRevision == 0 {
		return updaterapi.Operation{}, false, newTriggerError(
			errAcknowledgeRevisionRequired,
			"expected outcome revision must be greater than zero",
		)
	}
	if m.operation == nil || m.operation.ID != operationID {
		return updaterapi.Operation{}, false, newTriggerError(
			errAcknowledgeUnknown,
			fmt.Sprintf("operation %s is not the current updater operation", operationID),
		)
	}
	if !m.operation.StartedAt.Equal(expectedStartedAt) {
		return updaterapi.Operation{}, false, newTriggerError(
			errAcknowledgeStartedAtMismatch,
			fmt.Sprintf(
				"operation %s start time changed from %s to %s",
				operationID,
				expectedStartedAt.UTC().Format(time.RFC3339Nano),
				m.operation.StartedAt.UTC().Format(time.RFC3339Nano),
			),
		)
	}
	if !m.operation.Phase.Terminal() {
		return updaterapi.Operation{}, false, newTriggerError(
			errAcknowledgeActive,
			fmt.Sprintf("operation %s has not finished", operationID),
		)
	}
	if m.operation.OutcomeRevision != expectedOutcomeRevision {
		return updaterapi.Operation{}, false, newTriggerError(
			errAcknowledgeRevisionMismatch,
			fmt.Sprintf(
				"operation %s outcome revision changed from %d to %d",
				operationID,
				expectedOutcomeRevision,
				m.operation.OutcomeRevision,
			),
		)
	}
	if m.operation.Acknowledged {
		return *m.operation, true, nil
	}
	m.operation.Acknowledged = true
	if err := m.persistLocked(); err != nil {
		// An unpersisted acknowledgement would silently reappear after the next
		// updater restart, so report the failure instead of a partial success.
		m.operation.Acknowledged = false
		return updaterapi.Operation{}, false, fmt.Errorf("persist acknowledged operation: %w", err)
	}
	return *m.operation, false, nil
}

func (m *Manager) Trigger(targetVersion string) (updaterapi.Operation, error) {
	if !isCanonicalRelease(targetVersion) {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerInvalid,
			"target version must be a valid semantic release tag",
		)
	}
	if m.cfg.NewID == nil {
		return updaterapi.Operation{}, fmt.Errorf("generate updater operation id: generator is not configured")
	}
	return m.trigger(targetVersion, m.cfg.NewID(), false, false)
}

// TriggerWithID accepts a caller-generated UUID so a client that loses the
// HTTP response can reconcile the durable operation through Status without
// guessing by target or time. Reusing the ID for the same target is
// idempotent; reusing it for another target is rejected.
func (m *Manager) TriggerWithID(targetVersion, operationID string) (updaterapi.Operation, error) {
	return m.triggerWithID(targetVersion, operationID, false)
}

func (m *Manager) TriggerCompleteWithID(targetVersion, operationID string) (updaterapi.Operation, error) {
	return m.triggerWithID(targetVersion, operationID, true)
}

func (m *Manager) triggerWithID(targetVersion, operationID string, complete bool) (updaterapi.Operation, error) {
	parsedID, err := uuid.Parse(operationID)
	if err != nil || parsedID == uuid.Nil || parsedID.String() != operationID {
		return updaterapi.Operation{}, newTriggerError(errTriggerInvalid, "operation id must be a canonical UUID")
	}
	return m.trigger(targetVersion, operationID, true, complete)
}

func (m *Manager) trigger(targetVersion, operationID string, idempotent, complete bool) (updaterapi.Operation, error) {
	if operationID == "" {
		return updaterapi.Operation{}, fmt.Errorf("generate updater operation id: empty value")
	}
	if !isCanonicalRelease(targetVersion) {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerInvalid,
			"target version must be a valid semantic release tag",
		)
	}
	if complete && m.cfg.DeploymentMode != DeploymentModeHA {
		return updaterapi.Operation{}, newTriggerError(errTriggerPrecondition, "completion updates require HA deployment mode")
	}
	// Check before host-state validation so a retry can recover the original
	// response even after that operation has completed and changed the
	// installed version. The write-locked check below closes the race with a
	// first request that is still being admitted.
	m.mu.RLock()
	if idempotent && m.operation != nil && m.operation.ID == operationID {
		existing := *m.operation
		m.mu.RUnlock()
		if existing.TargetVersion != targetVersion || existing.Complete != complete {
			return updaterapi.Operation{}, newTriggerError(
				errTriggerInvalid,
				"operation id is already associated with another update",
			)
		}
		return existing, nil
	}
	if m.operation != nil && m.operation.RecoveryPending {
		recoveryOperationID := m.operation.ID
		m.mu.RUnlock()
		return updaterapi.Operation{}, newTriggerError(
			errTriggerBusy,
			fmt.Sprintf("HA application recovery for operation %s is pending; restart proto-fleet-updater.service to retry recovery", recoveryOperationID),
		)
	}
	m.mu.RUnlock()
	marker, err := m.readActivationMarker()
	if err != nil {
		return updaterapi.Operation{}, fmt.Errorf("inspect pending activation recovery: %w", err)
	}
	if marker != nil {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerBusy,
			fmt.Sprintf(
				"activation recovery for operation %s is pending; restart the updater after completing recovery",
				marker.OperationID,
			),
		)
	}
	if m.cfg.beforeTriggerAdmission != nil {
		m.cfg.beforeTriggerAdmission(targetVersion)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if idempotent && m.operation != nil && m.operation.ID == operationID {
		if m.operation.TargetVersion != targetVersion || m.operation.Complete != complete {
			return updaterapi.Operation{}, newTriggerError(
				errTriggerInvalid,
				"operation id is already associated with another update",
			)
		}
		return *m.operation, nil
	}
	if m.operation != nil && m.operation.RecoveryPending {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerBusy,
			fmt.Sprintf("HA application recovery for operation %s is pending; restart proto-fleet-updater.service to retry recovery", m.operation.ID),
		)
	}
	if m.closing {
		return updaterapi.Operation{}, errTriggerClosing
	}
	if m.operation != nil && !m.operation.Phase.Terminal() {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerBusy,
			fmt.Sprintf("an upgrade to %s is already in progress", m.operation.TargetVersion),
		)
	}
	if m.operationRunning {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerBusy,
			"the previous upgrade is still completing cleanup",
		)
	}
	// The installed deployment can change while an earlier trigger is running.
	// Validate the monotonic-version precondition while admission is serialized,
	// after the prior worker has fully finished and before this one is accepted.
	currentVersion, err := readInstalledVersion(filepath.Join(m.cfg.InstallRoot, "deployment", "version.txt"))
	if err != nil {
		return updaterapi.Operation{}, err
	}
	if !semver.IsValid(currentVersion) {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerPrecondition,
			fmt.Sprintf("installed version %q is not upgradeable", currentVersion),
		)
	}
	if semver.Compare(targetVersion, currentVersion) <= 0 {
		return updaterapi.Operation{}, newTriggerError(
			errTriggerPrecondition,
			fmt.Sprintf("target version %s must be newer than installed version %s", targetVersion, currentVersion),
		)
	}
	if err := m.cleanupStaleArtifacts(); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("clean stale upgrade artifacts: %w", err)
	}
	protectedLogName := ""
	if m.operation != nil {
		protectedLogName = operationLogFilename(m.operation.ID, m.operation.StartedAt)
	}
	if err := m.pruneOperationLogs(protectedLogName, 1, maxCommandLogBytes); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("reserve updater operation log capacity: %w", err)
	}
	now := m.cfg.Now().UTC()
	// StartedAt is part of the acknowledgement compare-and-set identity. Keep
	// each freshly admitted incarnation strictly newer than the one retained in
	// state even when the host clock is coarse, frozen, or moves backward.
	if m.operation != nil && !now.After(m.operation.StartedAt) {
		now = m.operation.StartedAt.Add(time.Nanosecond).UTC()
	}
	op := &updaterapi.Operation{
		ID:            operationID,
		TargetVersion: targetVersion,
		Complete:      complete,
		Phase:         updaterapi.PhaseQueued,
		Message:       "Upgrade queued",
		StartedAt:     now,
		UpdatedAt:     now,
	}
	previousOperation := m.operation
	m.operation = op
	if err := m.persistLocked(); err != nil {
		m.operation = previousOperation
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
		m.run(operationCtx, operationCopy.ID, operationCopy.StartedAt, targetVersion, complete)
	}()
	return operationCopy, nil
}

func isCanonicalRelease(version string) bool {
	return semver.IsValid(version) && semver.Canonical(version) == version
}

func (m *Manager) finishOperation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operationRunning = false
	m.cancelOperation = nil
}

func (m *Manager) run(ctx context.Context, operationID string, startedAt time.Time, targetVersion string, complete bool) {
	logName := operationLogFilename(operationID, startedAt)
	logPath := filepath.Join(m.cfg.StateDir, "logs", logName)
	logFile, err := m.logRoot.OpenFile(logName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		m.fail(operationID, fmt.Errorf("open upgrade log: %w", err), "")
		return
	}
	defer func() {
		operation := m.Status().Operation
		if operation != nil && operation.ID == operationID && operation.Phase.Terminal() {
			_, _ = fmt.Fprintf(
				logFile,
				"[%s] terminal phase=%s error=%q\n",
				m.cfg.Now().UTC().Format(time.RFC3339),
				operation.Phase,
				operation.Error,
			)
		}
		if err := logFile.Close(); err != nil {
			log.Printf("close updater operation log: %v", err)
		}
		// Terminal state is already persisted by this point. Retention is best
		// effort here so a filesystem cleanup problem cannot rewrite an upgrade
		// outcome after Fleet has already been activated (or failed safely).
		if err := m.pruneOperationLogs(logName, 0, 0); err != nil {
			log.Printf("prune updater operation logs after completion: %v", err)
		}
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
	previousVersion := ""
	if m.cfg.DeploymentMode == DeploymentModeHA {
		previousVersion, err = readInstalledVersion(filepath.Join(currentDeployment, "version.txt"))
		if err != nil {
			m.fail(operationID, fmt.Errorf("read current HA application version: %w", err), recovery)
			return
		}
	}
	if err := preserveDeploymentState(ctx, currentDeployment, stageDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("preserve deployment configuration: %w", err), recovery)
		return
	}
	if err := m.advance(operationID, updaterapi.PhasePreflight, "Building and validating the new stack while Fleet stays online"); err != nil {
		m.fail(operationID, fmt.Errorf("persist preflight phase: %w", err), recovery)
		return
	}
	if err := m.runPreflight(ctx, stageDeployment, commandOutput); err != nil {
		if ctx.Err() == nil {
			m.removeFailedPreflightImages(ctx, stageDeployment, commandOutput, logFile, targetVersion)
		}
		m.fail(operationID, fmt.Errorf("upgrade preflight failed: %w", err), recovery)
		return
	}
	// Preflight runs as the root updater and creates the proof consumed by
	// --skip-build. Restore the deployment owner after preflight so that proof,
	// along with any other generated state, remains usable by the supported
	// non-root deployment owner during manual recovery.
	if err := m.cfg.preserveDeploymentOwnership(ctx, currentDeployment, stageDeployment); err != nil {
		m.fail(operationID, fmt.Errorf("preserve deployment ownership: %w", err), recovery)
		return
	}
	if err := m.advance(operationID, updaterapi.PhasePreflight, "Persisting the staged release before activation"); err != nil {
		m.fail(operationID, fmt.Errorf("persist staged-release durability phase: %w", err), recovery)
		return
	}
	syncCtx, cancelSync := context.WithTimeout(ctx, m.cfg.ActivationTimeout)
	syncErr := m.cfg.syncStagedDeployment(syncCtx, stageDeployment)
	if syncErr == nil {
		syncErr = syncCtx.Err()
	}
	cancelSync()
	if syncErr != nil {
		m.fail(operationID, fmt.Errorf("persist staged deployment before activation: %w", syncErr), recovery)
		return
	}

	requiredRole := "passive"
	if m.cfg.DeploymentMode == DeploymentModeHA {
		requireArgs := []string{"require-passive", haNodeEnvPath, targetVersion}
		if complete {
			requiredRole = "active"
			requireArgs = []string{"require-active", haNodeEnvPath, targetVersion}
		}
		if err := m.runHACommand(ctx, m.cfg.ActivationTimeout, currentDeployment, commandOutput, requireArgs...); err != nil {
			m.fail(operationID, fmt.Errorf("local Fleet is not %s immediately before activation: %w", requiredRole, err), recovery)
			return
		}
	}
	if err := m.beginActivation(operationID, "Restarting the local Fleet application"); err != nil {
		m.fail(operationID, fmt.Errorf("persist activation phase: %w", err), recovery)
		return
	}
	activationCtx, cancelActivation := context.WithTimeout(ctx, m.cfg.ActivationTimeout)
	defer cancelActivation()
	backupDeployment := filepath.Join(m.cfg.InstallRoot, "deployment.previous")
	if m.cfg.DeploymentMode == DeploymentModeHA {
		recovery = m.activationRecoveryCommand(currentDeployment, previousVersion)
		if err := m.setRecoveryCommand(operationID, recovery); err != nil {
			m.failActivation(operationID, targetVersion, fmt.Errorf("persist passive application recovery: %w", err), logFile, false)
			return
		}
		if complete {
			if err := m.waitForQualificationBarrier(activationCtx, qualificationBeforeStopBarrierName); err != nil {
				m.fail(operationID, errors.Join(err, m.clearActivationMarker()), "")
				return
			}
		}
		if err := m.runHACommand(activationCtx, m.cfg.ActivationTimeout, currentDeployment, commandOutput, "app-stop", requiredRole); err != nil {
			if complete {
				restartErr := m.restartHAApplication(ctx, currentDeployment, previousVersion, commandOutput)
				if restartErr == nil {
					m.fail(operationID, fmt.Errorf("stop HA application failed; previous release restarted: %w", err), "")
					return
				}
				m.failPendingRecovery(operationID, errors.Join(
					fmt.Errorf("stop HA application: %w", err),
					fmt.Errorf("restart previous release: %w", restartErr),
				), recovery)
				return
			}
			m.failActivation(operationID, targetVersion, fmt.Errorf("stop HA application: %w", err), logFile, true)
			return
		}
		if complete {
			if err := m.waitForQualificationBarrier(activationCtx, qualificationAfterStopBarrierName); err != nil {
				restartErr := m.restartHAApplication(ctx, currentDeployment, previousVersion, commandOutput)
				if restartErr == nil {
					m.fail(operationID, fmt.Errorf("post-stop qualification pause failed; previous release restarted: %w", err), "")
					return
				}
				m.failPendingRecovery(operationID, errors.Join(err, restartErr), recovery)
				return
			}
			takeoverCtx, cancelTakeover := context.WithTimeout(activationCtx, ha.UpdateTakeoverTimeout)
			err := m.runHACommand(takeoverCtx, ha.UpdateTakeoverTimeout, currentDeployment, commandOutput, "wait-takeover", targetVersion)
			cancelTakeover()
			if err != nil {
				restartErr := m.restartHAApplication(ctx, currentDeployment, previousVersion, commandOutput)
				if restartErr == nil {
					m.fail(operationID, fmt.Errorf("updated peer did not take over; previous release restarted: %w", err), "")
					return
				}
				m.failPendingRecovery(operationID, errors.Join(err, restartErr), m.activationRecoveryCommand(currentDeployment, previousVersion))
				return
			}
		}
	}
	betweenRenames := func() error { return nil }
	if m.cfg.DeploymentMode == DeploymentModeHA {
		betweenRenames = func() error {
			return m.waitForQualificationBarrier(activationCtx, qualificationBetweenRenamesBarrierName)
		}
	}
	if err := activateDeployment(stageDeployment, currentDeployment, backupDeployment, betweenRenames); err != nil {
		m.failActivation(operationID, targetVersion, err, logFile, m.cfg.DeploymentMode == DeploymentModeHA)
		return
	}
	recovery = m.activationRecoveryCommand(currentDeployment, targetVersion)
	if err := m.setRecoveryCommand(operationID, recovery); err != nil {
		activationErr := fmt.Errorf("persist activation recovery command: %w", err)
		if m.cfg.DeploymentMode == DeploymentModeHA {
			m.failActivation(operationID, targetVersion, activationErr, logFile, true)
		} else {
			m.fail(operationID, activationErr, recovery)
		}
		return
	}
	if err := m.clearActivationMarker(); err != nil {
		m.failActivation(operationID, targetVersion, fmt.Errorf("complete activation swap: %w", err), logFile, m.cfg.DeploymentMode == DeploymentModeHA)
		return
	}

	if err := m.runActivation(activationCtx, currentDeployment, targetVersion, complete, commandOutput); err != nil {
		activationErr := fmt.Errorf("new stack failed to start: %w", err)
		// Migrations may already have run, so keep the new deployment active for
		// forward recovery instead of starting an older binary against its schema.
		failureRecovery := m.activationRecoveryCommand(currentDeployment, targetVersion)
		var recoveryErr error
		if m.cfg.DeploymentMode == DeploymentModeStandalone {
			failureRecovery, recoveryErr = activationRecoveryCommandAfterFailure(currentDeployment)
		}
		if recoveryErr != nil {
			activationErr = errors.Join(
				activationErr,
				fmt.Errorf("derive activation recovery command: %w", recoveryErr),
			)
			failureRecovery = ""
		}
		m.fail(operationID, activationErr, failureRecovery)
		return
	}
	successMessage := fmt.Sprintf("Fleet %s is running", targetVersion)
	selfUpdateSucceeded := false
	if preparedUpdater != nil {
		if err := m.refreshSelfUpdater(ctx, preparedUpdater, m.cfg.SelfUpdatePath, targetVersion); err != nil {
			_, _ = fmt.Fprintf(logFile, "[%s] warning: Fleet is healthy, but the host updater binary was not refreshed: %v\n", m.cfg.Now().UTC().Format(time.RFC3339), err)
			successMessage += "; host updater refresh needs attention (see upgrade log)"
		} else {
			selfUpdateSucceeded = true
		}
	}

	if err := m.succeed(operationID, successMessage, selfUpdateSucceeded); err != nil {
		_, _ = fmt.Fprintf(
			logFile,
			"[%s] error: Fleet is running, but updater completion was not persisted; further upgrades remain blocked until startup reconciliation: %v\n",
			m.cfg.Now().UTC().Format(time.RFC3339),
			err,
		)
		log.Printf("persist successful upgrade state; further upgrades remain blocked until startup reconciliation: %v", err)
		return
	}
	if selfUpdateSucceeded {
		select {
		case m.selfUpdateReady <- m.cfg.SelfUpdatePath:
		default:
		}
	}
	_, _ = fmt.Fprintf(logFile, "[%s] upgrade completed\n", m.cfg.Now().UTC().Format(time.RFC3339))
}

func (m *Manager) restartHAApplication(
	parent context.Context,
	deployment string,
	version string,
	output io.Writer,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), m.cfg.ActivationTimeout)
	defer cancel()
	if err := m.runHACommand(ctx, m.cfg.ActivationTimeout, deployment, output, "app-start", version, "any"); err != nil {
		return err
	}
	return m.clearActivationMarker()
}

// Root-created barriers let exact release qualification pause at otherwise
// unobservable crash windows. Normal hosts never create them and take the fast path.
func (m *Manager) waitForQualificationBarrier(ctx context.Context, name string) error {
	path := filepath.Join(m.cfg.StateDir, name)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("inspect HA update qualification barrier: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for HA update qualification barrier: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *Manager) runPreflight(ctx context.Context, deployment string, output io.Writer) error {
	if m.cfg.DeploymentMode == DeploymentModeHA {
		return m.runHACommand(ctx, m.cfg.PreflightTimeout, deployment, output, "update-preflight")
	}
	return m.runCommand(ctx, m.cfg.PreflightTimeout, deployment, output, "/bin/bash", "./run-fleet.sh", "--non-interactive", "--preflight-only")
}

func (m *Manager) runActivation(ctx context.Context, deployment, targetVersion string, complete bool, output io.Writer) error {
	if m.cfg.DeploymentMode == DeploymentModeHA {
		mode := "passive"
		if complete {
			mode = "complete"
		}
		return m.runHACommand(ctx, m.cfg.ActivationTimeout, deployment, output, "app-start", targetVersion, mode)
	}
	return m.runCommand(ctx, m.cfg.ActivationTimeout, deployment, output, "/bin/bash", "./run-fleet.sh", "--non-interactive", "--skip-build")
}

func (m *Manager) runHACommand(ctx context.Context, timeout time.Duration, deployment string, output io.Writer, args ...string) error {
	return m.runCommand(ctx, timeout, deployment, output, filepath.Join(deployment, "ha", "fleet-ha"), args...)
}

func (m *Manager) activationRecoveryCommand(deployment, targetVersion string) string {
	if m.cfg.DeploymentMode == DeploymentModeHA {
		return fmt.Sprintf("sudo -- %s app-start %s any", shellQuote(filepath.Join(deployment, "ha", "fleet-ha")), shellQuote(targetVersion))
	}
	return activationRecoveryCommand(deployment)
}

func operationArtifactBase(operationID string) string {
	digest := sha256.Sum256([]byte(operationID))
	return operationArtifactPrefix + hex.EncodeToString(digest[:])
}

func operationLogFilename(operationID string, startedAt time.Time) string {
	// Keep the externally visible name in the same fixed, SHA-derived format
	// while making the retained file unique to the host-owned operation
	// incarnation. The NUL separator makes the two variable-length identity
	// components unambiguous without exposing either one in the filesystem.
	identity := operationID + "\x00" + startedAt.UTC().Format(time.RFC3339Nano)
	return operationArtifactBase(identity) + ".log"
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
	if marker.OperationID == "" || !isCanonicalRelease(marker.TargetVersion) {
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
	restartHA bool,
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
	} else if restartHA {
		current := filepath.Join(m.cfg.InstallRoot, "deployment")
		version, err := readInstalledVersion(filepath.Join(current, "version.txt"))
		if err == nil {
			err = m.runHACommand(context.Background(), m.cfg.ActivationTimeout, current, logOutput, "app-start", version, "any")
		}
		if err != nil {
			activationErr = errors.Join(activationErr, fmt.Errorf("restart HA application after failed activation: %w", err))
		} else {
			layout.RecoveryCommand = ""
		}
	}
	if restartHA && layout.RecoveryCommand != "" {
		m.failPendingRecovery(operationID, activationErr, layout.RecoveryCommand)
		return
	}
	m.fail(operationID, activationErr, layout.RecoveryCommand)
}

// activateDeployment swaps an already-durable staged release through the
// two-rename activation window. The two renames cannot be committed as one
// portable filesystem operation, so every completed metadata step is fsynced
// and every pre-command failure attempts a checked restoration. Startup
// reconciliation covers a process or power loss between the renames.
func activateDeployment(staged, current, previous string, betweenRenames func() error) error {
	installRoot := filepath.Dir(current)
	stageRoot := filepath.Dir(staged)
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
	if err := betweenRenames(); err != nil {
		restoreErr := restorePreviousDeployment(current, previous, installRoot)
		return errors.Join(
			fmt.Errorf("pause between deployment renames: %w", err),
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

func syncTree(ctx context.Context, root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancel staged-tree sync: %w", err)
		}
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
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancel staged-tree sync: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sync staged tree: %w", err)
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sync staged tree: %w", err)
		}
		if err := syncDirectory(directories[i]); err != nil {
			return err
		}
	}
	return nil
}

func syncStagedDeployment(ctx context.Context, staged string) error {
	if err := syncTree(ctx, staged); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cancel staged deployment sync: %w", err)
	}
	if err := syncDirectory(filepath.Dir(staged)); err != nil {
		return fmt.Errorf("sync staging parent directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cancel staged deployment sync: %w", err)
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
	m.finishFailure(id, err, recovery, false)
}

func (m *Manager) failPendingRecovery(id string, err error, recovery string) {
	m.finishFailure(id, err, recovery, true)
}

func (m *Manager) finishFailure(id string, err error, recovery string, recoveryPending bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return
	}
	previous := *m.operation
	now := m.terminalOutcomeCutoff(*m.operation)
	advanceOutcomeRevision(m.operation)
	m.operation.Phase = updaterapi.PhaseFailed
	m.operation.Message = "Upgrade failed"
	m.operation.Error = err.Error()
	m.operation.RecoveryCommand = recovery
	m.operation.RecoveryPending = recoveryPending
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	if persistErr := m.persistLocked(); persistErr != nil {
		// Keep the last durable non-terminal state visible in memory so Trigger
		// cannot replace its recovery context. Startup reconciliation will turn
		// that durable state into a terminal failure once persistence recovers.
		*m.operation = previous
		log.Printf("upgrade failed (%v), but its terminal state was not persisted; further upgrades remain blocked until startup reconciliation: %v", err, persistErr)
	}
}

func (m *Manager) succeed(id, message string, closeForSelfUpdate bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operation == nil || m.operation.ID != id {
		return fmt.Errorf("operation %s is no longer current", id)
	}
	previous := *m.operation
	now := m.terminalOutcomeCutoff(*m.operation)
	advanceOutcomeRevision(m.operation)
	m.operation.Phase = updaterapi.PhaseSucceeded
	m.operation.Message = message
	m.operation.RecoveryCommand = ""
	m.operation.UpdatedAt = now
	m.operation.CompletedAt = &now
	if err := m.persistLocked(); err != nil {
		// Keep the last durable, non-terminal activation state (including its
		// recovery command) visible in memory. That fails closed: Trigger rejects
		// another upgrade until a restart reconciles and persists the outcome.
		*m.operation = previous
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
	terminalOutcomeBeforeRecovery := op
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
			now := m.terminalOutcomeCutoff(op)
			rewriteActivationRecoveryFailure(&op, now, err)
			m.operation = &op
			if persistErr := m.persistLocked(); persistErr != nil {
				return errors.Join(err, fmt.Errorf("persist activation recovery instructions: %w", persistErr))
			}
		}
		return err
	}
	if !wasTerminal {
		op.RecoveryPending = m.cfg.DeploymentMode == DeploymentModeHA && op.RecoveryCommand != ""
		now := m.terminalOutcomeCutoff(op)
		advanceOutcomeRevision(&op)
		op.Phase = updaterapi.PhaseFailed
		if restoredPrevious {
			op.Message = "Upgrade interrupted; previous deployment restored"
			op.Error = "The updater restarted during the activation swap. The previous deployment was restored."
		} else {
			op.Message = "Upgrade interrupted"
			op.Error = "The updater restarted before the operation completed; inspect the host log and recovery details before retrying."
		}
		op.UpdatedAt = now
		op.CompletedAt = &now
	} else if restoredPrevious {
		now := m.terminalOutcomeCutoff(op)
		advanceOutcomeRevision(&op)
		op.Phase = updaterapi.PhaseFailed
		op.Message = "Previous deployment restored during updater startup"
		if op.Error == "" {
			op.Error = "The active deployment was missing; the updater restored the validated previous deployment."
		} else {
			op.Error += " The active deployment was missing during startup; the updater restored the validated previous deployment."
		}
		if m.cfg.DeploymentMode != DeploymentModeHA {
			op.RecoveryCommand = ""
		}
		op.UpdatedAt = now
		// This is a new terminal outcome revision. Advance the remediation
		// cutoff with it so startup proof from before the rewrite cannot hide
		// the newly surfaced recovery information.
		op.CompletedAt = &now
	}
	if marker != nil {
		if m.cfg.DeploymentMode == DeploymentModeHA && op.RecoveryCommand != "" {
			op.RecoveryPending = true
		}
		if wasTerminal && !restoredPrevious && terminalOutcomeMateriallyChanged(terminalOutcomeBeforeRecovery, op) {
			now := m.terminalOutcomeCutoff(op)
			advanceOutcomeRevision(&op)
			op.UpdatedAt = now
			// Recovery derived from a retained activation marker is newly surfaced
			// terminal guidance. Advance the remediation cutoff with its revision.
			op.CompletedAt = &now
		}
	}
	m.operation = &op
	if err := m.persistReconciledState(); err != nil {
		return err
	}
	if marker != nil {
		return m.clearActivationMarker()
	}
	return nil
}

func rewriteActivationRecoveryFailure(operation *updaterapi.Operation, now time.Time, recoveryErr error) {
	advanceOutcomeRevision(operation)
	operation.Phase = updaterapi.PhaseFailed
	operation.Message = "Activation layout requires manual recovery"
	errorMessage := "Activation layout recovery did not complete: " + recoveryErr.Error()
	if !strings.Contains(operation.Error, errorMessage) {
		if operation.Error != "" {
			operation.Error += " "
		}
		operation.Error += errorMessage
	}
	operation.UpdatedAt = now
	// This is a new terminal outcome revision. Advance the remediation cutoff
	// with it so startup proof from before the rewrite cannot immediately
	// auto-acknowledge the new recovery guidance.
	operation.CompletedAt = &now
}

// terminalOutcomeCutoff keeps each newly surfaced terminal outcome from being
// ordered before state already visible on the host. In particular, Status
// treats a regular startup proof newer than CompletedAt as remediation, so a
// backward wall clock must not let an older proof hide a new recovery revision.
func (m *Manager) terminalOutcomeCutoff(operation updaterapi.Operation) time.Time {
	cutoff := m.cfg.Now().UTC()
	if operation.StartedAt.After(cutoff) {
		cutoff = operation.StartedAt.UTC()
	}
	if operation.UpdatedAt.After(cutoff) {
		cutoff = operation.UpdatedAt.UTC()
	}
	if operation.CompletedAt != nil && operation.CompletedAt.After(cutoff) {
		cutoff = operation.CompletedAt.UTC()
	}
	proof, err := os.Lstat(filepath.Join(m.cfg.InstallRoot, "deployment", startupProofFilename))
	if err == nil && proof.Mode().IsRegular() && proof.ModTime().After(cutoff) {
		cutoff = proof.ModTime().UTC()
	}
	return cutoff
}

// advanceOutcomeRevision gives each materially distinct terminal outcome a
// compare-and-set identity. A prior acknowledgement applies only to the
// outcome it observed, never to recovery guidance written later.
func advanceOutcomeRevision(operation *updaterapi.Operation) {
	operation.OutcomeRevision++
	operation.Acknowledged = false
}

func terminalOutcomeMateriallyChanged(before, after updaterapi.Operation) bool {
	return before.Phase != after.Phase ||
		before.Message != after.Message ||
		before.Error != after.Error ||
		before.RecoveryCommand != after.RecoveryCommand ||
		before.RecoveryPending != after.RecoveryPending
}

// persistReconciledState normally preserves the ordering of reconciliation,
// durable state, then stale-artifact cleanup. If the state write itself proves
// the filesystem is full, the exact reserved-name artifacts are the only safe
// source of space to reclaim: layout reconciliation has already succeeded, so
// clean them and retry the small state write once. Other persistence failures
// leave the artifacts untouched for diagnosis.
func (m *Manager) persistReconciledState() error {
	persistErr := m.persistLocked()
	if persistErr == nil || !errors.Is(persistErr, syscall.ENOSPC) {
		return persistErr
	}
	if cleanupErr := m.cleanupStaleArtifacts(); cleanupErr != nil {
		return errors.Join(
			persistErr,
			fmt.Errorf("clean stale upgrade artifacts after updater state exhausted storage: %w", cleanupErr),
		)
	}
	if retryErr := m.persistLocked(); retryErr != nil {
		return errors.Join(
			persistErr,
			fmt.Errorf("persist reconciled updater state after stale artifact cleanup: %w", retryErr),
		)
	}
	return nil
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
			if m.cfg.DeploymentMode == DeploymentModeHA {
				op.RecoveryCommand = m.activationRecoveryCommand(current, version)
			} else if version == op.TargetVersion {
				markerInfo, markerErr := os.Lstat(filepath.Join(current, preflightProofFilename))
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
	previousVersion, err := readInstalledVersion(filepath.Join(previous, "version.txt"))
	if err != nil {
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
	if m.cfg.DeploymentMode == DeploymentModeHA {
		op.RecoveryCommand = m.activationRecoveryCommand(current, previousVersion)
	}
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

// pruneOperationLogs retains the current operation log plus the newest prior
// logs within a bounded count and aggregate size. Callers may reserve capacity
// for an operation that has not created its log yet. The daemon-lifetime lock
// must be held, and Trigger additionally keeps operationRunning false while it
// prunes, so a live writer is never selected for removal.
func (m *Manager) pruneOperationLogs(protectedName string, reserveFiles int, reserveBytes int64) error {
	return pruneOperationLogsRoot(
		m.logRoot,
		protectedName,
		maxRetainedOperationLogs,
		maxRetainedLogBytes,
		reserveFiles,
		reserveBytes,
	)
}

type operationLogEntry struct {
	name    string
	size    int64
	modTime time.Time
}

func pruneOperationLogs(
	logDir string,
	protectedName string,
	maxFiles int,
	maxBytes int64,
	reserveFiles int,
	reserveBytes int64,
) error {
	logRoot, err := os.OpenRoot(logDir)
	if err != nil {
		return fmt.Errorf("open updater operation log root: %w", err)
	}
	defer logRoot.Close()
	return pruneOperationLogsRoot(
		logRoot,
		protectedName,
		maxFiles,
		maxBytes,
		reserveFiles,
		reserveBytes,
	)
}

func pruneOperationLogsRoot(
	logRoot *os.Root,
	protectedName string,
	maxFiles int,
	maxBytes int64,
	reserveFiles int,
	reserveBytes int64,
) error {
	if maxFiles < 0 || maxBytes < 0 || reserveFiles < 0 || reserveBytes < 0 ||
		reserveFiles > maxFiles || reserveBytes > maxBytes {
		return fmt.Errorf("invalid updater operation log retention limits")
	}
	logDirectory, err := logRoot.Open(".")
	if err != nil {
		return fmt.Errorf("open updater operation log directory: %w", err)
	}
	entries, readErr := logDirectory.ReadDir(-1)
	closeErr := logDirectory.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(
			wrapIfError("list updater operation logs", readErr),
			wrapIfError("close updater operation log directory", closeErr),
		)
	}

	logs := make([]operationLogEntry, 0, len(entries))
	var totalBytes int64
	for _, entry := range entries {
		if !operationLogNamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := logRoot.Lstat(entry.Name())
		if err != nil {
			return fmt.Errorf("inspect updater operation log %s: %w", entry.Name(), err)
		}
		// The updater creates regular files only. Ignore unexpected symlinks,
		// directories, and other special entries: retention must never follow a
		// link or recursively remove content it did not create.
		if !info.Mode().IsRegular() {
			continue
		}
		totalBytes = saturatingLogBytes(totalBytes, info.Size(), maxBytes)
		logs = append(logs, operationLogEntry{
			name:    entry.Name(),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].modTime.Equal(logs[j].modTime) {
			return logs[i].name < logs[j].name
		}
		return logs[i].modTime.Before(logs[j].modTime)
	})

	allowedFiles := maxFiles - reserveFiles
	allowedBytes := maxBytes - reserveBytes
	removed := false
	var pruneErr error
	for len(logs) > allowedFiles || totalBytes > allowedBytes {
		removeIndex := -1
		for i := range logs {
			if logs[i].name != protectedName {
				removeIndex = i
				break
			}
		}
		if removeIndex == -1 {
			pruneErr = fmt.Errorf("current updater operation log leaves insufficient retained capacity")
			break
		}
		candidate := logs[removeIndex]
		// Root.Remove confines the operation to the stable directory handle. It
		// cannot escape through an ancestor swap or a planted symlink.
		if err := logRoot.Remove(candidate.name); err != nil {
			pruneErr = fmt.Errorf("remove updater operation log %s: %w", candidate.name, err)
			break
		}
		removed = true
		if totalBytes > maxBytes {
			// Recompute after a saturated sum so later removals use exact sizes.
			totalBytes = 0
			for i := range logs {
				if i != removeIndex {
					totalBytes = saturatingLogBytes(totalBytes, logs[i].size, maxBytes)
				}
			}
		} else {
			totalBytes -= candidate.size
		}
		logs = append(logs[:removeIndex], logs[removeIndex+1:]...)
	}
	if removed {
		if err := syncOperationLogRoot(logRoot); err != nil {
			pruneErr = errors.Join(pruneErr, fmt.Errorf("persist updater operation log retention: %w", err))
		}
	}
	return pruneErr
}

func saturatingLogBytes(total, size, limit int64) int64 {
	if total > limit || size > limit || total > limit-size {
		if limit == math.MaxInt64 {
			return math.MaxInt64
		}
		return limit + 1
	}
	return total + size
}

func syncOperationLogRoot(logRoot *os.Root) error {
	logDirectory, err := logRoot.Open(".")
	if err != nil {
		return fmt.Errorf("open updater operation log directory for sync: %w", err)
	}
	syncErr := logDirectory.Sync()
	closeErr := logDirectory.Close()
	return errors.Join(
		wrapIfError("sync updater operation log directory", syncErr),
		wrapIfError("close updater operation log sync handle", closeErr),
	)
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
	if m.cfg.beforePersistState != nil {
		if err := m.cfg.beforePersistState(*m.operation); err != nil {
			return fmt.Errorf("persist updater state: %w", err)
		}
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
			value = strings.TrimSpace(value)
			if value != "" {
				return value, nil
			}
		}
	}
	return "", errors.New("installed version file has no version")
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
			if err := mkdirAllUnmasked(target, mode); err != nil {
				return fmt.Errorf("create archive directory %s: %w", header.Name, err)
			}
			// A file entry may already have created this directory as a
			// parent; the archive's own directory entry is authoritative.
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("apply archive directory mode %s: %w", header.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			// #nosec G301 -- extracted deployment parents must be traversable
			// by the account that owns and manually maintains the install.
			if err := mkdirAllUnmasked(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create archive parent for %s: %w", header.Name, err)
			}
			mode := os.FileMode(uint32(header.Mode & 0o755)) // #nosec G115 -- masked to nine permission bits
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create archive file %s: %w", header.Name, err)
			}
			if err := out.Chmod(mode); err != nil {
				out.Close()
				return fmt.Errorf("apply archive file mode %s: %w", header.Name, err)
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
	for _, path := range []string{".env", "server/influx_config/.env", "ha/node.env"} {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preserve deployment configuration: %w", err)
		}
		source := filepath.Join(current, path)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
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
	return setDeploymentTreeOwnership(ctx, staged, int(stat.Uid), int(stat.Gid))
}

func setDeploymentTreeOwnership(ctx context.Context, staged string, uid, gid int) error {
	err := filepath.WalkDir(staged, func(path string, _ os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preserve staged deployment ownership: %w", err)
		}
		if walkErr != nil {
			return fmt.Errorf("walk staged deployment: %w", walkErr)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect staged deployment entry: %w", err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to preserve ownership of non-regular staged entry %s", path)
		}
		// Lchown keeps the walk fail-safe if an entry changes after Lstat: a
		// replacement symlink itself may be re-owned, but its target is never
		// followed outside the root-private staging tree.
		if err := os.Lchown(path, uid, gid); err != nil {
			return fmt.Errorf("set staged deployment owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preserve staged deployment ownership: %w", err)
	}
	return nil
}

// mkdirAllUnmasked creates any missing directories on path, re-applying mode
// to each directory it creates. os.MkdirAll alone is narrowed by the process
// umask — 0o077 in this daemon, to protect its own state — which would leave
// staged deployment directories untraversable for the deployment owner and
// for the container users that bind-mount configuration out of the tree.
func mkdirAllUnmasked(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		return requireRealDirectory(path, info)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect directory %s: %w", path, err)
	}
	if parent := filepath.Dir(path); parent != path {
		if err := mkdirAllUnmasked(parent, mode); err != nil {
			return err
		}
	}
	if err := os.Mkdir(path, mode); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create directory %s: %w", path, err)
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return fmt.Errorf("inspect directory %s: %w", path, statErr)
		}
		return requireRealDirectory(path, info)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("apply directory mode %s: %w", path, err)
	}
	return nil
}

// requireRealDirectory rejects anything other than an actual directory —
// notably symlinks, which os.Stat would silently follow and which would let a
// crafted path redirect staged writes outside the staging root.
func requireRealDirectory(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("create directory %s: refusing to follow symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("create directory %s: path exists and is not a directory", path)
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
	if err := mkdirAllUnmasked(filepath.Dir(destination), 0o750); err != nil {
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
	if err := out.Chmod(info.Mode().Perm()); err != nil {
		out.Close()
		return fmt.Errorf("apply destination file mode: %w", err)
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

func (m *Manager) refreshSelfUpdater(
	parent context.Context,
	in *os.File,
	destination string,
	targetVersion string,
) error {
	candidatePath, err := stageExecutableCandidate(in, destination)
	if err != nil {
		return err
	}
	defer os.Remove(candidatePath)
	if err := m.validateUpdaterCandidate(parent, candidatePath, targetVersion); err != nil {
		return err
	}
	return installExecutableCandidate(candidatePath, destination)
}

func stageExecutableCandidate(in *os.File, destination string) (string, error) {
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind staged updater: %w", err)
	}
	info, err := in.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect staged updater: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Size() == 0 {
		return "", fmt.Errorf("staged updater is not a non-empty executable")
	}
	candidatePath := destination + ".candidate"
	if removed, err := unlinkExecutableSlot(candidatePath); err != nil {
		return "", fmt.Errorf("clean stale updater candidate: %w", err)
	} else if removed {
		if err := syncDirectory(filepath.Dir(destination)); err != nil {
			return "", fmt.Errorf("persist stale updater candidate cleanup: %w", err)
		}
	}
	fd, err := syscall.Open(
		candidatePath,
		syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("create updater executable candidate: %w", err)
	}
	candidate := os.NewFile(uintptr(fd), candidatePath)
	if candidate == nil {
		_ = syscall.Close(fd)
		return "", fmt.Errorf("create updater executable candidate")
	}
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(candidatePath)
		}
	}()
	written, err := io.Copy(candidate, in)
	if err != nil {
		_ = candidate.Close()
		return "", fmt.Errorf("copy staged updater: %w", err)
	}
	if written == 0 {
		_ = candidate.Close()
		return "", fmt.Errorf("staged updater is empty")
	}
	if err := candidate.Chmod(0o755); err != nil {
		_ = candidate.Close()
		return "", fmt.Errorf("make updater candidate executable: %w", err)
	}
	if err := candidate.Sync(); err != nil {
		_ = candidate.Close()
		return "", fmt.Errorf("sync updater executable candidate: %w", err)
	}
	if err := candidate.Close(); err != nil {
		return "", fmt.Errorf("close updater executable candidate: %w", err)
	}
	installed = true
	return candidatePath, nil
}

func (m *Manager) validateUpdaterCandidate(parent context.Context, candidatePath, targetVersion string) error {
	info, err := os.Lstat(candidatePath)
	if err != nil {
		return fmt.Errorf("inspect updater candidate: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Size() == 0 {
		return fmt.Errorf("updater candidate is not a non-empty executable")
	}
	var versionOutput bytes.Buffer
	boundedOutput := newCappedWriter(&versionOutput, maxCandidateVersionBytes)
	// Actually executing --version asks the host kernel to validate the
	// executable format and architecture. That is stronger and more portable
	// than parsing an ELF header while still proving the updater CLI contract.
	if err := m.runCommand(
		parent,
		defaultCandidateTimeout,
		filepath.Dir(candidatePath),
		boundedOutput,
		candidatePath,
		"--version",
	); err != nil {
		return fmt.Errorf("smoke-test updater candidate: %w", err)
	}
	if boundedOutput.truncated {
		return fmt.Errorf("updater candidate version output exceeded %d bytes", maxCandidateVersionBytes)
	}
	reportedVersion := strings.TrimSpace(versionOutput.String())
	if reportedVersion != targetVersion {
		return fmt.Errorf(
			"updater candidate reported version %q instead of %q",
			reportedVersion,
			targetVersion,
		)
	}
	return nil
}

func installExecutableCandidate(candidatePath, destination string) error {
	destinationInfo, err := os.Lstat(destination)
	if err != nil {
		return fmt.Errorf("inspect installed updater: %w", err)
	}
	if !destinationInfo.Mode().IsRegular() || destinationInfo.Mode().Perm()&0o111 == 0 || destinationInfo.Size() == 0 {
		return fmt.Errorf("installed updater is not a non-empty executable")
	}
	candidateInfo, err := os.Lstat(candidatePath)
	if err != nil {
		return fmt.Errorf("inspect validated updater candidate: %w", err)
	}
	if !candidateInfo.Mode().IsRegular() || candidateInfo.Mode().Perm()&0o111 == 0 || candidateInfo.Size() == 0 {
		return fmt.Errorf("validated updater candidate is not a non-empty executable")
	}

	parent := filepath.Dir(destination)
	backupPath := destination + selfUpdateBackupSuffix
	if removed, err := unlinkExecutableSlot(backupPath); err != nil {
		return fmt.Errorf("clean previous updater backup: %w", err)
	} else if removed {
		if err := syncDirectory(parent); err != nil {
			return fmt.Errorf("persist previous updater backup cleanup: %w", err)
		}
	}
	// A hard link preserves the running executable without creating a window
	// where the supervised destination is absent. It remains as an explicit
	// recovery slot until a later successful refresh replaces it.
	if err := os.Link(destination, backupPath); err != nil {
		return fmt.Errorf("retain previous updater executable: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		cleanupErr := os.Remove(backupPath)
		if cleanupErr == nil {
			cleanupErr = syncDirectory(parent)
		}
		return errors.Join(
			fmt.Errorf("persist previous updater executable: %w", err),
			cleanupErr,
		)
	}
	// The durable marker is installed after the previous executable is safely
	// retained but before the candidate can replace the supervised path. It is
	// the only cross-process authority to restore that backup.
	if err := writeSelfUpdateHandoffMarker(destination); err != nil {
		cleanupErr := cleanupFailedSelfUpdatePreparation(destination)
		return errors.Join(fmt.Errorf("prepare updater startup handoff: %w", err), cleanupErr)
	}
	if err := os.Rename(candidatePath, destination); err != nil {
		return errors.Join(
			fmt.Errorf("replace updater executable: %w", err),
			rollbackPendingSelfUpdate(destination),
		)
	}
	if err := syncDirectory(parent); err != nil {
		restoreErr := rollbackPendingSelfUpdate(destination)
		return errors.Join(fmt.Errorf("persist updater executable replacement: %w", err), restoreErr)
	}
	return nil
}

func cleanupFailedSelfUpdatePreparation(destination string) error {
	_, markerExists, err := readSelfUpdateHandoffMarker(destination)
	if err != nil {
		return err
	}
	if markerExists {
		return rollbackPendingSelfUpdate(destination)
	}
	removed, err := unlinkExecutableSlot(destination + selfUpdateBackupSuffix)
	if err != nil {
		return err
	}
	if removed {
		return syncDirectory(filepath.Dir(destination))
	}
	return nil
}

func restorePreviousExecutable(destination string) error {
	backupPath := destination + selfUpdateBackupSuffix
	backupInfo, err := os.Lstat(backupPath)
	if err != nil {
		return fmt.Errorf("inspect previous updater executable: %w", err)
	}
	if !backupInfo.Mode().IsRegular() || backupInfo.Mode().Perm()&0o111 == 0 || backupInfo.Size() == 0 {
		return fmt.Errorf("previous updater backup is not a non-empty executable")
	}
	destinationInfo, err := os.Lstat(destination)
	if err == nil {
		if !destinationInfo.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular updater destination")
		}
		if os.SameFile(destinationInfo, backupInfo) {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect updater destination before restoration: %w", err)
	}

	parent := filepath.Dir(destination)
	restorePath := destination + selfUpdateRestoreTempSuffix
	if removed, err := unlinkExecutableSlot(restorePath); err != nil {
		return fmt.Errorf("clean stale updater restoration link: %w", err)
	} else if removed {
		if err := syncDirectory(parent); err != nil {
			return fmt.Errorf("persist stale updater restoration cleanup: %w", err)
		}
	}
	if err := os.Link(backupPath, restorePath); err != nil {
		return fmt.Errorf("prepare previous updater restoration: %w", err)
	}
	defer os.Remove(restorePath)
	if err := os.Rename(restorePath, destination); err != nil {
		return fmt.Errorf("restore previous updater executable: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("persist previous updater executable restoration: %w", err)
	}
	return nil
}

func unlinkExecutableSlot(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect executable file slot: %w", err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("refusing to remove directory from executable file slot")
	}
	if err := syscall.Unlink(path); err != nil {
		return false, fmt.Errorf("unlink executable file slot: %w", err)
	}
	return true, nil
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
			if err := mkdirAllUnmasked(target, info.Mode().Perm()); err != nil {
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

func activationPreflightRecoveryCommand(deploymentPath string) string {
	return fmt.Sprintf(
		"cd %s && ./run-fleet.sh --non-interactive --preflight-only && ./run-fleet.sh --non-interactive --skip-build",
		shellQuote(deploymentPath),
	)
}

func activationRecoveryCommandAfterFailure(deploymentPath string) (string, error) {
	proofPath := filepath.Join(deploymentPath, preflightProofFilename)
	info, err := os.Lstat(proofPath)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("activation preflight proof is not a regular file")
		}
		return activationRecoveryCommand(deploymentPath), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return activationPreflightRecoveryCommand(deploymentPath), nil
	}
	return "", fmt.Errorf("inspect activation preflight proof: %w", err)
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
