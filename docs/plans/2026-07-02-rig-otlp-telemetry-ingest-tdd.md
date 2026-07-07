---
title: Rig OTLP telemetry via rig-otlp-bridge sidecar (external Prometheus)
date: 2026-07-02
status: draft
type: tdd
---

## Summary

Proto rigs natively emit rich OTLP metrics (mining, per-hashboard ASIC
aggregates, PSU, host) through an on-rig `telemetry-service` gRPC stream.
Historically those metrics were collected by a standalone per-site stack in
`miner-firmware/tools/telemetry/` (`otlp-bridge` → Prometheus → dedicated
Grafana) deployed and operated separately from proto-fleet.

This TDD brings that pipeline into proto-fleet as a **sidecar**: the
miner-firmware `otlp-bridge` is vendored into the repo as
`server/rig-otlp-bridge` and runs as its own container next to fleet-api.
Instead of subnet scanning and static site config, the bridge calls the
existing `fleetmanagement.v1/ListMinerStateSnapshots` RPC to learn which
paired proto rigs to stream from and what fleet context
(`device_identifier`, `site`, `building`, `rack`, `zone`) to stamp onto
every OTLP resource. Metrics land in an **external Prometheus that the
operator sets up and runs themselves** (`metrics_endpoint` in the bridge's
config file); the fleet stack bundles no metrics store and no dashboards.

Everything is opt-in behind the rig telemetry feature flag, and the bridge
is driven by a `config.json` mounted from the deployment directory and
re-read whenever the container restarts.

An earlier design that ran the ingest inside fleetd (a domain service with
direct DB enrichment) is archived on the
`archive/rig-telemetry-fleetd-ingest` branch; see Alternatives.

## Background

### Miner side (`miner-firmware`)

- On-rig services publish OTLP metrics over NATS; `telemetry-service`
  aggregates them and exposes gRPC server-streaming (`StreamMetrics` /
  `StreamLogs`, port 2123, plaintext) carrying opaque
  `ExportMetricsServiceRequest` bytes
  (`crates/rpc/protos/miner_telemetry_api.proto`) — designed for
  pass-through to a Prometheus OTLP receiver without transcoding.
- The standalone `otlp-bridge` discovers rigs by subnet scan, resolves
  `hostname` via the rig REST API (`GET /api/v1/network`), stamps
  `hostname` / `rig_ip` / `site` (from a static config) onto each OTLP
  Resource, and POSTs merged gzip batches to Prometheus's OTLP receiver.
- Volume: ~100–150 series/rig at a ~10 s cadence; mostly gauges plus
  sparse info metrics.

### Fleet side (`proto-fleet`)

- fleetd pulls coarse telemetry via driver plugins into TimescaleDB
  (`device_metrics`); that product path is untouched here.
- The device model supplies everything the bridge's discovery layer
  reconstructs from scratch: `device_identifier`, discovery IP + REST
  port/scheme, and the org → site → building → rack/zone hierarchy.
- Grafana ships via opt-in compose overlays; there was no Prometheus and
  no Loki. Fleet's RPC surface is Connect over h2c on one port, with an
  `UnauthenticatedProcedures` exemption list for procedures that validate
  their own credential in the handler (fleet-node bootstrap, and now this).

## Goals

1. Stream the full OTLP metrics of every directly-reachable paired proto
   rig to an external, operator-managed Prometheus (OTLP receiver).
2. Enrich every series with fleet context at ingest time, sourced from
   fleet RPCs rather than static config or direct DB access.
3. Drive the bridge from a `config.json` on disk that is re-read whenever
   the container restarts, so operators can repoint it without a rebuild.
4. Preserve query compatibility with the miner-firmware PromQL tooling.
5. Keep the pipeline fully feature-flagged: with the flag off, no bridge
   container exists.
6. Minimize divergence from the upstream otlp-bridge so re-vendoring stays
   cheap.

## Non-goals (deferred)

- **Logs.** The bridge's Loki path is vendored but disabled (fleet has no
  log store).
- **FleetNode (remote LAN) sites** — the target RPC excludes fleet-node
  devices; remote-site streaming is future work.
- **Non-proto miners**; the plugin polling path is unchanged.
- **MCAP backfill** (`otlp-bridge push`) — stays a miner-firmware lab tool;
  the subcommand is not vendored.
- **Alert rules on the new metrics** (explicit decision): visualization and
  ad-hoc querying only.
- **Bundled metrics storage/visualization.** An earlier revision shipped a
  Prometheus container and provisioned Grafana rig dashboards inside the
  fleet compose stack; that was cut — the bridge pushes to an external
  Prometheus the operator sets up and points it at via `config.json`.

