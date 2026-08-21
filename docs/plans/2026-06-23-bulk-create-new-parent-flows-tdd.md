---
title: "Multi-site UI hardening: create-new parent from bulk/row actions"
date: 2026-06-23
status: implementing
type: tdd
tracker: https://github.com/block/proto-fleet/pull/551
---

# Multi-site UI hardening: create-new parent from bulk/row actions

## Context

Three tightening tasks land in one UI-hardening PR. Tasks 1–2 are done and
type-check clean; this doc covers Task 3, which has the only real
architecture.

- **Task 1 (done):** `RackSettingsModal` — auto-labeler removed, label before
  zone, zone optional.
- **Task 2 (done):** `FleetSitesPage` / `FleetBuildingsPage` first-run empty
  branch renders the shared `NullState` and skips the filter row, matching the
  Racks tab.
- **Task 3 (this doc):** every "Add to rack/building/site" picker — bulk **and**
  single-row — gains a "New …" launch button that runs the existing creation
  flow with the current selection pre-seeded.

## Decisions (agreed)

1. **Hoist the create flows into `FleetLayout`.** Rather than navigate between
   pages to reach a create flow, a single controller mounted in `FleetLayout`
   hosts the rack/building/site create modal stacks and exposes launch functions
   via the fleet outlet context. Any tab launches them in place. Standalone
   routes (no `FleetLayout` Outlet) read the launcher through
   `useOptionalFleetOutletContext`; when absent, the "New …" button is hidden.
2. **Option (i) for off-diagonal selections.** When the selected item type does
   not match the target modal's position panel, the items are assigned to the
   new parent as **direct members** (no position) and surfaced as a count line
   under the left-panel list ("n miners unassigned to a rack", "n racks
   unassigned to a building"). Richer naked-item UI is a deferred design pass.
3. **Conflict warning reuse (B).** Before entering a create flow, run the same
   membership-conflict detection as the normal reparent flow; if any selected
   item already belongs to another parent, show the existing warn dialog and
   only continue on confirm.

## Seeding matrix

| Source  | "New …"      | Lands on            | Seed                                              |
| ------- | ------------ | ------------------- | ------------------------------------------------- |
| Miners  | New rack     | `ManageRackModal`   | position-seed miners (slot assignment)            |
| Racks   | New building | `ManageBuildingModal` | position-seed racks (aisle/position)            |
| Buildings | New site   | `ManageSiteModal`   | position-seed buildings                           |
| Miners  | New building | `ManageBuildingModal` | direct miners + count line                      |
| Miners  | New site     | `ManageSiteModal`   | direct miners + count line                        |
| Racks   | New site     | `ManageSiteModal`   | direct racks + count line                         |

## Create→manage handoff (per entity)

The handoff differs by modal because of how each persists:

- **Rack — "seed then save at end."** `ManageRackModal` already supports a
  no-`existingRackId` new mode and persists members + slots atomically on save.
  Flow: `RackSettingsModal` → `ManageRackModal({ seededMinerIds })` → save.
  (Seed prop added; done.)
- **Building — "create then assign then manage."** `useBuildingModals.detailsCreate`
  runs `CreateBuilding` and returns the new `Building` without opening manage.
  Flow: conflict check → `detailsCreate` → `assignRacksToBuilding`
  (+ direct miner assignment) → `openManage(newRow)` →
  `ManageBuildingModal` loads the now-assigned racks for positioning + count line.
- **Site — "create then assign then manage (edit mode)."** Constraint:
  `ManageSiteModal` create mode **gates building assignment until the site
  exists** (`useSiteModals.manageSave` create-path runs `CreateSite` with an
  empty delta). So the seeded handoff cannot use create mode. Flow: conflict
  check → `createSite` → `assignBuildingsToSite` (+ direct rack/miner
  assignment) → `openManageEdit(newSite)` → `ManageSiteModal` (edit) loads the
  assigned buildings + count line.

## Components

- **`ParentPickerModal` (done):** `onCreateNewLaunch` + `createNewLaunchLabel`
  render the "New …" button; distinct from the group inline-name path.
- **`FleetCreateFlowProvider`** (new, mounted in `FleetLayout`): owns dedicated
  `useBuildingModals` / `useSiteModals` instances plus rack-create state, renders
  `<BuildingModals>` / `<SiteModals>` / rack modals, and exposes:
  - `launchCreateRack({ minerIds })`
  - `launchCreateBuilding({ rackIds, minerIds })`
  - `launchCreateSite({ buildingIds, rackIds, minerIds })`
  These run the orchestration above (conflict check → create → assign → manage).
- **Outlet context:** add the three launchers (optional) so pages/menus read
  them via `useFleetOutletContext` and hide the button when undefined.
- **Manage modal seed props:** `ManageRackModal.seededMinerIds` (done);
  `ManageBuildingModal` direct-miner count line; `ManageSiteModal`
  direct-rack/miner count line. Position-seed for building/site is achieved by
  assigning before `openManage*`, so no extra seed prop is needed there — the
  modal loads the freshly-assigned children.

## Refresh coordination

The controller's create instances are separate from each page's own
`use*Modals`. After a create, refresh the active list: sites via FleetLayout's
`fetchSites`; buildings/racks via a new outlet-context pulse
(`fleetEntitiesChangedAt`) that list pages watch to refetch.

## Wiring (all bulk + single-row)

- Miners (`MinerReparentPicker`): "New rack/building/site" — resolve selection
  (reuse all-mode resolver), conflict check, then call the matching launcher.
- Racks (`RacksPage`): "New building/site" on bulk + single-row pickers.
- Buildings (`FleetBuildingsPage`): "New site" on bulk + single-row; **add the
  missing bulk "Add to site" action** (currently single-row only).

## Test plan

- Rack: bulk + row "New rack" from miners → settings → manage with seeded
  miners; save persists membership.
- Building: "New building" from racks → manage shows seeded racks; from miners →
  count line; conflict dialog fires when a selected rack/miner has another parent.
- Site: "New site" from buildings → manage (edit) shows assigned buildings; from
  racks/miners → count line.
- Standalone routes (no FleetLayout) hide the "New …" button.
- Type-check + lint + targeted vitest for the controller and pickers.
