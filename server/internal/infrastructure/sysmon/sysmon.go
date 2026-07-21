// Package sysmon samples host CPU, memory, and disk usage in-process and
// emits them as fleet_system_* contract metrics for the optional
// system-monitoring feature.
package sysmon

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
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

// The read funcs return nil when a read failed: the previous sample should
// stand rather than be clobbered by a bogus value.
type Collector struct {
	cfg      Config
	emitter  Emitter
	readCPU  func(ctx context.Context) *float64
	readMem  func(ctx context.Context) *float64
	readDisk func(ctx context.Context, diskPath string) *float64

	cpuBusy  atomic.Bool
	memBusy  atomic.Bool
	diskBusy atomic.Bool
	probes   sync.WaitGroup
}

// The Fleet Heartbeat Stale rule's staleness threshold is 120s, so the
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
	return &Collector{
		cfg:      cfg,
		emitter:  emitter,
		readCPU:  readCPU,
		readMem:  readMem,
		readDisk: readDisk,
	}
}

// Run samples immediately — the heartbeat-staleness rule budgets for a fresh
// sample shortly after boot — and then on every tick until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	defer c.probes.Wait()
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

// collectOnce emits the heartbeat synchronously and gathers each host gauge
// in its own single-flight goroutine. Heartbeat means "fleet-api and its
// metrics writer are alive", not "host stats are readable" — a statfs wedged
// on a dead mount must not stop it, and per-probe isolation keeps a hung
// disk read from also blinding the CPU and memory gauges: only the disk
// series goes stale, which is exactly what the Host Disk Monitoring Stalled
// rule reports.
func (c *Collector) collectOnce(ctx context.Context) {
	c.emitter.EmitSystemHeartbeat(ctx)
	c.launch(&c.cpuBusy, "cpu", func() {
		if v := c.readCPU(ctx); v != nil {
			c.emitter.EmitSystemCPUUsedPercent(ctx, *v)
		}
	})
	c.launch(&c.memBusy, "memory", func() {
		if v := c.readMem(ctx); v != nil {
			c.emitter.EmitSystemMemoryUsedPercent(ctx, *v)
		}
	})
	c.launch(&c.diskBusy, "disk", func() {
		if v := c.readDisk(ctx, c.cfg.DiskPath); v != nil {
			c.emitter.EmitSystemDiskUsedPercent(ctx, *v)
		}
	})
}

func (c *Collector) launch(busy *atomic.Bool, probe string, work func()) {
	if !busy.CompareAndSwap(false, true) {
		slog.Warn("sysmon: previous read still in flight, skipping this tick", "probe", probe)
		return
	}
	c.probes.Go(func() {
		defer busy.Store(false)
		work()
	})
}

func readCPU(ctx context.Context) *float64 {
	// interval=0 diffs against the previous call, so each tick reports
	// utilization over the last interval (since process start on the first).
	percents, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil || len(percents) == 0 {
		slog.Warn("sysmon: cpu read failed", "error", err)
		return nil
	}
	return &percents[0]
}

func readMem(ctx context.Context) *float64 {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		slog.Warn("sysmon: memory read failed", "error", err)
		return nil
	}
	return &vm.UsedPercent
}

func readDisk(ctx context.Context, diskPath string) *float64 {
	usage, err := disk.UsageWithContext(ctx, diskPath)
	if err != nil {
		slog.Warn("sysmon: disk read failed", "path", diskPath, "error", err)
		return nil
	}
	return &usage.UsedPercent
}
