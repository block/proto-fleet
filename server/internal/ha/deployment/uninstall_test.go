package deployment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUninstallDatabaseNodePreservesPersistentState(t *testing.T) {
	// Arrange
	var calls []string
	output := &bytes.Buffer{}
	deps := testUninstallDependencies(t, testUninstallNodeConfig("ha-a"), &calls)
	deps.output = output

	// Act
	err := uninstall(t.Context(), false, deps)

	// Assert
	require.NoError(t, err)
	require.Contains(t, output.String(), "preserve")
	require.Contains(t, output.String(), configRoot)
	require.Contains(t, output.String(), dataRoot)
	requireCallOrder(t, calls,
		"systemctl disable --now proto-fleet-updater.service",
		"flock -n "+updaterLock+" true",
		"systemctl disable --now keepalived.service",
		"ip address flush to "+testVirtualIP+"/32 dev eth0",
		"systemctl disable --now proto-fleet-ha.service",
		"stop-installed-services",
	)
	requireCallOrder(t, calls,
		"rm -f -- "+dockerDropIn+" "+dockerRecoveryDropIn,
		"systemctl disable --now proto-fleet-ha-firewall.service",
		"nft delete table inet proto_fleet_ha",
	)

	joined := strings.Join(calls, "\n")
	require.NotContains(t, joined, "rm -rf -- "+dataRoot)
	require.NotContains(t, joined, "rm -rf -- "+configRoot)
	require.NotContains(t, joined, "--volumes")
	require.NotContains(t, joined, "--rmi")
	require.NotContains(t, joined, "apt-get")
	require.NotContains(t, joined, "systemctl disable --now docker")
}

func TestUninstallPurgeDeletesPersistentStateLast(t *testing.T) {
	// Arrange
	var calls []string
	output := &bytes.Buffer{}
	deps := testUninstallDependencies(t, testUninstallNodeConfig("ha-b"), &calls)
	deps.output = output

	// Act
	err := uninstall(t.Context(), true, deps)

	// Assert
	require.NoError(t, err)
	require.Contains(t, output.String(), "permanently delete")
	cleanup := callIndex(calls, "rm -rf -- "+installBase)
	data := callIndex(calls, "rm -rf -- "+dataRoot)
	config := callIndex(calls, "rm -rf -- "+configRoot)
	require.Greater(t, data, cleanup)
	require.Greater(t, config, data)
	require.NotContains(t, strings.Join(calls, "\n"), "systemctl reset-failed")
}

func TestUninstallWitnessSkipsDatabaseServices(t *testing.T) {
	// Arrange
	var calls []string
	deps := testUninstallDependencies(t, testUninstallNodeConfig("ha-c"), &calls)
	deps.input = strings.NewReader("uninstall\n")

	// Act
	err := uninstall(t.Context(), true, deps)

	// Assert
	require.NoError(t, err)
	joined := strings.Join(calls, "\n")
	require.NotContains(t, joined, "proto-fleet-updater.service")
	require.NotContains(t, joined, "keepalived.service")
	require.NotContains(t, joined, "ip address flush")
	require.Contains(t, joined, "stop-installed-services")
	require.Contains(t, joined, "rm -rf -- "+dataRoot)
}

func TestUninstallFailureDoesNotDeletePersistentState(t *testing.T) {
	tests := []struct {
		name    string
		fail    func(*uninstallDependencies, *[]string)
		message string
	}{
		{name: "Compose shutdown", fail: func(deps *uninstallDependencies, calls *[]string) {
			deps.stopServices = func(context.Context, string) error {
				*calls = append(*calls, "stop-installed-services")
				return errors.New("compose failed")
			}
		}, message: "compose failed"},
		{name: "firewall cleanup", fail: func(deps *uninstallDependencies, _ *[]string) {
			run := deps.run
			deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				output, err := run(ctx, name, args...)
				if name == "nft" {
					return []byte("permission denied"), errors.New("exit status 1")
				}
				return output, err
			}
		}, message: "remove HA firewall table"},
		{name: "updater still running", fail: func(deps *uninstallDependencies, _ *[]string) {
			run := deps.run
			deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				output, err := run(ctx, name, args...)
				if name == "flock" {
					return nil, errors.New("exit status 1")
				}
				return output, err
			}
		}, message: "verify host updater stopped"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			var calls []string
			deps := testUninstallDependencies(t, testUninstallNodeConfig("ha-a"), &calls)
			test.fail(&deps, &calls)

			// Act
			err := uninstall(t.Context(), true, deps)

			// Assert
			require.ErrorContains(t, err, test.message)
			joined := strings.Join(calls, "\n")
			require.NotContains(t, joined, "rm -rf -- "+dataRoot)
			require.NotContains(t, joined, "rm -rf -- "+configRoot)
		})
	}
}

func TestUninstallRejectsUnsafeInvocationBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*uninstallDependencies)
		message string
		calls   []string
	}{
		{name: "not root", mutate: func(deps *uninstallDependencies) { deps.euid = func() int { return 1000 } }, message: "requires root"},
		{name: "not interactive", mutate: func(deps *uninstallDependencies) { deps.terminal = func() bool { return false } }, message: "interactive terminal"},
		{name: "wrong data path", mutate: func(deps *uninstallDependencies) {
			deps.loadConfig = func(string) (NodeConfig, error) {
				config := testUninstallNodeConfig("ha-a")
				config.DataDir = "/tmp/data"
				return config, nil
			}
		}, message: "HA_DATA_DIR"},
		{name: "missing updater", mutate: func(deps *uninstallDependencies) {
			lstat := deps.lstat
			deps.lstat = func(path string) (os.FileInfo, error) {
				if path == updaterUnit {
					return nil, os.ErrNotExist
				}
				return lstat(path)
			}
		}, message: "installation is incomplete"},
		{name: "confirmation refused", mutate: func(deps *uninstallDependencies) { deps.input = strings.NewReader("no\n") }, message: "uninstall canceled", calls: []string{"docker --host " + localDockerHost + " info"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			var calls []string
			deps := testUninstallDependencies(t, testUninstallNodeConfig("ha-a"), &calls)
			test.mutate(&deps)

			// Act
			err := uninstall(t.Context(), true, deps)

			// Assert
			require.ErrorContains(t, err, test.message)
			require.Equal(t, test.calls, calls)
		})
	}
}

func testUninstallDependencies(t *testing.T, config NodeConfig, calls *[]string) uninstallDependencies {
	t.Helper()
	file := t.TempDir() + "/regular"
	require.NoError(t, os.WriteFile(file, []byte("test"), 0o600))
	info, err := os.Stat(file)
	require.NoError(t, err)

	record := func(name string, args ...string) {
		*calls = append(*calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	}
	installed := map[string]bool{
		serviceUnit: true, firewallUnit: true, nftablesDropIn: true, dockerDropIn: true,
		infrastructureCompose: true, installRoot + "/ha/fleet-ha": true,
		keepalivedConfig: config.isDatabaseNode(), keepalivedOverride: config.isDatabaseNode(),
		keepalivedHealthCheck: config.isDatabaseNode(), updaterDropIn: config.isDatabaseNode(),
		haUpdaterDropIn: config.isDatabaseNode(), updaterBinary: config.isDatabaseNode(),
		updaterUnit: config.isDatabaseNode(), updaterEnvironment: config.isDatabaseNode(),
		updaterLock: config.isDatabaseNode(),
	}
	return uninstallDependencies{
		input: strings.NewReader("UNINSTALL\n"), output: io.Discard,
		euid: func() int { return 0 }, terminal: func() bool { return true },
		loadConfig: func(string) (NodeConfig, error) { return config, nil },
		lstat: func(path string) (os.FileInfo, error) {
			if installed[path] {
				return info, nil
			}
			return nil, os.ErrNotExist
		},
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			record(name, args...)
			return nil, nil
		},
		stopServices: func(context.Context, string) error {
			record("stop-installed-services")
			return nil
		},
	}
}

func testUninstallNodeConfig(role string) NodeConfig {
	nodeIP := map[string]string{"ha-a": testHostIPs[0], "ha-b": testHostIPs[1], "ha-c": testHostIPs[2]}[role]
	return NodeConfig{
		NodeName: role, NodeIP: nodeIP, DatabaseAIP: testHostIPs[0], DatabaseBIP: testHostIPs[1],
		WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP, NetworkInterface: "eth0",
		DataDir: dataRoot, SecretsDir: configRoot,
	}
}

func requireCallOrder(t *testing.T, calls []string, expected ...string) {
	t.Helper()
	previous := -1
	for _, command := range expected {
		index := callIndex(calls, command)
		require.NotEqual(t, -1, index, "missing command %q", command)
		require.Greater(t, index, previous, "command %q ran out of order", command)
		previous = index
	}
}
