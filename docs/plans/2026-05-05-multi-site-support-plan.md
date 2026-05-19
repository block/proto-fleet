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

`/settings/sites` is the admin surface for sites and buildings.

**Empty state (org has zero sites).** Page renders a CTA: "Create your
first site to organize miners by location." If the org has any
unassigned buildings (created explicitly by the operator), they
appear in a separate section below the CTA so the operator can
rename / edit them before assigning to a site.

**Specific site selected in topbar.** Page shows the config for that
one site, in the section layout below.

**"All Sites" selected in topbar.** Page shows every site, each
rendered as its own section (same layout), with a "Create site" CTA
at the top and an "Unassigned buildings" section at the bottom
listing buildings with no site. (Bulk reassignment of miners lives
on the miner list — see J6 — not here.)

In Phase 1 (no topbar yet), `/settings/sites` always renders in the
"All Sites" layout when ≥1 site exists. The empty-state layout is
unchanged.

**Per-site section layout.**

- Heading: site name (with edit affordance)
- **Site details card** (half width): location, capacity. Edit button
  → modal that updates site name, location, capacity, timezone.
- **Power contract card** (half width): ISO, utility, rate, contract
  end date. Edit button → modal that updates ISO, utility, rate type,
  rate, demand charge, transmission structure, power factor, contract
  start, contract end.
- **Buildings card** (full width): table of buildings with columns
  for name, power, racks, kebab menu (view racks, view miners, assign
  to another site, delete building). "Add building" CTA opens a
  modal.

**Unassigned buildings section** (rendered in "All Sites" mode and in
the empty-state page): a table of buildings with no `site_id`,
showing the same columns plus an "Assign to site" inline action.

**Site create modal.** Mirrors today's rack-creation flow that lets
the operator pick miners during create. The modal has two sections:

1. Site details — name, location, timezone, capacity, optional
   power contract.
2. Optional "Assign miners" picker — multi-select over the org's
   currently-unassigned miners, with the same selection ergonomics
   as the bulk-assign modal in J6. Operator can skip and assign
   later from the miner list.

On save, the create + miner-assign happen in one transaction:
the site row is inserted and `device.site_id` is updated for the
picked miners. Cross-collection rule still applies — if any picked
miner is in a rack already assigned to a *different* site, the
entire create is rejected with the same per-device error detail.
(In practice this is rare during create since the site itself is
brand new.)

Optionally extend with a "claim existing buildings" picker too;
flagged as an open question.

**Cross-site building moves** are allowed via the "Assign to another
site" inline action in the buildings card kebab menu. The confirmation
dialog explicitly warns that every rack stays under the building and
that every descendant rack and device will have its `site_id` moved to
the new site in the same transaction.

**Site and building deletion — cascade-unassign with warn-first
dialog.** Deletion is never blocked by attached entities. Instead,
the UI reads attachment counts from the list response and presents a
confirmation dialog before the destructive call:

- *Site delete dialog:* "Deleting site 'X' will unassign **N
  miners**, **M racks**, and **P buildings**. They will move to the
  Unassigned bucket. Continue?" Buttons: [Cancel] [Delete site].
- *Building delete dialog:* "Deleting building 'Y' will unassign
  **N racks** from this building and clear their zone labels. They
  will remain directly assigned to their current site. Continue?"
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

- Whether the empty-state page should also show unassigned **miners**
  (since the SitePicker provides "Unassigned" as a filter, the miner
  list is the better home for that). Working answer: no, miner
  triage stays on the miner list.

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

**Buildings.** From `/settings/sites` (Unassigned buildings section
or kebab menu on a per-site building row): "Assign to site" inline
action. Single-building modal picks a target site and shows the
descendant rack/device counts that will move with it. On confirm, the
server updates `building.site_id` and cascades the new `site_id` to
every rack and device under that building in one transaction.

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

### Domain logic and APIs

New domain packages:

- `server/internal/domain/sites/` — site CRUD, list, reassign-devices-
  to-site, network-config get/set, power-contract get/set. No
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

**New views:**

