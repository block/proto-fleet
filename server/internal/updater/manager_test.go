package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type recordedCommand struct {
	Dir  string
	Name string
	Args []string
}

type recordingRunner struct {
	mu                   sync.Mutex
	commands             []recordedCommand
	preflight            error
	activation           error
	cleanup              error
	preflightAt          chan struct{}
	release              chan struct{}
	waitForCancel        bool
	activationAt         chan struct{}
	releaseActivation    chan struct{}
	waitForCleanupCancel bool
	releaseCleanup       chan struct{}
	activationHook       func(string)
	candidateVersion     string
	candidateError       error
}

const operatorEnv = `DB_PASSWORD=secret
ENABLE_BETA_ALERTS=true
ENABLE_SYSTEM_MONITORING=true
ENABLE_TRACING=true
ENABLE_ONE_CLICK_UPDATES=true
`

func (r *recordingRunner) Run(ctx context.Context, dir string, output io.Writer, name string, args ...string) error {
	r.mu.Lock()
	r.commands = append(r.commands, recordedCommand{Dir: dir, Name: name, Args: append([]string(nil), args...)})
	r.mu.Unlock()
	if len(args) == 1 && args[0] == "--version" && name != "/bin/bash" {
		if r.candidateError != nil {
			return r.candidateError
		}
		candidateVersion := r.candidateVersion
		if candidateVersion == "" {
			candidateVersion = "v1.1.0"
		}
		if _, err := fmt.Fprintln(output, candidateVersion); err != nil {
			return fmt.Errorf("write candidate version: %w", err)
		}
		return nil
	}

	if name == "/bin/bash" && len(args) > 0 && args[len(args)-1] == "--preflight-only" {
		if r.preflightAt != nil {
			close(r.preflightAt)
		}
		if r.waitForCancel {
			<-ctx.Done()
			return fmt.Errorf("preflight canceled: %w", ctx.Err())
		}
		if r.release != nil {
			select {
			case <-r.release:
			case <-ctx.Done():
				return fmt.Errorf("preflight canceled: %w", ctx.Err())
			}
		}
		return r.preflight
	}
	if name == "docker" {
		if r.waitForCleanupCancel {
			select {
			case <-ctx.Done():
				return fmt.Errorf("cleanup canceled: %w", ctx.Err())
			case <-r.releaseCleanup:
				return fmt.Errorf("cleanup released before cancellation")
			}
		}
		return r.cleanup
	}
	if r.activationAt != nil {
		close(r.activationAt)
	}
	if r.releaseActivation != nil {
		select {
		case <-r.releaseActivation:
		case <-ctx.Done():
			return fmt.Errorf("activation canceled: %w", ctx.Err())
		}
	}
	if r.activationHook != nil {
		r.activationHook(dir)
	}
	return r.activation
}

func (r *recordingRunner) Commands() []recordedCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCommand(nil), r.commands...)
}

func TestManagerUpgradeStagesBeforeActivationAndPreservesConfiguration(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{}
	manager := newTestManager(t, installRoot, server, runner)

	operation, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	assert.Equal(t, updaterapi.PhaseQueued, operation.Phase)

	completed := waitForTerminal(t, manager)
	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	assert.Equal(t, "v1.1.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment.previous", "version.txt")))
	assert.Equal(t, operatorEnv, mustReadFile(t, filepath.Join(installRoot, "deployment", ".env")))
	assert.Equal(t, "certificate\n", mustReadFile(t, filepath.Join(installRoot, "deployment", "ssl", "cert.pem")))
	assert.Equal(t, "influx-secret\n", mustReadFile(t, filepath.Join(installRoot, "deployment", "server", "influx_config", ".env")))

	commands := runner.Commands()
	require.Len(t, commands, 2)
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--preflight-only"}, commands[0].Args)
	assert.Contains(t, commands[0].Dir, ".proto-fleet-upgrade-")
	assert.Equal(t, filepath.Join(installRoot, "deployment"), commands[1].Dir)
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--skip-build"}, commands[1].Args)
	assert.FileExists(t, completed.LogPath)
	assert.Empty(t, completed.RecoveryCommand)
	assert.NoFileExists(t, filepath.Join(manager.cfg.StateDir, activationMarkerFilename))
}

func TestManagerDefersSelfUpdateUntilActivationSucceeds(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0o755))
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{activation: assert.AnError}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.SelfUpdatePath = installedUpdater
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Equal(t, "old updater", mustReadFile(t, installedUpdater))
	select {
	case <-manager.SelfUpdateReady():
		t.Fatal("failed activation must not request a self-restart")
	default:
	}
}

func TestManagerSelfUpdateUsesVerifiedArchiveCopy(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0o755))
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{
		activationHook: func(dir string) {
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, "updater", "proto-fleet-updater"),
				[]byte("operator replacement"),
				0o755,
			))
		},
	}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.SelfUpdatePath = installedUpdater
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	assert.Equal(t, "updater", mustReadFile(t, installedUpdater))
	assert.Equal(t, "old updater", mustReadFile(t, installedUpdater+selfUpdateBackupSuffix))
	select {
	case <-manager.SelfUpdateReady():
	case <-time.After(time.Second):
		t.Fatal("successful updater replacement did not request a self-restart")
	}
	_, err = manager.Trigger("v1.2.0")
	require.ErrorContains(t, err, "updater is shutting down")
	var persisted updaterapi.Operation
	require.NoError(t, json.Unmarshal([]byte(mustReadFile(t, filepath.Join(manager.cfg.StateDir, stateFilename))), &persisted))
	assert.Equal(t, updaterapi.PhaseSucceeded, persisted.Phase)
	require.NoError(t, manager.Shutdown(context.Background()))
	restarted, err := NewManager(manager.cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, restarted.Close()) })
	restartedOperation := restarted.Status().Operation
	require.NotNil(t, restartedOperation)
	assert.Equal(t, updaterapi.PhaseSucceeded, restartedOperation.Phase)
	assert.Equal(t, "old updater", mustReadFile(t, installedUpdater+selfUpdateBackupSuffix))
}

