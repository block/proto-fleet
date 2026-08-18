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

const testVirtualIP = "10.40.0.100"

func verifyEndpointCertificate(path, ip string, roots *x509.CertPool, usages ...x509.ExtKeyUsage) error {
	certificate, err := readCertificate(path)
	if err != nil {
		return err
	}
	for _, usage := range usages {
		if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: ip, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
			return fmt.Errorf("verify %s certificate: %w", ip, err)
		}
	}
	return nil
}

func TestRenderFirewall(t *testing.T) {
	config := NodeConfig{
		NodeIP:           testHostIPs[0],
		DatabaseAIP:      testHostIPs[0],
		DatabaseBIP:      testHostIPs[1],
		WitnessIP:        testHostIPs[2],
		NetworkInterface: "eth0",
	}
	template := "nodes = { ${HA_DB_A_IP}, ${HA_DB_B_IP}, ${HA_DCS_C_IP} }\ninput = ${HA_NODE_IP} ${HA_NETWORK_INTERFACE}\n"
	rules, err := renderFirewall(template, config)
	if err != nil {
		t.Fatal(err)
	}
	want := "nodes = { 10.40.0.11, 10.40.0.12, 10.40.0.13 }\ninput = 10.40.0.11 eth0\n"
	if rules != want {
		t.Fatalf("renderFirewall() = %q, want %q", rules, want)
	}

	if _, err := renderFirewall(template+"node = ${UNRESOLVED}\n", config); err == nil {
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
		SecretsDir:       "/etc/proto-fleet/ha",
	}
	template := "source=${HA_NODE_IP}\npeer=${HA_PEER_IP}\nvip=${HA_VIRTUAL_IP}\ninterface=${HA_NETWORK_INTERFACE}\nheartbeat=${HA_ENDPOINT_HEARTBEAT_FILE}\nca=${HA_SECRETS_DIR}/service-ca.crt\n"

	rendered, err := renderKeepalivedConfig(template, config)
	if err != nil {
		t.Fatal(err)
	}
	want := "source=10.40.0.11\npeer=10.40.0.12\nvip=10.40.0.100\ninterface=eth0\nheartbeat=/run/proto-fleet-ha/endpoint-heartbeat\nca=/etc/proto-fleet/ha/service-ca.crt\n"
	if rendered != want {
		t.Fatalf("renderKeepalivedConfig() = %q, want %q", rendered, want)
	}

	config.NodeName = "ha-c"
	_, err = renderKeepalivedConfig(template, config)
	if err == nil || !strings.Contains(err.Error(), "Fleet hosts") {
		t.Fatalf("renderKeepalivedConfig(witness) error = %v", err)
	}
}

