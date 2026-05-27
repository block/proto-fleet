//go:build !windows

package main

import (
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReapOrphans_SkipsLivePluginsOfOtherAgents(t *testing.T) {
	t.Parallel()

	// Arrange — two parent agents (pids 100 and 200), each owns a plugin
	// under the same plugins dir. Pid 999 is a true orphan reparented to
	// init.
	psOutput := strings.Join([]string{
		"100 1 fleetnode run",
		"200 1 fleetnode run --state-dir /alt",
		"500 100 /plugins/proto-plugin",
		"600 200 /plugins/antminer-plugin",
		"999 1 /plugins/leftover-plugin",
		"",
	}, "\n")

	var (
		mu     sync.Mutex
		killed []int
	)
	killFn := func(pid int, _ syscall.Signal) error {
		mu.Lock()
		defer mu.Unlock()
		killed = append(killed, pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, "/plugins", 100, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Len(t, killed, 1, "should reap only the ppid==1 orphan")
	assert.Equal(t, 999, killed[0])
}

func TestReapOrphans_SkipsSelfPID(t *testing.T) {
	t.Parallel()

	// Arrange
	psOutput := "100 1 /plugins/foo\n"
	killFn := func(pid int, _ syscall.Signal) error {
		t.Fatalf("kill called for self pid %d", pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, "/plugins", 100, slog.New(slog.DiscardHandler), killFn)
}

func TestReapOrphans_KillsWhenParentExited(t *testing.T) {
	t.Parallel()

	// Arrange — child's ppid 777 is NOT in the alive set; kernel hasn't
	// reparented yet, so reap it anyway.
	psOutput := "500 777 /plugins/foo\n"
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, "/plugins", 1, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Equal(t, []int{500}, killed)
}

func TestReapOrphans_EmptyPsOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
	}{
		{name: "empty", out: ""},
		{name: "blank lines", out: "\n\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			killFn := func(pid int, _ syscall.Signal) error {
				t.Fatalf("kill must not be called for empty ps output (pid=%d)", pid)
				return nil
			}

			// Act
			reapOrphans(tc.out, "/plugins", 1, slog.New(slog.DiscardHandler), killFn)
		})
	}
}

func TestReapOrphans_ContinuesAfterKillFailure(t *testing.T) {
	t.Parallel()

	// Arrange — two orphans; the first kill returns an error. The second
	// orphan still has to be reaped so a stuck kill on pid A doesn't block
	// cleanup of pid B.
	psOutput := strings.Join([]string{
		"500 1 /plugins/first",
		"501 1 /plugins/second",
		"",
	}, "\n")
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		if pid == 500 {
			return syscall.EPERM
		}
		return nil
	}

	// Act
	reapOrphans(psOutput, "/plugins", 1, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Equal(t, []int{500, 501}, killed, "reaper must attempt to kill every matching entry even when one fails")
}

func TestReapOrphans_SkipsSubdirectoryProcesses(t *testing.T) {
	t.Parallel()

	// Arrange — a process invoked from a subdirectory of the plugins dir
	// must not be killed; only direct children of pluginsAbs are plugins.
	psOutput := strings.Join([]string{
		"500 1 /plugins/sub/foo",
		"501 1 /plugins/direct-plugin",
		"",
	}, "\n")
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, "/plugins", 1, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Equal(t, []int{501}, killed)
}
