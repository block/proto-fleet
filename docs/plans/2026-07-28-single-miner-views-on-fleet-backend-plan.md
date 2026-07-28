---
title: "Migrate single-miner views to render from the fleet backend"
date: 2026-07-28
status: draft
type: plan
---

# Migrate single-miner views to render from the fleet backend

## Context & goal

Today the single-miner UI ("ProtoOS") is built to talk **directly to on-miner
HTTP/gRPC APIs**. It is served two ways:

1. **Standalone on the miner** — ProtoOS is bundled onto the device (ProtoOS
   firmware) and served by the miner's embedded web/API server. Browser hits
   `/api/v1/*` on the miner itself.
2. **Embedded inside ProtoFleet** — the fleet UI opens a single-miner view under
   `/miners/:id/*` (`client/src/protoFleet/components/SingleMinerWrapper/`). It
   reuses the *same* ProtoOS React app, but points the API client at the fleet
   server, which **reverse-proxies** every `/api/v1/*` call through to the live
   miner (`server/internal/handlers/minerproxy/handler.go` — handles login,
   token caching, TLS to the device).

So even the "fleet" single-miner view is really ProtoOS-over-a-proxy. Both modes
have a hard dependency on the miner being **online, reachable, and running a
ProtoOS-compatible API**.

**Goal:** render single-miner views from **fleet-collected data** instead of
proxying to the miner. Benefits the user called out:

- We could stop serving ProtoOS on the miner (or reduce it to a thin status
  page), simplifying the device image.
- We could render a single-miner view for **any** miner regardless of OS /
  firmware / vendor — including offline miners (from last-known data) and
  non-ProtoOS devices that a plugin can normalize.

The central risk the user flagged — **API/data discrepancies**, especially ASIC
data — is real and is the gating constraint. This document maps the current
implementation, quantifies the gap, and proposes a phased plan.

---

## Locked assumptions (rev 2 — 2026-07-28)

Per direction, the following are now fixed and shape the plan below:

1. **ProtoOS-on-miner goes away.** No requirement to keep the full app on the
   device. A simpler on-device status page may be built later as a **separate**
   effort — out of scope here.
2. **Fold ProtoOS into ProtoFleet.** We collapse from two client apps to one.
   The single-miner experience becomes a first-class part of ProtoFleet, not a
   proxied embed of a separate app. Directory structure simplifies accordingly.
3. **Target = full parity** with today's ProtoOS single-miner view, with the
   explicit allowance to **reduce data granularity/freshness** to whatever
   ProtoFleet can reasonably support (e.g. 10-min device-level history instead of
   15s real-time; aggregated instead of per-ASIC where necessary).

Consequence of #1: the `minerproxy` reverse-proxy and all per-miner web-auth
(`login`/`refresh`/pairing) are **removed**, not migrated. Nothing renders by
talking to the miner's `/api/v1/*` anymore; everything comes from fleet RPCs.

## Current architecture (as-is)

### ProtoOS data layer

ProtoOS is a REST client generated from the miner's OpenAPI spec
(`client/src/protoOS/api/generatedApi.ts`), wrapped in ~37 hooks under
`client/src/protoOS/api/hooks/`. The base URL is injected by
`MinerHostingContext` (`mode: "direct" | "fleet"`); in fleet mode the base URL
points at the fleet proxy per device.

Data domains and their miner endpoints:

