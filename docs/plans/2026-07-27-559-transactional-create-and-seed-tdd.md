---
title: "Transactional create-and-seed RPCs for building/site create flows"
date: 2026-07-27
status: implementing
type: tdd
tracker: https://github.com/block/proto-fleet/issues/559
---

# Transactional create-and-seed RPCs for building/site create flows

## Context

Issue #559 (from the Codex review on PR #551). The "New building" / "New site"
create flows hosted by `FleetCreateFlowProvider` create the parent with one RPC,
then fire the seeded rack/miner/building assignments as **separate** follow-up
RPCs. Each assignment's `onError` only toasts and `resolve()`s, so the flow
continues: it `bumpEntities()`, clears the seed, and opens the manage modal using
the **requested** seed counts regardless of what actually assigned. There is no
rollback and no keep-open-for-retry.

Result: a permission failure, stale ID, semantic conflict, or transport error on
the assignment step leaves a newly created **empty or partially-populated**
building/site, and the manage modal displays counts for items that were never
assigned.

## Approach (Route B — decided)

Replace the multi-RPC client orchestration with **one atomic backend create per
parent type**. The seed rides on the existing create RPC as optional fields
(see Decision #1 — the initial `*WithSeed` split was collapsed after review):

- `CreateBuilding` on `BuildingService` — optional `rack_ids` /
  `device_identifiers`; when present, create a building and assign its seeded
  racks + miners in a single transaction.
- `CreateSite` on `SiteService` — optional `building_ids` / `rack_ids` /
  `device_identifiers`; when present, create a site and assign its seeded
  buildings + racks + miners in a single transaction.

Either everything commits or nothing does. The response carries **actual**
assigned counts (and any conflicts) so the manage modal stops lying. A request
with no seed fields behaves exactly like the old plain create.

The rack create flow (`SaveRack`) is **out of scope**: it is already a single
transactional create-and-members RPC.

## Decisions

1. **Seed as optional fields on `CreateBuilding`/`CreateSite` — NOT separate
   `*WithSeed` RPCs.** *(Revised 2026-07-28 after PR #821 review; the original
   decision below was reversed.)*

   The build first shipped dedicated `CreateBuildingWithSeed` /
   `CreateSiteWithSeed` RPCs. On review (Marvin's comment, echoing the planning
   discussion) we collapsed them into the existing create RPCs, adding the seed
   as optional fields on the request and the counts + conflicts to the response.

   **Why the reversal.** The two arguments for separate RPCs turned out weaker
   than they read:
   - *"Overloading a clean constructor."* The seed fields are `repeated` /
     `optional`, so a no-seed request is wire-identical to the old call and
     behaves identically (no building/conflict change). The only real cost is a
     weaker response-type invariant: `CreateBuildingResponse.building` is now
     unset on an unforced conflict. That is a documented, seed-only path — the
     handler leaves `building` unset and populates `conflicts`, exactly as the
     `Assign*` RPCs already do.
   - *"The codebase keeps conflict-returning RPCs separate."* Only the
     device-assign RPCs return conflicts, and they are separate because they are
     genuinely distinct operations — not to keep conflicts out of constructors.
     There was no deliberate "constructors never return conflicts" boundary to
     preserve.

   Against that, a second pair of near-duplicate RPCs (handlers, permission
   entries, client hooks, translators, generated code, and a parallel
   request/response surface) is real, permanent API-surface cost. Collapsing
   keeps one create surface per entity that a plain caller can ignore the seed
   fields on. Cost of the collapse: `CreateBuildingResponse` /
   `CreateSiteResponse` gain seed-count + conflict fields, and their `building` /
   `site` may be unset on an unforced conflict — both documented on the fields.

   <details><summary>Original decision (superseded)</summary>

   > **Separate RPCs, not options on `CreateBuilding`/`CreateSite`.** Folding the
   > seed into the existing create RPCs would make a plain `CreateBuilding`
   > response sometimes return *no building and a conflict list* (unforced
   > conflict → nothing created), overloading a clean constructor. The codebase
   > already keeps conflict-returning `Assign*` RPCs separate from plain
   > mutations; new `*WithSeed` RPCs keep that boundary. Cost: one extra handler +
   > one `rpc_permissions` entry + generated code.

   </details>
2. **No rebase off #558.** #558 (`fix/558-saverack-force-guard`) touches
   `device_set.proto` / `SaveRack` / `useDeviceSets` / `ManageRackModal`. #559
   touches `buildings.proto` / `sites.proto` / the two domain services /
   `FleetCreateFlowProvider`. Zero file overlap; the only shared contract is the
   canonical lock order (`site → building → rack → device`), respected in both.
   Branch #559 off `main`.
3. **Server-authoritative conflict gate.** The unforced-conflict → return-
   conflicts-and-write-nothing contract moves server-side (mirrors
   `AssignDevicesTo*`). The client's existing UI conflict confirm and fast-fail
   preflights (batch caps, `forceClearRackMembership && !canManageRacks`) stay as
   UX, but the server is authoritative.

## Architecture: extract tx-scoped cores

Today each domain method (`CreateBuilding`, `AssignRacksToBuilding`,
`AssignDevicesToBuilding`, and the three site equivalents) opens **its own**
`RunInTx*`. Transactions cannot nest, so the combined RPC cannot call them in
sequence — that would be three separate transactions, i.e. the non-atomic bug
we are fixing.

**Key enabling fact — the transactor is re-entrant.** `SQLTransactor.RunInTxWithResult`
(`stores/sqlstores/transactor.go:33`) checks for an ambient tx on the context and,
if present, runs the action on the *existing* tx instead of opening a nested one.
And `db.WithTransaction` retries the whole action on retryable Postgres errors,
preserving non-retryable `FleetError`s through the boundary
(`db/with_transaction.go:63`).

Consequences that shape the extraction:

- The combined method opens **one** `RunInTxWithResult`; inside it, calls to the
  existing per-op logic join that single tx re-entrantly — no manual tx-handle
  threading, and the big lock/cascade/position bodies **do not need to be
  relocated**.
- Activity logging is the only thing that must move. The existing methods log
  *after* their `RunInTx` precisely because the action replays on retry; if the
  combined method called them verbatim, their logging would fire inside the
  outer tx (duplicated per retry, emitted even on a later rollback). So each op
  is split into: a **core** = the existing validation + `RunInTxWithResult(body)`
  returning its tx-result struct, with **no logging**; a **log helper** = the
  post-commit activity tail; and a thin **public wrapper** = `core` + `log
  helper` (behavior identical to today). `createBuildingInTx` is the one raw
  (no-tx) core since its body is a single insert.

The **new** methods open one `RunInTxWithResult`, call the cores in canonical
lock order (joining re-entrantly), and call the same log helpers post-commit.
This reuses the hard-won cascade/lock/position logic rather than duplicating or
relocating it. Land it as its own commit with the existing domain suite green.

The unforced-conflict rollback uses a **plain `errors.New(...)` sentinel**
(`errSeedConflict`) returned from the tx closure to force a rollback; the
conflicts themselves are captured in a closure variable. `db.WithTransaction`
wraps the non-retryable, non-`FleetError` sentinel with `%w`, so the combined
wrapper detects it via `errors.Is(err, errSeedConflict)` and returns the
captured conflicts as a response. (A `FleetError` sentinel was considered but is
unnecessary: `%w` wrapping already preserves the chain across the boundary.)

## Transaction shape of the new methods

`CreateBuilding` (with a seed), inside a single `RunInTxWithResult`, in canonical
lock order (`site → building → rack → device`):

1. **Lock site** (`LockSiteForWrite`) if `site_id` set. *(site)*
2. **INSERT building** via `createBuildingInTx`. *(building)*
3. **Assign racks** via `assignRacksToBuildingInTx` — locks racks ascending,
   bulk placement + site/building cascade + capacity guard. *(rack)*
4. **Assign devices** via `assignDevicesToBuildingInTx` — locks devices sorted,
   computes conflicts. *(device)*
   - Conflicts **and** `force_clear_conflicting_rack_membership == false` →
     **abort** (§ Conflict rollback) so the building INSERT and rack moves roll
     back; return the conflicts.
   - Otherwise force-clear clearable rack memberships, assign, cascade site.
5. Return created building + real counts.

Post-commit: fire activity log(s).

`CreateSite` (with a seed) is the same skeleton with the site-create slug loop (below)
and an extra `assignBuildingsToSiteInTx` step between create and racks. Lock
order: `site(new) → buildings asc → racks asc → devices`.

**Why the order matters:** the cascade in `AssignRacksToBuilding`
(`CascadeRackDeviceSitesBulk`) takes device row locks *after* rack locks. Locking
devices before racks would invert `rack → device` and deadlock against a
concurrent rack-assign. Keeping INSERT/rack/device in canonical order avoids it.

## Conflict rollback mechanism

The existing single-op `AssignDevicesTo*` deliberately **commits** on conflict
(stash conflicts, return nil) because no writes preceded the check. In the
combined flow the building INSERT and rack moves happen *before* the device
conflict check, so on an unforced conflict we must **roll back**.

Mechanism (as implemented): a package-level plain sentinel
`var errSeedConflict = errors.New(...)` is returned from the tx closure while the
conflicts are captured in a closure variable → transaction rolls back → the
public wrapper does `errors.Is(err, errSeedConflict)` and returns
`(nil, capturedConflicts, nil)` to the handler instead of an error.

**Verified during implementation:** `WithTransaction` retries only on
serialization/deadlock codes, so the sentinel is non-retryable; being a
non-`FleetError`, it is wrapped with `%w` in an InternalError, which
`errors.Is` still traverses. A `FleetError`-implementing sentinel would also
work but is not needed — the plain sentinel + `%w` chain passes through
cleanly.

## Proto changes

`proto/buildings/v1/buildings.proto` — extend the existing `CreateBuilding`
request/response (no new RPC):

```proto
message CreateBuildingRequest {
    // ... existing fields 1..11 ...
    // Optional seed (#559): empty = plain create.
    repeated int64 rack_ids = 12 [(buf.validate.field).repeated = {max_items: 1000, items: {int64: {gt: 0}}}];
    repeated string device_identifiers = 13 [(buf.validate.field).repeated = {max_items: 10000, items: {string: {min_len: 1, max_len: 256}}}];
    optional bool force_clear_conflicting_rack_membership = 14;
}
message CreateBuildingResponse {
    Building building = 1;                               // unset when conflicts non-empty
    int64 assigned_rack_count = 2;
    int64 reassigned_device_count = 3;
    int64 site_reassigned_device_count = 4;
    repeated PerDeviceBuildingConflict conflicts = 5;    // non-empty => nothing created
}
```

`proto/sites/v1/sites.proto` — mirror on `CreateSiteRequest` /
`CreateSiteResponse`: add `repeated int64 building_ids`, `rack_ids`,
`device_identifiers`, the force flag to the request; the response carries `Site`
(unset on conflict), `network_config_warnings`, assigned counts, and `repeated
PerDeviceConflict conflicts`. No new RPCs — the fields hang off the existing
`CreateSite`.

Then `just gen` (regenerates Go `server/generated/grpc/`, TS
`client/src/protoFleet/api/generated/`, and the Go+Python SDK). **Commit the
`.proto` and all generated output in one commit** (AGENTS.md rule; see the
`proto-regen` skill for the protoc-gen-es version-pinning gotcha).

## Handler + RBAC

`handlers/buildings/handler.go` and `handlers/sites/handler.go`:

- Primary gate `authz.PermSiteManage`.
- Conditional `authz.PermRackManage` when `force_clear_conflicting_rack_membership`
  is true (same pattern as `AssignDevicesToBuilding`), so site-only operators
  can't bypass rack auth via the force flag.
- Map domain conflicts into the response; when conflicts present, return them
  with no created entity.
- The existing `CreateBuildingProcedure` / `CreateSiteProcedure` entries in
  `handlers/middleware/rpc_permissions.go` already map to `PermSiteManage`; add a
  comment noting the inline `rack:manage` gate, mirroring the
  `AssignDevicesToBuildingProcedure` entry. No new permission entries.

## Client changes

`FleetCreateFlowProvider.tsx`:

- Fold optional seed params into the existing `createBuilding` / `createSite`
  hooks in `useBuildings` / `useSites` (single connect call each;
  `onSuccess(result)` carrying entity + counts, `onError(message, conflicts)` —
  mirroring `assignDevicesTo*`'s conflict-carrying `onError`). No separate
  `*WithSeed` hooks; the plain-create callers (`useBuildingModals` /
  `useSiteModals`) read `result.building` / `result.site` and ignore the
  always-empty seed counts + conflicts.
- Rewrite `handleBuildingCreate` / `handleSiteCreate` to a single call:
  - **conflicts returned (force was false)** → show the existing reparent
    confirm, then retry once with `force_clear_conflicting_rack_membership: true`.
  - **success** → `bumpEntities()` / `refreshSitesAndBump()`, open the manage
    modal with **actual** counts from the response (not `seed.*.length`).
  - **error** → keep the create modal/seed open, toast; nothing created
    (atomicity) → no rollback, no orphan.
- Keep client fast-fail preflights (`batchCapError`, `forceClearRackMembership &&
  !canManageRacks`) as UX; server is authoritative.
- Seed types (`BuildingCreateSeed`/`SiteCreateSeed`) and all call sites
  (`MinerReparentPicker`, `FleetBuildingsPage`, `RacksPage`) are **unchanged** —
  they already carry `rackIds/minerIds/buildingIds/conflictCount/
  forceClearRackMembership`.

## Testing

- **Domain** (`buildings/service_test.go`, `sites/service_test.go`;
  `fakeTransactor` + `inTxCtx` + `gomock.InOrder`): happy path (entity created +
  racks + devices assigned in one tx, correct counts); unforced conflict →
  sentinel rollback, **nothing created**, conflicts returned; forced conflict →
  rack membership cleared then assigned; mid-seed failure (rack over-capacity or
  device store error) → whole tx rolls back, no entity; lock-order assertions via
  `InOrder`.
- **Domain — mixed / skip-level seeding**: seed shapes that skip a level land on
  the right direct-FK path, all in one tx:
  - `CreateSite` with only `device_identifiers` (miners with no
    rack/building) → `device.site_id` set directly, `building_id` NULL, no rack
    row; no building/rack assign calls fire.
  - `CreateSite` with only `rack_ids` (racks with no building) →
    `rack.site_id` set, `building_id` NULL.
  - `CreateSite` with a **mix** (buildings + loose racks + loose miners
    in one request) → all three assign cores run in canonical order and the
    response counts sum correctly.
  - `CreateBuilding` with only `device_identifiers` (miners with no rack)
    → `device.building_id` set directly, cascading `site_id`.
  - Skip-level device seed where a seeded miner sits in a rack at another site:
    unforced → `DEVICE_IN_RACK_AT_OTHER_SITE` conflict, nothing created; forced →
    rack membership dropped, miner moved.
- **Handler** (`handler_test.go`; `newTestHandler` + `sitePermsCtx`):
  `site:manage` required; `rack:manage` additionally required with the force
  flag; conflict-response mapping; response shape.
- **Client** (vitest on the provider; Conductor worktree needs the temp
  node_modules symlink): single RPC invoked with correct request; conflict
  response → confirm → retry with force; success → manage modal with actual
  counts; error → modal stays open, no orphan.
- **Optional E2E** (`proto-fleet-playwright-e2e`): create-building-with-racked-
  miners happy path.

## Risks & call-outs

- **Refactor blast radius** — extracting tx-cores edits the two busiest domain
  methods. Land the extraction as its own commit; keep the public methods
  behavior-identical; run the full domain suite before adding the new methods.
- **Sentinel-error passthrough** — the one thing to verify empirically against
  `SQLTransactor` (see Conflict rollback).
- **Site slug-collision loop** — standalone `CreateSite` retries the INSERT
  under autocommit. In the combined flow the collision aborts/poisons the whole
  seed transaction, so retrying just the INSERT inside the same tx is not
  possible. As implemented, the retry loop regenerates the slug and re-runs the
  **entire create+seed transaction** (a fresh `RunInTxWithResult`) on
  `ErrSiteSlugCollision`, tracking used slugs across attempts.
- **Activity logging** — move logging out of the tx-cores into the wrappers; the
  combined methods emit a coherent post-commit event (created + seeded N racks /
  M miners) and must still capture the site-scope metadata existing events use.

## Commit sequence

*(Final shape after the Decision #1 collapse — the seed lives on the existing
`CreateBuilding` / `CreateSite` RPCs, so there is no separate `*WithSeed`
proto/server/client surface.)*

1. `refactor(buildings,sites): extract tx-scoped create/assign cores` — no
   behavior change; existing tests green.
2. `feat(proto): add seed fields to CreateBuilding + CreateSite` — proto +
   `just gen` output, one commit.
3. `feat(server): transactional create-and-seed on CreateBuilding/CreateSite +
   handlers + RBAC` — + domain/handler tests.
4. `feat(client): single-RPC create-and-seed flow` — + vitest.
5. `test(e2e)` — optional.
