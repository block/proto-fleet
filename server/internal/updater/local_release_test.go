package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/block/proto-fleet/server/internal/updaterapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalReleaseUsesExistingVerificationWithoutNetwork(t *testing.T) {
	for _, test := range []struct {
		name, version, repository, wantError string
		missing, corrupt                     bool
	}{
		{name: "valid", version: "v1.1.0"},
		{name: "missing", missing: true, wantError: "no such file"},
		{name: "corrupt", version: "v1.1.0", corrupt: true, wantError: "checksum verification failed"},
		{name: "wrong version", version: "v1.2.0", wantError: "version"},
		{name: "wrong repository", version: "v1.1.0", repository: "other-owner/fleet", wantError: "repository"},
	} {
		t.Run(test.name, func(t *testing.T) {
			installRoot := t.TempDir()
			writeCurrentDeployment(t, installRoot, "v1.0.0")
			releaseDir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			if !test.missing {
				bundle := releaseBundle(t, test.version)
				if test.repository != "" {
					bundle = releaseBundle(t, test.version, test.repository)
				}
				versionDir := filepath.Join(releaseDir, "v1.1.0")
				require.NoError(t, os.Mkdir(versionDir, 0o700))
				name := "proto-fleet-v1.1.0-amd64.tar.gz"
				checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(bundle), name)
				if test.corrupt {
					bundle = append(bundle, 'x')
				}
				require.NoError(t, os.WriteFile(filepath.Join(versionDir, name), bundle, 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(versionDir, name+".sha256"), []byte(checksum), 0o600))
			}
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			runner := &recordingRunner{}
			manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) { cfg.ReleaseDir = releaseDir })
			_, err = manager.Trigger("v1.1.0")
			require.NoError(t, err)
			completed := waitForTerminal(t, manager)
			assert.Zero(t, requests.Load())
			if test.wantError == "" {
				require.Equal(t, updaterapi.PhaseSucceeded, completed.Phase, completed.Error)
			} else {
				require.Equal(t, updaterapi.PhaseFailed, completed.Phase)
				assert.Contains(t, completed.Error, test.wantError)
				assert.Empty(t, runner.Commands())
				assert.Equal(t, "v1.0.0", mustReadVersion(t, filepath.Join(installRoot, "deployment", "version.txt")))
			}
		})
	}
}

func TestLocalReleaseRejectsUnsafeSources(t *testing.T) {
	for _, kind := range []string{"file symlink", "relative file symlink", "directory symlink", "writable file", "writable directory", "oversize", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			directory := filepath.Join(root, "releases")
			require.NoError(t, os.Mkdir(directory, 0o700))
			source := filepath.Join(directory, "bundle")
			require.NoError(t, os.WriteFile(source, []byte("bundle"), 0o600))
			ctx := context.Background()
			limit := int64(100)
			switch kind {
			case "file symlink":
				require.NoError(t, os.Rename(source, source+".real"))
				require.NoError(t, os.Symlink(source+".real", source))
			case "relative file symlink":
				require.NoError(t, os.Rename(source, source+".real"))
				require.NoError(t, os.Symlink(filepath.Base(source)+".real", source))
			case "directory symlink":
				require.NoError(t, os.Rename(directory, directory+".real"))
				require.NoError(t, os.Symlink(directory+".real", directory))
			case "writable file":
				require.NoError(t, os.Chmod(source, 0o666)) //nolint:gosec // Deliberately unsafe fixture must be rejected.
			case "writable directory":
				require.NoError(t, os.Chmod(directory, 0o777)) //nolint:gosec // Deliberately unsafe fixture must be rejected.
			case "oversize":
				limit = 1
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = copyLocalRelease(ctx, source, filepath.Join(root, "copy"), limit)
			require.Error(t, err)
		})
	}
}

func TestLocalReleaseCopyIsProtectedAndIndependent(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	source := filepath.Join(root, "bundle")
	destination := filepath.Join(root, "copy")
	require.NoError(t, os.WriteFile(source, []byte("original"), 0o600))
	require.NoError(t, copyLocalRelease(context.Background(), source, destination, 100))
	require.NoError(t, os.WriteFile(source, []byte("replaced"), 0o600))
	assert.Equal(t, "original", mustReadFile(t, destination))
	info, err := os.Stat(destination)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	// An existing protected artifact is never overwritten.
	require.Error(t, copyLocalRelease(context.Background(), source, destination, 100))
	assert.Equal(t, "original", mustReadFile(t, destination))
}
