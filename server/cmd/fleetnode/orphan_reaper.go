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

// Matching against os.ReadDir entries (rather than parsing ps argv) lets
// plugin paths containing spaces resolve correctly. The ppid filter avoids
// killing live plugins owned by another agent sharing the same pluginsDir
// under a different --state-dir.
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
	// /bin/ps explicit so a hostile $PATH can't inject a shim during startup.
	out, err := exec.CommandContext(psCtx, "/bin/ps", "-eo", "pid=,ppid=,command=").Output()
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

// Split from reapOrphanedPlugins so tests inject ps output and the kill
// function directly instead of forking.
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
		// Live non-init parent = another agent (or debugger) still owns this child.
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
