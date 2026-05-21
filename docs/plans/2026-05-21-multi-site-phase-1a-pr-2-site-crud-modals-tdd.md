---
title: "Multi-site Phase 1a PR 2: site CRUD modals"
date: 2026-05-21
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/261
---

# Multi-site Phase 1a PR 2: site CRUD modals

## Context

PR 1 ([#198](https://github.com/block/proto-fleet/issues/198)) shipped
the multi-site frontend scaffold: `/sites`, `/settings/sites`, and
`/buildings/:id` routes, the global topbar `SitePicker`, the
read-only `useSites` / `useBuildings` hooks, and inert "Add a site"
CTAs on the empty states and page headers. Every CRUD button currently
either hides itself or renders disabled with a comment pointing at
this PR.

PR 2 wires the site create / edit / delete flow described in master
plan §J3 of
[`docs/plans/2026-05-05-multi-site-support-plan.md`](./2026-05-05-multi-site-support-plan.md).
Operators can stand up sites entirely through the UI: the modal
flow is the only supported way to seed sites going forward. Building
CRUD lands separately in [#262](https://github.com/block/proto-fleet/issues/262)
(PR 3); the building rows inside `ManageSiteModal` and the
`SiteSettingsSingleView` "Add building" CTA remain inert placeholders
until then.

The deferred miner-picker is intentional. Master plan §J3 documents a
two-call orchestration — `CreateSite` then optional
`ReassignDevicesToSite` — but the device-selection UI lands in Phase
1b alongside the miner-list site filter (#199). PR 2 implements the
orchestration plumbing now (Save calls `ReassignDevicesToSite` after
`CreateSite` whenever a `pendingDeviceIds` list is non-empty) but
leaves the list empty by default. When the Phase 1b miner picker
ships, it slots in by populating that list.

## Goals

- `SiteDetailsModal` — form entry modal with create and edit modes,
  reachable from every "Add a site" CTA (`/sites` empty state +
  header, `/settings/sites` empty state + header) and from the
  "Manage site" → "Edit details" path in `ManageSiteModal`. Edit
  mode is reachable from `SiteSettingsSingleView`'s
  "Manage site" → "Edit details" flow.
- `ManageSiteModal` — `FullScreenTwoPaneModal` consumer that drives
  both create (deferred-commit via Continue from `SiteDetailsModal`)
  and edit (via "Manage site" on `SiteSettingsSingleView`). Save
  commits to `CreateSite` / `UpdateSite`. Left pane contains the
  `network_config` textarea + a buildings-table placeholder; right
  pane contains the FPO building-grid preview.
- `SiteDeleteDialog` — cascade-unassign warn-first confirmation
  reading attachment counts from the already-loaded
  `SiteWithCounts` row. Collapses the cascade language when all
  counts are zero. Delete-on-confirm; toast on success/failure;
  refetch sites; reset the topbar `SitePicker` to "All Sites" if
  the deleted site was the active selection.
- Two-call orchestration: `CreateSite` succeeds even when
  `ReassignDevicesToSite` rejects; the UI surfaces a clear toast
  ("site created; miner assignment failed: …") so the operator
  knows to retry from the miner list (Phase 1b).
- Acceptance per issue #261: operator creates 3+ sites entirely
  through the UI, edits one (name + capacity + network config),
  deletes one with the cascade dialog showing impact counts.
  Sites render in both `/sites` and `/settings/sites` without
  page reload.

## Non-goals

- Address / zip / country / notes form inputs — proto fields land in
  the BE follow-up; FE inputs come alongside.
- Building rows inside `ManageSiteModal`'s left pane — #262 (PR 3).
- Real `BuildingCard` in the right-pane preview grid — Phase 1b
  (#263). PR 2 ships FPO grey boxes with the building label only.
- Miner-picker / device-reassignment UI inside `ManageSiteModal` —
  Phase 1b (#199). The orchestration code path is in place; only
  the picker UI is deferred.
- Per-row edit / delete buttons on the All Sites table — delete
  flows through the single-site Manage path per master plan §J3.
- Cross-site building moves — master plan defers to a later phase.
- Rollback of `CreateSite` when `ReassignDevicesToSite` fails —
  master plan §J3 explicitly accepts the partial state and pushes
  recovery to the operator via toast.
- URL deep-link to a specific modal state — modal opens are driven
  by component state, not query params, in this phase.

## Design

### File map

New files:

- `client/src/protoFleet/features/sites/components/SiteDetailsModal/`
  - `SiteDetailsModal.tsx` — form modal, create + edit modes.
  - `SiteDetailsModal.test.tsx`.
  - `index.ts` — default + named re-exports.
- `client/src/protoFleet/features/sites/components/ManageSiteModal/`
  - `ManageSiteModal.tsx` — `FullScreenTwoPaneModal` consumer.
  - `ManageSiteModal.test.tsx`.
  - `index.ts`.
- `client/src/protoFleet/features/sites/components/SiteDeleteDialog/`
  - `SiteDeleteDialog.tsx` — `Dialog` consumer with cascade copy.
  - `SiteDeleteDialog.test.tsx`.
  - `index.ts`.
- `client/src/protoFleet/features/sites/hooks/useSiteModals.ts` —
  shared modal state machine + handlers consumed by both pages so
  the create/edit/delete wiring lives in one place.
- `client/src/protoFleet/features/sites/hooks/useSiteModals.test.ts`.
- `client/src/protoFleet/features/sites/types.ts` — `SiteFormValues`
  shape shared between modals.

Touched files:

- `client/src/protoFleet/api/sites.ts` — add `createSite`,
  `updateSite`, `deleteSite`, `reassignDevicesToSite` to `useSites`.
- `client/src/protoFleet/features/sites/pages/SitesPage.tsx` —
  mount `useSiteModals`, wire `SitesPageHeader.onAddSite` and
  `SitesEmptyState.onAddSite`.
- `client/src/protoFleet/features/sites/pages/SettingsSitesPage.tsx` —
  mount `useSiteModals`, wire the header / empty-state CTAs, and
  pass an `onManage` callback into `SiteSettingsSingleView`.
- `client/src/protoFleet/features/sites/components/SiteSettingsSingleView.tsx` —
  accept `onManage`; replace the disabled stub with the real
  call. The "Add building" button stays inert (deferred to PR 3).

### `SiteFormValues`

```ts
export interface SiteFormValues {
  name: string;
  locationCity: string;
  locationState: string;
  timezone: string;
  powerCapacityMw: number;  // 0 when unset
  networkConfig: string;
}

export const emptySiteFormValues = (): SiteFormValues => ({
  name: "",
  locationCity: "",
  locationState: "",
  timezone: "",
  powerCapacityMw: 0,
  networkConfig: "",
});
```

Form draft is held in `useSiteModals` so it survives bouncing between
`SiteDetailsModal` (details) and `ManageSiteModal` (network config +
preview) in the create flow.

### Modal state machine (`useSiteModals`)

```ts
type ModalState =
  | { kind: "none" }
  | { kind: "detailsCreate"; draft: SiteFormValues }
  | { kind: "manageCreate";  draft: SiteFormValues }
  | { kind: "detailsEdit";   site: Site; draft: SiteFormValues }
  | { kind: "manageEdit";    site: Site; draft: SiteFormValues }
  | { kind: "deleteConfirm"; site: SiteWithCounts };
```

Transitions:

- `openCreate()` → `detailsCreate({ ...empty })`.
- `SiteDetailsModal` Continue (create) → `manageCreate(draft)`.
- `ManageSiteModal` "Edit details" (create) → `detailsCreate(draft)`.
- `openEdit(site)` → `manageEdit(site, draftFromSite)`.
- `ManageSiteModal` "Edit details" (edit) → `detailsEdit(site, draft)`.
- `SiteDetailsModal` Save (edit) → calls `updateSite`, closes on
  success, refetches.
- `SiteDetailsModal` Delete (edit) → `deleteConfirm(site)`.
- `ManageSiteModal` Save (create) → calls `createSite` then
  optionally `reassignDevicesToSite`; closes on success; refetches.
- `ManageSiteModal` Save (edit) → calls `updateSite`; closes on
  success; refetches.
- `SiteDeleteDialog` Confirm → calls `deleteSite`; closes on
  success; refetches; resets active SitePicker selection to "all"
  if the deleted site was active.

### `SiteDetailsModal`

- Built on `Modal` (standard size).
- Title: "Add site" (create) / "Edit site" (edit).
- Fields (in order):
  - `name` — required, 1–255.
  - `locationCity` — optional, ≤255.
  - `locationState` — optional, ≤255.
  - `powerCapacityMw` — optional, number ≥ 0. Rendered as text
    `Input` with `units="MW"`; non-numeric input shows inline error.
  - `timezone` — optional, ≤64.
- Buttons:
  - Create: `[Cancel] [Continue]`. Continue disabled when name is
    empty; on click calls `onContinue(values)`.
  - Edit: `[Delete] [Cancel] [Save]`. Save disabled when name is
    empty; on click calls `onSave(values)` (async — disables while
    in flight). Delete calls `onDeleteRequested()`.
- Validation runs on blur + on submit; surface inline errors via
  the existing `Input.error` prop.

### `ManageSiteModal`

- Built on `FullScreenTwoPaneModal`.
- Title: "Manage Site".
- Header buttons (laptop): `[Edit details] [Save]`. Mobile overflow
  inherits `FullScreenTwoPaneModal`'s default split.
- Left pane:
  - `Textarea` bound to `draft.networkConfig`. Label "Network
    config". Placeholder text mirrors master plan §"Network config
    validation" guidance ("One CIDR or IP per line; max 16 KB").
  - "Buildings" heading + FPO `PlaceholderBlock` (label
    "Buildings table — lands in #262"). Inert "Add building" stub.
- Right pane:
  - Header strip: top-left `site.name || draft.name || "Untitled
    site"` over `"${city}, ${state}"`; top-right `"${capacity} MW /
    ${n} buildings"`.
  - Grid: FPO `PlaceholderBlock` cells, one per existing building
    in edit mode (driven by `listBuildingsBySite` for the active
    site), or a single empty-state placeholder in create mode.
- Save:
  - Create: `createSite(draft)`. On success, replace `draft.networkConfig`
    with the canonical value from the response. If
    `network_config_warnings` is non-empty, render an inline
    `Callout` (intent `warning`) above the panes summarizing the
    warnings; the modal stays open so the operator can review the
    canonical text. The follow-up Save with no further edits closes
    the modal (idempotent UpdateSite is acceptable; warnings on
    `CreateSite` are advisory, not blocking).
  - Edit: `updateSite({ id, ...draft })`. Same warning surface.
- Two-call orchestration (create only): after `createSite` succeeds
  and warnings are clear, if `pendingDeviceIds.length > 0` call
  `reassignDevicesToSite({ targetSiteId, deviceIdentifiers })`. On
  reassign failure (`conflicts.length > 0` or transport error)
  push a toast `"site created; miner assignment failed: <msg>"`
  and close the modal — the site row is real and `/sites` will
  reflect it on refetch.
- Pending-state: Save button shows spinner + disables; Edit details
  + close icon disable while saving.

### `SiteDeleteDialog`

- Built on `Dialog`.
- Title: `Delete site "${name}"?`.
- Subtitle / body:
  - When any cascade count > 0:
    `"Deleting will unassign N miners, M racks, and P buildings. They will be removed from this site."`
  - When all counts are 0:
    `"Are you sure you want to delete this site?"`
- Buttons: `[Cancel] [Delete site]` (danger variant).
- Confirm → `deleteSite({ id })`. Toast success/failure. On success
  the modal closes, the page refetches sites, and the active
  SitePicker selection is reset to "all" if it matched.

### API surface additions (`api/sites.ts`)

Each new entry follows the existing `listSites` pattern: returns
`Promise<void>`, accepts `signal`, `onSuccess`, `onError`,
`onFinally`, and routes failures through `useAuthErrors`.

- `createSite({ values, signal?, onSuccess?, onError?, onFinally? })`
  — `onSuccess(site, warnings)`.
- `updateSite({ id, values, ... })` — same shape.
- `deleteSite({ id, ... })` — `onSuccess(counts)` where counts
  pass through the response (caller uses them for the toast).
- `reassignDevicesToSite({ targetSiteId?, deviceIdentifiers, ... })`
  — `onSuccess(reassignedCount)`, `onError(message, conflicts?)` so
  the modal can decide how to message partial failures. The
  underlying response carries `conflicts` only on rejection; the
  hook surfaces them through the error callback by widening the
  error signature with an optional second argument.

### Active-site reset on delete

`useSiteModals` accepts the current `activeSite` and the
`useActiveSite` setter as inputs (mirroring how
`SiteSettingsSingleView` already consumes them). On a successful
delete, if `activeSite.kind === "site"` and its id matches the
deleted row, it calls `setActiveSite({ kind: "all" })`.

### Refetch strategy

`useSiteModals` accepts a `refetchSites` callback supplied by the
host page (`SitesPage` / `SettingsSitesPage`); each successful
mutation invokes it. Buildings refetch is deferred to PR 3 — the
right-pane preview reads its own list lazily via `listBuildingsBySite`
on mount.

## Test plan

Unit tests use `@testing-library/react` + Vitest, matching the
existing repo conventions (see `SiteSettingsSingleView.test.tsx`).
Connect-RPC clients are mocked at the `sitesClient` module boundary.

### `SiteDetailsModal`

- Create mode: Continue disabled until name is set; clicking
  Continue invokes `onContinue` with the typed values.
- Edit mode: pre-populates inputs from `initialValues`; Save calls
  `onSave` with the typed values; Delete calls `onDeleteRequested`.
- `powerCapacityMw` rejects non-numeric input.
- Cancel calls `onDismiss` and preserves no state.

### `ManageSiteModal`

- Create + Save → `sitesClient.createSite` called with the draft
  values; on success refetch + close.
- Create + Save with warnings → modal stays open, `Callout` with
  warnings renders, textarea contents replaced with canonical value.
- Edit + Save → `sitesClient.updateSite` called with id + values.
- Edit details → consumer callback fires with current draft (page
  re-opens `SiteDetailsModal`).
- Pending miner reassignment failure → toast "site created; miner
  assignment failed: ..." and modal closes; no rollback.

### `SiteDeleteDialog`

- Renders cascade language when any count is non-zero.
- Collapses to bare confirm when all counts are zero.
- Confirm calls `sitesClient.deleteSite`; success refetches.

### `useSiteModals`

- State transitions for each user action match the table above.
- `setActiveSite({ kind: "all" })` is called after a successful
  delete of the currently active site; not called otherwise.

### Page integration (`SitesPage`, `SettingsSitesPage`)

- Empty-state "Add a site" opens `SiteDetailsModal`.
- Header "Add a site" opens `SiteDetailsModal`.
- `SiteSettingsSingleView` "Manage site" opens `ManageSiteModal`
  in edit mode for the active site.
- After a successful create / update / delete the sites list
  refetches and re-renders without a navigation event.

## Risks and mitigations

- **Partial-state from failed reassign.** Master plan accepts this;
  mitigation is the explicit toast + site row landing in the list
  so the operator can re-try from the miner list once Phase 1b
  ships. Documented in code with a `// Phase 1b miner picker fills
  this in` comment so the orchestration site is obvious.
- **Network-config canonicalization confusion.** Server may rewrite
  whitespace, dedupe entries, or normalize CIDR formatting. The
  modal replaces textarea contents with the canonical form on
  success so the operator sees what the server stored — no silent
  drift.
- **SitePicker pointing at a deleted site.** Solved by the
  `setActiveSite({ kind: "all" })` reset on delete; covered in tests.
- **Modal stack: ManageSiteModal + SiteDetailsModal overlap during
  the create flow.** Both modals use the existing modal infrastructure
  with sane z-index defaults; switching between them goes through
  the same state machine, so only one is mounted at a time.

## Open questions

- Should the create flow's `network_config_warnings` block Save (PR
  must re-Save to confirm) or pass through silently? Working answer:
  block the close on the first surfaced warning, allow second Save
  to confirm. Will revisit during code review.
- Should the All Sites table grow a per-row "Manage" affordance now,
  or wait for an explicit user ask? Working answer: defer. Single-
  site mode is one click away through the row.