## Design overview

```
┌─ rig (per miner) ────────────┐        ┌─ rig-otlp-bridge (sidecar) ──────────────┐
│ services → NATS → telemetry- │  gRPC  │ vendored from miner-firmware otlp-bridge │
│ service :2123 (OTLP batches) │◄───────┤  • targets + enrichment labels from      │
└──────────────────────────────┘ stream │    fleet RPC (30s scan interval)         │
                                        │  • hostname from rig REST (as upstream)  │
              ┌─────────────────────────┤  • label injection, merge, gzip POST     │
   RPC: ListMinerStateSnapshots         │  • config.json mounted from the          │
   (fleet API key, Connect)             │    deployment dir, read at startup       │
┌─────────────▼───────────────┐         └──────────────┬───────────────────────────┘
│ fleet-api                   │                        │ POST /api/v1/otlp/v1/metrics
│ (existing operator RPC,     │         ┌──────────────▼──────────────┐
│  models filter = proto rig) │         │ external Prometheus         │
└─────────────────────────────┘         │ (operator-run, OTLP         │
                                        │  receiver enabled; not part │
                                        │  of the fleet stack)        │
                                        └─────────────────────────────┘
```

## Detailed design

### 1. Vendored bridge (`server/rig-otlp-bridge`)

A standalone Go module (like `fake-proto-rig`), copied from
`miner-firmware/tools/telemetry/otlp-bridge` with deliberate minimal
divergence:

- **Dropped:** the `push` MCAP-backfill subcommand (and its mcap
  dependency); the miner-firmware default-config test.
- **Import paths:** the on-rig API stubs live at
  `internal/rigapi/minertelemetry`, generated in miner-firmware by
  `tools/telemetry/otlp-bridge/generate-fleet-stubs.sh` (module path is a
  script argument) and vendored here — the fleet build never compiles the
  non-conformant rig protos.
- **Added — fleet mode** (`fleet_targets.go`): when `fleet_api_url` is set
  (config, `--fleet-api-url`, or `OTLP_BRIDGE_FLEET_API_URL`), the scan
  loop fetches targets from the fleet RPC instead of subnet scanning. Per
  target the bridge still probes the telemetry port and resolves
  `hostname` from the rig REST API (per-target scheme/port from fleet
  discovery), then stamps `device_identifier`, `rig_ip`, `site`,
  `building`, `rack`, `zone` (empty context omitted). Subnet/target mode
  is retained as a fallback for standalone use.
- **Added — label-change restarts:** `registry.replace` now reports an
  address whose labels changed as removed+added, and the scan loop stops
  before starting, so a re-sited/re-racked rig restarts its worker and new
  series carry the new context.
- **Config-file-driven, `metrics_endpoint` required:** the fleet sidecar
  mounts `config.json` from the deployment directory (re-read on container
  restart); the OTLP receiver URL has no default since no Prometheus is
  bundled. Env/flag overrides remain, and with a fleet API URL provided
  via env/flag and no config file present the bridge still starts from
  defaults (standalone use).

The RPC client is the server's own generated Connect client (Connect
protocol over fleet-api's HTTP listener, bearer token in the request
header). The generated stubs live in a nested Go module
(`server/generated/grpc`) that the server and the bridge both consume via
replace directives — one copy of the codegen, no bridge-local generation,
and the bridge still never depends on the server module itself.

### 2. Enrichment via ListMinerStateSnapshots + service API key

The bridge consumes the existing operator RPC
`fleetmanagement.v1/ListMinerStateSnapshots` (paginated, `PermMinerRead`),
using the existing `models` filter (bridge config `fleet_target_models`,
default the proto rig model) — no bridge-specific API surface beyond the
additive `common.v1.PlacementRefs.zone`. The rig REST endpoint
(scheme/port) is fixed per deployment in bridge config rather than
carried per-device. Fleet-node-owned device exclusion is deferred until
fleet nodes are live. The response body is in `SensitiveBodyProcedures`
so fleet topology never lands in debug logs.

Auth is a **normal fleet API key** on the standard Bearer path — no
server-side auth changes at all. The operator creates the key in the
fleet UI (Settings → API Keys, a user whose role grants `miner:read`)
and pastes it into `rig-telemetry/config.json` as `fleet_api_token`; the
bridge presents it on every call. Rotation and revocation use the existing
API-key lifecycle (UI/RPC). Dev stacks seed a static key with
`just dev-bridge-key`. Trade-off (accepted per review — internal
deployments): the key is bound to a human user, so deactivating that
user disables telemetry, and scope is the user's role rather than a
service-scoped permission set.

