package deployment

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	installBase          = "/opt/proto-fleet"
	installRoot          = "/opt/proto-fleet/deployment"
	configRoot           = "/etc/proto-fleet/ha"
	dataRoot             = "/var/lib/proto-fleet/ha"
	serviceUnit          = "/etc/systemd/system/proto-fleet-ha.service"
	firewallUnit         = "/etc/systemd/system/proto-fleet-ha-firewall.service"
	dockerDropIn         = "/etc/systemd/system/docker.service.d/proto-fleet-ha.conf"
	dockerRecoveryDropIn = "/etc/systemd/system/docker.service.d/proto-fleet-ha-recovery.conf"
)

type InstallOptions struct {
	NodeEnvPath          string
	EtcdRootPasswordFile string
}

type installDependencies struct {
	goos         string
	goarch       string
	pageSize     int
	readFile     func(string) ([]byte, error)
	lstat        func(string) (os.FileInfo, error)
	lookPath     func(string) (string, error)
	requireEmpty func(string, string) error
	validateHost func(context.Context, string) (NodeConfig, error)
	run          func(context.Context, string, ...string) ([]byte, error)
	runInput     func(context.Context, string, string, ...string) error
	runDir       func(context.Context, string, string, ...string) ([]byte, error)
	sourceRoot   func() (string, error)
	verifyVIP    func(context.Context, NodeConfig) error
	sleep        func(time.Duration)
}

func defaultInstallDependencies() installDependencies {
	return installDependencies{
		goos: runtime.GOOS, goarch: runtime.GOARCH, pageSize: os.Getpagesize(),
		readFile: os.ReadFile, lstat: os.Lstat, lookPath: exec.LookPath, requireEmpty: requireEmptyDir, validateHost: ValidateHost,
		run: runCommand, runInput: runWithInput, runDir: runCommandInDir,
		sourceRoot: releaseRoot, verifyVIP: verifyInstallVirtualIP, sleep: time.Sleep,
	}
}

// Install validates a clean Debian host, installs the release, and starts its fixed HA role.
func Install(ctx context.Context, options InstallOptions) error {
	return install(ctx, options, defaultInstallDependencies())
}

func install(ctx context.Context, options InstallOptions, deps installDependencies) error {
	if options.NodeEnvPath == "" {
		return errors.New("install requires a node environment file")
	}
	if err := validateInstallPlatform(deps); err != nil {
		return err
	}
	for _, command := range []string{"ip", "ss"} {
		if _, err := deps.lookPath(command); err != nil {
			return fmt.Errorf("HA install requires iproute2 on the base Debian host; missing %s", command)
		}
	}
	config, err := deps.validateHost(ctx, options.NodeEnvPath)
	if err != nil {
		return err
	}
	if config.DataDir != dataRoot {
		return fmt.Errorf("HA install requires HA_DATA_DIR=%s", dataRoot)
	}
	if config.SecretsDir == configRoot {
		return errors.New("HA_SECRETS_DIR must point to the copied host secret bundle; the installer moves it into /etc/proto-fleet/ha")
	}
	if config.NodeName == "ha-a" {
		if options.EtcdRootPasswordFile == "" {
			return errors.New("ha-a install requires --etcd-root-password-file")
		}
		if _, err := readPassword(options.EtcdRootPasswordFile); err != nil {
			return fmt.Errorf("validate etcd root password file: %w", err)
		}
	} else if options.EtcdRootPasswordFile != "" {
		return errors.New("--etcd-root-password-file is accepted only on ha-a")
	}
	if _, err := copiedSecretEntries(config); err != nil {
		return err
	}

	source, err := deps.sourceRoot()
	if err != nil {
		return err
	}
	if err := validateRelease(ctx, source, deps); err != nil {
		return err
	}
	if _, err := deps.lookPath("sudo"); err != nil {
		return errors.New("HA install requires sudo")
	}
	if err := validateCleanInstallState(deps); err != nil {
		return err
	}
	if err := snapshotRelease(ctx, source, deps); err != nil {
		return errors.Join(err, removeReleaseSnapshot(ctx, deps))
	}
	source = installRoot

	if err := installARPing(ctx, deps); err != nil {
		return errors.Join(err, removeReleaseSnapshot(ctx, deps))
	}
	if config.isDatabaseNode() {
		if err := deps.verifyVIP(ctx, config); err != nil {
			return errors.Join(err, removeReleaseSnapshot(ctx, deps))
		}
	}
	if err := installPackages(ctx, deps); err != nil {
		return err
	}
	if err := installRelease(ctx, config, deps); err != nil {
		return err
	}
	if err := installFirewall(ctx, source, config, deps); err != nil {
		return err
	}
	if config.isDatabaseNode() {
		if err := installKeepalived(ctx, source, config, deps); err != nil {
			return err
		}
	}
	if err := installRootPassword(ctx, options, deps); err != nil {
		return err
	}
	if err := activateInstallPrerequisites(ctx, deps); err != nil {
		return err
	}
	if err := prepareImages(ctx, config, deps); err != nil {
		return err
	}
	if err := installDockerRecoveryHook(ctx, deps); err != nil {
		return err
	}
	if err := initialStart(ctx, config, deps); err != nil {
		return err
	}
	if err := consumeInstallCredentials(options, config); err != nil {
		slog.Warn("HA installation succeeded but staged credentials could not be removed", "error", err)
	}
	return nil
}

