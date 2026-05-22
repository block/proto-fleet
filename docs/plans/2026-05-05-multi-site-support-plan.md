---
title: Multi-site support
date: 2026-05-05
status: draft
type: plan
---

## Summary

proto-fleet today assumes one install = one site. This plan adds sites as a
first-class entity so a single install can manage miners across N physical
locations, with an operator-facing hierarchy of
`site → building → zone → rack → device`. In Phase 1, `zone` is a
building-scoped label stored on the rack, not its own table. Sites are
**optional**: an org can run without any sites and the app renders in a
site-less form; sites become useful when an operator wants to organize
miners by physical location. The miner list and settings pages become
site-aware; the pairing flow stays unchanged in MVP and gains
site-segmented discovery in Phase 2. An "All Sites" mode aggregates
reads across sites; writes always target a single site explicitly when
sites exist.

Sites surface in **two locations** in the product:

- **`/sites`** is the operational overview — health and performance of
  every site and its buildings. Read-only; no config controls.
  Navigated from the primary sidenav.
- **`/settings/sites`** is the configuration surface — site and
  building CRUD, network config, address details. Navigated from the
  settings subnav.

Both surfaces share the topbar SitePicker: "All Sites" renders an
aggregated layout (one section per site on `/sites`, a flat table
ordered by site name on `/settings/sites`), and selecting a specific
site narrows to that site's view.

**Phasing is scaffold-first.** Phase 1a ships routing + the SitePicker +
all four page shells + site/building CRUD modals with placeholder
blocks where the rich metric, card, and diagnostic content will land.
Phase 1b enriches those placeholders with real metric components,
BuildingCards, diagnostics, and the building-detail page sections.
This prioritizes navigation correctness and full-stack CRUD wiring
over visual richness so the team can dogfood the data model early,
and lets purely visual work proceed in parallel without blocking
nav-correctness review.

## Goals

- Block mining-ops can manage 3+ sites from one install: name the sites,
  organize them with buildings, assign miners to sites and buildings to
  sites, filter and navigate the UI scoped to a chosen site or
  aggregated across all sites.
- Existing single-site installs upgrade with no data loss and no required
  user action — the app continues to render in a site-less form until the
  operator chooses to create sites.
- The schema treats `site` as a first-class entity so the future
  on-prem-fleet node workstream (one fleet node per site) has a natural
  attachment point. We do not commit to its specific shape now and
  add no fleet-node-specific columns or tables in this plan.

## Non-goals

- Per-site RBAC, per-site permissions for non-admin users.
- Consolidating multiple existing proto-fleet installs into one multi-site
  install.
- Per-site config split for pools, security policies, firmware, schedules,
  team membership, API keys. These stay org-scoped in MVP. Sites carry
  network config (IP ranges for discovery), location/timezone/capacity,
  optional power contract, and a list of buildings. Layout details
  (aisles, racks per aisle, default rack settings) live on the building
  entity, not the site.
- Retroactive site attribution rewrites on log/snapshot rows that
  predate multi-site. Site-aware history *is* supported (errors,
  activity, telemetry, snapshots all capture `site_id` at write
  time once Phase 1 ships), but existing rows stay site-NULL and
  surface in a "(no site)" bucket. Site filters on those surfaces
  use the row-stamped `site_id`, never the device's *current*
  site, so history doesn't shift when a device is reassigned or a
  site is renamed/deleted.
- Site-scoped discovery via on-prem fleet nodes. Out of scope for this plan;
  owned by the fleet node workstream.
- Forcing site setup at onboarding. New orgs can pair miners and operate
  without ever creating a site.
- A first-class `zone` entity, zone CRUD UI, or a `building.zones[]`
  persisted array in this phase. Zones stay lightweight and rack-owned
  until there is evidence we need stronger lifecycle management.
- UI exposure of "unassigned buildings". The backend keeps
  `building.site_id` nullable so delete-cascade and migration-time
  edge cases stay safe, but every UI-driven building creation
  requires a parent site context. There is no unassigned-buildings
  section, no inline "Assign to site" action surfaced on such rows,
  and no site-create modal "claim existing buildings" picker. A
  building landing in the unassigned bucket (because its site was
  deleted, or because a script inserted one) shows up only via
  direct DB inspection until the operator reattaches it through a
  future tool. Keeping this off the UI matches the operator mental
  model "buildings live inside a site" and avoids the modal-state
  complexity of a parent-less create flow.

## Hierarchy and portability model

This section locks the semantics that the rest of the plan assumes.

**Operator-facing hierarchy**

- `site` → top-level physical location.
- `building` → physical structure or major subdivision within a site.
- `zone` → flexible sub-building grouping such as "Room 2" or
  "East Wall".
- `rack` → physical rack/container that can belong either to a
  building or directly to a site.
- `device` → miner that can belong either to a rack or directly to a
  site.

**Storage model**

- **Site** is a first-class table.
- **Building** is a first-class table with `site_id`. The FK stays
  nullable in storage so delete/unassign flows and site-less upgrades
  remain possible, but the normal operator mental model is still that a
  building belongs to a site.
- **Zone** is **not** its own entity in Phase 1. It is a
  building-scoped string on `device_set_rack.zone`. To list all zones in
  a building, the server scans that building's racks and returns the
  distinct non-empty zone strings. We intentionally do **not** add a
  `building.zones[]` array or a rack `zone_id` FK in this phase, because
  that would create an extra mutable namespace to reconcile on every rack
  move without yet buying enough product value.

  **Forward look — promoting zone to a first-class entity.** Phase 1 is
  a deliberate one-way *soft* door: zone-as-rack-string is fine for
  filtering, grouping, and dashboards, but a `zone` table (with
  `id`, `org_id`, `building_id`, `label`, plus future attributes)
  becomes the right shape when any of these triggers hit:

  1. Zone needs to carry its own data — color, floor-map coordinates
     or polygon, capacity (`max_kw`, `max_racks`), zone-level cooling
     defaults, environmental setpoints, ACLs, tags, or notes. Phase 1
     can attach none of these.
  2. Non-rack equipment (PDUs, cameras, environmental sensors) needs
     zone membership. Today device → zone is only reachable via rack.
  3. Renames across many racks become a hot operation. Today a rename
     is an N-row `UPDATE device_set_rack`; an entity model makes it
     one row.
  4. Operators need to pre-provision empty zones (a zone exists with
     no racks yet). Phase 1 cannot represent this — a zone exists only
     because some rack uses the label.

  **Migration path when those triggers fire** is bounded and
  mechanical: backfill `INSERT INTO zone (building_id, label, org_id)
  SELECT DISTINCT building_id, zone, org_id FROM device_set_rack`,
  add `device_set_rack.zone_id BIGINT REFERENCES zone(id)`, dual-write
  for one release, drop the denorm string. The wire-level cost is the
  same shape as the `(building_id, zone)` composite-key work in Phase
  1 (a breaking proto rename on filter fields), so the schema decision
  is not a wire-level one-way door either.

  **Filter contract today.** Because zones are unique only within a
  building, the wire-level zone filter on miner / rack lists uses
  `repeated ZoneKey { int64 building_id; string zone; }` rather than
  `repeated string`. Two racks with zone "Room 2" in different
  buildings stay distinguishable. `building_id = 0` is a
  transitional sentinel inside `ZoneKey` meaning "match this zone
  label across all buildings" — it covers today's
  no-buildings-in-UI dropdown (which sends only wildcards). Once
  the buildings UI ships and zones can only exist on racks inside
  a building, well-formed clients have no reason to emit wildcards
  anymore. The sentinel survives because removing it would be a
  breaking proto change for a marginal cleanup win — harmless dead
  surface area, not a permanent feature. When zone eventually
  becomes an entity, this filter migrates to `repeated int64
  zone_ids` — same migration cost whether we picked composite-key
  now or not.

  See `docs/plans/2026-05-14-229-miner-zone-building-filter-plan.md`
  for the full Phase 1 filter plan.
- **Rack** stores `site_id` (nullable), `building_id` (nullable), and
  `zone` (string). A rack may belong directly to a site without a
  building. When `building_id` is set, `rack.site_id` must match the
  parent building's `site_id`.
- **Device** stores `site_id` directly and may belong to a rack via the
  existing rack membership path. A device may belong directly to a site
  without a rack. Device does **not** store a direct `building_id`;
  building context is derived from the rack when present.
- **Groups** remain many-to-many and cross-site by design.

**Portability and reassignment**

- Every reassignment that can affect descendants uses a warn-first
  confirmation dialog and commits in one transaction.
- Reassigning a **building** to a different site updates
  `building.site_id` and cascades the new site to every descendant rack
  and device under that building. Racks stay attached to the building;
  zone labels are preserved because they are scoped to the building, not
  to the site.
- Reassigning a **rack** to a different building, or unassigning it from
  a building, updates the rack's `building_id` and/or `site_id` and
  cascades site changes to every descendant device. A rack may be moved
  directly under a site with no building, or into / out of a building
  under that same site. When a rack crosses a building boundary, its
  `zone` is cleared as part of the same transaction, because zone names
  are building-scoped and should not be silently carried into a
  different building's namespace.
