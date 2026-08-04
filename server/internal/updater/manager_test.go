package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type recordedCommand struct {
	Dir  string
	Name string
	Args []string
}

type recordingRunner struct {
	mu          sync.Mutex
	commands    []recordedCommand
	preflight   error
	activation  error
	cleanup     error
	preflightAt chan struct{}
	release     chan struct{}
}

const operatorEnv = `DB_PASSWORD=secret
ENABLE_BETA_ALERTS=true
ENABLE_SYSTEM_MONITORING=true
ENABLE_TRACING=true
ENABLE_ONE_CLICK_UPDATES=true
`

func (r *recordingRunner) Run(_ context.Context, dir string, _ io.Writer, name string, args ...string) error {
	r.mu.Lock()
	r.commands = append(r.commands, recordedCommand{Dir: dir, Name: name, Args: append([]string(nil), args...)})
	r.mu.Unlock()

	if name == "/bin/bash" && len(args) > 0 && args[len(args)-1] == "--preflight-only" {
		if r.preflightAt != nil {
			close(r.preflightAt)
		}
		if r.release != nil {
			<-r.release
		}
		return r.preflight
	}
	if name == "docker" {
		return r.cleanup
	}
	return r.activation
}

func (r *recordingRunner) Commands() []recordedCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCommand(nil), r.commands...)
}

func TestManagerUpgradeStagesBeforeActivationAndPreservesConfiguration(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{}
	manager := newTestManager(t, installRoot, server, runner)

	operation, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	assert.Equal(t, updaterapi.PhaseQueued, operation.Phase)

	completed := waitForTerminal(t, manager)
	require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
	assert.Equal(t, "v1.1.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment.previous", "version.txt")))
	assert.Equal(t, operatorEnv, mustReadFile(t, filepath.Join(installRoot, "deployment", ".env")))
	assert.Equal(t, "certificate\n", mustReadFile(t, filepath.Join(installRoot, "deployment", "ssl", "cert.pem")))
	assert.Equal(t, "influx-secret\n", mustReadFile(t, filepath.Join(installRoot, "deployment", "server", "influx_config", ".env")))

	commands := runner.Commands()
	require.Len(t, commands, 2)
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--preflight-only"}, commands[0].Args)
	assert.Contains(t, commands[0].Dir, ".proto-fleet-upgrade-")
	assert.Equal(t, filepath.Join(installRoot, "deployment"), commands[1].Dir)
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--skip-build"}, commands[1].Args)
	assert.FileExists(t, completed.LogPath)
	assert.Contains(t, completed.RecoveryCommand, "./run-fleet.sh --non-interactive --skip-build")
}

func TestManagerChecksumFailureLeavesCurrentDeploymentUntouched(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, strings.Repeat("0", sha256.Size*2))
	runner := &recordingRunner{}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "checksum verification failed")
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	assert.Empty(t, runner.Commands())
}

func TestManagerPreflightFailureNeverTakesTheRunningDeploymentOffline(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{preflight: assert.AnError}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "preflight failed")
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.NoDirExists(t, filepath.Join(installRoot, "deployment.previous"))
	commands := runner.Commands()
	require.Len(t, commands, 1+len(releaseImageRepositories))
	assert.Equal(t, []string{"./run-fleet.sh", "--non-interactive", "--preflight-only"}, commands[0].Args)
	for i, repository := range releaseImageRepositories {
		cleanup := commands[i+1]
		assert.Equal(t, "docker", cleanup.Name)
		assert.Equal(t, []string{"image", "rm", repository + ":v1.1.0"}, cleanup.Args)
		assert.Contains(t, cleanup.Dir, ".proto-fleet-upgrade-")
	}
}

func TestManagerFailedPreflightImageCleanupIsBestEffort(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{preflight: assert.AnError, cleanup: assert.AnError}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	completed := waitForTerminal(t, manager)

	assert.Equal(t, updaterapi.PhaseFailed, completed.Phase)
	assert.Contains(t, completed.Error, "preflight failed")
	assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
	assert.Contains(t, mustReadFile(t, completed.LogPath), "warning: could not remove failed preflight image proto-fleet-api:v1.1.0")
}

func TestManagerRejectsASecondUpgradeWhileOneIsRunning(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	bundle := releaseBundle(t, "v1.1.0")
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	runner := &recordingRunner{
		preflightAt: make(chan struct{}),
		release:     make(chan struct{}),
	}
	manager := newTestManager(t, installRoot, server, runner)

	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	select {
	case <-runner.preflightAt:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade never reached preflight")
	}

	_, err = manager.Trigger("v1.2.0")
	require.ErrorContains(t, err, "already in progress")
	close(runner.release)
	assert.Equal(t, updaterapi.PhaseSucceeded, waitForTerminal(t, manager).Phase)
}