func consumeInstallCredentials(options InstallOptions, config NodeConfig) error {
	entries, err := copiedSecretEntries(config)
	if err != nil {
		return err
	}
	if options.EtcdRootPasswordFile != "" {
		if err := os.Remove(options.EtcdRootPasswordFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove copied etcd root password: %w", err)
		}
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(config.SecretsDir, entry.Name())); err != nil {
			return fmt.Errorf("remove copied host secret %s: %w", entry.Name(), err)
		}
	}
	if err := os.Remove(config.SecretsDir); err != nil {
		return fmt.Errorf("remove empty copied host secret bundle: %w", err)
	}
	return nil
}

func copiedSecretEntries(config NodeConfig) ([]os.DirEntry, error) {
	expected := make(map[string]struct{})
	for _, name := range copiedSecretFiles(config) {
		expected[name] = struct{}{}
	}
	entries, err := os.ReadDir(config.SecretsDir)
	if err != nil {
		return nil, fmt.Errorf("inspect copied host secret bundle: %w", err)
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return nil, fmt.Errorf("copied host secret bundle contains unexpected entry %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect copied host secret %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("copied host secret bundle entry %s is not a regular file", entry.Name())
		}
		delete(expected, entry.Name())
	}
	if len(expected) != 0 {
		return nil, errors.New("copied host secret bundle is incomplete")
	}
	return entries, nil
}

func copiedSecretFiles(config NodeConfig) []string {
	files := []string{
		"service-ca.crt", "etcd-server.crt", "etcd-server.key", "etcd-peer.crt", "etcd-peer.key", "etcd-jwt.pub", "etcd-jwt.key",
		fleetEtcdPasswordFile,
	}
	if config.isDatabaseNode() {
		files = append(files,
			"patroni-rest.crt", "patroni-rest.key", "postgres.crt", "postgres.key",
			"fleet-client.crt", "fleet-client.key", fleetEnvironmentFile,
		)
		files = append(files, databasePasswordFiles...)
	}
	return files
}

func validateCleanInstallState(deps installDependencies) error {
	for _, command := range []string{"docker", "keepalived"} {
		if path, err := deps.lookPath(command); err == nil {
			return fmt.Errorf("HA install requires a clean host without %s; found %s", command, path)
		}
	}
	for _, path := range []string{installBase, configRoot, "/var/lib/docker", "/var/lib/containerd", "/etc/docker"} {
		if err := deps.requireEmpty(path, "existing service state"); err != nil {
			return fmt.Errorf("HA install failed: %w", err)
		}
	}
	for _, path := range []string{
		serviceUnit, firewallUnit, dockerDropIn,
		"/etc/keepalived/keepalived.conf",
		"/etc/systemd/system/keepalived.service.d/override.conf",
		"/usr/local/libexec/proto-fleet/check-fleet-active",
		"/etc/apt/sources.list.d/docker.sources", "/etc/apt/keyrings/docker.asc",
		"/usr/bin/docker", "/usr/sbin/keepalived", "/lib/systemd/system/docker.service",
	} {
		if _, err := deps.lstat(path); err == nil {
			return fmt.Errorf("HA install requires a clean host; found existing path %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect clean-host path %s: %w", path, err)
		}
	}
	return nil
}

