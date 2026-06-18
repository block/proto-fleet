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
Usage: install.sh [VERSION] [options]

If you omit VERSION or pass "latest", installs the latest GitHub release.
Pass "nightly" to install the latest successful nightly prerelease.
You can override by doing, e.g.:
  install.sh v0.1.0-beta-5
  install.sh nightly

HA options:
  --ha-role monitor|data
  --ha-cluster NAME
  --ha-node-host HOST
  --ha-node-name NAME
  --ha-monitor-host HOST
  --ha-monitor-url URL
  --ha-initial-primary
  --ha-join-primary-host HOST
  --expected-config-fingerprint HASH
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

### Raspberry Pi HA lab installs

HA mode is a lab path where PostgreSQL remains Dockerized. The intended
production topology is 3 Raspberry Pis, but macOS is supported for local lab
testing when Docker Desktop host networking is enabled. The HA stack runs a
Dockerized `pg_auto_failover` monitor and Dockerized TimescaleDB data nodes.
Fleet starts only on the data node whose local DB container is primary.

On macOS, systemd is unavailable, so `fleet-follows-primary.timer` is not
installed. Run `ha/fleet-follows-primary.sh` manually for local role changes,
or rely on the Linux data node's timer when testing failover to a Pi.

Start with the full runbook in `ha/README.md`. Minimal command shapes:

```bash
./install.sh latest \
  --ha-role monitor \
  --ha-cluster fleet-ha \
  --ha-node-host pi-3.local
```

```bash
./install.sh latest \
  --ha-role data \
  --ha-cluster fleet-ha \
  --ha-node-host pi-1.local \
  --ha-monitor-host pi-3.local \
  --ha-initial-primary
```

```bash
./install.sh latest \
  --ha-role data \
  --ha-cluster fleet-ha \
  --ha-node-host pi-2.local \
  --ha-monitor-host pi-3.local \
  --ha-join-primary-host pi-1.local \
  --expected-config-fingerprint <fingerprint-from-pi-1>
```

HA mode validates existing config and secrets; it does not copy or create them.

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

## Notifications

The notifications deployment runs an extra grafana service:

| Service   | Image (pinned)                        | Purpose                                                                       |
| --------- | ------------------------------------- | ----------------------------------------------------------------------------- |
| `grafana` | `grafana/grafana:13.1.0-25771031703`  | Evaluates alert rules over `notification_metric_sample` and routes alerts via its built-in Alertmanager. |

### Network topology

Grafana runs on a private docker bridge network called `monitoring`.
The UI is bound to `127.0.0.1:3000` so operators on the box can reach
it without exposing the dashboard to the LAN. Grafana reaches
`fleet-api` (host-networked) via the docker host gateway for outbound
webhook deliveries, and TimescaleDB on the standard fleet network for
queries.

### Enabling the notifications stack

The notifications sidecar is a beta feature and is **off by default**.
It lives in a separate compose file,
`docker-compose.notifications.yaml`, that `run-fleet.sh` layers in via
a second `-f` flag when the `--enable-beta-notifications` flag is
passed. To run a fleet with the beta notifications stack:

```bash
./run-fleet.sh --enable-beta-notifications
```

On the first run with notifications enabled, `run-fleet.sh` rotates the
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
