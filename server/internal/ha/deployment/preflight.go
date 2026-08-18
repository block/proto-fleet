package deployment

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
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
	goos              string
	localIPs          func() ([]netip.Addr, error)
	interfacePrefixes func(string) ([]netip.Prefix, error)
	runCommand        func(context.Context, string, ...string) ([]byte, error)
	applyFirewall     func(context.Context, NodeConfig, string) error
}

// ValidateHost verifies the immutable host inputs before installation changes the machine.
func ValidateHost(ctx context.Context, envPath string) (NodeConfig, error) {
	host := hostEnvironment{
		goos:              runtime.GOOS,
		localIPs:          localAddresses,
		interfacePrefixes: interfaceIPv4Prefixes,
		runCommand:        runCommand,
	}
	return validateHost(ctx, envPath, host, false)
}

// Preflight checks a dedicated host and loads the firewall required before startup.
func Preflight(ctx context.Context, envPath, firewallTemplatePath string) (NodeConfig, error) {
	host := hostEnvironment{
		goos:              runtime.GOOS,
		localIPs:          localAddresses,
		interfacePrefixes: interfaceIPv4Prefixes,
		runCommand:        runCommand,
		applyFirewall:     applyFirewall,
	}
	return preflight(ctx, envPath, firewallTemplatePath, host)
}

func preflight(ctx context.Context, envPath, firewallTemplatePath string, host hostEnvironment) (NodeConfig, error) {
	config, err := validateHost(ctx, envPath, host, true)
	if err != nil {
		return NodeConfig{}, err
	}
	if err := host.applyFirewall(ctx, config, firewallTemplatePath); err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
	}
	return config, nil
}

func validateHost(ctx context.Context, envPath string, host hostEnvironment, probeVirtualIP bool) (NodeConfig, error) {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return NodeConfig{}, err
	}
	if err := validateHostConfiguration(ctx, config, host, probeVirtualIP); err != nil {
		return NodeConfig{}, err
	}
	if err := validateSecrets(config); err != nil {
		return NodeConfig{}, fmt.Errorf("HA preflight failed: %w", err)
	}
	return config, nil
}