func validateInstallPlatform(deps installDependencies) error {
	if deps.goos != "linux" {
		return errors.New("HA install supports Debian Linux only")
	}
	if deps.goarch != "amd64" && deps.goarch != "arm64" {
		return fmt.Errorf("HA install supports amd64 and arm64, not %s", deps.goarch)
	}
	contents, err := deps.readFile("/etc/os-release")
	if err != nil {
		return fmt.Errorf("read /etc/os-release: %w", err)
	}
	values := parseOSRelease(string(contents))
	if values["ID"] != "debian" || values["VERSION_ID"] != "13" {
		return fmt.Errorf("HA install requires Debian 13; found %s %s", values["ID"], values["VERSION_ID"])
	}
	if deps.pageSize != 4096 {
		return fmt.Errorf("HA install requires a 4096-byte page size; found %d bytes. Boot a 4K-page kernel, reboot, verify with `getconf PAGESIZE`, then retry", deps.pageSize)
	}
	return nil
}

func parseOSRelease(contents string) map[string]string {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return values
}

func validateRelease(ctx context.Context, source string, deps installDependencies) error {
	required := []string{
		"deployment-manifest.sha256", "version.txt", "docker-compose.yaml", "server/docker-compose.base.yaml", "images/timescaledb.tar.gz",
		"server/Dockerfile", "server/fleetd", "server/proto-plugin", "server/antminer-plugin", "server/asicrs-plugin", "server/asicrs-config.yaml", "server/virtual-plugin", "server/virtual-plugin.json",
		"client/Dockerfile", "client/protoFleet/index.html", "client/docker-entrypoint.d/40-render-runtime-config.sh",
		"ha/fleet-ha", "ha/compose.yaml", "ha/fleet-compose.yaml", "ha/firewall.nft.tmpl",
		"ha/keepalived.conf.tmpl", "ha/keepalived-systemd.conf.tmpl", "ha/proto-fleet-ha.service", "ha/proto-fleet-ha-keepalived.conf",
		"ha/proto-fleet-ha-firewall.service", "ha/docker-systemd.conf", "ha/docker-ha-recovery-systemd.conf", "ha/scripts/check-fleet-active.sh",
		"client/nginx.https.conf",
	}
	for _, name := range required {
		info, err := os.Lstat(filepath.Join(source, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("release is missing %s", name)
		}
	}
	assets, err := os.ReadDir(filepath.Join(source, "client", "protoFleet", "assets"))
	hasBuiltAsset := false
	for _, asset := range assets {
		hasBuiltAsset = hasBuiltAsset || asset.Type().IsRegular()
	}
	if err != nil || !hasBuiltAsset {
		return errors.New("release is missing built Proto Fleet client assets")
	}
	if err := validateReleaseTree(source); err != nil {
		return err
	}
	output, err := deps.runDir(ctx, source, "sha256sum", "--check", "deployment-manifest.sha256")
	if err != nil {
		return fmt.Errorf("verify release manifest: %s", commandError(output, err))
	}
	return nil
}

func validateReleaseTree(source string) error {
	manifest, err := os.ReadFile(filepath.Join(source, "deployment-manifest.sha256"))
	if err != nil {
		return fmt.Errorf("read release manifest: %w", err)
	}
	expected := make(map[string]struct{})
	for line := range strings.SplitSeq(strings.TrimSpace(string(manifest)), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return errors.New("release manifest contains an invalid entry")
		}
		digest, name := line[:64], line[66:]
		relative := strings.TrimPrefix(name, "./")
		if strings.Trim(digest, "0123456789abcdefABCDEF") != "" || relative == name || relative == "." ||
			filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("release manifest contains an invalid entry")
		}
		if _, duplicate := expected[name]; duplicate {
			return fmt.Errorf("release manifest contains duplicate path %s", name)
		}
		expected[name] = struct{}{}
	}
	actual := make(map[string]struct{})
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release contains unsupported entry %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("resolve release path %s: %w", path, err)
		}
		name := "./" + filepath.ToSlash(relative)
		if name != "./deployment-manifest.sha256" {
			actual[name] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("validate release tree: %w", err)
	}
	for name := range expected {
		if _, ok := actual[name]; !ok {
			return fmt.Errorf("release manifest references missing file %s", name)
		}
	}
	for name := range actual {
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("release contains unlisted file %s", name)
		}
	}
	return nil
}