func TestGenerateSecrets(t *testing.T) {
	output := filepath.Join(t.TempDir(), "generated")
	if err := GenerateSecrets(output, testHostIPs, testVirtualIP); err != nil {
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
		for _, name := range []string{"service-ca.crt", "etcd-server.crt", "etcd-server.key", "etcd-peer.crt", "etcd-peer.key", "etcd-jwt.pub", "etcd-jwt.key", fleetEtcdPasswordFile} {
			requireFile(t, filepath.Join(dir, name))
		}
		if err := verifyEndpointCertificate(filepath.Join(dir, "etcd-server.crt"), testHostIPs[i], roots, x509.ExtKeyUsageServerAuth); err != nil {
			t.Errorf("verify %s etcd server certificate: %v", node, err)
		}
		serverCertificate, err := readCertificate(filepath.Join(dir, "etcd-server.crt"))
		if err != nil || !serverCertificate.NotAfter.Equal(ca.NotAfter) {
			t.Errorf("%s service certificate expiry does not match the CA: %v", node, err)
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
		for _, name := range []string{"patroni-rest.crt", "patroni-rest.key", "postgres.crt", "postgres.key", "fleet-client.crt", "fleet-client.key", fleetEnvironmentFile, "fleet-db-password", "patroni-etcd-password"} {
			requireFile(t, filepath.Join(dir, name))
		}
		if err := verifyEndpointCertificate(filepath.Join(dir, "postgres.crt"), testHostIPs[i], roots, x509.ExtKeyUsageServerAuth); err != nil {
			t.Errorf("verify %s PostgreSQL certificate: %v", node, err)
		}
		if err := verifyEndpointCertificate(filepath.Join(dir, "fleet-client.crt"), testVirtualIP, roots, x509.ExtKeyUsageServerAuth); err != nil {
			t.Errorf("verify %s Fleet client certificate: %v", node, err)
		}
		if err := validateFleetEnvironment(filepath.Join(dir, fleetEnvironmentFile)); err != nil {
			t.Errorf("validate %s Fleet environment: %v", node, err)
		}
	}
	offlineFleetEnvironment, err := os.ReadFile(filepath.Join(output, "offline", fleetEnvironmentFile))
	if err != nil {
		t.Fatal(err)
	}
	fleetDatabasePassword, err := readPassword(filepath.Join(output, "offline", "fleet-db-password"))
	if err != nil {
		t.Fatal(err)
	}
	wantDatabaseDSN := fmt.Sprintf(
		"DB_DSN=postgresql://fleet:%s@%s:5432,%s:5432/fleet?target_session_attrs=read-write&sslmode=verify-full&sslrootcert=/etc/proto-fleet/ha/service-ca.crt\n",
		fleetDatabasePassword,
		testHostIPs[0],
		testHostIPs[1],
	)
	if !strings.Contains(string(offlineFleetEnvironment), wantDatabaseDSN) {
		t.Fatal("generated Fleet environment does not contain the HA database DSN")
	}
	for _, node := range []string{"ha-a", "ha-b"} {
		nodeFleetEnvironment, err := os.ReadFile(filepath.Join(output, node, fleetEnvironmentFile))
		if err != nil {
			t.Fatal(err)
		}
		if string(nodeFleetEnvironment) != string(offlineFleetEnvironment) {
			t.Fatalf("%s Fleet environment differs from the offline copy", node)
		}
	}
	requireMode(t, filepath.Join(output, "ha-a", "etcd-server.key"), 0o600)
	requireMode(t, filepath.Join(output, "offline", fleetEnvironmentFile), 0o600)
	requireMode(t, filepath.Join(output, "ha-a", fleetEnvironmentFile), 0o600)

	if err := GenerateSecrets(output, testHostIPs, testVirtualIP); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("GenerateSecrets(existing directory) error = %v", err)
	}
	badOutput := filepath.Join(t.TempDir(), "bad")
	if err := GenerateSecrets(badOutput, [3]string{testHostIPs[0], testHostIPs[0], testHostIPs[2]}, testVirtualIP); err == nil {
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
	if err := GenerateSecrets(generated, testHostIPs, testVirtualIP); err != nil {
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
	databasePeerViaGateway := false
	arpingConflict := false
	listeners := ""
	var arpingArgs []string
	prefixes := []netip.Prefix{netip.MustParsePrefix("10.40.0.11/24")}
	host := hostEnvironment{
		goos:              "linux",
		localIPs:          func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(testHostIPs[0])}, nil },
		interfacePrefixes: func(string) ([]netip.Prefix, error) { return prefixes, nil },
		runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "sudo" && len(args) > 0 && args[0] == "arping" {
				arpingArgs = slices.Clone(args)
				if arpingConflict {
					return []byte("reply from 10.40.0.100"), errors.New("exit status 1")
				}
				return nil, nil
			}
			if name == "ip" {
				if databasePeerViaGateway && args[2] == testHostIPs[1] {
					return []byte(fmt.Sprintf("%s via 10.40.0.1 dev eth0 src %s\n", args[2], routeSource)), nil
				}
				if routeViaGateway && args[2] == "10.40.0.100" {
					return []byte(fmt.Sprintf("%s via 10.40.0.1 dev eth0 src %s\n", args[2], routeSource)), nil
				}
				return []byte(fmt.Sprintf("%s dev eth0 src %s\n", args[2], routeSource)), nil
			}
			if name == "ss" {
				return []byte(listeners), nil
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
	if !slices.Equal(arpingArgs, []string{"arping", "-D", "-I", "eth0", "-c", "2", "10.40.0.100"}) {
		t.Fatalf("arping arguments = %q", arpingArgs)
	}
	for _, port := range []int{80, 443} {
		listeners = fmt.Sprintf("LISTEN 0 4096 0.0.0.0:%d 0.0.0.0:*\n", port)
		if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("TCP port %d is already occupied", port)) {
			t.Fatalf("preflight(occupied Fleet port %d) error = %v", port, err)
		}
	}
	listeners = ""

	host.localIPs = func() ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr(testHostIPs[0]), netip.MustParseAddr("10.40.0.100")}, nil
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "HA_VIRTUAL_IP is already assigned") {
		t.Fatalf("preflight(assigned VIP) error = %v", err)
	}
	host.localIPs = func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(testHostIPs[0])}, nil }
	prefixes = []netip.Prefix{netip.MustParsePrefix("10.40.0.11/26")}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "IPv4 network") {
		t.Fatalf("preflight(VIP outside interface network) error = %v", err)
	}
	prefixes = []netip.Prefix{netip.MustParsePrefix("10.40.0.11/24")}
	databasePeerViaGateway = true
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "database peer") {
		t.Fatalf("preflight(routed database peer) error = %v", err)
	}
	databasePeerViaGateway = false
	prefixes = []netip.Prefix{netip.MustParsePrefix("10.40.0.11/32")}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "database peer") {
		t.Fatalf("preflight(database peer outside interface prefix) error = %v", err)
	}
	prefixes = []netip.Prefix{netip.MustParsePrefix("10.40.0.11/24")}

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
	arpingConflict = true
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "HA_VIRTUAL_IP is in use") {
		t.Fatalf("preflight(conflicting VIP) error = %v", err)
	}
	arpingConflict = false

	witnessEnvPath := filepath.Join(root, "witness.env")
	witnessEnv, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	witnessEnv = []byte(strings.NewReplacer(
		"HA_NODE_NAME=ha-a", "HA_NODE_NAME=ha-c",
		"HA_NODE_IP="+testHostIPs[0], "HA_NODE_IP="+testHostIPs[2],
		filepath.Join(generated, "ha-a"), filepath.Join(generated, "ha-c"),
	).Replace(string(witnessEnv)))
	if err := os.WriteFile(witnessEnvPath, witnessEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	host.localIPs = func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(testHostIPs[2])}, nil }
	host.interfacePrefixes = func(string) ([]netip.Prefix, error) {
		t.Fatal("preflight queried the witness VIP interface")
		return nil, nil
	}
	routeSource = testHostIPs[2]
	routeViaGateway = true
	arpingConflict = true
	listeners = "LISTEN 0 4096 0.0.0.0:80 0.0.0.0:*\nLISTEN 0 4096 0.0.0.0:443 0.0.0.0:*\n"
	host.applyFirewall = func(_ context.Context, gotConfig NodeConfig, _ string) error {
		if gotConfig.NodeName != "ha-c" {
			t.Fatalf("witness preflight node = %q", gotConfig.NodeName)
		}
		return nil
	}
	if _, err := preflight(context.Background(), witnessEnvPath, firewallTemplatePath, host); err != nil {
		t.Fatalf("preflight(off-L2 witness) error = %v", err)
	}
	host.localIPs = func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(testHostIPs[0])}, nil }
	host.interfacePrefixes = func(string) ([]netip.Prefix, error) { return prefixes, nil }
	routeSource = testHostIPs[0]
	routeViaGateway = false
	arpingConflict = false
	listeners = ""

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
	if err := os.WriteFile(filepath.Join(dataDir, "postgres", "PG_VERSION"), []byte("18"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preflight(context.Background(), envPath, firewallTemplatePath, host); err == nil || !strings.Contains(err.Error(), "pre-existing PostgreSQL state") {
		t.Fatalf("preflight(existing database) error = %v", err)
	}
}

func TestValidateVirtualIPPrefixRejectsNetworkAndBroadcast(t *testing.T) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("10.40.0.11/24")}
	for _, virtualIP := range []string{"10.40.0.0", "10.40.0.255"} {
		if err := validateVirtualIPPrefix(netip.MustParseAddr(testHostIPs[0]), netip.MustParseAddr(virtualIP), prefixes); err == nil || !strings.Contains(err.Error(), "network or broadcast") {
			t.Fatalf("validateVirtualIPPrefix(%s) error = %v", virtualIP, err)
		}
	}
}

