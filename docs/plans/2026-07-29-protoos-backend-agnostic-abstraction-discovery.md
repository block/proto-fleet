---
title: "Discovery: backend-agnostic ProtoOS via a canonical resolver layer"
date: 2026-07-29
status: draft
type: plan
---

# Discovery: backend-agnostic ProtoOS via a canonical resolver layer

> Sub-discovery of
> [Fleet-native single-miner view](./2026-07-28-single-miner-views-on-fleet-backend-plan.md).
> Explores: could ProtoOS be refactored to be backend-agnostic — a canonical
> list of data-fetching functions, with each backend supplying a resolver per
> item — so the same UI renders from either the on-miner REST API or the
> ProtoFleet Connect-RPC server?

## TL;DR

The theory is **architecturally sound and partially pre-built**, but it
**overestimates the fleet Connect-RPC server as a drop-in second data source.**
The blocker is not transformation difficulty — it's **missing capabilities**:
the fleet protos have no per-hashboard / per-ASIC / per-fan / per-PSU / pool-live
/ fan-control data to resolve to. A clean abstraction is achievable for a
**degraded fleet-sourced subset** (the ~4 headline metrics + pool config + coarse
cooling), gated by capability flags. "Render all of ProtoOS from fleet RPCs" is
blocked by the proto surface, not by code structure.

## The finding that reframes the theory

The existing `mode: "direct" | "fleet"` seam in
`client/src/protoOS/contexts/MinerHostingContext/MinerHostingContext.tsx` is
**not** "REST vs fleet-RPC." Both modes build the *identical* generated REST
`Api` class; the only per-mode branch is auth
(`securityWorker: mode === "direct" ? securityWorker : undefined`). Today's
"fleet" mode is the **same on-miner REST API reverse-proxied** through the fleet
server (`SingleMinerWrapper` sets `baseUrl=/api-proxy/miners/:id`). That's why it
was cheap — the data shapes are byte-identical.

So a true fleet-RPC backend would be a **third** backend with fundamentally
different message shapes — a new axis, not a widening of the existing toggle.

The `api` field in `MinerHostingContextType` is a concrete generated REST client,
not an interface — it's the object every hook consumes via
`const { api } = useMinerHosting()`. That is the wrong seam for the abstraction
(see below).

## What already helps

The Zustand store (`client/src/protoOS/store/`, see `store/README.md`) already
defines **backend-neutral canonical domain types**: `Measurement`,
`MetricTelemetry`, `MetricTimeSeries`, `AsicHardwareData`,
`HashboardHardwareData`, `FanTelemetryData`. This is effectively the
proto-agnostic domain model the theory needs — it already exists. Both the
current REST transforms and a hypothetical fleet transform target these types.

## Data-shape reality (the crux)

| Domain | REST shape (today) | Fleet-RPC counterpart | Verdict |
| --- | --- | --- | --- |
| Headline metrics (hashrate/temp/power/eff) | `TelemetryData.miner`, `MinerStateSnapshot` | `common.v1.Measurement` (+ `MinerStateSnapshot`) | ✅ thin remap: enum↔string units, **kW↔W scaling**, Timestamp↔ISO. Maps onto store `Measurement`. |
| Per-hashboard telemetry | `TelemetryData.hashboards[]`, `getHashboardStatus` | **none** (grep `hashboard` in fleet protos = 0 hits) | 🔴 no source data |
| Per-ASIC grid | `hashboards[].asics[]` | **none** | 🔴 no source data |
| Per-fan / per-PSU telemetry | `hashboards[].psus[]`, cooling `fans[]` | **none** | 🔴 no source data |
| Timeseries at hashboard/asic level | `getTimeSeries` `levels` | `GetCombinedMetrics` (4 types, **device-aggregated**, `device_count`) | 🟡 device-level only |
| Pools | `Pool` (~20 live-stat fields) | `PoolAssignment {pool_id,url,username}` | 🟡 config only, zero runtime stats |
| Cooling | fan control mode `Off/Auto/Manual` + fan RPMs | `CoolingMode` = medium `AIR/IMMERSION/MANUAL` | 🔴 different axis; only `MANUAL` overlaps by name |
| Mining target | `MiningTargetResponse` (watts + mode) | no read RPC | 🔴 needs new RPC |

Note: a `GetBatchMinerTelemetry` RPC is *referenced in a comment*
(`fleetmanagement.proto:209`) but **does not exist** — only `GetCombinedMetrics`
and `StreamCombinedMetricUpdates` do.

**Verdict:** for the 4 headline metrics it's a thin field+unit remap. For
everything that makes ProtoOS a *single-miner diagnostic tool*, the fleet server
has nothing to resolve to. Missing-capability problem, not a transform problem —
the same root cause as rows 1/2/3/8 in the main parity map.

## Where the abstraction should sit

**Not at the hook level and not at the `api` object.** Lowest-surface seam is a
**repository/resolver layer beneath the hooks, expressed in the store's canonical
domain types** — e.g. `getLatestTelemetry(): MinerTelemetrySnapshot`,
`getTimeSeries(range): MetricTimeSeries[]`, `getPools(): PoolDomain[]`,
`getCooling(): CoolingDomain`. Each backend supplies a resolver; hooks call the
repository and hydrate the store exactly as they do now. Store-backed KPI
components then become backend-agnostic for free.

