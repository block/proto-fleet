package deployment

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGuidedInstallPreparesClusterAndInstallsHAA(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	var installed InstallOptions
	inspected := false
	input := strings.NewReader(testHostIPs[1] + "\n" + testHostIPs[2] + "\n" + testVirtualIP + "\nINSTALL\nCOPIED\n")
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
		installed = options
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
	require.NotEmpty(t, installed.NodeEnvPath)
	require.Contains(t, prompts.String(), "ha-b IPv4 address")
	require.Contains(t, prompts.String(), "Type INSTALL")
	require.Contains(t, prompts.String(), "Type COPIED")
	require.Contains(t, prompts.String(), "Docker:    reuse existing installation")
	require.NotContains(t, output.String(), testEtcdRootPassword)
	require.Contains(t, output.String(), "Service CA SHA-256 fingerprint:")
	require.Contains(t, output.String(), "scp -p")
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-a")))
	require.NoFileExists(t, filepath.Join(exportDir, hostBundleName("ha-b")))
	require.NoFileExists(t, filepath.Join(exportDir, recoveryBundleName))
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

func TestPrepareInstallBundlesSeparatesRecoveryCredentials(t *testing.T) {
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
		require.Equal(t, role, bundle.metadata.Role)
		require.NotContains(t, bundle.files, "secrets/service-ca.key")
		serviceCA = bundle.files["secrets/service-ca.crt"]
		_, hasRootPassword := bundle.files[etcdRootPasswordFile]
		require.Equal(t, role == "ha-a", hasRootPassword)
	}
	recoveryEntries := archiveEntries(t, filepath.Join(exportDir, recoveryBundleName))
	require.Contains(t, recoveryEntries, "service-ca.key")
	require.Contains(t, recoveryEntries, etcdRootPasswordFile)
	for _, name := range []string{hostBundleName("ha-a"), hostBundleName("ha-b"), hostBundleName("ha-c"), recoveryBundleName} {
		requireMode(t, filepath.Join(exportDir, name), 0o600)
		requireMode(t, filepath.Join(exportDir, name+bundleChecksumSuffix), 0o600)
	}
	publicCA, err := os.ReadFile(filepath.Join(exportDir, publicCAName))
	require.NoError(t, err)
	require.Equal(t, serviceCA, publicCA)
	requireMode(t, filepath.Join(exportDir, publicCAName), 0o644)
	requireMode(t, filepath.Join(exportDir, publicCAName+bundleChecksumSuffix), 0o644)
}

