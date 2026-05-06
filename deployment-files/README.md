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

## Notifications / Monitoring Sidecars

Starting with Phase 1 of the notifications work (Epic A), the deployment
runs four additional containers that together form the alerting pipeline:

| Service             | Image (pinned)                                       | Approx. RAM | Purpose                                                                        |
| ------------------- | ---------------------------------------------------- | ----------- | ------------------------------------------------------------------------------ |
| `otel-collector`    | `otel/opentelemetry-collector-contrib:0.150.1`       | ~80 MB      | Receives OTLP from `fleet-api` and forwards metrics to VictoriaMetrics.        |
| `victoria-metrics`  | `victoriametrics/victoria-metrics:v1.107.0`          | ~150 MB     | Stores metrics. 30-day retention by default. Persistent volume.                |
| `vmalert`           | `victoriametrics/vmalert:v1.107.0`                   | ~50 MB      | Evaluates alert rules against VictoriaMetrics, fires to Alertmanager.          |
| `alertmanager`      | `prom/alertmanager:v0.27.0`                          | ~40 MB      | Routes firing alerts to channels (email/webhook). Persistent volume.           |

Total resource footprint is roughly **+320 MB RAM** and one persistent
volume per stateful sidecar (`victoria-metrics-data`, `alertmanager-data`).

### Network topology

All four sidecars run on a private docker bridge network called
`monitoring`. None of them publish ports on `0.0.0.0` — they are not
reachable from the LAN. To allow the host-networked `fleet-api` to talk to
them, the `vmalert`, `alertmanager`, and `otel-collector` services bind
their listen ports to **`127.0.0.1` on the host loopback only**. Operators
running tools on the box can hit `127.0.0.1:8880` (vmalert),
`127.0.0.1:9093` (Alertmanager), and `127.0.0.1:4317`/`4318` (OTLP
collector) — these endpoints are not exposed beyond loopback. VictoriaMetrics
itself is reachable only from inside the `monitoring` network.

`alertmanager` reaches `fleet-api` (for the activity-log webhook) via the
docker `host-gateway` extra-host entry, since `fleet-api` uses
`network_mode: host`.

### Disabling the notifications stack

The whole pipeline is gated by a single environment variable,
`FLEET_NOTIFICATIONS_ENABLED`, defaulting to `true`. To run a fleet without
notifications:

```bash
echo "FLEET_NOTIFICATIONS_ENABLED=false" >> .env
# When starting compose, omit the notifications profile:
docker compose --profile "" up -d
# (or simply `docker compose up -d` without `COMPOSE_PROFILES=notifications`)
```

When disabled, the four sidecars are not created, and `fleet-api` skips the
notifications surface (channels API, rules API, activity-log webhook). The
rest of the fleet — onboarding, telemetry, command dispatch, the dashboard
— continues to work.

### Configuration files

The configs that ProtoFleet's reload pipeline (Epic E) rewrites on every
notification change live under `deployment-files/server/monitoring/`:

- `otel-collector.yaml` — read-only OTLP receiver + VictoriaMetrics exporter.
- `vmalert/rules.yml` — user-rendered rule file (rewritten by ProtoFleet).
- `vmalert/rules.d/*.yml` — built-in rule groups (e.g. `protofleet-self.yml`)
  that ship with the deployment and are not mutated by the reload pipeline.
- `alertmanager/alertmanager.yml` — receivers and routes (rewritten by
  ProtoFleet).
