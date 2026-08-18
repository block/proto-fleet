package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testEtcdRootPassword = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestInstallGoldenPathOrdersFirewallBeforeServices(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	secrets := t.TempDir()
	rootPassword := filepath.Join(t.TempDir(), "etcd-root-password")
	if err := os.WriteFile(rootPassword, []byte(testEtcdRootPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := NodeConfig{
		NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: secrets,
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env", EtcdRootPasswordFile: rootPassword}, deps)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	firewall := callIndex(calls, "sudo systemctl enable --now proto-fleet-ha-firewall.service")
	docker := callIndex(calls, "sudo systemctl start docker.service")
	start := callIndex(calls, "sudo systemctl start --no-block proto-fleet-ha.service")
	enable := callIndex(calls, "sudo systemctl enable proto-fleet-ha.service")
	updater := callIndex(calls, "sudo systemctl enable proto-fleet-updater.service")
	updaterPermissions := callIndex(calls, updaterDropIn)
	keepalived := callIndex(calls, "/etc/systemd/system/proto-fleet-ha.service.d/keepalived.conf")
	vipCheck := callIndex(calls, "verify-vip")
	aptUpdate := callIndex(calls, "sudo apt-get update")
	nftablesPackage := callIndex(calls, "sudo apt-get install -y nftables")
	nftablesCompatibility := callIndex(calls, "sudo nft -j list ruleset")
	serviceMask := callIndex(calls, "sudo systemctl mask --runtime docker.service docker.socket keepalived.service")
	dockerPackages := callIndex(calls, "sudo apt-get install -y docker-ce")
	serviceUnmask := callIndex(calls, "sudo systemctl unmask --runtime docker.service docker.socket keepalived.service")
	rootPasswordInstall := callIndex(calls, configRoot+"/etcd-root-password")
	imageLoad := callIndex(calls, "images/fleet.tar.gz")
	dockerRecovery := callIndex(calls, dockerRecoveryDropIn)
	pinnedComposeInstall := callIndex(calls, "ha/compose.yaml "+infrastructureCompose)
	if aptUpdate < 0 || nftablesPackage < 0 || nftablesCompatibility < 0 || serviceMask < 0 || dockerPackages < 0 || serviceUnmask < 0 || vipCheck < 0 || firewall < 0 || docker < 0 || rootPasswordInstall < 0 || pinnedComposeInstall < 0 || imageLoad < 0 || start < 0 || enable < 0 || dockerRecovery < 0 || updaterPermissions < 0 || updater < 0 || keepalived < 0 ||
		!(aptUpdate < nftablesPackage && nftablesPackage < vipCheck && vipCheck < nftablesCompatibility && nftablesCompatibility < serviceMask && serviceMask < dockerPackages && dockerPackages < serviceUnmask && nftablesCompatibility < keepalived && keepalived < firewall && firewall < docker && docker < imageLoad && imageLoad < dockerRecovery && dockerRecovery < enable && enable < updater && updaterPermissions < updater && updater < start && rootPasswordInstall < start && pinnedComposeInstall < start) {
		t.Fatalf("firewall/start/keepalived order is wrong:\n%s", strings.Join(calls, "\n"))
	}
	if callIndex(calls, "sudo install -D -o root -g root -m 0600") < 0 {
		t.Fatalf("root password was not installed with protected permissions:\n%s", strings.Join(calls, "\n"))
	}
	if callIndex(calls, "sudo chmod -R a+rX,go-w "+installRoot) < 0 {
		t.Fatalf("installed release permissions were not normalized:\n%s", strings.Join(calls, "\n"))
	}
	if callIndex(calls, "build fleet-api fleet-client") >= 0 {
		t.Fatalf("installer rebuilt checksum-covered Fleet images:\n%s", strings.Join(calls, "\n"))
	}
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "wait-local-etcd "+testHostIPs[0])
	require.NotContains(t, joined, "fleet-ha status")
	require.Contains(t, joined, "sudo install -D -o root -g root -m 0644 "+filepath.Join(secrets, "service-ca.crt")+" "+filepath.Join(configRoot, "service-ca.crt"))
	require.Contains(t, joined, "sudo install -D -o root -g root -m 0600 "+filepath.Join(secrets, "fleet-client.key")+" "+filepath.Join(configRoot, "fleet-client.key"))
}

func TestInstallRejectsExistingNftablesInputFilteringBeforeConfiguration(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	run := deps.run
	inspectedNftables := false
	deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Join(append([]string{name}, args...), " ") == "sudo nft -j list ruleset" {
			inspectedNftables = true
			return []byte(`{"nftables":[{"chain":{"family":"inet","table":"filter","name":"input","hook":"input","policy":"drop"}}]}`), nil
		}
		return run(ctx, name, args...)
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.ErrorContains(t, err, "existing nftables input chain inet filter input is incompatible (policy drop)")
	require.ErrorContains(t, err, "dedicated host firewall without input filtering")
	joined := strings.Join(calls, "\n")
	require.True(t, inspectedNftables)
	require.Contains(t, joined, "sudo apt-get install -y nftables")
	require.NotContains(t, joined, "sudo cp -a")
	require.NotContains(t, joined, "docker-ce")
	require.NotContains(t, joined, "apt-get install -y keepalived")
	require.NotContains(t, joined, filepath.Join(configRoot, "node.env"))
	require.NotContains(t, joined, firewallUnit)
}

func TestValidateNftablesInputChains(t *testing.T) {
	tests := []struct {
		name      string
		ruleset   string
		wantError string
	}{
		{name: "no input base chain", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"filter","name":"forward","hook":"forward","policy":"drop"}}]}`},
		{name: "empty accepting input base chain", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"filter","name":"input","hook":"input","policy":"accept"}}]}`},
		{name: "empty input base chain without policy", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"filter","name":"input","hook":"input"}}]}`},
		{name: "input base chain has rules", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"filter","name":"input","hook":"input","policy":"accept"}},{"rule":{"family":"inet","table":"filter","chain":"input","expr":[{"accept":null}]}}]}`, wantError: "contains rules"},
		{name: "input base chain drops by policy", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"filter","name":"input","hook":"input","policy":"drop"}}]}`, wantError: "policy drop"},
		{name: "reserved HA table is replaced", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"proto_fleet_ha","name":"input","hook":"input","policy":"drop"}},{"rule":{"family":"inet","table":"proto_fleet_ha","chain":"input","expr":[{"drop":null}]}}]}`},
		{name: "reserved table name in another family is rejected", ruleset: `{"nftables":[{"chain":{"family":"ip","table":"proto_fleet_ha","name":"input","hook":"input","policy":"drop"}}]}`, wantError: "policy drop"},
		{name: "similarly named table is rejected", ruleset: `{"nftables":[{"chain":{"family":"inet","table":"proto_fleet_ha_old","name":"input","hook":"input","policy":"drop"}}]}`, wantError: "policy drop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNftablesInputChains([]byte(tt.ruleset))
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestRejectIncompatibleNftablesInputChainsChecksPersistentConfig(t *testing.T) {
	// Arrange
	persistentChecked := false
	deps := installDependencies{run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) > 0 && args[0] == "unshare" {
			persistentChecked = true
			return []byte(`{"nftables":[{"chain":{"family":"inet","table":"filter","name":"input","hook":"input","policy":"drop"}}]}`), nil
		}
		return []byte(`{"nftables":[]}`), nil
	}}

	// Act
	err := rejectIncompatibleNftablesInputChains(t.Context(), deps)

	// Assert
	require.True(t, persistentChecked)
	require.ErrorContains(t, err, "policy drop")
}

