# Proto Fleet Installation

This document provides instructions for installing Proto Fleet.

## Prerequisites

Before running the install script:

1. Enable host networking in Docker:
   - Open Docker Desktop
   - Go to Settings -> Resources -> Network
   - Check "Enable host networking"

## Installing Proto Fleet

```bash
bash <(curl -fsSL https://github.com/block/proto-fleet/releases/latest/download/install.sh)
```

The `install.sh` script sets up the Proto Fleet server components.

### Proto Fleet Installation Options

```bash
Usage: install.sh [VERSION]

If you omit VERSION or pass "latest", installs the latest GitHub release.
Pass "nightly" to install the latest successful nightly prerelease.
You can override by doing, e.g.:
  install.sh v0.1.0-beta-5
  install.sh nightly
```

Examples:

```bash
# Install the latest version
bash <(curl -fsSL https://github.com/block/proto-fleet/releases/latest/download/install.sh)

# Install a specific version
bash <(curl -fsSL https://github.com/block/proto-fleet/releases/latest/download/install.sh) v0.1.0-beta-5

# Install the latest nightly prerelease (installer is fetched from the resolved
# nightly release asset, not from the mutable nightly-channel branch)
VERSION=$(curl -fsSL https://raw.githubusercontent.com/block/proto-fleet/nightly-channel/latest.txt)
bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/$VERSION/install.sh") "$VERSION"
```

The script will:

- Check system compatibility (page size)
- Download and extract the specified version
- Preserve existing configuration files if present
- Run the deployment script automatically

## Optional Virtual Miners

Deployment bundles include the virtual miner plugin for stress testing, but it
is disabled by default and is not loaded during a regular fleet install. To
enable it, set `ENABLE_VIRTUAL_MINERS=true` in the deployment `.env` file and
rerun `./run-fleet.sh`.

The bundled `server/virtual-plugin.json` generates 1000 miners by default in
the `10.255.x.x` range; discover them from ProtoFleet with IP List discovery
starting at `10.255.0.2`.

For larger curtailment stress tests, add generation overrides to `.env`:

```bash
ENABLE_VIRTUAL_MINERS=true
VIRTUAL_MINER_COUNT=5000
VIRTUAL_MINER_IP_START=10.255.0.2
VIRTUAL_MINER_SERIAL_PREFIX=VM
VIRTUAL_MINER_BASELINE_VARIANCE_PERCENT=10
```

Virtual miners simulate both network latency and miner processing latency. The
default miner-internal latency is 200-500ms, with occasional 5-8s outliers.
Generation is capped at 50,000 virtual miners per plugin process.

## Host Profiles

The installer tunes the database and poller for the host hardware via a
profile, chosen once during an interactive `./run-fleet.sh` run and stored as
`FLEET_PROFILE` in the deployment `.env`:

- `standard` (default): Raspberry Pi 5 class host, 16GB RAM with SSD; up to
  ~5000 miners
- `mini`: low-power or SD-card host, <=4GB RAM; up to ~200 miners
- `max`: dedicated server, 32GB+ RAM, 8+ cores, NVMe; 5000+ miners with
  maximum performance and durability

Non-interactive installs skip the prompt and keep conservative defaults; set
the profile directly in `.env` and rerun:

```bash
FLEET_PROFILE=standard
```

The full key list and per-value rationale live in `profiles/*.env`. Any single
key set in `.env` overrides the profile value (operator values win). Remove
the `FLEET_PROFILE` line to return to the untuned defaults. Because profiles
only apply through `run-fleet.sh`'s env-file layering, always restart the
stack with `./run-fleet.sh` rather than a bare `docker compose up`, which
would recreate the containers untuned.

## Database Connection Override

By default, fleet-api builds its PostgreSQL connection from `DB_USERNAME`,
`DB_PASSWORD`, `DB_NAME`, `DB_ADDRESS`, and `DB_SSL_MODE`. Advanced deployments
can set `DB_DSN` to provide the full PostgreSQL connection string instead. When
`DB_DSN` contains multiple hosts, it must include
`target_session_attrs=read-write` so fleet-api targets the current writable
database endpoint.

## Uninstalling Proto Fleet

```bash
bash <(curl -fsSL https://github.com/block/proto-fleet/releases/latest/download/uninstall.sh)
```

If Proto Fleet was installed in a non-default location, pass it explicitly:

```bash
bash <(curl -fsSL https://github.com/block/proto-fleet/releases/latest/download/uninstall.sh) --deployment-path /path/to/install/root
```

### SSL/TLS Configuration

During installation, you'll be prompted to choose a protocol mode:

1. **HTTP only** (default) - No encryption. Simplest option for isolated/air-gapped LANs.
2. **HTTPS with self-signed certificate** - Encryption enabled, but browsers will show security warnings.
3. **HTTPS with your own certificates** - Use your own CA-signed or custom certificates.

#### Using Your Own Certificates

To use your own SSL certificates, place them in the `ssl/` directory before running the installation:

```bash
mkdir -p ssl
cp /path/to/your/cert.pem ssl/cert.pem
cp /path/to/your/key.pem ssl/key.pem
```

