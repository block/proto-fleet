package deployment

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	start := callIndex(calls, "sudo systemctl start proto-fleet-ha.service")
	enable := callIndex(calls, "sudo systemctl enable proto-fleet-ha.service")
	updater := callIndex(calls, "sudo systemctl enable --now proto-fleet-updater.service")
	updaterPermissions := callIndex(calls, updaterDropIn)
	keepalived := callIndex(calls, "/etc/systemd/system/proto-fleet-ha.service.d/keepalived.conf")
	vipCheck := callIndex(calls, "verify-vip")
	packages := callIndex(calls, "iputils-arping")
	serviceMask := callIndex(calls, "sudo systemctl mask --runtime docker.service docker.socket keepalived.service")
	dockerPackages := callIndex(calls, "sudo apt-get install -y docker-ce")
	serviceUnmask := callIndex(calls, "sudo systemctl unmask --runtime docker.service docker.socket keepalived.service")
	snapshot := callIndex(calls, "dir:"+installRoot+" sha256sum --check deployment-manifest.sha256")
	rootPasswordInstall := callIndex(calls, configRoot+"/etcd-root-password")
	imageBuild := callIndex(calls, "build fleet-api fleet-client")
	dockerRecovery := callIndex(calls, dockerRecoveryDropIn)
	pinnedComposeInstall := callIndex(calls, "ha/compose.yaml "+infrastructureCompose)
	if packages < 0 || serviceMask < 0 || dockerPackages < 0 || serviceUnmask < 0 || snapshot < 0 || vipCheck < 0 || firewall < 0 || docker < 0 || rootPasswordInstall < 0 || pinnedComposeInstall < 0 || imageBuild < 0 || start < 0 || enable < 0 || dockerRecovery < 0 || updaterPermissions < 0 || updater < 0 || keepalived < 0 ||
		!(snapshot < packages && packages < vipCheck && serviceMask < dockerPackages && dockerPackages < serviceUnmask && keepalived < firewall && firewall < docker && docker < imageBuild && imageBuild < dockerRecovery && dockerRecovery < start && start < enable && enable < updater && updaterPermissions < updater && rootPasswordInstall < start && pinnedComposeInstall < start) {
		t.Fatalf("firewall/start/keepalived order is wrong:\n%s", strings.Join(calls, "\n"))
	}
	if callIndex(calls, "sudo install -D -o root -g root -m 0600") < 0 {
		t.Fatalf("root password was not installed with protected permissions:\n%s", strings.Join(calls, "\n"))
	}
	if callIndex(calls, "sudo chmod -R a+rX,go-w "+installRoot) < 0 {
		t.Fatalf("installed release permissions were not normalized:\n%s", strings.Join(calls, "\n"))
	}
	if _, err := os.Stat(rootPassword); !os.IsNotExist(err) {
		t.Fatalf("copied root password still exists: %v", err)
	}
	if _, err := os.Stat(secrets); !os.IsNotExist(err) {
		t.Fatalf("copied host secret bundle still exists: %v", err)
	}
}