### 3. External Prometheus

The bridge POSTs OTLP to whatever `metrics_endpoint` names — typically a
Prometheus run with `--web.enable-otlp-receiver` at
`<host>:9090/api/v1/otlp/v1/metrics`. Setting it up, securing it, and
provisioning dashboards on top of it are the operator's responsibility
and out of scope here; nothing in the fleet compose stack stores or
visualizes the metrics.

### 4. Feature flag & wiring

**Production** (`run-fleet.sh --enable-rig-telemetry`): layers
`docker-compose.rig-telemetry.yaml`, which adds only the
`rig-otlp-bridge` service (host networking — it must reach rigs on the
site LAN like fleet-api; it reaches fleet-api at `127.0.0.1:4000`). The
bridge is configured entirely by `rig-telemetry/config.json` in the
deployment directory, mounted read-only into the container (as a
directory mount so host edits are visible) and read once at startup —
edit, then `docker compose restart rig-otlp-bridge` to apply. On first
enable `run-fleet.sh` writes a `600`-mode template and exits so the
operator can fill in `fleet_api_token` (an API key created in the fleet
UI) and `metrics_endpoint`. The artifact workflow builds the bridge
binary (standalone module, `GOWORK=off`, CGO disabled), ships it in the
server tarball, and packages it via
`deployment-files/server/Dockerfile.rig-otlp-bridge`.

**Dev**: `just dev-rig-telemetry` / `just dev-monitoring` (or
`ENABLE_RIG_TELEMETRY=true` through `dev.sh`); the dev overlay builds the
bridge from source and mounts the committed
`rig-otlp-bridge/dev-config/config.json` (static dev token, fake-rig REST
port, `metrics_endpoint` at `host.docker.internal:9090` for a
Prometheus the developer runs on the host). With no flag, `just dev`
runs no bridge.

**Bridge config (sidecar):** `fleet_api_url`, `fleet_api_token`,
`metrics_endpoint`, rig REST `api_scheme`/`api_port`, optional
`fleet_target_cidrs` — everything else uses the upstream defaults (30 s
scan, 1–30 s stream reconnect backoff, gzip on, logs disabled). The
`--otlp-endpoint`/`OTLP_BRIDGE_OTLP_ENDPOINT` and fleet API env/flag
overrides remain for standalone use.

### 5. Security considerations

- Rig telemetry gRPC is plaintext/unauthenticated on the site LAN — the
  established trust model, unchanged.
- The enrichment RPC exposes device/site topology → shared bearer token,
  constant-time compare, refuse-when-unconfigured, single Unauthenticated
  code for missing/wrong token.
- The external Prometheus's OTLP receiver is typically unauthenticated;
  keeping it off the untrusted network is the operator's responsibility
  (it is no longer part of this stack). The bridge config file holds the
  fleet API key and is written with mode `600`.
- Malformed rig payloads fail that batch's decode and are logged and
  skipped (upstream behavior).

### 6. Failure modes

| Failure | Behavior |
|---|---|
| fleet-api down / RPC failing | Scan logs the error and keeps current rigs streaming; targets refresh when the RPC recovers. |
| Rig unreachable | Skipped at probe, retried next scan; established streams reconnect with backoff. |
| External Prometheus down/unreachable | Bounded upload queue, drop-newest with logged drops (upstream behavior). |
| Missing/blank `metrics_endpoint` | Bridge refuses to start (config validation). |
| Bridge restart | Gap for the restart duration; no replay exists upstream. Streams re-established from the first scan. |
| Device unpaired | Dropped from the RPC response → worker stopped next scan. |
| Device re-sited/re-racked | Label change → worker restart → new series under new labels. |
| Missing bridge token | Bridge refuses to start (config validation) — a misconfigured sidecar fails fast instead of looping on Unauthenticated. |
| Wrong bridge token | RPC returns Unauthenticated; bridge keeps retrying and logs each scan. |

### 7. Capacity

~150 series/rig × 500 rigs ≈ 75 k series / 7.5 k samples/s — small for a
single Prometheus (≲2 GB RSS, ~15–30 GB disk for 15 d), sizing guidance
for whoever operates the external instance. The bridge itself is the
same binary already proven at site scale in the standalone deployments.

## Testing

1. **Bridge unit/integration tests** (vendored upstream tests plus new
   ones): fleet-mode discovery against an in-process fake
   RigTelemetryService + fake rig REST/gRPC listeners (labels incl.
   hostname, empty-context omission, bad-token → empty), config validation
   for fleet mode, registry restart-on-label-change.
