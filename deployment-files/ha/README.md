# Proto Fleet Docker HA Lab Runbook

This is the fast HA path for on-prem curtailment deployments where PostgreSQL
and TimescaleDB stay in Docker. The intended production topology is Raspberry
Pi OS, but macOS is supported for local lab testing when Docker Desktop host
networking is enabled.

## Topology

| Node | Role |
| --- | --- |
| `pi-1` | Fleet + HA TimescaleDB data node |
| `pi-2` | Fleet + HA TimescaleDB data node |
| `pi-3` | `pg_auto_failover` monitor |

Fleet runs on exactly one data node: the node whose local `fleet-ha-db`
container is the writable primary. The standby data node keeps its DB container
running, but `fleet-api` and `fleet-client` stay stopped.

`pi-3` is not in the steady-state curtailment path. If `pi-3` goes down, the
already-active Fleet node keeps running, but no new automatic failover can be
confirmed until the monitor returns.

For a local-machine-plus-one-Pi smoke test, the local machine may run the
monitor and one data node while the Pi runs the second data node. On macOS,
systemd is unavailable, so the local machine does not get an automatic
`fleet-follows-primary.timer`; run `./ha/fleet-follows-primary.sh` manually if
you need Fleet to follow a local role change.

## Lab Baseline

Run on all three Pis:

```bash
cat /etc/os-release
uname -m
getconf PAGESIZE
docker version
docker compose version
hostname -f
hostname -I
```

Pass criteria:

- Raspberry Pi OS 64-bit.
- `getconf PAGESIZE` returns `4096`.
- Docker and Docker Compose work.
- `pi-1`, `pi-2`, and `pi-3` resolve each other by hostname.

On macOS lab hosts, also confirm Docker Desktop host networking is enabled
before running HA containers.

## Image Validation

The release bundle includes:

```text
images/timescaledb-ha.tar.gz
```

After install, validate the loaded image:

```bash
docker run --rm proto-fleet-timescaledb-ha:latest pg_autoctl --version
docker run --rm proto-fleet-timescaledb-ha:latest postgres --version
```

Every Pi must load the same image ID.

## Install The Monitor On `pi-3`

```bash
./install.sh latest \
  --ha-role monitor \
  --ha-cluster fleet-ha \
  --ha-node-host pi-3.local
```

Validate:

```bash
docker logs fleet-ha-monitor
docker exec -u postgres fleet-ha-monitor pg_autoctl show state --pgdata /home/postgres/pgdata/data
```

Pass criteria:

- `fleet-ha-monitor` is running.
- `pg_autoctl show state` works.
- Logs do not show an auth loop.

## Install Data Node A On `pi-1`

Before running the HA installer, put the production `.env`, SSL files, plugin
config, and curtailment/MQTT config in place. HA mode validates these files; it
does not create or copy secrets.

```bash
./install.sh latest \
  --ha-role data \
  --ha-cluster fleet-ha \
  --ha-node-host pi-1.local \
  --ha-monitor-host pi-3.local \
  --ha-initial-primary
```

Capture the config fingerprint:

```bash
./ha/status.sh
```

or:

```bash
./ha/config-fingerprint.sh
```

Validate:

```bash
docker logs fleet-ha-db
docker exec -u postgres fleet-ha-db pg_autoctl show state --pgdata /home/postgres/pgdata/data
docker exec -u postgres fleet-ha-db psql -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" -c "select pg_is_in_recovery();"
```

## Install Data Node B On `pi-2`

Copy the same `.env`, SSL files, plugin config, and curtailment/MQTT config to
`pi-2`. Use the fingerprint from `pi-1`.

```bash
./install.sh latest \
  --ha-role data \
  --ha-cluster fleet-ha \
  --ha-node-host pi-2.local \
  --ha-monitor-host pi-3.local \
  --ha-join-primary-host pi-1.local \
  --expected-config-fingerprint <fingerprint-from-pi-1>
```

Pass criteria:

- One data node is primary.
- One data node is standby.
- Fleet runs only on the primary.

## Validate Extensions And Migrations

On the current primary:

```bash
docker exec -u postgres fleet-ha-db psql -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" -c "create extension if not exists timescaledb;"
docker exec -u postgres fleet-ha-db psql -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" -c "create extension if not exists timescaledb_toolkit;"
docker exec -u postgres fleet-ha-db psql -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" -c "create extension if not exists vector;"
```

Then verify Fleet has run migrations and the standby received the schema.

## Manual Failover Gate

Before relying on automation:

1. Confirm the current primary with `./ha/status.sh`.
2. Stop Fleet on the primary: `./ha/fleet-control.sh stop`.
3. Trigger a controlled `pg_auto_failover` switchover.
4. Confirm the other DB container becomes primary.
5. Start Fleet on the new primary: `./ha/fleet-control.sh start`.
6. Confirm curtailment dispatch works.
7. Confirm Fleet remains stopped on the old primary.

Pass criteria:

- Fleet works after DB promotion.
- No dual Fleet writers.
- No manual DB repair required.

## Active Data-Node Failure Gate

With active curtailment load running:

1. Record `T0`.
2. Kill or power off the active data-node Pi.
3. Watch promotion from `pi-3` or the standby.
4. Wait for `fleet-follows-primary.timer` to start Fleet on the promoted node.
5. Record the first successful curtailment dispatch as `T_dispatch`.

Pass gate:

```text
T_dispatch - T0 <= 60 seconds
```

Run 10 trials. All 10 must pass.

Capture:

| Event | Timestamp |
| --- | --- |
| Failure injected |  |
| Monitor detects failure |  |
| Standby promotes |  |
| DB writable on promoted node |  |
| Fleet start begins |  |
| Fleet health responds |  |
| MQTT source active |  |
| First curtailment dispatch succeeds |  |

## Monitor Failure Gate

On `pi-3`:

```bash
docker stop fleet-ha-monitor
```

Then trigger curtailment while active Fleet and active DB continue running.

Pass criteria:

- Active curtailment continues.
- Standby Fleet does not start.
- No automatic failover occurs while the monitor is unavailable.

Restart:

```bash
docker start fleet-ha-monitor
```

## Useful Commands

```bash
./ha/status.sh
./ha/status.sh --json
./ha/fleet-control.sh start
./ha/fleet-control.sh stop
systemctl status fleet-follows-primary.timer
journalctl -u fleet-follows-primary.service -n 100
```

## Deferred From V1

- VIP/load balancer.
- Grafana/notifications HA.
- Automatic failback.
- Windows installer HA.
- Native PostgreSQL install.
- Patroni or the full TimescaleDB HA image.
- Secret copying.
- Docker Swarm or Kubernetes.