func TestReadHostBundleRejectsUnsafeArchiveEntries(t *testing.T) {
	tests := []struct {
		name      string
		entryName string
		entryType byte
		contents  []byte
		wantError string
	}{
		{name: "traversal", entryName: "../service-ca.key", entryType: tar.TypeReg, wantError: "unsafe path"},
		{name: "symlink", entryName: "secrets/service-ca.crt", entryType: tar.TypeSymlink, wantError: "regular file"},
		{name: "unexpected", entryName: "secrets/operator-notes", entryType: tar.TypeReg, wantError: "unexpected entry"},
		{name: "oversized", entryName: "secrets/service-ca.crt", entryType: tar.TypeReg, contents: make([]byte, maxBundleFileSize+1), wantError: "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			path := filepath.Join(t.TempDir(), "bundle.tar.gz")
			metadata := bundleMetadata{
				FormatVersion: bundleFormatVersion, Role: "ha-c", NodeIP: testHostIPs[2],
				DatabaseAIP: testHostIPs[0], DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2],
				VirtualIP: testVirtualIP, Version: "v0.2.10", Commit: "abc123",
			}
			contents := tt.contents
			if contents == nil {
				contents = []byte("bad")
			}
			writeTestBundle(t, path, metadata, archiveTestEntry{name: tt.entryName, typeFlag: tt.entryType, contents: contents})

			// Act
			_, err := readHostBundle(path)

			// Assert
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestInstallHostBundleValidatesIdentityAndRelease(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.Mkdir(exportDir, 0o700))
	require.NoError(t, prepareInstallBundles(exportDir, clusterMetadata{
		Version: "v0.2.10", Commit: "abc123", DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
	}))
	bundlePath := filepath.Join(exportDir, hostBundleName("ha-b"))

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

func TestInstallHostBundleRejectsChecksumMismatchBeforeInstall(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.Mkdir(exportDir, 0o700))
	require.NoError(t, prepareInstallBundles(exportDir, clusterMetadata{
		Version: "v0.2.10", Commit: "abc123", DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
	}))
	bundlePath := filepath.Join(exportDir, hostBundleName("ha-b"))
	bundle, err := os.OpenFile(bundlePath, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = bundle.WriteString("corrupt")
	require.NoError(t, err)
	require.NoError(t, bundle.Close())
	installCalled := false
	deps := testGuidedDependencies(source, strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
	deps.install = func(context.Context, InstallOptions) error {
		installCalled = true
		return nil
	}

	// Act
	err = guidedInstall(t.Context(), bundlePath, deps)

	// Assert
	require.ErrorContains(t, err, "checksum does not match")
	require.False(t, installCalled)
	require.FileExists(t, bundlePath)
	require.FileExists(t, bundlePath+bundleChecksumSuffix)
}

func TestInstallHostBundleDeletesBundleOnlyAfterSuccess(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.Mkdir(exportDir, 0o700))
	require.NoError(t, prepareInstallBundles(exportDir, clusterMetadata{
		Version: "v0.2.10", Commit: "abc123", DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
	}))
	original := filepath.Join(exportDir, hostBundleName("ha-c"))

	// Act: installation fails.
	deps := testGuidedDependencies(source, strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
	deps.install = func(context.Context, InstallOptions) error { return context.Canceled }
	err := guidedInstall(t.Context(), original, deps)

	// Assert
	require.ErrorIs(t, err, context.Canceled)
	require.FileExists(t, original)
	require.FileExists(t, original+bundleChecksumSuffix)

	// Act: installation succeeds.
	deps = testGuidedDependencies(source, strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
	err = guidedInstall(t.Context(), original, deps)

	// Assert
	require.NoError(t, err)
	require.NoFileExists(t, original)
	require.NoFileExists(t, original+bundleChecksumSuffix)
}

func TestInstallHostBundleDeletesBundleWhileServiceConverges(t *testing.T) {
	// Arrange
	source := testGuidedRelease(t, "v0.2.10", "abc123")
	exportDir := filepath.Join(t.TempDir(), "exports")
	require.NoError(t, os.Mkdir(exportDir, 0o700))
	require.NoError(t, prepareInstallBundles(exportDir, clusterMetadata{
		Version: "v0.2.10", Commit: "abc123", DatabaseAIP: testHostIPs[0],
		DatabaseBIP: testHostIPs[1], WitnessIP: testHostIPs[2], VirtualIP: testVirtualIP,
	}))
	bundlePath := filepath.Join(exportDir, hostBundleName("ha-c"))
	deps := testGuidedDependencies(source, strings.NewReader("INSTALL\n"), &bytes.Buffer{}, &bytes.Buffer{})
	deps.install = func(context.Context, InstallOptions) error {
		return fmt.Errorf("%w; check systemctl", errInstallConverging)
	}

	// Act
	err := guidedInstall(t.Context(), bundlePath, deps)

	// Assert
	require.ErrorIs(t, err, errInstallConverging)
	require.ErrorContains(t, err, "check systemctl")
	require.NoFileExists(t, bundlePath)
	require.NoFileExists(t, bundlePath+bundleChecksumSuffix)
}

type archiveTestEntry struct {
	name     string
	typeFlag byte
	contents []byte
}

func writeTestBundle(t *testing.T, path string, metadata bundleMetadata, entries ...archiveTestEntry) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	require.NoError(t, err)
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	metadataJSON, err := metadata.marshal()
	require.NoError(t, err)
	require.NoError(t, writeTarEntry(tarWriter, bundleMetadataFile, metadataJSON, 0o600))
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.typeFlag, Mode: 0o600, Size: int64(len(entry.contents))}
		require.NoError(t, tarWriter.WriteHeader(header))
		if entry.typeFlag == tar.TypeReg {
			_, err = tarWriter.Write(entry.contents)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, file.Close())
}

func archiveEntries(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	require.NoError(t, err)
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := make(map[string]struct{})
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		entries[header.Name] = struct{}{}
	}
	return entries
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
