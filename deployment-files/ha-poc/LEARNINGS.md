# HA POC Implementation Learnings

These are the implementation-relevant lessons from running the active/passive HA
POC on three Raspberry Pis. The fake app and scripts are disposable; the
coordination model, failure behavior, and installer constraints are the useful
outputs.

## Core Architecture

- Keep the HA responsibilities separate:
  - Patroni/etcd owns Postgres primary election.
  - Fleet owns an application-level active lease in the writable database.
  - keepalived owns the LAN VIP and follows Fleet's local active health check.
- Do not infer Fleet active status from the local Patroni role. The POC proved
  that the active Fleet process and the Postgres primary can live on different
  hosts. That is valid as long as Fleet can renew the active lease and write
  heartbeats through the multi-host DSN.
- Fleet should use both Postgres hosts in its DSN with
  `target_session_attrs=read-write`. That lets the app reconnect to whichever
  node Patroni promotes instead of pinning to a hostname.
- A third witness node is useful for the first production shape. Two Fleet hosts
  plus an etcd witness keep DCS quorum available when one Fleet host fails.
- The VIP is the stable client endpoint, not proof of database leadership. It
  should move to the host whose Fleet process is currently active.

## Active Lease And Fencing

- Fleet's active role should be coordinated in the database, not by local
  process state, VIP ownership, or Patroni leadership.
- Active-only work should fail closed unless the process can prove it still owns
  the current lease epoch in the writable database.
- Active writes should be fenced with both `holder_id` and `lease_epoch`. A
  restarted, stalled, or partitioned old active must not be able to keep writing
  after another process takes over.
- The first real Fleet integration should gate only active/passive health and
  one harmless active loop. Scheduler, command dispatch, curtailment, MQTT, and
  ControlStreams should come after the coordinator behavior is proven inside
  real Fleet.

## Health Semantics

- Keep separate health surfaces:
  - liveness: process is running.
  - readiness: Fleet can reach a writable database path.
  - active: this process owns the current Fleet active lease.
  - HA diagnostics: holder, host, epoch, renew time, heartbeat time, DB target,
    Patroni role, and degraded state.
- `/health/active` must be strict because keepalived consumes it. It should
  return 200 only when the process can safely receive active traffic.
- A passive host returning 503 from `/health/active` is expected. keepalived
  logs may show curl exit 22 for the tracking script on passive hosts; that is a
  normal standby signal.
- Diagnostic timestamps should be initialized or omitted when unknown. Zero-time
  values are confusing during an outage or demo and should not be exposed as if
  they were real observations.
- HA diagnostics should be access-controlled. They expose topology, lease
  holders, timing, and failover state.

## Networking And VIP

- VRRP/VIP behavior must be tested on real same-subnet Linux hosts. Local Docker
  simulation and Tailscale-only addressing do not prove the L2 behavior that
  keepalived depends on.
- HA internals must use LAN interface addresses: etcd peer/client URLs, Patroni
  API addresses, Postgres addresses, VRRP peers, and the VIP. Tailscale is fine
  for SSH/operator access, but not for the HA control plane.
- Installer/config validation should reject Tailscale/CGNAT-looking
  `100.64.0.0/10` addresses for HA internals and provide a clear LAN-IP
  discovery command.
- Tooling should contact Patroni through its configured LAN listen/connect
  address, not `127.0.0.1`, because Patroni may not bind loopback.
- If the VIP needs to be reachable from laptops over Tailscale, advertise the
  VIP as a narrow subnet route such as `<vip>/32`. Do not advertise the whole
  LAN unless that is an intentional network policy decision.
- keepalived is host-level state. Containers, compose projects, and alternate
  ports do not isolate it. The installer must detect, back up, and avoid
  clobbering existing keepalived/VRRP configuration.

## Installer And Operations

- Existing Fleet deployments must not be disturbed by HA experiments or partial
  installs. The installer should preflight for occupied ports, existing Docker
  projects/volumes, and host-level keepalived state before changing anything.
- Startup should be idempotent and resumable. SSH sessions and package installs
  can fail mid-run on small hosts, so rerunning setup/start/restore should
  converge instead of requiring manual cleanup.
- etcd quorum health should be validated before expecting Patroni to converge.
  A refused peer port can leave Patroni stuck or degraded.
- Patroni/Postgres data directory ownership and mode must be enforced before
  Postgres starts. PostgreSQL refuses to start unless `PGDATA` is owned by the
  postgres user and has a safe mode such as `0700`.
- Preflight should make it obvious which host is `fleet-a`, which is `fleet-b`,
  which is the witness, and which IP is the VIP. Operator mistakes with host
  identity were more likely than code mistakes.
- Restore procedures should be explicit after destructive tests. Stopping the
  app and stopping Patroni are useful demos, but production runbooks need clear
  commands for returning a host to service and confirming it is healthy.

## Acceptance Tests To Preserve

The real implementation should keep these behaviors as acceptance criteria:

- Exactly one Fleet instance is active after cluster start.
- VIP traffic reaches the active Fleet instance.
- The active Fleet instance can be on a different host from the Postgres primary.
- Killing the active Fleet process causes the peer to acquire the lease and VIP.
- Killing the DB primary causes Patroni promotion and Fleet reconnection through
  the multi-host DSN.
- Killing the standby DB keeps active Fleet running but reports degraded HA.
- Stopping or partitioning one host does not produce dual-active behavior.
- Restarting an old active cannot dispatch, heartbeat, or write with a stale
  epoch.
- Passive health stays passive after restart unless that process wins the
  current database lease.

## Still Unproven

- Real Fleet command dispatch behavior under active/passive transitions.
- Fleet Node ControlStream reconnect behavior at scale.
- MQTT behavior during active/passive transitions.
- Curtailment and scheduler behavior under failover.
- Telemetry and Grafana behavior in degraded HA states.
- Production TLS, certificate automation, and secret distribution.
- Installer UX, upgrade behavior, rollback behavior, and existing-deployment
  migration.
