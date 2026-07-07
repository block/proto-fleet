# rig-otlp-bridge

The rig telemetry sidecar: streams OTLP metrics from paired proto rigs'
on-rig `telemetry-service` gRPC endpoints to an external Prometheus
(OTLP receiver) named by `metrics_endpoint` in its config file, with
fleet context stamped onto every series. Runs next to fleet-api under the
rig telemetry compose overlay (see
`docs/plans/2026-07-02-rig-otlp-telemetry-ingest-tdd.md`).

## Provenance

Vendored from the private miner-firmware repository,
`tools/telemetry/otlp-bridge/` (see `proto-rig-api/VERSION.md` for the
pinned commit and re-vendoring procedure). Fleet-local divergences from
upstream, kept deliberately small so re-vendoring stays cheap:

- **Removed:** the `push` MCAP-backfill subcommand (`push_mcap*.go`) and
  its dependencies; the miner-firmware bundled-default-config test.
- **Import paths:** on-rig API stubs live at
  `internal/rigapi/minertelemetry`, generated in miner-firmware by
  `tools/telemetry/otlp-bridge/generate-fleet-stubs.sh`.
- **Added `fleet_targets.go` (+ config/flags):** fleet mode. When
  `fleet_api_url` is configured (`--fleet-api-url` /
  `OTLP_BRIDGE_FLEET_API_URL`), targets and enrichment labels come from
  proto-fleet's `fleetmanagement.v1/ListMinerStateSnapshots` (paginated;
  the existing `models` filter via `fleet_target_models`, default the
  proto rig model; fleet API key via `OTLP_BRIDGE_FLEET_API_TOKEN`)
  instead of subnet scanning. The rig REST scheme/port for identity and
  hostname lookups is fixed per deployment (`api_scheme`/`api_port`).
  Transient probe/hostname failures keep an already-streaming rig's
  previous info for the same device only, and only within a 10-minute
  re-verification grace window. Client stubs are the server's own generated
  Connect stubs, shared via the `../generated/grpc` nested module
  (replace directive) — no bridge-local codegen.
- **`registry.replace` restarts workers on label changes** (stop-before-
  start in the scan loop), so re-sited/re-racked rigs start new series
  with fresh context. A failed fleet listing is an error that skips
  reconciliation entirely — it never tears down streaming workers.
- **Config file optional** when a fleet API URL is provided via env/flag;
  fleet mode requires a non-empty token (fail-fast at startup).
- **`metrics_endpoint` config field replaces upstream's bundled-Prometheus
  default:** fleet ships no metrics store, so the OTLP receiver URL is
  required (config file or `--otlp-endpoint` / `OTLP_BRIDGE_OTLP_ENDPOINT`)
  and validated at startup.
- **Rig REST client skips TLS verification** (`rigAPIHTTPClient` in
  discovery.go): fleet discovery records real rigs as https with
  self-signed certs, matching the proto plugin's intentional
  InsecureSkipVerify. An empty hostname from the rig is an error in both
  modes (empty labels are invisible to the dashboards).
- **Reconnect backoff resets after a healthy stream** (stream survived
  past the backoff ceiling), instead of inheriting the max delay forever —
  a fix worth upstreaming.
- **`--metric-queue-expected-rigs` / OTLP_BRIDGE_METRIC_QUEUE_EXPECTED_RIGS**
  override, since fleet mode has no target list to size queues from.
- **Dockerfile runtime stage uses alpine** (non-root user) instead of
  upstream's gcr.io distroless base.
- **Injected labels override same-key rig attributes** (upstream lets the
  rig win): fleet identity labels must not be spoofable by rig payloads.
- **Fleet-mode targets are routability-checked** (no loopback/link-local/
  multicast) and the per-message OTLP receive cap is explicit (4 MiB).
  Targets are also **identity-checked**: the live rig's `cb_sn` (from the
  unauthenticated pairing-info endpoint) must match fleet's recorded
  serial, so a reused IP cannot stream under another device's labels.
  `fleet_target_cidrs` / `OTLP_BRIDGE_FLEET_TARGET_CIDRS` restricts
  targets to site LAN prefixes, defaulting to the private ranges
  (RFC 1918 + ULA) so a poisoned fleet record cannot point the bridge
  at public hosts; sites with public-IP rigs must set it explicitly.
- **Uploaders flush early on a pending count/byte cap** so a fast rig
  stream cannot grow between-tick memory unbounded, the tick/shutdown
  drain flushes in budget-bounded chunks, and the per-batch split-retry
  fallback has a 30s budget (a wedged receiver cannot stall the loop
  for queue-length × timeout) — worth upstreaming.
- **Rig REST responses are size-bounded and hostnames validated** (DNS
  charset, 253 max) before promotion to labels; duplicates of
  bridge-owned attribute keys are stripped, not just first-instance
  overridden.
- **Subnet-scan expansion is capped at a /16-equivalent** (validation
  rejects larger CIDR sets before the scanner materializes them).
  `fleet_api_url` accepts https; plain http is loopback-only unless
  `fleet_api_insecure_http` opts in (the API key rides every request).

Subnet/target scanning still works (config file as upstream), so the
binary remains usable standalone.

## Development

Standalone Go module (not in `go.work`):

```bash
GOWORK=off go build ./...
GOWORK=off go test ./...
```

The dev compose overlay (`just dev-rig-telemetry` / `just dev-monitoring`)
builds this directory's Dockerfile and mounts `dev-config/config.json`,
which points at a Prometheus you run on the host
(`host.docker.internal:9090`, started with `--web.enable-otlp-receiver`);
production ships a prebuilt binary packaged by
`deployment-files/server/Dockerfile.rig-otlp-bridge` configured via
`rig-telemetry/config.json` in the deployment directory.