func installARPing(ctx context.Context, deps installDependencies) error {
	for _, args := range [][]string{
		{"apt-get", "update"},
		{"apt-get", "install", "-y", "iputils-arping"},
	} {
		if output, err := deps.run(ctx, "sudo", args...); err != nil {
			return fmt.Errorf("install HA virtual IP probe: %s", commandError(output, err))
		}
	}
	return nil
}

func installPackages(ctx context.Context, deps installDependencies) error {
	commands := [][]string{
		{"sudo", "apt-get", "install", "-y", "ca-certificates", "curl", "iproute2"},
		{"sudo", "install", "-m", "0755", "-d", "/etc/apt/keyrings"},
	}
	for _, command := range commands {
		if output, err := deps.run(ctx, command[0], command[1:]...); err != nil {
			return fmt.Errorf("install HA prerequisites: %s", commandError(output, err))
		}
	}
	key, err := deps.run(ctx, "curl", "-fsSL", "https://download.docker.com/linux/debian/gpg")
	if err != nil {
		return fmt.Errorf("download Docker repository key: %s", commandError(key, err))
	}
	if err := deps.runInput(ctx, string(key), "sudo", "tee", "/etc/apt/keyrings/docker.asc"); err != nil {
		return fmt.Errorf("install Docker repository key: %w", err)
	}
	if output, err := deps.run(ctx, "sudo", "chmod", "a+r", "/etc/apt/keyrings/docker.asc"); err != nil {
		return fmt.Errorf("protect Docker repository key: %s", commandError(output, err))
	}
	repository := "Types: deb\nURIs: https://download.docker.com/linux/debian\nSuites: trixie\nComponents: stable\nArchitectures: " + deps.goarch + "\nSigned-By: /etc/apt/keyrings/docker.asc\n"
	if err := deps.runInput(ctx, repository, "sudo", "tee", "/etc/apt/sources.list.d/docker.sources"); err != nil {
		return fmt.Errorf("configure Docker repository: %w", err)
	}
	for _, args := range [][]string{
		{"apt-get", "update"},
	} {
		if output, err := deps.run(ctx, "sudo", args...); err != nil {
			return fmt.Errorf("install HA packages: %s", commandError(output, err))
		}
	}
	services := []string{"docker.service", "docker.socket", "keepalived.service"}
	if output, err := deps.run(ctx, "sudo", append([]string{"systemctl", "mask", "--runtime"}, services...)...); err != nil {
		return fmt.Errorf("prevent HA services from starting before the firewall: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", "apt-get", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin", "keepalived", "nftables"); err != nil {
		return fmt.Errorf("install HA packages: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", append([]string{"systemctl", "unmask", "--runtime"}, services...)...); err != nil {
		return fmt.Errorf("restore HA service startup after package installation: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", "systemctl", "disable", "--now", "keepalived.service"); err != nil {
		return fmt.Errorf("disable keepalived until the database role is ready: %s", commandError(output, err))
	}
	return nil
}

func verifyInstallVirtualIP(ctx context.Context, config NodeConfig) error {
	output, err := runCommand(ctx, "sudo", "arping", "-D", "-I", config.NetworkInterface, "-c", "2", config.VirtualIP)
	if err == nil {
		return nil
	}

	peerIP := config.DatabaseAIP
	if config.NodeName == "ha-a" {
		peerIP = config.DatabaseBIP
	}
	vipMAC, vipErr := neighborMAC(ctx, config.NetworkInterface, config.VirtualIP)
	peerMAC, peerErr := neighborMAC(ctx, config.NetworkInterface, peerIP)
	if vipErr == nil && peerErr == nil && vipMAC == peerMAC {
		return nil
	}
	return fmt.Errorf("HA virtual IP is owned by an unexpected host or cannot be checked: %s", commandError(output, err))
}

func neighborMAC(ctx context.Context, networkInterface, address string) (string, error) {
	if _, err := runCommand(ctx, "sudo", "arping", "-I", networkInterface, "-c", "1", "-w", "2", address); err != nil {
		return "", err
	}
	output, err := runCommand(ctx, "ip", "neigh", "show", "to", address, "dev", networkInterface)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(output))
	for index, field := range fields {
		if field == "lladdr" && index+1 < len(fields) {
			return strings.ToLower(fields[index+1]), nil
		}
	}
	return "", fmt.Errorf("neighbor entry for %s has no MAC address", address)
}

func snapshotRelease(ctx context.Context, source string, deps installDependencies) error {
	for _, args := range [][]string{
		{"install", "-d", "-o", "root", "-g", "root", "-m", "0755", installBase},
		{"install", "-d", "-o", "root", "-g", "root", "-m", "0755", installRoot},
		{"cp", "-a", source + "/.", installRoot + "/"},
		{"chown", "-R", "root:root", installRoot},
		{"chmod", "-R", "go-w", installRoot},
		{"chmod", "0755", filepath.Join(installRoot, "ha", "fleet-ha")},
	} {
		if output, err := deps.run(ctx, "sudo", args...); err != nil {
			return fmt.Errorf("install HA release: %s", commandError(output, err))
		}
	}
	if output, err := deps.runDir(ctx, installRoot, "sha256sum", "--check", "deployment-manifest.sha256"); err != nil {
		return fmt.Errorf("verify installed HA release snapshot: %s", commandError(output, err))
	}
	return nil
}

func removeReleaseSnapshot(ctx context.Context, deps installDependencies) error {
	output, err := deps.run(ctx, "sudo", "rm", "-rf", "--", installBase)
	if err != nil {
		return fmt.Errorf("remove incomplete HA release snapshot: %s", commandError(output, err))
	}
	return nil
}

func installRelease(ctx context.Context, config NodeConfig, deps installDependencies) error {
	for _, args := range [][]string{
		{"install", "-d", "-o", "root", "-g", "root", "-m", "0700", configRoot},
		{"install", "-d", "-o", "root", "-g", "root", "-m", "0750", dataRoot},
		{"cp", filepath.Join(installRoot, "client", "nginx.https.conf"), filepath.Join(installRoot, "client", "nginx.conf")},
	} {
		if output, err := deps.run(ctx, "sudo", args...); err != nil {
			return fmt.Errorf("install HA release: %s", commandError(output, err))
		}
	}
	for _, name := range copiedSecretFiles(config) {
		if output, err := deps.run(ctx, "sudo", "install", "-o", "root", "-g", "root", "-m", "0600", filepath.Join(config.SecretsDir, name), filepath.Join(configRoot, name)); err != nil {
			return fmt.Errorf("install HA secret %s: %s", name, commandError(output, err))
		}
	}
	installedConfig := config
	installedConfig.SecretsDir = configRoot
	temp, err := writeInstallTemp("node.env", renderNodeEnvironment(installedConfig), 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if output, err := deps.run(ctx, "sudo", "install", "-o", "root", "-g", "root", "-m", "0600", temp, filepath.Join(configRoot, "node.env")); err != nil {
		return fmt.Errorf("install node configuration: %s", commandError(output, err))
	}
	baseEnv, err := writeInstallTemp("fleet-base.env", "DB_USERNAME=fleet\nDB_PASSWORD=unused\n", 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(baseEnv)
	if output, err := deps.run(ctx, "sudo", "install", "-o", "root", "-g", "root", "-m", "0600", baseEnv, filepath.Join(configRoot, "base.env")); err != nil {
		return fmt.Errorf("install Fleet base environment: %s", commandError(output, err))
	}
	for sourceName, target := range map[string]string{
		"proto-fleet-ha.service":          serviceUnit,
		"proto-fleet-ha-firewall.service": firewallUnit,
		"docker-systemd.conf":             dockerDropIn,
	} {
		if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(installRoot, "ha", sourceName), target); err != nil {
			return fmt.Errorf("install HA systemd unit: %s", commandError(output, err))
		}
	}
	return nil
}

func renderNodeEnvironment(config NodeConfig) string {
	return fmt.Sprintf("HA_NODE_NAME=%s\nHA_NODE_IP=%s\nHA_DB_A_IP=%s\nHA_DB_B_IP=%s\nHA_DCS_C_IP=%s\nHA_VIRTUAL_IP=%s\nHA_NETWORK_INTERFACE=%s\nHA_DATA_DIR=%s\nHA_SECRETS_DIR=%s\n",
		config.NodeName, config.NodeIP, config.DatabaseAIP, config.DatabaseBIP, config.WitnessIP,
		config.VirtualIP, config.NetworkInterface, config.DataDir, config.SecretsDir)
}

func installFirewall(ctx context.Context, source string, config NodeConfig, deps installDependencies) error {
	template, err := deps.readFile(filepath.Join(source, "ha", "firewall.nft.tmpl"))
	if err != nil {
		return fmt.Errorf("read firewall template: %w", err)
	}
	rules, err := renderFirewall(string(template), config)
	if err != nil {
		return err
	}
	if err := deps.runInput(ctx, rules, "sudo", "nft", "-c", "-f", "-"); err != nil {
		return fmt.Errorf("validate HA firewall: %w", err)
	}
	temp, err := writeInstallTemp("firewall.nft", rules, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if output, err := deps.run(ctx, "sudo", "install", "-o", "root", "-g", "root", "-m", "0600", temp, filepath.Join(configRoot, "firewall.nft")); err != nil {
		return fmt.Errorf("persist HA firewall: %s", commandError(output, err))
	}
	return nil
}

func installKeepalived(ctx context.Context, source string, config NodeConfig, deps installDependencies) error {
	template, err := deps.readFile(filepath.Join(source, "ha", "keepalived.conf.tmpl"))
	if err != nil {
		return fmt.Errorf("read keepalived template: %w", err)
	}
	rendered, err := renderKeepalivedConfig(string(template), NodeConfig{
		NodeName: config.NodeName, NodeIP: config.NodeIP, DatabaseAIP: config.DatabaseAIP,
		DatabaseBIP: config.DatabaseBIP, VirtualIP: config.VirtualIP,
		NetworkInterface: config.NetworkInterface, SecretsDir: configRoot,
	})
	if err != nil {
		return err
	}
	systemdTemplate, err := deps.readFile(filepath.Join(source, "ha", "keepalived-systemd.conf.tmpl"))
	if err != nil {
		return fmt.Errorf("read keepalived systemd template: %w", err)
	}
	for name, contents := range map[string]string{
		"/etc/keepalived/keepalived.conf": rendered,
		"/etc/systemd/system/keepalived.service.d/override.conf": strings.NewReplacer(
			"${HA_VIRTUAL_IP}", config.VirtualIP,
			"${HA_NETWORK_INTERFACE}", config.NetworkInterface,
		).Replace(string(systemdTemplate)),
	} {
		temp, err := writeInstallTemp(filepath.Base(name), contents, 0o600)
		if err != nil {
			return err
		}
		defer os.Remove(temp)
		if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0644", temp, name); err != nil {
			return fmt.Errorf("install keepalived configuration: %s", commandError(output, err))
		}
	}
	if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0755", filepath.Join(source, "ha", "scripts", "check-fleet-active.sh"), "/usr/local/libexec/proto-fleet/check-fleet-active"); err != nil {
		return fmt.Errorf("install keepalived health check: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(source, "ha", "proto-fleet-ha-keepalived.conf"), "/etc/systemd/system/proto-fleet-ha.service.d/keepalived.conf"); err != nil {
		return fmt.Errorf("install keepalived service dependency: %s", commandError(output, err))
	}
	return nil
}

func prepareImages(ctx context.Context, config NodeConfig, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", filepath.Join(installRoot, "ha", "fleet-ha"), "compose", "--env-file", filepath.Join(configRoot, "node.env"), "--file", filepath.Join(installRoot, "ha", "compose.yaml"), "pull", "etcd"); err != nil {
		return fmt.Errorf("pull etcd image: %s", commandError(output, err))
	}
	if config.isDatabaseNode() {
		if output, err := deps.run(ctx, "sudo", "docker", "load", "--input", filepath.Join(installRoot, "images", "timescaledb.tar.gz")); err != nil {
			return fmt.Errorf("load HA database images: %s", commandError(output, err))
		}
		args := fleetComposeArgs("build", "fleet-api", "fleet-client")
		commandArgs := append([]string{filepath.Join(installRoot, "ha", "fleet-ha"), "compose"}, args...)
		if output, err := deps.run(ctx, "sudo", commandArgs...); err != nil {
			return fmt.Errorf("build Fleet images: %s", commandError(output, err))
		}
	}
	return nil
}

func installDockerRecoveryHook(ctx context.Context, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(installRoot, "ha", "docker-ha-recovery-systemd.conf"), dockerRecoveryDropIn); err != nil {
		return fmt.Errorf("install Docker HA recovery hook: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("reload Docker HA recovery hook: %s", commandError(output, err))
	}
	return nil
}

func activateInstallPrerequisites(ctx context.Context, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", "systemctl", "enable", "--now", "proto-fleet-ha-firewall.service"); err != nil {
		return fmt.Errorf("enable HA firewall: %s", commandError(output, err))
	}
	if output, err := deps.run(ctx, "sudo", "systemctl", "start", "docker.service"); err != nil {
		return fmt.Errorf("start Docker after HA firewall: %s", commandError(output, err))
	}
	return nil
}

func installRootPassword(ctx context.Context, options InstallOptions, deps installDependencies) error {
	if options.EtcdRootPasswordFile == "" {
		return nil
	}
	const installedRootPassword = configRoot + "/etcd-root-password" //nolint:gosec // Credential file path, not a credential.
	if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0600", options.EtcdRootPasswordFile, installedRootPassword); err != nil {
		return fmt.Errorf("install etcd root password: %s", commandError(output, err))
	}
	return nil
}