func TestInstallFailureStopsServiceAfterCancellation(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	secrets := t.TempDir()
	rootPassword := filepath.Join(t.TempDir(), "etcd-root-password")
	require.NoError(t, os.WriteFile(rootPassword, []byte(testEtcdRootPassword+"\n"), 0o600))
	config := NodeConfig{
		NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: secrets,
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	run := deps.run
	ctx, cancel := context.WithCancel(t.Context())
	cleanupUsedFreshContext := false
	updaterCleanupUsedFreshContext := false
	deps.run = func(runCtx context.Context, name string, args ...string) ([]byte, error) {
		output, err := run(runCtx, name, args...)
		command := strings.Join(append([]string{name}, args...), " ")
		if strings.Contains(command, "systemctl disable --now proto-fleet-ha.service") {
			cleanupUsedFreshContext = runCtx.Err() == nil
		}
		if strings.Contains(command, "systemctl disable --now proto-fleet-updater.service") {
			updaterCleanupUsedFreshContext = runCtx.Err() == nil
		}
		if strings.Contains(command, "systemctl start --no-block proto-fleet-ha.service") {
			cancel()
			return nil, context.Canceled
		}
		return output, err
	}

	// Act
	err := install(ctx, InstallOptions{NodeEnvPath: "node.env", EtcdRootPasswordFile: rootPassword}, deps)

	// Assert
	require.ErrorContains(t, err, context.Canceled.Error())
	require.True(t, cleanupUsedFreshContext)
	require.True(t, updaterCleanupUsedFreshContext)
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, configRoot+"/etcd-root-password")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-updater.service")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
}

