package runtimemetrics

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolScrape(t *testing.T) {
	p, err := Start(Config{Enabled: true, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Shutdown(context.Background())) })
	require.NoError(t, p.ObservePool("application", func() sql.DBStats {
		return sql.DBStats{OpenConnections: 4, InUse: 3, Idle: 1, MaxOpenConnections: 10, WaitCount: 7, WaitDuration: 1500 * time.Millisecond}
	}))
	observe, err := p.ConnectionObserver("application")
	require.NoError(t, err)
	observe(context.Background(), nil)
	observe(context.Background(), errors.New("private connection error"))
	body := scrapeMetrics(t, p)
	require.NotContains(t, body, "private connection error")
	require.Positive(t, runtimeStartTime(t, body))
	for _, line := range []string{
		`fleet_db_connection_attempts_total{pool="application",result="failure"} 1`,
		`fleet_db_connection_attempts_total{pool="application",result="success"} 1`,
		`fleet_db_pool_open_connections{pool="application"} 4`,
		`fleet_db_pool_in_use_connections{pool="application"} 3`,
		`fleet_db_pool_idle_connections{pool="application"} 1`,
		`fleet_db_pool_max_connections{pool="application"} 10`,
		`fleet_db_pool_waits_total{pool="application"} 7`,
		`fleet_db_pool_wait_seconds_total{pool="application"} 1.5`,
	} {
		require.Contains(t, body, line)
	}
}

func TestNodeConnectionScrape(t *testing.T) {
	p, err := Start(Config{Enabled: true, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Shutdown(context.Background())) })
	var count atomic.Int64
	require.NoError(t, p.ObserveNodeConnections(count.Load))
	require.Contains(t, scrapeMetrics(t, p), "\nfleet_node_control_streams 0\n")
	count.Store(3)
	require.Contains(t, scrapeMetrics(t, p), "\nfleet_node_control_streams 3\n")
	count.Store(0)
	require.Contains(t, scrapeMetrics(t, p), "\nfleet_node_control_streams 0\n")
}

func TestCommandMetricsScrape(t *testing.T) {
	p, err := Start(Config{Enabled: true, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Shutdown(context.Background())) })
	commands, err := p.NewCommandMetrics()
	require.NoError(t, err)
	dispatch, err := p.DispatchObserver()
	require.NoError(t, err)
	ctx := t.Context()
	queuedAt := time.Now().Add(-time.Minute)
	commands.CommandStarted(ctx, "reboot", "control", queuedAt)
	commands.CommandStarted(ctx, "reboot", "control", queuedAt)
	commands.CommandStarted(ctx, "reboot", "control", time.Time{})
	commands.CommandStarted(ctx, "reboot", "control", time.Now().Add(time.Hour))
	commands.CommandAttemptFinished(ctx, "reboot", "control", "timeout", time.Second)
	commands.CommandCommitted(ctx, "reboot", "control", "reaped", time.Time{})
	commands.CommandPersistenceFailed(ctx, "control")
	dispatch(ctx, "reboot", "control", "send_failed", time.Second)

	body := scrapeMetrics(t, p)
	for _, sample := range []string{
		`fleet_command_queue_wait_seconds_count{command_class="control",kind="reboot"} 2`,
		`fleet_command_attempt_duration_seconds_count{command_class="control",kind="reboot",result="timeout"} 1`,
		`fleet_command_dispatch_send_seconds_count{admission_class="control",kind="reboot",result="send_failed"} 1`,
		`fleet_command_committed_outcomes_total{command_class="control",kind="reboot",result="reaped"} 1`,
		`fleet_command_persistence_failures_total{command_class="control"} 1`,
	} {
		require.Contains(t, body, sample)
	}
	for _, removed := range []string{"fleet_command_attempts_total", "fleet_command_attempt_outcomes_total", "fleet_command_dispatch_outcomes_total"} {
		require.NotContains(t, body, removed)
	}
}

func TestStartupScrape(t *testing.T) {
	beforeStart := time.Now().Unix()
	p, err := Start(Config{Enabled: true, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Shutdown(context.Background())) })
	afterStart := time.Now().Unix()
	_, err = p.ConnectionObserver("application")
	require.NoError(t, err)

	body := scrapeMetrics(t, p)
	started := runtimeStartTime(t, body)
	require.GreaterOrEqual(t, started, float64(beforeStart))
	require.LessOrEqual(t, started, float64(afterStart))
	require.NotContains(t, body, "fleet_db_pool_open_connections")
	for _, result := range []string{"success", "failure"} {
		require.Contains(t, body, `fleet_db_connection_attempts_total{pool="application",result="`+result+`"} 0`)
	}

	for _, pool := range []string{"application", "secondary"} {
		require.NoError(t, p.ObservePool(pool, func() sql.DBStats { return sql.DBStats{} }))
	}
	body = scrapeMetrics(t, p)
	require.Equal(t, started, runtimeStartTime(t, body))
	require.Equal(t, 1, strings.Count(body, "\nfleet_runtime_start_time_seconds "))
}

func scrapeMetrics(t *testing.T, p *Provider) string {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + p.address + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return string(body)
}

func runtimeStartTime(t *testing.T, body string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if value, ok := strings.CutPrefix(line, "fleet_runtime_start_time_seconds "); ok {
			started, err := strconv.ParseFloat(value, 64)
			require.NoError(t, err)
			return started
		}
	}
	t.Fatal("runtime start time is missing from metrics")
	return 0
}

func TestDisabledDoesNotCollect(t *testing.T) {
	p, err := Start(Config{})
	require.NoError(t, err)
	require.NoError(t, p.ObservePool("application", func() sql.DBStats { t.Fatal("disabled metrics collected"); return sql.DBStats{} }))
	require.Empty(t, p.address)
	require.NoError(t, p.Shutdown(context.Background()))
}

func TestRejectPublicMetricsListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:9090", "[::]:9090", "192.0.2.1:9090", "localhost:9090", "bad"} {
		_, err := Start(Config{Enabled: true, Address: address})
		require.Error(t, err)
	}
}

func TestMetricsBindFailure(t *testing.T) {
	p, err := Start(Config{Enabled: true, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Shutdown(context.Background())) })
	_, err = Start(Config{Enabled: true, Address: p.address})
	require.ErrorContains(t, err, "listen for runtime metrics")
}
