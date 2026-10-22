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
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	runner := &haRecordingRunner{}
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0755))
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA; cfg.SelfUpdatePath = installedUpdater })
	_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
	require.NoError(t, err)
	result := waitPreparedOperation(t, manager)
	require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
	require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
	require.Len(t, runner.Commands(), 4)
	require.Equal(t, "old updater", mustReadFile(t, installedUpdater))
	require.NoError(t, manager.Close())
	server.Close() // Activation must not fetch the release again.
	restarted, err := NewManager(manager.cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	_, err = restarted.TriggerPreparedWithID("v1.1.0", "22222222-2222-4222-8222-222222222222", false, true, false)
	require.NoError(t, err)
	result = waitPreparedOperation(t, restarted)
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
	for _, mutation := range []string{"missing", "source", "staged", "target"} {
		t.Run(mutation, func(t *testing.T) {
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
			_, err := manager.TriggerPreparedWithID(target, "22222222-2222-4222-8222-222222222222", false, true, false)
			require.NoError(t, err)
			require.Equal(t, updaterapi.PhaseFailed, waitPreparedOperation(t, manager).Phase)
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
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	runner := &haRecordingRunner{capabilities: []string{}}
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.DeploymentMode = DeploymentModeHA })
	_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
	require.NoError(t, err)
	result := waitPreparedOperation(t, manager)
	require.Equal(t, updaterapi.PhaseFailed, result.Phase)
	require.Contains(t, result.Error, "does not support prepared-update-v1")
	require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
	require.Len(t, runner.Commands(), 1)
}

func TestPreparedHAUpdateRequiresFreshSchemaImagesAndRole(t *testing.T) {
	for _, failure := range []string{"schema-check", "check-images", "require-passive"} {
		t.Run(failure, func(t *testing.T) {
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
			_, err = manager.TriggerPreparedWithID("v1.1.0", "22222222-2222-4222-8222-222222222222", false, true, false)
			require.NoError(t, err)
			require.Equal(t, updaterapi.PhaseFailed, waitPreparedOperation(t, manager).Phase)
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
			_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
			require.NoError(t, err)
			require.Equal(t, updaterapi.PhaseFailed, waitPreparedOperation(t, manager).Phase)
			_, err = os.Stat(filepath.Join(installRoot, preparedDirectoryName))
			require.ErrorIs(t, err, os.ErrNotExist)
			require.Equal(t, "v1.0.0", mustInstalledVersion(t, installRoot))
			for _, command := range runner.Commands() {
				require.NotEqual(t, "app-stop", command.Args[0])
			}
		})
	}
}
