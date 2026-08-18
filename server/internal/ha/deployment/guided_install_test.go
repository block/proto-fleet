package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGuidedInstallPreparesClusterAndInstallsHAA(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	inspected := false
	input := strings.NewReader(testHostIPs[1] + "\n" + testHostIPs[2] + "\n" + testVirtualIP + "\n\nINSTALL\n")
	var output, prompts bytes.Buffer
	deps := testGuidedDependencies(source, input, &output, &prompts)
	deps.operatorUsername = func() string { return "operator" }
	var sshEvents []string
	var expectedFingerprint string
	deps.checkPeer = func(_ context.Context, localUser, target string) error {
		require.Equal(t, "operator", localUser)
		sshEvents = append(sshEvents, "check "+target)
		return nil
	}
	deps.transferBundle = func(_ context.Context, localUser, target, bundlePath string) error {
		require.Equal(t, "operator", localUser)
		require.FileExists(t, bundlePath)
		requireMode(t, bundlePath, 0o600)
		if expectedFingerprint == "" {
			bundle, err := readHostBundle(bundlePath)
			require.NoError(t, err)
			expectedFingerprint, err = serviceCAFingerprint(bundle.Secrets["service-ca.crt"])
			require.NoError(t, err)
		}
		sshEvents = append(sshEvents, "transfer "+target)
		return nil
	}
	identityPeer := ""
	deps.primaryIdentity = func(_ context.Context, peer string) (hostIdentity, error) {
		require.Contains(t, prompts.String(), "ha-b IPv4 address")
		require.Contains(t, prompts.String(), "ha-c IPv4 address")
		require.Contains(t, prompts.String(), "Virtual IPv4 address")
		identityPeer = peer
		return hostIdentity{address: testHostIPs[0], networkInterface: "enp1s0"}, nil
	}
	deps.interfaceForIP = func(string) (string, error) { return "enp1s0", nil }
	deps.makeExportDir = func() (string, error) {
		return exportDir, os.Mkdir(exportDir, 0o700)
	}
	deps.inspect = func(_ context.Context, _ string, config NodeConfig) (installedDependencies, error) {
		require.Equal(t, testHostIPs[0], config.NodeIP)
		require.Equal(t, "enp1s0", config.NetworkInterface)
		inspected = true
		return installedDependencies{docker: true}, nil
	}
	deps.install = func(_ context.Context, options InstallOptions) error {
		require.True(t, inspected)
		require.FileExists(t, filepath.Join(exportDir, hostBundleName("ha-b")))
		require.FileExists(t, filepath.Join(exportDir, hostBundleName("ha-c")))
		config, err := loadNodeConfig(options.NodeEnvPath)
		require.NoError(t, err)
		require.Equal(t, "ha-a", config.NodeName)
		require.Equal(t, "enp1s0", config.NetworkInterface)
		require.FileExists(t, filepath.Join(config.SecretsDir, "etcd-server.crt"))
		require.FileExists(t, options.EtcdRootPasswordFile)
		return nil
	}

	// Act
	err := guidedInstall(t.Context(), "", deps)

	// Assert
	require.NoError(t, err)
	require.Equal(t, testHostIPs[1], identityPeer)
	require.Contains(t, prompts.String(), "Type INSTALL")
	require.Contains(t, prompts.String(), "Peer SSH username [operator]")
	require.NotContains(t, prompts.String(), "Type COPIED")
	require.Contains(t, prompts.String(), "Docker:    reuse existing installation")
	require.NotContains(t, output.String(), testEtcdRootPassword)
	require.Contains(t, output.String(), peerInstallCommand("operator", testHostIPs[1], "v0.2.10"))
	require.Contains(t, output.String(), peerInstallCommand("operator", testHostIPs[2], "v0.2.10"))
	require.Contains(t, output.String(), "test -f /var/tmp/proto-fleet-ha-host.json")
	require.Contains(t, output.String(), "releases/download/v0.2.10/install.sh")
	require.NotContains(t, output.String(), "curl -fsSL https://fleet.proto.xyz/install.sh |")
	require.Contains(t, output.String(), "Public service CA SHA-256 fingerprint: "+expectedFingerprint)
	require.Equal(t, []string{
		"check operator@" + testHostIPs[1],
		"check operator@" + testHostIPs[2],
		"transfer operator@" + testHostIPs[1],
		"transfer operator@" + testHostIPs[2],
	}, sshEvents)
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-a")))
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-b")))
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-c")))
}

