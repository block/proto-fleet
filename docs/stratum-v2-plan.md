# Stratum V2 Support — Design Plan (v1)

## Summary

Add Stratum V2 (SV2) support to proto-fleet: the ability to configure SV2 pools, push them to miners that natively speak SV2, and transparently bridge SV1-only miners through a bundled translation proxy. Job Declaration (miner-chosen transactions) is deferred to v2 but the schema and config surface are shaped so it is additive, not a rewrite.

v1 ships end-to-end pool assignment over SV2: schema, validation, URL rewriting at command build time, and a bundled SRI translation proxy as a new Docker Compose service. v2 adds a Job Declarator Client alongside the proxy and lets operators point it at their own `bitcoin-core-sv2` node. v3+ explores bundled Core, multi-site proxy topology, and pool-side integrations.

## Goals

- Operator can set a pool's protocol (`SV1` or `SV2`) via the existing MiningPools UI and API.
- Native-SV2-capable miners (as firmware providers ship support) connect directly to SV2 pools.
- SV1-only miners reach SV2 pools through a bundled translation proxy, with URL rewriting handled server-side at command build time — the pool record stores the logical (SV2) URL and never needs to be kept in sync with proxy configuration.

> **Note on protoOS and SV2.** The current protoOS firmware (miner-firmware repo, `crates/mcdd`) speaks Stratum V1 only — it uses the `sv1_api` Rust crate for all pool messaging; there is no Noise handshake or SV2 wire code. `PoolProtocol::StratumV2` exists in protoOS's RPC enum but is only used to report the URL scheme back to callers; pointing protoOS at a `stratum2+tcp://` URL directly would fail. Practically, **every protoOS miner takes the tProxy path** for SV2 pools in v1. The Proto plugin's capability probe is forward-looking: it returns `Unsupported` today, and flips to `Supported` when protoOS ships native SV2.
- Per-plugin capability flag (`stratum_v2_native`) gates the direct-vs-proxy decision; drivers own "does this firmware actually speak SV2".
- Translation proxy runs as an optional Docker Compose service with its own config volume, health probe, and versioned image pin.
- The protocol enum is reserved on the proto so v2's Job Declaration is additive — no migration churn on existing pool rows.
- E2E: assign an SV2 pool to a mixed fleet; native-SV2 miners connect direct, SV1 miners connect via the proxy, both report active mining in telemetry.

## Non-goals (explicit)

- **No Job Declaration Protocol in v1.** Miners don't pick transactions yet. Schema reserves the endpoint; the JDC service is not deployed.
- **No bundled Bitcoin Core / template provider.** When v2 ships JDC, operators point it at their own `bitcoin-core-sv2` node via config. Shipping Core is a v3+ conversation.
- **No Fleet-side SV2 protocol implementation.** Fleet integrates with the SRI binaries as opaque services; we don't write SV2 wire code in Go.
- **No pool-side components.** JDS, SV2 Pool Server, and mempool sync are the pool operator's problem.
- **No multi-site / multi-tenant proxy topology.** One bundled proxy per Fleet deployment, one upstream SV2 pool per proxy instance. Multi-pool fleets are a documented limitation; spin up multiple proxies manually if needed.
- **No runtime proxy reconfiguration.** Proxy upstream is set at deploy time via the mounted TOML config. Changing it requires a container restart.
- **No SV2 for pool operators.** We are a miner fleet manager; running a pool is out of scope.
- **No HA posture beyond what the server has today.** Single-instance proxy, single-instance Fleet.

## Design overview

SV2 slots into the existing pool assignment path without a new domain package. The proto gains a protocol enum, the SDK gains a capability flag, the command execution service gains a URL rewriter, and the deployment gains an optional sidecar service.

```
┌─────────────────────────────────────────────────────────────┐
│  PoolsService (existing)                                    │
│  List · Create · Update · Delete · ValidatePool             │
│    + pool.protocol ∈ {SV1, SV2}                             │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  MinerCommandService.UpdateMiningPools (existing)           │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ poolUrlForDevice(pool, device, proxyCfg):            │   │
│  │   SV1 pool          → pool.url                       │   │
│  │   SV2 pool, native  → pool.url                       │   │
│  │   SV2 pool, SV1 dev → proxyCfg.MinerURL (rewrite)    │   │
│  │   SV2 pool, SV1 dev, proxy off → error               │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────┬──────────────────────────┬───────────────────┘
               │                          │
               ▼                          ▼
     ┌────────────────┐        ┌──────────────────────┐
     │ native SV2     │        │ SV1 miner            │
     │ miner          │        │ (pointed at tproxy)  │
     └───────┬────────┘        └──────────┬───────────┘
             │                            │
             │                            ▼
             │              ┌─────────────────────────┐
             │              │ sv2-tproxy (NEW)        │
             │              │ SRI translator-proxy    │
             │              │ docker-compose service  │
             │              │ listens :34255          │
             │              └────────────┬────────────┘
             │                           │
             └──────────┬────────────────┘
                        ▼
              ┌─────────────────────┐
              │ SV2 Pool            │
              │ (operator's choice) │
              └─────────────────────┘
```

**What we reuse, unchanged:**

- `PoolsService` CRUD shape (`proto/pools/v1/pools.proto:9-24`) — only new fields, no new RPCs.
- `MinerCommandService.UpdateMiningPools` — same request shape, pool lookup is the only branch point.
- Per-vendor plugin drivers and the capabilities map (`server/sdk/v1/interface.go:485-523`). SV2 support is just a new capability bit.
- Docker Compose + `extends` pattern (`server/docker-compose.base.yaml`, `deployment-files/docker-compose.yaml`). The proxy is just another service.
- Kong-tagged config (`server/cmd/fleetd/config.go`). The proxy config block is a peer of `Plugins`, `Telemetry`, etc.

**What is genuinely new:**

1. A URL rewriter in the command build path — pure function of (pool, device capabilities, proxy config).
2. A bundled `sv2-tproxy` service in the compose file, with a mounted TOML config and a TCP health probe.
3. A `StratumV2` Kong config block exposing `Enabled`, `MinerURL`, `UpstreamPool`, plus TLS knobs.
4. UI surfaces: protocol selector on pool form, compatibility warnings on pool assignment.

## URL scheme

SV2 has no canonical URL form in the spec. We adopt and document:

- `stratum2+tcp://host:port[/AUTHORITY_PUBKEY]` — plain Noise-protected SV2.

