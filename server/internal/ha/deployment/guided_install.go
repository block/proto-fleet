package deployment

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/term"
)

const (
	bundleFormatVersion = 1
	maxBundleSize       = 2 << 20
)

var (
	sshUsernamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	releaseVersionPattern = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z]+)*|nightly-[0-9]{8}-[0-9a-f]{12})$`)
	releaseCommitPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type clusterMetadata struct {
	Version     string
	Commit      string
	DatabaseAIP string
	DatabaseBIP string
	WitnessIP   string
	VirtualIP   string
}

type bundleMetadata struct {
	FormatVersion int    `json:"format_version"`
	Role          string `json:"role"`
	NodeIP        string `json:"node_ip"`
	DatabaseAIP   string `json:"database_a_ip"`
	DatabaseBIP   string `json:"database_b_ip"`
	WitnessIP     string `json:"witness_ip"`
	VirtualIP     string `json:"virtual_ip"`
	Version       string `json:"release_version"`
	Commit        string `json:"release_commit"`
}

type preparedHostBundle struct {
	Metadata         bundleMetadata    `json:"metadata"`
	Secrets          map[string][]byte `json:"secrets"`
	EtcdRootPassword []byte            `json:"etcd_root_password,omitempty"`
}

type hostIdentity struct {
	address          string
	networkInterface string
}

type guidedInstallDependencies struct {
	input            io.Reader
	output           io.Writer
	prompts          io.Writer
	terminal         func() bool
	sourceRoot       func() (string, error)
	primaryIdentity  func(context.Context, string) (hostIdentity, error)
	interfaceForIP   func(string) (string, error)
	makeExportDir    func() (string, error)
	operatorUsername func() string
	checkPeer        func(context.Context, string, string) error
	transferBundle   func(context.Context, string, string, string) error
	inspect          func(context.Context, string, NodeConfig, fleetApplicationProfile) (installedDependencies, error)
	install          func(context.Context, InstallOptions) error
}

// GuidedInstall prepares a new HA cluster when bundlePath is empty, or installs
// the current host from a prepared bundle.
func GuidedInstall(ctx context.Context, bundlePath string) error {
	return guidedInstall(ctx, bundlePath, defaultGuidedInstallDependencies())
}

func defaultGuidedInstallDependencies() guidedInstallDependencies {
	return guidedInstallDependencies{
		input: os.Stdin, output: os.Stdout, prompts: os.Stderr,
		terminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd()))
		},
		sourceRoot: ReleaseRoot,
		primaryIdentity: func(ctx context.Context, peer string) (hostIdentity, error) {
			output, err := runCommand(ctx, "ip", "route", "get", peer)
			if err != nil {
				return hostIdentity{}, fmt.Errorf("detect HA peer route: %s", commandError(output, err))
			}
			address, addressOK := routeSource(output)
			networkInterface, interfaceOK := routeDevice(output)
			if !addressOK || !interfaceOK {
				return hostIdentity{}, errors.New("detect HA peer route: response did not contain a source address and interface")
			}
			return hostIdentity{address: address, networkInterface: networkInterface}, nil
		},
		interfaceForIP:   networkInterfaceForIP,
		makeExportDir:    makeBundleExportDir,
		operatorUsername: invokingUsername,
		checkPeer: func(ctx context.Context, localUsername, target string) error {
			return runSSH(ctx, localUsername, nil, target,
				`command -v curl >/dev/null 2>&1 || { echo "curl is required for Proto Fleet HA installation" >&2; exit 1; }`)
		},
		transferBundle: func(ctx context.Context, localUsername, target, bundlePath string) error {
			bundle, err := os.Open(bundlePath)
			if err != nil {
				return fmt.Errorf("open host bundle: %w", err)
			}
			defer bundle.Close()
			return runSSH(ctx, localUsername, bundle, target, remoteBundleInstallCommand)
		},
		inspect: func(ctx context.Context, source string, config NodeConfig, profile fleetApplicationProfile) (installedDependencies, error) {
			host := hostEnvironment{
				goos: runtime.GOOS, localIPs: localAddresses, interfacePrefixes: interfaceIPv4Prefixes,
				runCommand: runCommand,
			}
			if err := validateNodeConfig(config); err != nil {
				return installedDependencies{}, fmt.Errorf("HA preflight failed: %w", err)
			}
			if err := validateHostEnvironment(ctx, config, host, false, profile); err != nil {
				return installedDependencies{}, err
			}
			_, installed, err := inspectInstallBase(ctx, source, defaultInstallDependencies())
			return installed, err
		},
		install: func(ctx context.Context, options InstallOptions) error {
			return install(ctx, options, defaultInstallDependencies())
		},
	}
}

func guidedInstall(ctx context.Context, bundlePath string, deps guidedInstallDependencies) error {
	if bundlePath != "" {
		if err := os.Unsetenv("DD_API_KEY"); err != nil {
			return fmt.Errorf("clear DD_API_KEY before prepared host installation: %w", err)
		}
	}
	source, err := deps.sourceRoot()
	if err != nil {
		return err
	}
	release, err := readReleaseIdentity(filepath.Join(source, "version.txt"))
	if err != nil {
		return err
	}
	if bundlePath != "" {
		return installPreparedHost(ctx, source, bundlePath, release, false, deps)
	}
	if !deps.terminal() {
		return errors.New("fleet-ha install requires an interactive terminal; use ssh -t HOST 'fleet-ha install'")
	}
	scanner := bufio.NewScanner(deps.input)
	return prepareAndInstallCluster(ctx, source, release, scanner, deps)
}

func prepareAndInstallCluster(ctx context.Context, source string, release clusterMetadata, scanner *bufio.Scanner, deps guidedInstallDependencies) error {
	metadata := release
	applicationProfile, err := captureFleetApplicationEnvironment()
	if err != nil {
		return fmt.Errorf("validate HA feature configuration: %w", err)
	}
	if metadata.DatabaseBIP, err = readPrompt(scanner, deps.prompts, "ha-b IPv4 address: "); err != nil {
		return err
	}
	if metadata.WitnessIP, err = readPrompt(scanner, deps.prompts, "ha-c IPv4 address: "); err != nil {
		return err
	}
	if metadata.VirtualIP, err = readPrompt(scanner, deps.prompts, "Virtual IPv4 address: "); err != nil {
		return err
	}
	localUsername := deps.operatorUsername()
	peerUsername, err := readPromptWithDefault(scanner, deps.prompts, "Peer SSH username", localUsername)
	if err != nil {
		return err
	}
	if !sshUsernamePattern.MatchString(peerUsername) {
		return errors.New("peer SSH username contains unsupported characters")
	}
	identity, err := deps.primaryIdentity(ctx, metadata.DatabaseBIP)
	if err != nil {
		return err
	}
	metadata.DatabaseAIP = identity.address
	if err := validateClusterMetadata(metadata); err != nil {
		return err
	}
	if err := writeInstallerOutput(deps.output, "[validation] Checking the release, topology, network, and dedicated host...\n"); err != nil {
		return err
	}
	config := NodeConfig{
		NodeName: "ha-a", NodeIP: metadata.DatabaseAIP, DatabaseAIP: metadata.DatabaseAIP,
		DatabaseBIP: metadata.DatabaseBIP, WitnessIP: metadata.WitnessIP, VirtualIP: metadata.VirtualIP,
		NetworkInterface: identity.networkInterface, DataDir: dataRoot, SecretsDir: configRoot,
	}
	installed, err := deps.inspect(ctx, source, config, applicationProfile)
	if err != nil {
		return err
	}
	if err := printInstallSummary(deps.prompts, bundleMetadata{
		Role: "ha-a", NodeIP: metadata.DatabaseAIP, DatabaseAIP: metadata.DatabaseAIP,
		DatabaseBIP: metadata.DatabaseBIP, WitnessIP: metadata.WitnessIP, VirtualIP: metadata.VirtualIP,
		Version: metadata.Version, Commit: metadata.Commit,
	}, identity.networkInterface, installed); err != nil {
		return err
	}
	if err := requireAcknowledgement(scanner, deps.prompts, "Type INSTALL to continue: ", "INSTALL"); err != nil {
		return err
	}

	if err := writeInstallerOutput(deps.output, "[bundle generation] Creating protected host bundles...\n"); err != nil {
		return err
	}
	exportDir, err := deps.makeExportDir()
	if err != nil {
		return err
	}
	if err := prepareInstallBundles(exportDir, metadata, applicationProfile); err != nil {
		return fmt.Errorf("bundle generation failed; partial exports remain at %s: %w", exportDir, err)
	}
	haABundle := filepath.Join(exportDir, hostBundleName("ha-a"))
	bundle, err := readHostBundle(haABundle)
	if err != nil {
		return fmt.Errorf("read generated ha-a bundle; bundle exports remain at %s: %w", exportDir, err)
	}
	fingerprint, err := serviceCAFingerprint(bundle.Secrets["service-ca.crt"])
	if err != nil {
		return fmt.Errorf("fingerprint public service CA; bundle exports remain at %s: %w", exportDir, err)
	}
	if err := writeInstallerOutput(deps.output, "Public service CA SHA-256 fingerprint: %s\n", fingerprint); err != nil {
		return err
	}
	peers := []struct {
		role    string
		address string
	}{
		{role: "ha-b", address: metadata.DatabaseBIP},
		{role: "ha-c", address: metadata.WitnessIP},
	}
	for _, peer := range peers {
		target := peerUsername + "@" + peer.address
		if err := deps.checkPeer(ctx, localUsername, target); err != nil {
			return fmt.Errorf("connect to %s for %s; bundle exports remain at %s: %w", target, peer.role, exportDir, err)
		}
	}
	for _, peer := range peers {
		target := peerUsername + "@" + peer.address
		bundlePath := filepath.Join(exportDir, hostBundleName(peer.role))
		if err := deps.transferBundle(ctx, localUsername, target, bundlePath); err != nil {
			return fmt.Errorf("transfer %s bundle to %s; bundle exports remain at %s: %w", peer.role, target, exportDir, err)
		}
	}

	if err := installPreparedHost(ctx, source, haABundle, release, true, deps); err != nil {
		return err
	}
	if err := removeCopiedExports(exportDir); err != nil {
		return err
	}
	_ = os.Remove(exportDir)
	return printPeerInstallCommands(deps.output, peerUsername, metadata)
}

func makeBundleExportDir() (string, error) {
	dir, err := os.MkdirTemp("", "proto-fleet-ha-bundles-")
	if err != nil {
		return "", fmt.Errorf("create protected bundle export directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // Protected directories need owner traversal permission.
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("protect bundle export directory: %w", err)
	}
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("resolve protected bundle export directory: %w", err)
	}
	return absoluteDir, nil
}

func installPreparedHost(ctx context.Context, source, bundlePath string, release clusterMetadata, skipPreflight bool, deps guidedInstallDependencies) error {
	bundle, err := readHostBundle(bundlePath)
	if err != nil {
		return err
	}
	if bundle.Metadata.Version != release.Version || bundle.Metadata.Commit != release.Commit {
		return fmt.Errorf("host bundle release does not match this release: bundle=%s@%s local=%s@%s",
			bundle.Metadata.Version, bundle.Metadata.Commit, release.Version, release.Commit)
	}
	networkInterface, err := deps.interfaceForIP(bundle.Metadata.NodeIP)
	if err != nil {
		return fmt.Errorf("bundle node address %s is not assigned to this host", bundle.Metadata.NodeIP)
	}
	stagingDir, err := os.MkdirTemp("", "proto-fleet-ha-install-")
	if err != nil {
		return fmt.Errorf("create protected install staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	if err := os.Chmod(stagingDir, 0o700); err != nil { //nolint:gosec // Protected directories need owner traversal permission.
		return fmt.Errorf("protect install staging directory: %w", err)
	}
	options, err := stageHostBundle(bundle, stagingDir, networkInterface)
	if err != nil {
		return err
	}
	config, err := loadNodeConfig(options.NodeEnvPath)
	if err != nil {
		return err
	}
	if !skipPreflight {
		if err := writeInstallerOutput(deps.output, "[validation] Checking the bundle, release, network, and dedicated host...\n"); err != nil {
			return err
		}
		profile := fleetApplicationProfile{}
		if config.isDatabaseNode() {
			profile, err = loadValidatedFleetApplicationProfile(filepath.Join(config.SecretsDir, fleetEnvironmentFile))
			if err != nil {
				return err
			}
		}
		installed, err := deps.inspect(ctx, source, config, profile)
		if err != nil {
			return err
		}
		if err := printInstallSummary(deps.prompts, bundle.Metadata, networkInterface, installed); err != nil {
			return err
		}
	}
	if err := writeInstallerOutput(deps.output, "Installing %s from the verified host bundle...\n", bundle.Metadata.Role); err != nil {
		return err
	}
	installErr := deps.install(ctx, options)
	if installErr != nil && !errors.Is(installErr, errInstallConverging) {
		return installErr
	}
	if err := os.Remove(bundlePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove consumed host bundle: %w", err)
	}
	return installErr
}

func stageHostBundle(bundle preparedHostBundle, stagingDir, networkInterface string) (InstallOptions, error) {
	secretsDir := filepath.Join(stagingDir, "secrets")
	if err := os.Mkdir(secretsDir, 0o700); err != nil {
		return InstallOptions{}, fmt.Errorf("create staged secrets directory: %w", err)
	}
	for _, name := range copiedSecretFiles(NodeConfig{NodeName: bundle.Metadata.Role}) {
		contents := bundle.Secrets[name]
		if err := writeFile(filepath.Join(secretsDir, name), contents, secretFileMode(name)); err != nil {
			return InstallOptions{}, err
		}
	}
	config := NodeConfig{
		NodeName: bundle.Metadata.Role, NodeIP: bundle.Metadata.NodeIP,
		DatabaseAIP: bundle.Metadata.DatabaseAIP, DatabaseBIP: bundle.Metadata.DatabaseBIP,
		WitnessIP: bundle.Metadata.WitnessIP, VirtualIP: bundle.Metadata.VirtualIP,
		NetworkInterface: networkInterface, DataDir: dataRoot, SecretsDir: secretsDir,
	}
	nodeEnvPath := filepath.Join(stagingDir, "node.env")
	if err := writeFile(nodeEnvPath, []byte(renderNodeEnvironment(config)), 0o600); err != nil {
		return InstallOptions{}, err
	}
	options := InstallOptions{NodeEnvPath: nodeEnvPath}
	if len(bundle.EtcdRootPassword) != 0 {
		options.EtcdRootPasswordFile = filepath.Join(stagingDir, etcdRootPasswordFile)
		if err := writeFile(options.EtcdRootPasswordFile, bundle.EtcdRootPassword, 0o600); err != nil {
			return InstallOptions{}, err
		}
	}
	return options, nil
}

func prepareInstallBundles(exportDir string, metadata clusterMetadata, environment fleetApplicationProfile) (err error) {
	generated, err := os.MkdirTemp("", "proto-fleet-ha-secrets-")
	if err != nil {
		return fmt.Errorf("create secret generation workspace: %w", err)
	}
	defer os.RemoveAll(generated)
	secretsRoot := filepath.Join(generated, "generated")
	if err := GenerateSecrets(secretsRoot, [3]string{metadata.DatabaseAIP, metadata.DatabaseBIP, metadata.WitnessIP}, metadata.VirtualIP); err != nil {
		return err
	}
	deploymentEnvironment := renderFleetDeploymentEnvironment(environment)

	for _, role := range []string{"ha-a", "ha-b", "ha-c"} {
		nodeIP := map[string]string{"ha-a": metadata.DatabaseAIP, "ha-b": metadata.DatabaseBIP, "ha-c": metadata.WitnessIP}[role]
		bundleMetadata := bundleMetadata{
			FormatVersion: bundleFormatVersion, Role: role, NodeIP: nodeIP,
			DatabaseAIP: metadata.DatabaseAIP, DatabaseBIP: metadata.DatabaseBIP,
			WitnessIP: metadata.WitnessIP, VirtualIP: metadata.VirtualIP,
			Version: metadata.Version, Commit: metadata.Commit,
		}
		bundle := preparedHostBundle{Metadata: bundleMetadata, Secrets: make(map[string][]byte)}
		config := NodeConfig{NodeName: role}
		for _, name := range copiedSecretFiles(config) {
			contents, err := os.ReadFile(filepath.Join(secretsRoot, role, name))
			if err != nil {
				return fmt.Errorf("read generated %s secret %s: %w", role, name, err)
			}
			if name == fleetEnvironmentFile {
				contents = append(contents, deploymentEnvironment...)
			}
			bundle.Secrets[name] = contents
		}
		if role == "ha-a" {
			contents, err := os.ReadFile(filepath.Join(secretsRoot, "offline", etcdRootPasswordFile))
			if err != nil {
				return fmt.Errorf("read generated etcd root password: %w", err)
			}
			bundle.EtcdRootPassword = contents
		}
		if err := writeBundle(filepath.Join(exportDir, hostBundleName(role)), bundle); err != nil {
			return err
		}
	}

	return nil
}

func readHostBundle(path string) (preparedHostBundle, error) {
	info, err := secureFileInfo(path, 0o600)
	if err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: %w", err)
	}
	if err := requireCurrentOwner(info, "host bundle"); err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: %w", err)
	}
	if info.Size() > maxBundleSize {
		return preparedHostBundle{}, errors.New("host bundle rejected: document is too large")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return preparedHostBundle{}, fmt.Errorf("read host bundle: %w", err)
	}
	return decodeHostBundle(contents)
}

func decodeHostBundle(contents []byte) (preparedHostBundle, error) {
	var bundle preparedHostBundle
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return preparedHostBundle{}, errors.New("host bundle rejected: invalid JSON after the bundle document")
	}
	if err := validateBundleMetadata(bundle.Metadata); err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: %w", err)
	}
	config := NodeConfig{NodeName: bundle.Metadata.Role}
	expectedSecrets := copiedSecretFiles(config)
	for _, name := range expectedSecrets {
		if len(bundle.Secrets[name]) == 0 {
			return preparedHostBundle{}, fmt.Errorf("host bundle rejected: missing secret %s", name)
		}
	}
	if len(bundle.Secrets) != len(expectedSecrets) {
		return preparedHostBundle{}, errors.New("host bundle rejected: contains a secret not used by this role")
	}
	if bundle.Metadata.Role == "ha-a" && len(bundle.EtcdRootPassword) == 0 {
		return preparedHostBundle{}, errors.New("host bundle rejected: missing etcd root password")
	}
	if bundle.Metadata.Role != "ha-a" && len(bundle.EtcdRootPassword) != 0 {
		return preparedHostBundle{}, errors.New("host bundle rejected: etcd root password is only valid for ha-a")
	}
	if config.isDatabaseNode() {
		if _, err := parseFleetDeploymentEnvironment(bundle.Secrets[fleetEnvironmentFile]); err != nil {
			return preparedHostBundle{}, fmt.Errorf("host bundle rejected: %w", err)
		}
	}
	return bundle, nil
}

func serviceCAFingerprint(contents []byte) (string, error) {
	block, _ := pem.Decode(contents)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("host bundle contains an invalid public service CA")
	}
	digest := sha256.Sum256(block.Bytes)
	encoded := strings.ToUpper(hex.EncodeToString(digest[:]))
	pairs := make([]string, 0, len(encoded)/2)
	for index := 0; index < len(encoded); index += 2 {
		pairs = append(pairs, encoded[index:index+2])
	}
	return strings.Join(pairs, ":"), nil
}

func validateBundleMetadata(metadata bundleMetadata) error {
	if metadata.FormatVersion != bundleFormatVersion {
		return fmt.Errorf("unsupported format version %d", metadata.FormatVersion)
	}
	expectedIP := map[string]string{"ha-a": metadata.DatabaseAIP, "ha-b": metadata.DatabaseBIP, "ha-c": metadata.WitnessIP}
	if expectedIP[metadata.Role] == "" {
		return errors.New("role must be ha-a, ha-b, or ha-c")
	}
	if metadata.NodeIP != expectedIP[metadata.Role] {
		return errors.New("node address does not match the bundle role")
	}
	return nil
}

func validateClusterMetadata(metadata clusterMetadata) error {
	if err := validateNodeConfig(NodeConfig{
		NodeName: "ha-a", NodeIP: metadata.DatabaseAIP, DatabaseAIP: metadata.DatabaseAIP, DatabaseBIP: metadata.DatabaseBIP,
		WitnessIP: metadata.WitnessIP, VirtualIP: metadata.VirtualIP, NetworkInterface: "interface",
		DataDir: dataRoot, SecretsDir: configRoot,
	}); err != nil {
		return fmt.Errorf("invalid HA topology: %w", err)
	}
	return nil
}

func writeBundle(path string, bundle preparedHostBundle) error {
	contents, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode host bundle: %w", err)
	}
	return writeFile(path, contents, 0o600)
}

func readReleaseIdentity(path string) (clusterMetadata, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return clusterMetadata{}, fmt.Errorf("read packaged release identity: %w", err)
	}
	var identity clusterMetadata
	for line := range strings.SplitSeq(string(contents), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "version":
			identity.Version = strings.TrimSpace(value)
		case "commit":
			identity.Commit = strings.TrimSpace(value)
		}
	}
	if !releaseVersionPattern.MatchString(identity.Version) || !releaseCommitPattern.MatchString(identity.Commit) {
		return clusterMetadata{}, errors.New("packaged release identity must contain safe version and commit values")
	}
	return identity, nil
}

func networkInterfaceForIP(rawIP string) (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list network interfaces: %w", err)
	}
	for _, networkInterface := range interfaces {
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.String() == rawIP {
				return networkInterface.Name, nil
			}
		}
	}
	return "", os.ErrNotExist
}

func printInstallSummary(output io.Writer, metadata bundleMetadata, networkInterface string, installed installedDependencies) error {
	return writeInstallerOutput(output, `