func TestServiceCAFingerprintMatchesOpenSSL(t *testing.T) {
	// Arrange
	const certificate = `-----BEGIN CERTIFICATE-----
MIIBoTCCAUegAwIBAgIUXLAXgfn7OuXjkUfB2NO7RzuV25UwCgYIKoZIzj0EAwIw
JjEkMCIGA1UEAwwbUHJvdG8gRmxlZXQgdGVzdCBzZXJ2aWNlIENBMB4XDTI2MDgx
ODIxNTgyOVoXDTM2MDgxNTIxNTgyOVowJjEkMCIGA1UEAwwbUHJvdG8gRmxlZXQg
dGVzdCBzZXJ2aWNlIENBMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEMRNxVvQt
K6p55t4/fA9/wapZwOrPVqANtGHEPhTKo+2PqqBTccydn1Xn6vp0xi/6eONi1er3
R0wwLDNMKKUsTaNTMFEwHQYDVR0OBBYEFLWxZhcDBioJ+WxBjThpXsInI0LUMB8G
A1UdIwQYMBaAFLWxZhcDBioJ+WxBjThpXsInI0LUMA8GA1UdEwEB/wQFMAMBAf8w
CgYIKoZIzj0EAwIDSAAwRQIhANLd2j3ndHOTdvuP3OQamgqu2vgcntLR2RVshe/S
ebCBAiACqqak+Din9945lq0fFYzfw1ybLTC+HvyDSRCMT1bk0A==
-----END CERTIFICATE-----
`

	// Act
	fingerprint, err := serviceCAFingerprint([]byte(certificate))

	// Assert
	require.NoError(t, err)
	require.Equal(t, "43:0C:7C:D7:B8:4B:1C:47:AA:D0:0B:78:CF:DA:A6:AD:CC:54:CD:A3:B3:BD:66:DF:8D:4D:83:E0:CC:96:38:1B", fingerprint)
}

func TestGuidedInstallUsesPeerSSHUsernameOverride(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	input := strings.NewReader(testHostIPs[1] + "\n" + testHostIPs[2] + "\n" + testVirtualIP + "\npeer-admin\nINSTALL\n")
	deps := testGuidedDependencies(source, input, &bytes.Buffer{}, &bytes.Buffer{})
	deps.operatorUsername = func() string { return "operator" }
	deps.makeExportDir = func() (string, error) { return exportDir, os.Mkdir(exportDir, 0o700) }
	var targets []string
	deps.checkPeer = func(_ context.Context, localUser, target string) error {
		require.Equal(t, "operator", localUser)
		targets = append(targets, target)
		return nil
	}
	deps.transferBundle = func(_ context.Context, _, target, _ string) error {
		targets = append(targets, target)
		return nil
	}

	// Act
	err := guidedInstall(t.Context(), "", deps)

	// Assert
	require.NoError(t, err)
	require.Equal(t, []string{
		"peer-admin@" + testHostIPs[1],
		"peer-admin@" + testHostIPs[2],
		"peer-admin@" + testHostIPs[1],
		"peer-admin@" + testHostIPs[2],
	}, targets)
}

func TestGuidedInstallRetainsExportsWhenPeerTransferFails(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	input := strings.NewReader(testHostIPs[1] + "\n" + testHostIPs[2] + "\n" + testVirtualIP + "\n\nINSTALL\n")
	deps := testGuidedDependencies(source, input, &bytes.Buffer{}, &bytes.Buffer{})
	deps.operatorUsername = func() string { return "operator" }
	deps.makeExportDir = func() (string, error) { return exportDir, os.Mkdir(exportDir, 0o700) }
	deps.checkPeer = func(context.Context, string, string) error { return nil }
	deps.transferBundle = func(_ context.Context, _, target, _ string) error {
		if target == "operator@"+testHostIPs[2] {
			return errors.New("connection lost")
		}
		return nil
	}
	installed := false
	deps.install = func(context.Context, InstallOptions) error {
		installed = true
		return nil
	}

	// Act
	err := guidedInstall(t.Context(), "", deps)

	// Assert
	require.ErrorContains(t, err, "operator@"+testHostIPs[2])
	require.ErrorContains(t, err, "connection lost")
	require.False(t, installed)
	require.FileExists(t, filepath.Join(exportDir, hostBundleName("ha-a")))
	require.FileExists(t, filepath.Join(exportDir, hostBundleName("ha-b")))
	require.FileExists(t, filepath.Join(exportDir, hostBundleName("ha-c")))
}