func TestEtcdRootPasswordMustBeGeneratedAndUnique(t *testing.T) {
	// Arrange
	secrets := t.TempDir()
	config := NodeConfig{NodeName: "ha-a", NodeIP: testHostIPs[0], SecretsDir: secrets, DatabaseAIP: testHostIPs[0]}
	writeTestSecretBundle(t, config)
	rootPassword := filepath.Join(t.TempDir(), "etcd-root-password")
	require.NoError(t, os.WriteFile(rootPassword, []byte("weak\n"), 0o600))

	// Act and assert
	require.ErrorContains(t, validateEtcdRootPassword(rootPassword, secrets), "32 random bytes")
	require.NoError(t, os.WriteFile(rootPassword, []byte(testEtcdRootPassword+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(secrets, fleetEtcdPasswordFile), []byte(testEtcdRootPassword+"\n"), 0o600))
	require.ErrorContains(t, validateEtcdRootPassword(rootPassword, secrets), fleetEtcdPasswordFile)
	require.NoError(t, os.WriteFile(filepath.Join(secrets, fleetEtcdPasswordFile), []byte("different\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(secrets, fleetEnvironmentFile), []byte(
		"AUTH_CLIENT_SECRET_KEY="+testEtcdRootPassword+"\n"+
			"ENCRYPT_SERVICE_MASTER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"+
			"DB_DSN=postgresql://fleet:test@db/fleet\n",
	), 0o600))
	require.ErrorContains(t, validateEtcdRootPassword(rootPassword, secrets), "AUTH_CLIENT_SECRET_KEY")
}

func TestInstallFailurePreservesCopiedCredentials(t *testing.T) {
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
	deps.run = func(runCtx context.Context, name string, args ...string) ([]byte, error) {
		output, err := run(runCtx, name, args...)
		command := strings.Join(append([]string{name}, args...), " ")
		if strings.Contains(command, "systemctl disable --now proto-fleet-ha.service") {
			cleanupUsedFreshContext = runCtx.Err() == nil
		}
		if strings.Contains(command, "systemctl start proto-fleet-ha.service") {
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
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, configRoot+"/etcd-root-password")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
	require.FileExists(t, rootPassword)
	require.DirExists(t, secrets)
}

func TestInstallReadinessFailureDisablesHA(t *testing.T) {
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
		if strings.Contains(strings.Join(args, " "), "fleet-ha status") {
			return nil, errors.New("not ready")
		}
		return run(ctx, name, args...)
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env", EtcdRootPasswordFile: rootPassword}, deps)

	// Assert
	require.ErrorContains(t, err, "failover readiness did not become healthy")
	joined := strings.Join(calls, "\n")
	require.Contains(t, joined, "sudo systemctl disable --now proto-fleet-ha.service")
	require.NotContains(t, joined, "sudo systemctl enable proto-fleet-ha.service")
	require.Contains(t, joined, "docker-ha-recovery-systemd.conf "+dockerRecoveryDropIn)
	require.Contains(t, joined, "sudo rm -f "+dockerRecoveryDropIn)
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
	require.NoError(t, os.WriteFile(rootPassword, []byte("root-password\n"), 0o600))
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	run := deps.run
	deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "enable --now proto-fleet-updater.service") {
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

func TestPrepareImagesRejectsMissingHADatabaseImage(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{NodeName: "ha-a", NodeIP: testHostIPs[0], DatabaseAIP: testHostIPs[0]}
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)
	run := deps.run
	deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "docker image inspect proto-fleet-timescaledb-ha:test") {
			return nil, errors.New("missing image")
		}
		return run(ctx, name, args...)
	}

	// Act
	err := prepareImages(t.Context(), source, config, deps)

	// Assert
	require.ErrorContains(t, err, "archive did not load required HA database image")
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

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "sudo systemctl disable --now keepalived.service") {
		t.Fatalf("witness did not disable keepalived:\n%s", joined)
	}
	for _, unexpected := range []string{"fleet-api", "fleet-client", "timescaledb.tar.gz", "proto-fleet-updater.service", "proto-fleet-ha.service.d/keepalived.conf", " arping "} {
		if strings.Contains(joined, unexpected) {
			t.Fatalf("witness installed database-host service %q:\n%s", unexpected, joined)
		}
	}
}

func TestValidateInstallPlatformRejects16KPages(t *testing.T) {
	// Act
	err := validateInstallPlatform(installDependencies{
		goos: "linux", goarch: "arm64", pageSize: 16384,
		readFile: func(string) ([]byte, error) { return []byte("ID=debian\nVERSION_ID=13\n"), nil },
	})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "4096-byte") || !strings.Contains(err.Error(), "reboot") {
		t.Fatalf("validateInstallPlatform() error = %v", err)
	}
}

func TestValidateReleaseRejectsSymlinks(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	if err := os.Symlink("version.txt", filepath.Join(source, "unlisted-link")); err != nil {
		t.Fatal(err)
	}

	// Act
	err := validateRelease(t.Context(), source, installDependencies{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
		t.Fatalf("validateRelease() error = %v", err)
	}
}

func TestConsumeInstallCredentialsPreservesUnexpectedFiles(t *testing.T) {
	// Arrange
	secrets := t.TempDir()
	unrelated := filepath.Join(secrets, "operator-notes")
	require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0o600))

	// Act
	err := consumeInstallCredentials(InstallOptions{}, NodeConfig{NodeName: "ha-c", SecretsDir: secrets})

	// Assert
	require.ErrorContains(t, err, "unexpected entry")
	require.FileExists(t, unrelated)
}

func TestInstallRejectsUnexpectedSecretBeforeMutation(t *testing.T) {
	// Arrange
	source := testInstallRelease(t)
	config := NodeConfig{
		NodeName: "ha-c", NodeIP: testHostIPs[2], DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
		NetworkInterface: "eth0", DataDir: dataRoot, SecretsDir: t.TempDir(),
	}
	writeTestSecretBundle(t, config)
	require.NoError(t, os.WriteFile(filepath.Join(config.SecretsDir, "offline-ca.key"), []byte("secret"), 0o600))
	var calls []string
	deps := testInstallerDependencies(source, config, &calls)

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.ErrorContains(t, err, "unexpected entry")
	require.NotContains(t, strings.Join(calls, "\n"), "apt-get")
}

