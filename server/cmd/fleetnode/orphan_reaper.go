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

// reapOrphanedPlugins kills plugin-binary processes whose argv0 matches an
// installed plugin under pluginsDir and whose parent is no longer alive
// (true orphans). Runs before newPluginDiscoverer and under the state lock,
// so any match left after the ppid filter is leftover from a prior crash.
// Best-effort: a missing/broken ps never blocks startup.
//
// pluginsDir must be the same string the loader passes to exec.Command so
// argv0 in ps matches the paths enumerated here. resolvePluginsDir rejects
// a symlinked leaf, but path components above can still be symlinks --
// using the unresolved path keeps loader and reaper aligned in either case.
//
// We match against os.ReadDir entries (the actual plugin filenames) rather
// than splitting ps's space-joined argv on spaces; that way install paths
// containing spaces ("/opt/Proto Fleet/plugins/...") match correctly.
//
// The ppid check matters when two agents share a binary/plugins layout but
// hold different state locks (e.g. --state-dir variants). Without it, agent
// B's startup would kill agent A's live plugin children.
func reapOrphanedPlugins(ctx context.Context, pluginsDir string, logger *slog.Logger) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		logger.Debug("orphan reaper: read plugins dir failed; skipping", "plugins_dir", pluginsDir, "err", err)
		return
	}
	allowed := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		allowed = append(allowed, filepath.Join(pluginsDir, e.Name()))
	}
	if len(allowed) == 0 {
		return
	}
	psCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(psCtx, "ps", "-eo", "pid=,ppid=,command=").Output()
	if err != nil {
		logger.Debug("orphan reaper: ps failed; skipping", "err", err)
		return
	}
	reapOrphans(string(out), allowed, os.Getpid(), logger, syscall.Kill)
}

type psEntry struct {
	pid     int
	ppid    int
	command string
}

// reapOrphans is split out for testability: callers inject the ps output,
// the set of allowed plugin paths, the running self pid, and the kill
// function. Matching against allowed paths sidesteps ps's space-joined argv
// representation -- an entry matches when e.command is exactly an allowed
// path, or an allowed path followed by a space (its first argument).
func reapOrphans(psOutput string, allowed []string, selfPID int, logger *slog.Logger, killFn func(pid int, sig syscall.Signal) error) {
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
		matchedPath := ""
		for _, path := range allowed {
			if e.command == path || strings.HasPrefix(e.command, path+" ") {
				matchedPath = path
				break
			}
		}
		if matchedPath == "" {
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
		logger.Info("reaped stray plugin process", "pid", e.pid, "plugin", matchedPath)
	}
}