func initialStart(ctx context.Context, config NodeConfig, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", "systemctl", "enable", "--now", "proto-fleet-ha.service"); err != nil {
		return fmt.Errorf("enable HA services: %s", commandError(output, err))
	}
	if config.isDatabaseNode() {
		for range 300 {
			if _, err := deps.run(ctx, "sudo", filepath.Join(installRoot, "ha", "fleet-ha"), "status", filepath.Join(configRoot, "node.env"), "--check"); err == nil {
				return nil
			}
			deps.sleep(2 * time.Second)
		}
		readinessErr := errors.New("failover readiness did not become healthy within 10 minutes")
		if output, err := deps.run(ctx, "sudo", "systemctl", "disable", "--now", "proto-fleet-ha.service"); err != nil {
			return fmt.Errorf("%w; disable incomplete HA installation: %s", readinessErr, commandError(output, err))
		}
		return readinessErr
	}
	fmt.Println("HA witness installed; etcd quorum is ready")
	return nil
}

func fleetComposeArgs(operation string, services ...string) []string {
	args := []string{
		"--env-file", filepath.Join(configRoot, "base.env"),
		"--env-file", filepath.Join(configRoot, fleetEnvironmentFile),
		"--env-file", filepath.Join(configRoot, "node.env"),
		"--file", filepath.Join(installRoot, "docker-compose.yaml"),
		"--file", filepath.Join(installRoot, "ha", "fleet-compose.yaml"), operation,
	}
	return append(args, services...)
}

func writeInstallTemp(name, contents string, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp("", "proto-fleet-ha-"+name+"-")
	if err != nil {
		return "", fmt.Errorf("create temporary %s: %w", name, err)
	}
	path := file.Name()
	if _, err := file.WriteString(contents); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("write temporary %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close temporary %s: %w", name, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("protect temporary %s: %w", name, err)
	}
	return path, nil
}

func releaseRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate fleet-ha executable: %w", err)
	}
	root := filepath.Dir(filepath.Dir(executable))
	if _, err := os.Stat(filepath.Join(root, "docker-compose.yaml")); err != nil {
		return "", errors.New("fleet-ha install must run from a packaged release")
	}
	return root, nil
}

func runCommandInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run %s: %w", name, err)
	}
	return output, nil
}