func TestManagerFailsClosedWhenSuccessfulStateCannotPersist(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0o755))
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	manager := newTestManagerWithConfig(t, installRoot, server, &recordingRunner{}, func(cfg *Config) {
		cfg.SelfUpdatePath = installedUpdater
		cfg.beforePersistState = func(operation updaterapi.Operation) error {
			if operation.Phase == updaterapi.PhaseSucceeded {
				return assert.AnError
			}
			return nil
		}
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		manager.mu.RLock()
		defer manager.mu.RUnlock()
		return !manager.operationRunning
	}, 5*time.Second, 10*time.Millisecond, "upgrade worker did not finish")

	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseActivating, operation.Phase)
	assert.NotEmpty(t, operation.RecoveryCommand)
	assert.Nil(t, operation.CompletedAt)
	assert.NotContains(t, operation.Message, "is running")
	assert.Equal(t, "updater", mustReadFile(t, installedUpdater), "self-update must reach the signal gate")
	assert.Contains(t, mustReadFile(t, operation.LogPath), "further upgrades remain blocked")
	select {
	case <-manager.SelfUpdateReady():
		t.Fatal("non-durable success must not request a self-restart")
	default:
	}

	var persisted updaterapi.Operation
	require.NoError(t, json.Unmarshal(
		[]byte(mustReadFile(t, filepath.Join(manager.cfg.StateDir, stateFilename))),
		&persisted,
	))
	assert.Equal(t, updaterapi.PhaseActivating, persisted.Phase)
	assert.Equal(t, operation.RecoveryCommand, persisted.RecoveryCommand)
	assert.Nil(t, persisted.CompletedAt)

	_, err = manager.Trigger("v1.2.0")
	require.ErrorContains(t, err, "already in progress")

	// A restart reconciles the durable activation state into a terminal failure;
	// until then, both the in-memory and persisted non-terminal state fail closed.
	require.NoError(t, manager.Close())
	restarted, err := NewManager(manager.cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, restarted.Close()) })
	restartedOperation := restarted.Status().Operation
	require.NotNil(t, restartedOperation)
	assert.Equal(t, updaterapi.PhaseFailed, restartedOperation.Phase)
}

func TestManagerRejectsUpdaterCandidateWithUnexpectedVersionBeforeReplacement(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0o755))
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{candidateVersion: "v9.9.9"}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.SelfUpdatePath = installedUpdater
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	assert.Equal(t, "old updater", mustReadFile(t, installedUpdater))
	assert.NoFileExists(t, installedUpdater+selfUpdateBackupSuffix)
	assert.NoFileExists(t, installedUpdater+".candidate")
	assert.Contains(t, completed.Message, "host updater refresh needs attention")
	assert.Contains(t, mustReadFile(t, completed.LogPath), "reported version")
	select {
	case <-manager.SelfUpdateReady():
		t.Fatal("invalid updater candidate must not request a self-restart")
	default:
	}
}

func TestManagerSelfUpdateFailureIsADegradedSuccess(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{}
	missingParent := filepath.Join(t.TempDir(), "missing", "proto-fleet-updater")
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.SelfUpdatePath = missingParent
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	assert.Equal(t, "v1.1.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Contains(t, completed.Message, "host updater refresh needs attention")
	assert.Contains(t, mustReadFile(t, completed.LogPath), "host updater binary was not refreshed")
	assert.NoFileExists(t, missingParent)
	select {
	case <-manager.SelfUpdateReady():
		t.Fatal("failed updater replacement must not request a self-restart")
	default:
	}
}

func TestManagerChecksumFailureLeavesCurrentDeploymentUntouched(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, strings.Repeat("0", sha256.Size*2))
	runner := &recordingRunner{}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "checksum verification failed")
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	assert.Empty(t, runner.Commands())
}

func TestManagerPreflightFailureNeverTakesTheRunningDeploymentOffline(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{preflight: assert.AnError}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "preflight failed")
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	commands := runner.Commands()
	require.Len(t, commands, 1+len(releaseImageRepositories))
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--preflight-only"}, commands[0].Args)
	for i, repository := range releaseImageRepositories {
		cleanup := commands[i+1]
		assert.Equal(t, "docker", cleanup.Name)
		assert.Equal(t, []string{"image", "rm", repository + ":v1.1.0"}, cleanup.Args)
		assert.Contains(t, cleanup.Dir, ".proto-fleet-upgrade-")
	}
}

func TestManagerFailedPreflightImageCleanupIsBestEffort(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{preflight: assert.AnError, cleanup: assert.AnError}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "preflight failed")
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Contains(t, mustReadFile(t, completed.LogPath), "warning: could not remove failed preflight image proto-fleet-api:v1.1.0")
}

func TestManagerFailedPreflightImageCleanupHasABoundedDeadline(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	releaseCleanup := make(chan struct{})
	runner := &recordingRunner{
		preflight:            assert.AnError,
		waitForCleanupCancel: true,
		releaseCleanup:       releaseCleanup,
	}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.CleanupTimeout = 25 * time.Millisecond
	})
	t.Cleanup(func() { close(releaseCleanup) })

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "preflight failed")
	assert.NotContains(t, completed.Error, "cleanup canceled")
	assert.Contains(t, mustReadFile(t, completed.LogPath), context.DeadlineExceeded.Error())
}

func TestManagerActivationFailurePreservesForwardRecovery(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{activation: assert.AnError}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "new stack failed to start")
	assert.Equal(t, "v1.1.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment.previous", "version.txt")))
	assert.Contains(t, completed.RecoveryCommand, "./run-fleet.sh --non-interactive --skip-build")
	commands := runner.Commands()
	require.Len(t, commands, 2)
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--skip-build"}, commands[1].Args)
}

func TestManagerFailsActivationOnlyAfterRestoringPreviousDeployment(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	started := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	manager.mu.Lock()
	manager.operation = &updaterapi.Operation{
		ID:              "activation-error",
		TargetVersion:   "v1.1.0",
		Phase:           updaterapi.PhaseActivating,
		RecoveryCommand: "stale",
		StartedAt:       started,
		UpdatedAt:       started,
	}
	require.NoError(t, manager.persistLocked())
	manager.mu.Unlock()
	require.NoError(t, manager.writeActivationMarker(activationMarker{
		OperationID:   "activation-error",
		TargetVersion: "v1.1.0",
	}))
	require.NoError(t, os.Rename(
		filepath.Join(installRoot, "deployment"),
		filepath.Join(installRoot, "deployment.previous"),
	))

	manager.failActivation("activation-error", "v1.1.0", assert.AnError, io.Discard)

	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	assert.Contains(t, operation.Error, assert.AnError.Error())
	assert.Empty(t, operation.RecoveryCommand)
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	var persisted updaterapi.Operation
	require.NoError(t, json.Unmarshal([]byte(mustReadFile(t, filepath.Join(stateDir, stateFilename))), &persisted))
	assert.Equal(t, updaterapi.PhaseFailed, persisted.Phase)
	assert.Empty(t, persisted.RecoveryCommand)
}

