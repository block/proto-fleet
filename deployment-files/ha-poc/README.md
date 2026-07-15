# Proto Fleet HA POC

This directory is an isolated proof-of-concept harness for RFC 0002. It is not
the production HA installer.

See [LEARNINGS.md](./LEARNINGS.md) for the implementation lessons from running
this POC on real Raspberry Pis.

The POC proves four things on three real same-subnet Linux hosts:

- Patroni promotes a new writable Postgres primary.
- A pgx multi-host DSN reconnects to the writable DB.
- A database-backed `fleet-active` lease allows exactly one fake Fleet app to
  report active.
- keepalived/VRRP moves an on-prem VIP to the host whose local fake Fleet app
  passes `/health/active`.

## Topology

| Host | Containers | Host service |
| --- | --- | --- |
| `fleet-a` | `etcd`, `patroni`, `fake-fleet` | `keepalived` |
| `fleet-b` | `etcd`, `patroni`, `fake-fleet` | `keepalived` |
| `witness` | `etcd` | none |

The fake Fleet app exposes:

- `/health`: process liveness.
- `/health/ready`: database reachability and writable-session status.
- `/health/active`: 200 only while this process owns `fleet-active`.
- `/health/ha`: POC diagnostics, optionally protected by
  `HA_POC_STATUS_TOKEN`.

When active, the app writes one fenced heartbeat row per second. The heartbeat
insert succeeds only if the DB lease row still matches the app's `holder_id`
and `lease_epoch`, so a restarted or stalled old active cannot keep writing
after takeover.

## Raspberry Pi Fast Path

Use three dedicated Raspberry Pis when possible, but the Pi wrapper defaults to
a coexist-safe port preset so it can run beside an existing Fleet instance.
With the default `HA_POC_PORT_MODE=coexist`, the POC uses host ports `12379`,
`12380`, `15432`, `18008`, and `14080` instead of Fleet's usual `2379`,
`2380`, `5432`, `8008`, and `4080`.

The POC uses its own Docker volumes and does not reuse Fleet's database. The
coexist preset avoids the normal Fleet ports, but `keepalived` is still a host
service on `fleet-a` and `fleet-b`; do not install it on a host that already
uses keepalived/VRRP unless you intentionally replace that config. The installer
backs up any existing `/etc/keepalived/keepalived.conf` before writing the POC
config.

Pick these values before starting:

```text
fleet-a IP: <fleet-a-lan-ip-on-eth0>
fleet-b IP: <fleet-b-lan-ip-on-eth0>
witness IP: <witness-lan-ip-on-eth0>
VIP:        <unused-lan-ip-on-the-same-subnet>
interface:  eth0
```

It is fine to SSH from your laptop to the Pis over Tailscale, but do not put
Tailscale `100.x` addresses in `.env`. etcd, Patroni, Postgres, and VRRP must
use the Pis' LAN IPs on the same subnet as the VIP. From each Pi, run:

```bash
./scripts/pi-poc.sh lan-ip eth0
```

Use those LAN IPs in the `configure` command below. `configure` rejects
Tailscale/CGNAT-looking `100.64.0.0/10` addresses for HA internals.

On each Pi, check out the branch and configure the host-local `.env`:

```bash
git clone https://github.com/block/proto-fleet.git ~/proto-fleet
cd ~/proto-fleet
git fetch origin ankitg/poc-active-passive-ha
git checkout -B ankitg/poc-active-passive-ha origin/ankitg/poc-active-passive-ha
cd deployment-files/ha-poc

# Run the matching command on each host:
HA_POC_PASSWORD='change-me' ./scripts/pi-poc.sh configure fleet-a <fleet-a-lan-ip-on-eth0> <fleet-b-lan-ip-on-eth0> <witness-lan-ip-on-eth0> <vip> eth0
HA_POC_PASSWORD='change-me' ./scripts/pi-poc.sh configure fleet-b <fleet-a-lan-ip-on-eth0> <fleet-b-lan-ip-on-eth0> <witness-lan-ip-on-eth0> <vip> eth0
HA_POC_PASSWORD='change-me' ./scripts/pi-poc.sh configure witness <fleet-a-lan-ip-on-eth0> <fleet-b-lan-ip-on-eth0> <witness-lan-ip-on-eth0> <vip> eth0

./scripts/pi-poc.sh install-deps
./scripts/pi-poc.sh doctor
```

