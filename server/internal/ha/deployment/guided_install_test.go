package deployment

import (
	"bytes"
	"context"
	"encoding/json"
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
	input := strings.NewReader(testHostIPs[1] + "\n" + testHostIPs[2] + "\n" + testVirtualIP + "\nINSTALL\ncopied\n")
	var output, prompts bytes.Buffer
	deps := testGuidedDependencies(source, input, &output, &prompts)
	identityPeer := ""
	deps.primaryIdentity = func(_ context.Context, peer string) (hostIdentity, error) {
		require.Contains(t, prompts.String(), "ha-b IPv4 address")
		require.Contains(t, prompts.String(), "ha-c IPv4 address")
		require.Contains(t, prompts.String(), "Virtual IPv4 address")
		identityPeer = peer
		return hostIdentity{address: testHostIPs[0], networkInterface: "enp1s0"}, nil
	}
	deps.interfaceForIP = func(string) (string, error) { return "enp1s0", nil }
	deps.makeExportDir = func(string) (string, error) {
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
	require.Contains(t, prompts.String(), "Type COPIED")
	require.Contains(t, prompts.String(), "Docker:    reuse existing installation")
	require.NotContains(t, output.String(), testEtcdRootPassword)
	require.Contains(t, output.String(), "Service CA SHA-256 fingerprint:")
	require.Contains(t, output.String(), "On your operator machine")
	require.Contains(t, output.String(), "from ha-a")
	require.Contains(t, output.String(), "scp -p")
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-a")))
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-b")))
	require.NoFileExists(t, filepath.Join(exportDir, publicCAName))
}

func TestBundleExportDirectoryIsOutsideRelease(t *testing.T) {
	// Arrange
	parent := t.TempDir()
	source := filepath.Join(parent, "release")
	require.NoError(t, os.Mkdir(source, 0o700))

	// Act
	exportDir, err := makeBundleExportDir(source)

	// Assert
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(exportDir)) })
	require.Equal(t, parent, filepath.Dir(exportDir))
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
	var serviceCA []byte
	for _, role := range []string{"ha-a", "ha-b", "ha-c"} {
		bundle, err := readHostBundle(filepath.Join(exportDir, hostBundleName(role)))
		require.NoError(t, err)
		require.Equal(t, role, bundle.Metadata.Role)
		require.NotContains(t, bundle.Secrets, "service-ca.key")
		serviceCA = bundle.Secrets["service-ca.crt"]
		require.Equal(t, role == "ha-a", len(bundle.EtcdRootPassword) != 0)
	}
	for _, name := range []string{hostBundleName("ha-a"), hostBundleName("ha-b"), hostBundleName("ha-c")} {
		requireMode(t, filepath.Join(exportDir, name), 0o600)
		require.NoFileExists(t, filepath.Join(exportDir, name+".sha256"))
	}
	publicCA, err := os.ReadFile(filepath.Join(exportDir, publicCAName))
	require.NoError(t, err)
	require.Equal(t, serviceCA, publicCA)
	requireMode(t, filepath.Join(exportDir, publicCAName), 0o644)
	require.NoFileExists(t, filepath.Join(exportDir, publicCAName+".sha256"))
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
			deps := testGuidedDependencies(source, strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
			deps.install = func(context.Context, InstallOptions) error { return tt.installErr }

			// Act
			err := guidedInstall(t.Context(), bundlePath, deps)

			// Assert
			require.ErrorIs(t, err, tt.installErr)
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
		interfaceForIP: func(string) (string, error) { return "eth0", nil },
		makeExportDir:  func(string) (string, error) { return os.MkdirTemp("", "fleet-ha-test-exports-") },
		inspect: func(context.Context, string, NodeConfig) (installedDependencies, error) {
			return installedDependencies{}, nil
		},
		install: func(context.Context, InstallOptions) error { return nil },
	}
}
