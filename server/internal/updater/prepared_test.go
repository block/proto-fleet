package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/block/proto-fleet/server/internal/updaterapi"
	"github.com/stretchr/testify/require"
)

func TestPreparedHAUpdateSurvivesRestartWithoutRepeatingPreparation(t *testing.T) {
	// Arrange
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	runner := &haRecordingRunner{}
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0755))
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA; cfg.SelfUpdatePath = installedUpdater })

	// Act
	_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
	require.NoError(t, err)
	result := waitPreparedOperation(t, manager)

	// Assert
	require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
	require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
	require.Len(t, runner.Commands(), 4)
	require.Equal(t, "old updater", mustReadFile(t, installedUpdater))

	// Arrange
	require.NoError(t, manager.Close())
	server.Close() // Activation must not fetch the release again.
	restarted, err := NewManager(manager.cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })

	// Act
	_, err = restarted.TriggerPreparedWithID("v1.1.0", "22222222-2222-4222-8222-222222222222", false, true, false)
	require.NoError(t, err)
	result = waitPreparedOperation(t, restarted)

	// Assert
	require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
	require.Equal(t, "v1.1.0", mustInstalledVersion(t, installRoot))
	require.Equal(t, "updater", mustReadFile(t, installedUpdater))
	count := 0
	for _, command := range runner.Commands() {
		if command.Args[0] == "update-preflight" {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestPreparedHAUpdateRefusesMissingStaleOrTamperedPreparation(t *testing.T) {
	for _, tc := range []struct {
		mutation string
		wantErr  string
	}{
		{mutation: "missing", wantErr: "prepared release is unavailable"},
		{mutation: "source", wantErr: "source configuration is stale"},
		{mutation: "staged", wantErr: "content changed"},
		{mutation: "target", wantErr: "does not match requested target and repository"},
	} {
		mutation := tc.mutation
		t.Run(mutation, func(t *testing.T) {
			// Arrange
			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
			runner := &haRecordingRunner{}
			manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })
			if mutation != "missing" {
				_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
				require.NoError(t, err)
				result := waitPreparedOperation(t, manager)
				require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
			}
			if mutation == "source" {
				require.NoError(t, os.WriteFile(filepath.Join(installRoot, "deployment", ".env"), []byte("changed"), 0600))
			}
			if mutation == "staged" {
				require.NoError(t, os.WriteFile(filepath.Join(installRoot, preparedDirectoryName, "deployment", "ha", "fleet-ha"), []byte("tampered"), 0600))
			}
			target := "v1.1.0"
			if mutation == "target" {
				target = "v1.2.0"
			}

			// Act
			_, err := manager.TriggerPreparedWithID(target, "22222222-2222-4222-8222-222222222222", false, true, false)
			require.NoError(t, err)
			result := waitPreparedOperation(t, manager)

			// Assert
			require.Equal(t, updaterapi.PhaseFailed, result.Phase)
			require.Contains(t, result.Error, tc.wantErr)
			require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
			for _, command := range runner.Commands() {
				require.NotEqual(t, "app-stop", command.Args[0])
			}
		})
	}
}

func mustInstalledVersion(t *testing.T, root string) string {
	t.Helper()
	version, err := readInstalledVersion(filepath.Join(root, "deployment", "version.txt"))
	require.NoError(t, err)
	return version
}

func TestPreparedHAUpdateRejectsTargetWithoutProtocolBeforeStopping(t *testing.T) {
	// Arrange
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	runner := &haRecordingRunner{capabilities: []string{}}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })

	// Act
	_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
	require.NoError(t, err)
	result := waitPreparedOperation(t, manager)

	// Assert
	require.Equal(t, updaterapi.PhaseFailed, result.Phase)
	require.Contains(t, result.Error, "does not support prepared-update-v1")
	require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
	require.Len(t, runner.Commands(), 1)
}

func TestPreparedHAUpdateRequiresFreshSchemaImagesAndRole(t *testing.T) {
	for _, failure := range []string{"schema-check", "check-images", "require-passive"} {
		t.Run(failure, func(t *testing.T) {
			// Arrange
			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
			runner := &haRecordingRunner{}
			manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })
			_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
			require.NoError(t, err)
			require.Equal(t, updaterapi.PhaseSucceeded, waitPreparedOperation(t, manager).Phase)
			runner.mu.Lock()
			runner.fail = map[string]error{failure: os.ErrNotExist}
			runner.mu.Unlock()

			// Act
			_, err = manager.TriggerPreparedWithID("v1.1.0", "22222222-2222-4222-8222-222222222222", false, true, false)
			require.NoError(t, err)
			phase := waitPreparedOperation(t, manager).Phase

			// Assert
			require.Equal(t, updaterapi.PhaseFailed, phase)
			require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
			for _, command := range runner.Commands() {
				require.NotEqual(t, "app-stop", command.Args[0])
			}
		})
	}
}

func waitPreparedOperation(t *testing.T, manager *Manager) updaterapi.Operation {
	t.Helper()
	operation := waitForTerminal(t, manager)
	manager.operationWG.Wait()
	return operation
}