func validateHostConfiguration(ctx context.Context, config NodeConfig, host hostEnvironment, probeVirtualIP bool) error {
	if err := validateNodeConfig(config); err != nil {
		return fmt.Errorf("HA preflight failed: %w", err)
	}
	if host.goos != "linux" {
		return errors.New("HA preflight failed: the HA profile requires Linux host networking")
	}

	addresses, err := host.localIPs()
	if err != nil {
		return fmt.Errorf("HA preflight failed: list local addresses: %w", err)
	}
	nodeIP, _ := netip.ParseAddr(config.NodeIP)
	if !slices.Contains(addresses, nodeIP) {
		return errors.New("HA preflight failed: HA_NODE_IP is not assigned to this host")
	}
	var interfacePrefixes []netip.Prefix
	if config.isDatabaseNode() {
		interfacePrefixes, err = host.interfacePrefixes(config.NetworkInterface)
		if err != nil {
			return fmt.Errorf("HA preflight failed: list addresses on %s: %w", config.NetworkInterface, err)
		}
	}
	for _, peer := range []string{config.DatabaseAIP, config.DatabaseBIP, config.WitnessIP} {
		if peer == config.NodeIP {
			continue
		}
		output, err := host.runCommand(ctx, "ip", "route", "get", peer)
		if err != nil {
			return fmt.Errorf("HA preflight failed: no route to HA peer %s: %s", peer, commandError(output, err))
		}
		source, ok := routeSource(output)
		if !ok || source != config.NodeIP {
			return fmt.Errorf("HA preflight failed: route to HA peer %s must use HA_NODE_IP %s as its source", peer, config.NodeIP)
		}
		if config.isDatabaseNode() && (peer == config.DatabaseAIP || peer == config.DatabaseBIP) {
			device, deviceOK := routeDevice(output)
			_, routedViaGateway := routeField(output, "via")
			peerIP, _ := netip.ParseAddr(peer)
			if !deviceOK || device != config.NetworkInterface || routedViaGateway || !addressSharesNodePrefix(nodeIP, peerIP, interfacePrefixes) {
				return fmt.Errorf("HA preflight failed: database peer %s must be directly connected on %s", peer, config.NetworkInterface)
			}
		}
	}
	if config.isDatabaseNode() {
		virtualIP, _ := netip.ParseAddr(config.VirtualIP)
		if slices.Contains(addresses, virtualIP) {
			return errors.New("HA preflight failed: HA_VIRTUAL_IP is already assigned")
		}
		if err := validateVirtualIPPrefix(nodeIP, virtualIP, interfacePrefixes); err != nil {
			return fmt.Errorf("HA preflight failed: %w", err)
		}
		// VRRP moves the VIP with ARP, so it must be directly connected rather
		// than routed through a gateway.
		output, err := host.runCommand(ctx, "ip", "route", "get", config.VirtualIP)
		if err != nil {
			return fmt.Errorf("HA preflight failed: no route to HA virtual IP %s: %s", config.VirtualIP, commandError(output, err))
		}
		source, sourceOK := routeSource(output)
		device, deviceOK := routeDevice(output)
		_, routedViaGateway := routeField(output, "via")
		if !sourceOK || source != config.NodeIP || !deviceOK || device != config.NetworkInterface || routedViaGateway {
			return fmt.Errorf("HA preflight failed: route to HA_VIRTUAL_IP must use %s with source %s", config.NetworkInterface, config.NodeIP)
		}
		if probeVirtualIP {
			// The privileged duplicate-address probe rejects a VIP already claimed by another host.
			output, err = host.runCommand(ctx, "sudo", "arping", "-D", "-I", config.NetworkInterface, "-c", "2", config.VirtualIP)
			if err != nil {
				return fmt.Errorf("HA preflight failed: HA_VIRTUAL_IP is in use or cannot be checked: %s", commandError(output, err))
			}
		}
	}
	listeners, err := host.runCommand(ctx, "ss", "-H", "-lnt")
	if err != nil {
		return fmt.Errorf("HA preflight failed: inspect listening ports: %s", commandError(listeners, err))
	}
	ports := []int{2379, 2380}
	if config.isDatabaseNode() {
		ports = append(ports, 80, 443, 3030, 4000, 5432, 8008)
	}
	for _, port := range ports {
		if portIsListening(string(listeners), port) {
			return fmt.Errorf("HA preflight failed: TCP port %d is already occupied", port)
		}
	}

	if err := requireEmptyDir(filepath.Join(config.DataDir, "etcd"), "pre-existing etcd state"); err != nil {
		return fmt.Errorf("HA preflight failed: %w", err)
	}
	if config.isDatabaseNode() {
		if err := requireEmptyDir(filepath.Join(config.DataDir, "postgres"), "pre-existing PostgreSQL state"); err != nil {
			return fmt.Errorf("HA preflight failed: %w", err)
		}
	}
	return nil
}

func addressSharesNodePrefix(nodeIP, address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Addr() == nodeIP && prefix.Contains(address) {
			return true
		}
	}
	return false
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
		publicFiles = append(publicFiles, "patroni-rest.crt", "postgres.crt", "fleet-client.crt")
		keyFiles = append(keyFiles,
			"patroni-rest.key", "postgres.key", "fleet-client.key",
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
	if _, err := readPassword(filepath.Join(config.SecretsDir, fleetEtcdPasswordFile)); err != nil {
		return fmt.Errorf("required password file %s: %w", fleetEtcdPasswordFile, err)
	}
	if config.isDatabaseNode() {
		if err := validateFleetEnvironment(filepath.Join(config.SecretsDir, fleetEnvironmentFile)); err != nil {
			return fmt.Errorf("required Fleet environment file %s: %w", fleetEnvironmentFile, err)
		}
		for _, name := range databasePasswordFiles {
			if _, err := readPassword(filepath.Join(config.SecretsDir, name)); err != nil {
				return fmt.Errorf("required password file %s: %w", name, err)
			}
		}
	}

	return nil
}

