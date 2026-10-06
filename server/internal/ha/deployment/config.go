package deployment

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"syscall"
)

var envLine = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=([A-Za-z0-9._/:@+=?,&-]+)$`)

const endpointModeExternal = "external"

var allowedEnvKeys = map[string]func(*NodeConfig, string){
	"HA_NODE_NAME":         func(c *NodeConfig, v string) { c.NodeName = v },
	"HA_ENDPOINT_MODE":     func(c *NodeConfig, v string) { c.EndpointMode = v },
	"HA_PUBLIC_URL":        func(c *NodeConfig, v string) { c.PublicURL = v },
	"HA_NODE_IP":           func(c *NodeConfig, v string) { c.NodeIP = v },
	"HA_DB_A_IP":           func(c *NodeConfig, v string) { c.DatabaseAIP = v },
	"HA_DB_B_IP":           func(c *NodeConfig, v string) { c.DatabaseBIP = v },
	"HA_DCS_C_IP":          func(c *NodeConfig, v string) { c.WitnessIP = v },
	"HA_VIRTUAL_IP":        func(c *NodeConfig, v string) { c.VirtualIP = v },
	"HA_NETWORK_INTERFACE": func(c *NodeConfig, v string) { c.NetworkInterface = v },
	"HA_DATA_DIR":          func(c *NodeConfig, v string) { c.DataDir = v },
	"HA_SECRETS_DIR":       func(c *NodeConfig, v string) { c.SecretsDir = v },
}

// NodeConfig is the fixed three-host HA identity read from node.env.
type NodeConfig struct {
	EndpointMode     string
	PublicURL        string
	NodeName         string
	NodeIP           string
	DatabaseAIP      string
	DatabaseBIP      string
	WitnessIP        string
	VirtualIP        string
	NetworkInterface string
	DataDir          string
	SecretsDir       string
}

func (c NodeConfig) externalEndpoint() bool { return c.EndpointMode == endpointModeExternal }
func (c NodeConfig) usesVIP() bool          { return !c.externalEndpoint() && c.isDatabaseNode() }
func (c NodeConfig) publicURL() string {
	if c.externalEndpoint() {
		return c.PublicURL
	}
	return "https://" + c.VirtualIP
}
func (c NodeConfig) hostCertificateIdentity(address string) string {
	if c.externalEndpoint() {
		return address
	}
	return c.VirtualIP
}
func (c NodeConfig) publicTLS(serviceTLS *tls.Config) *tls.Config {
	if c.externalEndpoint() {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return serviceTLS
}

func validatePublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return errors.New("HA_PUBLIC_URL must be an HTTPS origin without credentials, path, query, or fragment")
	}
	if u.Port() != "" && u.Port() != "443" {
		return errors.New("HA_PUBLIC_URL must use port 443")
	}
	return nil
}

func (c NodeConfig) isDatabaseNode() bool {
	return c.NodeName == "ha-a" || c.NodeName == "ha-b"
}

func loadNodeConfig(path string) (NodeConfig, error) {
	info, err := secureFileInfo(path, 0o600)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("node environment file rejected: %w", err)
	}
	if err := requireCurrentOwner(info, "node environment file"); err != nil {
		return NodeConfig{}, fmt.Errorf("node environment file rejected: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("open node environment file: %w", err)
	}
	defer file.Close()

	var config NodeConfig
	seen := make(map[string]struct{}, len(allowedEnvKeys))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		match := envLine.FindStringSubmatch(line)
		if match == nil {
			return NodeConfig{}, fmt.Errorf("node environment file rejected: contains a malformed entry")
		}
		key, value := match[1], match[2]
		assign, ok := allowedEnvKeys[key]
		if !ok {
			return NodeConfig{}, fmt.Errorf("node environment file rejected: contains unknown key: %s", key)
		}
		if _, duplicate := seen[key]; duplicate {
			return NodeConfig{}, fmt.Errorf("node environment file rejected: contains duplicate key: %s", key)
		}
		seen[key] = struct{}{}
		assign(&config, value)
	}
	if err := scanner.Err(); err != nil {
		return NodeConfig{}, fmt.Errorf("read node environment file: %w", err)
	}
	return config, nil
}

func secureFileInfo(path string, mode os.FileMode) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("must be a regular, non-symlink file: %s", path)
	}
	if mode != 0 && info.Mode().Perm() != mode {
		return nil, fmt.Errorf("must have mode %04o: %s", mode, path)
	}
	return info, nil
}

func requireCurrentOwner(info os.FileInfo, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine %s ownership", label)
	}
	// UIDs are uint32 on the Unix hosts this deployment command supports.
	if stat.Uid != uint32(os.Geteuid()) { //nolint:gosec // os.Geteuid cannot be negative.
		return fmt.Errorf("%s must be owned by the current deployment user", label)
	}
	return nil
}
