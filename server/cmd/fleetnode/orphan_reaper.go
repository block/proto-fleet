//go:build !windows

package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// reapOrphanedPlugins kills plugin-binary processes under pluginsDir whose
// parent is no longer alive (true orphans). Runs before newPluginDiscoverer
// and under the state lock, so any match left after the ppid filter is
// leftover from a prior crash. Best-effort: a missing/broken ps never blocks
// startup.
//
// The ppid check matters when two agents share a binary/plugins layout but
// hold different state locks (e.g. --state-dir variants). Without it, agent
// B's startup would kill agent A's live plugin children.
func reapOrphanedPlugins(ctx context.Context, pluginsDir string, logger *slog.Logger) {
	abs, err := filepath.EvalSymlinks(pluginsDir)
	if err != nil {
		logger.Debug("orphan reaper: resolve symlinks failed; skipping", "plugins_dir", pluginsDir, "err", err)
		return
	}
	psCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(psCtx, "ps", "-eo", "pid=,ppid=,command=").Output()
	if err != nil {
		logger.Debug("orphan reaper: ps failed; skipping", "err", err)
		return
	}
	reapOrphans(string(out), abs, os.Getpid(), logger, syscall.Kill)
}

type psEntry struct {
	pid     int
	ppid    int
	command string
}

// reapOrphans is split out for testability: callers inject the ps output,
// the running self pid, and the kill function.
func reapOrphans(psOutput, pluginsAbs string, selfPID int, logger *slog.Logger, killFn func(pid int, sig syscall.Signal) error) {
	prefix := pluginsAbs + string(filepath.Separator)
	var entries []psEntry
	alive := make(map[int]bool)
	for _, line := range strings.Split(psOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		alive[pid] = true
		entries = append(entries, psEntry{pid: pid, ppid: ppid, command: strings.Join(fields[2:], " ")})
	}
	for _, e := range entries {
		if e.pid == selfPID {
			continue
		}
		if !strings.HasPrefix(e.command, prefix) {
			continue
		}
		// Only kill direct children of pluginsAbs so an operator process
		// invoked from a subdirectory under the prefix isn't reaped.
		argv0 := e.command
		if i := strings.IndexByte(argv0, ' '); i >= 0 {
			argv0 = argv0[:i]
		}
		if strings.ContainsRune(argv0[len(prefix):], filepath.Separator) {
			continue
		}
		// Skip if the parent is still alive and not init/launchd. A live
		// non-init parent means another agent (or a debugger) still owns
		// this child.
		if e.ppid > 1 && alive[e.ppid] {
			logger.Debug("orphan reaper: parent still alive; skipping", "pid", e.pid, "ppid", e.ppid)
			continue
		}
		if err := killFn(e.pid, syscall.SIGKILL); err != nil {
			logger.Warn("orphan reaper: kill failed", "pid", e.pid, "command", e.command, "err", err)
			continue
		}
		logger.Info("reaped stray plugin process", "pid", e.pid, "command", e.command)
	}
}