2. **Handler tests**: token required / wrong / unconfigured (all
   Unauthenticated through the production error-mapping interceptor),
   driver default, row→proto mapping.
3. **Fake rig**: `fake-proto-rig` serves the telemetry stream gRPC
   (`TELEMETRY_GRPC_PORT`, default 2123) with jittered fixtures honoring
   `ERROR_TEMPERATURE`; its stub copy is generated by the same upstream
   script.
4. **Dev-stack validation** (performed): `dev-rig-telemetry` set with 5
   fake rigs and a host-run Prometheus (`--web.enable-otlp-receiver`) —
   bridge discovers targets via the RPC, streams, and the external
   Prometheus serves fresh series labeled with `device_identifier`/
   `hostname`/`rig_ip`; a request without the token is rejected
   Unauthenticated.
5. **Sim/pilot validation**: as before — miner-firmware Docker sim, then a
   side-by-side site pilot gating decommission of the standalone stacks.

## Rollout

1. **PR A (miner-firmware):** `generate-fleet-stubs.sh` (parameterizable
   module path) — no firmware/wire changes.
2. **PR B (proto-fleet):** everything here (vendored bridge + RPC +
   overlays + CI). Feature off by default everywhere; dev opt-in via the
   `dev-rig-telemetry`/`dev-monitoring` recipes.
3. **Pilot** at one site alongside the standalone stack (series parity,
   resource usage, dashboards), then repoint miner-firmware tooling and
   decommission the standalone site stacks. `tools/telemetry/` remains a
   lab tool and the canonical home of the bridge source; re-vendoring
   follows the `proto-rig-api/VERSION.md` procedure.

## Alternatives considered

- **In-fleetd ingest (previous iteration, archived on
  `archive/rig-telemetry-fleetd-ingest`).** A fleetd domain service ported
  from the bridge with direct DB enrichment. Fewer moving parts (no
  sidecar, no RPC, no token), but it couples stream fan-out lifecycle,
  goroutines, and OTLP dependencies into the fleetd process, and any
  ingest bug or resource leak shares fleet-api's blast radius. The sidecar
  keeps fleetd's only new surface a read-only RPC, isolates the pipeline's
  failure domain, and keeps the bridge code re-vendorable nearly verbatim.
- **Store in TimescaleDB** — rejected for cardinality/rewrite cost (full
  PromQL ecosystem reuse wins); unchanged from the earlier analysis.
- **Bridge queries the DB directly** — a second service with DB
  credentials and schema coupling; the RPC keeps the contract explicit and
  fleet-api the only DB client.
- **Reusing existing operator RPCs instead of a new one.** The closest is
  `fleetmanagement.v1.ListMinerStateSnapshots` (device_identifier,
  ip_address, pairing_status, driver_name, site/building/rack labels), but
  it cannot cover the contract: `api_port` is exposed by no client-facing
  RPC (`MinerStateSnapshot.url` deliberately omits ports), `zone` lives
  only on `RackInfo` in `DeviceSetService` (device→rack→zone stitching
  across extra paginated calls, `PermRackRead`), `api_scheme` is only
  embedded in the `url` string, there is no server-side driver filter, and
  fleet-node-owned devices are not excluded. Decisively, those RPCs
  require an API key — user-backed, org-scoped, RBAC-resolved, and
  mintable only interactively by an operator with `PermAPIKeyManage`; no
  non-interactive bootstrap path exists for a headless sidecar. Closing
  the gaps would mean extending operator-facing protos (port, zone,
  driver filter), teaching the bridge pagination + multi-RPC stitching
  (growing divergence from upstream), and inventing API-key bootstrap —
  strictly more new surface than the one internal RPC.
- **API-key auth for the RPC** — fleet API keys are org-scoped operator
  credentials requiring interactive provisioning; the shared-token pattern
  matches the existing alertmanager webhook and is provisioned
  automatically by `run-fleet.sh`.
- **Making the rig protos buf-conformant / rig-side OTLP push** — as
  before: wire-breaking or firmware-gated; not the path to first
  ingestion.

## Open questions

1. **Multi-org installs:** stamp an `org` label? Deferred until a real
   multi-org deployment exists.
2. **`hostname` vs `device_identifier` long-term:** dashboards key on
   `hostname` today; once canonical in proto-fleet they could key on
   `device_identifier`, letting the bridge drop the REST lookup.
3. **Alerting (deferred entirely):** Grafana-provisioned PromQL rules vs
   Prometheus-side rules, whenever taken up.
4. **Upstreaming fleet mode:** should `fleet_targets.go` + the
   label-restart change flow back to miner-firmware's bridge so the two
   copies stay identical?