func validateFleetEnvironment(path string) error {
	values, err := loadFleetEnvironment(path)
	if err != nil {
		return err
	}
	if values["DB_DSN"] == "" {
		return errors.New("DB_DSN is required")
	}
	if len(values["AUTH_CLIENT_SECRET_KEY"]) < 32 {
		return errors.New("AUTH_CLIENT_SECRET_KEY must contain at least 32 characters")
	}
	masterKey, err := base64.StdEncoding.DecodeString(values["ENCRYPT_SERVICE_MASTER_KEY"])
	if err != nil || len(masterKey) != 32 {
		return errors.New("ENCRYPT_SERVICE_MASTER_KEY must be a base64-encoded 32-byte key")
	}
	for _, key := range []string{
		"GRAFANA_ADMIN_PASSWORD",
		"GRAFANA_DB_PASSWORD",
		"GRAFANA_SECRET_KEY",
		"FLEET_ALERTS_WEBHOOK_TOKEN",
	} {
		if len(values[key]) < 32 {
			return fmt.Errorf("%s must contain at least 32 characters", key)
		}
	}
	return nil
}

func loadFleetEnvironment(path string) (map[string]string, error) {
	info, err := secureFileInfo(path, 0o600)
	if err != nil {
		return nil, err
	}
	if err := requireCurrentOwner(info, fleetEnvironmentFile); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Fleet environment file: %w", err)
	}
	defer file.Close()

	allowed := map[string]struct{}{
		"AUTH_CLIENT_SECRET_KEY": {}, "ENCRYPT_SERVICE_MASTER_KEY": {}, "DB_DSN": {},
		"GRAFANA_ADMIN_PASSWORD": {}, "GRAFANA_DB_PASSWORD": {}, "GRAFANA_SECRET_KEY": {},
		"FLEET_ALERTS_WEBHOOK_TOKEN": {},
	}
	values := make(map[string]string, len(allowed))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := envLine.FindStringSubmatch(scanner.Text())
		if match == nil {
			return nil, errors.New("contains a malformed entry")
		}
		key := match[1]
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("contains unknown key: %s", key)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("contains duplicate key: %s", key)
		}
		values[key] = match[2]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Fleet environment file: %w", err)
	}
	return values, nil
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

func interfaceIPv4Prefixes(name string) ([]netip.Prefix, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("find interface %s: %w", name, err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("list addresses on interface %s: %w", name, err)
	}
	prefixes := make([]netip.Prefix, 0, len(addresses))
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes, nil
}

func validateVirtualIPPrefix(nodeIP, virtualIP netip.Addr, prefixes []netip.Prefix) error {
	for _, prefix := range prefixes {
		if prefix.Addr() != nodeIP {
			continue
		}
		if !prefix.Contains(virtualIP) {
			return errors.New("HA_VIRTUAL_IP must be on HA_NETWORK_INTERFACE's IPv4 network")
		}
		if prefix.Bits() <= 30 && (virtualIP == prefix.Masked().Addr() || virtualIP == ipv4Broadcast(prefix)) {
			return errors.New("HA_VIRTUAL_IP must not be the network or broadcast address")
		}
		return nil
	}
	return errors.New("HA_NODE_IP is not assigned to HA_NETWORK_INTERFACE")
}

func ipv4Broadcast(prefix netip.Prefix) netip.Addr {
	networkBytes := prefix.Masked().Addr().As4()
	network := binary.BigEndian.Uint32(networkBytes[:])
	hostMask := ^uint32(0) >> prefix.Bits()
	var broadcast [4]byte
	binary.BigEndian.PutUint32(broadcast[:], network|hostMask)
	return netip.AddrFrom4(broadcast)
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
