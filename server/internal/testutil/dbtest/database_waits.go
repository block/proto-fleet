package dbtest

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// ObserveDatabaseWaits records bounded, credential-free diagnostics for lifetime
// tests. Passing tests keep their logs hidden; failures include the wait history.
func ObserveDatabaseWaits(t *testing.T, database *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		probeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		var version, extension, jit string
		var available bool
		err := database.QueryRowContext(probeCtx, `SELECT version(),(SELECT extversion FROM pg_extension WHERE extname='timescaledb'),current_setting('jit'),pg_jit_available()`).Scan(&version, &extension, &jit, &available)
		stop()
		if err == nil {
			t.Logf("DB lifetime diagnostics: %s; TimescaleDB=%s jit=%s available=%t", version, extension, jit, available)
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probeCtx, stop := context.WithTimeout(ctx, time.Second)
				func() {
					rows, err := database.QueryContext(probeCtx, `SELECT pid,state,COALESCE(wait_event_type,''),COALESCE(wait_event,''),COALESCE(substring(query FROM '^-- name: ([A-Za-z0-9_]+)'),split_part(query,' ',1)),pg_blocking_pids(pid)::text,extract(epoch FROM clock_timestamp()-query_start)::float8
 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state<>'idle' AND query_start<clock_timestamp()-interval '500 milliseconds'`)
					if err != nil {
						return
					}
					defer rows.Close()
					{
						for rows.Next() {
							var pid int
							var state, kind, wait, operation, blockers string
							var elapsed float64
							if err := rows.Scan(&pid, &state, &kind, &wait, &operation, &blockers, &elapsed); err == nil {
								t.Logf("DB lifetime wait: pid=%d state=%s wait=%s/%s operation=%s blockers=%s elapsed=%.3fs pool=%+v", pid, state, kind, wait, operation, blockers, elapsed, database.Stats())
							}
						}
						if err := rows.Err(); err != nil {
							t.Logf("DB lifetime diagnostic scan failed: %v", err)
						}
					}
				}()
				stop()
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
}
