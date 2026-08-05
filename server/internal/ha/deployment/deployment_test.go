package deployment

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"
)

var testHostIPs = [3]string{"10.40.0.11", "10.40.0.12", "10.40.0.13"}

func TestRenderFirewall(t *testing.T) {
	config := NodeConfig{
		DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1],
		WitnessIP:   testHostIPs[2],
	}
	template := "nodes = { ${HA_DB_A_IP}, ${HA_DB_B_IP}, ${HA_DCS_C_IP} }\n"
	rules, err := renderFirewall(template, config)
	if err != nil {
		t.Fatal(err)
	}
	want := "nodes = { 10.40.0.11, 10.40.0.12, 10.40.0.13 }\n"
	if rules != want {
		t.Fatalf("renderFirewall() = %q, want %q", rules, want)
	}

	if _, err := renderFirewall(template+"node = ${HA_NODE_IP}\n", config); err == nil {
		t.Fatal("renderFirewall accepted an unresolved placeholder")
	}
}

func TestRenderKeepalivedConfig(t *testing.T) {
	config := NodeConfig{
		NodeName:         "ha-a",
		NodeIP:           testHostIPs[0],
		DatabaseAIP:      testHostIPs[0],
		DatabaseBIP:      testHostIPs[1],
		VirtualIP:        "10.40.0.100",
		NetworkInterface: "eth0",
		DataDir:          "/var/lib/proto-fleet/ha",
	}
	template := "source=${HA_NODE_IP}\npeer=${HA_PEER_IP}\nvip=${HA_VIRTUAL_IP}\ninterface=${HA_NETWORK_INTERFACE}\nheartbeat=${HA_ENDPOINT_HEARTBEAT_FILE}\n"

	rendered, err := renderKeepalivedConfig(template, config)
	if err != nil {
		t.Fatal(err)
	}
	want := "source=10.40.0.11\npeer=10.40.0.12\nvip=10.40.0.100\ninterface=eth0\nheartbeat=/run/proto-fleet-ha-endpoint-heartbeat\n"
	if rendered != want {
		t.Fatalf("renderKeepalivedConfig() = %q, want %q", rendered, want)
	}

	config.NodeName = "ha-c"
	_, err = renderKeepalivedConfig(template, config)
	if err == nil || !strings.Contains(err.Error(), "database hosts") {
		t.Fatalf("renderKeepalivedConfig(witness) error = %v", err)
	}
}

func TestGenerateSecrets(t *testing.T) {
	output := filepath.Join(t.TempDir(), "generated")
	if err := GenerateSecrets(output, testHostIPs); err != nil {
		t.Fatal(err)
	}

	ca, err := readCertificate(filepath.Join(output, "offline", "service-ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for i, node := range []string{"ha-a", "ha-b", "ha-c"} {
		dir := filepath.Join(output, node)
		for _, name := range []string{"service-ca.crt", "etcd-server.crt", "etcd-server.key", "etcd-peer.crt", "etcd-peer.key", "etcd-jwt.pub", "etcd-jwt.key"} {
			requireFile(t, filepath.Join(dir, name))
		}
		if err := verifyEndpointCertificate(filepath.Join(dir, "etcd-server.crt"), testHostIPs[i], roots, x509.ExtKeyUsageServerAuth); err != nil {
			t.Errorf("verify %s etcd server certificate: %v", node, err)
		}
		if err := verifyEndpointCertificate(filepath.Join(dir, "etcd-peer.crt"), testHostIPs[i], roots, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth); err != nil {
			t.Errorf("verify %s etcd peer certificate: %v", node, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "service-ca.key")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s received the offline CA key", node)
		}
	}
	for i, node := range []string{"ha-a", "ha-b"} {
		dir := filepath.Join(output, node)
		for _, name := range []string{"patroni-rest.crt", "patroni-rest.key", "postgres.crt", "postgres.key", "fleet-db-password", "fleet-etcd-password", "patroni-etcd-password"} {
			requireFile(t, filepath.Join(dir, name))
		}
		if err := verifyEndpointCertificate(filepath.Join(dir, "postgres.crt"), testHostIPs[i], roots, x509.ExtKeyUsageServerAuth); err != nil {
			t.Errorf("verify %s PostgreSQL certificate: %v", node, err)
		}
	}
	requireMode(t, filepath.Join(output, "offline", "service-ca.key"), 0o600)
	requireMode(t, filepath.Join(output, "ha-a", "etcd-server.key"), 0o600)

	if err := GenerateSecrets(output, testHostIPs); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("GenerateSecrets(existing directory) error = %v", err)
	}
	badOutput := filepath.Join(t.TempDir(), "bad")
	if err := GenerateSecrets(badOutput, [3]string{testHostIPs[0], testHostIPs[0], testHostIPs[2]}); err == nil {
		t.Fatal("GenerateSecrets accepted duplicate host IPs")
	}
	if _, err := os.Stat(badOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid input left a partial output directory")
	}
}