- **Sites admin page** at `/settings/sites`. Renders an empty-state
  CTA when the org has zero sites, with an "Unassigned buildings"
  section below if any exist. Renders per-site sections (site
  details + power contract + buildings) when ≥1 site exists, plus
  support for racks that live directly under a site without a
  building, and an "Unassigned buildings" section at the bottom in
  "All Sites" mode.
- **Site create modal** — site details + power contract + optional
  "Assign miners" picker (see J3).
- **Site edit modal** (site details + power contract).
- **Building edit modal** (name + capacity + layout + default rack
  settings + assign-to-site dropdown). Cross-site building moves
  surface the descendant device count in the confirmation dialog.
- **Topbar SitePicker** — replaces today's placeholder
  `LocationSelector`. Hidden when org has zero sites. Otherwise:
  "All Sites" + each accessible site + "Unassigned" entry.
  Selection persists to localStorage keyed by username.
- **"Assign to site" bulk modal** — used from miner list bulk action.
  Includes "(Unassigned)" as a target option.

**Updated views:**

- **Miner List** — new site column (rendered when org has ≥1 site;
  hidden when site-less), new site filter chip with "Unassigned"
  as a value alongside the actual sites, site-aware saved views.
  Active-site selection from the topbar applies on top of any
  saved view's filters (intersection). The miner list gains a
  **building** filter while the existing **zone** filter remains as
  the sub-building organizer within a building, and racks may appear
  with a site but no building.
- **Needs Attention status** — the existing built-in "Needs
  Attention" saved view gains a new condition: when the org has
  ≥1 site and a miner has `site_id IS NULL`, that miner is in
  Needs Attention, parallel to today's "needs authentication"
  condition. Org without sites: condition is inert (no false
  positives in site-less mode).
- **CompleteSetup module** — the existing TaskCards screen
  (`client/src/protoFleet/features/onboarding/components/CompleteSetup.tsx`)
  gains a new TaskCard "Assign miners to sites" alongside today's
  "Authenticate miners" and "Configure pools" cards. The card
  surfaces only when the org has ≥1 site and ≥1 unassigned miner;
  click-through opens the miner list pre-filtered to "Unassigned"
  with the bulk-assign action ready.
- **Page header / app shell** — SitePicker mounted; pages read
  active site from localStorage and scope reads accordingly.
- **Settings layout** — adds "Sites" entry to the settings nav.

**Components / patterns reused:**

- Existing modal pattern for create/edit forms.
- Existing saved-views machinery and filter-chip components.
- Existing `SettingsLayout` shell for the new Sites pages.
- Existing `useLocalStorage` hook for active-site persistence.

## Phasing

Phasing is driven by what unblocks the Block dogfood acceptance gate
fastest, then by what de-risks the bigger refactors. Each phase ships
behind whatever flagging the team uses today; the doc doesn't pick a
flag mechanism.

### Phase 1 — data layer + minimal admin (dogfood unblock)

Goal: Block ops can create 3+ sites, organize them with buildings,
assign existing miners to sites via bulk action, see the site column
and filter on the miner list. No topbar yet, no discovery
segmentation yet. App fully functional in site-less form for orgs
that don't opt in.

- Migrations: `site` (location, timezone, network config; power-
  contract columns deferred to a follow-up); `building` (nullable
  `site_id` + layout columns); `device.site_id` nullable;
  `device_set_rack.site_id` nullable; `device_set_rack.building_id`
  nullable, no auto-backfill from zones (operators opt into
  buildings explicitly); `zone` stays on the rack as the Phase 1
  building-scoped sub-organization field.
- `SiteService` proto + handlers: list (returns device + building
  + rack counts), create, update, delete (soft, cascade-unassigns
  devices, racks, and buildings; activity log captures impact);
  reassign-devices; assign-building-to-site with descendant rack and
  device cascade.
- `BuildingService` proto + handlers: list (filterable by site or
  "unassigned"; returns rack count), create, update, delete (soft,
  cascade-unassigns racks and clears their zones).
- `site_ids` (repeated int64) + `include_unassigned` (bool) filter
  fields on miner-list query — split rather than overloaded.
  `site_id` + `site_label` on `MinerStateSnapshot` with writer
  audit.