func TestActivationMarkerWriteIsAtomicAndExclusive(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    filepath.Join(t.TempDir(), "state"),
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	marker := activationMarker{OperationID: "atomic-marker", TargetVersion: "v1.1.0"}
	require.NoError(t, manager.writeActivationMarker(marker))
	assert.NoFileExists(t, filepath.Join(manager.cfg.StateDir, activationMarkerTempName))
	persisted, err := manager.readActivationMarker()
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.Equal(t, marker, *persisted)
	original := mustReadFile(t, filepath.Join(manager.cfg.StateDir, activationMarkerFilename))

	err = manager.writeActivationMarker(activationMarker{OperationID: "replacement", TargetVersion: "v1.2.0"})
	require.ErrorContains(t, err, "already exists")
	assert.Equal(t, original, mustReadFile(t, filepath.Join(manager.cfg.StateDir, activationMarkerFilename)))
	assert.NoFileExists(t, filepath.Join(manager.cfg.StateDir, activationMarkerTempName))
}

func TestManagerRejectsASecondUpgradeWhileOneIsRunning(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{
		preflightAt: make(chan struct{}),
		release:     make(chan struct{}),
	}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	select {
	case <-runner.preflightAt:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade never reached preflight")
	}

	_, err = manager.Trigger("v1.2.0")
	require.ErrorContains(t, err, "already in progress")
	close(runner.release)
	assert.Equal(t, updaterapi.PhaseSucceeded, waitForTerminal(t, manager).Phase)
}

func TestManagerRejectsAnotherUpgradeWhileActivationRecoveryIsPending(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	manager.mu.Lock()
	manager.operation = &updaterapi.Operation{
		ID:            "pending-recovery",
		TargetVersion: "v1.1.0",
		Phase:         updaterapi.PhaseFailed,
		Message:       "Activation layout requires manual recovery",
		StartedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   &now,
	}
	require.NoError(t, manager.persistLocked())
	manager.mu.Unlock()
	require.NoError(t, manager.writeActivationMarker(activationMarker{
		OperationID:   "pending-recovery",
		TargetVersion: "v1.1.0",
	}))
	stateBefore := mustReadFile(t, filepath.Join(stateDir, stateFilename))

	_, err = manager.Trigger("v1.2.0")
	require.ErrorContains(t, err, "activation recovery for operation pending-recovery is pending")
	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, "pending-recovery", operation.ID)
	assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	assert.Equal(t, stateBefore, mustReadFile(t, filepath.Join(stateDir, stateFilename)))
	assert.FileExists(t, filepath.Join(stateDir, activationMarkerFilename))
}

func TestManagerProcessLockPreventsASecondDaemonFromMutatingState(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	stateDir := filepath.Join(t.TempDir(), "state")
	runner := &recordingRunner{
		preflightAt: make(chan struct{}),
		release:     make(chan struct{}),
	}
	config := Config{
		InstallRoot:              installRoot,
		StateDir:                 stateDir,
		DownloadBaseURL:          server.URL,
		HTTPClient:               server.Client(),
		Runner:                   runner,
		GOARCH:                   "amd64",
		NewID:                    func() string { return "locked-operation" },
		allowTestDownloadBaseURL: true,
	}
	manager, err := NewManager(config)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	_, err = manager.Trigger("v1.1.0")
	require.NoError(t, err)
	select {
	case <-runner.preflightAt:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade never reached preflight")
	}
	contestedArtifact := filepath.Join(stateDir, operationArtifactBase("contested-cleanup")+".updater")
	require.NoError(t, os.WriteFile(contestedArtifact, []byte("still owned by the live daemon"), 0o600))

	_, err = NewManager(config)
	require.ErrorContains(t, err, "another updater process is already running")
	assert.FileExists(t, contestedArtifact, "a lock contender must not mutate updater state")
	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.False(t, operation.Phase.Terminal())
	var persisted updaterapi.Operation
	require.NoError(t, json.Unmarshal([]byte(mustReadFile(t, filepath.Join(stateDir, stateFilename))), &persisted))
	assert.False(t, persisted.Phase.Terminal())

	close(runner.release)
	assert.Equal(t, updaterapi.PhaseSucceeded, waitForTerminal(t, manager).Phase)
	require.NoError(t, manager.Close())
	replacement, err := NewManager(config)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, replacement.Close()) })
	assert.NoFileExists(t, contestedArtifact)
}

func TestManagerPreflightDeadlineFailsTheOperation(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{waitForCancel: true}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.PreflightTimeout = 25 * time.Millisecond
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "phase deadline")
	assert.Contains(t, completed.Error, context.DeadlineExceeded.Error())
}

func TestManagerActivationDeadlineLetsShutdownFinishWithoutRollback(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	activationAt := make(chan struct{})
	runner := &recordingRunner{
		activationAt:      activationAt,
		releaseActivation: make(chan struct{}),
	}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.ActivationTimeout = 250 * time.Millisecond
	})

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	select {
	case <-activationAt:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade never reached activation")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	require.NoError(t, manager.Shutdown(shutdownCtx))
	completed := manager.Status().Operation
	require.NotNil(t, completed)
	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "phase deadline")
	assert.Contains(t, completed.Error, context.DeadlineExceeded.Error())
	assert.NotContains(t, completed.Error, context.Canceled.Error())
	assert.Equal(t, "v1.1.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment.previous", "version.txt")))
	assert.Contains(t, completed.RecoveryCommand, "./run-fleet.sh --non-interactive --skip-build")
}