func TestManagerMarksAnInterruptedPersistedOperationFailed(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	started := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, stateFilename),
		[]byte(fmt.Sprintf(`{"id":"old","target_version":"v1.1.0","phase":"activating","started_at":%q,"updated_at":%q}`, started.Format(time.RFC3339Nano), started.Format(time.RFC3339Nano))),
		0o600,
	))

	manager, err := NewManager(Config{
		InstallRoot:     installRoot,
		StateDir:        stateDir,
		DownloadBaseURL: "https://example.invalid",
		GOARCH:          "amd64",
		Now:             func() time.Time { return started.Add(time.Minute) },
	})
	require.NoError(t, err)

	operation := manager.Status().Operation
	require.NotNil(t, operation)
	assert.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	assert.Contains(t, operation.Error, "updater restarted")
	require.NotNil(t, operation.CompletedAt)
}

func TestManagerRejectsUnsafeDownloadBaseURLs(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"http://example.com/releases",
		"https://",
		"https://user@example.com/releases",
		"https://example.com/releases?channel=test",
		"https://example.com/releases#fragment",
	} {
		_, err := NewManager(Config{
			InstallRoot:     t.TempDir(),
			StateDir:        filepath.Join(t.TempDir(), "state"),
			DownloadBaseURL: rawURL,
			GOARCH:          "amd64",
		})
		require.Error(t, err, rawURL)
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	t.Parallel()

	archive := filepath.Join(t.TempDir(), "unsafe.tar.gz")
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}))
	_, err := tarWriter.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, os.WriteFile(archive, buffer.Bytes(), 0o600))

	err = extractArchive(archive, t.TempDir())
	require.ErrorContains(t, err, "unsafe path")
}

func TestAtomicReplaceExecutableRefreshesTheInstalledUpdater(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "staged-updater")
	destination := filepath.Join(root, "installed", "proto-fleet-updater")
	require.NoError(t, os.WriteFile(source, []byte("new updater"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o750))
	require.NoError(t, os.WriteFile(destination, []byte("old updater"), 0o755))

	require.NoError(t, atomicReplaceExecutable(source, destination))
	assert.Equal(t, "new updater", mustReadFile(t, destination))
	info, err := os.Stat(destination)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111)
}

func newTestManager(t *testing.T, installRoot string, server *httptest.Server, runner CommandRunner) *Manager {
	t.Helper()
	manager, err := NewManager(Config{
		InstallRoot:     installRoot,
		StateDir:        filepath.Join(t.TempDir(), "state"),
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Runner:          runner,
		GOARCH:          "amd64",
		NewID:           func() string { return "test-operation" },
	})
	require.NoError(t, err)
	return manager
}

func releaseServer(t *testing.T, version, arch string, bundle []byte, checksumOverride string) *httptest.Server {
	t.Helper()
	archiveName := fmt.Sprintf("proto-fleet-%s-%s.tar.gz", version, arch)
	checksum := fmt.Sprintf("%x", sha256.Sum256(bundle))
	if checksumOverride != "" {
		checksum = checksumOverride
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+version+"/"+archiveName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bundle)
	})
	mux.HandleFunc("/"+version+"/"+archiveName+".sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, archiveName)
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	return server
}

func releaseBundle(t *testing.T, version string) []byte {
	t.Helper()
	files := map[string]string{
		"deployment/version.txt":                         "version: " + version + "\n",
		"deployment/docker-compose.yaml":                 "services: {}\n",
		"deployment/run-fleet.sh":                        "#!/usr/bin/env bash\n",
		"deployment/server/fleetd":                       "fleetd",
		"deployment/server/proto-plugin":                 "plugin",
		"deployment/server/antminer-plugin":              "plugin",
		"deployment/server/asicrs-plugin":                "plugin",
		"deployment/updater/proto-fleet-updater":         "updater",
		"deployment/updater/proto-fleet-updater.service": "[Service]\n",
	}
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, contents := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, "run-fleet.sh") || strings.HasSuffix(name, "fleetd") || strings.HasSuffix(name, "-plugin") || strings.HasSuffix(name, "proto-fleet-updater") {
			mode = 0o755
		}
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(contents)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tarWriter.Write([]byte(contents))
		require.NoError(t, err)
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return buffer.Bytes()
}

func writeCurrentDeployment(t *testing.T, installRoot, version string) {
	t.Helper()
	files := map[string]string{
		"version.txt":               "version: " + version + "\n",
		".env":                      operatorEnv,
		"ssl/cert.pem":              "certificate\n",
		"server/influx_config/.env": "influx-secret\n",
	}
	for name, contents := range files {
		path := filepath.Join(installRoot, "deployment", name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}
}

func waitForTerminal(t *testing.T, manager *Manager) updaterapi.Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status()
		if status.Operation != nil && status.Operation.Phase.Terminal() {
			return *status.Operation
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("upgrade did not reach a terminal state")
	return updaterapi.Operation{}
}

func mustReadVersion(t *testing.T, path string) string {
	t.Helper()
	version, err := readInstalledVersion(path)
	require.NoError(t, err)
	return version
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
