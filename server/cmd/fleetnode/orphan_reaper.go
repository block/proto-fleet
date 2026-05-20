//go:build !windows

package main

import (
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// reapOrphanedPlugins kills plugin-binary processes whose original parent
// died and were re-parented to init (ppid == 1). Without this, a SIGKILL
// on the agent (or any other defer-skipping exit) leaves go-plugin
// children running, occupying their sockets and eventually blocking the
// next agent's plugin handshake.
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
	prefix := abs + string(filepath.Separator)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid != 1 {
			continue
		}
		command := strings.Join(fields[2:], " ")
		if !strings.HasPrefix(command, prefix) {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			logger.Warn("orphan reaper: kill failed", "pid", pid, "command", command, "err", err)
			continue
		}
		logger.Info("reaped orphaned plugin process", "pid", pid, "command", command)
	}
}