func TestManagerShutdownCancelsPreActivationWorkAndWaits(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{
		preflightAt:   make(chan struct{}),
		waitForCancel: true,
	}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.PreflightTimeout = time.Hour
	})
	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	select {
	case <-runner.preflightAt:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade never reached preflight")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, manager.Shutdown(shutdownCtx))
	completed := manager.Status().Operation
	require.NotNil(t, completed)
	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, context.Canceled.Error())
	_, err = manager.Trigger("v1.2.0")
	require.ErrorContains(t, err, "updater is shutting down")
}

func TestManagerShutdownDuringActivationRetainsTheProcessLockUntilCompletion(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	stateDir := filepath.Join(t.TempDir(), "state")
	activationAt := make(chan struct{})
	releaseActivation := make(chan struct{})
	var releaseOnce sync.Once
	runner := &recordingRunner{
		activationAt:      activationAt,
		releaseActivation: releaseActivation,
	}
	config := Config{
		InstallRoot:              installRoot,
		StateDir:                 stateDir,
		DownloadBaseURL:          server.URL,
		HTTPClient:               server.Client(),
		Runner:                   runner,
		GOARCH:                   "amd64",
		NewID:                    func() string { return "activating-operation" },
		ActivationTimeout:        time.Hour,
		allowTestDownloadBaseURL: true,
	}
	manager, err := NewManager(config)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseActivation) }) })

	_, err = manager.Trigger("v1.1.0")
	require.NoError(t, err)
	select {
	case <-activationAt:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade never reached activation")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 50*time.Millisecond)
	err = manager.Shutdown(shutdownCtx)
	cancelShutdown()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseActivating, operation.Phase)

	contender, contenderErr := NewManager(config)
	if contender != nil {
		_ = contender.Close()
	}
	require.ErrorContains(t, contenderErr, "another updater process is already running")

	releaseOnce.Do(func() { close(releaseActivation) })
	completed := waitForTerminal(t, manager)
	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	finalShutdownCtx, cancelFinalShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, manager.Shutdown(finalShutdownCtx))
	cancelFinalShutdown()

	var replacement *Manager
	require.Eventually(t, func() bool {
		var replacementErr error
		replacement, replacementErr = NewManager(config)
		return replacementErr == nil
	}, 2*time.Second, 10*time.Millisecond, "replacement manager did not reacquire the released process lock")
	t.Cleanup(func() { assert.NoError(t, replacement.Close()) })
}

func TestManagerMarksAnInterruptedPersistedOperationFailed(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	started := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, stateFilename),
		[]byte(fmt.Sprintf(`{"id":"old","target_version":"v1.1.0","phase":"activating","started_at":%q,"updated_at":%q}`, started.Format(time.RFC3339Nano), started.Format(time.RFC3339Nano))),
		0o600,
	))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
		Now:         func() time.Time { return started.Add(time.Minute) },
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	assert.Contains(t, operation.Error, "updater restarted")
	require.NotNil(t, operation.CompletedAt)
}

func TestManagerRestoresPreviousDeploymentAfterInterruptedSwap(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	require.NoError(t, os.Rename(
		filepath.Join(installRoot, "deployment"),
		filepath.Join(installRoot, "deployment.previous"),
	))
	stateDir := filepath.Join(t.TempDir(), "state")
	writeInterruptedOperationState(t, stateDir, "v1.1.0")

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	assert.Contains(t, operation.Message, "previous deployment restored")
	assert.Empty(t, operation.RecoveryCommand)
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	assert.NoFileExists(t, filepath.Join(stateDir, activationMarkerFilename))
}

func TestManagerReconcilesTerminalFailedActivationBeforeCleaningArtifacts(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	require.NoError(t, os.Rename(
		filepath.Join(installRoot, "deployment"),
		filepath.Join(installRoot, "deployment.previous"),
	))
	stageRoot := filepath.Join(installRoot, operationArtifactBase("failed-operation"))
	require.NoError(t, os.MkdirAll(filepath.Join(stageRoot, "deployment"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stageRoot, "deployment", "partial"), []byte("stale"), 0o600))

	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	started := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	completed := started.Add(time.Minute)
	persisted := updaterapi.Operation{
		ID:              "failed-operation",
		TargetVersion:   "v1.1.0",
		Phase:           updaterapi.PhaseFailed,
		Message:         "Upgrade failed",
		Error:           "activate staged deployment: input/output error",
		RecoveryCommand: "stale",
		StartedAt:       started,
		UpdatedAt:       completed,
		CompletedAt:     &completed,
	}
	data, err := json.Marshal(persisted)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, stateFilename), data, 0o600))
	marker, err := json.Marshal(activationMarker{
		OperationID:   persisted.ID,
		TargetVersion: persisted.TargetVersion,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, activationMarkerFilename), marker, 0o600))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	assert.Contains(t, operation.Error, "input/output error")
	assert.Contains(t, operation.Error, "restored the validated previous deployment")
	assert.Contains(t, operation.Message, "Previous deployment restored")
	assert.Equal(t, completed, *operation.CompletedAt)
	assert.Empty(t, operation.RecoveryCommand)
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	assert.NoDirExists(t, stageRoot)
}

func TestManagerDoesNotRestorePreviousWithoutAPendingSwapMarker(t *testing.T) {
	t.Parallel()

	for _, phase := range []updaterapi.Phase{
		updaterapi.PhaseFailed,
		updaterapi.PhaseSucceeded,
	} {
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()

			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			require.NoError(t, os.Rename(
				filepath.Join(installRoot, "deployment"),
				filepath.Join(installRoot, "deployment.previous"),
			))
			stateDir := filepath.Join(t.TempDir(), "state")
			require.NoError(t, os.MkdirAll(stateDir, 0o700))
			now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
			operation := updaterapi.Operation{
				ID:            "historical-operation",
				TargetVersion: "v1.1.0",
				Phase:         phase,
				StartedAt:     now,
				UpdatedAt:     now,
				CompletedAt:   &now,
			}
			data, err := json.Marshal(operation)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(stateDir, stateFilename), data, 0o600))

			_, err = NewManager(Config{
				InstallRoot: installRoot,
				StateDir:    stateDir,
				GOARCH:      "amd64",
			})
			require.ErrorContains(t, err, "without a pending activation swap")
			assert.NoDirExists(t, filepath.Join(installRoot, "deployment"))
			assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment.previous", "version.txt")))
		})
	}
}

