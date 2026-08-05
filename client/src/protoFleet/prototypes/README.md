# Single-miner view prototypes ("The Lab")

> **Throwaway.** This whole directory exists to prototype three strategies for
> the single-miner view on the `migrate-single-miner-to-fleet` branch. It is
> **not** meant to merge to `main`. See
> `docs/plans/2026-07-28-single-miner-views-on-fleet-backend-plan.md`.

## What's here

A dev-only **Prototype Lab** at route `/lab` that previews three strategies
against one deliberately-distilled single-miner view (identity + 3 KPI tiles +
a hashboard/ASIC mini-grid + one control):

1. **Fleet-native** (`fleetNative/`) — data from the fleet server via a
   throwaway Connect RPC; plus a "single-miner mode" IP-connect entry.
2. **Proxy, versioned** (`proxyVersioned/`) — reverse-proxy to the miner, with a
   different mini client per MDK firmware version.
3. **Adapter** (`adapter/`) — one generic view, swappable backend adapters
   (fleet, MDK v1, MDK v2).

`shared/` holds the pieces every strategy reuses: the `SingleMinerSnapshot`
contract, the presentational `<SingleMinerView>`, and mock data.

## Deleting the prototype

Everything is isolated so it can be removed cleanly:

- `client/src/protoFleet/prototypes/` (this dir)
- the `/lab` route block in `client/src/protoFleet/router.tsx`
- `proto/prototype/` + `server/internal/handlers/_prototype/` (S1 backend)
- the `MDK_VERSION` knob in `server/fake-proto-rig/`
- the `lab-up` recipe in the `justfile`