Proto Fleet HA installation
  Role:      %s
  This host: %s on %s
  ha-a:      %s
  ha-b:      %s
  ha-c:      %s
  VIP:       %s
  Release:   %s (%s)
  Paths:     %s, %s, %s
  Docker:    %s
  keepalived: %s
  Packages:  ensure arping, nftables, certificates, curl, and iproute2
  Network:   reserve all node addresses and exclude the VIP from DHCP
`, metadata.Role, metadata.NodeIP, networkInterface, metadata.DatabaseAIP, metadata.DatabaseBIP,
		metadata.WitnessIP, metadata.VirtualIP, metadata.Version, metadata.Commit, installRoot, configRoot,
		dataRoot, installAction(installed.docker), installAction(installed.keepalived))
}

func installAction(installed bool) string {
	if installed {
		return "reuse existing installation"
	}
	return "install"
}

func printPeerInstallCommands(output io.Writer, username string, metadata clusterMetadata) error {
	return writeInstallerOutput(output, "\nRun these commands from your operator machine:\n%s\n%s\n",
		peerInstallCommand(username, metadata.DatabaseBIP, metadata.Version),
		peerInstallCommand(username, metadata.WitnessIP, metadata.Version))
}

func peerInstallCommand(username, address, version string) string {
	installerURL := fmt.Sprintf("https://github.com/block/proto-fleet/releases/download/%s/install.sh", version)
	return fmt.Sprintf(`ssh -t %s@%s 'test -f /var/tmp/proto-fleet-ha-host.json || { echo "Prepared HA bundle is missing: /var/tmp/proto-fleet-ha-host.json" >&2; exit 1; }; tmp=$(mktemp /var/tmp/proto-fleet-install.sh.XXXXXX) || exit; trap "rm -f $tmp" EXIT; curl -fsSL %s -o "$tmp" && sudo bash "$tmp" --ha %s'`,
		username, address, installerURL, version)
}

func invokingUsername() string {
	if username := strings.TrimSpace(os.Getenv("SUDO_USER")); username != "" && username != "root" {
		return username
	}
	if current, err := user.Current(); err == nil {
		return current.Username
	}
	return ""
}

const remoteBundleInstallCommand = `set -eu; umask 077; tmp=$(mktemp /var/tmp/proto-fleet-ha-host.json.XXXXXX); trap 'rm -f "$tmp"' EXIT HUP INT TERM; cat >"$tmp"; chmod 0600 "$tmp"; mv -f "$tmp" /var/tmp/proto-fleet-ha-host.json; trap - EXIT`

func runSSH(ctx context.Context, localUsername string, input io.Reader, args ...string) error {
	name := "ssh"
	commandArgs := append([]string{"-o", "ConnectTimeout=10"}, args...)
	if os.Geteuid() == 0 && localUsername != "" && localUsername != "root" {
		name = "sudo"
		commandArgs = []string{"-H", "-u", localUsername}
		if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
			commandArgs = append(commandArgs, "env", "SSH_AUTH_SOCK="+socket)
		}
		commandArgs = append(commandArgs, "ssh", "-o", "ConnectTimeout=10")
		commandArgs = append(commandArgs, args...)
	}
	command := exec.CommandContext(ctx, name, commandArgs...)
	if input == nil {
		command.Stdin = os.Stdin
	} else {
		command.Stdin = input
	}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run SSH command: %w", err)
	}
	return nil
}

func readPrompt(scanner *bufio.Scanner, output io.Writer, prompt string) (string, error) {
	if err := writeInstallerOutput(output, "%s", prompt); err != nil {
		return "", err
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read installer input: %w", err)
		}
		return "", errors.New("installation canceled: input closed")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func readPromptWithDefault(scanner *bufio.Scanner, output io.Writer, prompt, defaultValue string) (string, error) {
	value, err := readPrompt(scanner, output, fmt.Sprintf("%s [%s]: ", prompt, defaultValue))
	if value == "" {
		value = defaultValue
	}
	return value, err
}

func writeInstallerOutput(output io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(output, format, args...); err != nil {
		return fmt.Errorf("write installer output: %w", err)
	}
	return nil
}

func requireAcknowledgement(scanner *bufio.Scanner, output io.Writer, prompt, expected string) error {
	value, err := readPrompt(scanner, output, prompt)
	if err != nil {
		return err
	}
	if !strings.EqualFold(value, expected) {
		return errors.New("installation canceled")
	}
	return nil
}

func removeCopiedExports(exportDir string) error {
	for _, name := range []string{hostBundleName("ha-b"), hostBundleName("ha-c")} {
		path := filepath.Join(exportDir, name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove copied bundle export %s: %w", path, err)
		}
	}
	return nil
}

func hostBundleName(role string) string {
	return "proto-fleet-ha-" + role + ".json"
}

func secretFileMode(name string) os.FileMode {
	if strings.HasSuffix(name, ".crt") || strings.HasSuffix(name, ".pub") {
		return 0o644
	}
	return 0o600
}