func TestManagerCleansCrashAbandonedArtifactsOnStartup(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	artifactBase := operationArtifactBase("../../path-like-operation-id")
	assert.True(t, staleStagingArtifactName.MatchString(artifactBase))
	assert.NotContains(t, artifactBase, string(os.PathSeparator))

	staleStatePaths := []string{
		filepath.Join(stateDir, artifactBase+".tar.gz"),
		filepath.Join(stateDir, artifactBase+".sha256"),
		filepath.Join(stateDir, artifactBase+".updater"),
		filepath.Join(stateDir, stateTempPrefix+"abandoned"),
		filepath.Join(stateDir, activationMarkerTempName),
	}
	for _, path := range staleStatePaths {
		require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))
	}
	keepPath := filepath.Join(stateDir, ".proto-fleet-upgrade-lookalike.tar.gz")
	require.NoError(t, os.WriteFile(keepPath, []byte("keep"), 0o600))
	stageRoot := filepath.Join(installRoot, artifactBase)
	require.NoError(t, os.MkdirAll(filepath.Join(stageRoot, "deployment", "nested"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stageRoot, "deployment", "nested", "file"), []byte("stale"), 0o600))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	for _, path := range staleStatePaths {
		assert.NoFileExists(t, path)
	}
	assert.NoDirExists(t, stageRoot)
	assert.FileExists(t, keepPath)
}

func TestManagerRejectsSymlinkedLogDirectoryWithoutTouchingItsTarget(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	externalLogDir := t.TempDir()
	externalLog := filepath.Join(externalLogDir, operationLogFilename("external"))
	require.NoError(t, os.WriteFile(externalLog, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(externalLogDir, filepath.Join(stateDir, "logs")))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	if manager != nil {
		_ = manager.Close()
	}
	require.ErrorContains(t, err, "updater log path is not a directory")
	assert.Equal(t, "keep", mustReadFile(t, externalLog))
}

func TestPruneOperationLogsRetainsCurrentAndNewestWithinBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		maxFiles     int
		maxBytes     int64
		reserveFiles int
		reserveBytes int64
		wantRemoved  []string
		wantRetained []string
	}{
		{
			name:         "count",
			maxFiles:     3,
			maxBytes:     100,
			wantRemoved:  []string{"oldest"},
			wantRetained: []string{"current", "middle", "newest"},
		},
		{
			name:         "bytes",
			maxFiles:     10,
			maxBytes:     8,
			wantRemoved:  []string{"oldest", "middle"},
			wantRetained: []string{"current", "newest"},
		},
		{
			name:         "reserved next operation",
			maxFiles:     4,
			maxBytes:     16,
			reserveFiles: 1,
			reserveBytes: 4,
			wantRemoved:  []string{"oldest"},
			wantRetained: []string{"current", "middle", "newest"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			logDir := t.TempDir()
			baseTime := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
			for index, id := range []string{"current", "oldest", "middle", "newest"} {
				path := filepath.Join(logDir, operationLogFilename(id))
				require.NoError(t, os.WriteFile(path, []byte("1234"), 0o600))
				modified := baseTime.Add(time.Duration(index) * time.Minute)
				require.NoError(t, os.Chtimes(path, modified, modified))
			}

			require.NoError(t, pruneOperationLogs(
				logDir,
				operationLogFilename("current"),
				test.maxFiles,
				test.maxBytes,
				test.reserveFiles,
				test.reserveBytes,
			))

			for _, id := range test.wantRemoved {
				assert.NoFileExists(t, filepath.Join(logDir, operationLogFilename(id)))
			}
			for _, id := range test.wantRetained {
				assert.FileExists(t, filepath.Join(logDir, operationLogFilename(id)))
			}
		})
	}
}

func TestPruneOperationLogsIgnoresUnownedAndNonRegularEntries(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	unrelated := filepath.Join(logDir, "operator-notes.log")
	require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0o600))

	external := filepath.Join(t.TempDir(), "external.log")
	require.NoError(t, os.WriteFile(external, []byte("keep"), 0o600))
	symlinkPath := filepath.Join(logDir, operationLogFilename("symlink"))
	require.NoError(t, os.Symlink(external, symlinkPath))

	directoryPath := filepath.Join(logDir, operationLogFilename("directory"))
	require.NoError(t, os.Mkdir(directoryPath, 0o700))
	sentinel := filepath.Join(directoryPath, "sentinel")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o600))

	require.NoError(t, pruneOperationLogs(logDir, "", 0, 0, 0, 0))

	assert.FileExists(t, unrelated)
	_, err := os.Lstat(symlinkPath)
	assert.NoError(t, err)
	assert.Equal(t, "keep", mustReadFile(t, external))
	assert.FileExists(t, sentinel)
}

func TestSaturatingLogBytesCannotOverflowBelowTheQuota(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(11), saturatingLogBytes(9, 2, 10))
	assert.Equal(t, int64(11), saturatingLogBytes(0, int64(^uint64(0)>>1), 10))
}

func TestManagerRejectsTriggerWhenCurrentLogConsumesReservedCapacity(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	logDir := filepath.Join(stateDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0o700))
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	operation := updaterapi.Operation{
		ID:            "current-operation",
		TargetVersion: "v1.0.0",
		Phase:         updaterapi.PhaseSucceeded,
		StartedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   &now,
	}
	operation.LogPath = filepath.Join(logDir, operationLogFilename(operation.ID))
	data, err := json.Marshal(operation)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, stateFilename), data, 0o600))
	require.NoError(t, os.WriteFile(operation.LogPath, nil, 0o600))
	require.NoError(t, os.Truncate(
		operation.LogPath,
		maxRetainedLogBytes-maxCommandLogBytes+1,
	))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	_, err = manager.Trigger("v1.1.0")
	require.ErrorContains(t, err, "reserve updater operation log capacity")
	require.ErrorContains(t, err, "current updater operation log leaves insufficient retained capacity")
	assert.Equal(t, updaterapi.PhaseSucceeded, manager.Status().Operation.Phase)
	assert.FileExists(t, operation.LogPath)
}