Why not the `api` object: it's a concrete REST class with dozens of
OpenAPI-shaped methods; abstracting there forces a fleet backend to impersonate
the entire OpenAPI surface (high surface area, impossible for missing caps).

**The real work / prerequisite:** the transform logic that populates the
canonical types is currently **inlined inside the hooks** (`processHashboards` in
`useTelemetry.ts`, ASIC/voltage backfill in `useHashboardStatus.ts`, fan padding
in `useCoolingStatus.ts`). Several hooks carry `[STORE_REFACTOR]` TODOs
acknowledging the muddy layering. Extracting these into a `RestResolver` is
needed regardless and is the bulk of the effort.

**Wrinkle — dual read paths.** Components read from *both* the store (KPI tiles:
`Hashrate/Efficiency/PowerUsage/Temperature/Cooling`) *and* raw hook return
values (`useHashboardStatus`, diagnostics, pools table). The raw-response
consumers are exactly the ones with no fleet data — so they'd stay REST-only
behind a capability flag, which is actually convenient.

## Risks / wrinkles

- **Polling vs streaming.** All ProtoOS reads are `usePoll` snapshots (15s/30s).
  Fleet telemetry is a server stream (`StreamCombinedMetricUpdates`, min 10s). A
  request→single-response repository fits REST but is awkward for streaming; the
  interface must express subscriptions as first-class or lose streaming's point.
- **Auth divergence.** Direct = miner JWT via `securityWorker`; fleet-proxy =
  ambient session; true fleet-RPC = org-scoped fleet session (`site_ids`,
  `include_unassigned`). The ProtoOS `useAuth`/login-modal machinery is
  miner-JWT-specific — dead weight on a fleet-RPC backend (already partly gated
  by `isFleetHosted`).
- **Identity model.** REST addresses one miner implicitly via base URL; fleet
  RPCs require an explicit `device_identifier` on every call. Available in
  `MinerHostingContext.metadata`/`baseUrl` today but implicit.
- **Leaky params.** REST-only (`levels`, `aggregation`, `hbSn`, fan mode, pool
  priority) vs fleet-only (`DeviceSelector`, `site_ids`, pagination, org scope) —
  a canonical signature can't carry both without an "extras" bag.
- **Write/command asymmetry.** REST writes hit the miner directly; fleet writes
  route through `minercommand`/`fleetnodegateway` with different shapes, not
  covered by the read-oriented fleet RPCs.

## Opportunities / suggestions to improve the strategy

1. **Define the canonical layer in store-domain types, not OpenAPI method names.**
   The store types already exist and are neutral — anchor the resolver interface
   there.
2. **Make it capability-gated, not all-or-nothing.** A resolver advertises which
   canonical functions it supports; the UI degrades (hide ASIC grid / pool stats)
   when a capability is absent, rather than pretending. This is the honest way to
   support fleet + on-miner + future non-ProtoOS backends with one UI.
3. **Do the hook-transform extraction first — it's a no-regrets refactor.**
   It pays down the `[STORE_REFACTOR]` debt and unblocks the abstraction whether
   or not the fleet backend is ever added.
4. **Reconsider whether this abstraction is even the right vehicle.** Given the
   main plan's decision to *fold ProtoOS into ProtoFleet* and go fleet-native,
   the abstraction layer competes with "just build fleet-native views on the
   fleet RPCs." The abstraction is most valuable if we want **one UI that serves
   both an on-device build (full REST fidelity) and a fleet build (degraded)**
   simultaneously — i.e. if ProtoOS-on-miner does *not* fully go away. If it does
   go away (main-plan assumption 1), the abstraction's payoff shrinks to "ease
   the migration," and a direct fleet-native rebuild may be simpler.
5. **Treat streaming as the primary fleet contract**, adapting REST polling *up*
   to a subscribe-shaped interface, rather than adapting the stream *down* to
   polling.

## Honest bottom line

Sound seam, real pre-existing scaffolding (store domain types + transport seam),
but the fleet RPC surface can only feed a degraded subset. Pursue only if we want
a single UI spanning full-fidelity on-device *and* degraded fleet backends at
once; otherwise the fold-into-ProtoFleet + fleet-native rebuild path likely
dominates. Either way, extracting hook transforms into resolvers is a
no-regrets first step.

## Key references

- Transport seam: `client/src/protoOS/contexts/MinerHostingContext/MinerHostingContext.tsx`
- Canonical domain types: `client/src/protoOS/store/types.ts`, `store/README.md`
- Representative hooks: `client/src/protoOS/api/hooks/{useTelemetry,useTimeSeries,useHashboardStatus,usePoolsInfo,useCoolingStatus,useMiningTarget}.ts`
- Embed proof: `client/src/protoFleet/components/SingleMinerWrapper/SingleMinerWrapper.tsx`
- Fleet protos: `proto/telemetry/v1/telemetry.proto`, `proto/fleetmanagement/v1/fleetmanagement.proto`, `proto/common/v1/{measurement,cooling}.proto`
