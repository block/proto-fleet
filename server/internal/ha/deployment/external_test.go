package deployment

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExternalNodeEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.env")
	require.NoError(t, os.WriteFile(path, []byte(`HA_NODE_NAME=ha-a
HA_NODE_IP=10.0.1.10
HA_DB_A_IP=10.0.1.10
HA_DB_B_IP=10.0.2.10
HA_DCS_C_IP=10.0.3.10
HA_ENDPOINT_MODE=external
HA_PUBLIC_URL=https://fleet.example.com
HA_DATA_DIR=/var/lib/proto-fleet/ha
HA_SECRETS_DIR=/etc/proto-fleet/ha
`), 0o600))
	config, err := loadNodeConfig(path)
	require.NoError(t, err)
	require.NoError(t, validateNodeConfig(config))
	require.Equal(t, "https://fleet.example.com", config.publicURL())
	require.Equal(t, config.NodeIP, config.hostCertificateIdentity(config.NodeIP))
	require.Nil(t, config.publicTLS(nil).RootCAs, "public certificates must use system roots, not the private cluster CA")
	environment, err := composeEnvironment([]string{"--env-file", path})
	require.NoError(t, err)
	require.Contains(t, environment, "HA_ENDPOINT_NODE_IP=10.0.1.10")
	require.Contains(t, environment, "HA_ENDPOINT_MODE=external")
	rendered := renderNodeEnvironment(config)
	require.NotContains(t, rendered, "HA_VIRTUAL_IP")
	require.NoError(t, os.WriteFile(path, []byte(rendered), 0o600))
	roundTrip, err := loadNodeConfig(path)
	require.NoError(t, err)
	require.Equal(t, config, roundTrip)
}

func externalTestConfig() NodeConfig {
	return NodeConfig{EndpointMode: "external", PublicURL: "https://fleet.example.com", NodeName: "ha-a", NodeIP: "10.0.1.10",
		DatabaseAIP: "10.0.1.10", DatabaseBIP: "10.0.2.10", WitnessIP: "10.0.3.10", DataDir: dataRoot, SecretsDir: configRoot}
}

func TestExternalConfigurationRejectsAmbiguousRouting(t *testing.T) {
	for _, raw := range []string{"", "http://fleet.example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?x=1", "https://example.com#fragment", "https://example.com:8443"} {
		config := externalTestConfig()
		config.PublicURL = raw
		require.Error(t, validateNodeConfig(config), raw)
	}
	for _, change := range []func(*NodeConfig){
		func(c *NodeConfig) { c.VirtualIP = testVirtualIP },
		func(c *NodeConfig) { c.NetworkInterface = "eth0" },
		func(c *NodeConfig) { c.EndpointMode = "unknown" },
	} {
		config := externalTestConfig()
		change(&config)
		require.Error(t, validateNodeConfig(config))
	}
}

func TestExternalPreflightAllowsRoutedPeersWithoutVIPProbes(t *testing.T) {
	config := externalTestConfig()
	config.DataDir = t.TempDir()
	var commands []string
	host := hostEnvironment{
		goos: "linux", localIPs: func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(config.NodeIP)}, nil },
		interfacePrefixes: func(string) ([]netip.Prefix, error) {
			t.Fatal("external mode must not require a VIP interface")
			return nil, nil
		},
		runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if name == "ip" {
				return []byte(fmt.Sprintf("%s via 10.0.1.1 dev ens5 src %s", args[len(args)-1], config.NodeIP)), nil
			}
			if name == "ss" {
				return nil, nil
			}
			t.Fatalf("unexpected command %s", name)
			return nil, nil
		},
	}
	require.NoError(t, validateHostEnvironment(t.Context(), config, host, true, fleetApplicationProfile{}))
	require.Len(t, commands, 3)
	host.runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("10.0.2.10 via 10.0.1.1 dev ens5 src 10.0.1.99"), nil
	}
	require.ErrorContains(t, validateHostEnvironment(t.Context(), config, host, true, nil), "must use HA_NODE_IP")
}

func TestExternalPreparedSecretsUsePerHostCertificates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "prepared")
	config := externalTestConfig()
	t.Setenv("ENABLE_BETA_ALERTS", "false")
	t.Setenv("ENABLE_TRACING", "false")
	require.NoError(t, PrepareExternal(root, [3]string{config.DatabaseAIP, config.DatabaseBIP, config.WitnessIP}, config.PublicURL))
	var sharedEnvironment []byte
	for i, name := range []string{"ha-a", "ha-b", "ha-c"} {
		node, err := loadNodeConfig(filepath.Join(root, name, "node.env"))
		require.NoError(t, err)
		require.NoError(t, validateNodeConfig(node))
		_, err = validateSecrets(node)
		require.NoError(t, err)
		if i == 2 {
			continue
		}
		ca, err := os.ReadFile(filepath.Join(node.SecretsDir, "service-ca.crt"))
		require.NoError(t, err)
		roots := x509.NewCertPool()
		require.True(t, roots.AppendCertsFromPEM(ca))
		require.NoError(t, verifyEndpointCertificate(filepath.Join(node.SecretsDir, "fleet-client.crt"), node.NodeIP, roots, x509.ExtKeyUsageServerAuth))
		require.Error(t, verifyEndpointCertificate(filepath.Join(node.SecretsDir, "fleet-client.crt"), testVirtualIP, roots, x509.ExtKeyUsageServerAuth))
		environment, err := os.ReadFile(filepath.Join(node.SecretsDir, fleetEnvironmentFile))
		require.NoError(t, err)
		if i == 0 {
			sharedEnvironment = environment
		} else {
			require.Equal(t, sharedEnvironment, environment)
		}
	}
	require.ErrorContains(t, PrepareExternal(root, [3]string{config.DatabaseAIP, config.DatabaseBIP, config.WitnessIP}, config.PublicURL), "already exists")
}

