//go:build linux

package updater

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/block/proto-fleet/server/internal/updaterapi"
	"github.com/stretchr/testify/require"
)

// execve fails with ETXTBSY while any descriptor holds the file open for writing.
type writerDetectingRunner struct {
	*haRecordingRunner
	mu      sync.Mutex
	writers []string
}

func (r *writerDetectingRunner) Run(ctx context.Context, dir string, output io.Writer, name string, args ...string) error {
	if len(args) == 1 && args[0] == "--version" {
		writers := openWriters(name)
		r.mu.Lock()
		r.writers = append(r.writers, writers...)
		r.mu.Unlock()
	}
	return r.haRecordingRunner.Run(ctx, dir, output, name, args...)
}

func openWriters(path string) []string {
	var writers []string
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return []string{fmt.Sprintf("read /proc/self/fd: %v", err)}
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil || target != path {
			continue
		}
		info, err := os.ReadFile(filepath.Join("/proc/self/fdinfo", entry.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(info), "\n") {
			value, ok := strings.CutPrefix(line, "flags:")
			if !ok {
				continue
			}
			flags, err := strconv.ParseUint(strings.TrimSpace(value), 8, 64)
			if err == nil && flags&syscall.O_ACCMODE != syscall.O_RDONLY {
				writers = append(writers, fmt.Sprintf("fd %s flags %o", entry.Name(), flags))
			}
		}
	}
	return writers
}

func TestPreparedHAUpdateValidatesUpdaterWithoutOpenWriters(t *testing.T) {
	// Arrange
	installRoot := t.TempDir()
	writeCurrentDeployment(t, installRoot, "v1.0.0")
	server := releaseServer(t, "v1.1.0", "amd64", releaseBundle(t, "v1.1.0"), "")
	runner := &writerDetectingRunner{haRecordingRunner: &haRecordingRunner{}}
	installedUpdater := filepath.Join(t.TempDir(), "proto-fleet-updater")
	require.NoError(t, os.WriteFile(installedUpdater, []byte("old updater"), 0o755))
	manager := newTestManagerWithConfig(t, installRoot, server, runner, func(cfg *Config) {
		cfg.DeploymentMode = DeploymentModeHA
		cfg.SelfUpdatePath = installedUpdater
	})

	// Act
	_, err := manager.TriggerPreparedWithID("v1.1.0", "11111111-1111-4111-8111-111111111111", true, false, false)
	require.NoError(t, err)
	result := waitPreparedOperation(t, manager)

	// Assert
	require.Equal(t, updaterapi.PhaseSucceeded, result.Phase, result.Error)
	runner.mu.Lock()
	defer runner.mu.Unlock()
	require.Empty(t, runner.writers)
	require.FileExists(t, filepath.Join(installRoot, preparedDirectoryName, preparedUpdaterFilename))
}
