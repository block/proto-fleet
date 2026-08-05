# Single-miner view prototypes ("The Lab")

> **Throwaway.** This whole directory exists to prototype three strategies for
> the single-miner view on the `migrate-single-miner-to-fleet` branch. It is
> **not** meant to merge to `main`. See
> `docs/plans/2026-07-28-single-miner-views-on-fleet-backend-plan.md`.

## What's here

A dev-only **Prototype Lab** at route `/lab` that previews three strategies
against one deliberately-distilled single-miner view (identity + 3 KPI tiles +
a hashboard/ASIC mini-grid + one control):

1. **Fleet-native** (`fleetNative/`) — identity + KPIs sourced entirely from the
   fleet server via the existing `ListMinerStateSnapshots` RPC (by /32 ipCidr)
   over `/api-proxy`; the page IS the "single-miner mode" IP-connect entry. The
   ASIC grid is **synthesized** — the fleet collects components in the collector
   but discards them at persistence and exposes no RPC for them (see
   `fleetAdapter.ts` for the exact file:line and the `prototype/v1` RPC that
   would make it real; that backend RPC is documented-but-deferred, since each
   server change needs a full image rebuild).
2. **Proxy, versioned** (`proxyVersioned/`) — probe `/api/version`, then mount a
   bespoke mini client per MDK version (`ProtoOSv1Mini` / `ProtoOSv2Mini`), each
   rendering its native shape (not the shared view).
3. **Adapter** (`adapter/`) — one generic view, swappable backend adapters
   (MDK v1 REST, MDK v2 consolidated) that fold divergent shapes into the shared
   snapshot; version chosen by `/api/version` probe.

`shared/` holds the pieces the fleet-native + adapter strategies reuse: the
`SingleMinerSnapshot` contract, the `SingleMinerAdapter` seam (`adapter.ts`), the
presentational `<SingleMinerView>`, and mock data.

## Running it

Start the two fake rigs (MDK v1 on :18081, MDK v2 on :18082, both mining):

```
just lab-fakes
```

Then open `/lab` in the client. Strategy 2/3 use the fake rigs directly (they
send permissive CORS). Strategy 1 needs the fleet-api up and an authenticated
session; use "Pick a discovered miner" to grab a valid target.

## Deleting the prototype

Everything is isolated so it can be removed cleanly:

- `client/src/protoFleet/prototypes/` (this dir)
- the `/lab` route block in `client/src/protoFleet/router.tsx`
- `server/fake-proto-rig/mdk_v2.go` (the whole file), plus the prototype-tagged
  additions in `main.go` (`withPrototypeCORS`, `FAKE_RIG_MINING`,
  `RegisterV2Routes`) and the `v2State` constant fix
- the `lab-fakes` recipe in the `justfile`

No fleet-server code was added (the `prototype/v1` RPC was designed but not
built), so there is nothing to remove under `proto/` or `server/internal/`.