func TestInstallRejectsExistingDockerBeforeMutation(t *testing.T) {
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
		if name == "sudo" || name == "ip" || name == "ss" {
			return "/usr/bin/" + name, nil
		}
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", fmt.Errorf("not found")
	}

	// Act
	err := install(t.Context(), InstallOptions{NodeEnvPath: "node.env"}, deps)

	// Assert
	require.ErrorContains(t, err, "clean host without docker")
	require.NotContains(t, strings.Join(calls, "\n"), "apt-get")
}

func TestCleanInstallRejectsHAServiceDropIns(t *testing.T) {
	// Arrange
	deps := installDependencies{
		lookPath: func(string) (string, error) { return "", os.ErrNotExist },
		requireEmpty: func(path, _ string) error {
			if path == "/etc/systemd/system/docker.service.d" {
				return fmt.Errorf("unexpected entry in %s", path)
			}
			return nil
		},
		lstat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}

	// Act
	err := validateCleanInstallState(deps)

	// Assert
	require.ErrorContains(t, err, "/etc/systemd/system/docker.service.d")
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
	require.Contains(t, joined, "sudo rm -rf -- "+installBase)
	require.NotContains(t, joined, "docker-ce")
	require.NotContains(t, joined, "/etc/apt/keyrings/docker.asc")
}

func TestFleetComposeArgsAreRawDockerComposeArguments(t *testing.T) {
	// Act
	args := fleetComposeArgs("stop", "fleet-api", "fleet-client")

	// Assert
	require.Equal(t, "--env-file", args[0])
	require.Equal(t, []string{"stop", "fleet-api", "fleet-client"}, args[len(args)-3:])
}

func testInstallerDependencies(source string, config NodeConfig, calls *[]string) installDependencies {
	record := func(name string, args ...string) {
		*calls = append(*calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	}
	return installDependencies{
		goos: "linux", goarch: "arm64", pageSize: 4096,
		readFile: func(path string) ([]byte, error) {
			if path == "/etc/os-release" {
				return []byte("ID=debian\nVERSION_ID=13\n"), nil
			}
			if relative, ok := strings.CutPrefix(path, installRoot+string(os.PathSeparator)); ok {
				path = filepath.Join(source, relative)
			}
			return os.ReadFile(path)
		},
		lstat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		lookPath: func(name string) (string, error) {
			if name == "sudo" || name == "ip" || name == "ss" {
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
			if name == "curl" {
				return []byte("docker-signing-key"), nil
			}
			return nil, nil
		},
		runInput: func(_ context.Context, _ string, name string, args ...string) error {
			record("input:"+name, args...)
			return nil
		},
		runDir: func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
			record("dir:"+dir+" "+name, args...)
			return nil, nil
		},
		sourceRoot: func() (string, error) { return source, nil },
		sleep:      func(time.Duration) {},
	}
}

func testInstallRelease(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	required := map[string]string{
		"version.txt":                                            "version: test\n",
		"docker-compose.yaml":                                    "services: {}\n",
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
		"ha/fleet-ha":                                            "binary",
		"ha/compose.yaml":                                        "services:\n  patroni:\n    image: proto-fleet-timescaledb-ha:test\n",
		"ha/fleet-compose.yaml":                                  "services: {}\n",
		"ha/firewall.nft.tmpl":                                   "${HA_NODE_IP} ${HA_DB_A_IP} ${HA_DB_B_IP} ${HA_DCS_C_IP} ${HA_NETWORK_INTERFACE}\n",
		"ha/keepalived.conf.tmpl":                                "${HA_NODE_IP} ${HA_PEER_IP} ${HA_VIRTUAL_IP} ${HA_NETWORK_INTERFACE} ${HA_ENDPOINT_HEARTBEAT_FILE} ${HA_SECRETS_DIR}\n",
		"ha/keepalived-systemd.conf.tmpl":                        "${HA_VIRTUAL_IP} ${HA_NETWORK_INTERFACE}\n",
		"ha/proto-fleet-ha.service":                              "[Service]\n",
		"ha/proto-fleet-ha-keepalived.conf":                      "[Unit]\nWants=keepalived.service\n",
		"ha/proto-fleet-ha-firewall.service":                     "[Service]\n",
		"ha/docker-systemd.conf":                                 "[Unit]\n",
		"ha/docker-ha-recovery-systemd.conf":                     "[Unit]\n",
		"ha/updater-systemd.conf":                                "[Service]\nReadWritePaths=/etc/proto-fleet/ha\n",
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
	var manifest []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read test release file: %w", err)
		}
		digest := sha256.Sum256(contents)
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve test release path: %w", err)
		}
		manifest = append(manifest, fmt.Sprintf("%x  ./%s", digest, filepath.ToSlash(relative)))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(manifest)
	if err := os.WriteFile(filepath.Join(root, "deployment-manifest.sha256"), []byte(strings.Join(manifest, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
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