func TestManagerPreservesTerminalStatusWhenQueuedStateCannotPersist(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	logDir := filepath.Join(stateDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0o700))
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	previous := updaterapi.Operation{
		ID:            "previous-operation",
		TargetVersion: "v1.0.0",
		Phase:         updaterapi.PhaseSucceeded,
		StartedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   &now,
	}
	previous.LogPath = filepath.Join(logDir, operationLogFilename(previous.ID))
	data, err := json.Marshal(previous)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, stateFilename), data, 0o600))
	require.NoError(t, os.WriteFile(previous.LogPath, []byte("previous log"), 0o600))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	// #nosec G302 -- these are restrictive directory modes used to force a
	// persistence failure; no file is made group/world accessible.
	require.NoError(t, os.Chmod(stateDir, 0o500))
	t.Cleanup(func() {
		// #nosec G302 -- restore owner-only directory access for TempDir cleanup.
		_ = os.Chmod(stateDir, 0o700)
	})

	_, err = manager.Trigger("v1.1.0")
	require.ErrorContains(t, err, "create updater state temp file")
	status := manager.Status().Operation
	require.NotNil(t, status)
	assert.Equal(t, previous.ID, status.ID)
	assert.Equal(t, updaterapi.PhaseSucceeded, status.Phase)
}

func TestManagerPostCompletionLogPruneFailureDoesNotChangeTerminalState(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	stateDir := filepath.Join(t.TempDir(), "state")
	operationID := "oversized-current-log"
	runner := &recordingRunner{
		activationHook: func(string) {
			require.NoError(t, os.Truncate(
				filepath.Join(stateDir, "logs", operationLogFilename(operationID)),
				maxRetainedLogBytes+1,
			))
		},
	}
	manager, err := NewManager(Config{
		InstallRoot:              installRoot,
		StateDir:                 stateDir,
		DownloadBaseURL:          server.URL,
		HTTPClient:               server.Client(),
		Runner:                   runner,
		GOARCH:                   "amd64",
		NewID:                    func() string { return operationID },
		allowTestDownloadBaseURL: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	_, err = manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)
	require.Eventually(t, func() bool {
		manager.mu.RLock()
		defer manager.mu.RUnlock()
		return !manager.operationRunning
	}, 5*time.Second, 10*time.Millisecond)

	assert.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	info, err := os.Stat(completed.LogPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), maxRetainedLogBytes)
	assert.Equal(t, updaterapi.PhaseSucceeded, manager.Status().Operation.Phase)
}

func TestManagerCleansDeferredArtifactsBeforeAcceptingAnotherUpgrade(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	manager := newTestManager(t, installRoot, server, &recordingRunner{})
	staleArtifact := filepath.Join(manager.cfg.StateDir, operationArtifactBase("previous-operation")+".updater")
	require.NoError(t, os.WriteFile(staleArtifact, []byte("stale"), 0o600))

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)
	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	assert.NoFileExists(t, staleArtifact)
}

func TestManagerStaleArtifactCleanupDoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	externalRoot := t.TempDir()
	externalFile := filepath.Join(externalRoot, "updater")
	externalStageFile := filepath.Join(externalRoot, "deployment-data")
	require.NoError(t, os.WriteFile(externalFile, []byte("keep"), 0o600))
	require.NoError(t, os.WriteFile(externalStageFile, []byte("keep"), 0o600))
	artifactBase := operationArtifactBase("symlink-operation")
	stateLink := filepath.Join(stateDir, artifactBase+".updater")
	stageLink := filepath.Join(installRoot, artifactBase)
	require.NoError(t, os.Symlink(externalFile, stateLink))
	require.NoError(t, os.Symlink(externalRoot, stageLink))

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	_, err = os.Lstat(stateLink)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Lstat(stageLink)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Equal(t, "keep", mustReadFile(t, externalFile))
	assert.Equal(t, "keep", mustReadFile(t, externalStageFile))
}

func TestManagerRefusesAStaleStateDirectoryInAFileSlot(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		operationArtifactBase("directory-operation") + ".updater",
		activationMarkerTempName,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			stateDir := filepath.Join(t.TempDir(), "state")
			unexpectedDirectory := filepath.Join(stateDir, name)
			require.NoError(t, os.MkdirAll(unexpectedDirectory, 0o700))
			sentinel := filepath.Join(unexpectedDirectory, "evidence")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o600))

			_, err := NewManager(Config{
				InstallRoot: installRoot,
				StateDir:    stateDir,
				GOARCH:      "amd64",
			})
			require.ErrorContains(t, err, "state directory from file slot")
			assert.FileExists(t, sentinel)
		})
	}
}

func TestManagerLeavesStagingEvidenceWhenDeploymentCannotBeReconciled(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	artifactBase := operationArtifactBase("unrecoverable-operation")
	stageRoot := filepath.Join(installRoot, artifactBase)
	require.NoError(t, os.MkdirAll(stageRoot, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stageRoot, "evidence"), []byte("keep"), 0o600))
	stateDir := filepath.Join(t.TempDir(), "state")

	_, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.ErrorContains(t, err, "active deployment is missing without a pending activation swap")
	assert.FileExists(t, filepath.Join(stageRoot, "evidence"))
}

func TestManagerKeepsForwardDeploymentAfterInterruptedActivation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name             string
		withProof        bool
		withSwapMarker   bool
		wantRecoveryHint bool
	}{
		{name: "swap pending and preflight proof remains", withProof: true, withSwapMarker: true, wantRecoveryHint: true},
		{name: "swap completed and preflight proof remains", withProof: true, withSwapMarker: false, wantRecoveryHint: true},
		{name: "preflight proof was consumed", withProof: false, withSwapMarker: false, wantRecoveryHint: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.1.0")
			if test.withProof {
				require.NoError(t, os.WriteFile(
					filepath.Join(installRoot, "deployment", ".update-preflight-complete"),
					[]byte("proof\n"),
					0o600,
				))
			}
			stateDir := filepath.Join(t.TempDir(), "state")
			writeInterruptedOperationState(t, stateDir, "v1.1.0")
			if !test.withSwapMarker {
				require.NoError(t, os.Remove(filepath.Join(stateDir, activationMarkerFilename)))
			}

			manager, err := NewManager(Config{
				InstallRoot: installRoot,
				StateDir:    stateDir,
				GOARCH:      "amd64",
			})
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, manager.Close()) })

			operation := manager.Status().Operation
			require.NotNil(t, operation)
			assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
			assert.Equal(t, "v1.1.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
			if test.wantRecoveryHint {
				assert.Contains(t, operation.RecoveryCommand, "--skip-build")
			} else {
				assert.Empty(t, operation.RecoveryCommand)
			}
		})
	}
}

