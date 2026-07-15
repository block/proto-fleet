# HA POC Learnings

These are implementation lessons from the active/passive HA POC. The POC is a
throwaway harness, but the behavior it exercised should shape the real Fleet HA
implementation.

## What Worked

- A three-node etcd quorum is enough for Patroni in this shape: two Fleet hosts
  plus a witness lets one Fleet host fail without losing the DCS quorum.
- Patroni and a multi-host Postgres DSN fit together cleanly. The app should use
  both Fleet database endpoints with `target_session_attrs=read-write` so it
  reconnects to the promoted writable primary instead of pinning to a host.
- The app active role should be coordinated in the database, not inferred from
  the local process or the Patroni role. Patroni decides which Postgres is
  writable; the Fleet lease decides which app instance may perform active work.
- keepalived should follow the app's local active health endpoint. Tracking
  `/health/active` moves the VIP to the host that can actually serve active
  Fleet traffic, independent of whether that host was originally primary.
- A fenced lease epoch is useful. Heartbeats and future active-only loops should
  include both `holder_id` and `lease_epoch` so a stalled or restarted old active
  cannot keep dispatching work after another instance takes over.

## Real Implementation Shape

- Keep the HA concerns separated:
  - Patroni/etcd owns Postgres primary election.
  - Fleet owns an application-level active lease in the writable database.
  - keepalived owns the LAN VIP and tracks the local active Fleet health check.
- Fleet should fail closed for active duties if it cannot prove it still owns
  the current lease epoch in the writable database.
- Active-only work should be gated at the loop or dispatcher boundary, not
  scattered through lower-level command/MQTT/ControlStream plumbing first.
- The first real Fleet POC should replace the fake app with real Fleet only far
  enough to gate active/passive health and run one harmless active loop.
  Scheduler, command dispatch, curtailment, MQTT, and ControlStream scale
  behavior should remain out of scope until the coordinator shape is stable.

## Networking Lessons

- VRRP/VIP behavior must be tested on real same-subnet Linux hosts. Docker
  Desktop, Tailscale-only addressing, and local-only simulations do not prove the
  L2 behavior that keepalived depends on.
- Operator access and HA internals are different planes. SSH over Tailscale is
  fine, but etcd, Patroni, Postgres, VRRP, and the VIP must use LAN interface
  addresses on the same subnet.
- The installer should reject Tailscale/CGNAT-looking `100.64.0.0/10`
  addresses for HA internals and provide an explicit LAN-IP discovery command.
- If the VIP needs to be reachable from a laptop over Tailscale, advertise the
  VIP as a narrow subnet route such as `<vip>/32`, not the whole LAN, and require
  route approval/ACLs in the tailnet.
- keepalived is host-level state. Even when containers use coexist-safe ports
  and isolated Docker volumes, installing keepalived can overwrite or compete
  with existing host VRRP configuration.

## Bootstrap And Operations Lessons

- Start order matters for a clean first boot: witness etcd first, then both
  Fleet hosts. If a peer's etcd peer port is refused, Patroni will stall or
  report unhealthy DCS connectivity until quorum is healthy.
- Passive `/health/active` returning 503 is expected. keepalived logs may show
  the local active-check script returning curl exit 22 on passive hosts; that is
  a normal standby signal, not an application crash.
- The POC needs coexist-safe ports when it runs beside an existing Fleet
  install. The useful defaults were:
  - etcd client/peer: `12379` / `12380`
  - Postgres: `15432`
  - Patroni API: `18008`
  - Fleet HTTP/VIP: `14080`
- Docker build context paths matter on remote Pi checkouts. The Patroni image
  should copy only files inside the POC context so remote builds do not fail on
  missing script paths.
- Postgres data directory permissions need explicit repair before Patroni starts.
  On the Pi run, Postgres refused to start until the mounted `PGDATA` directory
  was owned by `postgres` and mode `0700`.
- Long-running Pi setup should be idempotent and resumable. SSH sessions can
  drop during package installs or Docker builds, so scripts should make it easy
  to rerun `start`, `restore`, `status`, and targeted rebuilds safely.

## Health And Diagnostics

- Keep separate health surfaces:
  - liveness: process is up.
  - readiness: database path is usable.
  - active: this instance currently owns the Fleet lease.
  - HA diagnostics: current holder, epoch, renew time, heartbeat time, and
    Patroni/database view.
- The active health endpoint should be strict because keepalived consumes it.
  Returning 200 while the process is passive or unable to renew the lease would
  put the VIP on the wrong host.
- Diagnostic endpoints should be optionally token-protected. The POC data is not
  highly sensitive, but a real deployment will expose topology, lease holders,
  and timing data that should not be broadly visible.

## Failure Cases To Preserve

The real implementation should keep these as acceptance tests:

- exactly one active Fleet instance after cluster start.
- VIP traffic reaches the active Fleet instance.
- killing the active Fleet process causes the peer to take the lease and VIP.
- killing the DB primary causes Patroni promotion and app reconnection through
  the multi-host DSN.
- killing the standby DB keeps the active app running but reports degraded HA.
- stopping or partitioning one host does not produce dual-active behavior.
- restarting an old active cannot dispatch or heartbeat with a stale epoch.

## What The POC Did Not Prove

- real Fleet command dispatch semantics.
- Fleet Node ControlStream reconnect behavior at scale.
- MQTT behavior during active/passive transitions.
- telemetry and Grafana behavior in degraded HA states.
- production TLS, certificate automation, and secret distribution.
- final installer UX and upgrade behavior.
