package runtimemetrics

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNodeMetricsWithoutDatabase(t *testing.T) {
	p, err := Start(Config{Enabled: true, Address: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Shutdown(context.Background())) })
	m, err := p.NewNodeMetrics()
	require.NoError(t, err)
	m.Started(t.Context(), "telemetry", "deferrable_read")
	m.Finished(t.Context(), "telemetry", "deferrable_read", "success", time.Second)
	m.AuthOutcome(t.Context(), "session", "session_rejected")
	m.ProtocolCheck(false)
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + p.address + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	for _, line := range []string{
		`fleet_runtime_start_time_seconds`,
		`fleet_node_operation_attempts_total{admission_class="deferrable_read",kind="telemetry"} 1`,
		`fleet_node_operation_outcomes_total{admission_class="deferrable_read",kind="telemetry",result="success"} 1`,
		`fleet_node_operations_inflight{admission_class="deferrable_read",kind="telemetry"} 0`,
		`fleet_node_protocol_checks_total{result="upgrade_required"} 1`,
		`fleet_node_auth_outcomes_total{operation="session",result="session_rejected"} 1`,
	} {
		require.Contains(t, string(body), line)
	}
}