Rationale: `stratum2+tcp://` is what Braiins Pool documents and what operators paste from pool-operator instructions. The Stratum V2 spec itself does not mandate a URL syntax — SRI's tProxy config takes bare `address` / `port` / `authority_pubkey` fields — but Braiins' documented `stratum2+tcp://HOST:PORT/PUBKEY` is the closest thing to a canonical operator-facing rendering. Validation is keyed off the URL scheme — `stratum+tcp` is SV1, `stratum2+tcp` is SV2. We do not try to auto-detect.

Plain TCP only in v1: TLS-wrapped variants (`stratum+ssl://` / `stratum2+ssl://`) and the WebSocket variant (`stratum+ws://`) are intentionally rejected by both the CEL rule and `rewriter.ProtocolFromURL`. The dispatch path uses bare `net.Dial` for SV1 and explicit host:port TCP for the SV2 Noise handshake; advertising `ssl`/`ws` schemes the runtime does not implement would just result in pools the API said were valid that fail at dispatch. TLS support is a v1.5 follow-up if operator demand materialises.

## API

### Proto changes — `proto/pools/v1/pools.proto`

```proto
enum PoolProtocol {
  POOL_PROTOCOL_UNSPECIFIED = 0;  // treated as SV1 (backward compat for existing rows)
  POOL_PROTOCOL_SV1         = 1;
  POOL_PROTOCOL_SV2         = 2;
}

message PoolConfig {
  string url                            = 1;
  string username                       = 2;
  google.protobuf.StringValue password  = 3;
  string pool_name                      = 4;
  PoolProtocol protocol                 = 5;  // NEW — defaults to SV1 semantics
  // Reserved for v2 (Job Declaration):
  // string jds_endpoint                = 6;  // Job Declarator Server endpoint
  // bool   job_declaration_enabled     = 7;
}

message Pool {
  int64 pool_id        = 1;
  string url           = 2;
  string username      = 3;
  string pool_name     = 4;
  PoolProtocol protocol = 5;  // NEW
  // Reserved for v2: 6, 7 (see PoolConfig)
}

message UpdatePoolRequest {
  int64                       pool_id   = 1;
  optional string             pool_name = 2;  // CHANGED — proto3 explicit presence
  optional string             url       = 3;  // CHANGED
  optional string             username  = 4;  // CHANGED
  google.protobuf.StringValue password  = 5;  // already wrapped
  optional PoolProtocol       protocol  = 6;  // NEW
}
```

`UpdatePoolRequest` becomes consistently patch-shaped. The existing string fields used the empty-string-means-unchanged convention — a well-known footgun that made it impossible to legitimately set a field to `""`. Since we're regenerating the proto for SV2 anyway, fix it once: every optional field uses proto3 explicit presence.

Semantics for every optional field:

- absent → leave stored value as-is.
- present and valid → set it.
- present and invalid (e.g. `PoolProtocol.UNSPECIFIED`, empty `url`) → handler rejects with `INVALID_ARGUMENT`.

Server-side migration: the handler loses its current "if input == '' leave unchanged" branches in favor of "if !msg.HasPoolName() leave unchanged." This is a behavior change for one edge case — callers that relied on empty-string-means-unchanged now need to not set the field at all — but existing Fleet clients don't exercise that edge case and external API callers get a clearer contract. Documented in the v1 release notes.

Validation today is a single regex at `pools.proto:135` that hardcodes `stratum+(tcp|ssl|ws)://`. Replace with a CEL rule that branches on `protocol`:

```proto
message ValidatePoolRequest {
  option (buf.validate.message).cel = {
    id: "pool_url_matches_protocol",
    message: "url scheme must match protocol",
    expression:
      "(this.protocol == 1 || this.protocol == 0) "
      "? this.url.matches('^stratum\\\\+(tcp|ssl|ws)://...') "
      ": this.url.matches('^sv2\\\\+(tcp|ssl)://...')"
  };
  // ... existing fields
  PoolProtocol protocol = 5;
}
```

(CEL expression abbreviated; same host/port sub-patterns as today.)

### `ValidatePool` behavior for SV2

Current `pools.Service.ValidatePool` (`server/internal/domain/pools/service.go:188`) runs `stratumv1.Authenticate` — a full SV1 `mining.subscribe` + `mining.authorize` probe that proves credentials work. The same RPC powers the "Test connection" button in the Mining Pools UI, so whatever we pick here is the behavior operators get when they test an SV2 pool.

**v1 decision: SV2 `ValidatePool` performs a TCP dial with timeout, nothing more.** The response makes that explicit rather than burying it in a string field.

Why not deeper:
- A full SV2 probe requires a Noise-NX handshake + `SetupConnection` roundtrip. Implementing a minimal SV2 client in Go for the sole purpose of a validation button is weeks of work and ongoing maintenance against an evolving SRI — out of scope for v1.
- A proxy-mediated probe (dial the tProxy and let it upstream) only works when `ProxyEnabled=true` and when the proxy happens to be configured for *this specific upstream pool*, which is almost never the case at pool-creation time.
- Syntax-only validation (regex match, no network) is what the operator already got by filling out the form. The button has to do *something* observable, or it's a lie.

