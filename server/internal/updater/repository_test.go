package updater

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/releaseinfo"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type repositoryTransport func(*http.Request) (*http.Response, error)

func (f repositoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func useEmbeddedRepository(t *testing.T, repository string) {
	t.Helper()
	// Build identity is process-global; callers must not run in parallel.
	original := releaseinfo.Repository
	releaseinfo.Repository = repository
	t.Cleanup(func() { releaseinfo.Repository = original })
}

func TestRepositorySurvivesUpgradeAndRestart(t *testing.T) {
	const repository = "example-owner/fleet-fork"
	useEmbeddedRepository(t, repository)
	t.Setenv("PROTO_FLEET_RELEASE_REPOSITORY", "other-owner/fleet")
	root := t.TempDir()
	writeCurrentDeployment(t, root, "v1.0.0")
	metadata := filepath.Join(root, "deployment", "version.txt")
	require.NoError(t, os.WriteFile(metadata, []byte("version: v1.0.0\nrelease_repository: "+repository+"\n"), 0o600))
	bundle := releaseBundle(t, "v1.1.0", repository)
	server := releaseServer(t, "v1.1.0", "amd64", bundle, "")
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	var mu sync.Mutex
	var paths []string
	client := server.Client()
	transport := client.Transport
	client.Transport = repositoryTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		paths = append(paths, r.URL.String())
		mu.Unlock()
		if r.URL.Host != "github.com" || !strings.HasPrefix(r.URL.Path, "/"+repository+"/releases/download/") {
			t.Errorf("unexpected download source %s", r.URL)
		}
		forward := r.Clone(r.Context())
		forward.URL.Scheme, forward.URL.Host = endpoint.Scheme, endpoint.Host
		forward.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+repository+"/releases/download")
		return transport.RoundTrip(forward)
	})
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0o755))
	cfg := Config{InstallRoot: root, StateDir: filepath.Join(t.TempDir(), "state"), HTTPClient: client, Runner: &recordingRunner{}, GOARCH: "amd64", SelfUpdatePath: installedUpdater}
	manager, err := NewManager(cfg)
	require.NoError(t, err)
	_, err = manager.Trigger("v1.1.0")
	require.NoError(t, err)
	require.Equal(t, updaterapi.PhaseSucceeded, waitForTerminal(t, manager).Phase)
	select {
	case path := <-manager.SelfUpdateReady():
		require.Equal(t, manager.cfg.SelfUpdatePath, path)
	case <-time.After(time.Second):
		t.Fatal("successful alternate-source self-update did not request a restart")
	}
	require.Equal(t, "updater", mustReadFile(t, installedUpdater))
	require.NoError(t, manager.Close())
	mu.Lock()
	require.Len(t, paths, 2)
	mu.Unlock()
	require.NoError(t, checkDeploymentRepository(filepath.Join(root, "deployment"), repository))
	require.NoError(t, checkDeploymentRepository(filepath.Join(root, "deployment.previous"), repository))
	manager, err = NewManager(cfg)
	require.NoError(t, err)
	require.Equal(t, repository, manager.Status().ReleaseRepository)
	require.NoError(t, manager.Close())
	releaseinfo.Repository = "block/proto-fleet"
	_, err = NewManager(cfg)
	require.ErrorContains(t, err, "persisted source")
}

func TestRepositoryPinLegacyAndConflict(t *testing.T) {
	useEmbeddedRepository(t, "example-owner/fleet-fork")
	root := t.TempDir()
	writeCurrentDeployment(t, root, "v1.0.0")
	cfg := Config{InstallRoot: root, StateDir: t.TempDir()}
	_, err := NewManager(cfg)
	require.ErrorContains(t, err, "conflicts with trusted updater source")
	releaseinfo.Repository = releaseinfo.DefaultRepository
	manager, err := NewManager(cfg)
	require.NoError(t, err)
	require.NoError(t, manager.Close())
	pin, err := os.ReadFile(filepath.Join(cfg.StateDir, repositoryFilename))
	require.NoError(t, err)
	require.Equal(t, "block/proto-fleet\n", string(pin))
}

func TestAlternateRepositoryRejectsUpstreamBundleBeforeActivation(t *testing.T) {
	useEmbeddedRepository(t, "example-owner/fleet-fork")
	root := t.TempDir()
	writeCurrentDeployment(t, root, "v1.0.0")
	require.NoError(t, os.WriteFile(filepath.Join(root, "deployment", "version.txt"), []byte("version: v1.0.0\nrelease_repository: example-owner/fleet-fork\n"), 0o600))
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	manager := newTestManagerWithConfig(t, root, server, &recordingRunner{}, nil)
	_, err := manager.Trigger("v1.1.0")
	require.NoError(t, err)
	operation := waitForTerminal(t, manager)
	require.Equal(t, updaterapi.PhaseFailed, operation.Phase)
	require.Contains(t, operation.Error, "conflicts with trusted updater source")
	require.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(root, "deployment", "version.txt")))
}

func TestAlternateRepositorySurvivesInterruptedSwap(t *testing.T) {
	const repository = "example-owner/fleet-fork"
	useEmbeddedRepository(t, repository)
	root := t.TempDir()
	writeCurrentDeployment(t, root, "v1.0.0")
	require.NoError(t, os.WriteFile(filepath.Join(root, "deployment", "version.txt"), []byte("version: v1.0.0\nrelease_repository: "+repository+"\n"), 0o600))
	cfg := Config{InstallRoot: root, StateDir: filepath.Join(t.TempDir(), "state")}
	manager, err := NewManager(cfg)
	require.NoError(t, err)
	require.NoError(t, manager.Close())
	require.NoError(t, os.Rename(filepath.Join(root, "deployment"), filepath.Join(root, "deployment.previous")))
	writeInterruptedOperationState(t, cfg.StateDir, "v1.1.0")
	restarted, err := NewManager(cfg)
	require.NoError(t, err)
	require.Equal(t, repository, restarted.Status().ReleaseRepository)
	require.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(root, "deployment", "version.txt")))
	require.NoError(t, checkDeploymentRepository(filepath.Join(root, "deployment"), repository))
	require.NoError(t, restarted.Close())
}

func TestRepositoryPinRejectsUnsafeFiles(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"writable", "symlink", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root, state := t.TempDir(), t.TempDir()
			writeCurrentDeployment(t, root, "v1.0.0")
			path := filepath.Join(state, repositoryFilename)
			switch mode {
			case "writable":
				require.NoError(t, os.WriteFile(path, []byte("block/proto-fleet\n"), 0o600))
				require.NoError(t, os.Chmod(path, 0o666)) //nolint:gosec // Deliberately unsafe fixture must be rejected.
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				require.NoError(t, os.WriteFile(target, []byte("block/proto-fleet\n"), 0o600))
				require.NoError(t, os.Symlink(target, path))
			case "duplicate":
				require.NoError(t, os.WriteFile(path, []byte("block/proto-fleet\nblock/proto-fleet\n"), 0o600))
			}
			_, err := NewManager(Config{InstallRoot: root, StateDir: state})
			require.ErrorContains(t, err, "repository")
		})
	}
}