Start order:

```bash
# witness first
./scripts/pi-poc.sh start

# then fleet-a and fleet-b
./scripts/pi-poc.sh start
```

Common operations:

```bash
./scripts/pi-poc.sh status        # status from a Fleet host
./scripts/pi-poc.sh active-host   # current VIP-backed active holder
./scripts/pi-poc.sh watch         # measure failover from another terminal
./scripts/pi-poc.sh fail-app      # run on the currently active Fleet host
./scripts/pi-poc.sh fail-db       # stop local Patroni; script prints primary/replica
./scripts/pi-poc.sh restore       # bring local services back
./scripts/pi-poc.sh stop          # stop local POC services
./scripts/pi-poc.sh reset --yes   # delete local POC Docker volumes
```

To force the original ports on dedicated hosts, set
`HA_POC_PORT_MODE=standard` when running `configure`.

## Manual Setup

Copy this repo to all three hosts and create one host-local `.env` from
`ha-poc.env.example`.

On `fleet-a` and `fleet-b`:

```bash
cd deployment-files/ha-poc
cp ha-poc.env.example .env
$EDITOR .env
./scripts/preflight.sh .env
docker compose --env-file .env -f docker-compose.fleet.yaml up -d --build
sudo ./scripts/install-keepalived.sh .env
```

On `witness`:

```bash
cd deployment-files/ha-poc
cp ha-poc.env.example .env
$EDITOR .env
./scripts/preflight.sh .env
docker compose --env-file .env -f docker-compose.witness.yaml up -d
```

Use the Postgres maintenance database for the first POC run:

```text
HA_POC_DB_DSN=postgres://postgres:<password>@<fleet-a-ip>:15432,<fleet-b-ip>:15432/postgres?sslmode=disable&target_session_attrs=read-write
```

That avoids mixing the proof harness with Fleet migrations. A later Fleet POC
can point the same app shape at the real Fleet database after creating the app
database and grants.

## Status

From either Fleet app host:

```bash
./scripts/ha-status.sh .env
```

Manual checks:

```bash
curl -fsS http://<fleet-a-ip>:14080/health/ha | jq
curl -fsS http://<fleet-b-ip>:14080/health/ha | jq
curl -fsS http://<vip>:14080/health/active | jq
curl -fsS http://<fleet-a-ip>:18008/cluster | jq
```

## Test Cases

Start cluster:

```bash
./scripts/ha-status.sh .env
```

Expected: exactly one fake Fleet app has `/health/active` returning 200, and
the VIP endpoint returns that app's `holder_id`.

Measure failover RTO in one terminal, then trigger a failure in another:

```bash
./scripts/watch-failover.sh .env
```

Kill active fake Fleet process:

```bash
docker compose --env-file .env -f docker-compose.fleet.yaml stop fake-fleet
```

Expected: the peer acquires a higher lease epoch and keepalived moves the VIP
after local `/health/active` passes on the peer.

Kill DB primary container:

```bash
curl -fsS http://127.0.0.1:18008/patroni | jq .role
docker compose --env-file .env -f docker-compose.fleet.yaml stop patroni
```

Run the stop on the host whose Patroni role is `primary`. Expected: Patroni
promotes the standby; the fake Fleet app reconnects through the multi-host DSN
and resumes lease renewals/heartbeats.

Kill standby DB:

```bash
docker compose --env-file .env -f docker-compose.fleet.yaml stop patroni
```

Run this on the standby host. Expected: the active fake Fleet app keeps running
against the primary. The cluster is degraded and not failover-ready.

Stop one host:

```bash
sudo systemctl stop keepalived
docker compose --env-file .env -f docker-compose.fleet.yaml down
```

Expected: no dual-active. The host that cannot reach the authoritative writable
DB cannot renew `fleet-active`.

Restart old active:

```bash
docker compose --env-file .env -f docker-compose.fleet.yaml up -d fake-fleet
```

Expected: the restarted process receives a new `holder_id`; if it becomes
active, it must do so at a later epoch. If it is passive, stale heartbeat writes
are rejected by the lease/epoch guard.

## What This Does Not Prove

- Real Fleet command dispatch behavior.
- Fleet Node ControlStream reconnect behavior at scale.
- Telemetry/Grafana degraded behavior.
- Production TLS/cert automation.
- Final installer UX.
