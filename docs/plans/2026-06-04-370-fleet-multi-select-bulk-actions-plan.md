---
title: "Fleet Sites + Buildings multi-select & bulk actions"
date: 2026-06-04
status: draft
type: plan
tracker: https://github.com/block/proto-fleet/issues/370
---

# Fleet Sites + Buildings multi-select & bulk actions

## Context

#368 shipped the `/fleet` tab scaffold (Sites / Buildings / Racks /
Miners). #369 (PR [#412](https://github.com/block/proto-fleet/pull/412),
merged 2026-06-11) shipped the per-row ellipsis menu pattern on the
Sites, Buildings, and Racks tabs via a unified `FleetGroupActionsMenu`.

The Miners tab already has a polished multi-select + bulk-action UX:
row checkboxes feed `MinerList`'s `selectedMiners` state, which renders
a bottom-fixed `MinerListActionBar` (built on the shared `ActionBar`
shell) hosting `MinerActionsMenu`.

This task (#370) ports that same UX to the Sites and Buildings tabs.

## Mechanical parity with Miners-tab bulk

The two flows are mechanically identical end-to-end except for two
boundary conditions:

| Stage | Miners tab | Sites/Buildings tab |
| --- | --- | --- |
| Selection source | Row checkboxes → `selectedMiners: string[]` | Row checkboxes → `selectedScopes: GroupScope[]` |
| **Device-id resolution** | **Direct from selection** | **Lazy fetch:** `listMinerStateSnapshots({filter: {siteIds | buildingIds: scopes.map(s => s.id)}})` paginates and collects the union (existing logic in `FleetGroupActionsMenu.fetchDeviceIds`, just generalized to id arrays) |
| Selection-mode semantics | Supports `"all"` (fleet-wide via `currentFilter`) and `"subset"` | `"subset"` only — ids are always materialized upfront |
| Action handler hook | `useMinerActions` | `useMinerActions` (same hook) |
| Batch RPC dispatch | `useBatchActions` → `startBatchOperation` / `completeBatchOperation` | `useBatchActions` (same) |
| Confirmation dialog | `BulkActionConfirmDialog` | `BulkActionConfirmDialog` (same) |
| Unsupported-miners gate | `UnsupportedMinersModal` | `UnsupportedMinersModal` (same) |
| Modal stack | `MinerActionModalStack` | `MinerActionModalStack` (same — already used by `FleetGroupActionsMenu`) |
| Pool selection flow | `PoolSelectionPageWrapper` | `PoolSelectionPageWrapper` (same) |
| Permission gating | `usePermittedActions` + `ACTION_PERMISSIONS` | Same (already implemented in `FleetGroupActionsMenu` via `permittedKeys`) |
| Toast lifecycle | `useBatchActions` toasts + manual loading toast | Same (already implemented) |
| Bar shell | `ActionBar` (shared) | `ActionBar` (shared — reuse) |
| Bar position | `fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50` | Same |
| Global toaster push-up | `setActionBarVisible(true)` from Zustand UI slice | Same — reuse `useSetActionBarVisible` |
| Set hidden during modal flows | `onActionStart` / `onActionComplete` → `setHidden(true/false)` | Same — needs to be added to `FleetGroupActionsMenu` (see Approach) |

**Action-set delta** (J10): Sites and Buildings tabs do NOT surface
`rename`, `updateWorkerNames`, or the per-row "View *" / "Edit *"
extras in the bulk menu. They expose only the top-wired + bottom-wired
clusters that `FleetGroupActionsMenu` already renders for the per-row
case: sleep / wake / reboot / download logs / manage power / update
firmware / edit pool / add-to-group / manage security / unpair.

That leaves only **one** structural difference: id resolution is
filter-aggregated rather than selection-direct. Everything downstream
(modals, confirmations, toasts, RPCs, permission gates, ActionBar shell)
is the same code path.

## Goal

Operator on `/fleet/sites` or `/fleet/buildings`:

1. Checks one or more rows.
2. Bottom-fixed `ActionBar` slides in — same shell as Miners tab.
3. Picks an action via `FleetGroupActionsMenu`.
4. Confirmation dialog announces affected miner count across the
   union of descendants.
5. Action fans out via a single `listMinerStateSnapshots` call
   filtered by the combined id set, then runs the existing
   `useMinerActions` flow.

## Scope

### In scope

- Row-checkbox column on `SiteList` and `BuildingList` (leftmost),
  via `List`'s existing `customSelectedItems` + `customSelectionMode`
  controlled props.
- Selection state owned by the page (`FleetSitesPage`,
  `FleetBuildingsPage`). Id-keyed; pruned against current visible
  items on each poll (borrow `MinerList`'s effect).
- Bottom-fixed `ActionBar` rendered when `selection.length > 0`.
  Hosts `FleetGroupActionsMenu` in `renderActions`.
- `Select all visible` / `Select none` controls in `selectionControls`.
- Extend `FleetGroupActionsMenu`:
  - prop rename `scope: GroupScope` → `scopes: GroupScope[]`
    (same kind for all entries), or accept `scopes` alongside `scope`
    with one acting as a one-element shim;
  - filter construction uses `scopes.map(s => s.id)`;
  - toast/dialog labels: `scopeLabel = scopes.length === 1 ?
    scopes[0].name : "${n} ${pluralize(kind, n)}"`;
  - new `onActionStart` / `onActionComplete` props, forwarded to
    `useMinerActions` (mirrors `MinerActionsMenu`);
  - host-supplied `extraActions` continue to render for the single-scope
    case; multi-scope bar calls with `extraActions = []`.
- Extend shared `ActionBar`:
  - parameterize the noun in `selectionText` (currently hardcoded
    "miner"/"miners"). Add an optional `itemNoun: { singular: string,
    plural: string }` prop with default `{singular: "miner", plural:
    "miners"}` for backward-compat.
- Update existing `FleetGroupActionsMenu.test.tsx` for the
  array-of-scopes path (single-scope continues to pass via length-1
  array).
- Playwright E2E: select 2 sites on `/fleet/sites` → reboot → confirm
  → assert toast + fake-rig receives the union device set.

### Out of scope

- Adding actions beyond the existing top/bottom wired clusters
  (no rename, no worker-names).
- Mixed-kind selection (one site + one building); `scopes[]` requires
  same `kind`. Sites tab can only produce site scopes and vice versa.
- `"all"` selection mode for sites/buildings; we always materialize
  ids from the picked scopes.
- Quick-action shortcuts in the bar (reboot/blink/etc.). Phase 2 if
  operators ask.
- Saved views, list/grid toggle.

## Approach

### Selection wiring (mirrors `MinerList`)

`SiteList` gains controlled-selection props:

```ts
selectedIds?: string[];
onSelectedIdsChange?: (ids: string[]) => void;
```

Pass-through to `List` as `customSelectedItems` /
`customSelectionMode`. When `selectedIds === undefined` the checkbox
column is hidden — the existing per-row `FleetGroupActionsMenu`
continues to work standalone. `BuildingList` is identical.

`FleetSitesPage` / `FleetBuildingsPage` own a `useState<string[]>`
for selection and prune it against the visible item set on each poll
(`MinerList` already has this pattern; copy it).

### Bottom-fixed ActionBar (mirrors `MinerListActionBar`)

New `FleetGroupListActionBar` component, modeled directly on
`MinerListActionBar.tsx`:

```tsx
<ActionBar
  className="fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50"
  selectedItems={selectedIds}
  selectionMode="subset"
  itemNoun={{ singular: kind, plural: pluralKind }}  // "site"/"sites" or "building"/"buildings"
  onClose={onClearSelection}
  selectionControls={<SelectAllVisible /><SelectNone />}
  renderActions={(setHidden) => (
    <FleetGroupActionsMenu
      scopes={selectedScopes}
      ariaLabel={...}
      testIdPrefix="fleet-bulk-actions"
      onActionStart={() => { setHidden(true); setActionBarVisible(false); }}
      onActionComplete={() => { setHidden(false); setActionBarVisible(true); }}
    />
  )}
/>
```

The bar's `useSetActionBarVisible(selection.length > 0)` effect mirrors
`MinerListActionBar`, including the cleanup on unmount, so the
global toaster pushes up the same way it does on /miners.

Page renders the bar conditionally — same JSX shape as how
`MinerList.tsx` mounts `MinerListActionBar` only when selection > 0.

### `FleetGroupActionsMenu` multi-scope extension

Prop shape change (single `scope` → `scopes: GroupScope[]` with
`scopes.length >= 1`):

```ts
interface FleetGroupActionsMenuProps {
  scopes: GroupScope[];           // same kind across all entries
  ariaLabel: string;
  testIdPrefix?: string;
  extraActions?: RowAction[];     // ignored when scopes.length > 1
  onActionStart?: () => void;
  onActionComplete?: () => void;
}
```

Inside, build the filter from id arrays:

```ts
const ids = scopes.map((s) => s.id);
const kind = scopes[0].kind;
const filterInit =
  kind === "building" ? { buildingIds: ids }
  : kind === "rack"   ? { rackIds: ids }
  :                     { siteIds: ids };
```

`scopeLabel` replaces the existing `scope.name` references in the
loading toast and error path:

```ts
const scopeLabel = scopes.length === 1
  ? scopes[0].name
  : `${scopes.length} ${pluralize(kind, scopes.length)}`;
```

Existing single-scope call sites in `SiteList` / `BuildingList` /
`RacksPage` migrate to `scopes={[scope]}` — one-line change per
caller.

Forward `onActionStart` / `onActionComplete` into
`useMinerActions({...})` so the bar can hide-during-modal exactly
like `MinerListActionBar` does.

### `ActionBar` noun parameterization

Today's hardcoded "miner"/"miners" in `selectionText` becomes:

```ts
const noun = itemNoun ?? { singular: "miner", plural: "miners" };
const selectionText = `${count} ${count === 1 ? noun.singular : noun.plural} selected`;
```

No other call sites change (default keeps Miners-tab behavior).

## File-level breakdown

Modified:

- `features/fleetManagement/components/ActionBar/ActionBar.tsx`
  — add optional `itemNoun` prop; default to current `"miner"/"miners"`.
- `features/fleetManagement/components/FleetGroupActionsMenu/FleetGroupActionsMenu.tsx`
  — `scope` → `scopes`; multi-scope filter construction; `scopeLabel`
  helper; `onActionStart` / `onActionComplete` forwarded to
  `useMinerActions`; skip `extraActions` when `scopes.length > 1`.
- `features/fleetManagement/components/FleetGroupActionsMenu/FleetGroupActionsMenu.test.tsx`
  — migrate fixtures to `scopes={[…]}`; add multi-scope cases.
- `features/fleetManagement/components/SiteList/SiteList.tsx`
  — accept `selectedIds` / `onSelectedIdsChange`; pass to `List`;
  update single-scope call site to `scopes={[scope]}`.
- `features/fleetManagement/components/BuildingList/BuildingList.tsx`
  — same.
- `features/rackManagement/pages/RacksPage.tsx`
  (and rack row consumers) — update existing single-scope call site to `scopes={[scope]}` and add rack multi-select + bulk actions via `FleetGroupListActionBar`.
- `features/fleetManagement/pages/FleetSitesPage.tsx`
  — selection state; mount `FleetGroupListActionBar` when count > 0.
- `features/fleetManagement/pages/FleetBuildingsPage.tsx`
  — same.

New:

- `features/fleetManagement/components/FleetGroupActionsMenu/FleetGroupListActionBar.tsx`
  — direct analog of `MinerListActionBar`. Hosts `FleetGroupActionsMenu`
  in `renderActions`; wires `Select all visible` / `Clear` /
  `useSetActionBarVisible` lifecycle.
- `features/fleetManagement/components/FleetGroupActionsMenu/pluralize.ts`
  (or inline) — `pluralize("site", n)` → `"site" | "sites"`, ditto
  building, rack.

Tests:

- `SiteList.test.tsx` / `BuildingList.test.tsx` — checkbox column
  toggles with `selectedIds`; absent when prop omitted.
- `FleetSitesPage.test.tsx` / `FleetBuildingsPage.test.tsx` — bar
  mounts at selection ≥ 1; Add CTA still visible (separate slot);
  Select all / Clear behavior.
- `FleetGroupListActionBar.test.tsx` — wires the menu with `scopes`;
  `onActionStart` flips `setHidden`; `onActionComplete` restores.
- `ActionBar.test.tsx` — add a case for `itemNoun` override
  ("3 sites selected").

E2E (`client/e2eTests/protoFleet/spec/`):

- `multiSite.spec.ts` (new): select two sites on `/fleet/sites` →
  open bottom bar → reboot → confirm → assert fake-proto-rig
  received the union device set.

## Risks

- **Visible-set prune mid-selection.** Selection must drop ids that
  disappear from the visible list (site deleted, picker switched).
  Copy the `MinerList` effect verbatim.
- **`scopes` mixed-kind misuse.** Defensive: derive `kind` from
  `scopes[0]` and console-warn on mismatch in dev. Sites tab and
  Buildings tab cannot mix; not a real production risk.
- **Two ActionBars at once.** Only one `/fleet/*` tab renders at a
  time, so `setActionBarVisible` collisions can't happen. Tab swap
  unmounts the bar via the cleanup effect.
- **50k miner cap on bulk.** Already enforced inside
  `FleetGroupActionsMenu.fetchDeviceIds`. The union across multiple
  scopes can plausibly hit the cap; existing error toast applies
  ("Too many miners… filter the list and try again"). No new code
  needed.
- **ActionBar noun churn.** Adding `itemNoun` touches a shared
  component; default preserves current behavior for `MinerListActionBar`.
  Snapshot/test coverage on `ActionBar.test.tsx` should catch
  regressions.

## Test plan

- Unit: list checkbox plumbing; page bar mount/unmount; `scopes` →
  filter-arg shape; pluralization; `onActionStart`/`onActionComplete`
  hide/restore.
- Integration: `FleetGroupListActionBar` end-to-end with
  `useMinerActions` mocked → asserts `listMinerStateSnapshots` called
  with correct id array; confirmation dialog text shows union miner
  count.
- E2E: 2-site bulk reboot via fake-proto-rig.

## Rollout

Behind `MULTI_SITE_ENABLED` — Sites + Buildings tabs are already
gated. No new flag.

## Open questions

1. **Select-all semantics.** Sites/Buildings tabs aren't paginated
   server-side, so "Select all visible" == "Select all matching".
   Mirror MinerListActionBar's labels for consistency. If pagination
   lands later, revisit `selectionMode: "all"` parity.
2. **Quick actions in the bar.** Miners-tab bar has quick-action
   buttons (reboot/blink/manage-power) at the laptop+ breakpoint.
   Defer for Sites/Buildings unless operator feedback pushes for them
   — top-wired actions are already one click away in the popover.
3. **Edit single selection.** When `scopes.length === 1` on the bulk
   bar, should we still surface "Edit site" / "View site"? Current
   plan says no (matches Miners-tab bar — single-row uses ellipsis
   menu for navigation; bulk bar is action-only). Worth confirming
   with design.