func TestBundleExportDirectoryIsOutsideRelease(t *testing.T) {
	// Arrange
	parent := t.TempDir()
	source := filepath.Join(parent, "release")
	require.NoError(t, os.Mkdir(source, 0o700))

	// Act
	exportDir, err := makeBundleExportDir()

	// Assert
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(exportDir)) })
	relative, err := filepath.Rel(parent, exportDir)
	require.NoError(t, err)
	require.True(t, relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)))
	info, err := os.Stat(exportDir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestPrepareInstallBundlesCreatesRoleScopedBundles(t *testing.T) {
	// Arrange
	exportDir := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.Mkdir(exportDir, 0o700))
	metadata := clusterMetadata{
		Version: "v0.2.10", Commit: "abc123", DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
	}

	// Act
	err := prepareInstallBundles(exportDir, metadata)

	// Assert
	require.NoError(t, err)
	for _, role := range []string{"ha-a", "ha-b", "ha-c"} {
		bundle, err := readHostBundle(filepath.Join(exportDir, hostBundleName(role)))
		require.NoError(t, err)
		require.Equal(t, role, bundle.Metadata.Role)
		require.NotContains(t, bundle.Secrets, "service-ca.key")
		require.Equal(t, role == "ha-a", len(bundle.EtcdRootPassword) != 0)
	}
	for _, name := range []string{hostBundleName("ha-a"), hostBundleName("ha-b"), hostBundleName("ha-c")} {
		requireMode(t, filepath.Join(exportDir, name), 0o600)
		require.NoFileExists(t, filepath.Join(exportDir, name+".sha256"))
	}
}

func TestDecodeHostBundleRejectsUnexpectedContent(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "bundle.json")
	writeValidTestBundle(t, path, testBundleMetadata("ha-c"))
	valid, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
		error  string
	}{
		{name: "unknown top-level field", error: "unknown field", mutate: func(contents []byte) []byte {
			var document map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(contents, &document))
			document["unexpected"] = json.RawMessage(`true`)
			mutated, err := json.Marshal(document)
			require.NoError(t, err)
			return mutated
		}},
		{name: "unused role secret", error: "secret not used by this role", mutate: func(contents []byte) []byte {
			var bundle preparedHostBundle
			require.NoError(t, json.Unmarshal(contents, &bundle))
			bundle.Secrets["operator-notes"] = []byte("bad")
			mutated, err := json.Marshal(bundle)
			require.NoError(t, err)
			return mutated
		}},
		{name: "trailing JSON", error: "after the bundle document", mutate: func(contents []byte) []byte {
			return append(contents, []byte("\n{}")...)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			_, err := decodeHostBundle(test.mutate(slices.Clone(valid)))

			// Assert
			require.ErrorContains(t, err, test.error)
		})
	}
}

