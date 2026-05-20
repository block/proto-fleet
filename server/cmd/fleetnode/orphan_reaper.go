//go:build !windows

package main

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// reapOrphanedPlugins kills any plugin-binary process whose executable lives
// in pluginsDir. The reaper runs before newPluginDiscoverer, so at this
// point the current agent has not yet spawned anything; every match is
// leftover from a prior run that didn't shut down cleanly (SIGKILL, panic,
// OOM, or the brief window between parent-death and kernel reparenting).
//
// An earlier version restricted reaping to processes whose ppid was 1, but
// that missed real strays whose parent zombie had not yet been collected,
// and contributed nothing because two concurrent agents on the same
// pluginsDir is already impossible (state.lock contention).
//
// Best-effort: silently skips on any error so a missing `ps` binary or
// permission failure never blocks normal startup.
func reapOrphanedPlugins(pluginsDir string, logger *slog.Logger) {
	abs, err := filepath.EvalSymlinks(pluginsDir)
	if err != nil {
		return
	}
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,command=").Output()
	if err != nil {
		logger.Debug("orphan reaper: ps failed; skipping", "err", err)
		return
	}
	selfPID := os.Getpid()
	prefix := abs + string(filepath.Separator)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		command := strings.Join(fields[2:], " ")
		if !strings.HasPrefix(command, prefix) {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == selfPID {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			logger.Warn("orphan reaper: kill failed", "pid", pid, "command", command, "err", err)
			continue
		}
		logger.Info("reaped stray plugin process", "pid", pid, "command", command)
	}
}
