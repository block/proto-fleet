package deployment

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type hostEnvironment struct {
	goos          string
	localIPs      func() ([]netip.Addr, error)
	runCommand    func(context.Context, string, ...string) ([]byte, error)
	applyFirewall func(context.Context, NodeConfig, string) error
}

// Preflight checks a clean host and loads the firewall required before startup.
func Preflight(ctx context.Context, envPath, firewallTemplatePath string) (NodeConfig, error) {
	host := hostEnvironment{
		goos:          runtime.GOOS,
		localIPs:      localAddresses,
		runCommand:    runCommand,
		applyFirewall: applyFirewall,
	}
	return preflight(ctx, envPath, firewallTemplatePath, host)
}

func preflight(ctx context.Context, envPath, firewallTemplatePath string, host hostEnvironment) (NodeConfig, error) {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return NodeConfig{}, err
	}
	if err := validateNodeConfig(config); err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
	}
	if host.goos != "linux" {
		return NodeConfig{}, errors.New("HA preflight failed: the HA profile requires Linux host networking")
	}

	addresses, err := host.localIPs()
	if err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: list local addresses: %w", err)
	}
	nodeIP, _ := netip.ParseAddr(config.NodeIP)
	if !slices.Contains(addresses, nodeIP) {
		return NodeConfig{}, errors.New("HA preflight failed: HA_NODE_IP is not assigned to this host")
	}
	virtualIP, _ := netip.ParseAddr(config.VirtualIP)
	if slices.Contains(addresses, virtualIP) {
		return NodeConfig{}, errors.New("HA preflight failed: HA_VIRTUAL_IP is already assigned")
	}

	for _, peer := range []string{config.DatabaseAIP, config.DatabaseBIP, config.WitnessIP} {
		if peer == config.NodeIP {
			continue
		}
		output, err := host.runCommand(ctx, "ip", "route", "get", peer)
		if err != nil {
			return NodeConfig{}, fmt.Errorf("HA preflight failed: no route to HA peer %s: %s", peer, commandError(output, err))
		}
		source, ok := routeSource(output)
		if !ok || source != config.NodeIP {
			return NodeConfig{}, fmt.Errorf("HA preflight failed: route to HA peer %s must use HA_NODE_IP %s as its source", peer, config.NodeIP)
		}
	}
	output, err := host.runCommand(ctx, "ip", "route", "get", config.VirtualIP)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: no route to HA virtual IP %s: %s", config.VirtualIP, commandError(output, err))
	}
	source, sourceOK := routeSource(output)
	device, deviceOK := routeDevice(output)
	_, routedViaGateway := routeField(output, "via")
	if !sourceOK || source != config.NodeIP || !deviceOK || device != config.NetworkInterface || routedViaGateway {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: route to HA_VIRTUAL_IP must use %s with source %s", config.NetworkInterface, config.NodeIP)
	}
	listeners, err := host.runCommand(ctx, "ss", "-H", "-lnt")
	if err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: inspect listening ports: %s", commandError(listeners, err))
	}
	ports := []int{2379, 2380}
	if config.isDatabaseNode() {
		ports = append(ports, 4000, 5432, 8008)
	}
	for _, port := range ports {
		if portIsListening(string(listeners), port) {
			return NodeConfig{}, fmt.Errorf("HA preflight failed: TCP port %d is already occupied", port)
		}
	}

	if err := requireEmptyDir(filepath.Join(config.DataDir, "etcd"), "pre-existing etcd state"); err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
	}
	if config.isDatabaseNode() {
		if err := requireEmptyDir(filepath.Join(config.DataDir, "postgres"), "pre-existing PostgreSQL state"); err != nil {
			return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
		}
	}
	if err := validateSecrets(config); err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
	}
	if err := host.applyFirewall(ctx, config, firewallTemplatePath); err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
	}
	return config, nil
}