func TestManagerRejectsUnrecoverableInterruptedActivationLayout(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), "state")
	writeInterruptedOperationState(t, stateDir, "v1.1.0")

	_, err := NewManager(Config{
		InstallRoot: t.TempDir(),
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.ErrorContains(t, err, "both deployment and deployment.previous are missing")
}

func TestManagerRejectsWritableStateDirectory(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "group writable", mode: 0o770},
		{name: "world writable", mode: 0o707},
		{name: "sticky world writable", mode: os.ModeSticky | 0o707},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stateDir := filepath.Join(t.TempDir(), "state")
			require.NoError(t, os.Mkdir(stateDir, 0o700))
			require.NoError(t, os.Chmod(stateDir, test.mode))

			_, err := NewManager(Config{
				InstallRoot: t.TempDir(),
				StateDir:    stateDir,
				GOARCH:      "amd64",
			})
			require.ErrorContains(t, err, "state directory must not be group- or world-writable")
		})
	}
}

func TestManagerRejectsWritableStateDirectoryAncestor(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "group writable", mode: 0o770},
		{name: "world writable", mode: 0o707},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			unsafeAncestor := filepath.Join(t.TempDir(), "unsafe")
			require.NoError(t, os.Mkdir(unsafeAncestor, 0o700))
			require.NoError(t, os.Chmod(unsafeAncestor, test.mode))
			t.Cleanup(func() {
				// #nosec G302 -- directories require execute permission for cleanup.
				assert.NoError(t, os.Chmod(unsafeAncestor, 0o700))
			})

			_, err := NewManager(Config{
				InstallRoot: t.TempDir(),
				StateDir:    filepath.Join(unsafeAncestor, "nested", "state"),
				GOARCH:      "amd64",
			})
			require.ErrorContains(t, err, "state ancestor is group- or world-writable without the sticky bit")
		})
	}
}

func TestManagerAllowsStickySharedStateDirectoryAncestor(t *testing.T) {
	t.Parallel()

	sharedAncestor := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(sharedAncestor, 0o700))
	require.NoError(t, os.Chmod(sharedAncestor, os.ModeSticky|0o777))
	t.Cleanup(func() {
		// #nosec G302 -- directories require execute permission for cleanup.
		assert.NoError(t, os.Chmod(sharedAncestor, 0o700))
	})
	stateDir := filepath.Join(sharedAncestor, "owned", "state")
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	info, err := os.Stat(stateDir)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o077)
}

func TestManagerRejectsStateDirectorySymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	stateDir := filepath.Join(root, "state")
	require.NoError(t, os.Symlink(target, stateDir))

	_, err := NewManager(Config{
		InstallRoot: t.TempDir(),
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.ErrorContains(t, err, "state directory must not be a symlink")
}

func TestManagerCanonicalizesTrustedStateDirectorySymlinkAncestor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(realParent, 0o700))
	linkedParent := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(realParent, linkedParent))
	stateDir := filepath.Join(linkedParent, "state")
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")

	manager, err := NewManager(Config{
		InstallRoot: installRoot,
		StateDir:    stateDir,
		GOARCH:      "amd64",
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })

	canonicalStateDir, err := filepath.EvalSymlinks(stateDir)
	require.NoError(t, err)
	assert.Equal(t, canonicalStateDir, manager.cfg.StateDir)
}

func TestManagerRejectsUnsafeDownloadBaseURLs(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"http://example.com/releases",
		"https://",
		"https://user@example.com/releases",
		"https://example.com/releases?channel=test",
		"https://example.com/releases#fragment",
	} {
		_, err := NewManager(Config{
			InstallRoot:              t.TempDir(),
			StateDir:                 filepath.Join(t.TempDir(), "state"),
			DownloadBaseURL:          rawURL,
			GOARCH:                   "amd64",
			allowTestDownloadBaseURL: true,
		})
		require.Error(t, err, rawURL)
	}
}

func TestManagerRejectsProductionDownloadBaseOverride(t *testing.T) {
	t.Parallel()

	_, err := NewManager(Config{
		InstallRoot:     t.TempDir(),
		StateDir:        filepath.Join(t.TempDir(), "state"),
		DownloadBaseURL: "https://mirror.example.com/proto-fleet",
		GOARCH:          "amd64",
	})
	require.ErrorContains(t, err, "official GitHub Releases URL")
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	t.Parallel()

	archive := filepath.Join(t.TempDir(), "unsafe.tar.gz")
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}))
	_, err := tarWriter.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, os.WriteFile(archive, buffer.Bytes(), 0o600))

	err = extractArchive(archive, t.TempDir())
	require.ErrorContains(t, err, "unsafe path")
}

func TestInstallExecutableCandidateRetainsAndRestoresThePreviousUpdater(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "staged-updater")
	destination := filepath.Join(root, "installed", "proto-fleet-updater")
	require.NoError(t, os.WriteFile(source, []byte("new updater"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o750))
	require.NoError(t, os.WriteFile(destination, []byte("old updater"), 0o755))

	in, err := os.Open(source)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, in.Close()) })
	candidatePath, err := stageExecutableCandidate(in, destination)
	require.NoError(t, err)
	require.NoError(t, installExecutableCandidate(candidatePath, destination))
	assert.Equal(t, "new updater", mustReadFile(t, destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination+selfUpdateBackupSuffix))
	info, err := os.Stat(destination)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111)

	require.NoError(t, restorePreviousExecutable(destination))
	assert.Equal(t, "old updater", mustReadFile(t, destination))
	assert.NoFileExists(t, destination+selfUpdateBackupSuffix)
}