- Reassigning a **device** directly to a site is allowed only when it
  does not contradict its current rack context. If the device is in a
  rack, the target `device.site_id` must match that rack's `site_id`.

## User journeys

These are the surfaces in the product that touch the concept of "site". Each
journey calls out the open design questions it raises.

### J1. Onboarding a new org

Onboarding does **not** prompt for site configuration. New orgs flow
through today's existing onboarding (welcome → general settings →
security → miner pairing → completion) unchanged. Pairing assigns
miners to no site (`site_id IS NULL`); they sit in an "Unassigned"
bucket until an operator creates sites later.

If and when the operator wants to organize by site, they navigate to
`/settings/sites`, create sites, and use the bulk-assign action from the
miner list.

### J2. Page-header app switcher (site picker)

The topbar SitePicker is **Phase 1a** scope (was previously Phase 2).
It drives both `/sites` and `/settings/sites` rendering modes from
day one, so the scaffold ships with the picker working end-to-end
rather than relying on URL params or a temporary single-site mode.

When the org has at least one site, every page sits behind a topbar
control that selects a specific site, "All Sites" (aggregate across all
the user's sites), or "Unassigned" (miners with no site). This replaces
the placeholder `LocationSelector` in `PageHeader.tsx`.

When the org has **zero sites**, the topbar SitePicker is hidden — the
app renders in site-less form. The miner list shows no site column.
`/settings/sites` shows an empty state with a "Create site" CTA. The
moment the operator creates their first site, the SitePicker appears,
defaulting to that newly-created site (per the default-after-login
rule below).

**Feature-flag gating.** Both the primary sidenav entry for `/sites`
and the settings subnav entry for `/settings/sites` are wrapped in a
Vite-time feature flag (env-driven) so the buttons stay hidden in
production builds until the Phase 1b enrichment ships. The routes
themselves are **not** flag-protected — an operator who knows the
URL can navigate directly, which keeps dogfood + QA paths open
without adding route-guard logic. Removing the flag is a one-line
config change once we're ready to expose the feature to operators.

**Global picker mount in Phase 1a, transitional consumption.**
The SitePicker replaces today's `LocationSelector` in `PageHeader`
globally from Phase 1a — it is visible on every protoFleet route
including `/miners`, `/racks`, and the dashboards. The three new
routes (`/sites`, `/settings/sites`, `/buildings/:id`) consume the
selection immediately. Existing routes do **not** yet read the
active site in 1a; their data queries continue to render org-wide
results regardless of the picker's state. This is a documented
transitional UX captured in the issue and PR description: the
picker looks the same everywhere, but only the new pages react to
it. Phase 1b (#202) closes the gap for miner-list and rack-list;
history-bearing pages join in Phase 2. We chose the global mount
over route-scoping because the route-scoped variant adds matching
logic to `PageHeader`, splits state behavior across the app, and
breaks selection continuity when an operator navigates from
`/buildings/:id` to `/racks?building_id=...` — exactly the moment
they'd want context to follow them.

- **Specific site selected** → all reads scoped to that site. All writes
  target that site without further prompting.
- **"All Sites" selected** → reads aggregate across every site the user
  can see. Writes that target a site (create rack at building, etc.)
  require an explicit site picker inside the action's UI.
- **"Unassigned" selected** → reads scoped to miners/racks/buildings
  with no site assignment. Useful for triage post-pairing or
  post-upgrade. This option is the fastest path to surfacing
  unassigned miners and racks for bulk handling — included in MVP
  rather than left as a follow-up filter.
- **Bulk operations** (firmware update, restart) across miners from
  multiple sites are allowed when "All Sites" is active — the operation
  is per-miner, so cross-site batching is fine.

**Persistence.** Active site selection is stored client-side in
localStorage, keyed by username, mirroring the saved-views pattern at
`client/src/shared/hooks/useLocalStorage.ts:3-45`. Server validates that
any `site_id` sent with a request belongs to the user's org — that's
the actual security boundary; "active site" itself is pure UX
preference.

**Default after first login.** "All Sites" if the user has access to
more than one site; the single accessible site if exactly one;
SitePicker hidden if none.

### J3. Site config (Settings → Sites)

`/settings/sites` is the configuration surface for sites and
buildings. Health and performance metrics belong on `/sites` (see J8);
this page is intentionally config-only.

**Empty state (org has zero sites).** Page renders a CTA: "Create your
first site to organize miners by location." There is no unassigned-
buildings section — see the non-goal above; building creation always
happens inside a site context.

**Specific site selected in topbar.** Page shows the single-site
layout described below.

**"All Sites" selected in topbar.** Page shows a flat table of every
site, with one row per site, plus a header and "Add a site" CTA.
Rows are ordered by `site.name` ascending; the table is not
user-sortable in Phase 1. There is no per-site stacked section
layout here — for the operational overview see `/sites` (J8).

**All Sites layout.**

- Page header
  - Headline: "Sites"
  - Subheadline: "Manage your sites, buildings, and rack
    infrastructure."
  - Right-aligned "Add a site" button → `SiteDetailsModal` in create
    mode.
- Table
  - Each column renders a two-line stack: top line `text-300-emphasis`,
    bottom line `text-300`.
  - **Site** column — `site.label` over `"${site.city}, ${site.state}"`.
  - **Infrastructure** column — `"${n} buildings"` over
    `"${n} miners"`.
  - **Power / Efficiency** column —
    `"${site.power} / ${site.power_capacity} MW"` over
    `"${site.efficiency}"`. Derived values; see Phase 1b enrichment.
  - Row click → navigates to single-site view of `/settings/sites`
    (i.e. selects the site in the topbar SitePicker).

**Single-site layout.**

- Button row
  - Left-aligned "< All sites" button — same effect as picking
    "All Sites" in the topbar SitePicker.
  - Right-aligned "Manage site" button → `ManageSiteModal`
    (FullScreenTwoPane).
- Header
  - Headline: `site.label`.
  - Subheadline: `site.address` (full address — depends on the
    site address BE follow-up; until then renders city/state).
- **Details table** (config metadata, not metrics)
  - Heading "Details" above the table.
  - Two columns, no row headers, justified between so each column
    aligns to the table edges.
  - Rows: Power (used / capacity MW + %), PUE, Timezone, Gateway,
    Notes. PUE / Gateway / Notes depend on BE follow-ups; FE omits
    rows whose underlying field is not yet present.
- **Buildings table**
  - Heading "Buildings" with right-aligned "Add building" CTA →
    `BuildingDetailsModal` in create mode (scoped to the current
    site).
  - Three columns with row headers: name (`building.label`), type
    (`building.type` — depends on BE follow-up; row hides until
    present), power (`"${used} / ${capacity} MW (${pct}%)"`).
  - Row click → `/buildings/${building.id}` (J9).

**Modals.** Site and building CRUD mirror today's rack-creation flow.
Three distinct modals:

- **`SiteDetailsModal`** — site detail entry form. Fields: name,
  address, city, state, zip, country, power capacity (MW), timezone,
  notes. Component states differ between create and edit:
  - *Create mode*: primary action is "Continue" → opens
    `ManageSiteModal` with the entered details in memory; the site
    row is not persisted until "Save" inside `ManageSiteModal`.
  - *Edit mode*: primary actions are "Delete" + "Save" (Save calls
    `UpdateSite` directly).
  - Address / zip / country / notes inputs depend on BE follow-ups
    and stay hidden until those fields land.
- **`ManageSiteModal`** — FullScreenTwoPane membership manager.
  Header: left-aligned "Manage Site" with X close, right-aligned
  "Edit details" (→ `SiteDetailsModal` edit mode, or back-to-details
  in create flow) + "Save" (commits via `CreateSite` or
  `UpdateSite`).
  - Left pane: network text input for IP range / CIDR, plus the
    buildings table (3 cols: name / type / power) with "Add
    building" CTA → `BuildingDetailsModal`.
  - Right pane: building-grid preview. Top-left `site.label` /
    `city, state`. Top-right `power_capacity / n buildings`. Grid
    of building boxes (label only) arranged horizontally, wrapping
    when out of space.
- **`BuildingDetailsModal`** — create or edit a single building.
  Heading: `building.label`. Inputs (PR 3 scope): name, type
  (greyed-out stub until the `building_type` enum follow-up
  lands), power capacity (MW; converted to `power_kw` on
  submit), overhead (kW). Buttons: Save in both modes, Delete
  only in edit. Cooling type / IP range remain hidden until
  their BE follow-ups land. Default rack layout inputs
  (`default_rack_rows` / `default_rack_columns` /
  `default_rack_order_index`) are intentionally absent — the
  proto fields stay optional + zero-defaulted server-side until
  a downstream feature needs them. Aisles + racks_per_aisle live
  on `ManageBuildingModal` because they only matter for the grid
  view.
- **`ManageBuildingModal`** — manage rack membership inside a
  building. Header has an "Edit building" button that stacks
  `BuildingDetailsModal` on top (mirror of `ManageSiteModal` →
  `SiteDetailsModal`). Delete is owned by the details modal,
  not the manage modal.
  - Left pane: aisles + racks_per_aisle inputs (drive grid
    dimensions; persist via `UpdateBuilding`); "Assign racks"
    button → `SearchRacksModal`; byName / manual mode toggle;
    list of currently-assigned racks.
  - Right pane: `aisles × racks_per_aisle` grid. Empty cells
    render a `+` block, assigned cells render the rack label.
    byName mode auto-fills cells alphabetically; manual mode
    lets the operator click a cell + rack to position.
  - Save persists touched racks via repeated
    `BuildingService.AssignRackToBuilding` calls — one per rack
    whose membership or grid cell changed. SaveRack stays out of
    this path; grid placement is its own write contract.

**Site create + miner assignment.** The plan previously claimed the
site row insert and `device.site_id` updates happen in one
transaction. The generated `CreateSiteRequest` does not carry
`device_ids`; the realistic Phase 1 implementation is two RPCs from
the client (`CreateSite` then `ReassignDevicesToSite`). Treat that as
the canonical flow; the modal surfaces a single "Save" and orchestrates
the two calls, rolling back the create on a reassign failure is not
worth the complexity here — instead the UI surfaces a clear
"site created; miner assignment failed: ..." error so the operator
retries the assignment from the miner list. Folding device_ids into
`CreateSite` is tracked as a follow-up, not a blocker.

**Cross-site building moves** are not surfaced as a UI action in
Phase 1. `SiteService.AssignBuildingToSite` exists at the API layer
but `/settings/sites` does not expose a "move to another site"
control in this phase. Operators who need to move a building edit
the building directly, change its parent site, and accept the
cascade dialog. Open question whether a dedicated "Move building"
flow is worth its own UI in Phase 1b.

**Site and building deletion — cascade-unassign with warn-first
dialog.** Deletion is never blocked by attached entities. The UI
reads attachment counts from the list response and presents a
confirmation dialog before the destructive call:

- *Site delete dialog:* "Deleting site 'X' will unassign **N
  miners**, **M racks**, and **P buildings**. They will be removed
  from this site. Continue?" Buttons: [Cancel] [Delete site].
- *Building delete dialog:* "Deleting building 'Y' will unassign
  **N racks** from this building and clear their zone labels. They
  will remain directly assigned to '${site.name}'. Continue?"
  Buttons: [Cancel] [Delete building].

If counts are zero, the dialog still confirms but skips the
unassignment language ("Are you sure you want to delete site 'X'?").

On confirm, the server runs in one transaction:

1. Soft-deletes the row (sets `deleted_at`).
2. Sets `site_id = NULL` on every device pointing at the deleted
   site, every rack pointing at the deleted site, and every building
   pointing at the deleted site. (Building delete: sets
   `building_id = NULL` and clears `zone` on every rack pointing at
   the deleted building, while leaving those racks directly assigned
   to their existing `site_id`.)
3. Writes an activity-log row capturing the deletion + the
   unassignment counts so audits can reconstruct the cascade.

Open questions:

- Whether `ManageSiteModal`'s right-pane building grid should be
  interactive (drag-to-reorder, click-to-edit) in Phase 1b or stay
  display-only.
- Whether building delete should call out indirect device impact
  (devices remain site-assigned but lose rack/building linkage) in
  the dialog body. Working answer: yes, when `device_count > 0`
  derived from the building's racks.

### J4. Add miners (Miner List → Add Miners)

Pairing flow is **unchanged from today** in MVP. No site picker, no
target-site modal step. Discovery uses today's request-supplied IP
ranges (or mDNS link-local). Paired miners land with `site_id IS NULL`
and the operator assigns them via the bulk-assign-to-site action on
the miner list (see J6).

**Future (Phase 2):** discovery results are segmented by site network
config — each discovered miner is grouped by which site's IP range
caught it. Operator can drag-and-drop discovered miners between site
buckets before clicking Pair, and miners pair directly into the
operator-confirmed site. This is the eventual UX; MVP ships the
flat unsegmented flow first to keep Phase 1 small.

**Site→miner mapping rule.** A miner's site is inferred from the
site whose configured IP range caught it during discovery, not from
the fleet node or transport that relayed it. This rule holds today
(direct cloud scan) and in the future fleet node architecture (fleet node
scans its local network and relays the results, but the site bucket
is still chosen from the site's network config matching the miner's
IP). Operators can override the inferred site at pair time
(Phase 2 DnD) or after pairing (J6 bulk assign).

### J5. Upgrading an existing install

Existing orgs upgrade with **no auto-created site** and **no required
user action**. The migration:

- Adds new tables (`site`, `building`) but populates no rows.
- Adds nullable `site_id` to `device`, leaving every existing miner
  with `site_id = NULL` (Unassigned).
- Adds nullable `site_id` to `device_set_rack`, leaving every existing
  rack with `site_id = NULL` (Unassigned) until the operator assigns it
  directly to a site or through a building.
- Adds nullable `building_id` to `device_set_rack`. Existing racks
  keep `building_id = NULL` and continue to surface their `zone`
  string in the UI. Buildings are not auto-promoted from zones —
  zone continues to coexist with building as the flexible
  sub-building label, and operators opt into buildings explicitly
  when they want per-building config (capacity, layout defaults,
  site assignment).
- Leaves `device_set_rack.zone` column in place as the Phase 1 zone
  implementation; there is no planned drop in this plan.
- Blocks the upgrade deployment if any pairing or discovery job is in
  flight.

No migration banner ships with this rollout. The fleet doesn't yet
have a user base large enough to warrant a one-time educational
prompt; an upgraded operator discovers `/settings/sites` from the
settings nav. A coach-mark / onboarding nudge can be revisited later
if real-world usage shows operators missing the feature.

After upgrade, an existing operator's org is in site-less form:
miner list shows no site column, `/settings/sites` is empty.
Creating sites, creating buildings, and assigning miners is
entirely opt-in.

### J6. Assigning miners / racks / buildings to sites

Once at least one site exists, three assignment flows surface:

**Miners (bulk).** From the miner list:

1. Filter or scroll to the target miners; multi-select rows.
2. Bulk action menu → "Assign to site" opens a modal with a target
   site picker.
3. Server runs `ReassignDevicesToSite` as an all-or-nothing
   transaction:
   - Validates every selected device belongs to the user's org.
   - For every device currently in a rack whose rack `site_id` is
     assigned to a different site, rejects the entire batch with
     `reason = "device_in_rack_at_other_site"` and per-device error
     details. The operator unracks the offenders or assigns the
     rack to the same site, then retries.
   - On success, updates `device.site_id` for the batch and writes
     one activity-log row capturing user / source-site (or
     "unassigned") / target-site / device-ids JSON.
4. The bulk action is also the unassign action — the modal includes
   "(Unassigned)" as a pickable target.

**Buildings.** Cross-site building moves are not exposed in the UI
in Phase 1 — see the corresponding J3 note. `AssignBuildingToSite`
remains a first-class RPC and is callable from scripts or future
admin tooling; when a UI surface for the move ships, the
confirmation dialog displays the descendant rack/device counts that
will move with the building before the server runs the cascade.

**Racks.** Racks may belong directly to a site or to a building within
a site. Reassigning a rack from one building to another, from direct
site placement into a building, from a building back to direct site
placement, or from one site to another goes through the existing rack
edit modal. In Phase 1, that flow becomes a transactional cascade:
moving the rack updates `site_id` and/or `building_id`, updates every
device in the rack to the rack's new `site_id` (or `NULL` if the rack
is being fully unassigned), and clears `zone` when the rack crosses a
building boundary. The confirmation dialog must call out both the
device site reassignment and the zone clearing so the operator knows
the downstream impact before confirming.

### J7. Foreman import — sitemap → site / building / rack

Today's Foreman import (`server/internal/domain/foremanimport/`)
flattens a Foreman sitemap (a tree of `SiteMapGroup` rows with
parent pointers, with `SiteMapRack` rows attached to leaf groups)
into a flat list of fleet groups + racks. With multi-site landing,
the importer needs to map the tree onto `site → building → rack`
instead.

**Mapping rule (working assumption).**

- Each **root** Foreman group (group with `parent_id IS NULL`)
  becomes a fleet **site**.
- Every **non-root** Foreman group — at any depth below the root —
  becomes a fleet **building** under the corresponding site.
  Multiple parent levels collapse to one building per Foreman
  group; intermediate groups don't get their own intermediate
  entity. Building name = Foreman group name.
- Each Foreman rack becomes a fleet rack under the building
  matching its parent group. If a Foreman rack sits directly under a
  root group, it becomes a rack directly under that site with no
  building.
- A miner's `site_id` is set to its rack's `site_id` at import time,
  satisfying the cross-collection invariant.
- Pre-existing fleet groups created from Foreman keep working;
  no retroactive promotion to sites/buildings.

**Open questions.**

- Whether to expose the depth-collapsing rule to the operator
  before import runs, or apply silently with a post-import summary
  ("imported 3 sites, 12 buildings, 187 racks, 9402 miners").
- Idempotency: re-importing from Foreman after a site is renamed
  in fleet — does the importer rename back to Foreman's name, skip
  the rename, or warn? Working assumption: skip the rename, log a
  warning. Operators rename in fleet for a reason.
- How to handle Foreman rack-only entries with no parent group
  (today they go to a default group). Working assumption: those
  racks and miners land in the Unassigned bucket and the operator
  uses J6.

**Phasing.** Foreman importer changes ship in **Phase 1** alongside
the site/building schema, not deferred — the importer is a
production write path and would otherwise create stale flat groups
that operators then have to clean up by hand.

### J8. Sites overview (`/sites`)

`/sites` is the operational dashboard for site health and performance.
Read-only — no config controls live here. The page is reached from the
primary sidenav (button feature-flagged in Phase 1a).

The "All Sites" vs "Single site" mode is driven by the topbar
SitePicker, just like `/settings/sites`. The two modes share their
per-site rendering; the only difference is whether one section or many
are stacked.

**Per-site section layout.**

- **Header**: metric row of five components rendered horizontally.
  - Location — `"${city}, ${state}"`.
  - Hashrate — `"${value} EH/s"` (derived telemetry rollup).
  - Power — `"${used} / ${capacity} MW"`.
  - Efficiency — `"${value} J/TH"`.
  - Buildings — `"${n}"`.
- **Building cards** — one card per building in the site, arranged in
  a responsive grid.
  - Phase 1a ships an FPO card: grey box, building label, "n racks
    / m miners". Whole card is a link to `/buildings/${building.id}`.
  - Phase 1b replaces the FPO with the real `BuildingCard`
    component (visual + per-building metrics).

**All Sites mode.** Sections stack vertically, one per site, with a
divider between. No table — the rich metric + card layout per site
is the point of this page.

**Empty state.** When the org has zero sites, the page renders the
same CTA used by `/settings/sites` empty state ("Create your first
site...") so the operator has a starting point from either surface.
The CTA opens `SiteDetailsModal` in create mode.

**Open questions.**

- Whether `/sites` should accept a deep-link `/sites/${site.id}` for
  bookmarking a specific site, separate from the SitePicker state.
  Working answer: yes, but the URL drives the SitePicker on mount
  rather than diverging from it.
- Whether metric components are shared with `/buildings/:id` and the
  rack overview page or duplicated. Working answer: shared — they
  are the same shape.

### J9. Building overview (`/buildings/:id`)

`/buildings/:id` is the per-building operational page. Mirrors the
existing rack overview page in structure, scoped one level up.

- **Header**
  - Headline: `building.label`.
  - Right-aligned buttons:
    - "View racks" → racks list page filtered to this building.
    - "View miners" → miners list page filtered to this building.
    - "Edit building" → `ManageBuildingModal` (sibling of
      `ManageSiteModal`; FullScreenTwoPane; not yet detailed —
      Phase 1b deliverable).
- **Metrics row**
  - Hashrate, Power (used / capacity MW + %), Efficiency,
    Miners online (`"${online} / ${total}"`).
- **Diagnostics section** — same shape as the rack overview's
  diagnostics, with the grid one level up: a rack grid (one cell per
  rack in the building) instead of a miner grid. The healthy /
  sleeping / needs-attention / offline stats represent **racks**,
  not miners. The derivation rule for rack-level health is an open
  question (working answer: a rack is "needs attention" if any
  member miner is needs-attention; "offline" if every member miner
  is offline; "sleeping" if every member is sleeping; "healthy"
  otherwise). The "component health" subsection is identical to the
  rack overview's.
- **Performance section** — same shape as the rack overview's,
  scoped to this building.

Phase 1a ships header + placeholder blocks for metrics, diagnostics,
and performance. Phase 1b lands the metric components, the rack grid
+ rack-state derivation, and the performance charts.

**Open questions.**

- Rack-state derivation rule (above) needs validation against real
  miner-state data before lock-in.
- Whether a building with zero racks renders the diagnostics section
  at all, or replaces it with an empty-state message.

## Backend updates

High-level only — the technical plan that follows this one will spell out
each migration, query, and handler.

### Schema and migrations

New entities and relationships introduced:

- **`site`** — first-class table, org-scoped. Holds:
  - `name` (unique within org)
  - `description` (optional)
  - `location_city`, `location_state`
  - `timezone`
  - `power_capacity_mw` (nullable; optional)
  - `network_config` (text; newline-separated CIDRs/IPs for discovery
    scan; optional) — see "Network config validation" below.
  - **Power contract fields — DEFERRED.** The eventual shape (ISO /
    balancing-authority / rate-type enums, utility operating company,
    `rate_cents_per_kwh`, `demand_charge_cents_per_kwh`,
    `transmission_structure`, `power_factor`, contract start/end
    dates) is captured in the design history but did NOT ship in
    issue #195. They land in a follow-up migration once the modeling
    is locked in; until then the column set is just location +
    timezone + capacity + network_config.
  - Standard timestamp columns + `deleted_at` for soft delete.

  Cooling mode is **not** a site-level field. Miners already carry
  cooling-mode settings; site-level cooling is redundant.

  **ISO note.** Independent System Operator (ISO) / Regional
  Transmission Organization (RTO) is the entity that runs the
  wholesale power market and dispatches the grid in a region. The 7
  US ISOs/RTOs cover roughly 60% of US load; the remainder
  (Southeast, much of the West) is "non-ISO" — operated by
  vertically integrated utilities under bilateral contracts and
  coordinated through balancing authorities (TVA, BPA, etc.).
  Bitcoin mining sites are sited heavily in both kinds of regions,
  so the form must handle both.

  **Utility list note.** Utility is modeled as a free-text /
  long-list `utility_operating_company` rather than a hard-bound
  enum. Real utility operating companies span multiple ISOs (Duke
  Indiana = MISO; Duke Carolinas = non-ISO; Entergy = MISO; AEP =
  PJM and SPP), so any ISO→utility hard filter would be wrong. The
  UI shows a suggested utility list filtered by chosen ISO with a
  "show all" escape and a free-text fallback. Mismatches surface as
  a soft warning, not a block. Initial suggestion list is in the
  appendix.

- **`building`** — first-class entity for per-building config
  (capacity, layout defaults, site assignment). Coexists with the
  building-scoped `device_set_rack.zone` string; operators opt into
  buildings rather than having zones auto-promoted on upgrade.
  Holds:
  - `site_id` (**nullable** FK; a building may exist without an
    assigned site — placeholder buildings created ahead of site
    assignment, or buildings whose site has been deleted)
  - `name` (unique within site when site is set; unique within org
    when unassigned)
  - `power_kw` (capacity)
  - `overhead_kw` (non-miner load: cooling, lighting, etc.)
  - `aisles` (count)
  - `physical_rack_count` (physical racks present in the building,
    not the count of software-configured rack rows)
  - `racks_per_aisle`
  - `default_rack_rows int`, `default_rack_columns int` —
    mirrors today's `device_set_rack.rows` and
    `device_set_rack.columns`. Rack "type" today is purely a
    derived API concept (`ListRackTypes` does
    `GROUP BY rows, columns`); no `rack_type` table exists to FK
    to. Storing the two integers directly avoids inventing one.
  - `default_rack_order_index` — points at the existing
    `RackOrderIndex` enum (`BOTTOM_LEFT`, `TOP_LEFT`,
    `BOTTOM_RIGHT`, `TOP_RIGHT` — see
    `proto/device_set/v1/device_set.proto:105`).

  Cooling mode is **not** a building-level field — miner-level
  cooling settings already cover this.

  The default-rack fields describe defaults applied when adding a
  new rack to the building; pre-existing racks may not match these
  defaults, and that's allowed.

- **`device.site_id`** — **nullable** FK. Existing devices migrate
  with `site_id = NULL`. New pairings default to `NULL`. Operator
  assigns via bulk action.

- **`device_set_rack.site_id`** — **nullable** FK. Existing racks
  migrate with `site_id = NULL`. A rack may be assigned directly to a
  site even when `building_id` is NULL. When `building_id` is set,
  `rack.site_id` must match the parent building's `site_id`.

- **`device_set_rack.building_id`** — **nullable** FK. No automatic
  backfill from `zone` strings; operators opt into buildings
  explicitly via the rack edit modal or bulk assign. A rack may have a
  building only when it is also assigned to that building's site.

- **`device_set_rack.zone`** — retained as the Phase 1 zone model.
  Free-form string, interpreted within the scope of a building.
  Crossing a building boundary clears it so the rack can be assigned
  a new zone explicitly in its new building.

- **`device_set_rack.aisle_index`** + **`device_set_rack.position_in_aisle`**
  — both nullable `INT`s, added in PR 3 to back the
  `ManageBuildingModal` grid. When `building_id` is set, the pair
  positions the rack at `(aisle_index, position_in_aisle)` inside
  the building's layout. SQL-level CHECK constraints: paired
  (both NULL or both set), non-negative when set, and
  position-requires-building (cannot be set when `building_id IS
  NULL`). A partial unique index on
  `(building_id, aisle_index, position_in_aisle)` ensures a cell
  holds at most one rack. Upper bounds (`< building.aisles`,
  `< building.racks_per_aisle`) are validated application-side
  because they depend on the parent building row. Cleared on any
  `building_id` transition through `UpdateRackPlacement` so
  positions never outlive their building. Writes flow through a
  dedicated `BuildingService.AssignRackToBuilding` RPC; reads for
  the grid flow through `BuildingService.ListBuildingRacks`.

- **History-bearing tables get a nullable `site_id` column** so
  per-site filtering on Phase 2 dashboards uses the row-stamped
  site, not the device's *current* site (which would rewrite
  history on rename/reassign/delete). The column is added to:
  `activity_log`, `miner_state_snapshots`,
  `command_on_device_log`, the errors table, telemetry, and any
  other history table that joins to `device`. Writers populate
  from `device.site_id` at write time. Pre-multi-site rows stay
  NULL and surface in a "(no site)" bucket on the relevant pages.
  No retroactive backfill of historical rows.

Active-site selection is **not** stored in the database — it lives in
client localStorage keyed by username (see J2).

The reserved `connection_kind` enum from the source design doc is
**not** included. The fleet node workstream will define whatever
discriminator and fleet-node-side schema it needs when it ships.

Relationships after migration:

```
site 1 ──< building 1 ──< rack(zone: string) 1 ──< membership >── device
   └────────────────────< rack
   └──────────────────────────────────────────────────────────────< device

         (a building may have no site, a rack may have no building,
          a rack may belong directly to a site, and a device may
          belong directly to a site)
```

Groups remain org-scoped (no `site_id`); they can span sites.

**Cross-collection consistency rule.** Site assignment is explicit on
devices and racks, but those FKs must agree with any parent context.
Stated as write-time checks:

- Pairing / bulk-assign: if a device is in a rack whose `site_id` is
  set, the device's target site must match that rack site.
- Building site-assignment: the move updates the building's
  `site_id` and rewrites every descendant rack and device `site_id`
  to the new site (or `NULL` when moving the building to Unassigned)
  in the same transaction.
- **Rack edit / move**: moving a rack to a different building
  rewrites the rack's `site_id` and every descendant device's
  `site_id` to the target site (or `NULL` when the rack becomes fully
  unassigned) in the same transaction, and clears `zone` if the rack
  crossed a building boundary. This closes the loophole where rack
  moves would otherwise let devices drift to the wrong site because
  `device.site_id` is a direct FK independent of the rack context.
- **Add devices to rack** (`AddDevicesToDeviceSet` with
  `device_set_type = 'rack'`): when the target rack has
  `site_id IS NOT NULL`, cascade the rack's `site_id` onto every
  added device whose current `site_id` differs, in the same
  transaction as the membership insert. The rack wins because the
  operator explicitly picked it; the prior device `site_id` is
  captured in the activity-log row so the implicit reassignment is
  auditable. The client shows a confirmation dialog summarizing the
  device-site reassignment counts before the call fires. Targets of
  `device_set_type = 'group'` are exempt — groups are org-scoped and
  may span sites by design.
- A rack may be directly assigned to a site with `building_id = NULL`.
  A device may be directly assigned to a site without any rack.
- Otherwise (any of the FKs are NULL): no constraint.

**Network config validation.** The site `network_config` field is
stored as text but canonicalized + validated server-side at every
write:

- Each non-blank line must parse as a valid CIDR or IP address;
  malformed entries reject the save with a per-line error.
- Subnet mask cap: reject any CIDR broader than `/20` to prevent
  inadvertent ranges that would scan tens of thousands of hosts.
  (Operators with genuinely wider footprints can submit multiple
  `/20`-or-narrower entries.)
- Within-site overlap: reject duplicates and overlapping subnets
  in the same site at save time.
- Cross-site overlap (same org): warn at save time but do not
  block — operators legitimately have label-overlap during DR or
  migration. Discovery match precedence when one IP falls in
  multiple sites' ranges: most-specific subnet wins; ties broken
  by oldest `site_id` (deterministic and stable across restarts).
- Server returns the canonicalized form on save (e.g.
  `10.0.0.0/8`, not `10.0.0.0 / 8` or `10/8`); UI replaces the
  textarea contents with the returned canonical text so the
  operator sees what's actually stored.

### Deferred fields — backend follow-ups

Phase 1a frontend work does not block on these. The FE omits the
corresponding inputs and display rows until each lands; once a
column ships, the FE adds the field in a small follow-up PR. Each
bullet here gets its own GitHub issue so the cross-stack work is
tracked, not lost.

**`site` table follow-ups.**

- `address_line1 text`, `address_line2 text`, `postal_code text`,
  `country text` — augment today's `location_city` /
  `location_state`. Surface on `SiteDetailsModal` form and on the
  single-site header subline. Country defaults to "US" on
  pre-existing rows during migration.
- `notes text` — free-form operator notes. Surface on the
  single-site Details table and on `SiteDetailsModal`.
- Gateway display on the single-site Details table — almost
  certainly derived from the fleet-node workstream rather than a
  column on `site`. Confirm shape when the fleet-node workstream
  picks up a stable runtime identity for the gateway. Until then,
  the Details row is omitted.

**`building` table follow-ups.**

- `building_type` enum: `WAREHOUSE`, `CONTAINER`, `DATA_CENTER`,
  `MODULAR`, `OUTDOOR`, `RESIDENTIAL`. Surface on the buildings
  table column "type", on `BuildingDetailsModal`, and in saved
  filters once the miner list gains building-type filtering.
  Default for pre-existing rows: `WAREHOUSE` (most common operator
  case) — confirm at migration time, otherwise leave nullable and
  let the UI render "—".
- `network_config text` — per-building CIDR/IP range mirroring the
  site-level shape (same canonicalization + validation rules).
  Required for the eventual building-scoped discovery refinement,
  but BE work can ship after `BuildingDetailsModal` lands its
  placeholder input.
- `cooling_type` — likely derivable from member miners'
  cooling-mode settings. Hold the schema column until the FE
  surface actually needs it; until then the modal hides the
  cooling input.

**Derived / computed metrics** (no schema changes).

- PUE, hashrate, efficiency, used-MW, miners-online — read from
  existing telemetry rollups + the new `MinerStateSnapshot`
  `site_id` field (Phase 1, issue #197). FE work to surface these
  on `/sites`, `/buildings/:id`, and the `/settings/sites` All
  Sites table is tracked separately as the Phase 1b enrichment
  ticket; nothing on the BE blocks it once #197 lands.

Power-contract columns remain deferred per the existing schema
note above. They are listed in the design appendix but not
scheduled for Phase 1 or Phase 1b.

### Domain logic and APIs

New domain packages:

- `server/internal/domain/sites/` — site CRUD, list, reassign-devices-
  to-site, network-config get/set. (Power-contract get/set is
  deferred along with the columns themselves.) No
  set-active-site RPC (active site is client-side). `ListSites`
  returns `device_count`, `rack_count`, and `building_count` per site
  so the delete-confirm dialog has its impact numbers without a
  separate RPC. `AssignBuildingToSite` lives here because it owns the
  site-level cascade: building move + descendant rack/device site
  rewrite in one transaction. `DeleteSite` runs the soft-delete +
  cascade-unassign in one transaction and writes an activity-log row
  that includes the unassignment counts.
- `server/internal/domain/buildings/` — building CRUD, list
  (filterable by site or by "unassigned"), layout settings.
  `ListBuildings` returns `rack_count` per
  building for the delete-confirm dialog. `DeleteBuilding` runs the
  soft-delete + cascade-unassign of racks in one transaction and
  clears their `zone` strings because the building-scoped namespace
  is gone, but leaves those racks directly assigned to their existing
  `site_id`.

Updated domain packages:

- `pairing/` — unchanged in MVP. Pair RPC does not accept a `site_id`.
  Discovery uses today's request-supplied IP ranges (and mDNS
  link-local). Future Phase 2 work introduces site-segmented discovery.
- `device/` — list-devices query gains **two** filter fields rather
  than overloading one with a state sentinel:
  - `repeated int64 site_ids` — empty means "no site filter",
    populated means "match any of these sites". Same shape as the
    existing `group_ids` / `rack_ids` filters.
  - `bool include_unassigned` — separate boolean controlling
    whether `site_id IS NULL` rows are included. Allowed
    combinations: only `site_ids` (specific sites), only
    `include_unassigned` (Unassigned bucket alone), both (specific
    sites *plus* Unassigned), neither (no site filter).

  Splitting ID list and state sentinel keeps the filter clean
  through proto generation, URL params, and saved-view JSON; a
  single field carrying both numeric IDs and a magic
  `"unassigned"` string would be fragile across all three
  surfaces. The `MinerStateSnapshot` proto gains `site_id`
  (nullable) and `site_label`; every writer is updated.
- `activity/` — every site CRUD, building CRUD, and device-reassign
  writes one log row capturing user, source/target site, device-ids
  JSON. Activity rows themselves also gain a row-stamped `site_id`
  (the activity's primary device's site at write time, when
  applicable) so the activity feed can be filtered per-site.
- `rack/` — rack edit/move flow is updated so site/building changes
  rewrite `rack.site_id`, cascade device site rewrites, and clear
  `zone` as described above.
- `foremanimport/` — `mapper.go` rewritten to build site +
  building + rack rows from Foreman's parent-pointer sitemap tree
  per J7. Existing flat-group output path is removed — Foreman
  imports into the new hierarchy directly.
- All history-writing domain packages (`miner_state_snapshots`,
  errors, telemetry, command-log, etc.) populate the row-stamped
  `site_id` from `device.site_id` at insert time.
- `onboarding/` — **no changes.** Site setup is not part of
  onboarding.

Existing domain APIs that continue to operate org-scoped (no per-site
slicing in MVP): pools, schedules, queue, api_keys, team, firmware.
Listed explicitly so reviewers don't expect site filters that aren't
there. Errors / activity / telemetry / snapshots *do* gain per-site
filtering via the row-stamped `site_id`, but their config and
ownership remain org-level.

### RBAC

The proto-fleet auth model today defines two roles: `SUPER_ADMIN` and
`ADMIN`. SUPER_ADMIN is the only role that can manage team members
(create/reset/deactivate users); ADMIN can do everything else
fleet-related.

Multi-site preserves that model:

| RPC | SUPER_ADMIN | ADMIN |
|---|---|---|
| `ListSites`, `ListBuildings` | ✓ | ✓ |
| `CreateSite` / `UpdateSite` / `DeleteSite` | ✓ | ✓ |
| `CreateBuilding` / `UpdateBuilding` / `DeleteBuilding` / `AssignBuildingToSite` | ✓ | ✓ |
| `ReassignDevicesToSite` | ✓ | ✓ |
| `Pair` | ✓ | ✓ |

User management remains SUPER_ADMIN-only, unchanged from today.

## Frontend updates

Core views to add or update. Component naming is illustrative; final
names land in the technical plan.

**Feature-flag gating.** Both the primary sidenav button for `/sites`
and the settings subnav entry for `/settings/sites` sit behind a
Vite-time env flag (e.g. `VITE_MULTI_SITE_ENABLED`). The buttons are
hidden when the flag is off; the routes themselves are not
flag-guarded, so QA + dogfood can still navigate directly. When the
flag flips on, no other code paths change — every page already
renders without a SitePicker when the org has zero sites, so the
flag-off state is a strict subset of the flag-on state.

**New views (Phase 1a scaffolding).**

- **Sites overview page** at `/sites`. Reached from the
  feature-flagged primary sidenav. Renders the per-site sections
  described in J8, stacked in All Sites mode and singular in
  single-site mode. Phase 1a ships header + placeholder blocks for
  metric components and BuildingCards (FPO grey boxes linking to
  `/buildings/:id`); Phase 1b replaces those with real components.
- **Sites admin page** at `/settings/sites`. Reached from the
  feature-flagged settings subnav. Renders the empty-state CTA, the
  All Sites flat table, or the single-site config view per J3.
- **Building overview page** at `/buildings/:id`. Renders per J9.
  Phase 1a ships header + placeholder blocks; Phase 1b lands metric
  components, the rack grid + rack-state derivation, and the
  performance charts.
- **Topbar SitePicker** — Phase 1a replaces today's
  `LocationSelector` placeholder in `PageHeader` globally on every
  protoFleet route. The three new routes consume the selection
  immediately; existing routes (`/miners`, `/racks`, dashboards)
  leave the picker informational until Phase 1b wires their
  queries. Hidden when org has zero sites. Otherwise: "All Sites"
  + each accessible site + "Unassigned" entry. Selection persists
  to localStorage keyed by username so it survives navigation and
  reload.

**Modals (Phase 1a, shared across `/sites` and `/settings/sites`).**

- **`SiteDetailsModal`** — site detail form. Create-mode primary
  button is "Continue" (defers create); edit-mode buttons are
  "Delete" + "Save". Fields per J3.
- **`ManageSiteModal`** — FullScreenTwoPane membership manager per
  J3. Drives both site create (via deferred-commit "Save" from
  `SiteDetailsModal` Continue) and site edit (via "Manage site"
  button on single-site page).
- **`BuildingDetailsModal`** — create or edit a single building,
  always scoped to a parent site. Fields land per the PR 3 spec
  in J3 (name, type stub, power MW, overhead kW). Default rack
  layout inputs are deliberately *not* surfaced in Phase 1a.
- **`ManageBuildingModal`** — Phase 1a (PR 3). FullScreenTwoPane
  sibling of `ManageSiteModal` for managing rack membership
  inside a building. MVP shape is `AssignMinersModal`-inspired
  (aisles × racks_per_aisle grid; byName / manual modes;
  `SearchRacksModal` for the rack picker). Production design
  refinement lands in Phase 1b.
- **`SearchRacksModal`** — Phase 1a (PR 3). Sibling of
  `SearchMinersModal`; lists racks under the parent site with
  ineligible-but-visible greying for racks already assigned to a
  different building.
- **"Assign to site" bulk modal** — used from miner list bulk
  action (Phase 1b ticket; tracks #199). Includes "(Unassigned)"
  as a target option.

**Updated views:**

- **Miner List** (Phase 1b, tracks #199) — new site column
  (rendered when org has ≥1 site; hidden when site-less), new site
  filter chip with "Unassigned" as a value alongside the actual
  sites, site-aware saved views. Active-site selection from the
  topbar applies on top of any saved view's filters (intersection).
  Gains a **building** filter; existing **zone** filter remains as
  the sub-building organizer within a building. Racks may appear
  with a site but no building.
- **Needs Attention status** (Phase 1b, tracks #200) — gains the
  `site_id IS NULL` condition, gated on org having ≥1 site.
- **CompleteSetup module** (Phase 1b, tracks #200) — "Assign miners
  to sites" TaskCard, gated identically.
- **Page header / app shell** — Phase 1a replaces the
  `LocationSelector` placeholder with the SitePicker on every
  route. The new pages read active site from localStorage and
  scope reads accordingly; existing pages render the picker but
  don't yet consume the selection. Phase 1b wires the existing
  miner list + rack list to consume the active site (rack list
  BE filter is a Phase 1b deliverable).
- **Primary sidenav** (Phase 1a) — adds "Sites" entry pointing at
  `/sites`, wrapped in the Vite feature flag.
- **Settings layout** (Phase 1a) — adds "Sites" entry to the
  settings subnav, wrapped in the same Vite feature flag.

**Components / patterns reused:**

- Existing modal pattern for create/edit forms.
- Existing FullScreenTwoPane modal shell (used today by rack
  creation) — `ManageSiteModal` and `ManageBuildingModal` adopt it
  directly so the visual rhythm matches.
- Existing saved-views machinery and filter-chip components.
- Existing `SettingsLayout` shell for the new `/settings/sites`
  page.
- Existing `useLocalStorage` hook for active-site persistence.
- Existing metric-component primitives from the rack overview page
  (shared with `/sites` and `/buildings/:id`).

## Phasing

Phasing is scaffold-first. The Block dogfood acceptance gate is the
target outcome (ops creates sites, organizes buildings, assigns miners,
filters miner list), but the order of work is rearranged so that
navigation correctness + full-stack CRUD wiring land first and the
purely visual content lands second. This protects the architectural
review surface — which moves slowly — from getting blocked by metric
component fidelity, which can iterate in parallel.

The backend foundation (PRs A/B/C, issues #195/#196/#197) is already
underway and follows its own merge order; phase boundaries below
describe **frontend + downstream** work. PR A (#195) and PR B (#196)
have shipped; PR C (#197) is still open and unblocks the miner-list
work in Phase 1b.

Both sidenav entries for the new feature ship behind a Vite-time
env flag (e.g. `VITE_MULTI_SITE_ENABLED`) so the buttons remain
hidden until Phase 1b is complete. The routes themselves are not
flag-protected, which keeps dogfood + QA paths open.

### Phase 1a — scaffolding and CRUD (single PR, ships first)

Goal: every route exists, every modal works end-to-end, every CRUD
operation persists through to the DB. Visual content is placeholder
blocks. Block ops can manually seed sites/buildings via the modals or
direct DB inserts to demo the navigation flow.

Work in this phase consolidates into three sequential PRs:

**PR 1 — scaffold + SitePicker** (this is what previously was issue
#198 plus a slice of Phase 2):

- Routes wired: `/sites`, `/settings/sites`, `/buildings/:id`. Each
  page is a shell — header / button row only, with FPO placeholder
  blocks where Phase 1b will land metric components, BuildingCards,
  diagnostics, and the performance section.
- Topbar SitePicker — replaces today's `LocationSelector`
  placeholder in `PageHeader` globally; visible on every
  protoFleet route from Phase 1a. The three new routes consume
  the selection immediately; existing routes (`/miners`,
  `/racks`, dashboards) leave the picker informational until
  Phase 1b (#202) wires their queries. Hidden when org has zero
  sites; renders "All Sites" + each accessible site +
  "Unassigned" otherwise. localStorage-keyed by username so the
  selection persists across navigation and reload.
- Primary sidenav "Sites" button → `/sites`, feature-flagged.
- Settings subnav "Sites" entry → `/settings/sites`,
  feature-flagged with the same flag.
- `/settings/sites` empty-state CTA (zero sites). All Sites flat
  table render. Single-site config view render. All three modes
  driven by the topbar SitePicker.
- `/sites` empty-state CTA. All Sites stacked-section render with
  FPO building cards. Single-site render.
- `/buildings/:id` header + button row (View racks / View miners /
  Edit building) wired; body sections are placeholder blocks.
- `useSites` + `useBuildings` API hooks (ListSites / ListBuildings).
- Acceptance: with a hand-seeded DB containing 3 sites, several
  buildings, and a few miners, an operator can navigate through
  every route via the SitePicker and the sidenav, see correct
  attachment counts in the table cells that already render, and
  reach a placeholder building page from a building card. No
  modals are required to pass this acceptance.

**PR 2 — site creation + edit flows:**

- `SiteDetailsModal` (create + edit modes) wired to `CreateSite` /
  `UpdateSite`.
- `ManageSiteModal` (FullScreenTwoPane) with network input,
  buildings table, building-grid preview, and "Save" /
  "Edit details" buttons. Drives both site create (deferred-commit
  via "Continue" from `SiteDetailsModal`) and site edit.
- Site delete with cascade-unassign confirm dialog reading
  `device_count` / `building_count` / `rack_count` from
  `ListSites`.
- Two-call orchestration documented in J3: `CreateSite` then
  optional `ReassignDevicesToSite`. UI surfaces a clear error if
  the second call fails.
- Acceptance: operator creates 3+ sites entirely through the UI,
  edits one, deletes one with the cascade dialog. Sites render in
  both `/sites` and `/settings/sites`.

**PR 3 — building CRUD + rack assignment flows:**

- `BuildingDetailsModal` (create + edit modes) wired to
  `CreateBuilding` / `UpdateBuilding`. Always scoped to a parent
  site context — no orphan-building create paths in the UI.
  Fields: name, type (disabled stub pending `building_type` enum
  follow-up — see Phase 1b deferred fields), power capacity (MW;
  converted to `power_kw` on submit), overhead (kW). Default rack
  layout inputs (`default_rack_rows` / `default_rack_columns` /
  `default_rack_order_index`) are intentionally not surfaced in
  Phase 1a — the proto fields stay optional and BE-side default
  to UNSPECIFIED/0 until a future FE need surfaces them. Aisles
  and racks_per_aisle move onto `ManageBuildingModal` (see below)
  because they only matter for the grid view.
- `BuildingDeleteDialog` — cascade-confirm reading `rack_count`
  from `BuildingWithCounts`. Rack-only language (no
  device-count callout in this PR — `BuildingWithCounts` does not
  carry `device_count`; the indirect-impact wording in J3 is
  deferred to a follow-up that extends the response shape).
- `ManageBuildingModal` (FullScreenTwoPane) — pulled into PR 3
  from Phase 1b. MVP shape inspired by `AssignMinersModal`:
  - Header "Edit building" button stacks `BuildingDetailsModal`
    on top (mirror of `ManageSiteModal` → `SiteDetailsModal`
    stacking). Delete is owned by the details modal.
  - Left pane: aisles + racks_per_aisle inputs (drive grid
    dimensions; persist via `UpdateBuilding`), "Assign racks"
    button → `SearchRacksModal`, byName / manual assignment mode
    toggle, list of currently-assigned racks. byName auto-fills
    grid cells alphabetically; manual lets the operator click a
    cell + rack to position.
  - Right pane: `aisles × racks_per_aisle` grid. Empty cells
    render a `+` block, assigned cells render the rack label.
  - Save persists touched racks via repeated `SaveRack` calls
    (sets `building_id`, `aisle_index`, `position_in_aisle` per
    rack) — see schema additions in the next bullet.
- `SearchRacksModal` — sibling of `SearchMinersModal`. Lists
  racks under the parent site; racks already in a different
  building render greyed-out (ineligible-but-visible, same
  pattern as `SearchMinersModal`).
- **Schema additions in this PR**: add nullable
  `device_set_rack.aisle_index INT` and
  `device_set_rack.position_in_aisle INT`. Constraints: paired
  (both NULL or both set), non-negative when set, require
  `building_id IS NOT NULL`, and a partial unique index on
  `(building_id, aisle_index, position_in_aisle)` so a cell holds
  at most one rack. The existing `UpdateRackPlacement` query
  (used by `SaveRack`) clears both fields on any `building_id`
  transition (mirrors the zone-clear cascade) — positions never
  outlive their parent building.
- **New dedicated RPC**: `BuildingService.AssignRackToBuilding`.
  Inputs: `rack_id` (int64, required), `building_id` (optional —
  unset = unassign from building), `aisle_index` /
  `position_in_aisle` (optional pair — unset = building member
  without specific grid placement). Validates upper bounds
  against the parent building's `aisles` / `racks_per_aisle`,
  enforces cell uniqueness via the partial index, and runs the
  same site-cascade rules that `SaveRack` already runs when
  building changes. SaveRack stays focused on rack-level
  full-state writes; this RPC is the position-write path used by
  `ManageBuildingModal`. Read path for the grid is a new
  `BuildingService.ListBuildingRacks(building_id)` returning
  `repeated BuildingRack { int64 rack_id; string rack_label;
  optional int32 aisle_index; optional int32 position_in_aisle; }`.
  Keeping these on `BuildingService` (not `CollectionService`)
  scopes the new contract to the building UI without bloating
  the rack RPC surface.
- Entry points wired:
  - `/settings/sites` single-site Buildings table — "Add
    building" CTA → `BuildingDetailsModal` create; row click →
    `BuildingDetailsModal` edit.
  - `ManageSiteModal` left-pane buildings table — replaces the
    Phase 1a placeholder. Same Add CTA + row-click semantics.
  - `/buildings/:id` header "Edit building" → opens
    `ManageBuildingModal`. The page's `Edit building` stub from
    Phase 1a goes away.
  - `/sites` per-site overview building tiles — Phase 1a left
    them as inert FPO blocks; PR 3 makes them clickable links to
    `/buildings/:id` (the real `BuildingCard` component still
    lands in Phase 1b).
- Post-delete navigation rules: delete from
  `BuildingDetailsModal` opened *inside* `ManageSiteModal`
  collapses the details modal back to the manage view (the
  building goes away in the left-pane table). Delete from
  `BuildingDetailsModal` opened *inside* `ManageBuildingModal`
  redirects to `/sites` — the manage modal's anchor is the now-
  deleted building.
- Acceptance: operator adds buildings to a site through both
  `ManageSiteModal`'s left-pane "Add building" button and the
  single-site Buildings table's "Add building" CTA. Edits and
  deletes a building. Opens `ManageBuildingModal` from
  `/buildings/:id`, assigns racks via the grid, switches between
  byName / manual modes, persists positions, and reloads to see
  the same layout.

### Phase 1b — data enrichment (parallel-safe with 1a tail)

Goal: replace every placeholder block with real content. Light
backend follow-ups land here where they unblock visible product.

- Real metric components everywhere: `/sites` per-site metric row,
  single-site Details table, `/buildings/:id` metrics row.
  Underlying data joins `MinerStateSnapshot` (with #197's
  `site_id`) and the existing telemetry rollups.
- Real `BuildingCard` component replacing the FPO grey box on
  `/sites`.
- `/buildings/:id` diagnostics section (rack grid + rack-state
  derivation) and performance section.
- `ManageBuildingModal` polish — full design lands here (the PR 3
  MVP is `AssignMinersModal`-inspired; the production design is
  still in flux).
- BE follow-ups for deferred fields, each in its own PR:
  - `site` address columns (`address_line1`, `address_line2`,
    `postal_code`, `country`) and `notes`.
  - `building.building_type` enum.
  - `building.network_config`.
  - `building.cooling_type` (only if FE surface needs it; can
    slip to Phase 2).
- FE inputs and display rows for each newly-shipped column.
- Miner list site column + site filter chip + bulk "Assign to
  site" action (was #199). Depends on PR C (#197) landing for the
  filter fields. Miner list also begins honoring the active-site
  selection from the topbar (intersection with any saved-view
  filters).
- **Rack list site scoping (new BE work).** Add `repeated int64
  site_ids` + `bool include_unassigned` to
  `ListDeviceSetsRequest` (proto + handler + sqlc), mirroring the
  miner-list filter shape. `RacksPage` reads the active-site
  selection from localStorage and passes it into the query.
  Optional rack-list site filter chip + site column on the rack
  table track as part of the same enrichment work.
- **SitePicker consumption rollout.** With miner-list and
  rack-list scoping in place, both pages start reading the active
  site from localStorage and pass it through to their list
  queries. The SitePicker was already mounted globally in Phase
  1a; this work removes the transitional gap noted in the picker
  tooltip / release notes. History-bearing pages (errors,
  activity, telemetry, dashboards) still ignore the selection
  until Phase 2 — tooltip / release-note guidance updates to
  reflect the new scope.
- "Needs Attention" gains the `site_id IS NULL` condition (was
  #200).
- CompleteSetup "Assign miners to sites" TaskCard (was #200).
- Foreman importer rewritten to map the sitemap tree onto
  `site → building → rack` per J7 (was #201).
- Activity-log rows on every site CRUD, building CRUD, and
  reassignment (BE side ships with #196/#197; FE side ensures
  reads honor them).
- Flip the Vite feature flag to expose the sidenav + settings
  subnav entries once everything above is stable.

Acceptance: Block ops walks through the full create-3+-sites,
organize-buildings, assign-miners workflow in <30 minutes from
`/settings/sites`, `/sites`, and the miner list, no engineer help.
An org that ignores the feature continues operating site-less with
no regressions.

### Phase 2 — site-segmented discovery + history filters

Goal: pairing flow becomes site-aware and history surfaces honor
site filters.

- "All Sites" / "Unassigned" modes wired through history-bearing
  pages (errors, activity, telemetry, dashboards). Site filter on
  those pages reads the row-stamped `site_id` (added in Phase 1),
  not the device's *current* `site_id`. Pre-multi-site rows
  surface in a "(no site)" bucket and are excluded from
  specific-site filters.
- Discovery results segmented by site network config: each
  discovered miner is grouped under the site whose IP range caught
  it; operator can drag-and-drop between site buckets before
  clicking Pair, and miners pair directly into the operator-
  confirmed site.
- Saved views: site filter included in the existing serialization;
  pre-existing saved views remain valid.
- Evaluate whether zones need promotion beyond the Phase 1
  rack-owned string model. No zone schema change is planned by
  default in Phase 2.
- Polish: multi-select on bulk reassign, undo, batch progress.

Acceptance: pairing into a specific site works without a separate
post-pair assignment step.

### Phase 3 — site energy statistics

Goal: surface the energy data captured in the site config (power
capacity, contract terms, demand charges, etc.) as dashboards and
operational signals. Not blocking the multi-site basics, so
deferred until the foundation is in place. Scope detailed in a
follow-on plan.

No further phases planned. The fleet node workstream owns its own
schema and discriminators; site `network_config` remains the
canonical signal for "which miner belongs to which site" whether
the data plane is direct-from-cloud or fleet-node-relayed, so there is
no multi-site work tied to the fleet node rollout. If mining ops later
asks to split currently-org-scoped config (pools, schedules, etc.)
per-site, that's a separate plan.

## Open questions to resolve in the technical plan

These are intentionally not answered here — they need code-level review
before they're locked.

1. The exact `/20` CIDR cap on `network_config` entries — calibrate
   against real Block-ops site sizes before locking. (Validation
   shape itself is locked above.)
2. Behavior when a site-segmented discovery (Phase 2) finds a miner
   reachable on a different site's IP range than the operator's drag-
   and-drop choice: do we warn, block, or silently honor the operator?
   Working answer: warn, honor.
3. Whether a rack moved into a different building should always have its
   `zone` cleared, or whether the UI should offer a "preserve when the
   target building already has the same zone label" shortcut. Working
   answer for MVP: always clear.
4. Building deletion confirmation dialog wording when racks are
   present but those racks contain devices — working answer:
   call out the indirect device-impact count when
   `device_count > 0` (derived from the building's racks), keep
   it concise otherwise.
5. Power-contract enum coverage gaps as customers onboard — utility
   list completeness for unfamiliar regions. (Deferred along with
   the columns themselves.)
6. Whether `ManageSiteModal`'s right-pane building grid becomes
   interactive (drag-to-reorder, click-to-edit) in Phase 1b or
   stays display-only.
7. Whether `/sites` should accept a deep-link `/sites/${site.id}`
   for bookmarking a specific site, separate from the SitePicker
   state. Working answer: yes, but the URL drives the SitePicker
   on mount rather than diverging from it.
8. Rack-state derivation rule for the `/buildings/:id` diagnostics
   grid (healthy / sleeping / needs-attn / offline). Working
   answer: "needs attention" if any member miner is needs-attn;
   "offline" if every member is offline; "sleeping" if every
   member is sleeping; "healthy" otherwise. Validate against real
   miner-state data before lock-in.
9. Whether a building with zero racks renders the `/buildings/:id`
   diagnostics section at all, or replaces it with an empty-state
   message.
10. Whether `zone` should eventually graduate from the Phase 1
    rack-owned string into a first-class entity. Working answer:
    stay rack-owned until one of the documented triggers fires —
    zone-level attributes (color, coordinates, capacity, cooling
    defaults, setpoints, ACLs), non-rack equipment in a zone,
    pre-provisioning empty zones, or rename-as-hot-path. See the
    "Forward look" callout in the Storage model section for the
    migration shape.

## Appendix — power contract enum suggestions

> **Status: deferred.** Power-contract columns are not in Phase 1 or
> Phase 1b. This appendix is retained for the eventual follow-up
> migration so the enum coverage and utility-list reasoning is not
> lost.

ISOs / RTOs (FERC-recognized):

- ERCOT, PJM, MISO, CAISO, SPP, NYISO, ISO-NE, plus
  "Non-ISO / Bilateral".

When `iso = NON_ISO`, balancing authority dropdown:

- TVA (Tennessee Valley Authority) — TN, KY, AL, MS
- Southern Company — Georgia Power, Alabama Power, Mississippi Power
- Duke Energy Carolinas / Duke Energy Progress (NC, SC)
- BPA (Bonneville Power Administration) — WA, OR, ID
- PacifiCorp East/West — WY, UT, OR, ID
- Salt River Project (AZ)
- Associated Electric Cooperative (MO/AR/OK)
- Other (free-text fallback)

Initial utility-operating-company suggestion list (free-text fallback
allowed; ISO is a soft filter, not a hard one):

- Texas / ERCOT: Oncor Electric, CenterPoint Energy, AEP Texas, TNMP,
  LCRA, Brazos Electric Cooperative, Bluebonnet Electric Cooperative,
  Pedernales Electric Cooperative
- Texas / non-ERCOT: Entergy Texas (MISO), El Paso Electric (WECC
  non-ISO), SWEPCO (SPP)
- PJM: AEP Ohio, Duke Energy Ohio, Duke Energy Kentucky, ComEd, PECO,
  ConEd
- MISO: Entergy (LA/AR/MS), Ameren, Duke Energy Indiana
- SPP: Xcel Energy (Southwestern Public Service), AEP SWEPCO,
  Westar/Evergy
- CAISO: PG&E, SCE, SDG&E
- NYISO: ConEd, National Grid (NY)
- ISO-NE: National Grid (MA/RI), Eversource, NSTAR
- Non-ISO Southeast: Duke Energy Carolinas, Duke Energy Progress,
  Georgia Power, Florida Power & Light, Alabama Power
- Non-ISO West / mining-heavy: Rocky Mountain Power (PacifiCorp),
  Black Hills Energy, Idaho Power, Grant County PUD, Chelan PUD,
  Douglas PUD, NV Energy, Salt River Project
- Non-ISO upper Midwest: Basin Electric Power Cooperative,
  Tri-State G&T, Otter Tail Power, Montana-Dakota Utilities
- Non-ISO TVA: Knoxville Utilities Board, Memphis Light Gas & Water,
  Nashville Electric Service (TVA local power companies)
- Non-ISO Kentucky: Kentucky Utilities, LG&E (PPL)

Operators in regions not represented above pick "Other" and free-text
their utility name. Track which free-text values come up most often
and promote to the suggestion list over time.

## References

- Source design doc:
  `~/.gstack/projects/block-proto-fleet/flesher-main-design-20260505-114045.md`
- Current onboarding:
  `server/internal/domain/onboarding/service.go`
- Current topbar placeholder:
  `client/src/protoFleet/components/PageHeader/LocationSelector/LocationSelector.tsx`
- Current saved-views infra:
  `client/src/protoFleet/features/fleetManagement/views/savedViews.ts`
- Current localStorage hook:
  `client/src/shared/hooks/useLocalStorage.ts`
- Current rack/zone schema:
  `server/migrations/000012_create_device_collection_tables.up.sql`
- Current pairing service (discovery methods):
  `server/internal/domain/pairing/service.go`
- Current auth/RBAC service:
  `server/internal/domain/auth/service.go`
