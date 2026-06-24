---
title: "Multi-site: apply site scope to Groups pages"
date: 2026-06-23
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/520
---

# Multi-site: apply site scope to Groups pages

## Context

PR [#516](https://github.com/block/proto-fleet/pull/516) shipped the
path-based site-scope foundation (`/:siteScope/{dashboard,fleet,groups,…}`
routes, `SiteScopeProvider`/`useRouteSiteScope`, the `ActiveSite` union,
`scopedPath`, `siteFilterFromActive`). PR
[#519](https://github.com/block/proto-fleet/pull/519) scoped the Dashboard
using that foundation. PR [#539](https://github.com/block/proto-fleet/pull/539)
("Preserve site scope from detail pages") made detail-page back-links carry
scope via `scopedPath`.

The scoped `/groups` route currently **renders org-wide group data**. PR
#516 only made the route capable of carrying the selected site; it did not
scope the group list, the detail page, or bulk actions. This TDD covers
#520.

All work lands behind `MULTI_SITE_ENABLED` (`VITE_MULTI_SITE_ENABLED`, off
in prod).

## Product decisions (this issue, as directed)

These are the operator-facing decisions for this issue. **A couple are
reconciled against the #520 issue text — see "Decisions reconciled with the
issue text" below.**

1. **List membership scoping.** `/{site}/groups` shows **only groups that
   contain at least one device in the selected site.** Groups with zero
   in-scope devices are hidden (not shown empty).
2. **Row metrics stay org-wide (cross-site).** Each visible row still shows **org-wide**
   miner counts, issue counts, hashrate/efficiency/power/temperature, and
   health composition. Group identity and its rollups are global; the site
   scope only decides *which groups appear*, not what the numbers mean.
3. **Bulk actions are site-scoped, with a confirmation gate.** A bulk action
   triggered from a site-scoped Groups page applies **only to the group's
   miners in the selected site.** Before executing, show a confirm dialog
   stating the action applies to **X of Y** miners (in-scope of total) for
   that group.
4. **Detail page is a canonical, unscoped URL — match rack/building.** The
   group detail page renders the same group content regardless of the
   current site selection, so its URL carries no site segment
   (`/groups/:groupLabel`), exactly like `/racks/:id` and `/buildings/:id`.
   Scope still round-trips: the page reads `activeSite` from the Zustand UI
   store (`useFleetStore(state.ui.activeSite)`) and builds its back/parent
   links with `scopedPath`, just as `RackOverviewPage`/`BuildingPage` do.
5. **`/unassigned/groups`** shows groups that contain at least one device
   currently assigned to no site (`site_id IS NULL`); same row/metrics rules
   as a site scope.

## Decisions reconciled with the issue text

- **Counts/metrics: issue floats "site-filtered device counts/metrics";
  this issue keeps them org-wide.** Rows reflect cross-org telemetry/issue/
  miner counts (decision #2). Groups are unique in that they legitimately
  span sites, so org-wide rollups are the meaningful number; the site scope
  decides *which groups appear*, not what each number means. Consequence: a
  row can show "500 miners / 12 issues" while a bulk action only touches the
  30 in-scope — which is exactly why the confirm dialog (decision #3)
  exists. **Cross-site exposure here is an accepted decision (team,
  2026-06-23): "org-wide" means literally every site, regardless of the
  viewer's per-site access — see the RBAC section.**
- **Detail URL: issue AC says "update group links to use `scopedPath`";
  this issue instead matches the rack/building canonical-detail pattern
  (decision #4).** The detail page renders the same content regardless of
  selection, so a scoped URL would falsely imply a filter that isn't
  applied. This is *not* a divergence from house style — it aligns the group
  detail with `/racks/:id` and `/buildings/:id`. The #539 work scoped detail
  *back-links* (via Zustand `activeSite`), which we keep; it did not scope
  the rack/building detail *URLs*. `GroupOverviewPage` is currently the
  outlier (scoped URL, reads `useRouteSiteScope`); this issue brings it in
  line. **Confirm with eng that aligning group detail to rack/building is
  preferred over the literal #520 AC wording.**

## RBAC interplay — the cross-site exposure question

This feature deliberately surfaces **org-wide** telemetry/counts on a
site-scoped page (decision #2). Because groups can span sites, that means a
group row can show numbers aggregated from sites the viewer may not be
authorized to read. We need to be deliberate about this against the RBAC
model.

**What the RBAC system actually is (implemented, enforced):**

- Permissions can be granted at **org scope or site scope** —
  `user_organization_role.scope_type ∈ {org, site}`, `scope_id` → `site`
  (`server/sqlc/queries/user_organization_role.sql`).
- `authz.ResourceContext{ SiteID *int64 }` lets a handler check a permission
  *at a specific site*; site-scoped grants override the org grant at that
  site (`server/internal/domain/authz/effective.go:21-50`).
- There is a distinct `site:read` (`authz/catalog.go`), separate from
  rack/fleet read.

**The gap that makes this risky:**

- Every deviceset/group handler gates with empty `ResourceContext{}`
  (`server/internal/handlers/deviceset/handler.go` — `RequirePermission(ctx,
  PermRackRead, authz.ResourceContext{})` on every method). That checks an
  **org-level** grant and never clamps to the caller's accessible sites.
- The collection service treats groups as org-wide by design ("org-scoped
  (devices may span sites) so this skips the rack site lock",
  `collection/service.go`). So `ListDeviceSets` / `GetDeviceSetStats` /
  `ListDeviceSetMembers` return cross-site data with no per-site filtering.
- The RBAC plan (`docs/plans/2026-05-19-001-feat-granular-rbac-plan.md`,
  ~lines 254-259) says list endpoints **should** be *filtered* to the
  caller's accessible sites rather than denied — **but that filtering is not
  implemented yet.**

**Decision (team, 2026-06-23): cross-site exposure is accepted for
groups.** Groups deliberately surface org-wide (cross-site) telemetry and
counts regardless of the viewer's per-site access. Rationale: a group is a
single cross-site entity, and a partial/clamped rollup would be misleading
without heavy disclaimer; the meaningful number is the whole group. This
matches the behavior on `/groups` today (group endpoints gate at org level
only). So **no accessible-sites clamp is required for #520** — the list,
row aggregates, member counts, and detail page all show the full group.

Scope of the decision:

- A user with org-level group/fleet read sees true org-wide rows
  (decision #2), unchanged.
- A **site-restricted** user *also* sees full cross-site group telemetry and
  counts. We are explicitly **not** clamping group rollups to the caller's
  accessible-site set. A group appears in `/{site}/groups` whenever it has
  ≥1 device in the selected site (membership filter, decision #1), but the
  numbers shown remain whole-group.
- This means `ListDeviceSets`/`GetDeviceSetStats`/`ListDeviceSetMembers`
  keep their current org-level gate (empty `ResourceContext{}`); no
  `GetAccessibleSites()` resolver or deviceset clamp is needed.

**Bulk actions are the one place site scope still binds** (decision #3): a
scoped action only touches the in-scope devices, gated by the X/Y dialog —
independent of the RBAC question. Display is whole-group; *action effect* is
site-scoped.

**Caveat carried forward, not blocking:** the RBAC plan's "filter, don't
reject" list semantics still implies a future where site-restricted users
get clamped list responses. If/when that lands org-wide, the team should
revisit whether groups remain an explicit exception. Flag for
`/plan-eng-review` so the exception is recorded, not to block.

## The core gap: group list/members/stats RPCs do not filter by site

| Surface | Hook / call | RPC | Site filter today |
|---|---|---|---|
| Groups list rows | `useDeviceSets().listGroups` | `DeviceSet.ListDeviceSets(type=GROUP)` | **Rejected** — `site_ids` is "only valid when type is RACK; otherwise INVALID_ARGUMENT" |
| Row stats (counts/telemetry) | `getDeviceSetStats` | `DeviceSet.GetDeviceSetStats` | org-wide (no change needed — decision #2) |
| Group members (for actions) | `useDeviceSets().listGroupMembers` | `DeviceSet.ListDeviceSetMembers` | **No** site filter |
| Bulk action execution | `useMinerActions` → batch RPCs | `DeviceSelector.include_devices` (explicit member IDs, org-wide) | **No** — operates on all members |
| Action snapshots | `fetchAllMinerSnapshots({groupIds})` | snapshot list | takes a `MinerListFilter` (supports `siteIds` already) |

### Reference patterns already in tree

- `MinerListFilter` already has `repeated int64 site_ids = 13` +
  `bool include_unassigned = 14`, and the server **AND**s `group_ids` with
  `site_ids` at the SQL level
  (`server/internal/domain/stores/sqlstores/device_filters.go:293-303`
  group EXISTS clause + `:377-393` site clause). So "devices in group AND in
  site" is already expressible via a single `MinerListFilter`.
- The standard site predicate (mirror `siteFilterFromActive`):
  ```sql
  AND (
       (cardinality($site_ids::bigint[]) = 0 AND $include_unassigned::boolean = false)
    OR device.site_id = ANY($site_ids::bigint[])
    OR ($include_unassigned::boolean AND device.site_id IS NULL)
  )
  ```
- Wire semantics: `all` → `site_ids=[]`, `include_unassigned=false` (no
  filter); `site(id)` → `site_ids=[id]`; `unassigned` →
  `include_unassigned=true`.

## Goals

- `/groups` renders the org-wide group list, unchanged.
- `/{site}/groups` lists only groups with ≥1 device in that site; rows show
  org-wide metrics; an explicit empty state when no group qualifies.
- `/unassigned/groups` lists groups with ≥1 unassigned device.
- `/groups/:groupLabel` (detail) is org-wide and refresh-safe; list→detail
  links are unscoped; detail back-link returns to the scoped list.
- Site-scoped bulk actions apply only to in-scope miners, gated by an "X of
  Y" confirm dialog.
- SitePicker selection + refresh preserve the scoped route; navigating
  between Groups and other primary sections preserves scope.

## Non-goals

- Changing `/groups` (all-sites) behavior or the default flag state.
- Scoping group row **metrics** (deliberately org-wide — decision #2).
- Redesigning the actions menu, stats panels, or list columns.
- Scoping other surfaces already handled (Dashboard #519, Activity #522).

## Server / proto changes

### 1. `ListDeviceSets` — allow `site_ids` for `type = GROUP` — **MEDIUM**

Today the handler rejects `site_ids`/`include_unassigned` for GROUP. Lift
that restriction and implement membership-based filtering: a group is
returned iff it has ≥1 member device matching the site predicate.

- **proto** `proto/device_set/v1/device_set.proto` `ListDeviceSetsRequest`:
  update the `site_ids`/`include_unassigned` comments (lines ~363-374) to
  document GROUP support; keep field numbers.
- **handler/service** remove the GROUP guard; route GROUP+site to a query
  that joins `device_set` → `device_set_membership` → `device` and applies
  the standard `device.site_id` predicate inside an `EXISTS`/semi-join, so
  pagination + sort stay server-side. Reuse the existing site predicate SQL.
- **Pairing population:** match the population already used by group stats
  (PAIRED + AUTHENTICATION_NEEDED + DEFAULT_PASSWORD — see
  `reference_device_resolver_pairing_default`) so "appears in list" agrees
  with "has miners" on the row. Pass the paired-like set explicitly.
- **Empty-resolution guard:** a site filter that resolves to zero devices
  must yield zero groups — not fall through to "no filter = all groups."
- **Counts-stay-org-wide guard (P2, easy to get wrong):** the site filter
  must be a **semi-join / `WHERE EXISTS`** that only gates *row inclusion*.
  Do **not** implement it as an inner JOIN onto `device_set_membership` +
  `device`, because if `ListDeviceSets` computes `device_count` in the same
  query, a narrowing JOIN would silently scope the count to the site and
  break decision #2 (org-wide rows). Verify `device_count` is identical for
  the same group on `/groups` and `/{site}/groups`.

*Stats (`GetDeviceSetStats`) stays org-wide — no change (decision #2).*

### 2. Group members scoped to site (for bulk actions + the X/Y count) — **SMALL**

The confirm dialog needs **X** (in-scope members) and **Y** (total
members), and the action must execute on the in-scope set.

- **Y (total):** already available — the row's `DeviceSetStats.device_count`
  (org-wide), or the unscoped `listGroupMembers` length. Use the **same
  population** for X and Y so the ratio is honest.
- **X (in-scope) + action target:** add `site_ids` + `include_unassigned`
  to `ListDeviceSetMembersRequest` and filter members by `device.site_id`
  (same predicate). When site-scoped, `listGroupMembers` returns only
  in-scope member IDs; the action's `include_devices` selector is built from
  those, so execution is automatically scoped. `fetchAllMinerSnapshots`
  already accepts a `MinerListFilter` — add `siteIds` there too so firmware
  checks match the scoped set.

  *Alternative (no member-RPC change):* switch the action selector from
  explicit `include_devices` to `all_devices` = `MinerListFilter{group_ids,
  site_ids}` (server already ANDs them). Cleaner execution, but the menu
  also needs the explicit ID list for snapshot/firmware UX, so we'd still
  fetch members. Scoping `ListDeviceSetMembers` is the smaller, more direct
  change — **preferred**; note the alternative at review.

### Regen

After proto edits run `/regen` (see `proto-regen` skill). Commit generated
Go + TS in the same PR.

## Client changes

### GroupsPage (list) — `features/groupManagement/pages/GroupsPage.tsx`

- It already reads `useRouteSiteScope()` (`activeSite`) and builds the
  detail href via `scopedPath`. Two changes:
  1. **Scope the list fetch:** pass `siteFilterFromActive(activeSite)`
     (`siteIds`/`includeUnassigned`) into `listGroups`. Fold scope into the
     fetch/scope key so changing site re-fetches and discards stale pages
     (follow the existing request-id/pagination pattern).
  2. **Unscope the detail href (decision #4):** change `groupDetailHref` to
     `/groups/${encodeURIComponent(label)}` (drop `scopedPath`).
- **Empty state:** when scoped and the list is empty, show "No groups have
  miners in <site>" rather than the generic empty list.
- Leave stats/columns untouched (org-wide — decision #2).

### GroupOverviewPage (detail) — make it canonical like rack/building

`/racks/:id` and `/buildings/:id` are defined **outside**
`createScopableRoutes` (`router.tsx` ~lines 210-218): unscoped URLs, data
fetched by ID, scope read from `useFleetStore(state.ui.activeSite)` only for
back-nav. `GroupOverviewPage` is currently **inside** the scopable set with
a scoped URL — bring it in line:

- **Route:** move `/groups/:groupLabel` out of `createScopableRoutes` to a
  top-level unscoped route alongside `/racks/:rackId`. Drop the scoped
  `/{site}/groups/:groupLabel` variant.
- **Scope source:** replace `useRouteSiteScope()` with
  `useFleetStore((s) => s.ui.activeSite)` (matches `RackOverviewPage:52`).
- **Data:** unchanged — org-wide, fetched by group label/ID, no site filter.
- **Back/parent link:** keep `scopedPath("/groups", activeSite)` so returning
  lands on the scoped list (scope round-trips via the store).
- **"View miners" link:** keep `scopedPath("/fleet/miners?group=…")`.

### Bulk actions — `features/groupManagement/components/DeviceSetActionsMenu.tsx` + `useMinerActions`

`DeviceSetActionsMenu` is shared by **two** call sites: list rows
(`GroupsTable/GroupNameCell.tsx:22`) and the detail page
(`GroupOverviewPage.tsx:224`). Scoping must be **driven by an explicit prop,
not read ambiently** from the store inside the menu — otherwise the canonical
detail page would inherit the persisted site (see the decision below).

- **New prop** `siteScope?: SiteFilter` (or `activeSite`), defaulting to
  none/`all` (= whole group). The menu scopes only when the prop is set.
- **List rows:** `GroupsPage` already has the route `activeSite`; thread it
  through `GroupNameCell` into the menu so list-row actions are
  site-scoped.
- **Detail page (`GroupOverviewPage`):** pass **no** site scope (or `all`).
  Decision (review, 2026-06-23): the canonical detail page acts on the
  **whole group**, regardless of the globally selected site. "Canonical"
  governs both display and actions; site-scoped actions live only on the
  site-scoped list. This is why the menu must not read `activeSite` itself.
- When scoped: fetch members scoped to site (RPC #2) → `memberDeviceIds` is
  the in-scope set the action runs on; pass `siteIds` into
  `fetchAllMinerSnapshots` so firmware checks match.
- **Confirm dialog:** reuse `BulkActionConfirmDialog`. When scoped, prepend a
  scope notice to every confirmable action:
  *"This action applies to **X of Y** miners in <site name>."* For actions
  not normally confirmation-gated, the site scope itself triggers the dialog
  (operator always sees X/Y before a scoped action runs). Unscoped (list at
  `all`, or detail page) = current behavior.
- **X and Y both come from the server.** X = scoped member count (RPC #2).
  Y = total (unscoped) member count. `GroupNameCell` only receives
  `deviceSetId` today, so the menu does not have the row's org-wide count —
  either pass the row `DeviceSetStats.device_count` down for Y, or have the
  scoped `ListDeviceSetMembers` response also return the unscoped total. Use
  the **same paired-like population** for X and Y so the ratio is honest.
- Edge cases: X = 0 → block with "No miners in this site"; X = Y → still
  show the dialog, phrased "all N miners (all in <site>)".

## Testing

- **Unit (client)** — `GroupsPage` list request construction per scope
  (`all` → no `site_ids`; `site(7)` → `site_ids=[7n]`; `unassigned` →
  `includeUnassigned=true`); scope change invalidates stale pages; empty
  scoped result renders the empty state; detail href is unscoped.
- **Unit (client)** — `DeviceSetActionsMenu`: scoped member fetch drives the
  action target; confirm dialog shows correct X/Y; X=0 blocks; `all` scope =
  no scope notice.
- **Unit (client) — detail page acts whole-group (CRITICAL guard for the
  review decision):** with a site selected in the store, `GroupOverviewPage`
  passes no scope, so the action target is the full member set and **no**
  X/Y scope notice appears. Prevents a future refactor from letting the
  canonical detail page silently inherit the persisted site.
- **Unit (server) — counts stay org-wide:** `device_count` for a group is
  identical between `ListDeviceSets` org-wide and GROUP+site (guards the
  semi-join-not-JOIN requirement).
- **Unit/handler (server)** — `ListDeviceSets` GROUP+site returns only
  groups with an in-scope member; empty resolution ⇒ zero groups; `all` ⇒
  unchanged; SQL-level test for the membership semi-join + site predicate.
  `ListDeviceSetMembers` site filter table test.
- **E2E** (`client/e2eTests/protoFleet`, see `proto-fleet-playwright-e2e`):
  group present in one site absent in another; row metrics identical across
  scopes (org-wide); scoped bulk action shows the X/Y dialog and only
  affects in-scope miners; detail link is unscoped and refresh-safe.

## Acceptance criteria (from #520, reconciled with decisions above)

- [ ] `/groups` remains org-wide.
- [ ] `/{site}/groups` lists only groups with ≥1 device in scope; rows show
      org-wide metrics; explicit empty state when none qualify.
- [ ] `/{site}/groups/:groupLabel` is org-wide and refresh-safe.
- [ ] `/unassigned/groups` lists groups with ≥1 unassigned device.
- [ ] Site-scoped bulk action shows an X-of-Y confirm dialog and applies
      only to in-scope miners.
- [ ] List→detail links are unscoped; detail back-link returns to the
      scoped list; navigating between Groups and other sections preserves
      scope.
- [ ] Tests cover scoped request construction and empty/no-devices-in-scope
      behavior.

## Implementation order (single PR)

1. `ListDeviceSets` GROUP+site end-to-end (proto → server semi-join →
   `listGroups` wiring → list renders scoped, empty state, unscoped detail
   href). Proves the regen + scoped-list path.
2. `ListDeviceSetMembers` site filter → scoped member fetch → snapshot
   filter.
3. Bulk-action X/Y confirm dialog + scoped execution.
4. Detail page back-link + integration tests + E2E.

## Open questions / for review

- **RBAC cross-site exposure — RESOLVED (team, 2026-06-23):** groups show
  full cross-site telemetry/counts regardless of the viewer's site access;
  no accessible-sites clamp for #520. Recorded here so `/plan-eng-review`
  ratifies the groups exception against the RBAC plan's "filter, don't
  reject" list semantics. Not blocking.
- **Detail-page action scope — RESOLVED (review, 2026-06-23):** the canonical
  detail page acts on the whole group regardless of selected site; scoping is
  an explicit prop passed only from list rows. See Bulk actions section.
- **Detail-URL alignment.** Confirm eng prefers matching the rack/building
  canonical-detail pattern (unscoped URL) over the literal #520 AC wording
  ("use `scopedPath`").
- **X/Y population consistency — folded into plan:** Y uses the same
  paired-like population as X; source Y from `device_count` or an unscoped
  total in the members response (see Bulk actions section).
- **Sort/pagination cost** — the GROUP membership semi-join over large orgs;
  verify it uses existing indexes on `device_set_membership` /
  `device.site_id`. (Stat columns are not server-sortable —
  `DeviceSetList/sortConfig.ts:19-20` — so the filter does not interact with
  a server-side stat sort.)

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | CLEAR (PLAN) | 1 issue, 0 critical gaps |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

- **UNRESOLVED:** 1 — confirm with eng that the group detail URL should match
  the rack/building canonical (unscoped) pattern over the literal #520 AC
  wording ("use `scopedPath`"). Non-blocking.
- **VERDICT:** ENG CLEARED — ready to implement. Scope accepted as-is; the one
  architecture issue (detail-page bulk-action scope) resolved to whole-group;
  two silent-bug risks (count scoping, detail-page action scope) now guarded
  with required tests.