| Domain | ProtoOS hook / endpoint | R/W |
| --- | --- | --- |
| Current telemetry (miner/hashboard/asic/psu) | `getCurrentTelemetry` `GET /telemetry?level=…` (15s poll) | R |
| Per-hashboard ASIC stats | `getHashboardStatus` `GET /hashboards/{hb_sn}` | R |
| Per-ASIC detail | `getAsicStatus` `GET /hashboards/{hb_sn}/{asic_id}` | R |
| Historical timeseries (miner/hb/asic) | `getTimeSeries` `POST /timeseries` (1m/5m/15m) | R |
| Mining status / target | `getMiningStatus`, `getMiningTarget` | R |
| Hardware inventory | `getHardware` `GET /hardware` | R |
| Network config | `getNetwork` / `setNetworkConfig` | R/W |
| Cooling | `getCooling` / `setCoolingMode` | R/W |
| Pools | `listPools` / `createPools` / `editPool` / `deletePool` / `testPoolConnection` | R/W |
| System info / status / tag | `getSystemInfo`, `getSystemStatus`, `getSystemTag`/`setSystemTag` | R/W |
| Errors / logs | `getErrors`, `getSystemLogs`, `downloadLogs` | R |
| Control | `startMining`, `stopMining`, `rebootSystem`, `locateSystem`, firmware/PSU update | W |
| Auth | `login`, `setPassword`, `changePassword` | R/W |

The on-miner API behind these (proto in `proto-rig-api/grpc/`) is richer than
the REST surface: `MinerDataApi`, `MinerCommandApi`, `MinerSystemApi`,
`MinerTelemetryApi` (server-streaming `StreamMetrics` / `StreamLogs`),
`MinerDebugApi`, plus `GetAsicMetadata` (binning/wafer/die) and NATS async
health streams.

### Fleet backend data layer

- **Collector:** `server/internal/domain/telemetry/service.go` polls each device
  every **10 minutes** (`defaultDevicePollInterval`), via plugins. A 5s
  broadcaster and on-demand `RefreshMiners` RPC exist for freshness.
- **In-memory model:** the collector builds a full
  `models/v2.DeviceMetrics` — including `HashBoards[] → ASICs[]`, `PSUMetrics[]`,
  `FanMetrics[]`, per-component temps/voltages/frequency
  (`server/internal/domain/telemetry/models/v2/component_metrics.go`).
- **Persistence (the crux):**
  `server/internal/infrastructure/timescaledb/telemetry_store.go` maps that
  model into `InsertDeviceMetricsParams` — and writes **only device-level
  scalars**: `hash_rate_hs, temp_c, fan_rpm, power_w, efficiency_jh, voltage_v,
  current_a, inlet/outlet/ambient_temp_c, chip_count, chip_frequency_mhz,
  health`. **The `HashBoards`/`ASICs`/`PSUMetrics`/`FanMetrics` arrays are
  discarded — they are never persisted to any table or JSON column.**
- **Storage:** TimescaleDB hypertable `device_metrics` (raw 30d) + continuous
  aggregates `device_metrics_hourly` (90d) / `device_metrics_daily` (3y), plus
  `device_status_*` for temp-distribution/uptime histograms.
