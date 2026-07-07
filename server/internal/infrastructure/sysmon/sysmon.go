// Package sysmon samples host CPU, memory, and disk usage in-process and
// emits them as fleet_system_* contract metrics for the optional
// system-monitoring feature.
package sysmon

import (
	"context"
	"log/slog"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

type Config struct {
	Enabled  bool          `help:"Collect host CPU/memory/disk gauges into the alerts metric store (requires FLEET_ALERTS_ENABLED)" default:"false" env:"ENABLED"`
	Interval time.Duration `help:"How often host stats are sampled" default:"30s" env:"INTERVAL"`
	DiskPath string        `help:"Filesystem path whose usage is reported; production mounts a sentinel volume on the docker-volumes filesystem read-only at /hostfs" default:"/" env:"DISK_PATH"`
}

// Emitter is the subset of *metrics.Provider the collector depends on.
type Emitter interface {
	EmitSystemCPUUsedPercent(ctx context.Context, percent float64)
	EmitSystemMemoryUsedPercent(ctx context.Context, percent float64)
	EmitSystemDiskUsedPercent(ctx context.Context, percent float64)
	EmitSystemHeartbeat(ctx context.Context)
}

// hostStats carries one tick's readings; nil means that read failed and the
// previous sample should stand rather than be clobbered by a bogus value.
type hostStats struct {
	cpuPercent  *float64
	memPercent  *float64
	diskPercent *float64
}

type Collector struct {
	cfg     Config
	emitter Emitter
	read    func(ctx context.Context, diskPath string) hostStats
}

// The Fleet Heartbeat Stale rule fires at 300s without a sample, so the
// interval must stay well under that no matter what an operator hand-sets:
// above the max, every gap between ticks would read as a fleet outage.
const (
	minInterval = 5 * time.Second
	maxInterval = time.Minute
)

func New(cfg Config, emitter Emitter) *Collector {
	if cfg.Interval < minInterval || cfg.Interval > maxInterval {
		clamped := min(max(cfg.Interval, minInterval), maxInterval)
		slog.Warn("sysmon: interval outside allowed range, clamping",
			"configured", cfg.Interval, "clamped", clamped)
		cfg.Interval = clamped
	}
	return &Collector{cfg: cfg, emitter: emitter, read: readHostStats}
}

// Run samples immediately — the heartbeat-staleness rule budgets for a fresh
// sample shortly after boot — and then on every tick until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	c.collectOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectOnce(ctx)
		}
	}
}

func (c *Collector) collectOnce(ctx context.Context) {
	stats := c.read(ctx, c.cfg.DiskPath)
	if stats.cpuPercent != nil {
		c.emitter.EmitSystemCPUUsedPercent(ctx, *stats.cpuPercent)
	}
	if stats.memPercent != nil {
		c.emitter.EmitSystemMemoryUsedPercent(ctx, *stats.memPercent)
	}
	if stats.diskPercent != nil {
		c.emitter.EmitSystemDiskUsedPercent(ctx, *stats.diskPercent)
	}
	// Heartbeat means "fleet-api and its metrics writer are alive", not
	// "host stats are readable" — emit it even when every read failed.
	c.emitter.EmitSystemHeartbeat(ctx)
}

func readHostStats(ctx context.Context, diskPath string) hostStats {
	var stats hostStats
	// interval=0 diffs against the previous call, so each tick reports
	// utilization over the last interval (since process start on the first).
	if percents, err := cpu.PercentWithContext(ctx, 0, false); err != nil || len(percents) == 0 {
		slog.Warn("sysmon: cpu read failed", "error", err)
	} else {
		stats.cpuPercent = &percents[0]
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err != nil {
		slog.Warn("sysmon: memory read failed", "error", err)
	} else {
		stats.memPercent = &vm.UsedPercent
	}
	if usage, err := disk.UsageWithContext(ctx, diskPath); err != nil {
		slog.Warn("sysmon: disk read failed", "path", diskPath, "error", err)
	} else {
		stats.diskPercent = &usage.UsedPercent
	}
	return stats
}