func TestInstallInterruptedDuringConvergenceLeavesHAEnabled(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	ctx, cancel := context.WithCancel(t.Context())
	deps.vipReady = func(context.Context, NodeConfig) bool {
		cancel()
		return false
	}

	// Act
	err := install(ctx, InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.ErrorContains(t, err, "remains enabled and is still converging")
	require.ErrorContains(t, err, "systemctl status proto-fleet-ha.service")
	require.ErrorIs(t, err, errInstallConverging)
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "sudo systemctl enable proto-fleet-ha.service")
	require.Contains(t, joined, "sudo systemctl start --no-block proto-fleet-ha.service")
	require.NotContains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
	require.Contains(t, joined, "docker-ha-recovery-systemd.conf "+dockerRecoveryDropIn)
	require.NotContains(t, joined, "sudo rm -f "+dockerRecoveryDropIn)
}

func TestInitialStartCleansUpFailedWitnessService(t *testing.T) {
	// Arrange
	config := NodeConfig{NodeName: "ha-c", NodeIP: testHostIPs[2], WitnessIP: testHostIPs[2]}
	var calls []string
	deps := installDependencies{
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := strings.Join(append([]string{name}, args...), " ")
			calls = append(calls, call)
			if call == "sudo systemctl is-active proto-fleet-ha.service" {
				return []byte("failed\n"), nil
			}
			return nil, nil
		},
		localReady: func(context.Context, NodeConfig) error { return nil },
		vipReady:   func(context.Context, NodeConfig) bool { return true },
		sleep:      func(time.Duration) {},
	}

	// Act
	err := initialStart(t.Context(), config, deps)

	// Assert
	require.ErrorContains(t, err, "proto-fleet-ha.service failed")
	require.NotErrorIs(t, err, errInstallConverging)
	require.Contains(t, strings.Join(calls, "\n"), "sudo systemctl disable --now proto-fleet-ha.service")
}

func TestInitialStartWaitsForWitnessServiceToBecomeActive(t *testing.T) {
	// Arrange
	config := NodeConfig{NodeName: "ha-c", NodeIP: testHostIPs[2], WitnessIP: testHostIPs[2]}
	stateChecks := 0
	vipChecks := 0
	deps := installDependencies{
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if strings.Join(append([]string{name}, args...), " ") != "sudo systemctl is-active proto-fleet-ha.service" {
				return nil, nil
			}
			stateChecks++
			if stateChecks == 1 {
				return []byte("activating\n"), nil
			}
			return []byte("active\n"), nil
		},
		localReady: func(context.Context, NodeConfig) error { return nil },
		vipReady: func(context.Context, NodeConfig) bool {
			vipChecks++
			return true
		},
		sleep: func(time.Duration) {},
	}

	// Act
	err := initialStart(t.Context(), config, deps)

	// Assert
	require.NoError(t, err)
	require.Equal(t, 2, stateChecks)
	require.Equal(t, 1, vipChecks)
}

func TestInitialStartCancellationDuringLocalEtcdWaitLeavesServicesEnabled(t *testing.T) {
	// Arrange
	config := NodeConfig{NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0]}
	var calls []string
	deps := installDependencies{
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
		localReady: func(context.Context, NodeConfig) error {
			return fmt.Errorf("stopped waiting for local etcd member: %w", context.Canceled)
		},
	}

	// Act
	err := initialStart(t.Context(), config, deps)

	// Assert
	require.ErrorIs(t, err, errInstallConverging)
	require.ErrorContains(t, err, "reconnect and run systemctl status proto-fleet-ha.service")
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "sudo systemctl enable proto-fleet-ha.service")
	require.Contains(t, joined, "sudo systemctl enable proto-fleet-updater.service")
	require.Contains(t, joined, "sudo systemctl start --no-block proto-fleet-ha.service")
	require.NotContains(t, joined, "sudo systemctl disable --now proto-fleet-updater.service")
	require.NotContains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
}