func TestValidateUpdaterCandidateRejectsAnUnrunnablePayload(t *testing.T) {
	t.Parallel()

	candidatePath := filepath.Join(t.TempDir(), "proto-fleet-updater.candidate")
	require.NoError(t, os.WriteFile(candidatePath, []byte("not an executable format"), 0o755))
	manager := &Manager{cfg: Config{Runner: execRunner{}}}

	err := manager.validateUpdaterCandidate(context.Background(), candidatePath, "v1.1.0")
	require.ErrorContains(t, err, "smoke-test updater candidate")
}

func TestActivationRestoreCommandQuotesEveryPathUse(t *testing.T) {
	t.Parallel()

	current := "/opt/proto fleet/deploy'ment"
	previous := "/opt/proto fleet/previous'copy"
	command := activationRestoreCommand(current, previous)

	assert.Contains(t, command, "test ! -e "+shellQuote(current))
	assert.Contains(t, command, "test ! -L "+shellQuote(current))
	assert.Contains(t, command, "test -d "+shellQuote(previous))
	assert.Contains(t, command, "mv -- "+shellQuote(previous)+" "+shellQuote(current))
}

func TestCappedWriterDiscardsExcessWhileLeavingTheLogWritable(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	commandOutput := newCappedWriter(&log, 4)
	written, err := commandOutput.Write([]byte("abcdefgh"))
	require.NoError(t, err)
	assert.Equal(t, 8, written)
	written, err = commandOutput.Write([]byte("discarded"))
	require.NoError(t, err)
	assert.Equal(t, len("discarded"), written)
	_, err = io.WriteString(&log, "terminal message\n")
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(log.String(), "command output truncated"))
	assert.Contains(t, log.String(), "abcd")
	assert.NotContains(t, log.String(), "efgh")
	assert.Contains(t, log.String(), "terminal message")
}

func TestExecRunnerCancellationKillsBackgroundDescendants(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	childStarted := filepath.Join(directory, "child-started")
	delayedMarker := filepath.Join(directory, "delayed-marker")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- (execRunner{}).Run(
			ctx,
			directory,
			io.Discard,
			"/bin/bash",
			"-c",
			`(printf child-started > "$1"; sleep 0.25; printf survived > "$2") & wait`,
			"bash",
			childStarted,
			delayedMarker,
		)
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(childStarted)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "background child never started")

	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("exec runner did not return after cancellation")
	}
	time.Sleep(350 * time.Millisecond)
	assert.NoFileExists(t, delayedMarker, "background descendant survived command cancellation")
}

func newTestManager(t *testing.T, installRoot string, server *httptest.Server, runner CommandRunner) *Manager {
	t.Helper()
	return newTestManagerWithConfig(t, installRoot, server, runner, nil)
}

func newTestManagerWithConfig(
	t *testing.T,
	installRoot string,
	server *httptest.Server,
	runner CommandRunner,
	configure func(*Config),
) *Manager {
	t.Helper()
	cfg := Config{
		InstallRoot:              installRoot,
		StateDir:                 filepath.Join(t.TempDir(), "state"),
		DownloadBaseURL:          server.URL,
		HTTPClient:               server.Client(),
		Runner:                   runner,
		GOARCH:                   "amd64",
		NewID:                    func() string { return "test-operation" },
		allowTestDownloadBaseURL: true,
	}
	if configure != nil {
		configure(&cfg)
	}
	manager, err := NewManager(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, manager.Close()) })
	return manager
}

func releaseServer(t *testing.T, version, arch string, bundle []byte, checksumOverride string) *httptest.Server {
	t.Helper()
	archiveName := fmt.Sprintf("proto-fleet-%s-%s.tar.gz", version, arch)
	checksum := fmt.Sprintf("%x", sha256.Sum256(bundle))
	if checksumOverride != "" {
		checksum = checksumOverride
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+version+"/"+archiveName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bundle)
	})
	mux.HandleFunc("/"+version+"/"+archiveName+".sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, archiveName)
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	return server
}

func releaseBundle(t *testing.T, version string) []byte {
	t.Helper()
	files := map[string]string{
		"deployment/version.txt":                         "version: " + version + "\n",
		"deployment/docker-compose.yaml":                 "services: {}\n",
		"deployment/run-fleet.sh":                        "#!/usr/bin/env bash\n",
		"deployment/server/fleetd":                       "fleetd",
		"deployment/server/proto-plugin":                 "plugin",
		"deployment/server/antminer-plugin":              "plugin",
		"deployment/server/asicrs-plugin":                "plugin",
		"deployment/updater/proto-fleet-updater":         "updater",
		"deployment/updater/proto-fleet-updater.service": "[Service]\n",
	}
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, contents := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, "run-fleet.sh") || strings.HasSuffix(name, "fleetd") || strings.HasSuffix(name, "-plugin") || strings.HasSuffix(name, "proto-fleet-updater") {
			mode = 0o755
		}
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(contents)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tarWriter.Write([]byte(contents))
		require.NoError(t, err)
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return buffer.Bytes()
}

func writeInterruptedOperationState(t *testing.T, stateDir, targetVersion string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	started := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, stateFilename),
		[]byte(fmt.Sprintf(
			`{"id":"interrupted","target_version":%q,"phase":"activating","started_at":%q,"updated_at":%q,"recovery_command":"stale"}`,
			targetVersion,
			started.Format(time.RFC3339Nano),
			started.Format(time.RFC3339Nano),
		)),
		0o600,
	))
	marker, err := json.Marshal(activationMarker{
		OperationID:   "interrupted",
		TargetVersion: targetVersion,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, activationMarkerFilename),
		marker,
		0o600,
	))
}

func writeCurrentDeployment(t *testing.T, installRoot, version string) {
	t.Helper()
	files := map[string]string{
		"version.txt":               "version: " + version + "\n",
		".env":                      operatorEnv,
		"ssl/cert.pem":              "certificate\n",
		"server/influx_config/.env": "influx-secret\n",
	}
	for name, contents := range files {
		path := filepath.Join(installRoot, "deployment", name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}
}

func waitForTerminal(t *testing.T, manager *Manager) updaterapi.Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status()
		if status.Operation != nil && status.Operation.Phase.Terminal() {
			return *status.Operation
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("upgrade did not reach a terminal state")
	return updaterapi.Operation{}
}

func mustReadVersion(t *testing.T, path string) string {
	t.Helper()
	version, err := readInstalledVersion(path)
	require.NoError(t, err)
	return version
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