- **Per-device read RPCs today:** `FleetManagementService.LookupMinerByIdentifier`
  and `ListMinerStateSnapshots` (identity, placement [site/building/rack, #793],
  status, and current power/temp/hashrate/efficiency measurements);
  `TelemetryService.GetCombinedMetrics` / `StreamCombinedMetricUpdates`
  (historical + live device-level candles); `GetMinerPoolAssignments`,
  `GetMinerCoolingMode`.
- **Control RPCs today:** `MinerCommandService` already has `Reboot`,
  `Start/StopMining`, `SetCoolingMode`, `SetPowerTarget`, `UpdateMiningPools`,
  `UpdateMinerPassword`, `BlinkLED`, `DownloadLogs`, `FirmwareUpdate`, `Unpair`
  — async, batch-based (`batch_identifier` + `StreamCommandBatchUpdates`).

---

## Data gap analysis (fleet vs. miner)

This is the heart of the migration. Grouped by how hard the gap is to close.

### A. Already available on fleet (low effort — rebind hooks)

- Device-level current + historical hashrate / temp / power / efficiency
  (`GetCombinedMetrics`, `MinerStateSnapshot`).
- Identity, firmware version, placement, status.
- Pool assignments (read: `GetMinerPoolAssignments`).
- Cooling mode (read: `GetMinerCoolingMode`).
- Control actions (write): reboot, start/stop, cooling, power target, pools,
  password, LED, logs download, firmware — via `MinerCommandService`.

### B. Collected but thrown away (medium effort — persist what we already build)

The collector already produces this every poll; we just don't store it:

- **Per-hashboard** hashrate/temp/voltage/current/inlet/outlet/chip-count/freq.
- **Per-ASIC** temp / frequency / voltage / hashrate.
- **Per-PSU** input/output power/voltage/current, hotspot temp, efficiency.
- **Per-fan** RPM / percent / temp.

Closing this means: add storage (see options below), stop discarding the arrays
in `telemetry_store.go`, and add read RPCs. **But note the cadence problem:**
even persisted, this data would be 10-minute-granular, not the ~15s real-time /
1m timeseries ProtoOS shows today. Per-ASIC timeseries at 10-min resolution over
3 years is also a large cardinality increase (chips-per-board × boards ×
devices) — needs a deliberate schema + retention decision, not a naive column
add.

### C. Not collected at all (high effort — new collection + storage)

- **Real-time telemetry** (ProtoOS polls the miner every 15s; fleet is 10-min).
  Live per-ASIC/hashboard charts and the "temperature/:serial" ASIC heatmap have
  no fleet equivalent at that fidelity without either (a) an on-demand
  passthrough to the miner, or (b) a much faster/streaming collector.
- **ASIC metadata** (`GetAsicMetadata`: lot/wafer/die/binning) — not collected.
- **Detailed error/recovery stats** (`MinerDebugApi`) — not collected.
- **Live log streaming** (`StreamLogs`) — fleet has `DownloadLogs` (batch) only.
- **Pool connection test** (`testPoolConnection`) — inherently a live miner op.
- **Network config read/write** (`getNetwork`/`setNetwork`) — no fleet RPC.
- **Full hardware inventory** (`getHardware` per-slot firmware/bootloader/chip
  IDs) — only partially represented on fleet.

### D. Structurally live-only (should probably stay a miner call — or drop)

Some operations are meaningless against stored data and only make sense against
the live device: `testPoolConnection`, `locateSystem` (LED), initial onboarding
(network setup, first-boot password), firmware upload streaming. These argue for
keeping *some* thin device-reachability path even post-migration, OR accepting
that these live only in an on-miner status page.

---

## Full ProtoOS → ProtoFleet API parity map

This is the exhaustive inventory of the API calls the ProtoOS single-miner view
**actually makes** (the 27 live endpoints behind its ~37 hooks; the generated
`curtailment`/`ssh`/`secure`/`unlock`/per-`hbSn` `hashrate|power|efficiency`
endpoints are dead generated code, not wired into any screen), mapped to the
ProtoFleet fleet-server analog and the work to reach parity.

Legend: ✅ exists on fleet · 🟡 partial / semantics differ · 🔴 no analog.

### Reads — telemetry & metrics

| # | ProtoOS call (endpoint) | Shows | Fleet analog | Parity | Work to reach parity |
| --- | --- | --- | --- | --- | --- |
| 1 | `getCurrentTelemetry` `GET /telemetry?level=miner,hashboard,asic,psu` (15s) | Live device + per-hashboard/ASIC/PSU telemetry | Device-level: `MinerStateSnapshot`, `TelemetryService.StreamCombinedMetricUpdates` | 🟡 | Device-level ✅ (accept ~10-min / 5s-broadcast freshness). Per-hashboard/ASIC/PSU **not persisted** → Phase 3 (persist component arrays) or live passthrough. |
| 2 | `getTimeSeries` `POST /timeseries` (miner/hb/asic, 1m/5m/15m) | Historical charts | `TelemetryService.GetCombinedMetrics` (device-level, 10-min raw / hourly / daily) | 🟡 | Device-level ✅ at reduced granularity. Per-hb/ASIC timeseries 🔴 → Phase 3. |
| 3 | `getHashboards` `GET /hashboards`, `getHashboardStatus` `GET /hashboards/{hbSn}` (+ per-ASIC grid) | Per-board status + ASIC heatmap grid | none | 🔴 | Persist per-component snapshots + new read RPC (Phase 3); live ASIC grid via passthrough (Phase 4). |

### Reads — status, identity, errors

| # | ProtoOS call | Shows | Fleet analog | Parity | Work to reach parity |
| --- | --- | --- | --- | --- | --- |
| 4 | `getMiningStatus` `GET /mining` | Mining / paused / curtailed | `MinerStateSnapshot.device_status` (+ curtailment service state) | 🟡 | Map ProtoOS status enum → fleet `device_status`; surface curtailed-vs-stopped distinction. |
| 5 | `getSystemStatus` `GET /system/status` | Per-component health rollup | `MinerStateSnapshot` (`device_status`, `temperature_status`) | 🟡 | Device-level ✅; per-component health needs Phase 3 data. |
| 6 | `getSystemInfo` `GET /system` | Firmware, model, control-board, serials | `MinerStateSnapshot` (model, manufacturer, firmware_version, mac, serial) | 🟡 | Core fields ✅; control-board/MPU/bootloader detail 🔴 (see #8). |
| 7 | `getErrors` `GET /errors` | Active alerts | `ErrorQueryService.ListMinerErrors` / `Watch` (stream) | ✅ | Wire hook; verify alert shape/severity parity. |
| 8 | `getHardware` `GET /hardware` | Per-slot hashboard/PSU/fan firmware, bootloader, chip IDs | none (partial in unpersisted component model) | 🔴 | Collect + persist inventory (firmware/bootloader/chip IDs not collected today) + new RPC. |

### Config reads & writes

| # | ProtoOS call | Shows / does | Fleet analog | Parity | Work to reach parity |
| --- | --- | --- | --- | --- | --- |
| 9 | `getCooling` / `setCoolingMode` `GET|PUT /cooling` | Fan mode/speed read + set | Read `FleetManagementService.GetMinerCoolingMode`; write `MinerCommandService.SetCoolingMode` | ✅ | Wire; write is async batch (`batch_identifier` + `StreamCommandBatchUpdates`). |
| 10 | `getNetwork` / `setNetworkConfig` `GET|PUT /network` | IP/gateway/DNS/MAC/DHCP read + **write** | Read `NetworkInfoService.GetNetworkInfo` (IP/gateway/subnet/ipv6 only); write = `UpdateNetworkNickname` only | 🟡 read / 🔴 write | Read: add DNS/MAC/DHCP-flag fields. **Write of static-IP/DHCP has no fleet RPC** — this is a live/on-device op; likely belongs to the separate status page (assumption 1), not fleet. |
| 11 | `listPools` `GET /pools` | Miner's current pool config | `FleetManagementService.GetMinerPoolAssignments` | ✅ | Wire. |
| 12 | `createPools`/`editPool`/`deletePool` `POST|PUT|DELETE /pools[/id]` | Set/replace pools | `MinerCommandService.UpdateMiningPools` (replace-set) | ✅ | Wire; current embed already routes pool writes through Fleet (read-only ProtoOS UI). Reconcile add/edit/delete UX → single replace-set call. |
| 13 | `testPoolConnection` `POST /pools/test-connection` | Live stratum reachability from the miner | `PoolsService.ValidatePool` | 🟡 | `ValidatePool` validates a pool definition, not necessarily a live test **from that miner**. Verify semantics; true per-miner test may be live-only (drop or Phase 4 passthrough). |
| 14 | `getMiningTarget` / `editMiningTarget` `GET|PUT /mining/target` | Power target / performance mode read + set | Write `MinerCommandService.SetPowerTarget` (`PerformanceMode`); **no read RPC** | 🟡 | Write ✅. Read 🔴 → add a getter or surface current mode/target on `MinerStateSnapshot`. |
| 15 | `getSystemTag` / `setSystemTag` / delete `/system/tag` | User label on the miner | `FleetManagementService.RenameMiners` / `UpdateWorkerNames` | 🟡 | Decide mapping: ProtoOS "system tag" vs fleet device name vs pool worker name. Likely fold tag → fleet device name. |

### Control (writes)

| # | ProtoOS call | Does | Fleet analog | Parity | Work to reach parity |
| --- | --- | --- | --- | --- | --- |
| 16 | `startMining` / `stopMining` `POST /mining/start|stop` | Resume/pause mining | `MinerCommandService.StartMining` / `StopMining` | ✅ | Wire (async batch). |
| 17 | `rebootSystem` `POST /system/reboot` | Reboot | `MinerCommandService.Reboot` | ✅ | Wire. |
| 18 | `locateSystem` `POST /system/locate` | LED locate | `MinerCommandService.BlinkLED` | ✅ | Wire. |
| 19 | `updateCheck` + `postUpdateSystem` `POST /system/update/check|/update` | Check + apply firmware | `MinerCommandService.FirmwareUpdate` (staged artifact) | 🟡 | Wire apply. "Check for update" semantics differ (fleet stages an artifact) — reconcile UX. |
| 20 | `postUpdatePsu` `POST /power-supplies/update` | PSU firmware update | none | 🔴 | Add PSU-firmware command or drop from parity. |

### Logs

| # | ProtoOS call | Does | Fleet analog | Parity | Work to reach parity |
| --- | --- | --- | --- | --- | --- |
| 21 | `getSystemLogs` / `downloadLogs` `GET /system/logs` | Inline log tail + CSV download (miner_sw/pool/os) | `MinerCommandService.DownloadLogs` (async artifact); `ServerLogService`/`ActivityService` for fleet-side | 🟡 | Download ✅ (async, not inline tail). Live tail (`StreamLogs`) has no fleet analog — accept batch-only or Phase 4. |

### Auth & pairing (collapse, do not migrate)

| # | ProtoOS call | Was for | Fleet handling | Parity | Work |
| --- | --- | --- | --- | --- | --- |
| 22 | `login` / `refresh` / `logout` `/auth/*` | Per-miner web session | `AuthService.Authenticate` / `Logout` + fleet RBAC | ✅ collapses | Remove per-miner auth from the view entirely. |
| 23 | `setPassword` / `changePassword` `/auth/*` | The miner's own web password | `MinerCommandService.UpdateMinerPassword` (miner pw); `AuthService.UpdatePassword` (fleet user pw) | ✅ | Keep as a "change miner password" action; drop first-boot set-password (→ status page). |
| 24 | `getPairingInfo` / auth-key `/pairing/*` | Device↔fleet pairing | `PairingService` / `FleetNodeAdminService` | ✅ collapses | Remove from single-miner view (pairing is a fleet flow already). |

**Onboarding flow** (`/onboarding/*`: network setup, verify, first password, pool
selection) is inherently on-device / pre-pairing → out of scope per assumption 1
(belongs to the future status page).

### Parity summary by effort

- **Rebind only (analog exists, ✅):** errors (7), cooling (9), pool read (11),
  pool write (12), power-target write (14 write), start/stop (16), reboot (17),
  locate (18), auth/pairing collapse (22–24). → **Phase 1–2.**
- **Small backend add (🟡, extend existing):** device status/info mapping (4–6),
  network read fields (10 read), power-target read (14 read), system-tag mapping
  (15), firmware-update UX (19), logs download (21). → **Phase 1–2 + minor proto.**
- **Real backend work (🔴, collect+persist+RPC):** per-hashboard/ASIC/PSU
  telemetry — current (1) & historical (2), hashboard/ASIC detail screen (3),
  hardware inventory (8). → **Phase 3** (+ Phase 4 passthrough for live fidelity).
- **Likely drop or defer:** static-IP/DHCP write (10 write), live pool test (13),
  PSU firmware (20), live log tail (21) — mostly live-only ops that fit the
  separate on-device status page, not the fleet view.

**Headline:** ~18 of 24 domains are ✅/🟡 (rebind or small extend). The genuine
backend build is concentrated in the **per-component telemetry/inventory** rows
(1, 2, 3, 8) — exactly the ASIC-data concern. Everything else is largely wiring.

## Client restructuring (fold ProtoOS into ProtoFleet)

With assumption 2, the two-app split (and AGENTS.md rule 5's import boundary)
collapses. Rough shape of the client work, independent of backend parity:

- Retire `client/src/protoOS` as a standalone app; move its **presentational**
  components (charts, gauges, ASIC grid, diagnostics panels, layout) into
  ProtoFleet (or `shared/` where genuinely shared).
- Replace the ProtoOS data layer (generated REST client + `MinerHostingContext`
  `mode`) with ProtoFleet's Connect-RPC clients. The `direct`/`fleet` mode split
  disappears — there is only fleet.
- Remove `SingleMinerWrapper`'s proxy embed; the single-miner view becomes a
  native ProtoFleet route rendering from fleet RPCs.
- Delete `server/internal/handlers/minerproxy` once no screen proxies.
- Collapse the two Vite entry points / build outputs to one; update
  `routePrefetch.ts` + `router.tsx` (AGENTS.md rule 9) for the merged routes.
- E2E: `test-e2e-protoos` folds into `test-e2e-fleet`.

This is a sizable but mostly mechanical refactor; it can proceed in parallel with
Phase 1 backend wiring since it doesn't depend on the ASIC-data work.

## Strategic decisions required

These are product/architecture calls that shape the plan. Recommendations given,
but they need sign-off before build.

1. **What happens to ProtoOS-on-miner?**
   - Option 1a: Replace with a **thin status page** on the device (health,
     hashrate, "managed by Fleet", onboarding/network setup). Recommended — keeps
     first-boot / offline-from-fleet usable without shipping the whole app.
   - Option 1b: Remove entirely, require ProtoFleet for everything. Simpler
     device image but breaks any air-gapped / pre-pairing workflow.

2. **ASIC / per-component detail: match, degrade, or passthrough?**
   - Option 2a (**recommended, hybrid**): persist per-component snapshots at the
     10-min cadence for historical/offline views; for the *live* ASIC heatmap
     keep an **on-demand passthrough** to the miner when it's reachable
     (reuse/trim the existing minerproxy or a single `RefreshMiner` detail RPC).
     Best fidelity, honest about freshness, degrades gracefully when offline.
   - Option 2b: fleet-only, drop real-time ASIC views; show 10-min historical
     per-ASIC. Simplest, but a visible regression for on-miner users.
   - Option 2c: build a true streaming collector to match 15s fidelity fleet-wide.
     Highest cost (storage cardinality, ingest load); likely overkill.

3. **Multi-OS / vendor-agnostic rendering.** The fleet `DeviceMetrics` model is
   already plugin-normalized, so a fleet-rendered view naturally generalizes to
   non-ProtoOS devices — *if* the view is driven by the normalized model rather
   than ProtoOS-specific REST shapes. This is an argument for a **new
   fleet-native single-miner view** rather than re-pointing ProtoOS's REST hooks.

4. **New view vs. re-point existing ProtoOS hooks.** Re-pointing 37 REST hooks at
   fleet-shaped RPCs is a lot of shim work and permanently couples the fleet view
   to ProtoOS's device-centric API shape (blocking decision #3). Recommendation:
   build the embedded single-miner view as **fleet-native** (Connect-RPC against
   `FleetManagementService` / `TelemetryService` / `MinerCommandService`),
   reusing ProtoOS *presentational* components where the boundary allows (shared/
   cannot import protoOS/; presentational pieces may need to move to `shared/`).

---

## Recommended approach

**Hybrid, fleet-native, incremental.** Concretely:

- Build the ProtoFleet single-miner view against fleet RPCs, not the proxy.
- Persist per-component (hashboard/ASIC/PSU/fan) snapshots we already collect, so
  historical + offline detail works fleet-side.
- Keep a **thin on-demand passthrough** for genuinely-live operations
  (real-time ASIC heatmap, pool test, LED, network setup) that degrades to
  "unavailable — miner offline" instead of blocking the whole view.
- Reduce ProtoOS-on-miner to a status/onboarding page once the fleet view
  reaches parity for managed miners.

This preserves the multi-OS benefit, kills the hard dependency on a live
ProtoOS-compatible miner for the common case, and is honest about data freshness.

---

## Phased plan

**Phase 0 — Alignment (this doc).** Lock decisions 1–4 with product/design. Define
"parity" explicitly per screen (what fidelity/freshness is acceptable fleet-side).

**Phase 1 — Fleet-native read view (no proxy) for what already exists.**
Build the embedded single-miner shell rendering identity, placement, status, and
device-level hashrate/temp/power/efficiency (current + historical) from
`MinerStateSnapshot` + `GetCombinedMetrics`. Read-only pools/cooling. This alone
lets the common dashboard render for offline miners and non-ProtoOS devices.

**Phase 2 — Wire control through `MinerCommandService`.** Reboot, start/stop,
cooling, power target, pool edit, password, LED, logs, firmware — replace
proxied writes with the existing async batch RPCs + `StreamCommandBatchUpdates`
for status. (Pools already read-only-via-Fleet in the current embed — extend.)

**Phase 3 — Persist per-component telemetry.** Stop discarding
`HashBoards/ASICs/PSU/Fan` in `telemetry_store.go`; design storage + retention
(likely a separate hypertable / JSONB snapshot keyed by device+time, with a
tighter retention than device scalars given cardinality). Add read RPCs. Render
per-hashboard / per-ASIC historical detail + the temperature-detail screen from
fleet data.

**Phase 4 — Live-detail passthrough (bounded).** For the real-time ASIC heatmap
and live-only ops, add a single narrow on-demand path (trimmed minerproxy or a
`RefreshMinerDetail` RPC) that fetches fresh detail when the miner is reachable
and degrades cleanly when not. Fill remaining gaps (network config RPC, hardware
inventory, error stats) as product deems necessary.

**Phase 5 — Shrink ProtoOS-on-miner.** Once managed-miner parity is reached,
replace the on-device app with the thin status/onboarding page (decision 1a) and
retire the full embedded proxy for managed devices.

---

## Open questions / risks

- **Freshness contract.** ProtoOS shows ~15s-fresh data; fleet is 10-min. Every
  screen needs an explicit "how fresh must this be" answer. Some screens will
  visibly regress unless we do Phase 4 passthrough.
- **Per-ASIC storage cardinality.** chips/board × boards × devices × 10-min ×
  3y is large. Needs a schema/retention design review before Phase 3.
- **Offline semantics.** A fleet-rendered view must clearly distinguish
  "last known (stale)" from "live". Design work required.
- **Onboarding / first-boot / air-gapped.** These are inherently on-device;
  decision 1 determines whether they survive and where.
- **Component boundary.** Reusing ProtoOS presentational components from a
  fleet-native view means promoting them to `shared/` (AGENTS.md rule 5).
- **Auth model shift.** Proxy handles miner login/token today; fleet-native view
  relies on fleet RBAC + `MinerCommandService` authz instead — verify coverage
  (e.g. `GetDeviceIdentifiersByOrgWithFilter` pairing defaults).
- **Non-ProtoOS coverage depends on plugins** actually populating the normalized
  `DeviceMetrics` for those devices — verify per driver.

## Key references

- Proxy (as-is fleet path): `server/internal/handlers/minerproxy/handler.go`
- Embedded shell: `client/src/protoFleet/components/SingleMinerWrapper/`
- Collector + poll cadence: `server/internal/domain/telemetry/service.go`
- Discarded component model: `server/internal/domain/telemetry/models/v2/component_metrics.go`
- Persistence (scalar-only): `server/internal/infrastructure/timescaledb/telemetry_store.go`
- Fleet per-device RPCs: `proto/fleetmanagement/v1/`, `proto/telemetry/v1/`, `proto/minercommand/v1/`
- On-miner API surface: `proto-rig-api/grpc/*.proto`
- ProtoOS hooks: `client/src/protoOS/api/hooks/`