- Cross-collection enforcement on direct device assignment, plus
  transactional descendant cascades on building assign-to-site,
  **the existing rack edit/move flow**, and the
  `AddDevicesToDeviceSet` rack-add flow. Rack edit must land in
  Phase 1 so miners cannot drift to the wrong site via a rack move,
  and cross-building moves must clear the rack's zone as part of
  the same transaction. `AddDevicesToDeviceSet` cascades the rack's
  `site_id` onto added devices (when the target is a rack with
  `site_id IS NOT NULL`) and records the prior device site in the
  activity log; group-target adds are exempt.
- Server-side validation of site `network_config` (CIDR parse,
  `/20` cap, within-site overlap rejection, cross-site overlap
  warning, canonical-form round-trip).
- Add nullable `site_id` to history-bearing tables
  (`activity_log`, `miner_state_snapshots`,
  `command_on_device_log`, errors, telemetry); writers populate
  from `device.site_id` at write time. Existing rows stay NULL.
- `/settings/sites` page rendering empty state (zero sites) or the
  "All Sites" layout (per-site sections + unassigned buildings
  section). Inline edit modals. Site create modal with optional
  "Assign miners" picker. The topbar-driven single-site rendering
  mode lands in Phase 2 with the SitePicker.
- Site column + site filter chip in Miner List (rendered only when
  org has ≥1 site). Bulk "Assign to site" action.
- "Needs Attention" saved view gains the unassigned-miner condition
  (gated on org having ≥1 site so site-less orgs see no change).
- CompleteSetup TaskCard "Assign miners to sites" (gated on org
  having ≥1 site and ≥1 unassigned miner).
- Foreman importer (`server/internal/domain/foremanimport/`)
  rewritten to map the sitemap tree onto `site → building → rack`
  per J7.
- Activity-log rows on every site CRUD, building CRUD, and
  reassignment.

Acceptance: Block ops walks through the full create-3+-sites,
assign-buildings, assign-miners workflow in <30 minutes from
`/settings/sites` and the miner list, no engineer help. An org that
ignores the feature continues operating site-less with no regressions.

### Phase 2 — topbar and site-segmented discovery

Goal: every page is site-aware, pairing flow gains site segmentation.

- Topbar SitePicker replaces the `LocationSelector` placeholder.
  localStorage-backed active-site selection. Hidden when org has
  zero sites; otherwise renders "All Sites" + sites + "Unassigned".
- "All Sites" / "Unassigned" modes wired through every list/read
  page (miner list, errors, activity, dashboards, etc.). Site
  filter on errors / activity / telemetry / dashboards reads the
  row-stamped `site_id` (added in Phase 1), not the device's
  *current* `site_id`. Pre-multi-site rows surface in a "(no
  site)" bucket and are excluded from specific-site filters.
- Discovery results segmented by site network config: each
  discovered miner is grouped under the site whose IP range caught
  it; operator can drag-and-drop between site buckets before
  clicking Pair, and miners pair directly into the operator-
  confirmed site.
- Saved views: site filter included in the existing serialization;
  pre-existing saved views remain valid.
- Evaluate whether zones need promotion beyond the Phase 1 rack-owned
  string model (for example: dedicated zone picker UX, caching of
  distinct zones per building, or a first-class entity). No zone
  schema change is planned by default in Phase 2.
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
   present but those racks contain devices — call out the indirect
   impact (devices stay site-assigned but lose their rack/building
   linkage), or keep the dialog focused on rack count only.
5. Power-contract enum coverage gaps as customers onboard — utility
   list completeness for unfamiliar regions.
6. Whether the "Unassigned buildings" section should also offer a
   single-click "Create site from this building" shortcut.
7. Whether the site-create modal should also include a "Claim
   existing buildings" picker (alongside the "Assign miners"
   picker), useful for operators who built up unassigned buildings
   before creating their first site.
8. Whether `zone` should eventually graduate from the Phase 1
   rack-owned string into something stronger (cached distinct values,
   first-class entity, or explicit per-building managed list), or
   remain lightweight indefinitely.

## Appendix — power contract enum suggestions

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
