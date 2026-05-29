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
	allowed := []string{
		"/plugins/proto-plugin",
		"/plugins/antminer-plugin",
		"/plugins/leftover-plugin",
	}

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
	reapOrphans(psOutput, allowed, 100, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Len(t, killed, 1, "should reap only the ppid==1 orphan")
	assert.Equal(t, 999, killed[0])
}

func TestReapOrphans_SkipsSelfPID(t *testing.T) {
	t.Parallel()

	// Arrange
	psOutput := "100 1 /plugins/foo\n"
	allowed := []string{"/plugins/foo"}
	killFn := func(pid int, _ syscall.Signal) error {
		t.Fatalf("kill called for self pid %d", pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, allowed, 100, slog.New(slog.DiscardHandler), killFn)
}

func TestReapOrphans_KillsWhenParentExited(t *testing.T) {
	t.Parallel()

	// Arrange — child's ppid 777 is NOT in the alive set; kernel hasn't
	// reparented yet, so reap it anyway.
	psOutput := "500 777 /plugins/foo\n"
	allowed := []string{"/plugins/foo"}
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, allowed, 1, slog.New(slog.DiscardHandler), killFn)

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
			allowed := []string{"/plugins/foo"}
			killFn := func(pid int, _ syscall.Signal) error {
				t.Fatalf("kill must not be called for empty ps output (pid=%d)", pid)
				return nil
			}

			// Act
			reapOrphans(tc.out, allowed, 1, slog.New(slog.DiscardHandler), killFn)
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
	allowed := []string{"/plugins/first", "/plugins/second"}
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		if pid == 500 {
			return syscall.EPERM
		}
		return nil
	}

	// Act
	reapOrphans(psOutput, allowed, 1, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Equal(t, []int{500, 501}, killed, "reaper must attempt to kill every matching entry even when one fails")
}

func TestReapOrphans_SkipsUnknownProcessesUnderPluginsDir(t *testing.T) {
	t.Parallel()

	// Arrange — a process whose command is under the plugins dir but does
	// NOT match any allowed entry (e.g. a subdirectory invocation, or a
	// plugin file that has since been removed) must not be killed. Only
	// processes whose argv0 matches an installed plugin file are reaped.
	psOutput := strings.Join([]string{
		"500 1 /plugins/sub/foo",
		"501 1 /plugins/removed-plugin",
		"502 1 /plugins/direct-plugin",
		"",
	}, "\n")
	allowed := []string{"/plugins/direct-plugin"}
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, allowed, 1, slog.New(slog.DiscardHandler), killFn)

	// Assert
	require.Equal(t, []int{502}, killed)
}

func TestReapOrphans_PluginsPathWithSpaces(t *testing.T) {
	t.Parallel()

	// Arrange — plugin install path contains spaces. ps space-joins argv
	// without quoting, so naive argv0 extraction by splitting on the first
	// space would slice into the prefix. Matching against the exact
	// installed-plugin paths sidesteps the parsing problem entirely:
	// e.command must equal an allowed path, or start with an allowed path
	// followed by a space (its first argument).
	psOutput := strings.Join([]string{
		"500 1 /opt/Proto Fleet/plugins/proto-plugin arg1 arg2",
		"600 1 /opt/Proto Fleet/plugins/legit-plugin",
		"",
	}, "\n")
	allowed := []string{
		"/opt/Proto Fleet/plugins/proto-plugin",
		"/opt/Proto Fleet/plugins/legit-plugin",
	}
	var killed []int
	killFn := func(pid int, _ syscall.Signal) error {
		killed = append(killed, pid)
		return nil
	}

	// Act
	reapOrphans(psOutput, allowed, 1, slog.New(slog.DiscardHandler), killFn)

	// Assert — both genuine orphans are reaped even though their install
	// path contains whitespace.
	require.Equal(t, []int{500, 600}, killed, "space-containing install paths must still reap orphans by allowed-path matching")
}
