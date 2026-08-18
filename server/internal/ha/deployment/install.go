package deployment

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	installBase           = "/opt/proto-fleet"
	installRoot           = "/opt/proto-fleet/deployment"
	configRoot            = "/etc/proto-fleet/ha"
	dataRoot              = "/var/lib/proto-fleet/ha"
	infrastructureCompose = configRoot + "/compose.yaml"
	serviceUnit           = "/etc/systemd/system/proto-fleet-ha.service"
	firewallUnit          = "/etc/systemd/system/proto-fleet-ha-firewall.service"
	nftablesDropIn        = "/etc/systemd/system/nftables.service.d/proto-fleet-ha.conf"
	nftablesReloadConfig  = configRoot + "/nftables-reload.conf"
	firewallReplaceConfig = configRoot + "/firewall-replace.nft"
	dockerDropIn          = "/etc/systemd/system/docker.service.d/proto-fleet-ha.conf"
	dockerRecoveryDropIn  = "/etc/systemd/system/docker.service.d/proto-fleet-ha-recovery.conf"
	fleetComposeProject   = "deployment"
	minimumComposeVersion = "v2.24.4" // fleet-compose.yaml uses !override, added in this Compose release.
	updaterDropIn         = "/etc/systemd/system/proto-fleet-updater.service.d/proto-fleet-ha.conf"
	haUpdaterDropIn       = "/etc/systemd/system/proto-fleet-ha.service.d/proto-fleet-updater.conf"
	updaterBinary         = "/usr/local/libexec/proto-fleet/proto-fleet-updater"
	updaterUnit           = "/etc/systemd/system/proto-fleet-updater.service"
	updaterEnvironment    = "/etc/proto-fleet/updater.env"
	updaterStateRoot      = "/var/lib/proto-fleet-updater"
	updaterRuntimeRoot    = "/run/proto-fleet-updater"
	updaterLock           = updaterStateRoot + "/updater.lock"
	keepalivedConfig      = "/etc/keepalived/keepalived.conf"
	keepalivedOverride    = "/etc/systemd/system/keepalived.service.d/override.conf"
	keepalivedHealthCheck = "/usr/local/libexec/proto-fleet/check-fleet-active"
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

type nftablesRuleset struct {
	Nftables []nftablesRulesetEntry `json:"nftables"`
}

type nftablesRulesetEntry struct {
	Chain *nftablesChain `json:"chain"`
	Rule  *nftablesRule  `json:"rule"`
}

type nftablesChain struct {
	Family string `json:"family"`
	Table  string `json:"table"`
	Name   string `json:"name"`
	Hook   string `json:"hook"`
	Policy string `json:"policy"`
}

type nftablesRule struct {
	Family string `json:"family"`
	Table  string `json:"table"`
	Chain  string `json:"chain"`
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
	sourceRoot   func() (string, error)
	verifyVIP    func(context.Context, NodeConfig) error
	sleep        func(time.Duration)
}

func defaultInstallDependencies() installDependencies {
	return installDependencies{
		goos: runtime.GOOS, goarch: runtime.GOARCH, pageSize: os.Getpagesize(),
		readFile: os.ReadFile, lstat: os.Lstat, lookPath: exec.LookPath, requireEmpty: requireEmptyDir, validateHost: ValidateHost,
		run: runCommand, runInput: runWithInput,
		sourceRoot: ReleaseRoot, verifyVIP: verifyInstallVirtualIP, sleep: time.Sleep,
	}
}