func TestPublicCAInstructionsUseTheVIP(t *testing.T) {
	// Arrange
	var output strings.Builder

	// Act
	printPublicCAInstructions(&output, testVirtualIP)

	// Assert
	require.Contains(t, output.String(), "https://"+testVirtualIP+"/proto-fleet-ha-service-ca.crt")
	require.Contains(t, output.String(), "--noproxy '*'")
	require.Contains(t, output.String(), "openssl x509 -in proto-fleet-ha-service-ca.download -out proto-fleet-ha-service-ca.crt")
	require.Contains(t, output.String(), "Import only proto-fleet-ha-service-ca.crt")
}

func TestInitialStartCleansUpWhenLocalEtcdDoesNotStart(t *testing.T) {
	// Arrange
	config := NodeConfig{NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0]}
	var calls []string
	deps := installDependencies{
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
		localReady: func(context.Context, NodeConfig) error {
			return errors.New("local etcd member did not start within one minute")
		},
	}

	// Act
	err := initialStart(t.Context(), config, deps)

	// Assert
	require.ErrorContains(t, err, "local etcd member did not start within one minute")
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-updater.service")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
}

func TestInstallRejectsUnavailableSystemdBeforeMutation(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	deps.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil, errors.New("systemd is not running")
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.ErrorContains(t, err, "requires a running systemd manager")
	require.Equal(t, []string{"systemctl show --property=Version --value"}, calls)
}

func TestInstallUpdaterFailureDisablesHA(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	rootPassword := filepath.Join(t.TempDir(), "etcd-root-password")
	require.NoError(t, os.WriteFile(rootPassword, []byte(testEtcdRootPassword+"\n"), 0o600))
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	run := deps.run
	deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "enable proto-fleet-updater.service") {
			return nil, errors.New("enable failed")
		}
		return run(ctx, name, args...)
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env", EtcdRootPasswordFile: rootPassword}, deps)

	// Assert
	require.ErrorContains(t, err, "enable host updater")
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-updater.service")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
}

func TestPrepareImagesRejectsMissingReleaseImage(t *testing.T) {
	for _, image := range []string{"proto-fleet-timescaledb-ha:test", "proto-fleet-api:test"} {
		t.Run(image, func(t *testing.T) {
			// Arrange
			source := testInstallRelease(t)
			config := NodeConfig{NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0]}
			var calls []string
			deps := testInstallerDependencies(source, config, &calls)
			run := deps.run
			deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), "docker image inspect "+image) {
					return nil, errors.New("missing image")
				}
				return run(ctx, name, args...)
			}

			// Act
			err := prepareImages(t.Context(), source, config, deps)

			// Assert
			require.ErrorContains(t, err, "archive did not load required image "+image)
		})
	}
}

func TestInstallWitnessSelectsOnlyEtcd(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	vipProbes := 0
	deps.vipReady = func(context.Context, NodeConfig) bool {
		vipProbes++
		return vipProbes == 3
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 3, vipProbes)
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "sudo systemctl disable --now keepalived.service") {
		t.Fatalf("witness did not disable keepalived:\n%s", joined)
	}
	for _, unexpected := range []string{"fleet-api", "fleet-client", "fleet.tar.gz", "timescaledb.tar.gz", "proto-fleet-updater.service", updaterDropIn, haUpdaterDropIn, "proto-fleet-ha.service.d/keepalived.conf", "iputils-arping", " arping "} {
		if strings.Contains(joined, unexpected) {
			t.Fatalf("witness installed database-host service %q:\n%s", unexpected, joined)
		}
	}
}

func TestValidateInstallPlatformRejects16KPages(t *testing.T) {
	// Act
	_, err := validateInstallPlatform(installDependencies{
		goos: "linux", goarch: "arm64", pageSize: 16384,
		readFile: func(string) ([]byte, error) { return []byte("ID=debian\nVERSION_ID=13\n"), nil },
	})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "4096-byte") || !strings.Contains(err.Error(), "reboot") {
		t.Fatalf("validateInstallPlatform() error = %v", err)
	}
}

