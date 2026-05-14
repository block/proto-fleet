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

## Notifications

The deployment runs four additional containers that together form the alerting pipeline:

| Service             | Image (pinned)                                       | Purpose                                                                        |
| ------------------- | ---------------------------------------------------- | ------------------------------------------------------------------------------ |
| `otel-collector`    | `otel/opentelemetry-collector-contrib:0.150.1`       | Receives OTLP from `fleet-api` and forwards metrics to VictoriaMetrics.        |
| `victoria-metrics`  | `victoriametrics/victoria-metrics:v1.107.0`          | Stores metrics. 30-day retention by default. Persistent volume.                |
| `vmalert`           | `victoriametrics/vmalert:v1.107.0`                   | Evaluates alert rules against VictoriaMetrics, fires to Alertmanager.          |
| `alertmanager`      | `prom/alertmanager:v0.27.0`                          | Routes firing alerts to channels (email/webhook). Persistent volume.           |

### Network topology

All four sidecars run on a private docker bridge network called `monitoring`.
Operators running tools on the box can hit `127.0.0.1:8880` (vmalert),
`127.0.0.1:9093` (Alertmanager), and `127.0.0.1:4317`/`4318` (OTLP collector) —
these endpoints are not exposed beyond loopback.
VictoriaMetrics itself is reachable only from inside the `monitoring` network.

### Enabling the notifications stack

The notifications sidecars are a beta feature and are **off by default**. The
four sidecars live in a separate compose file,
`docker-compose.notifications.yaml`, that `run-fleet.sh` layers in via a
second `-f` flag when the `--enable-beta-notifications` flag is passed. To
run a fleet with the beta notifications stack:

```bash
./run-fleet.sh --enable-beta-notifications
```

### Configuration files

The configs live under `deployment-files/server/monitoring/`:

- `otel-collector.yaml` — read-only OTLP receiver + VictoriaMetrics exporter.
- `vmalert/rules.yml` — user-rendered rule file (rewritten by ProtoFleet).
- `vmalert/rules.d/*.yml` — built-in rule groups (e.g. `protofleet-self.yml`)
  that ship with the deployment and are not mutated by the reload pipeline.
- `alertmanager/alertmanager.yml` — receivers and routes (rewritten by
  ProtoFleet).