func install(ctx context.Context, options InstallOptions, deps installDependencies) error {
	fmt.Println("[validation] Verifying installation inputs and current host state...")
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
	if options.EtcdRootPasswordFile != "" {
		if _, err := readPassword(options.EtcdRootPasswordFile); err != nil {
			return fmt.Errorf("validate etcd root password file: %w", err)
		}
	}

	fmt.Println("[package setup] Installing missing host dependencies...")
	if err := installValidationPrerequisites(ctx, config.isDatabaseNode(), deps); err != nil {
		return err
	}
	if config.isDatabaseNode() {
		if err := deps.verifyVIP(ctx, config); err != nil {
			return err
		}
	}
	if err := rejectIncompatibleNftablesInputChains(ctx, deps); err != nil {
		return err
	}
	if err := snapshotRelease(ctx, source, deps); err != nil {
		return errors.Join(err, removeReleaseSnapshot(ctx, deps))
	}
	source = installRoot

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
		return stopIncompleteHA(ctx, deps, err, false)
	}
	fmt.Println("[service startup] Enabling the local HA service...")
	startErr := initialStart(ctx, config, deps)
	if startErr != nil && !errors.Is(startErr, errInstallConverging) {
		return startErr
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
	if err := validateRelease(source, deps.readFile); err != nil {
		return installPlatform{}, installedDependencies{}, err
	}
	installed, err := inspectDedicatedHost(ctx, deps)
	return platform, installed, err
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
		installBase, configRoot, dataRoot, "/var/lib/proto-fleet-updater", "/run/proto-fleet-updater",
		"/etc/systemd/system/proto-fleet-ha.service.d",
		"/etc/systemd/system/proto-fleet-ha-firewall.service.d",
		"/etc/systemd/system/proto-fleet-updater.service.d",
	} {
		if err := deps.requireEmpty(path, "existing service state"); err != nil {
			return installedDependencies{}, fmt.Errorf("HA install failed: %w", err)
		}
	}
	for _, path := range []string{
		serviceUnit, firewallUnit, nftablesDropIn, nftablesReloadConfig,
		keepalivedHealthCheck,
		updaterBinary,
		updaterUnit,
		updaterEnvironment,
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
	if err := rejectExistingPath(deps, keepalivedConfig, "existing keepalived configuration"); err != nil {
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
		output, err := deps.run(ctx, "sudo", "docker", "compose", "version", "--short")
		if err != nil {
			return installedDependencies{}, fmt.Errorf("existing Docker installation requires working Compose v2: %s", commandError(output, err))
		}
		composeVersion := strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
		if !semver.IsValid("v" + composeVersion) {
			return installedDependencies{}, fmt.Errorf("existing Docker installation returned invalid Compose version %q", strings.TrimSpace(string(output)))
		}
		if semver.Compare("v"+composeVersion, minimumComposeVersion) < 0 {
			return installedDependencies{}, fmt.Errorf("existing Docker Compose %s is too old; Proto Fleet HA requires %s or newer", composeVersion, strings.TrimPrefix(minimumComposeVersion, "v"))
		}
		output, err = deps.run(ctx, "sudo", "docker", "ps", "-aq")
		if err != nil {
			return installedDependencies{}, fmt.Errorf("inspect existing Docker containers: %s", commandError(output, err))
		}
		if strings.TrimSpace(string(output)) != "" {
			return installedDependencies{}, errors.New("existing Docker installation has existing containers; remove them before installing Proto Fleet HA")
		}
	} else {
		for _, command := range []string{"containerd", "runc"} {
			if path, err := deps.lookPath(command); err == nil {
				return installedDependencies{}, fmt.Errorf("HA install requires Docker's bundled container runtime; remove %s before installing: %s", command, path)
			}
		}
		for _, path := range []string{"/var/lib/docker", "/var/lib/containerd"} {
			if err := deps.requireEmpty(path, "residual container runtime state"); err != nil {
				return installedDependencies{}, fmt.Errorf("HA install failed: %w", err)
			}
		}
	}

	if _, err := deps.lookPath("keepalived"); err == nil {
		installed.keepalived = true
		if state, err := systemdUnitState(ctx, deps, "is-active", "keepalived.service"); state != "inactive" {
			if err != nil {
				return installedDependencies{}, fmt.Errorf("inspect keepalived active state: %w", err)
			}
			return installedDependencies{}, fmt.Errorf("keepalived must be inactive before HA installation; found %s", state)
		}
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

func validateRelease(source string, readFile func(string) ([]byte, error)) error {
	required := []string{
		"version.txt", "docker-compose.yaml", "docker-compose.alerts.yaml", "server/docker-compose.base.yaml", "images/fleet.tar.gz", "images/timescaledb.tar.gz",
		"server/monitoring/grafana/grafana.ini", "server/monitoring/grafana/provisioning/alerting/notification-policies.yaml",
		"server/monitoring/grafana/ha/proto-fleet-ha-rules.yaml", "server/monitoring/grafana/ha/timescaledb.yaml",
		"server/Dockerfile", "server/fleetd", "server/proto-plugin", "server/antminer-plugin", "server/asicrs-plugin", "server/asicrs-config.yaml", "server/virtual-plugin", "server/virtual-plugin.json",
		"client/Dockerfile", "client/nginx.https.conf", "client/protoFleet/index.html", "client/docker-entrypoint.d/40-render-runtime-config.sh",
		"updater/proto-fleet-updater", "updater/proto-fleet-updater.service",
		"ha/updater-systemd.conf", "ha/ha-updater-systemd.conf",
		"ha/fleet-ha", "ha/compose.yaml", "ha/fleet-compose.yaml", "ha/firewall.nft.tmpl", "ha/firewall-replace.nft",
		"ha/keepalived.conf.tmpl", "ha/keepalived-systemd.conf.tmpl", "ha/proto-fleet-ha.service", "ha/proto-fleet-ha-keepalived.conf",
		"ha/proto-fleet-ha-firewall.service", "ha/nftables-systemd.conf", "ha/nftables-reload.conf", "ha/docker-systemd.conf", "ha/docker-ha-recovery-systemd.conf", "ha/scripts/check-fleet-active.sh",
	}
	for _, name := range required {
		info, err := os.Lstat(filepath.Join(source, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("release is missing %s", name)
		}
	}
	if _, err := haDatabaseImage(source, readFile); err != nil {
		return err
	}
	assets, err := os.ReadDir(filepath.Join(source, "client", "protoFleet", "assets"))
	if err != nil {
		return fmt.Errorf("read built Proto Fleet client assets: %w", err)
	}
	for _, asset := range assets {
		if asset.Type().IsRegular() {
			return nil
		}
	}
	return errors.New("release is missing built Proto Fleet client assets")
}

func sudoStep(ctx context.Context, deps installDependencies, action string, args ...string) error {
	output, err := deps.run(ctx, "sudo", args...)
	if err != nil {
		return fmt.Errorf("%s: %s", action, commandError(output, err))
	}
	return nil
}

func placeFile(ctx context.Context, deps installDependencies, action, source, target, mode string) error {
	return sudoStep(ctx, deps, action, "install", "-D", "-o", "root", "-g", "root", "-m", mode, source, target)
}

func installValidationPrerequisites(ctx context.Context, needsARPing bool, deps installDependencies) error {
	if err := sudoStep(ctx, deps, "refresh apt package indexes", "apt-get", "update"); err != nil {
		return err
	}
	packages := []string{"nftables"}
	if needsARPing {
		packages = append(packages, "iputils-arping")
	}
	return sudoStep(ctx, deps, "install HA validation prerequisites", append([]string{"apt-get", "install", "-y"}, packages...)...)
}

func installPackages(ctx context.Context, platform installPlatform, installed installedDependencies, deps installDependencies) error {
	if err := sudoStep(ctx, deps, "install HA prerequisites", "apt-get", "install", "-y", "ca-certificates", "curl", "iproute2"); err != nil {
		return err
	}
	if !installed.docker {
		if err := sudoStep(ctx, deps, "install HA prerequisites", "install", "-m", "0755", "-d", "/etc/apt/keyrings"); err != nil {
			return err
		}
		key, err := deps.run(ctx, "curl", "-fsSL", "https://download.docker.com/linux/"+platform.repository+"/gpg")
		if err != nil {
			return fmt.Errorf("download Docker repository key: %s", commandError(key, err))
		}
		if err := deps.runInput(ctx, string(key), "sudo", "tee", "/etc/apt/keyrings/docker.asc"); err != nil {
			return fmt.Errorf("install Docker repository key: %w", err)
		}
		if err := sudoStep(ctx, deps, "protect Docker repository key", "chmod", "a+r", "/etc/apt/keyrings/docker.asc"); err != nil {
			return err
		}
		repository := "Types: deb\nURIs: https://download.docker.com/linux/" + platform.repository + "\nSuites: " + platform.suite + "\nComponents: stable\nArchitectures: " + deps.goarch + "\nSigned-By: /etc/apt/keyrings/docker.asc\n"
		if err := deps.runInput(ctx, repository, "sudo", "tee", "/etc/apt/sources.list.d/docker.sources"); err != nil {
			return fmt.Errorf("configure Docker repository: %w", err)
		}
		if err := sudoStep(ctx, deps, "install HA packages", "apt-get", "update"); err != nil {
			return err
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
	if len(services) > 0 {
		if err := sudoStep(ctx, deps, "prevent HA services from starting before the firewall", append([]string{"systemctl", "mask", "--runtime"}, services...)...); err != nil {
			return err
		}
	}
	if len(packages) > 0 {
		if err := sudoStep(ctx, deps, "install HA packages", append([]string{"apt-get", "install", "-y"}, packages...)...); err != nil {
			return err
		}
	}
	if len(services) > 0 {
		if err := sudoStep(ctx, deps, "restore HA service startup after package installation", append([]string{"systemctl", "unmask", "--runtime"}, services...)...); err != nil {
			return err
		}
	}
	return sudoStep(ctx, deps, "disable keepalived until the database role is ready", "systemctl", "disable", "--now", "keepalived.service")
}

func rejectIncompatibleNftablesInputChains(ctx context.Context, deps installDependencies) error {
	ruleset, err := deps.run(ctx, "sudo", "nft", "-j", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("inspect existing nftables firewall: %s", commandError(ruleset, err))
	}
	if err := validateNftablesInputChains(ruleset); err != nil {
		return err
	}
	persistentRuleset, err := deps.run(ctx, "sudo", "unshare", "--net", "--", "/bin/sh", "-c", "/usr/sbin/nft -f /etc/nftables.conf && /usr/sbin/nft -j list ruleset")
	if err != nil {
		return fmt.Errorf("inspect persistent nftables firewall in an isolated network namespace: %s", commandError(persistentRuleset, err))
	}
	return validateNftablesInputChains(persistentRuleset)
}

func validateNftablesInputChains(ruleset []byte) error {
	var parsed nftablesRuleset
	if err := json.Unmarshal(ruleset, &parsed); err != nil {
		return fmt.Errorf("parse existing nftables ruleset: %w", err)
	}
	inputChains := make(map[string]nftablesChain)
	for _, entry := range parsed.Nftables {
		chain := entry.Chain
		if chain == nil || chain.Hook != "input" || isReservedHAFirewallTable(chain.Family, chain.Table) {
			continue
		}
		if chain.Policy != "" && chain.Policy != "accept" {
			return incompatibleNftablesInputChain(*chain, "policy "+chain.Policy)
		}
		inputChains[nftablesChainKey(chain.Family, chain.Table, chain.Name)] = *chain
	}
	for _, entry := range parsed.Nftables {
		rule := entry.Rule
		if rule == nil {
			continue
		}
		if chain, ok := inputChains[nftablesChainKey(rule.Family, rule.Table, rule.Chain)]; ok {
			return incompatibleNftablesInputChain(chain, "contains rules")
		}
	}
	return nil
}

func isReservedHAFirewallTable(family, table string) bool {
	return family == "inet" && table == "proto_fleet_ha"
}

func nftablesChainKey(family, table, chain string) string {
	return family + "\x00" + table + "\x00" + chain
}

func incompatibleNftablesInputChain(chain nftablesChain, reason string) error {
	return fmt.Errorf("existing nftables input chain %s %s %s is incompatible (%s); HA installation requires a dedicated host firewall without input filtering, so remove the input-hook chain before retrying (unrelated non-input nftables tables and chains are preserved)", chain.Family, chain.Table, chain.Name, reason)
}

func verifyInstallVirtualIP(ctx context.Context, config NodeConfig) error {
	output, err := runCommand(ctx, "sudo", "arping", "-D", "-I", config.NetworkInterface, "-c", "2", config.VirtualIP)
	if err == nil {
		return nil
	}
	if config.NodeName == "ha-a" {
		return fmt.Errorf("HA virtual IP is already owned or cannot be checked: %s", commandError(output, err))
	}

	peerIP := config.DatabaseAIP
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
		if err := sudoStep(ctx, deps, "install HA release", args...); err != nil {
			return err
		}
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
		{"install", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(installRoot, "ha", "compose.yaml"), infrastructureCompose},
	} {
		if err := sudoStep(ctx, deps, "install HA release", args...); err != nil {
			return err
		}
	}
	for _, name := range copiedSecretFiles(config) {
		if err := placeFile(ctx, deps, "install HA secret "+name, filepath.Join(config.SecretsDir, name), filepath.Join(configRoot, name), "0600"); err != nil {
			return err
		}
	}
	installedConfig := config
	installedConfig.SecretsDir = configRoot
	temp, err := writeInstallTemp("node.env", renderNodeEnvironment(installedConfig), 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if err := placeFile(ctx, deps, "install node configuration", temp, filepath.Join(configRoot, "node.env"), "0600"); err != nil {
		return err
	}
	baseEnv, err := writeInstallTemp("fleet-base.env", "DB_USERNAME=fleet\nDB_PASSWORD=unused\n", 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(baseEnv)
	if err := placeFile(ctx, deps, "install Fleet base environment", baseEnv, filepath.Join(configRoot, "base.env"), "0600"); err != nil {
		return err
	}
	for sourceName, target := range map[string]string{
		"proto-fleet-ha.service":          serviceUnit,
		"proto-fleet-ha-firewall.service": firewallUnit,
		"nftables-systemd.conf":           nftablesDropIn,
		"nftables-reload.conf":            nftablesReloadConfig,
		"firewall-replace.nft":            firewallReplaceConfig,
		"docker-systemd.conf":             dockerDropIn,
	} {
		if err := placeFile(ctx, deps, "install HA systemd unit", filepath.Join(installRoot, "ha", sourceName), target, "0644"); err != nil {
			return err
		}
	}
	if config.isDatabaseNode() {
		if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0755", filepath.Join(installRoot, "updater", "proto-fleet-updater"), updaterBinary); err != nil {
			return fmt.Errorf("install host updater: %s", commandError(output, err))
		}
		if output, err := deps.run(ctx, "sudo", "install", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(installRoot, "updater", "proto-fleet-updater.service"), updaterUnit); err != nil {
			return fmt.Errorf("install host updater service: %s", commandError(output, err))
		}
		for sourceName, target := range map[string]string{
			"updater-systemd.conf":    updaterDropIn,
			"ha-updater-systemd.conf": haUpdaterDropIn,
		} {
			if output, err := deps.run(ctx, "sudo", "install", "-D", "-o", "root", "-g", "root", "-m", "0644", filepath.Join(installRoot, "ha", sourceName), target); err != nil {
				return fmt.Errorf("install updater recovery ordering: %s", commandError(output, err))
			}
		}
		updaterEnv := fmt.Sprintf(
			"PROTO_FLEET_UPDATER_DEPLOYMENT_MODE=ha\nPROTO_FLEET_INSTALL_ROOT=%s\nPROTO_FLEET_UPDATER_BINARY_PATH=%s\n",
			installBase, updaterBinary,
		)
		temp, err := writeInstallTemp("updater.env", updaterEnv, 0o600)
		if err != nil {
			return err
		}
		defer os.Remove(temp)
		if output, err := deps.run(ctx, "sudo", "install", "-o", "root", "-g", "root", "-m", "0600", temp, updaterEnvironment); err != nil {
			return fmt.Errorf("install host updater configuration: %s", commandError(output, err))
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
	if err := placeFile(ctx, deps, "persist HA firewall", temp, filepath.Join(configRoot, "firewall.nft"), "0600"); err != nil {
		return err
	}
	if output, err := deps.run(ctx, "sudo", "nft", "-c", "-f", nftablesReloadConfig); err != nil {
		return fmt.Errorf("validate combined nftables reload: %s", commandError(output, err))
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
		keepalivedConfig: rendered,
		keepalivedOverride: strings.NewReplacer(
			"${HA_VIRTUAL_IP}", config.VirtualIP,
			"${HA_NETWORK_INTERFACE}", config.NetworkInterface,
		).Replace(string(systemdTemplate)),
	} {
		temp, err := writeInstallTemp(filepath.Base(name), contents, 0o600)
		if err != nil {
			return err
		}
		defer os.Remove(temp)
		if err := placeFile(ctx, deps, "install keepalived configuration", temp, name, "0644"); err != nil {
			return err
		}
	}
	if err := placeFile(ctx, deps, "install keepalived health check", filepath.Join(source, "ha", "scripts", "check-fleet-active.sh"), keepalivedHealthCheck, "0755"); err != nil {
		return err
	}
	return placeFile(ctx, deps, "install keepalived service dependency", filepath.Join(source, "ha", "proto-fleet-ha-keepalived.conf"), "/etc/systemd/system/proto-fleet-ha.service.d/keepalived.conf", "0644")
}

func prepareImages(ctx context.Context, source string, config NodeConfig, deps installDependencies) error {
	if output, err := deps.run(ctx, "sudo", filepath.Join(installRoot, "ha", "fleet-ha"), "compose", "--env-file", filepath.Join(configRoot, "node.env"), "--file", infrastructureCompose, "pull", "etcd"); err != nil {
		return fmt.Errorf("pull etcd image: %s", commandError(output, err))
	}
	if config.isDatabaseNode() {
		pullArgs := fleetComposeArgs("pull", "grafana")
		if output, err := deps.run(ctx, "sudo", append([]string{filepath.Join(installRoot, "ha", "fleet-ha"), "compose"}, pullArgs...)...); err != nil {
			return fmt.Errorf("pull Grafana image: %s", commandError(output, err))
		}
		for _, archive := range []string{"timescaledb.tar.gz", "fleet.tar.gz"} {
			if output, err := deps.run(ctx, "sudo", "docker", "load", "--input", filepath.Join(installRoot, "images", archive)); err != nil {
				return fmt.Errorf("load release images from %s: %s", archive, commandError(output, err))
			}
		}
		images := []struct {
			file   string
			prefix string
		}{
			{file: "ha/compose.yaml", prefix: "proto-fleet-timescaledb-ha:"},
			{file: "docker-compose.yaml", prefix: "proto-fleet-api:"},
			{file: "docker-compose.yaml", prefix: "proto-fleet-client:"},
		}
		for _, expected := range images {
			image, err := composeImage(source, expected.file, expected.prefix, deps.readFile)
			if err != nil {
				return err
			}
			if output, err := deps.run(ctx, "sudo", "docker", "image", "inspect", image); err != nil {
				return fmt.Errorf("release archive did not load required image %s: %s", image, commandError(output, err))
			}
		}
	}
	return nil
}

func haDatabaseImage(source string, readFile func(string) ([]byte, error)) (string, error) {
	return composeImage(source, "ha/compose.yaml", "proto-fleet-timescaledb-ha:", readFile)
}

func composeImage(source, name, prefix string, readFile func(string) ([]byte, error)) (string, error) {
	contents, err := readFile(filepath.Join(source, name))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	for line := range strings.SplitSeq(string(contents), "\n") {
		image := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		if strings.HasPrefix(image, prefix) {
			return image, nil
		}
	}
	return "", fmt.Errorf("%s does not name required image %s", name, prefix)
}

func installDockerRecoveryHook(ctx context.Context, deps installDependencies) error {
	if err := placeFile(ctx, deps, "install Docker HA recovery hook", filepath.Join(installRoot, "ha", "docker-ha-recovery-systemd.conf"), dockerRecoveryDropIn, "0644"); err != nil {
		return err
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
	return placeFile(ctx, deps, "install etcd root password", options.EtcdRootPasswordFile, installedRootPassword, "0600")
}

func initialStart(ctx context.Context, config NodeConfig, deps installDependencies) error {
	cleanupUpdater := false
	if output, err := deps.run(ctx, "sudo", "systemctl", "enable", "proto-fleet-ha.service"); err != nil {
		return stopIncompleteHA(ctx, deps, fmt.Errorf("enable HA services: %s", commandError(output, err)), cleanupUpdater)
	}
	if config.isDatabaseNode() {
		if output, err := deps.run(ctx, "sudo", "systemctl", "enable", "proto-fleet-updater.service"); err != nil {
			return stopIncompleteHA(ctx, deps, fmt.Errorf("enable host updater: %s", commandError(output, err)), true)
		}
		cleanupUpdater = true
	}
	if output, err := deps.run(ctx, "sudo", "systemctl", "start", "--no-block", "proto-fleet-ha.service"); err != nil {
		return stopIncompleteHA(ctx, deps, fmt.Errorf("start HA services: %s", commandError(output, err)), cleanupUpdater)
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
			return stopIncompleteHA(ctx, deps, errors.New("HA service failed during local startup; inspect journalctl -u proto-fleet-ha.service"), cleanupUpdater)
		}
		deps.sleep(2 * time.Second)
	}
}

func stopIncompleteHA(ctx context.Context, deps installDependencies, cause error, cleanupUpdater bool) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	var cleanupErrs []error
	if cleanupUpdater {
		if output, err := deps.run(cleanupCtx, "sudo", "systemctl", "disable", "--now", "proto-fleet-updater.service"); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("disable incomplete host updater: %s", commandError(output, err)))
		}
	}
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
	return fleetComposeArgsAt(installRoot, operation, services...)
}

func fleetComposeArgsAt(root, operation string, services ...string) []string {
	return fleetComposeArgsAtProfile(root, true, operation, services...)
}

func fleetComposeArgsAtProfile(root string, includeAlerts bool, operation string, services ...string) []string {
	args := []string{
		"--project-name", fleetComposeProject,
		"--env-file", filepath.Join(configRoot, "base.env"),
		"--env-file", filepath.Join(configRoot, fleetEnvironmentFile),
		"--env-file", filepath.Join(configRoot, "node.env"),
		"--file", filepath.Join(root, "docker-compose.yaml"),
	}
	if includeAlerts {
		args = append(args, "--file", filepath.Join(root, "docker-compose.alerts.yaml"))
	}
	args = append(args, "--file", filepath.Join(root, "ha", "fleet-compose.yaml"), operation)
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

// ReleaseRoot returns the packaged deployment containing this fleet-ha binary.
func ReleaseRoot() (string, error) {
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