func TestInstallHostBundleValidatesIdentityAndRelease(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.Mkdir(exportDir, 0o700))
	bundlePath := filepath.Join(exportDir, hostBundleName("ha-b"))
	writeValidTestBundle(t, bundlePath, testBundleMetadata("ha-b"))

	// Act: the bundle is presented to the wrong host.
	deps := testGuidedDependencies(source, strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
	deps.interfaceForIP = func(string) (string, error) { return "", os.ErrNotExist }
	err := guidedInstall(t.Context(), bundlePath, deps)

	// Assert
	require.ErrorContains(t, err, "is not assigned to this host")
	require.FileExists(t, bundlePath)

	// Act: the matching host runs a different release.
	deps = testGuidedDependencies(testGuidedRelease(t, "v0.2.11", "def456"), strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
	deps.interfaceForIP = func(string) (string, error) { return "eth9", nil }
	err = guidedInstall(t.Context(), bundlePath, deps)

	// Assert
	require.ErrorContains(t, err, "release does not match")
	require.FileExists(t, bundlePath)
}

func TestGuidedInstallRejectsUnsafePackagedReleaseVersion(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10'", "abc123")
	deps := testGuidedDependencies(source, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Act
	err := guidedInstall(t.Context(), "", deps)

	// Assert
	require.ErrorContains(t, err, "safe version and commit values")
}

func TestInstallHostBundleConsumption(t *testing.T) {
	tests := []struct {
		name       string
		installErr error
		wantExists bool
	}{
		{name: "success"},
		{name: "fatal failure", installErr: context.Canceled, wantExists: true},
		{name: "service converging", installErr: fmt.Errorf("%w; check systemctl", errInstallConverging)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			source := testGuidedRelease(t, "v0.2.10", "abc123")
			bundlePath := filepath.Join(t.TempDir(), hostBundleName("ha-c"))
			writeValidTestBundle(t, bundlePath, testBundleMetadata("ha-c"))
			var prompts bytes.Buffer
			deps := testGuidedDependencies(source, strings.NewReader(""), &bytes.Buffer{}, &prompts)
			deps.terminal = func() bool { return false }
			inspected := false
			deps.inspect = func(context.Context, string, NodeConfig) (installedDependencies, error) {
				inspected = true
				return installedDependencies{}, nil
			}
			deps.install = func(context.Context, InstallOptions) error { return tt.installErr }

			// Act
			err := guidedInstall(t.Context(), bundlePath, deps)

			// Assert
			require.ErrorIs(t, err, tt.installErr)
			require.True(t, inspected)
			require.NotContains(t, prompts.String(), "Type INSTALL")
			if tt.wantExists {
				require.FileExists(t, bundlePath)
			} else {
				require.NoFileExists(t, bundlePath)
			}
		})
	}
}

func testBundleMetadata(role string) bundleMetadata {
	return bundleMetadata{
		FormatVersion: bundleFormatVersion, Role: role,
		NodeIP:      map[string]string{"ha-a": testHostIPs[0], "ha-b": testHostIPs[1], "ha-c": testHostIPs[2]}[role],
		DatabaseAIP: testHostIPs[0], DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2],
		VirtualIP: testVirtualIP, Version: "v0.2.10", Commit: "abc123",
	}
}

func writeValidTestBundle(t *testing.T, path string, metadata bundleMetadata) {
	t.Helper()
	secrets := make(map[string][]byte)
	for _, name := range copiedSecretFiles(NodeConfig{NodeName: metadata.Role}) {
		secrets[name] = []byte("test")
	}
	writeTestBundle(t, path, metadata, secrets)
}

func writeTestBundle(t *testing.T, path string, metadata bundleMetadata, secrets map[string][]byte) {
	t.Helper()
	bundle := preparedHostBundle{Metadata: metadata, Secrets: secrets}
	if metadata.Role == "ha-a" {
		bundle.EtcdRootPassword = []byte("test")
	}
	contents, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}

func testGuidedRelease(t *testing.T, version, commit string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "version.txt"), []byte("version: "+version+"\ncommit: "+commit+"\n"), 0o600))
	return root
}

func testGuidedDependencies(source string, input *strings.Reader, output, prompts *bytes.Buffer) guidedInstallDependencies {
	return guidedInstallDependencies{
		input: input, output: output, prompts: prompts, terminal: func() bool { return true },
		sourceRoot: func() (string, error) { return source, nil },
		primaryIdentity: func(context.Context, string) (hostIdentity, error) {
			return hostIdentity{address: testHostIPs[0], networkInterface: "eth0"}, nil
		},
		interfaceForIP:   func(string) (string, error) { return "eth0", nil },
		makeExportDir:    func() (string, error) { return os.MkdirTemp("", "fleet-ha-test-exports-") },
		operatorUsername: func() string { return "operator" },
		checkPeer:        func(context.Context, string, string) error { return nil },
		transferBundle:   func(context.Context, string, string, string) error { return nil },
		inspect: func(context.Context, string, NodeConfig) (installedDependencies, error) {
			return installedDependencies{}, nil
		},
		install: func(context.Context, InstallOptions) error { return nil },
	}
}
