package deployment

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

var errInstallConverging = errors.New("HA service remains enabled and is still converging")

type InstallOptions struct {
	NodeEnvPath          string
	EtcdRootPasswordFile string
}

type installPlatform struct {
	repository string
	suite      string
}

type installedDependencies struct {
	docker     bool
	keepalived bool
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

// Install validates a dedicated host, installs the release, and starts its fixed HA role.
func Install(ctx context.Context, options InstallOptions) error {
	return install(ctx, options, defaultInstallDependencies())
}

func install(ctx context.Context, options InstallOptions, deps installDependencies) error {
	fmt.Println("[validation] Verifying installation inputs and current host state...")
	if options.NodeEnvPath == "" {
		return errors.New("install requires a node environment file")
	}
	source, err := deps.sourceRoot()
	if err != nil {
		return err
	}
	platform, installed, err := inspectInstallBase(ctx, source, deps)
	if err != nil {
		return err
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
		if err := validateEtcdRootPassword(options.EtcdRootPasswordFile, config.SecretsDir); err != nil {
			return fmt.Errorf("validate etcd root password file: %w", err)
		}
	} else if options.EtcdRootPasswordFile != "" {
		return errors.New("--etcd-root-password-file is accepted only on ha-a")
	}
	if _, err := copiedSecretEntries(config); err != nil {
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
	fmt.Println("[package setup] Installing missing host dependencies...")
	if err := installPackages(ctx, platform, installed, deps); err != nil {
		return err
	}
	fmt.Println("[configuration] Installing the release, secrets, firewall, and service units...")
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
	fmt.Println("[image preparation] Loading and building the packaged service images...")
	if err := prepareImages(ctx, source, config, deps); err != nil {
		return err
	}
	// Install the Docker recovery dependency before etcd first starts so a
	// reboot cannot restore an unauthenticated cluster without the auth gate.
	if err := installDockerRecoveryHook(ctx, deps); err != nil {
		return stopIncompleteHA(ctx, deps, err)
	}
	fmt.Println("[service startup] Enabling the local HA service...")
	startErr := initialStart(ctx, config, deps)
	if startErr != nil && !errors.Is(startErr, errInstallConverging) {
		return startErr
	}
	if err := consumeInstallCredentials(options, config); err != nil {
		slog.Warn("HA installation succeeded but staged credentials could not be removed", "error", err)
	}
	if startErr != nil {
		return startErr
	}
	return nil
}

func inspectInstallBase(ctx context.Context, source string, deps installDependencies) (installPlatform, installedDependencies, error) {
	platform, err := validateInstallPlatform(deps)
	if err != nil {
		return installPlatform{}, installedDependencies{}, err
	}
	for _, command := range []string{"apt-get", "ip", "ss", "sudo", "systemctl"} {
		if _, err := deps.lookPath(command); err != nil {
			return installPlatform{}, installedDependencies{}, fmt.Errorf("HA install requires apt, iproute2, sudo, and systemd on the base host; missing %s", command)
		}
	}
	if output, err := deps.run(ctx, "systemctl", "show", "--property=Version", "--value"); err != nil || strings.TrimSpace(string(output)) == "" {
		return installPlatform{}, installedDependencies{}, fmt.Errorf("HA install requires a running systemd manager: %s", commandError(output, err))
	}
	if err := validateRelease(ctx, source, deps); err != nil {
		return installPlatform{}, installedDependencies{}, err
	}
	installed, err := inspectDedicatedHost(ctx, deps)
	return platform, installed, err
}

func validateEtcdRootPassword(path, secretsDir string) error {
	rootPassword, err := readPassword(path)
	if err != nil {
		return err
	}
	decoded, err := hex.DecodeString(rootPassword)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != rootPassword {
		return errors.New("password must be generated as 32 random bytes encoded in lowercase hexadecimal")
	}
	for _, name := range append([]string{fleetEtcdPasswordFile}, databasePasswordFiles...) {
		servicePassword, err := readPassword(filepath.Join(secretsDir, name))
		if err != nil {
			return fmt.Errorf("read service password %s: %w", name, err)
		}
		if subtle.ConstantTimeCompare([]byte(rootPassword), []byte(servicePassword)) == 1 {
			return fmt.Errorf("password must differ from %s", name)
		}
	}
	fleetEnvironment, err := loadFleetEnvironment(filepath.Join(secretsDir, fleetEnvironmentFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", fleetEnvironmentFile, err)
	}
	if subtle.ConstantTimeCompare([]byte(rootPassword), []byte(fleetEnvironment["AUTH_CLIENT_SECRET_KEY"])) == 1 {
		return errors.New("password must differ from AUTH_CLIENT_SECRET_KEY")
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

// inspectDedicatedHost is read-only so the guided installer can show whether
// dependencies will be reused before asking for destructive confirmation.
func inspectDedicatedHost(ctx context.Context, deps installDependencies) (installedDependencies, error) {
	var installed installedDependencies
	for _, path := range []string{
		installBase, configRoot, dataRoot,
		"/etc/systemd/system/proto-fleet-ha.service.d",
		"/etc/systemd/system/proto-fleet-ha-firewall.service.d",
	} {
		if err := deps.requireEmpty(path, "existing service state"); err != nil {
			return installedDependencies{}, fmt.Errorf("HA install failed: %w", err)
		}
	}
	for _, path := range []string{
		serviceUnit, firewallUnit,
		"/usr/local/libexec/proto-fleet/check-fleet-active",
	} {
		if _, err := deps.lstat(path); err == nil {
			return installedDependencies{}, fmt.Errorf("HA install requires a dedicated host without existing Proto Fleet state; found %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return installedDependencies{}, fmt.Errorf("inspect dedicated-host path %s: %w", path, err)
		}
	}
	if err := rejectExistingPath(deps, "/etc/docker/daemon.json", "custom Docker daemon configuration"); err != nil {
		return installedDependencies{}, err
	}
	if err := rejectExistingPath(deps, "/etc/systemd/system/docker.service", "foreign Docker systemd unit"); err != nil {
		return installedDependencies{}, err
	}
	if err := rejectExistingPath(deps, "/etc/keepalived/keepalived.conf", "existing keepalived configuration"); err != nil {
		return installedDependencies{}, err
	}
	if err := rejectExistingPath(deps, "/etc/systemd/system/keepalived.service", "foreign keepalived systemd unit"); err != nil {
		return installedDependencies{}, err
	}
	for _, path := range []string{"/etc/docker", "/etc/systemd/system/docker.service.d", "/etc/systemd/system/keepalived.service.d"} {
		if err := deps.requireEmpty(path, "foreign service override"); err != nil {
			return installedDependencies{}, fmt.Errorf("HA install failed: %w", err)
		}
	}

	if _, err := deps.lookPath("docker"); err == nil {
		installed.docker = true
		if output, err := deps.run(ctx, "sudo", "docker", "compose", "version"); err != nil {
			return installedDependencies{}, fmt.Errorf("existing Docker installation requires working Compose v2: %s", commandError(output, err))
		}
		output, err := deps.run(ctx, "sudo", "docker", "ps", "-aq")
		if err != nil {
			return installedDependencies{}, fmt.Errorf("inspect existing Docker containers: %s", commandError(output, err))
		}
		if strings.TrimSpace(string(output)) != "" {
			return installedDependencies{}, errors.New("existing Docker installation has existing containers; remove them before installing Proto Fleet HA")
		}
	} else {
		for _, path := range []string{"/var/lib/docker", "/var/lib/containerd", "/lib/systemd/system/docker.service"} {
			if _, statErr := deps.lstat(path); statErr == nil {
				return installedDependencies{}, fmt.Errorf("partial Docker installation found at %s; repair or remove it before installing Proto Fleet HA", path)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return installedDependencies{}, fmt.Errorf("inspect %s: %w", path, statErr)
			}
		}
	}

	keepalivedInstalled := false
	if _, err := deps.lookPath("keepalived"); err == nil {
		keepalivedInstalled = true
	} else if info, statErr := deps.lstat("/usr/sbin/keepalived"); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return installedDependencies{}, errors.New("partial keepalived installation found at /usr/sbin/keepalived; repair or remove it before installing Proto Fleet HA")
		}
		keepalivedInstalled = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return installedDependencies{}, fmt.Errorf("inspect /usr/sbin/keepalived: %w", statErr)
	}
	if keepalivedInstalled {
		installed.keepalived = true
		if state, err := systemdUnitState(ctx, deps, "is-active", "keepalived.service"); state != "inactive" {
			if err != nil {
				return installedDependencies{}, fmt.Errorf("inspect keepalived active state: %w", err)
			}
			return installedDependencies{}, fmt.Errorf("keepalived must be inactive before HA installation; found %s", state)
		}
		if state, err := systemdUnitState(ctx, deps, "is-enabled", "keepalived.service"); state != "disabled" {
			if err != nil {
				return installedDependencies{}, fmt.Errorf("inspect keepalived enabled state: %w", err)
			}
			return installedDependencies{}, fmt.Errorf("keepalived must be disabled before HA installation; found %s", state)
		}
	} else if _, statErr := deps.lstat("/lib/systemd/system/keepalived.service"); statErr == nil {
		return installedDependencies{}, errors.New("partial keepalived installation found; repair or remove it before installing Proto Fleet HA")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return installedDependencies{}, fmt.Errorf("inspect keepalived package state: %w", statErr)
	}
	return installed, nil
}

func rejectExistingPath(deps installDependencies, path, label string) error {
	if _, err := deps.lstat(path); err == nil {
		return fmt.Errorf("HA install rejected %s at %s", label, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	return nil
}

func systemdUnitState(ctx context.Context, deps installDependencies, command, unit string) (string, error) {
	output, err := deps.run(ctx, "sudo", "systemctl", command, unit)
	state := strings.TrimSpace(string(output))
	if state != "" {
		return state, nil
	}
	return "", err
}

func validateInstallPlatform(deps installDependencies) (installPlatform, error) {
	if deps.goos != "linux" {
		return installPlatform{}, errors.New("HA install supports Linux only")
	}
	contents, err := deps.readFile("/etc/os-release")
	if err != nil {
		return installPlatform{}, fmt.Errorf("read /etc/os-release: %w", err)
	}
	values := parseOSRelease(string(contents))
	if values["ID"] == "raspbian" && deps.goarch != "arm64" {
		return installPlatform{}, errors.New("HA install requires 64-bit Raspberry Pi OS")
	}
	if deps.goarch != "amd64" && deps.goarch != "arm64" {
		return installPlatform{}, fmt.Errorf("HA install supports amd64 and arm64, not %s", deps.goarch)
	}
	if deps.pageSize != 4096 {
		return installPlatform{}, fmt.Errorf("HA install requires a 4096-byte page size; found %d bytes. Boot a 4K-page kernel, reboot, verify with `getconf PAGESIZE`, then retry", deps.pageSize)
	}

	id, version := values["ID"], values["VERSION_ID"]
	platform := installPlatform{repository: "debian"}
	switch id {
	case "debian":
		if version != "12" && version != "13" {
			return installPlatform{}, fmt.Errorf("HA install requires Debian 12 or 13; found Debian %s", version)
		}
	case "ubuntu":
		if version != "22.04" && version != "24.04" {
			return installPlatform{}, fmt.Errorf("HA install requires Ubuntu 22.04 or 24.04; found Ubuntu %s", version)
		}
		platform.repository = "ubuntu"
	case "raspbian":
		if version != "12" && version != "13" {
			return installPlatform{}, fmt.Errorf("HA install requires Raspberry Pi OS based on Debian 12 or 13; found %s", version)
		}
	}

	platform.suite = values["VERSION_CODENAME"]
	if id == "ubuntu" || slices.Contains(strings.Fields(values["ID_LIKE"]), "ubuntu") {
		platform.repository = "ubuntu"
		if values["UBUNTU_CODENAME"] != "" {
			platform.suite = values["UBUNTU_CODENAME"]
		}
	}
	if platform.suite == "" {
		return installPlatform{}, errors.New("HA install requires VERSION_CODENAME in /etc/os-release for this Linux distribution")
	}
	if !validReleaseCodename(platform.suite) {
		return installPlatform{}, fmt.Errorf("HA install found invalid release codename %q", platform.suite)
	}
	return platform, nil
}

func validReleaseCodename(value string) bool {
	for i, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || (i > 0 && (char == '.' || char == '-')) {
			continue
		}
		return false
	}
	return value != ""
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
	if _, err := haDatabaseImage(source, deps.readFile); err != nil {
		return err
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

func installPackages(ctx context.Context, platform installPlatform, installed installedDependencies, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", "apt-get", "install", "-y", "ca-certificates", "curl", "iproute2"); err != nil {
		return fmt.Errorf("install HA prerequisites: %s", commandError(output, err))
	}
	if !installed.docker {
		if output, err := deps.run(ctx, "sudo", "install", "-m", "0755", "-d", "/etc/apt/keyrings"); err != nil {
			return fmt.Errorf("install HA prerequisites: %s", commandError(output, err))
		}
		key, err := deps.run(ctx, "curl", "-fsSL", "https://download.docker.com/linux/"+platform.repository+"/gpg")
		if err != nil {
			return fmt.Errorf("download Docker repository key: %s", commandError(key, err))
		}
		if err := deps.runInput(ctx, string(key), "sudo", "tee", "/etc/apt/keyrings/docker.asc"); err != nil {
			return fmt.Errorf("install Docker repository key: %w", err)
		}
		if output, err := deps.run(ctx, "sudo", "chmod", "a+r", "/etc/apt/keyrings/docker.asc"); err != nil {
			return fmt.Errorf("protect Docker repository key: %s", commandError(output, err))
		}
		repository := "Types: deb\nURIs: https://download.docker.com/linux/" + platform.repository + "\nSuites: " + platform.suite + "\nComponents: stable\nArchitectures: " + deps.goarch + "\nSigned-By: /etc/apt/keyrings/docker.asc\n"
		if err := deps.runInput(ctx, repository, "sudo", "tee", "/etc/apt/sources.list.d/docker.sources"); err != nil {
			return fmt.Errorf("configure Docker repository: %w", err)
		}
		if output, err := deps.run(ctx, "sudo", "apt-get", "update"); err != nil {
			return fmt.Errorf("install HA packages: %s", commandError(output, err))
		}
	}
	services := make([]string, 0, 3)
	packages := make([]string, 0, 7)
	if !installed.docker {
		services = append(services, "docker.service", "docker.socket")
		packages = append(packages, "docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin")
	}
	if !installed.keepalived {
		services = append(services, "keepalived.service")
		packages = append(packages, "keepalived")
	}
	packages = append(packages, "nftables")
	if len(services) > 0 {
		if output, err := deps.run(ctx, "sudo", append([]string{"systemctl", "mask", "--runtime"}, services...)...); err != nil {
			return fmt.Errorf("prevent HA services from starting before the firewall: %s", commandError(output, err))
		}
	}
	if output, err := deps.run(ctx, "sudo", append([]string{"apt-get", "install", "-y"}, packages...)...); err != nil {
		return fmt.Errorf("install HA packages: %s", commandError(output, err))
	}
	if len(services) > 0 {
		if output, err := deps.run(ctx, "sudo", append([]string{"systemctl", "unmask", "--runtime"}, services...)...); err != nil {
			return fmt.Errorf("restore HA service startup after package installation: %s", commandError(output, err))
		}
	}
	if !installed.keepalived {
		if output, err := deps.run(ctx, "sudo", "systemctl", "disable", "--now", "keepalived.service"); err != nil {
			return fmt.Errorf("disable keepalived until the database role is ready: %s", commandError(output, err))
		}
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
		{"chmod", "-R", "a+rX,go-w", installRoot},
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
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	output, err := deps.run(cleanupCtx, "sudo", "rm", "-rf", "--", installBase)
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

func prepareImages(ctx context.Context, source string, config NodeConfig, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", filepath.Join(installRoot, "ha", "fleet-ha"), "compose", "--env-file", filepath.Join(configRoot, "node.env"), "--file", filepath.Join(installRoot, "ha", "compose.yaml"), "pull", "etcd"); err != nil {
		return fmt.Errorf("pull etcd image: %s", commandError(output, err))
	}
	if config.isDatabaseNode() {
		if output, err := deps.run(ctx, "sudo", "docker", "load", "--input", filepath.Join(installRoot, "images", "timescaledb.tar.gz")); err != nil {
			return fmt.Errorf("load HA database images: %s", commandError(output, err))
		}
		databaseImage, err := haDatabaseImage(source, deps.readFile)
		if err != nil {
			return err
		}
		if output, err := deps.run(ctx, "sudo", "docker", "image", "inspect", databaseImage); err != nil {
			return fmt.Errorf("release archive did not load required HA database image %s: %s", databaseImage, commandError(output, err))
		}
		args := fleetComposeArgs("build", "fleet-api", "fleet-client")
		commandArgs := append([]string{filepath.Join(installRoot, "ha", "fleet-ha"), "compose"}, args...)
		if output, err := deps.run(ctx, "sudo", commandArgs...); err != nil {
			return fmt.Errorf("build Fleet images: %s", commandError(output, err))
		}
	}
	return nil
}

func haDatabaseImage(source string, readFile func(string) ([]byte, error)) (string, error) {
	contents, err := readFile(filepath.Join(source, "ha", "compose.yaml"))
	if err != nil {
		return "", fmt.Errorf("read HA Compose file: %w", err)
	}
	for line := range strings.SplitSeq(string(contents), "\n") {
		image := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		if strings.HasPrefix(image, "proto-fleet-timescaledb-ha:") {
			return image, nil
		}
	}
	return "", errors.New("HA Compose file does not name the required database image")
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
	if output, err := deps.run(ctx, "sudo", "systemctl", "enable", "proto-fleet-ha.service"); err != nil {
		return stopIncompleteHA(ctx, deps, fmt.Errorf("enable HA services: %s", commandError(output, err)))
	}
	if output, err := deps.run(ctx, "sudo", "systemctl", "start", "--no-block", "proto-fleet-ha.service"); err != nil {
		return stopIncompleteHA(ctx, deps, fmt.Errorf("start HA services: %s", commandError(output, err)))
	}
	fmt.Println("[peer waiting] HA service is enabled and will keep converging while peers join")
	for {
		if config.isDatabaseNode() {
			if _, err := deps.run(ctx, "sudo", filepath.Join(installRoot, "ha", "fleet-ha"), "status", filepath.Join(configRoot, "node.env")); err == nil {
				fmt.Println("[final readiness] HA control and failover paths are ready")
				return nil
			}
		} else if state, _ := systemdUnitState(ctx, deps, "is-active", "proto-fleet-ha.service"); state == "active" {
			fmt.Println("[final readiness] HA witness joined the etcd quorum")
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w; reconnect and run systemctl status proto-fleet-ha.service: %v", errInstallConverging, err)
		}
		if state, _ := systemdUnitState(ctx, deps, "is-failed", "proto-fleet-ha.service"); state == "failed" {
			return stopIncompleteHA(ctx, deps, errors.New("HA service failed during local startup; inspect journalctl -u proto-fleet-ha.service"))
		}
		deps.sleep(2 * time.Second)
	}
}

func stopIncompleteHA(ctx context.Context, deps installDependencies, cause error) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	var cleanupErrs []error
	if output, err := deps.run(cleanupCtx, "sudo", "systemctl", "disable", "--now", "proto-fleet-ha.service"); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("disable HA services: %s", commandError(output, err)))
	}
	if output, err := deps.run(cleanupCtx, "sudo", "rm", "-f", dockerRecoveryDropIn); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("remove Docker HA recovery hook: %s", commandError(output, err)))
	}
	if output, err := deps.run(cleanupCtx, "sudo", "systemctl", "daemon-reload"); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("reload systemd after HA cleanup: %s", commandError(output, err)))
	}
	return errors.Join(cause, errors.Join(cleanupErrs...))
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