func TestLoadNodeConfigRejectsUnsafeInput(t *testing.T) {
	valid := testNodeEnv(t, t.TempDir(), "/tmp/data", "/tmp/secrets")
	if _, err := loadNodeConfig(valid); err != nil {
		t.Fatalf("loadNodeConfig(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		line   string
		needle string
	}{
		{"shell syntax", "HA_NODE_IP=$(touch /tmp/executed)\n", "malformed"},
		{"unknown key", "HA_UNKNOWN=value\n", "unknown key"},
		{"duplicate key", "HA_NODE_NAME=ha-b\n", "duplicate key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := testNodeEnv(t, t.TempDir(), "/tmp/data", "/tmp/secrets")
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString(test.line); err != nil {
				t.Fatal(err)
			}
			file.Close()
			if _, err := loadNodeConfig(path); err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("loadNodeConfig() error = %v, want %q", err, test.needle)
			}
		})
	}

	if err := os.Chmod(valid, 0o644); err != nil { //nolint:gosec // Deliberately tests rejection of an unsafe mode.
		t.Fatal(err)
	}
	if _, err := loadNodeConfig(valid); err == nil || !strings.Contains(err.Error(), "mode 0600") {
		t.Fatalf("loadNodeConfig(permissive mode) error = %v", err)
	}
}