func validateNodeConfig(config NodeConfig) error {
	required := map[string]string{
		"HA_NODE_NAME":         config.NodeName,
		"HA_NODE_IP":           config.NodeIP,
		"HA_DB_A_IP":           config.DatabaseAIP,
		"HA_DB_B_IP":           config.DatabaseBIP,
		"HA_DCS_C_IP":          config.WitnessIP,
		"HA_VIRTUAL_IP":        config.VirtualIP,
		"HA_NETWORK_INTERFACE": config.NetworkInterface,
		"HA_DATA_DIR":          config.DataDir,
		"HA_SECRETS_DIR":       config.SecretsDir,
	}
	for key, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	peers := []string{config.DatabaseAIP, config.DatabaseBIP, config.WitnessIP}
	seen := make(map[netip.Addr]struct{}, len(peers))
	for _, rawIP := range peers {
		ip, ok := parseRoutableIPv4(rawIP)
		if !ok {
			return errors.New("HA peer identity must be a routable literal IPv4 address")
		}
		if _, duplicate := seen[ip]; duplicate {
			return errors.New("HA peer identities must be unique")
		}
		seen[ip] = struct{}{}
	}
	virtualIP, ok := parseRoutableIPv4(config.VirtualIP)
	if !ok {
		return errors.New("HA_VIRTUAL_IP must be a routable literal IPv4 address")
	}
	if _, duplicate := seen[virtualIP]; duplicate {
		return errors.New("HA_VIRTUAL_IP must differ from every HA node address")
	}

	expectedIP := map[string]string{
		"ha-a": config.DatabaseAIP,
		"ha-b": config.DatabaseBIP,
		"ha-c": config.WitnessIP,
	}
	nodeIP, ok := expectedIP[config.NodeName]
	if !ok {
		return errors.New("HA_NODE_NAME must be ha-a, ha-b, or ha-c")
	}
	if config.NodeIP != nodeIP {
		return fmt.Errorf("%s must use its configured HA peer IP", config.NodeName)
	}
	return nil
}

