package sv2

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// HealthMonitor runs a background TCP probe against the bundled
// translator proxy on the configured interval and exposes the last-known
// up/down state to callers. It logs only on transitions so steady-state
// noise stays out of logs.
//
// The monitor is intentionally narrow — it does not push status into the
// activity log, and does not block pool-assignment decisions. Pool
// assignment always runs the synchronous rewriter against the proxy
// config itself (ProxyEnabled + MinerURL); the health monitor exists so
// operators can see at a glance whether the proxy they enabled is
// actually listening.
type HealthMonitor struct {
	addr     string
	interval time.Duration
	dial     func(ctx context.Context, addr string, timeout time.Duration) error
	logger   *slog.Logger

	up       atomic.Bool
	hasState atomic.Bool // true after the first probe completes
}

// NewHealthMonitor builds a monitor against the provided TCP host:port.
// interval must be > 0; use cfg.ProxyHealthInterval from sv2.Config for
// the standard deployment-configured value.
func NewHealthMonitor(addr string, interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		addr:     addr,
		interval: interval,
		dial:     dialTCP,
		logger:   slog.Default(),
	}
}

// Start blocks on the given context. When ctx is canceled the probe loop
// returns and the final state is preserved (Up() still reports the last
// observation). Intended to be launched in a goroutine from main.go.
//
// Runs one probe immediately so the initial state is populated before
// the first tick fires — callers that check Up() right after Start()
// returns see a real value rather than the default false.
func (m *HealthMonitor) Start(ctx context.Context) {
	if m.interval <= 0 {
		m.logger.Warn("sv2 proxy health monitor: disabled (interval <= 0)", "addr", m.addr)
		return
	}

	m.probe(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.probe(ctx)
		}
	}
}

// Up reports the last-known probe outcome. Returns false before the
// first probe completes; callers that want to distinguish "down" from
// "haven't probed yet" should consult HasState.
func (m *HealthMonitor) Up() bool       { return m.up.Load() }
func (m *HealthMonitor) HasState() bool { return m.hasState.Load() }

func (m *HealthMonitor) probe(ctx context.Context) {
	err := m.dial(ctx, m.addr, DefaultTCPDialTimeout)
	newState := err == nil

	oldState := m.up.Load()
	m.up.Store(newState)

	if !m.hasState.Swap(true) {
		// First observation — log as "initial up/down" rather than a
		// transition so operators know the monitor is live.
		if newState {
			m.logger.Info("sv2 proxy health: initial up", "addr", m.addr)
		} else {
			m.logger.Warn("sv2 proxy health: initial down", "addr", m.addr, "error", err)
		}
		return
	}

	if oldState == newState {
		return
	}
	if newState {
		m.logger.Info("sv2 proxy health: up", "addr", m.addr)
	} else {
		m.logger.Warn("sv2 proxy health: down", "addr", m.addr, "error", err)
	}
}