func TestPreflight(t *testing.T) {
	root := t.TempDir()
	generated := filepath.Join(root, "generated")
	if err := GenerateSecrets(generated, testHostIPs); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "postgres"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "etcd"), 0o700); err != nil {
		t.Fatal(err)
	}
	envPath := testNodeEnv(t, root, dataDir, filepath.Join(generated, "ha-a"))
	const firewallTemplatePath = "firewall.nft.tmpl"
	firewallApplied := false
	routeSource := testHostIPs[0]
	routeViaGateway := false
	host := hostEnvironment{
		goos:     "linux",
		localIPs: func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(testHostIPs[0])}, nil },
		runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ip" {
				if routeViaGateway && args[2] == "10.40.0.100" {
					return []byte(fmt.Sprintf("%s via 10.40.0.1 dev eth0 src %s\n", args[2], routeSource)), nil
				}
				return []byte(fmt.Sprintf("%s dev eth0 src %s\n", args[2], routeSource)), nil
			}
			return nil, nil
		},
		applyFirewall: func(_ context.Context, gotConfig NodeConfig, gotTemplatePath string) error {
			if gotConfig.NodeName != "ha-a" || gotTemplatePath != firewallTemplatePath {
				t.Fatalf("applyFirewall config/template = %q, %q", gotConfig.NodeName, gotTemplatePath)
			}
			firewallApplied = true
			return nil
		},
	}

	config, err := preflight(context.Background(), envPath, firewallTemplatePath, host)
	if err != nil {
		t.Fatalf("preflight(clean host) error = %v", err)
	}
	if config.NodeName != "ha-a" {
		t.Fatalf("preflight node = %q", config.NodeName)
	}
	if !firewallApplied {
		t.Fatal("preflight did not apply the firewall")
	}

	host.localIPs = func() ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr(testHostIPs[0]), netip.MustParseAddr("10.40.0.100")}, nil
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "HA_VIRTUAL_IP is already assigned") {
		t.Fatalf("preflight(assigned VIP) error = %v", err)
	}
	host.localIPs = func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(testHostIPs[0])}, nil }

	routeSource = "10.40.0.99"
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "must use HA_NODE_IP") {
		t.Fatalf("preflight(mismatched route source) error = %v", err)
	}
	routeSource = testHostIPs[0]
	routeViaGateway = true
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "route to HA_VIRTUAL_IP") {
		t.Fatalf("preflight(routed VIP) error = %v", err)
	}
	routeViaGateway = false

	host.applyFirewall = func(context.Context, NodeConfig, string) error {
		return errors.New("injected apply failure")
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "injected apply failure") {
		t.Fatalf("preflight(firewall apply failure) error = %v", err)
	}

	host.applyFirewall = func(context.Context, NodeConfig, string) error { return nil }
	publicIdentity := filepath.Join(generated, "ha-a", "service-ca.crt")
	if err := os.Chmod(publicIdentity, 0o666); err != nil { //nolint:gosec // Deliberately tests rejection of an unsafe mode.
		t.Fatal(err)
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "must not be group/world writable") {
		t.Fatalf("preflight(writable public identity) error = %v", err)
	}
	if err := os.Chmod(publicIdentity, 0o644); err != nil { //nolint:gosec // Restores the generated public certificate mode.
		t.Fatal(err)
	}
	serverKeyPath := filepath.Join(generated, "ha-a", "etcd-server.key")
	serverKey, err := os.ReadFile(serverKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	peerKey, err := os.ReadFile(filepath.Join(generated, "ha-a", "etcd-peer.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverKeyPath, peerKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "certificate does not match private key") {
		t.Fatalf("preflight(mismatched certificate key) error = %v", err)
	}
	if err := os.WriteFile(serverKeyPath, serverKey, 0o600); err != nil {
		t.Fatal(err)
	}

	jwtKeyPath := filepath.Join(generated, "ha-a", "etcd-jwt.key")
	jwtKey, err := os.ReadFile(jwtKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jwtKeyPath, serverKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "public key does not match private key") {
		t.Fatalf("preflight(mismatched JWT key) error = %v", err)
	}
	if err := os.WriteFile(jwtKeyPath, jwtKey, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dataDir, "postgres", "PG_VERSION"), []byte("18"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "pre-existing PostgreSQL state") {
		t.Fatalf("preflight(existing database) error = %v", err)
	}
}

func TestBootstrapEtcdAuthEnablesAuthLast(t *testing.T) {
	client := &recordingAuthClient{}
	if err := bootstrapEtcdAuth(context.Background(), client, "root-pass", "patroni-pass", "fleet-pass"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"health",
		"role:patroni",
		"permission:patroni:/service/proto-fleet/:readwrite",
		"user:patroni:patroni-pass",
		"grant:patroni:patroni",
		"role:fleet-observer",
		"permission:fleet-observer:/service/proto-fleet/:read",
		"user:fleet-observer:fleet-pass",
		"grant:fleet-observer:fleet-observer",
		"role:root",
		"user:root:root-pass",
		"grant:root:root",
		"auth",
	}
	if !slices.Equal(client.calls, want) {
		t.Fatalf("bootstrap calls:\n%v\nwant:\n%v", client.calls, want)
	}

	failing := &recordingAuthClient{failAt: "user:fleet-observer:fleet-pass"}
	if err := bootstrapEtcdAuth(context.Background(), failing, "root-pass", "patroni-pass", "fleet-pass"); err == nil {
		t.Fatal("bootstrap succeeded after a role setup failure")
	}
	if slices.Contains(failing.calls, "auth") {
		t.Fatal("bootstrap enabled authentication after a partial setup failure")
	}
}

type recordingAuthClient struct {
	calls  []string
	failAt string
}

func (c *recordingAuthClient) record(call string) error {
	c.calls = append(c.calls, call)
	if call == c.failAt {
		return errors.New("injected failure")
	}
	return nil
}

func (c *recordingAuthClient) Healthy(context.Context) error { return c.record("health") }
func (c *recordingAuthClient) AddRole(_ context.Context, role string) error {
	return c.record("role:" + role)
}
func (c *recordingAuthClient) GrantPermission(_ context.Context, role, prefix string, permission clientv3.PermissionType) error {
	name := "read"
	if permission == clientv3.PermissionType(clientv3.PermReadWrite) {
		name = "readwrite"
	}
	return c.record(fmt.Sprintf("permission:%s:%s:%s", role, prefix, name))
}
func (c *recordingAuthClient) AddUser(_ context.Context, user, password string) error {
	return c.record("user:" + user + ":" + password)
}
func (c *recordingAuthClient) GrantRole(_ context.Context, user, role string) error {
	return c.record("grant:" + user + ":" + role)
}
func (c *recordingAuthClient) EnableAuth(context.Context) error { return c.record("auth") }

func testNodeEnv(t *testing.T, dir, dataDir, secretsDir string) string {
	t.Helper()
	path := filepath.Join(dir, "node.env")
	contents := fmt.Sprintf(`HA_NODE_NAME=ha-a
HA_NODE_IP=%s
HA_DB_A_IP=%s
HA_DB_B_IP=%s
HA_DCS_C_IP=%s

HA_VIRTUAL_IP=10.40.0.100
HA_NETWORK_INTERFACE=eth0

HA_DATA_DIR=%s
HA_SECRETS_DIR=%s
`, testHostIPs[0], testHostIPs[0], testHostIPs[1], testHostIPs[2], dataDir, secretsDir)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("required file %s: %v", path, err)
	}
}

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