TCP dial catches the 80% case (typo'd host, wrong port, firewall block) honestly. A deeper probe is a v1.5 fast-follow — added as a new `ValidationMode` value without changing the RPC shape.

The response message gains explicit booleans plus a typed mode so the UI can render "reachable but credentials unverified" without parsing string conventions:

```proto
enum ValidationMode {
  VALIDATION_MODE_UNSPECIFIED   = 0;
  VALIDATION_MODE_SV1_AUTHENTICATE = 1;  // existing SV1 subscribe+authorize
  VALIDATION_MODE_SV2_TCP_DIAL     = 2;  // v1 SV2 default
  VALIDATION_MODE_SV2_HANDSHAKE    = 3;  // v1.5 follow-up: Noise + SetupConnection
}

message ValidatePoolResponse {
  bool           reachable            = 1;  // TCP (and handshake if mode > TCP_DIAL) succeeded
  bool           credentials_verified = 2;  // only set when mode authenticates
  ValidationMode mode                 = 3;  // what was actually attempted
}
```

Semantics: `reachable=true, credentials_verified=false, mode=SV2_TCP_DIAL` is the typical v1 SV2 success. The UI renders a distinct "connected, credentials unverified" state when `reachable && !credentials_verified`, matching what actually happened on the wire.

### Proto changes — `proto/minercommand/v1/command.proto`

Raw pool assignments (pool configured on-miner but not in Fleet's DB) flow through `RawPoolInfo` on `PoolSlotConfig`. Without a protocol field there, raw SV2 assignments are unrepresentable — and since Decision §1 says we never auto-detect protocol from a URL, we must add it:

```proto
message RawPoolInfo {
  string url              = 1;
  string username         = 2;
  optional string password = 3;
  PoolProtocol protocol   = 4;  // NEW; UNSPECIFIED treated as SV1 for back-compat
}
```

`UpdateMiningPoolsRequest` itself does not change shape — the per-slot protocol travels inside each `PoolSlotConfig` (via the pool-ID lookup or the raw pool info).

### Proto changes — `server/sdk/v1/pb/driver.proto`

```proto
message MiningPoolConfig {
  int32  priority   = 1;
  string url        = 2;   // already opaque; rewriter fills with proxy URL when needed
  string worker_name = 3;
  PoolProtocol protocol = 4;  // NEW — lets drivers log / branch when useful
}

message ConfiguredPool {
  int32  priority = 1;
  string url      = 2;
  string username = 3;
  PoolProtocol protocol = 4;  // NEW
}
```

Driver-side: most plugins treat URL as opaque and don't need to care. The field is informational for logging and future vendor-specific behavior (e.g. a plugin might surface "this pool is SV2 but firmware is SV1" in its own telemetry).

### Internal DTOs — threading protocol through the Go layer

The proto-level additions are not sufficient on their own; protocol must be carried across Fleet's internal pool types or the worker-name reapply path (which rebuilds pool payloads from current miner state at `server/internal/domain/command/execution_service.go:549`) will drop SV2 intent. In scope for v1:

- `server/internal/domain/miner/dto/command_dto.go:16` — `MiningPool` gains `Protocol PoolProtocol`.
- `server/internal/domain/miner/interfaces/miner.go:72` — `MinerConfiguredPool` gains `Protocol PoolProtocol` (so reapply round-trips preserve protocol).
- `server/internal/domain/plugins/plugin_miner.go:275` — `UpdateMiningPools` conversion copies the field onto the SDK struct.
- `server/internal/domain/command/capability_checker.go:202` — capability checks that run before dispatch gain access to the per-device capability set so the rewriter/preflight can consult it.

None of these are user-facing; they are the "thread the field through" work that keeps the roundtrip honest. Miss any one of them and the reapply path silently rewrites SV2 pools as SV1.

### SDK changes — `server/sdk/v1/interface.go`

```go
const (
    // ... existing capabilities ...
    CapabilityStratumV2Native = "stratum_v2_native"
)
```

Per Decision §6, SV2 support is a **per-device** property, not a per-driver or per-model one — firmware can be upgraded in place and the capability should follow. `DescribeDriver()` is therefore the wrong layer; it answers "what can this driver do in the abstract," not "what does the firmware on device X report today."

SV2 is currently the only dynamic capability we know we need. A generic `DynamicCapabilityReporter` interface + new RPC would be future-flexible but is overkill when one bit is at stake — the machinery would outweigh the signal it carries. Instead, v1 attaches the bit directly to the existing telemetry snapshot, which already flows from plugin to server on every scrape:

```go
// DeviceMetrics (interface.go:167) gains a single optional field:
type DeviceMetrics struct {
    // ... existing fields ...

    // StratumV2Support reports whether the firmware on this device natively
    // speaks SV2, as observed at the moment of this telemetry scrape.
    // Plugins that cannot probe for this leave it Unknown; the server falls
    // back to the merged static/model view in that case.
    StratumV2Support StratumV2SupportStatus
}

type StratumV2SupportStatus int32
const (
    StratumV2SupportUnknown     StratumV2SupportStatus = 0  // plugin did not probe
    StratumV2SupportUnsupported StratumV2SupportStatus = 1  // firmware confirmed SV1-only
    StratumV2SupportSupported   StratumV2SupportStatus = 2  // firmware confirmed SV2-capable
)
```

This is strictly additive, non-breaking (the zero value means "no opinion, fall back to static"), costs one field in the telemetry proto and the `DeviceMetrics` struct, and reuses the telemetry scrape cache that the server already maintains. No new RPC, no new provider interface, no new detection pattern.

**Capability merge.** The rewriter and preflight consult a merged view: start from the static driver capabilities, overlay `ModelCapabilitiesProvider` if present, then overlay the telemetry-reported `StratumV2Support` when it is not `Unknown`. Telemetry wins on conflict. The "merged view" helper is a small pure function next to the rewriter.

**When the generic abstraction arrives.** If a second dynamic capability shows up (curtailment-support-per-firmware, Stratum V3, etc.), revisit — at two bits the generic-provider case becomes stronger than the per-bit-on-telemetry case. Until then, one field in telemetry is the smallest correct thing.

## URL rewriting — the core logic

Single pure function, lives in `server/internal/domain/pools/rewriter.go` (new file). Called from `server/internal/domain/command/execution_service.go` at the point where `UpdateMiningPools` builds the per-device command.

```go
type ProxyConfig struct {
    ProxyEnabled bool
    MinerURL     string  // what miners on the LAN connect to
}

type DeviceCapabilities interface {
    Has(capability string) bool
}

func PoolURLsForDevice(slots []PoolSlot, caps DeviceCapabilities, proxy ProxyConfig) ([]ResolvedSlot, error) {
    // Each slot resolves independently...
    resolved, err := resolvePerSlot(slots, caps, proxy)
    if err != nil {
        return nil, err
    }
    // ...but if the device requires the proxy for more than one slot, we reject:
    // the single bundled proxy has exactly one upstream pool, so pointing two SV2
    // pool slots at the same proxy URL silently collapses primary/backup semantics.
    // Operators with multi-SV2 backup fleets need either native-SV2 miners for the
    // backup slots or must wait for multi-proxy support (v4). See "Known limitations".
    if countProxiedSlots(resolved) > 1 {
        return nil, ErrMultipleSV2SlotsRequireProxy
    }
    return resolved, nil
}

func resolveSingle(pool Pool, caps DeviceCapabilities, proxy ProxyConfig) (string, error) {
    switch pool.Protocol {
    case PoolProtocolSV1, PoolProtocolUnspecified:
        return pool.URL, nil
    case PoolProtocolSV2:
        if caps.Has(CapabilityStratumV2Native) {
            return pool.URL, nil
        }
        if proxy.ProxyEnabled {
            return proxy.MinerURL, nil
        }
        return "", ErrSV2PoolNotSupportedByDevice
    default:
        return "", fmt.Errorf("unknown pool protocol: %v", pool.Protocol)
    }
}
```

The rewriter operates on the full slot set (default + backup_1 + backup_2) rather than one pool at a time, specifically so it can enforce the "at most one proxied slot per device" rule. Without it, a mixed fleet where an operator configures three SV2 pools as primary+backups for SV1 miners would push three identical proxy URLs to each miner and silently lose both backups. The rewriter rejects the batch with `FAILED_PRECONDITION: multiple_sv2_slots_require_proxy` and the preview RPC surfaces the same warning per-device.

**Why rewrite at command build time rather than at pool save time:**

- Pool rows stay as single source of truth. If an operator later upgrades a miner's firmware and the plugin starts reporting `stratum_v2_native`, the next `UpdateMiningPools` dispatch routes direct instead of via the proxy — no manual pool update needed.
- If the proxy is reconfigured (new `MinerURL`), every next command picks up the new value. No stale URLs written to devices until the next push.
- If a plugin is downgraded and loses the capability, the next push routes via the proxy. Same story in reverse.
- Keeps `Pool` free of deployment-specific concerns (proxy endpoints are a property of the deploy, not the pool).

**Capability mismatch with proxy disabled — synchronous preflight with per-device payloads.** The current `UpdateMiningPools` RPC (`server/internal/domain/command/service.go:722`) enqueues an async batch and returns before per-device dispatch runs, so a naive "fail with `FAILED_PRECONDITION`" claim would never fire synchronously. v1 adds an explicit preflight step at the top of `command.Service.UpdateMiningPools`, before the batch is created in `processCommand` (`service.go:495`):

1. Resolve `DeviceSelector` to a concrete device list.
2. Load each device's merged capability set (driver static ∪ model ∪ dynamic).
3. Run the rewriter against every (device, slot) pair to produce the resolved per-device pool payload.
4. If any pair returns `ErrSV2PoolNotSupportedByDevice` or the device-level `ErrMultipleSV2SlotsRequireProxy`, reject the whole request with `FAILED_PRECONDITION` and a structured `mismatches: [{device_identifier, slot, reason}]` payload. The batch is never enqueued.
5. Otherwise, enqueue with the resolved per-device payloads written into each device's `queue_message.payload`.

**Queue payload contract change.** The current queue API (`server/internal/infrastructure/queue/interface.go:23`, `server/internal/infrastructure/queue/service.go:31`) takes one payload and writes the same marshaled bytes against each device row. v1 extends the contract so `Enqueue` accepts per-device payloads:

```go
// Before (retained for non-per-device commands like StartMining):
Enqueue(ctx, batchID, commandType, deviceIDs, payload []byte) error

// Added — per-device payloads:
EnqueuePerDevice(ctx, batchID, commandType, payloads map[string][]byte) error
```

The underlying `queue_message` row already has its own `payload` column per device, so the storage change is zero — only the interface gains a variant that writes distinct bytes per row. `UpdateMiningPools` switches to `EnqueuePerDevice`; every other command keeps using `Enqueue` unchanged.

**This removes the dispatch-time rewriter entirely.** `execution_service.go:412` unmarshals the device's own payload and pushes it straight to the plugin — it never needs to consult pool state, capabilities, or proxy config again. The rewriter runs exactly once per request, at preflight, against a single consistent capability snapshot. Preview and commit produce identical URLs by construction (same code path, same inputs); there is no capability-flip race to document because the dispatch worker does not re-evaluate.

The preflight also stashes a `capability_snapshot_at` timestamp on the batch row for observability: if a dispatch fails with a capability-type error from the plugin itself (e.g. firmware rejects the SV2 URL), the activity log can correlate against when the snapshot was taken, and an operator can rerun against current state.

Reapply paths (worker-name reapply at `execution_service.go:549`) follow the same flow — they build per-device payloads from `MinerConfiguredPool` (which now carries protocol) through the preflight-and-resolve path, then enqueue per-device.

### Preview/preflight API — stable, typed, shared

The preview and preflight code paths are the single source of truth for pool-assignment behavior: which slot resolves to which URL, why a slot was rewritten, why a device was rejected. They are consumed by the web UI, the forthcoming CLI, integration tests, and the commit path itself (via the shared preflight). Because of that, the RPC is treated as a stable public API, not a UI helper — all reasons are typed enums, not strings.

```proto
rpc PreviewMiningPoolAssignment(PreviewMiningPoolAssignmentRequest)
    returns (PreviewMiningPoolAssignmentResponse);

message PreviewMiningPoolAssignmentResponse {
  repeated DevicePoolPreview previews = 1;
}

enum PoolSlot {
  POOL_SLOT_UNSPECIFIED = 0;
  POOL_SLOT_DEFAULT     = 1;
  POOL_SLOT_BACKUP_1    = 2;
  POOL_SLOT_BACKUP_2    = 3;
}

enum RewriteReason {
  REWRITE_REASON_UNSPECIFIED   = 0;
  REWRITE_REASON_PASSTHROUGH   = 1;  // SV1 pool, pushed as-is
  REWRITE_REASON_NATIVE        = 2;  // SV2 pool, device speaks native SV2
  REWRITE_REASON_PROXIED       = 3;  // SV2 pool, device is SV1, proxy URL substituted
}

enum SlotWarning {
  SLOT_WARNING_UNSPECIFIED             = 0;
  SLOT_WARNING_SV2_NOT_SUPPORTED       = 1;  // SV2 pool + SV1 device + proxy disabled
}

enum DeviceWarning {
  DEVICE_WARNING_UNSPECIFIED                 = 0;
  DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED  = 1;  // >1 SV2 slot would route through single bundled proxy
}

message SlotPreview {
  PoolSlot      slot               = 1;
  PoolProtocol  effective_protocol = 2;
  string        effective_url      = 3;  // what will actually be pushed to this slot
  RewriteReason rewrite_reason     = 4;
  SlotWarning   warning            = 5;  // UNSPECIFIED iff slot would succeed
}

message DevicePoolPreview {
  string               device_identifier = 1;
  repeated SlotPreview slots             = 2;  // one entry per slot present in the request
  // Device-scoped warning that is not attributable to a single slot —
  // a property of the combination of slots on this device, not of any one.
  DeviceWarning        device_warning    = 3;
}
```

The same typed warnings flow back on the commit path's `FAILED_PRECONDITION` detail payload — no string-to-enum conversion, no frontend branching on free-form text:

```proto
message UpdateMiningPoolsMismatch {
  string        device_identifier = 1;
  PoolSlot      slot              = 2;  // UNSPECIFIED for device-level mismatches
  SlotWarning   slot_warning      = 3;
  DeviceWarning device_warning    = 4;
}
```

**Package boundary.** The preview/preflight implementation lives in `server/internal/domain/pools/preflight/` as a reusable package with typed inputs and outputs. The Preview RPC handler and `command.Service.UpdateMiningPools` both call into it; the CLI and tests also depend on it directly.

The UI calls `PreviewMiningPoolAssignment` before enabling Save. No writes; pure projection over the rewriter.

## Data model

One column added; no new tables in v1.

### `pool` table migration

```sql
ALTER TABLE pool
  ADD COLUMN protocol TEXT NOT NULL DEFAULT 'sv1'
  CHECK (protocol IN ('sv1', 'sv2'));

-- For v2, reserved:
-- ALTER TABLE pool ADD COLUMN jds_endpoint TEXT;
-- ALTER TABLE pool ADD COLUMN job_declaration_enabled BOOLEAN NOT NULL DEFAULT FALSE;
```

Existing rows default to `sv1`, matching the `PoolProtocol.UNSPECIFIED → SV1` semantics on the proto side. `PoolProtocol.UNSPECIFIED` is not persisted — handlers normalize it to `sv1` on insert.

### No new tables for the proxy

Proxy configuration lives entirely in Kong-tagged config + the mounted TOML. There is no `proxy_instance` table in v1 because there is exactly one bundled proxy. When multi-proxy lands (deferred), the table becomes justified.

## SDK + plugin rollout

Per the capability-reporting design above, each plugin sets `StratumV2Support` on the telemetry snapshot it returns from `Status(ctx)` based on what it can probe live from the firmware. Plugins that can't probe leave it `Unknown` and the server falls back to the static/model view.

1. **Virtual plugin** — returns `Supported` when a plugin config toggle is set, so integration tests exercise both direct and proxied paths.
2. **Proto plugin** — probes the ProtoOS HTTP API (via `proto-rig-api`) for SV2 support on each telemetry cycle and sets the field accordingly. On current protoOS firmware the probe returns `Unsupported` (the firmware's stratum client is SV1-only); the probe is in place so the flip to `Supported` is zero-fleet-config when native SV2 ships.
3. **Antminer (stock / Braiins OS)** — probes firmware identifier strings returned during telemetry; Braiins OS returns `Supported`, stock returns `Unsupported`.
4. **asic-rs-backed plugins** — return `Supported` only when asic-rs explicitly reports SV2 support for the connected device. Default to `Unknown` (falls back to static driver capabilities).

No plugin needs to speak SV2 itself. The field is a claim about the firmware on the other end of the network, not about the plugin.

## Deployment

### New Compose service

`deployment-files/docker-compose.yaml` and `server/docker-compose.base.yaml` gain:

```yaml
sv2-tproxy:
  image: stratumv2/translator_sv2:<pinned-tag>@sha256:<pinned-digest>  # see "Known limitations"
  restart: unless-stopped
  profiles: ["sv2"]  # only started when operator opts in
  ports:
    - "34255:34255"
  volumes:
    - ./sv2/tproxy.toml:/etc/tproxy/config.toml:ro
  environment:
    - RUST_LOG=info
  healthcheck:
    test: ["CMD", "nc", "-z", "localhost", "34255"]
    interval: 30s
    timeout: 5s
    retries: 3
```

Profile-gated so the default `docker compose up` is unchanged for operators who don't want SV2. Operators enable with `COMPOSE_PROFILES=sv2` or `docker compose --profile sv2 up`.

Sibling config file `deployment-files/sv2/tproxy.toml` mounts into the container. Installer script templates it with the operator's upstream pool details.

### Server config block

`server/cmd/fleetd/config.go:43` gains, alongside `Plugins plugins.Config`:

```go
type StratumV2Config struct {
    ProxyEnabled         bool          `help:"Enable bundled SV2 translation proxy (for SV1 miners mining SV2 pools)" env:"PROXY_ENABLED"      default:"false"`
    ProxyMinerURL        string        `help:"What SV1 miners connect to (pushed on URL rewrite)"                     env:"PROXY_MINER_URL"    default:""`
    ProxyUpstreamURL     string        `help:"SV2 pool URL the proxy connects upstream to"                            env:"PROXY_UPSTREAM_URL" default:""`
    ProxyHealthCheckAddr string        `help:"tcp host:port for Fleet to probe proxy health"                          env:"PROXY_HEALTH_ADDR"  default:"127.0.0.1:34255"`
    ProxyHealthInterval  time.Duration `help:"Proxy TCP probe interval"                                               env:"PROXY_HEALTH_INTERVAL" default:"30s"`
}

type Config struct {
    // ... existing embeds ...
    StratumV2 StratumV2Config `embed:"" prefix:"stratum-v2-" envprefix:"STRATUM_V2_"`
}
```

**What the flag does — and doesn't do.** `ProxyEnabled` controls exactly one thing: whether the bundled translation proxy is part of the deployment and whether the rewriter may emit proxied URLs. It is not a master "SV2 support" gate:

| Action | `ProxyEnabled=true` | `ProxyEnabled=false` |
|---|---|---|
| Create an SV2 `Pool` record | allowed | **allowed** |
| Assign SV2 pool to a native-SV2 miner | allowed (direct route) | **allowed (direct route)** |
| Assign SV2 pool to an SV1 miner | allowed (proxy route) | rejected at preflight |

A fleet of only native-SV2 miners never needs to flip this on. The flag renamed from the earlier sketch's `Enabled` to `ProxyEnabled` to make the scope explicit — it's a deployment-topology switch, not a protocol switch.

Startup validation: if `ProxyEnabled=true`, `ProxyMinerURL` and `ProxyUpstreamURL` are required and the server refuses to start without them. If `ProxyEnabled=false`, all proxy fields are ignored and the `sv2-tproxy` Compose profile is not needed.

**Note on `ProxyHealthCheckAddr` default.** The packaged deployment runs `fleet-api` with `network_mode: host` (`deployment-files/docker-compose.yaml:24`), so Docker's service-name DNS (`sv2-tproxy:34255`) is not reachable from the API process. The proxy exposes port 34255 on the host, so the reachable address from fleet-api is `127.0.0.1:34255` — that is the default. Operators who deploy with bridge networking (non-default) override via `STRATUM_V2_PROXY_HEALTH_ADDR`.

### Installer integration

`deployment-files/install.sh` gains a one-shot prompt at install time:

```
Enable Stratum V2 translation proxy? [y/N]
  Upstream SV2 pool URL (stratum2+tcp://...): _
  Miner-facing proxy URL (stratum+tcp://...:34255): _
```

Operator can skip and enable later by editing `.env`. Uninstaller leaves the proxy config intact.

## Observability & audit

### Activity log

Reuses existing `activity_log` with new event types:

| Event type | Emitted when |
|---|---|
| `pool.sv2.created` | CreatePool with `protocol=SV2` |
| `pool.sv2.updated` | UpdatePool that changes `protocol` |
| `pool.assignment.proxied` | URL rewrite replaces pool URL with proxy URL for a device |
| `pool.assignment.rejected` | Capability mismatch with proxy disabled |
| `sv2.proxy.health_transition` | Health probe flips up/down |

### Metrics

- `proto_fleet_sv2_proxy_up` — gauge, 1/0 from the TCP probe.
- `proto_fleet_pool_assignment_rewrites_total{reason}` — counter, labels `native|proxied|passthrough|rejected`.
- `proto_fleet_pool_protocol_total{protocol}` — gauge, labels `sv1|sv2`, count of pools.

### Proxy logs

SRI tproxy logs to stdout; Compose picks them up. For v1 we do not ingest them into Fleet — operators get them via `docker compose logs sv2-tproxy`. Log shipping is deferred.

## Package layout

```
server/
  cmd/fleetd/
    config.go                          # + StratumV2Config block
  internal/
    domain/
      pools/
        rewriter.go                    # NEW — PoolURLsForDevice (per-slot set)
        rewriter_test.go               # NEW — exhaustive capability/protocol matrix
        service.go                     # honor protocol field; ValidatePool SV2 mode
        preflight/                     # NEW — shared preflight package
          preflight.go                 #   typed inputs/outputs, used by preview RPC, commit, CLI, tests
          preflight_test.go
      command/
        execution_service.go           # consume per-device payload (no rewrite at dispatch)
        service.go                     # call preflight; use EnqueuePerDevice
      sv2/
        proxy_health.go                # NEW — TCP probe loop (shared with ValidatePool SV2)
        proxy_health_test.go
    handlers/
      pools/
        handler.go                     # validate URL scheme vs protocol; UpdatePool patch semantics
      minercommand/
        handler.go                     # PreviewMiningPoolAssignment
    infrastructure/
      queue/
        interface.go                   # + EnqueuePerDevice variant
        service.go                     # per-device payload marshaling
  sdk/v1/
    interface.go                       # + CapabilityStratumV2Native, StratumV2SupportStatus on DeviceMetrics
    pb/driver.proto                    # + PoolProtocol on pool structs, + StratumV2Support on telemetry snapshot

proto/
  pools/v1/
    pools.proto                        # + PoolProtocol enum, + protocol field, CEL validation

client/
  src/shared/components/MiningPools/
    PoolForm/PoolForm.tsx              # protocol selector
    PoolForm/PoolForm.utils.ts         # URL scheme hints per protocol
  src/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/
    PoolSelectionPage.tsx              # capability warnings via preview RPC

deployment-files/
  docker-compose.yaml                  # + sv2-tproxy service (profile: sv2)
  sv2/
    tproxy.toml                        # default config, templated by installer
    README.md                          # operator setup notes
  install.sh                           # SV2 prompt
```

## Build sequence

1. **Proto foundations** — `PoolProtocol` enum; `protocol` field on `PoolConfig`, `Pool`, `ValidatePoolRequest`, `RawPoolInfo`, and the driver-side pool structs; `UpdatePoolRequest` migrated to proto3 explicit presence on every patch field; typed enums (`ValidationMode`, `PoolSlot`, `RewriteReason`, `SlotWarning`, `DeviceWarning`) + `SlotPreview`/`DevicePoolPreview`/`UpdateMiningPoolsMismatch` messages; `reachable`/`credentials_verified`/`mode` on `ValidatePoolResponse`; `StratumV2Support` on the telemetry snapshot; relaxed validation via CEL. Regenerate; existing tests still pass because `UNSPECIFIED → SV1` on reads.
2. **Internal DTO thread-through** — add protocol to `dto.MiningPool`, `interfaces.MinerConfiguredPool`, and the plugin-miner conversion at `plugin_miner.go:275`. `UpdatePool` handler migrates to presence-based patching (behavior change for callers relying on empty-string-means-unchanged; documented in release notes). Cover the worker-name reapply round-trip with a test that proves SV2 pools reapply as SV2.
3. **DB migration** — add `protocol` column with SV1 default, backfill is a no-op.
4. **SDK dynamic capability via telemetry** — `CapabilityStratumV2Native` constant; `StratumV2SupportStatus` field on `DeviceMetrics` and the telemetry proto; server-side merged-view helper (static ∪ model ∪ telemetry-reported, telemetry wins when not `Unknown`); capability cache keyed on telemetry scrape. Virtual plugin sets the field behind a config toggle. Strictly additive; no existing plugin interface changes.
5. **URL rewriter** — pure function operating on the full slot set + exhaustive unit tests covering every (protocol, capability, `ProxyEnabled`) combination plus the multi-SV2-slot rejection path.
6. **Queue per-device payloads** — add `EnqueuePerDevice(ctx, batchID, commandType, payloads map[string][]byte)` to the queue interface (`server/internal/infrastructure/queue/interface.go`) and service (`.../service.go`); existing `Enqueue` retained for one-payload-per-batch commands. Existing `queue_message.payload` storage is unchanged — only the write path gains a per-device variant. Tests cover heterogeneous payloads in the same batch.
7. **Shared preflight package + synchronous commit path** — `server/internal/domain/pools/preflight/` holds the typed preflight function (input: pool slots + device IDs; output: per-device `SlotPreview` list + typed warnings or `UpdateMiningPoolsMismatch` list). Both the Preview RPC handler and `command.Service.UpdateMiningPools` call it. On success, the commit path marshals per-device pool payloads and uses `EnqueuePerDevice`; dispatch in `execution_service.go` just unmarshals and pushes to the plugin.
8. **Preview RPC** — `PreviewMiningPoolAssignment` on `MinerCommandService` + handler; thin wrapper over the preflight package. Reusable by CLI and integration tests directly against the preflight package, not the RPC.
9. **Proxy config block** — Kong struct, env wiring, startup validation (reject empty `ProxyMinerURL` / `ProxyUpstreamURL` when `ProxyEnabled=true`). SV2 pool creation and native-SV2 assignment remain allowed regardless of this flag.
10. **Proxy health probe + SV2 `ValidatePool`** — shared TCP-probe helper. The proxy health loop runs on an interval; `ValidatePool` for SV2 calls the same helper and populates `reachable`/`credentials_verified`/`mode` accordingly. Activity log transitions for the proxy. Proxy probe starts/stops with `ProxyEnabled`.
11. **Docker Compose** — `sv2-tproxy` service under the `sv2` profile, default `tproxy.toml`, healthcheck.
12. **Installer prompt** — `install.sh` branches; writes `.env` entries and templates the TOML.
13. **UI — pool form** — protocol selector, URL-scheme hints, client-side validation mirroring the CEL rule; protocol editable on update; validation button renders the three-field response explicitly.
14. **UI — pool assignment** — call `PreviewMiningPoolAssignment` on scope change; switch on `RewriteReason`/`SlotWarning`/`DeviceWarning` enum values (no string parsing); block Save when any `SlotWarning` or `DeviceWarning` is set.
15. **Plugin SV2-support rollout** — Proto plugin (ProtoOS probe populates `StratumV2Support` per scrape), Antminer plugin (firmware-id inspection), asic-rs models that report SV2 support at telemetry time.
16. **E2E test** — mixed fleet (native + SV1), assign SV2 pool, verify telemetry shows both cohorts active; toggle proxy off, verify capability-mismatch rejection surfaces synchronously via typed enum; configure three SV2 pools on an SV1 miner, verify rejection via `DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED`.

Rough estimate: ~3–4 engineer-weeks for v1 if the sequence is followed linearly. Parallelizable: (1)–(4) are foundations for everyone; (6)–(7) (queue + preflight) are the critical path to unblock UI; (9)–(12) (deployment) can run in parallel with (13)–(14) (UI) once the preview RPC lands in step 8.

## Roadmap — how v1 enables each later phase

**v2 — Job Declaration Protocol (miner-chosen transactions)**

Reserved proto fields (`jds_endpoint`, `job_declaration_enabled` on `PoolConfig` and `Pool`) unlock without a migration rewrite.

New Compose service `sv2-jdc` (SRI Job Declarator Client), same profile-gated pattern as `sv2-tproxy`. tProxy config file flips its upstream from the pool to the JDC; JDC connects to the pool's JDS and to the operator's `bitcoin-core-sv2` node.

Operators supply a `TEMPLATE_PROVIDER_ENDPOINT` env var pointing at their own Core node. A new settings page surfaces JDC status (declared template count, fallback events, Core reachability). No changes to the rewriter or the per-device command path — JDC is transparent once tProxy upstream is redirected.

Pool fallback maps naturally to the existing backup pool slots in `UpdateMiningPoolsRequest` (`proto/minercommand/v1/command.proto:76-111`) — JDC's "pool rejected template, switch to fallback" event triggers the same mechanism, no new schema needed.

**v3 — Bundled Bitcoin Core**

Ship `bitcoin-core-sv2` as a profile-gated Compose service with its own volume. Deferred until there is concrete operator demand; Core adds ~600GB of storage plus IBD time plus upgrade coordination, and most mining operators already have a node.

**v4 — Multi-site / multi-pool proxy topology**

One tProxy per (site, pool) pair. Introduces a `proxy_instance` table keyed by `(site_id, pool_id)`, generalizes `StratumV2Config.ProxyMinerURL` into a per-site lookup. Rewriter gets an extra input (device's site) and returns the appropriate proxy URL. Additive — no existing field changes meaning.

**v5 — Proxy admin API integration**

When SRI exposes a stable admin HTTP API on tProxy/JDC, replace the TCP probe with richer status (connected miners, hashrate, declared templates). Surfaces as Fleet telemetry rather than just a health gauge.

**v6 — Fleet-native SV2 in ProtoOS**

Out of scope for this plan; tracked in the ProtoOS repo. Fleet's only concern is that the plugin declares `stratum_v2_native` when the firmware version supports it.

## Known limitations / trade-offs accepted for v1

1. **One upstream pool per proxy instance, at most one proxied slot per device.** A fleet pointing at two SV2 pools needs two proxies; v1 bundles one. For SV1 miners, this means the rewriter rejects any assignment that would proxy more than one slot (primary + backup) through the single bundled proxy — primary/backup semantics would otherwise collapse. Workarounds: use SV1 pools for the backup slots, use native-SV2 miners (unlimited SV2 slots), or run a second tProxy instance manually. Addressed properly in v4 (multi-proxy topology).
2. **Proxy config changes require a container restart.** `UpstreamPool`, `MinerURL`, Noise keys are baked into the mounted TOML. Hot reload is a later concern.
3. **SRI version pin is a maintenance burden.** SRI is post-1.0 but iterating. We pin a specific image tag, document the upgrade procedure, and accept that major SRI releases may need code on our side. Initial pin: latest 1.x release as of this plan.
4. **No Fleet-side observability of per-miner SV2 health.** We know the proxy is up and the miner is dispatching shares; we do not surface SV2-specific metrics (channel opens, extranonce rotations, job rejections). Future: ingest tProxy admin API.
5. **Capability detection is plugin-driven and therefore trust-based.** If a plugin incorrectly declares `stratum_v2_native` on firmware that doesn't support it, the miner will silently fail to connect. Mitigation: preview RPC shows which cohort each device fell into, and the pool assignment UI surfaces "zero miners mining" quickly via existing telemetry. No heuristic fallback in v1.
6. **No runtime multi-tenancy for the proxy.** The bundled proxy serves every SV1 miner in the fleet; there is no per-org isolation on the wire. Acceptable because Fleet's current deployment model is single-org anyway.
7. **Uninstall leaves proxy state.** The Compose volume for `sv2/tproxy.toml` persists across uninstalls by design (to preserve the operator's upstream config). Documented.
8. **SV2 `ValidatePool` is shallower than SV1.** SV1 does a full subscribe+authorize; SV2 does a TCP dial only. The response surfaces this explicitly via `reachable` + `credentials_verified` + `mode=SV2_TCP_DIAL`, so the UI can render "reachable but credentials unverified" without inferring semantics. A full SV2 handshake probe is a v1.5 fast-follow.
9. **`UpdatePoolRequest` patch semantics change.** Existing callers that set a field to `""` to mean "leave unchanged" will get `INVALID_ARGUMENT` after v1. Documented in the v1 release notes; no known in-tree callers exercise this edge case.

## Decisions

Each of the below is the recommended call for v1, with rationale and a revisit trigger. Team may override in review; defaults flow into the implementation otherwise.

1. **URL scheme: `stratum2+tcp://` only in v1; SSL/WS variants rejected.**
   Matches the format Braiins Pool documents for operators — the canonical SV2 pool, maintained by the protocol's co-authors — so URLs pasted from pool-operator docs match our validation regex without massaging. The SV2 spec itself defines no URL form; SRI's tProxy config takes separate `address` / `port` / `authority_pubkey` fields. An earlier draft of this plan adopted `sv2+tcp://` as a mirror of `stratum+tcp://`, but that invented a scheme no pool documents — Braiins' rendering is the one operators will paste. TLS variants (`stratum+ssl://`, `stratum2+ssl://`) and `stratum+ws://` are not in v1: the dispatch path uses plain `net.Dial`, and surfacing schemes the runtime can't honor would create configs the API accepted but mining can't use.
   *Revisit if* Braiins changes their documented format, or if a real customer needs TLS/WS — at which point `stratum2+ssl://` becomes a real (non-trivial) feature.

2. **`STRATUM_V2_PROXY_ENABLED` default: `false` (opt-in).**
   The translation proxy introduces a new long-running service, a new network dependency (the upstream SV2 pool), and new config surface. Defaulting on would force every existing deployment to either configure it or explicitly disable it on upgrade — net negative for the ~100% of installs that don't need it today. Note: this flag controls *only* the translation proxy. Operators with native-SV2-only fleets can create and assign SV2 pools without ever flipping it on.
   *Revisit when* the `pool_protocol_total{protocol="sv2"}` metric shows majority adoption and a majority of those deployments have the proxy enabled.

3. **Capability mismatch: hard-block the pool save with a structured error.**
   Silent "miner stopped hashing" failures are the single worst UX in a fleet tool — they surface hours later as support tickets, not as obvious errors. The `PreviewMiningPoolAssignment` RPC already gives the operator complete visibility into who would be rejected before they click Save, so hard-blocking is not a UX penalty; it's the natural endpoint of the preview-then-commit flow. The "operators can shoot themselves" precedent elsewhere in the repo applies to reversible actions, not to pushing known-bad config to a running fleet.
   *Revisit if* operators actually ask for force-override (unlikely — we can add a "--force" flag on the CLI path without changing the default).

4. **SRI images: pull `stratumv2/translator_sv2` from Docker Hub, pinned by both tag and sha256 digest; no vendored mirror in v1.**
   YAGNI on the mirror — air-gapped operators are a self-identifying cohort that already mirrors every image in the stack. Pin by digest because the translator sits in the miner→pool path and a tag overwrite or upstream compromise would be a direct hashrate-theft vector; the readable `:tag` pin stays alongside the digest so operators see what version they're running. Document image tag lookup in `deployment-files/sv2/README.md` so upgrade and mirror setup are both mechanical.
   *Revisit when* a real air-gapped customer asks, or if SRI's registry goes unreliable.

5. **`PreviewMiningPoolAssignment` lives on `MinerCommandService`.**
   The preview answers "what will be dispatched to each device" — that's a command concern, not a pool CRUD concern. Keeps `PoolsService` focused and matches the pattern in the curtailment plan (Preview sits next to Start, not next to pool CRUD). Cross-package dependency is trivial: the command service already reads pool records.

6. **Capability reporting: per-device at telemetry time; `ModelCapabilitiesProvider` only as fallback.**
   Firmware-version tables drift. Every new Braiins OS release requires a plugin PR just to flip a bit, and we will get the firmware-version cutoff wrong at least once. Per-device reporting — the plugin probes SV2 support during the same scrape that gathers telemetry — is self-updating, moves the truth closer to the wire, and matches how `pool_config`, `mining_start`, etc. are already surfaced. `ModelCapabilitiesProvider` remains available for plugins whose firmware doesn't expose a probe, but is not the default path.
   *Revisit if* telemetry-time probing proves too expensive on the fleet (unlikely — it's one capability bit per scrape).

7. **v2 ships bundled JDC with operator-provided Core, as a general-purpose feature.**
   The customers driving this ask care about the decentralization story; that story is Job Declaration, not just SV2 wire transport. Shipping a pool-partnership-only path ties Fleet to one pool and directly contradicts the "No fees. No training. Full control." framing in the README. The operator-provided Core UX is scoped — one endpoint config field plus a docs page on setting up `bitcoin-core-sv2` — and is the natural seam between "Fleet manages miners" and "operators own their bitcoin infrastructure."
   *Revisit if* v1 operational load proves higher than expected; bundled Core (v3) becomes a way to absorb that complexity rather than pushing it to operators.

8. **Queue gains a per-device payload variant; preflight is the only rewrite site.**
   The alternative (preflight validation + per-device rewrite at dispatch) minimized queue changes but created a capability-flip race the plan had to document and test. Extending the queue interface with `EnqueuePerDevice` and writing resolved payloads at commit time gives exact preview/commit parity by construction — the dispatch worker has nothing to decide. The storage contract doesn't change (per-device `queue_message.payload` already exists); only the write path gains a variant. This simplifies the overall design more than any of the other review-driven changes.
   *Revisit if* other commands need per-device payloads — at that point promote `EnqueuePerDevice` to the default and deprecate the single-payload path.

9. **All reason/warning/validation fields are typed enums, not strings.**
   `RewriteReason`, `SlotWarning`, `DeviceWarning`, `ValidationMode` — everything the UI or CLI branches on is an enum in the proto. Stringly-typed response fields on a public API are a footgun: every consumer gets to invent its own string comparison, values drift across languages, and there is no compile-time check that the UI handles every case. The preflight package is a stable typed contract shared by the RPC handler, commit path, and tests.
   *Revisit never.* This is the baseline.

10. **Dynamic SV2-support reports via the telemetry snapshot, not a generic provider interface.**
    A `DynamicCapabilityReporter` interface + new RPC was the flexible answer for a problem that only has one bit in it today. Until a second dynamic capability appears, the right shape is one field on `DeviceMetrics` — it reuses the existing telemetry flow, costs zero new RPCs, and is impossible to forget (telemetry runs on every scrape). If a second bit arrives, generalize then; don't pre-build the machinery.
    *Revisit when* a second dynamic capability ships, or when `DeviceMetrics` becomes the wrong carrier for reasons other than "we have >1 bit."
