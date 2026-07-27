package sysmon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeEmitter struct {
	mu         sync.Mutex
	cpu        []float64
	mem        []float64
	disk       []float64
	heartbeats int
}

func (f *fakeEmitter) EmitSystemCPUUsedPercent(_ context.Context, percent float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cpu = append(f.cpu, percent)
}

func (f *fakeEmitter) EmitSystemMemoryUsedPercent(_ context.Context, percent float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mem = append(f.mem, percent)
}

func (f *fakeEmitter) EmitSystemDiskUsedPercent(_ context.Context, percent float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disk = append(f.disk, percent)
}

func (f *fakeEmitter) EmitSystemHeartbeat(_ context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats++
}

func (f *fakeEmitter) heartbeatCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.heartbeats
}

func (f *fakeEmitter) gauges() (cpu, mem, disk []float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]float64{}, f.cpu...), append([]float64{}, f.mem...), append([]float64{}, f.disk...)
}

func ptr(v float64) *float64 { return &v }

func TestCollectOnceEmitsAllGauges(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: 30 * time.Second, DiskPath: "/data"}, emitter)
	var gotPath atomic.Value
	collector.readCPU = func(context.Context) *float64 { return ptr(12.5) }
	collector.readMem = func(context.Context) *float64 { return ptr(40) }
	collector.readDisk = func(_ context.Context, diskPath string) *float64 {
		gotPath.Store(diskPath)
		return ptr(63)
	}

	// Act
	collector.collectOnce(context.Background())

	// Assert
	require.Equal(t, 1, emitter.heartbeatCount())
	require.Eventually(t, func() bool {
		cpu, mem, disk := emitter.gauges()
		return len(cpu) == 1 && len(mem) == 1 && len(disk) == 1
	}, time.Second, 5*time.Millisecond)
	cpu, mem, disk := emitter.gauges()
	require.Equal(t, []float64{12.5}, cpu)
	require.Equal(t, []float64{40}, mem)
	require.Equal(t, []float64{63}, disk)
	require.Equal(t, "/data", gotPath.Load())
}

func TestCollectOnceEmitsHeartbeatWhenReadsFail(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: 30 * time.Second, DiskPath: "/"}, emitter)
	collector.readCPU = func(context.Context) *float64 { return nil }
	collector.readMem = func(context.Context) *float64 { return nil }
	collector.readDisk = func(context.Context, string) *float64 { return nil }

	// Act
	collector.collectOnce(context.Background())

	// Assert
	require.Equal(t, 1, emitter.heartbeatCount())
	require.Eventually(t, func() bool {
		return !collector.cpuBusy.Load() && !collector.memBusy.Load() && !collector.diskBusy.Load()
	}, time.Second, 5*time.Millisecond)
	cpu, mem, disk := emitter.gauges()
	require.Empty(t, cpu)
	require.Empty(t, mem)
	require.Empty(t, disk)
}

func TestHungDiskReadBlocksOnlyTheDiskProbe(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: 30 * time.Second, DiskPath: "/"}, emitter)
	release := make(chan struct{})
	var diskReads atomic.Int32
	collector.readCPU = func(context.Context) *float64 { return ptr(10) }
	collector.readMem = func(context.Context) *float64 { return ptr(20) }
	collector.readDisk = func(context.Context, string) *float64 {
		diskReads.Add(1)
		<-release
		return ptr(63)
	}

	// Act
	collector.collectOnce(context.Background())
	require.Eventually(t, func() bool { return diskReads.Load() == 1 }, time.Second, 5*time.Millisecond)
	collector.collectOnce(context.Background())

	// Assert
	require.Equal(t, 2, emitter.heartbeatCount(), "heartbeats must keep flowing while a read hangs")
	require.Eventually(t, func() bool {
		cpu, mem, _ := emitter.gauges()
		return len(cpu) == 2 && len(mem) == 2
	}, time.Second, 5*time.Millisecond, "a hung disk read must not blind the CPU and memory gauges")
	require.Equal(t, int32(1), diskReads.Load(), "hung disk read must be single-flight, not stacked")
	close(release)
	require.Eventually(t, func() bool {
		_, _, disk := emitter.gauges()
		return len(disk) == 1
	}, time.Second, 5*time.Millisecond)
}

func TestRunEmitsImmediatelyAndStopsOnCancel(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: time.Hour, DiskPath: "/"}, emitter)
	collector.readCPU = func(context.Context) *float64 { return nil }
	collector.readMem = func(context.Context) *float64 { return nil }
	collector.readDisk = func(context.Context, string) *float64 { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	// Act
	go func() {
		collector.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool { return emitter.heartbeatCount() == 1 },
		time.Second, 5*time.Millisecond, "Run should collect once before the first tick")
	cancel()

	// Assert
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunWaitsForInFlightProbes(t *testing.T) {
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: time.Hour, DiskPath: "/"}, emitter)
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	collector.readCPU = func(context.Context) *float64 { return nil }
	collector.readMem = func(context.Context) *float64 { return nil }
	collector.readDisk = func(context.Context, string) *float64 {
		close(probeStarted)
		<-releaseProbe
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		collector.Run(ctx)
		close(done)
	}()

	select {
	case <-probeStarted:
	case <-time.After(time.Second):
		t.Fatal("disk probe did not start")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("Run returned before its in-flight probe drained")
	case <-time.After(10 * time.Millisecond):
	}

	close(releaseProbe)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after its in-flight probe drained")
	}
}

func TestNewClampsIntervalToAllowedRange(t *testing.T) {
	// Act
	short := New(Config{Interval: time.Millisecond}, &fakeEmitter{})
	long := New(Config{Interval: time.Hour}, &fakeEmitter{})

	// Assert
	require.Equal(t, minInterval, short.cfg.Interval)
	require.Equal(t, maxInterval, long.cfg.Interval)
}