The script will auto-detect existing certificates and use HTTPS mode automatically.

#### Certificate Requirements

- Certificate file: `ssl/cert.pem` (PEM format)
- Private key file: `ssl/key.pem` (PEM format, unencrypted)
- For LAN access, ensure the certificate includes the server's IP address(es) in the Subject Alternative Names (SANs)

## Alerts

The alerts deployment runs an extra grafana service:

| Service   | Image (pinned)                        | Purpose                                                                       |
| --------- | ------------------------------------- | ----------------------------------------------------------------------------- |
| `grafana` | `grafana/grafana:13.1.0-25771031703`  | Evaluates alert rules over `notification_metric_sample` and routes alerts via its built-in Alertmanager. |

### Network topology

Grafana runs on a private docker bridge network called `monitoring`.
The UI is bound to `127.0.0.1:3030` so operators on the box can reach
it without exposing the dashboard to the LAN. Grafana reaches
`fleet-api` (host-networked) via the docker host gateway for outbound
webhook deliveries, and TimescaleDB on the standard fleet network for
queries.

### Enabling the alerts stack

The alerts sidecar is a beta feature and is **off by default**.
It lives in a separate compose file,
`docker-compose.alerts.yaml`, that `run-fleet.sh` layers in via
a second `-f` flag when the `--enable-beta-alerts` flag is
passed. To run a fleet with the beta alerts stack:

```bash
./run-fleet.sh --enable-beta-alerts
```

On the first run with alerts enabled, `run-fleet.sh` rotates the
Grafana admin password and writes it into `.env` as
`GRAFANA_ADMIN_PASSWORD`. It also creates a dedicated read-only
PostgreSQL role for Grafana (`grafana_ro` by default) with `SELECT`
only on `notification_metric_sample`, and persists those credentials
to `.env` as `GRAFANA_DB_USERNAME` / `GRAFANA_DB_PASSWORD`. Grafana
authenticates as this role rather than the broader fleet-api app role.

### Configuration files

The configs live under `server/monitoring/grafana/`:

- `grafana.ini` — base Grafana config: unified alerting on, anonymous
  sign-up off, no upstream phone-home.
- `provisioning/datasources/timescaledb.yaml` — datasource pointed at
  the shared TimescaleDB instance. Credentials come from
  `GRAFANA_DB_USERNAME`/`GRAFANA_DB_PASSWORD` injected by
  docker-compose (set up by `run-fleet.sh`).
- `provisioning/alerting/proto-fleet-rules.yaml` — bundled alert rules
  (offline / high temperature / telemetry-poll failures / metric ingest
  stalled). These mirror the rules that previously lived in vmalert.
- `provisioning/alerting/contact-points.yaml` — receivers consumed by
  the built-in Alertmanager. The default deployment ships a single
  webhook receiver that posts to fleet-api's
  `/internal/alertmanager-webhook` endpoint.
- `provisioning/alerting/notification-policies.yaml` — root routing
  tree (grouping + repeat interval).

### Enabling system monitoring

Host system monitoring is **off by default** and requires the alerts
stack, since it reuses the same metrics pipeline, Grafana rule engine,
and notification channels:

```bash
./run-fleet.sh --enable-beta-alerts --enable-system-monitoring
```

(or set `ENABLE_BETA_ALERTS=true` and `ENABLE_SYSTEM_MONITORING=true`
in `.env` so upgrades keep it on).

This layers in `docker-compose.system-monitoring.yaml`, which:

- starts an in-process collector in fleet-api that samples host CPU,
  memory, and disk usage every 30 seconds into
  `notification_metric_sample`;
- mounts an empty sentinel volume **read-only** at `/hostfs` inside
  fleet-api. With the default local volume driver all named volumes
  share one backing filesystem, so the disk gauge reports the disk
  holding the TimescaleDB data (the one that filling up takes fleet
  down) without exposing any database files. To watch a different
  filesystem, change the mount source in
  `docker-compose.system-monitoring.yaml`;
- provisions the `proto-fleet-system` alert rules (Host CPU High,
  Host Memory High, Host Disk Space Low, Fleet Heartbeat Stale). They
  deliver to each organization's configured alert channels like any
  other rule, and can be paused per-org from the alerts settings page;
- provisions a "System Monitoring" Grafana dashboard with host gauges
  and slow-query tables backed by `pg_stat_statements`, read through a
  narrow `fleet_slow_statements()` definer function so the Grafana role
  sees this database's normalized statement stats without cluster-wide
  statistics privileges.

If fleet-api itself goes down, Grafana keeps evaluating the heartbeat
rule but can only deliver the notification once fleet-api is back; use
the Grafana UI at `127.0.0.1:3030` during an outage.

Disabling the feature removes the alert rules on the next start (via a
provisioned tombstone) but leaves the System Monitoring dashboard in
Grafana; delete it from the UI if it bothers you. fleet-api also
serves `GET /health/ready` (200 only when its database answers a ping)
for external uptime monitors, alongside the always-static liveness
check at `GET /health`.