func TestPreparedHAUpdatePublishesOnlyDurableUnchangedConfiguration(t *testing.T) {
	for _, failure := range []string{"durability", "configuration"} {
		t.Run(failure, func(t *testing.T) {
			// Arrange
			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
			runner := &haRecordingRunner{}
			manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
				cfg.DeploymentMode = DeploymentModeHA
				cfg.syncStagedDeployment = func(ctx context.Context, stage string) error {
					require.FileExists(t, filepath.Join(stage, "ha", "fleet-ha"))
					if failure == "durability" {
						return os.ErrPermission
					}
					if err := os.WriteFile(filepath.Join(installRoot, "deployment", ".env"), []byte("changed while preparing"), 0600); err != nil {
						return fmt.Errorf("change source configuration: %w", err)
					}
					return syncStagedDeployment(ctx, stage)
				}
			})

			// Act
			_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
			require.NoError(t, err)
			phase := waitPreparedOperation(t, manager).Phase

			// Assert
			require.Equal(t, updaterapi.PhaseFailed, phase)
			_, err = os.Stat(filepath.Join(installRoot, preparedDirectoryName))
			require.ErrorIs(t, err, os.ErrNotExist)
			require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
			for _, command := range runner.Commands() {
				require.NotEqual(t, "app-stop", command.Args[0])
			}
		})
	}
}

func TestPreparedHACompletionUsesPreparedStateAndRecoversFailedTakeover(t *testing.T) {
	for _, failedTakeover := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "takeover failed"}[failedTakeover], func(t *testing.T) {
			// Arrange
			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
			runner := &haRecordingRunner{}
			manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })
			_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
			require.NoError(t, err)
			require.Equal(t, updaterapi.PhaseSucceeded, waitPreparedOperation(t, manager).Phase)
			server.Close()
			if failedTakeover {
				runner.mu.Lock()
				runner.fail = map[string]error{"wait-takeover": os.ErrDeadlineExceeded}
				runner.mu.Unlock()
			}

			// Act
			_, err = manager.TriggerPreparedWithID("v1.1.0", "22222222-2222-4222-8222-222222222222", false, true, true)
			require.NoError(t, err)
			result := waitPreparedOperation(t, manager)

			// Assert
			require.True(t, result.Prepared)
			require.True(t, result.Complete)
			commands := runner.Commands()
			preflights := 0
			var activation [][]string
			for _, command := range commands {
				if command.Args[0] == "update-preflight" {
					preflights++
				}
				switch command.Args[0] {
				case "require-active", "app-stop", "wait-takeover", "app-start":
					activation = append(activation, command.Args)
				}
			}
			require.Equal(t, 1, preflights)
			wantStart := []string{"app-start", "v1.1.0", "complete"}
			if failedTakeover {
				require.Equal(t, updaterapi.PhaseFailed, result.Phase)
				require.Contains(t, result.Error, "previous release restarted")
				require.Empty(t, result.RecoveryCommand)
				require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
				wantStart = []string{"app-start", "v1.0.0", "any"}
			} else {
				require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
				require.Equal(t, "v1.1.0", mustInstalledVersion(t, installRoot))
			}
			require.Equal(t, [][]string{
				{"require-active", "/etc/proto-fleet/ha/node.env", "v1.1.0"},
				{"app-stop", "active"},
				{"wait-takeover", "v1.1.0"},
				wantStart,
			}, activation)
		})
	}
}

func TestPreparedHAActivationRetryReusesPreparationAfterFailedTakeover(t *testing.T) {
	// Arrange
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	runner := &haRecordingRunner{}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })
	_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
	require.NoError(t, err)
	require.Equal(t, updaterapi.PhaseSucceeded, waitPreparedOperation(t, manager).Phase)
	server.Close() // A retry must not download the release again.
	runner.mu.Lock()
	runner.fail = map[string]error{"wait-takeover": os.ErrDeadlineExceeded}
	runner.mu.Unlock()
	_, err = manager.TriggerPreparedWithID("v1.1.0", "22222222-2222-4222-8222-222222222222", false, true, true)
	require.NoError(t, err)
	failed := waitPreparedOperation(t, manager)
	require.Equal(t, updaterapi.PhaseFailed, failed.Phase)
	require.Contains(t, failed.Error, "previous release restarted")
	require.DirExists(t, filepath.Join(installRoot, preparedDirectoryName))

	// Act
	_, err = manager.TriggerPreparedWithID("v1.1.0", "33333333-3333-4333-8333-333333333333", false, true, true)
	require.NoError(t, err)
	retried := waitPreparedOperation(t, manager)

	// Assert
	require.Equal(t, updaterapi.PhaseSucceeded, retried.Phase, retried.Error)
	require.Equal(t, "v1.1.0", mustInstalledVersion(t, installRoot))
	require.NoDirExists(t, filepath.Join(installRoot, preparedDirectoryName))
	preflights := 0
	for _, command := range runner.Commands() {
		if command.Args[0] == "update-preflight" {
			preflights++
		}
	}
	require.Equal(t, 1, preflights)
}

func TestHAUpdateRemovesAbandonedPreparedRelease(t *testing.T) {
	// Arrange
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	preparedRoot := filepath.Join(installRoot, preparedDirectoryName)
	require.NoError(t, os.Mkdir(preparedRoot, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(preparedRoot, "stale"), []byte("stale"), 0o600))
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	manager := newTestManagerWithConfig(t, installRoot, server, &haRecordingRunner{}, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })

	// Act
	_, err := manager.TriggerWithID("v1.1.0", "11111111-1111-4111-8111-111111111111")
	require.NoError(t, err)
	result := waitPreparedOperation(t, manager)

	// Assert
	require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
	require.NoDirExists(t, preparedRoot)
}
