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

// reapOrphanedPlugins kills any plugin-binary process under pluginsDir.
// Runs before newPluginDiscoverer and under the state lock, so any match is
// leftover from a prior crash; concurrent agents are already excluded by
// state.lock contention. Best-effort: a missing/broken ps never blocks
// startup.
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
