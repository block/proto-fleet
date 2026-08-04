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
}

const operatorEnv = `DB_PASSWORD=secret
ENABLE_BETA_ALERTS=true
ENABLE_SYSTEM_MONITORING=true
ENABLE_TRACING=true
ENABLE_ONE_CLICK_UPDATES=true
`

func (r *recordingRunner) Run(ctx context.Context, dir string, _ io.Writer, name string, args ...string) error {
	r.mu.Lock()
	r.commands = append(r.commands, recordedCommand{Dir: dir, Name: name, Args: append([]string(nil), args...)})
	r.mu.Unlock()

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

	_, err = NewManager(config)
	require.ErrorContains(t, err, "another updater process is already running")
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

	replacement, err := NewManager(config)
	require.NoError(t, err)
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
}

func TestManagerKeepsForwardDeploymentAfterInterruptedActivation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name             string
		withProof        bool
		wantRecoveryHint bool
	}{
		{name: "preflight proof remains", withProof: true, wantRecoveryHint: true},
		{name: "preflight proof was consumed", withProof: false, wantRecoveryHint: false},
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

func TestAtomicReplaceExecutableRefreshesTheInstalledUpdater(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "staged-updater")
	destination := filepath.Join(root, "installed", "proto-fleet-updater")
	require.NoError(t, os.WriteFile(source, []byte("new updater"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o750))
	require.NoError(t, os.WriteFile(destination, []byte("old updater"), 0o755))

	require.NoError(t, atomicReplaceExecutable(source, destination))
	assert.Equal(t, "new updater", mustReadFile(t, destination))
	info, err := os.Stat(destination)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111)
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