func TestBootstrapEtcdAuthEnablesAuthLast(t *testing.T) {
	client := &recordingAuthClient{}
	if err := bootstrapEtcdAuth(context.Background(), client, "root-pass", "patroni-pass", "fleet-pass"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"auth-status",
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
		"verify-access",
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

func TestBootstrapEtcdAuthVerifiesExistingAccess(t *testing.T) {
	// Arrange
	client := &recordingAuthClient{authEnabled: true}

	// Act
	err := bootstrapEtcdAuth(context.Background(), client, "root-pass", "patroni-pass", "fleet-pass")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"auth-status", "verify-access"}; !slices.Equal(client.calls, want) {
		t.Fatalf("bootstrap calls = %v, want %v", client.calls, want)
	}

	invalid := &recordingAuthClient{authEnabled: true, failAt: "verify-access"}
	if err := bootstrapEtcdAuth(context.Background(), invalid, "root-pass", "patroni-pass", "fleet-pass"); err == nil {
		t.Fatal("bootstrap accepted an unverified existing authentication policy")
	}
}

type recordingAuthClient struct {
	calls       []string
	failAt      string
	authEnabled bool
}

func (c *recordingAuthClient) record(call string) error {
	c.calls = append(c.calls, call)
	if call == c.failAt {
		return errors.New("injected failure")
	}
	return nil
}

func (c *recordingAuthClient) AuthEnabled(context.Context) (bool, error) {
	if err := c.record("auth-status"); err != nil {
		return false, err
	}
	return c.authEnabled, nil
}
func (c *recordingAuthClient) EnsureRole(_ context.Context, role string) error {
	return c.record("role:" + role)
}
func (c *recordingAuthClient) GrantPermission(_ context.Context, role, prefix string, permission clientv3.PermissionType) error {
	name := "read"
	if permission == clientv3.PermissionType(clientv3.PermReadWrite) {
		name = "readwrite"
	}
	return c.record(fmt.Sprintf("permission:%s:%s:%s", role, prefix, name))
}
func (c *recordingAuthClient) EnsureUser(_ context.Context, user, password string) error {
	return c.record("user:" + user + ":" + password)
}
func (c *recordingAuthClient) GrantRole(_ context.Context, user, role string) error {
	return c.record("grant:" + user + ":" + role)
}
func (c *recordingAuthClient) EnableAuth(context.Context) error { return c.record("auth") }
func (c *recordingAuthClient) VerifyAccess(context.Context, string, string, string) error {
	return c.record("verify-access")
}

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