func TestValidateInstallPlatformSelectsDockerRepository(t *testing.T) {
	tests := []struct {
		name      string
		osRelease string
		goarch    string
		want      installPlatform
	}{
		{name: "Debian 12", osRelease: "ID=debian\nVERSION_ID=12\nVERSION_CODENAME=bookworm\n", goarch: "amd64", want: installPlatform{repository: "debian", suite: "bookworm"}},
		{name: "Debian 13", osRelease: "ID=debian\nVERSION_ID=13\nVERSION_CODENAME=trixie\n", goarch: "arm64", want: installPlatform{repository: "debian", suite: "trixie"}},
		{name: "Ubuntu 22.04", osRelease: "ID=ubuntu\nVERSION_ID=22.04\nVERSION_CODENAME=jammy\n", goarch: "amd64", want: installPlatform{repository: "ubuntu", suite: "jammy"}},
		{name: "Ubuntu 24.04", osRelease: "ID=ubuntu\nVERSION_ID=24.04\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\n", goarch: "arm64", want: installPlatform{repository: "ubuntu", suite: "noble"}},
		{name: "Raspberry Pi OS", osRelease: "ID=raspbian\nVERSION_ID=12\nID_LIKE=debian\nVERSION_CODENAME=bookworm\n", goarch: "arm64", want: installPlatform{repository: "debian", suite: "bookworm"}},
		{name: "Ubuntu derivative", osRelease: "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\nVERSION_CODENAME=wilma\nUBUNTU_CODENAME=noble\n", goarch: "amd64", want: installPlatform{repository: "ubuntu", suite: "noble"}},
		{name: "Other apt derivative", osRelease: "ID=custom\nVERSION_CODENAME=trixie\n", goarch: "amd64", want: installPlatform{repository: "debian", suite: "trixie"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got, err := validateInstallPlatform(installDependencies{
				goos: "linux", goarch: tt.goarch, pageSize: 4096,
				readFile: func(string) ([]byte, error) { return []byte(tt.osRelease), nil },
			})

			// Assert
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateInstallPlatformRejectsUnsupportedHosts(t *testing.T) {
	tests := []struct {
		name      string
		osRelease string
		goarch    string
		wantError string
	}{
		{name: "old Debian", osRelease: "ID=debian\nVERSION_ID=11\n", goarch: "amd64", wantError: "Debian 12 or 13"},
		{name: "old Ubuntu", osRelease: "ID=ubuntu\nVERSION_ID=20.04\n", goarch: "amd64", wantError: "Ubuntu 22.04 or 24.04"},
		{name: "32-bit Raspberry Pi OS", osRelease: "ID=raspbian\nVERSION_ID=12\n", goarch: "arm", wantError: "64-bit"},
		{name: "unsupported architecture", osRelease: "ID=custom\nVERSION_CODENAME=bookworm\n", goarch: "386", wantError: "amd64 and arm64"},
		{name: "missing native codename", osRelease: "ID=debian\nVERSION_ID=12\n", goarch: "amd64", wantError: "VERSION_CODENAME"},
		{name: "unsafe derivative codename", osRelease: "ID=custom\nVERSION_CODENAME=bookworm stable\n", goarch: "amd64", wantError: "invalid release codename"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := validateInstallPlatform(installDependencies{
				goos: "linux", goarch: tt.goarch, pageSize: 4096,
				readFile: func(string) ([]byte, error) { return []byte(tt.osRelease), nil },
			})

			// Assert
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestInstallPackagesUsesUbuntuRepository(t *testing.T) {
	// Arrange
	var keyURL string
	var repository string
	deps := installDependencies{
		goarch: "arm64",
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "curl" {
				keyURL = args[len(args)-1]
				return []byte("docker-signing-key"), nil
			}
			return nil, nil
		},
		runInput: func(_ context.Context, input, _ string, args ...string) error {
			if slices.Contains(args, "/etc/apt/sources.list.d/docker.sources") {
				repository = input
			}
			return nil
		},
	}

	// Act
	err := installPackages(t.Context(), installPlatform{repository: "ubuntu", suite: "noble"}, installedDependencies{}, deps)

	// Assert
	require.NoError(t, err)
	require.Equal(t, "https://download.docker.com/linux/ubuntu/gpg", keyURL)
	require.Contains(t, repository, "URIs: https://download.docker.com/linux/ubuntu")
	require.Contains(t, repository, "Suites: noble")
}

func TestValidateReleaseRequiresHAUpdaterDropIn(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	require.NoError(t, os.Remove(filepath.Join(source, "ha", "updater-systemd.conf")))

	// Act
	err := validateRelease(source, os.ReadFile)

	// Assert
	require.ErrorContains(t, err, "release is missing ha/updater-systemd.conf")
}

func TestInstallReusesIdleDockerAndKeepalived(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	deps.lookPath = func(name string) (string, error) {
		if slices.Contains([]string{"apt-get", "ip", "ss", "sudo", "systemctl", "docker", "keepalived", "arping"}, name) {
			return "/usr/bin/" + name, nil
		}
		return "", fmt.Errorf("not found")
	}
	deps.requireEmpty = func(path, _ string) error {
		if path == "/var/lib/docker" || path == "/var/lib/containerd" {
			return fmt.Errorf("unexpected state in %s", path)
		}
		return nil
	}
	run := deps.run
	deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		command := strings.Join(append([]string{name}, args...), " ")
		switch command {
		case "sudo docker info":
			return []byte("ready\n"), nil
		case "sudo docker compose version --short":
			return []byte("v2.24.4\n"), nil
		case "sudo docker ps -aq":
			return nil, nil
		case "sudo systemctl is-active keepalived.service":
			return []byte("inactive\n"), errors.New("exit status 3")
		}
		return run(ctx, name, args...)
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.NoError(t, err)
	joined := strings.Join(calls, "\n")
	require.NotContains(t, joined, "download.docker.com")
	require.NotContains(t, joined, "docker-ce")
	require.NotContains(t, joined, "apt-get install -y keepalived")
	require.Less(t, callIndex(calls, "sudo apt-get update"), callIndex(calls, "sudo apt-get install -y nftables"))
	require.Contains(t, joined, "sudo systemctl disable --now keepalived.service")
}

func TestDedicatedHostRejectsConflictingDependencies(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*installDependencies)
		wantError string
	}{
		{
			name: "Docker has containers",
			configure: func(deps *installDependencies) {
				deps.lookPath = func(name string) (string, error) {
					if name == "docker" {
						return "/usr/bin/docker", nil
					}
					return "", os.ErrNotExist
				}
				deps.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
					switch strings.Join(args, " ") {
					case "docker compose version --short":
						return []byte("2.24.4\n"), nil
					case "docker ps -aq":
						return []byte("existing-container\n"), nil
					}
					return nil, nil
				}
			},
			wantError: "existing containers",
		},
		{
			name: "Docker Compose is unavailable",
			configure: func(deps *installDependencies) {
				deps.lookPath = func(name string) (string, error) {
					if name == "docker" {
						return "/usr/bin/docker", nil
					}
					return "", os.ErrNotExist
				}
				deps.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
					if strings.Join(args, " ") == "docker compose version --short" {
						return nil, errors.New("compose missing")
					}
					return nil, nil
				}
			},
			wantError: "Compose v2",
		},
		{
			name: "Docker Compose is too old",
			configure: func(deps *installDependencies) {
				deps.lookPath = func(name string) (string, error) {
					if name == "docker" {
						return "/usr/bin/docker", nil
					}
					return "", os.ErrNotExist
				}
				deps.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
					if strings.Join(args, " ") == "docker compose version --short" {
						return []byte("2.24.3\n"), nil
					}
					return nil, nil
				}
			},
			wantError: "requires 2.24.4 or newer",
		},
		{
			name: "keepalived is active",
			configure: func(deps *installDependencies) {
				deps.lookPath = func(name string) (string, error) {
					if name == "keepalived" {
						return "/usr/sbin/keepalived", nil
					}
					return "", os.ErrNotExist
				}
				deps.run = func(context.Context, string, ...string) ([]byte, error) {
					return []byte("active\n"), nil
				}
			},
			wantError: "keepalived must be inactive",
		},
		{
			name: "Docker has service drop-ins",
			configure: func(deps *installDependencies) {
				deps.requireEmpty = func(path, _ string) error {
					if path == "/etc/systemd/system/docker.service.d" {
						return fmt.Errorf("unexpected entry in %s", path)
					}
					return nil
				}
			},
			wantError: "/etc/systemd/system/docker.service.d",
		},
		{
			name: "Docker is absent with residual data",
			configure: func(deps *installDependencies) {
				deps.requireEmpty = func(path, _ string) error {
					if path == "/var/lib/docker" {
						return fmt.Errorf("unexpected entry in %s", path)
					}
					return nil
				}
			},
			wantError: "/var/lib/docker",
		},
		{
			name: "containerd is installed without Docker",
			configure: func(deps *installDependencies) {
				deps.lookPath = func(name string) (string, error) {
					if name == "containerd" {
						return "/usr/bin/containerd", nil
					}
					return "", os.ErrNotExist
				}
			},
			wantError: "remove containerd before installing",
		},
		{
			name: "updater has service drop-ins",
			configure: func(deps *installDependencies) {
				deps.requireEmpty = func(path, _ string) error {
					if path == "/etc/systemd/system/proto-fleet-updater.service.d" {
						return fmt.Errorf("unexpected entry in %s", path)
					}
					return nil
				}
			},
			wantError: "/etc/systemd/system/proto-fleet-updater.service.d",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			deps := installDependencies{
				lookPath:     func(string) (string, error) { return "", os.ErrNotExist },
				requireEmpty: func(string, string) error { return nil },
				lstat:        func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			}
			tt.configure(&deps)

			// Act
			_, err := inspectDedicatedHost(t.Context(), deps)

			// Assert
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestInstallRejectsMissingBaseCommandBeforeMutation(t *testing.T) {
	for _, missing := range []string{"apt-get", "systemctl"} {
		t.Run(missing, func(t *testing.T) {
			// Arrange
			source := testInstallRelease(t)
			config := NodeConfig{
				NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
				DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
				NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
			}
			writeTestSecretBundle(t, config)
			var calls []string
			deps := testInstallerDependencies(source, config, &calls)
			deps.lookPath = func(name string) (string, error) {
				if name == missing {
					return "", os.ErrNotExist
				}
				return "/usr/bin/" + name, nil
			}

			// Act
			err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

			// Assert
			require.ErrorContains(t, err, "missing "+missing)
			require.Empty(t, calls)
		})
	}
}

func TestReleaseSnapshotCleanupOutlivesInstallCancellation(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	deps := installDependencies{run: func(cleanupCtx context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, cleanupCtx.Err()
	}}

	// Act
	err := removeReleaseSnapshot(ctx, deps)

	// Assert
	require.NoError(t, err)
}

func TestValidateReleaseRejectsMissingRuntimeAsset(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	require.NoError(t, os.Remove(filepath.Join(source, "images", "fleet.tar.gz")))

	// Act
	err := validateRelease(source, os.ReadFile)

	// Assert
	require.ErrorContains(t, err, "release is missing images/fleet.tar.gz")
}

func TestInstallVIPConflictLeavesDockerUninstalled(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-b", NodeIP: testHostIPs[1], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	deps.verifyVIP = func(context.Context, NodeConfig) error { return fmt.Errorf("VIP is in use") }

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.ErrorContains(t, err, "VIP is in use")
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "iputils-arping")
	require.NotContains(t, joined, "sudo cp -a")
	require.NotContains(t, joined, "docker-ce")
	require.NotContains(t, joined, "/etc/apt/keyrings/docker.asc")
}

func testInstallerDependencies(source string, config NodeConfig, calls *[]string) installDependencies {
	record := func(name string, args ...string) {
		*calls = append(*calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	}
	return installDependencies{
		goos: "linux", goarch: "arm64", pageSize: 4096,
		readFile: func(path string) ([]byte, error) {
			if path == "/etc/os-release" {
				return []byte("ID=debian\nVERSION_ID=13\nVERSION_CODENAME=trixie\n"), nil
			}
			if relative, ok := strings.CutPrefix(path, installRoot+string(os.PathSeparator)); ok {
				path = filepath.Join(source, relative)
			}
			return os.ReadFile(path)
		},
		lstat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		lookPath: func(name string) (string, error) {
			if slices.Contains([]string{"apt-get", "ip", "ss", "sudo", "systemctl"}, name) {
				return "/usr/bin/" + name, nil
			}
			return "", fmt.Errorf("not found")
		},
		requireEmpty: func(string, string) error { return nil },
		validateHost: func(context.Context, string) (NodeConfig, error) { return config, nil },
		verifyVIP: func(context.Context, NodeConfig) error {
			record("verify-vip")
			return nil
		},
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			record(name, args...)
			if strings.Join(append([]string{name}, args...), " ") == "systemctl show --property=Version --value" {
				return []byte("255\n"), nil
			}
			if strings.Join(append([]string{name}, args...), " ") == "sudo nft -j list ruleset" {
				return []byte(`{"nftables":[]}`), nil
			}
			if name == "sudo" && len(args) > 0 && args[0] == "unshare" {
				return []byte(`{"nftables":[]}`), nil
			}
			if name == "curl" {
				return []byte("docker-signing-key"), nil
			}
			if strings.Join(append([]string{name}, args...), " ") == "sudo systemctl is-active proto-fleet-ha.service" {
				return []byte("active\n"), nil
			}
			return nil, nil
		},
		runInput: func(_ context.Context, _ string, name string, args ...string) error {
			record("input:"+name, args...)
			return nil
		},
		sourceRoot: func() (string, error) { return source, nil },
		localReady: func(_ context.Context, config NodeConfig) error {
			record("wait-local-etcd", config.NodeIP)
			return nil
		},
		vipReady: func(context.Context, NodeConfig) bool {
			record("probe-active-vip")
			return true
		},
		sleep: func(time.Duration) {},
	}
}

func testInstallRelease(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	required := map[string]string{
		"version.txt":                                            "version: test\n",
		"docker-compose.yaml":                                    "services:\n  fleet-api:\n    image: proto-fleet-api:test\n  fleet-client:\n    image: proto-fleet-client:test\n",
		"server/docker-compose.base.yaml":                        "services: {}\n",
		"server/Dockerfile":                                      "FROM scratch\n",
		"server/fleetd":                                          "binary",
		"server/proto-plugin":                                    "binary",
		"server/antminer-plugin":                                 "binary",
		"server/asicrs-plugin":                                   "binary",
		"server/asicrs-config.yaml":                              "config",
		"server/virtual-plugin":                                  "binary",
		"server/virtual-plugin.json":                             "config",
		"images/timescaledb.tar.gz":                              "image",
		"images/fleet.tar.gz":                                    "image",
		"ha/fleet-ha":                                            "binary",
		"ha/compose.yaml":                                        "services:\n  patroni:\n    image: proto-fleet-timescaledb-ha:test\n",
		"ha/fleet-compose.yaml":                                  "services: {}\n",
		"ha/firewall.nft.tmpl":                                   "${HA_NODE_IP} ${HA_DB_A_IP} ${HA_DB_B_IP} ${HA_DCS_C_IP} ${HA_NETWORK_INTERFACE}\n",
		"ha/firewall-replace.nft":                                "delete table inet proto_fleet_ha\ninclude \"/etc/proto-fleet/ha/firewall.nft\"\n",
		"ha/keepalived.conf.tmpl":                                "${HA_NODE_IP} ${HA_PEER_IP} ${HA_VIRTUAL_IP} ${HA_NETWORK_INTERFACE} ${HA_ENDPOINT_HEARTBEAT_FILE} ${HA_SECRETS_DIR}\n",
		"ha/keepalived-systemd.conf.tmpl":                        "${HA_VIRTUAL_IP} ${HA_NETWORK_INTERFACE}\n",
		"ha/proto-fleet-ha.service":                              "[Service]\n",
		"ha/proto-fleet-ha-keepalived.conf":                      "[Unit]\nWants=keepalived.service\n",
		"ha/proto-fleet-ha-firewall.service":                     "[Service]\n",
		"ha/nftables-systemd.conf":                               "[Service]\n",
		"ha/nftables-reload.conf":                                "include \"/etc/nftables.conf\"\n",
		"ha/docker-systemd.conf":                                 "[Unit]\n",
		"ha/docker-ha-recovery-systemd.conf":                     "[Unit]\n",
		"ha/updater-systemd.conf":                                "[Service]\nReadWritePaths=/etc/proto-fleet/ha\n",
		"ha/ha-updater-systemd.conf":                             "[Service]\n",
		"ha/scripts/check-fleet-active.sh":                       "#!/bin/sh\n",
		"client/Dockerfile":                                      "FROM scratch\n",
		"client/protoFleet/index.html":                           "index",
		"client/protoFleet/assets/app.js":                        "asset",
		"client/docker-entrypoint.d/40-render-runtime-config.sh": "#!/bin/sh\n",
		"client/nginx.https.conf":                                "server {}\n",
		"updater/proto-fleet-updater":                            "updater\n",
		"updater/proto-fleet-updater.service":                    "[Service]\n",
	}
	for name, contents := range required {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeTestSecretBundle(t *testing.T, config NodeConfig) {
	t.Helper()
	for _, name := range copiedSecretFiles(config) {
		contents := "test"
		if name == fleetEnvironmentFile {
			contents = "AUTH_CLIENT_SECRET_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
				"ENCRYPT_SERVICE_MASTER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
				"DB_DSN=postgresql://fleet:test@db/fleet\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(config.SecretsDir, name), []byte(contents), 0o600))
	}
}

func callIndex(calls []string, needle string) int {
	for index, call := range calls {
		if strings.Contains(call, needle) {
			return index
		}
	}
	return -1
}