func TestExternalInstallAndUninstallNeverManageKeepalived(t *testing.T) {
	config := externalTestConfig()
	var calls []string
	deps := testInstallerDependencies(testInstallRelease(t), config, &calls)
	deps.verifyVIP = func(context.Context, NodeConfig) error { t.Fatal("must not ARP probe"); return nil }
	require.NoError(t, install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps))
	joined := strings.Join(calls, "\n")
	require.NotContains(t, joined, "keepalived")
	require.NotContains(t, joined, "arping")
	require.Contains(t, joined, "artifacts/firmware")
	calls = nil
	uninstallDeps := testUninstallDependencies(t, config, &calls)
	require.NoError(t, uninstall(t.Context(), false, uninstallDeps))
	joined = strings.Join(calls, "\n")
	require.NotContains(t, joined, "keepalived")
	require.NotContains(t, joined, "ip address flush")
	require.NotContains(t, joined, "rm -rf -- "+dataRoot)
}

func TestExternalWitnessInstallationDoesNotWaitForPublicBootstrap(t *testing.T) {
	config := externalTestConfig()
	config.NodeName, config.NodeIP = "ha-c", config.WitnessIP
	var calls []string
	deps := testInstallerDependencies(testInstallRelease(t), config, &calls)
	deps.vipReady = func(context.Context, NodeConfig) bool {
		t.Fatal("public ingress is closed during bootstrap")
		return false
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, initialStart(ctx, config, deps))
}

func TestExternalInstallPreservesProvisionedDockerStorageConfiguration(t *testing.T) {
	var calls []string
	deps := testInstallerDependencies(testInstallRelease(t), externalTestConfig(), &calls)
	lstat := deps.lstat
	deps.lstat = func(path string) (os.FileInfo, error) {
		if path == "/etc/docker/daemon.json" {
			return nil, nil
		}
		return lstat(path)
	}
	requireEmpty := deps.requireEmpty
	deps.requireEmpty = func(path, label string) error {
		if path == "/etc/docker" || path == "/etc/systemd/system/docker.service.d" {
			return fmt.Errorf("provisioner supplied retained-storage configuration")
		}
		return requireEmpty(path, label)
	}
	require.NoError(t, install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps))
	for _, call := range calls {
		require.NotContains(t, call, "daemon.json", "installer must not replace provisioner configuration")
	}
}

func TestExternalComposeRetainsArtifactsAcrossUpdates(t *testing.T) {
	args := []string{"--file", "/release/docker-compose.yaml", "--file", "/release/ha/fleet-compose.yaml", "up", "fleet-api"}
	require.Equal(t, args, endpointComposeArgs(args, false))
	require.Equal(t, []string{"--file", "/release/docker-compose.yaml", "--file", "/release/ha/fleet-compose.yaml", "--file", "/release/ha/fleet-compose.external.yaml", "up", "fleet-api"}, endpointComposeArgs(args, true))
	infrastructure := []string{"--file", "/etc/proto-fleet/ha/compose.yaml", "up", "etcd"}
	require.Equal(t, infrastructure, endpointComposeArgs(infrastructure, true))
}

func TestExternalEndpointTimeoutAndFirewall(t *testing.T) {
	config := externalTestConfig()
	require.Equal(t, 180*time.Second, endpointTakeoverTimeout(config))
	require.Equal(t, 35*time.Second, endpointTakeoverTimeout(NodeConfig{}))
	template, err := os.ReadFile("../../../../deployment-files/ha/firewall.nft.tmpl")
	require.NoError(t, err)
	rules, err := renderFirewall(string(template), config)
	require.NoError(t, err)
	require.NotContains(t, rules, "vrrp")
	require.NotContains(t, rules, "${")
	require.Contains(t, rules, "tcp dport 2379 ip saddr @ha_nodes accept")
	require.Contains(t, rules, "tcp dport { 5432, 8008 } ip saddr @database_nodes accept")
	require.Contains(t, rules, "tcp dport 4000 drop")
	_, err = renderKeepalivedConfig("", config)
	require.ErrorContains(t, err, "does not use keepalived")
}
