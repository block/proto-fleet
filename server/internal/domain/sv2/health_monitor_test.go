package sv2

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthMonitor_InitialProbeUp(t *testing.T) {
	m := NewHealthMonitor("127.0.0.1:0", 50*time.Millisecond)
	var probes atomic.Int32
	m.dial = func(_ context.Context, _ string, _ time.Duration) error {
		probes.Add(1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Start(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool { return m.HasState() }, time.Second, 5*time.Millisecond,
		"initial probe must populate state before tick")
	assert.True(t, m.Up())

	cancel()
	<-done
	assert.GreaterOrEqual(t, probes.Load(), int32(1))
}

func TestHealthMonitor_Transitions(t *testing.T) {
	m := NewHealthMonitor("127.0.0.1:0", 10*time.Millisecond)
	var up atomic.Bool
	up.Store(true)
	m.dial = func(_ context.Context, _ string, _ time.Duration) error {
		if up.Load() {
			return nil
		}
		return errors.New("connection refused")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	require.Eventually(t, func() bool { return m.Up() }, time.Second, 5*time.Millisecond)

	up.Store(false)
	require.Eventually(t, func() bool { return !m.Up() }, time.Second, 5*time.Millisecond,
		"state should flip to down when probe fails")

	up.Store(true)
	require.Eventually(t, func() bool { return m.Up() }, time.Second, 5*time.Millisecond,
		"state should flip back to up when probe recovers")
}

func TestHealthMonitor_DisabledOnZeroInterval(t *testing.T) {
	m := NewHealthMonitor("127.0.0.1:0", 0)
	called := 0
	m.dial = func(_ context.Context, _ string, _ time.Duration) error {
		called++
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	m.Start(ctx) // returns immediately because interval <= 0

	assert.Equal(t, 0, called)
	assert.False(t, m.HasState())
}

func TestHealthMonitor_HonorsContextCancel(t *testing.T) {
	m := NewHealthMonitor("127.0.0.1:0", 5*time.Millisecond)
	m.dial = func(_ context.Context, _ string, _ time.Duration) error { return nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Start(ctx)
		close(done)
	}()
	time.Sleep(15 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start did not return after ctx cancel")
	}
}