func validateSecrets(config NodeConfig) error {
	dirInfo, err := os.Lstat(config.SecretsDir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("secrets directory must be a non-symlink directory: %s", config.SecretsDir)
	}
	if err := requireCurrentOwner(dirInfo, "secrets directory"); err != nil {
		return err
	}
	if dirInfo.Mode().Perm()&0o022 != 0 {
		return errors.New("secrets directory must not be group/world writable")
	}

	publicFiles := []string{"service-ca.crt", "etcd-server.crt", "etcd-peer.crt", "etcd-jwt.pub"}
	keyFiles := []string{"etcd-server.key", "etcd-peer.key", "etcd-jwt.key"}
	if config.isDatabaseNode() {
		publicFiles = append(publicFiles, "patroni-rest.crt", "postgres.crt")
		keyFiles = append(keyFiles,
			"patroni-rest.key", "postgres.key",
		)
	}
	for _, name := range publicFiles {
		info, err := secureFileInfo(filepath.Join(config.SecretsDir, name), 0)
		if err != nil {
			return fmt.Errorf("required identity file %s: %w", name, err)
		}
		if err := requireCurrentOwner(info, name); err != nil {
			return err
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s must not be group/world writable", name)
		}
	}
	for _, name := range keyFiles {
		info, err := secureFileInfo(filepath.Join(config.SecretsDir, name), 0)
		if err != nil {
			return fmt.Errorf("required identity file %s: %w", name, err)
		}
		if err := requireCurrentOwner(info, name); err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("%s must have mode 0600", name)
		}
	}
	if config.isDatabaseNode() {
		for _, name := range databasePasswordFiles {
			if _, err := readPassword(filepath.Join(config.SecretsDir, name)); err != nil {
				return fmt.Errorf("required password file %s: %w", name, err)
			}
		}
	}

	ca, err := readCertificate(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return fmt.Errorf("read service CA: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if err := verifyEndpointCertificate(filepath.Join(config.SecretsDir, "etcd-server.crt"), config.NodeIP, roots, x509.ExtKeyUsageServerAuth); err != nil {
		return fmt.Errorf("etcd server certificate: %w", err)
	}
	if err := verifyEndpointCertificate(filepath.Join(config.SecretsDir, "etcd-peer.crt"), config.NodeIP, roots, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth); err != nil {
		return fmt.Errorf("etcd peer certificate: %w", err)
	}
	if config.isDatabaseNode() {
		for _, name := range []string{"patroni-rest.crt", "postgres.crt"} {
			if err := verifyEndpointCertificate(filepath.Join(config.SecretsDir, name), config.NodeIP, roots, x509.ExtKeyUsageServerAuth); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	certificateNames := []string{"etcd-server", "etcd-peer"}
	if config.isDatabaseNode() {
		certificateNames = append(certificateNames, "patroni-rest", "postgres")
	}
	for _, name := range certificateNames {
		if err := verifyCertificateKeyPair(
			filepath.Join(config.SecretsDir, name+".crt"),
			filepath.Join(config.SecretsDir, name+".key"),
		); err != nil {
			return fmt.Errorf("%s identity: %w", name, err)
		}
	}
	if err := verifyPublicKeyPair(
		filepath.Join(config.SecretsDir, "etcd-jwt.pub"),
		filepath.Join(config.SecretsDir, "etcd-jwt.key"),
	); err != nil {
		return fmt.Errorf("etcd JWT identity: %w", err)
	}
	return nil
}

func verifyEndpointCertificate(path, ip string, roots *x509.CertPool, usages ...x509.ExtKeyUsage) error {
	certificate, err := readCertificate(path)
	if err != nil {
		return err
	}
	for _, usage := range usages {
		if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: ip, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
			return fmt.Errorf("verify %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

func verifyCertificateKeyPair(certificatePath, privateKeyPath string) error {
	certificate, err := readCertificate(certificatePath)
	if err != nil {
		return err
	}
	publicKey, err := publicKeyFromPrivateKey(privateKeyPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(certificate.RawSubjectPublicKeyInfo, publicKey) {
		return errors.New("certificate does not match private key")
	}
	return nil
}

func verifyPublicKeyPair(publicKeyPath, privateKeyPath string) error {
	contents, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	block, _ := pem.Decode(contents)
	if block == nil || block.Type != "PUBLIC KEY" {
		return errors.New("invalid PEM public key")
	}
	if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}
	privatePublicKey, err := publicKeyFromPrivateKey(privateKeyPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(block.Bytes, privatePublicKey) {
		return errors.New("public key does not match private key")
	}
	return nil
}

func publicKeyFromPrivateKey(path string) ([]byte, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(contents)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("invalid PEM private key")
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	signer, ok := privateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("private key cannot provide a public key")
	}
	publicKey, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return nil, fmt.Errorf("encode public key: %w", err)
	}
	return publicKey, nil
}

func requireEmptyDir(path, label string) error {
	directory, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	defer directory.Close()

	_, err = directory.Readdirnames(1)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("inspect %s: %w", path, err)
	default:
		return fmt.Errorf("%s found under %s", label, path)
	}
}

func localAddresses() ([]netip.Addr, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("list network interface addresses: %w", err)
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil {
			result = append(result, prefix.Addr())
		}
	}
	return result, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run %s: %w", name, err)
	}
	return output, nil
}

func portIsListening(listeners string, port int) bool {
	suffix := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(listeners, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasSuffix(fields[3], suffix) {
			return true
		}
	}
	return false
}

func routeSource(output []byte) (string, bool) {
	return routeField(output, "src")
}

func routeDevice(output []byte) (string, bool) {
	return routeField(output, "dev")
}

func routeField(output []byte, name string) (string, bool) {
	fields := strings.Fields(string(output))
	for i, field := range fields {
		if field == name && i+1 < len(fields) {
			return fields[i+1], true
		}
	}
	return "", false
}

func parseRoutableIPv4(raw string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(raw)
	return ip, err == nil && ip.Is4() && ip.IsGlobalUnicast() && ip.As4()[0] != 0
}

func commandError(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	return message
}
