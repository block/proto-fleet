package deployment

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"
)

const (
	bundleFormatVersion = 1
	bundleMetadataFile  = "bundle.json"
	publicCAName        = "proto-fleet-ha-service-ca.crt"
	maxBundleSize       = 2 << 20
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

func (m bundleMetadata) marshal() ([]byte, error) {
	contents, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode host bundle metadata: %w", err)
	}
	return append(contents, '\n'), nil
}

type preparedHostBundle struct {
	metadata bundleMetadata
	files    map[string][]byte
}

type hostIdentity struct {
	address          string
	networkInterface string
}

type guidedInstallDependencies struct {
	input           io.Reader
	output          io.Writer
	prompts         io.Writer
	terminal        func() bool
	sourceRoot      func() (string, error)
	primaryIdentity func(context.Context, string) (hostIdentity, error)
	interfaceForIP  func(string) (string, error)
	makeExportDir   func(string) (string, error)
	inspect         func(context.Context, string, NodeConfig) (installedDependencies, error)
	install         func(context.Context, InstallOptions) error
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
		sourceRoot: releaseRoot,
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
		interfaceForIP: networkInterfaceForIP,
		makeExportDir:  makeBundleExportDir,
		inspect: func(ctx context.Context, source string, config NodeConfig) (installedDependencies, error) {
			host := hostEnvironment{
				goos: runtime.GOOS, localIPs: localAddresses, interfacePrefixes: interfaceIPv4Prefixes,
				runCommand: runCommand,
			}
			if err := validateHostConfiguration(ctx, config, host, false); err != nil {
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
	if !deps.terminal() {
		return errors.New("fleet-ha install requires an interactive terminal; use ssh -t HOST 'fleet-ha install [HOST_BUNDLE]'")
	}
	source, err := deps.sourceRoot()
	if err != nil {
		return err
	}
	release, err := readReleaseIdentity(filepath.Join(source, "version.txt"))
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(deps.input)
	if bundlePath != "" {
		return installPreparedHost(ctx, source, bundlePath, release, scanner, true, deps)
	}
	return prepareAndInstallCluster(ctx, source, release, scanner, deps)
}

func prepareAndInstallCluster(ctx context.Context, source string, release clusterMetadata, scanner *bufio.Scanner, deps guidedInstallDependencies) error {
	metadata := release
	var err error
	if metadata.DatabaseBIP, err = readPrompt(scanner, deps.prompts, "ha-b IPv4 address: "); err != nil {
		return err
	}
	if metadata.WitnessIP, err = readPrompt(scanner, deps.prompts, "ha-c IPv4 address: "); err != nil {
		return err
	}
	if metadata.VirtualIP, err = readPrompt(scanner, deps.prompts, "Virtual IPv4 address: "); err != nil {
		return err
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
	installed, err := deps.inspect(ctx, source, config)
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
	exportDir, err := deps.makeExportDir(source)
	if err != nil {
		return err
	}
	if err := prepareInstallBundles(exportDir, metadata); err != nil {
		return fmt.Errorf("bundle generation failed; partial exports remain at %s: %w", exportDir, err)
	}
	if err := printBundleCopyCommand(deps.output, exportDir, metadata.DatabaseAIP); err != nil {
		return err
	}
	if err := requireAcknowledgement(scanner, deps.prompts, "Type COPIED after the command succeeds: ", "COPIED"); err != nil {
		return fmt.Errorf("bundle exports remain at %s: %w", exportDir, err)
	}
	if err := removeCopiedExports(exportDir); err != nil {
		return err
	}

	haABundle := filepath.Join(exportDir, hostBundleName("ha-a"))
	if err := installPreparedHost(ctx, source, haABundle, release, scanner, false, deps); err != nil {
		return err
	}
	_ = os.Remove(exportDir)
	return nil
}

func makeBundleExportDir(source string) (string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(source), "proto-fleet-ha-bundles-")
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

func installPreparedHost(ctx context.Context, source, bundlePath string, release clusterMetadata, scanner *bufio.Scanner, confirm bool, deps guidedInstallDependencies) error {
	bundle, err := readHostBundle(bundlePath)
	if err != nil {
		return err
	}
	if bundle.metadata.Version != release.Version || bundle.metadata.Commit != release.Commit {
		return fmt.Errorf("host bundle release does not match this release: bundle=%s@%s local=%s@%s",
			bundle.metadata.Version, bundle.metadata.Commit, release.Version, release.Commit)
	}
	networkInterface, err := deps.interfaceForIP(bundle.metadata.NodeIP)
	if err != nil {
		return fmt.Errorf("bundle node address %s is not assigned to this host", bundle.metadata.NodeIP)
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
	if confirm {
		if err := writeInstallerOutput(deps.output, "[validation] Checking the bundle, release, network, and dedicated host...\n"); err != nil {
			return err
		}
		installed, err := deps.inspect(ctx, source, config)
		if err != nil {
			return err
		}
		if err := printInstallSummary(deps.prompts, bundle.metadata, networkInterface, installed); err != nil {
			return err
		}
		if err := requireAcknowledgement(scanner, deps.prompts, "Type INSTALL to continue: ", "INSTALL"); err != nil {
			return err
		}
	}
	if err := writeInstallerOutput(deps.output, "Installing %s from the verified host bundle...\n", bundle.metadata.Role); err != nil {
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
	for path, contents := range bundle.files {
		if !strings.HasPrefix(path, "secrets/") {
			continue
		}
		name := strings.TrimPrefix(path, "secrets/")
		if err := writeFile(filepath.Join(secretsDir, name), contents, secretFileMode(name)); err != nil {
			return InstallOptions{}, err
		}
	}
	config := NodeConfig{
		NodeName: bundle.metadata.Role, NodeIP: bundle.metadata.NodeIP,
		DatabaseAIP: bundle.metadata.DatabaseAIP, DatabaseBIP: bundle.metadata.DatabaseBIP,
		WitnessIP: bundle.metadata.WitnessIP, VirtualIP: bundle.metadata.VirtualIP,
		NetworkInterface: networkInterface, DataDir: dataRoot, SecretsDir: secretsDir,
	}
	nodeEnvPath := filepath.Join(stagingDir, "node.env")
	if err := writeFile(nodeEnvPath, []byte(renderNodeEnvironment(config)), 0o600); err != nil {
		return InstallOptions{}, err
	}
	options := InstallOptions{NodeEnvPath: nodeEnvPath}
	if rootPassword, ok := bundle.files[etcdRootPasswordFile]; ok {
		options.EtcdRootPasswordFile = filepath.Join(stagingDir, etcdRootPasswordFile)
		if err := writeFile(options.EtcdRootPasswordFile, rootPassword, 0o600); err != nil {
			return InstallOptions{}, err
		}
	}
	return options, nil
}

func prepareInstallBundles(exportDir string, metadata clusterMetadata) (err error) {
	generated, err := os.MkdirTemp("", "proto-fleet-ha-secrets-")
	if err != nil {
		return fmt.Errorf("create secret generation workspace: %w", err)
	}
	defer os.RemoveAll(generated)
	secretsRoot := filepath.Join(generated, "generated")
	if err := GenerateSecrets(secretsRoot, [3]string{metadata.DatabaseAIP, metadata.DatabaseBIP, metadata.WitnessIP}, metadata.VirtualIP); err != nil {
		return err
	}

	for _, role := range []string{"ha-a", "ha-b", "ha-c"} {
		nodeIP := map[string]string{"ha-a": metadata.DatabaseAIP, "ha-b": metadata.DatabaseBIP, "ha-c": metadata.WitnessIP}[role]
		bundleMetadata := bundleMetadata{
			FormatVersion: bundleFormatVersion, Role: role, NodeIP: nodeIP,
			DatabaseAIP: metadata.DatabaseAIP, DatabaseBIP: metadata.DatabaseBIP,
			WitnessIP: metadata.WitnessIP, VirtualIP: metadata.VirtualIP,
			Version: metadata.Version, Commit: metadata.Commit,
		}
		metadataJSON, err := bundleMetadata.marshal()
		if err != nil {
			return err
		}
		files := map[string][]byte{bundleMetadataFile: metadataJSON}
		config := NodeConfig{NodeName: role}
		for _, name := range copiedSecretFiles(config) {
			contents, err := os.ReadFile(filepath.Join(secretsRoot, role, name))
			if err != nil {
				return fmt.Errorf("read generated %s secret %s: %w", role, name, err)
			}
			files["secrets/"+name] = contents
		}
		if role == "ha-a" {
			contents, err := os.ReadFile(filepath.Join(secretsRoot, "offline", etcdRootPasswordFile))
			if err != nil {
				return fmt.Errorf("read generated etcd root password: %w", err)
			}
			files[etcdRootPasswordFile] = contents
		}
		if err := writeBundle(filepath.Join(exportDir, hostBundleName(role)), files); err != nil {
			return err
		}
	}

	publicCA, err := os.ReadFile(filepath.Join(secretsRoot, "offline", "service-ca.crt"))
	if err != nil {
		return fmt.Errorf("read generated service CA: %w", err)
	}
	publicCAPath := filepath.Join(exportDir, publicCAName)
	return writeFile(publicCAPath, publicCA, 0o644)
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
	files := make(map[string][]byte)
	if err := json.Unmarshal(contents, &files); err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: invalid JSON: %w", err)
	}
	metadataJSON, ok := files[bundleMetadataFile]
	if !ok {
		return preparedHostBundle{}, errors.New("host bundle rejected: missing bundle metadata")
	}
	var metadata bundleMetadata
	decoder := json.NewDecoder(bytes.NewReader(metadataJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: invalid metadata: %w", err)
	}
	if err := validateBundleMetadata(metadata); err != nil {
		return preparedHostBundle{}, fmt.Errorf("host bundle rejected: %w", err)
	}
	expected := expectedHostBundleEntries(metadata.Role)
	for name := range files {
		if _, ok := expected[name]; !ok {
			return preparedHostBundle{}, fmt.Errorf("host bundle rejected: unexpected entry %s", name)
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		return preparedHostBundle{}, errors.New("host bundle rejected: bundle is incomplete")
	}
	delete(files, bundleMetadataFile)
	return preparedHostBundle{metadata: metadata, files: files}, nil
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

func expectedHostBundleEntries(role string) map[string]struct{} {
	expected := map[string]struct{}{bundleMetadataFile: {}}
	for _, name := range copiedSecretFiles(NodeConfig{NodeName: role}) {
		expected["secrets/"+name] = struct{}{}
	}
	if role == "ha-a" {
		expected[etcdRootPasswordFile] = struct{}{}
	}
	return expected
}

func writeBundle(path string, files map[string][]byte) error {
	contents, err := json.Marshal(files)
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
	if identity.Version == "" || identity.Commit == "" || strings.ContainsAny(identity.Version+identity.Commit, " \t\r\n") {
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

func printBundleCopyCommand(output io.Writer, exportDir, nodeIP string) error {
	username := "USER"
	if current, err := user.Current(); err == nil && current.Username != "" {
		username = current.Username
	}
	ca, err := readCertificate(filepath.Join(exportDir, publicCAName))
	if err != nil {
		return fmt.Errorf("read exported service CA: %w", err)
	}
	digest := sha256.Sum256(ca.Raw)
	fingerprint := make([]string, len(digest))
	for index, value := range digest {
		fingerprint[index] = fmt.Sprintf("%02X", value)
	}
	return writeInstallerOutput(output, "\nService CA SHA-256 fingerprint: %s\n"+
		"Copy the host bundles and public service CA from your operator machine before continuing:\n"+
		"mkdir -p proto-fleet-ha-bundles && scp -p '%s@%s:%s/*' proto-fleet-ha-bundles/\n",
		strings.Join(fingerprint, ":"), username, nodeIP, exportDir)
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
	if value != expected {
		return errors.New("installation canceled")
	}
	return nil
}

func removeCopiedExports(exportDir string) error {
	for _, name := range []string{hostBundleName("ha-b"), hostBundleName("ha-c"), publicCAName} {
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
