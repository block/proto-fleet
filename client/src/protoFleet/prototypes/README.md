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
   over `/api-proxy`; connect to a miner (IP + credentials) from a null state and
   watch the fleet-native view render, never touching the device. The ASIC grid
   is **synthesized** — the fleet collects components in the collector
   but discards them at persistence and exposes no RPC for them (see
   `fleetAdapter.ts` for the exact file:line and the `prototype/v1` RPC that
   would make it real; that backend RPC is documented-but-deferred, since each
   server change needs a full image rebuild).
2. **Proxy, version-aware** (`proxyVersioned/`) — proxy straight to the device,
   probe `/api/version`, and resolve the matching MDK adapter (v1 REST / v2
   consolidated). Both fold into the same snapshot and render the identical
   `<SingleMinerView>`, so v1 and v2 look the same and both match strategies 1
   and 3. The version changes only the _fetch_; the difference between this
   strategy and the others is the data path (shown in the details modal), not
   the view. In production the calls ride the minerproxy path
   (`/api-proxy/miners/:id`).
3. **Adapter** (`adapter/`) — one generic view, swappable backend adapters
   (fleet server, MDK v1 REST, MDK v2 consolidated) folded behind one
   `SingleMinerAdapter` seam; backend chosen by a selector, MDK version by the
   `/api/version` probe.

`shared/` holds the pieces all three strategies reuse: the
`SingleMinerSnapshot` contract, the `SingleMinerAdapter` seam (`adapter.ts`), the
presentational `<SingleMinerView>`, the mini `<MinersList>` (the dumbed-down
miners tab), the `<MinerViewFrame>` chrome, and mock data.

## Design

The pages are built from the ProtoFleet shared kit (`Button`, `Input`, `Select`,
`Card`, `Metric`, `StatusCircle`, `Chip`, and the diagnostic `AsicTablePreview`
heatmap for the ASIC grid) so the Lab reads in the product's design language
rather than bespoke Tailwind.

Each strategy picks a miner in the way that best illustrates its point, then
renders the identical `<SingleMinerView>`:

- **S1** starts at a null state with a **Connect a miner** button that opens a
  simple modal (IP + username + password + Connect).
- **S2** shows a two-row `<MinersList>` (one MDK v1 rig, one MDK v2); clicking a
  row proxies to that miner and renders its view.
- **S3** offers an **Adapter context** dropdown (`<Select>`): _Fleet server_
  (→ a discovered-miners list), _MDK v1 miner (direct)_, or _MDK v2 miner
  (direct)_.

Once a miner is open, `<MinerViewFrame>` keeps the view a clean canvas (status +
KPIs + ASIC grid + one control): a left action steps back to the picker, and a
right-aligned **Details** trigger tucks the identity + data-path chrome
(`<SingleMinerDetails>`) into a modal. So the only thing that reads differently
between strategies is how the miner is picked and how its data is sourced — the
view itself is identical.

## Running it

Start the two fake rigs (MDK v1 on :18081, MDK v2 on :18082, both mining):

```
just lab-fakes
```

Then open `/lab` in the client. Strategy 2 (both rows) and Strategy 3's direct
MDK contexts use the fake rigs directly (they send permissive CORS). Strategy 1
and Strategy 3's Fleet context need fleet-api up and an authenticated session; S1
takes any fleet miner's IP, and S3's Fleet context lists discovered miners to
click.

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
