package sysmon

import (
	"context"
	"sync"
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

func ptr(v float64) *float64 { return &v }

func TestCollectOnceEmitsAllGauges(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: 30 * time.Second, DiskPath: "/data"}, emitter)
	var gotPath string
	collector.read = func(_ context.Context, diskPath string) hostStats {
		gotPath = diskPath
		return hostStats{cpuPercent: ptr(12.5), memPercent: ptr(40), diskPercent: ptr(63)}
	}

	// Act
	collector.collectOnce(context.Background())

	// Assert
	require.Equal(t, "/data", gotPath)
	require.Equal(t, []float64{12.5}, emitter.cpu)
	require.Equal(t, []float64{40}, emitter.mem)
	require.Equal(t, []float64{63}, emitter.disk)
	require.Equal(t, 1, emitter.heartbeats)
}

func TestCollectOnceEmitsHeartbeatWhenReadsFail(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: 30 * time.Second, DiskPath: "/"}, emitter)
	collector.read = func(context.Context, string) hostStats { return hostStats{} }

	// Act
	collector.collectOnce(context.Background())

	// Assert
	require.Empty(t, emitter.cpu)
	require.Empty(t, emitter.mem)
	require.Empty(t, emitter.disk)
	require.Equal(t, 1, emitter.heartbeats)
}

func TestRunEmitsImmediatelyAndStopsOnCancel(t *testing.T) {
	// Arrange
	emitter := &fakeEmitter{}
	collector := New(Config{Interval: time.Hour, DiskPath: "/"}, emitter)
	collector.read = func(context.Context, string) hostStats { return hostStats{} }
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

func TestNewClampsIntervalToAllowedRange(t *testing.T) {
	// Act
	short := New(Config{Interval: time.Millisecond}, &fakeEmitter{})
	long := New(Config{Interval: time.Hour}, &fakeEmitter{})

	// Assert
	require.Equal(t, minInterval, short.cfg.Interval)
	require.Equal(t, maxInterval, long.cfg.Interval)
}
