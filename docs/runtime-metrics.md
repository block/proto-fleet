# Operational runtime metrics

`FLEET_RUNTIME_METRICS_ENABLED=true` enables a Prometheus endpoint inside Fleet
at `http://127.0.0.1:9090/metrics`. It defaults off, is independent of customer
alerts and trace sampling, and contains no backend credentials. The HA deployment
preserves the flag in its application profile and forwards it to the host-network
Fleet container. `FLEET_RUNTIME_METRICS_ADDRESS` can select another loopback IP/port
for standalone processes; the HA profile deliberately uses the fixed default.

An existing host collector scrapes and forwards the endpoint. Backend-specific
configuration belongs to the deployment repository. There is no extra service.
Non-loopback listen addresses are rejected; no application ingress route exposes
these operational metrics. Missing scrapes indicate lost visibility, never zero
load or recovery. Startup fails if the enabled endpoint cannot bind.

## Database pool pressure

The `application` pool is the main `sql.DB` connection pool. Collection reads its
in-memory `Stats` without acquiring a database connection or executing a query.
Pool metrics describe acquisition, not SQL execution. Physical connection attempts
are counted separately, including startup and replacement connections. Native
database health and query instrumentation complement them.

Connection attempts with any non-nil error count as `result=failure`, including
caller cancellation and deadline expiry during a dial. These failures can rise
while PostgreSQL remains healthy; correlate them with pool pressure and native
database health before treating them as database outages. Canceled pool checkouts
that never attempt a physical connection do not increment this counter.

| Prometheus series | Type | Meaning |
| --- | --- | --- |
| `fleet_db_pool_open_connections` | gauge | Open connections |
| `fleet_db_pool_in_use_connections` | gauge | Connections checked out |
| `fleet_db_pool_idle_connections` | gauge | Connections available |
| `fleet_db_pool_max_connections` | gauge | Configured capacity; zero means unlimited |
| `fleet_db_pool_waits_total` | counter | Acquisitions that waited for a connection |
| `fleet_db_pool_wait_seconds_total` | counter | Cumulative time spent waiting |
| `fleet_db_connection_attempts_total` | counter | Physical connection attempts, with `result=success` or `failure` |
| `fleet_runtime_start_time_seconds` | gauge | Unix time when instrumentation started |

Pool series carry only the fixed `pool` label. Counters reset on process restart;
collectors must handle resets. Compute utilization only for positive capacity.
Wait-duration deltas divided by wait-count deltas describe average wait time for
waiters, not latency of every acquisition. Long ongoing waits can precede their
duration accounting; utilization and wait count provide complementary evidence.

The endpoint uses OpenTelemetry metric instruments and a standard Prometheus
exporter. It does not register global collectors or change the trace provider.
Scrapes have bounded concurrency and timeouts. Add new signal families with
bounded labels and cheap in-memory callbacks, never database or device I/O.
