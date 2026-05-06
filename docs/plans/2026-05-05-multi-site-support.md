---
title: Multi-site support
date: 2026-05-05
status: draft
type: plan
---

## Summary

proto-fleet today assumes one install = one site. This plan adds sites as a
first-class entity so a single install can manage miners across N physical
locations, with a hierarchy of `site → building → rack → device`. The miner
list, settings pages, pairing flow, and onboarding all become site-aware. An
"All Sites" mode aggregates reads across sites; writes always target a single
site explicitly.

## Goals

- Block mining-ops can manage 3+ sites from one install: name the sites,
  reassign existing miners, pair new miners into the right site, filter and
  navigate the UI scoped to a chosen site or aggregated across all sites.
- Existing single-site installs upgrade with no data loss and no required
  user action — they get a "Default Site" backfill and a dismissible banner.
- Schema and APIs leave room for the future on-prem-agent workstream
  (one agent per site) without building any of it now.

## Non-goals

- Per-site RBAC, per-site permissions for non-admin users.
- Consolidating multiple existing proto-fleet installs into one multi-site
  install.
- Per-site config split for pools, security policies, firmware, schedules,
  team membership, API keys. These stay org-scoped in MVP. Sites carry
  network config (IP ranges for discovery), location/timezone/capacity,
  optional power contract, and a list of buildings. Layout details (aisles,
  racks per aisle, default rack settings) live on the building entity, not
  the site.
- Per-site historical reporting or retroactive site rewrites on existing
  log/snapshot rows.
- Site-scoped discovery via on-prem agents. Out of scope for this plan;
  owned by the agent workstream.

## User journeys

These are the surfaces in the product that touch the concept of "site". Each
journey calls out the open design questions it raises.

### J1. Onboarding a new org

A new org's first-run flow, after admin user + security setup:

1. **Name your sites.** Required step. User must create at least one site to
   proceed. UI defaults to a single row pre-populated with "Site 1"; user
   can add rows for additional sites. Per-row required fields:
   - Site name
   - Location (city, state)
   - Timezone
   - (Collapsible/optional) IP ranges to scan during discovery — one or
     more CIDRs / IP-range strings, defaulting to the install's local
     subnet. See J4 and §Backend.
   Optional fields (power capacity, power contract, etc.) are deferred
   to the post-onboarding settings page so the onboarding form stays
   short for solo operators.
2. **Configure pools.** Unchanged today. Pools remain org-scoped in MVP.
3. **Pair miners.** Pairing UI requires a target site (see J4). If the org
   has exactly one site, it's preselected. If multiple, user picks before
   the discovery scan starts.

Open questions:

- Whether the network-config field should be a single multiline string
  (newline-separated CIDRs/IPs) or a structured array. Working assumption
  in this plan: multiline string, because it matches how operators
  copy-paste IP ranges today and because the existing discovery code
  already accepts a flat list.

### J2. Page-header app switcher (site picker)

Every page in protoFleet sits behind a topbar control that selects either a
specific site or "All Sites". This replaces the placeholder
`LocationSelector` in `PageHeader.tsx`.

- **Specific site selected** → all reads scoped to that site. All writes
  target that site without further prompting.
- **"All Sites" selected** → reads aggregate across every site the user can
  see (today: every site in their org). Writes that target a site (create
  rack, add miners, etc.) require an explicit site picker inside the
  action's UI. Writes that *don't* target a site (org-scoped settings,
  pool config, etc.) are unaffected. Bulk operations (firmware update,
  restart) across miners from multiple sites are allowed — the operation
  is per-miner, so cross-site batching is fine.

**Persistence.** Active site selection is stored client-side in
localStorage, keyed by username, mirroring the saved-views pattern at
`client/src/shared/hooks/useLocalStorage.ts:3-45` and
`savedViews.ts:96`. This avoids a `session.active_site_id` migration and
the matching server-side resolution middleware. The server validates
that any `site_id` sent with a request belongs to the user's org —
that's the actual security boundary; "active site" itself is pure UX
preference.

**Default after first login.** "All Sites" if the user has access to
more than one site; otherwise the single accessible site.

### J3. Site config (Settings → Sites)

`/settings/sites` is the admin surface for sites.

- **Specific site selected in topbar** → page shows the config for that
  one site, in the section layout below.
- **"All Sites" selected in topbar** → page shows every site, each
  rendered as its own section (same layout), with a "Create site" CTA
  at the top and a "Reassign miners" bulk action.

**Per-site section layout (both modes use this).**

- Heading: site name (with edit affordance)
- **Site details card** (half width): location, cooling mode, capacity.
  Edit button → modal that updates site name, location, cooling mode,
  capacity, timezone.
- **Power contract card** (half width): ISO, utility, rate, contract end
  date. Edit button → modal that updates ISO, utility, rate type, rate,
  demand charge, transmission structure, power factor, contract start,
  contract end. (Detailed enums in §Backend.)
- **Buildings card** (full width): table of buildings with columns
  for name, cooling, power, racks, kebab menu (view racks, view miners,
  delete building). "Add building" CTA opens a modal.

Open questions:

- Building deletion semantics when racks exist. Working assumption:
  same pattern as site deletion — 409 unless the building is empty
  (no racks assigned). Last-building-in-site guard not enforced
  (a site without buildings is valid; only sites must have ≥1 site
  in the org).
- Cross-site building moves: out of scope. A building belongs to one
  site, full stop.

### J4. Add miners (Miner List → Add Miners)

The discovery + pairing flow gains site-awareness.

- **Specific site selected in topbar** → "Add Miners" button uses that
  site as the discovery + pairing target. Discovery uses the site's
  configured IP ranges; paired miners are assigned to that site.
- **"All Sites" selected in topbar** → "Add Miners" button first prompts
  the user to pick a target site (a small modal step before the existing
  discovery UI). Once chosen, the rest of the flow is identical. No
  "unassigned" bucket; no per-discovered-miner site picker.
- Discovery scope is governed by the chosen site's network config (CIDRs
  / IP ranges). Falls back to today's behavior (the install's reachable
  network) if a site has no network config set. mDNS / Bonjour discovery
  is link-local and operates without site config.

Open questions:

- Whether to add an "All Sites" scan mode that fans out across every
  site's configured IP ranges and segments findings by which site's
  range each miner came from, auto-assigning at pair time. Lower
  priority than the explicit-target flow above; tracked here so we
  don't lose it.
- Re-pair / move miner between sites: goes through the existing
  reassignment flow on the miner list, not the discovery flow.

### J5. Upgrading an existing install

Existing orgs running today's no-site proto-fleet upgrade seamlessly:

- Migration auto-creates a "Default Site" per org and backfills every
  device, rack, and (newly elevated) building to it.
- Existing zones (sub-rack string metadata today) become **buildings**
  during the same migration: each unique zone string within an org's
  Default Site becomes one building row; racks point at their new
  building. Racks with `zone IS NULL` go into a per-site "Default
  Building" row. Two racks with the same zone string become the same
  building (operator can split later by editing).
- On next SUPER_ADMIN login, a dismissible banner explains the change
  and links to `/settings/sites` to rename the default site, split
  into multiple sites, and reorganize buildings.
- The migration deployment script blocks the upgrade if any pairing
  or discovery job is in flight, to avoid mid-flight inconsistency.

**Migration banner UX.**

- **When:** first SUPER_ADMIN login after upgrade ships.
- **Where:** persistent banner at the top of every protoFleet page
  until dismissed.
- **Copy (draft):** "proto-fleet now supports multiple sites. Your
  existing miners are in 'Default Site'. Add more sites and reassign
  miners as needed." Buttons: [Manage sites] [Dismiss].
- **Persistence:** server-side via
  `user_organization.migration_banner_dismissed_at`. localStorage was
  considered but rejected — we don't want the banner to reappear in a
  new browser, incognito window, or different device. Once the user
  dismisses, the banner is gone forever for that user/org pair.

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
    scan; optional, falls back to install's reachable network)
  - **Power contract fields** (all nullable): `iso` (enum:
    ERCOT, PJM, MISO, CAISO, SPP, NYISO, ISO-NE, NON_ISO), and when
    `iso = NON_ISO`, `balancing_authority` (enum: TVA, SOUTHERN_CO,
    DUKE_CAROLINAS, DUKE_PROGRESS, BPA, PACIFICORP, SRP, ASSOC_ELECTRIC,
    OTHER); `utility_operating_company` (string; see note below);
    `rate_type` (enum: FIXED, INDEX_LMP, PPA, TOU, TIERED, HYBRID);
    `rate_cents_per_kwh`, `demand_charge_cents_per_kwh`,
    `transmission_structure` (enum: 4CP, 5CP, NONE_BUNDLED),
    `power_factor` (enum: 0.85, 0.9, 0.95, 0.97, 1.0),
    `contract_start_date`, `contract_end_date`.
  - Standard timestamp columns + `deleted_at` for soft delete.

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
  enum. Reason: real utility operating companies span multiple ISOs
  (Duke Indiana = MISO; Duke Carolinas = non-ISO; Entergy = MISO;
  AEP = PJM and SPP), so any ISO→utility hard filter would be wrong.
  The UI shows a suggested utility list filtered by chosen ISO with
  a "show all" escape and a free-text fallback. Mismatches surface
  as a soft warning, not a block. Initial suggestion list is in the
  appendix at the bottom of this doc.

- **`building`** — replaces today's `device_set_rack.zone` string as a
  first-class entity. Belongs to one site. Holds:
  - `name` (unique within site)
  - `power_kw` (capacity)
  - `overhead_kw` (non-miner load: cooling, lighting, etc.)
  - `aisles` (count)
  - `physical_rack_count` (physical racks present in the building,
    not the count of software-configured rack rows)
  - `racks_per_aisle`
  - `cooling_mode` (enum: AIR, IMMERSION)
  - `default_rack_type` (FK to existing `rack_type` entity)
  - `default_rack_order` (existing rack-order enum: BOTTOM_LEFT,
    BOTTOM_RIGHT, TOP_LEFT, TOP_RIGHT)

  The default-rack fields describe defaults applied when adding a new
  rack to the building; pre-existing racks may not match these
  defaults, and that's allowed.

- **`device.site_id`** — NOT NULL FK. Backfilled to org's "Default
  Site" during migration. Cross-collection rule: if the device is
  in a rack, `device.site_id` must equal the rack's building's
  site at write time (pair, reassign).

- **`device_set_rack.building_id`** — NOT NULL FK. Backfilled by
  promoting each unique `zone` string per org into a building row,
  then pointing racks at their building. Building's site is
  inherited. The `zone` column may be dropped in a follow-up
  migration after the writer audit completes.

- **`user_organization.migration_banner_dismissed_at`** — nullable
  timestamp gating the upgrade banner. Per-user-per-org. See J5 for
  UX rationale.

Active-site selection is **not** stored in the database — it lives in
client localStorage keyed by username (see J2). Server validates
`site_id` belongs to the user's org on every site-scoped RPC.

The reserved `connection_kind` enum from the source design doc is
**not** included. Rationale: the agent workstream will be the only
write path in the future, and we can build for IP-range discovery now
without committing to a discriminator the agent team hasn't designed
yet. When the agent ships, the agent team adds whatever entities and
discriminators they need.

Relationships after migration:

```
site 1 ──< building 1 ──< device_set_rack 1 ──< device_set_membership >── device
site 1 ──< device                              (direct FK; if device is in
                                                a rack, its site_id must
                                                equal the rack's building's
                                                site)
```

Groups remain org-scoped (no `site_id`); they can span sites.

### Domain logic and APIs

New domain packages:

- `server/internal/domain/sites/` — site CRUD, list, reassign-devices-
  to-site, network-config get/set, power-contract get/set. No
  set-active-site RPC (active site is client-side).
- `server/internal/domain/buildings/` — building CRUD scoped under a
  site, including layout settings (aisles, racks per aisle, default
  rack settings).

Updated domain packages:

- `pairing/` — Pair RPC accepts `site_id` from the request body
  (no session middleware resolution). Discovery handler reads the
  site's `network_config` to scope the scan; falls back to today's
  request-supplied IP ranges and to mDNS link-local when absent.
  In the future agent architecture, the agent will set `site_id`
  implicitly via its identity at install time.
- `device/` — list-devices query gains a `site_ids` filter (direct
  FK, same shape as the existing `group_ids` / `rack_ids` filters).
  The `MinerStateSnapshot` proto gains `site_id` and `site_label`;
  every writer is updated.
- `onboarding/` — adds a `SiteConfigured` gate (≥1 non-deleted site
  in the org with location + timezone set). New "name your sites"
  step before pool config.
- `activity/` — every site CRUD, building CRUD, and reassignment
  writes one log row capturing user, source/target site, device-ids
  JSON.

Existing domain APIs that continue to operate org-scoped (no per-site
slicing in MVP): pools, schedules, errors, telemetry, queue, api_keys,
team, firmware. Listed explicitly so reviewers don't expect site
filters that aren't there.

### RBAC

The proto-fleet auth model today defines two roles: `SUPER_ADMIN` and
`ADMIN`. SUPER_ADMIN is the only role that can manage team members
(create/reset/deactivate users); ADMIN can do everything else
fleet-related.

Multi-site preserves that model:

| RPC | SUPER_ADMIN | ADMIN |
|---|---|---|
| `ListSites` | ✓ | ✓ |
| `CreateSite` / `UpdateSite` / `DeleteSite` | ✓ | ✓ |
| `CreateBuilding` / `UpdateBuilding` / `DeleteBuilding` | ✓ | ✓ |
| `ReassignDevicesToSite` | ✓ | ✓ |
| `Pair` (with `site_id`) | ✓ | ✓ |

User management remains SUPER_ADMIN-only, unchanged from today.

### Bulk reassignment — what the action does

Use case: post-migration, an org has 500 miners auto-assigned to
"Default Site". The operator creates Site B and Site C and needs to
move ~200 miners to B and ~150 to C.

Flow:

1. From the miner list (any site context), the operator multi-selects
   miners.
2. Bulk action menu → "Reassign to site" opens a modal with a target
   site picker.
3. Server runs `ReassignDevicesToSite` as an all-or-nothing
   transaction:
   - Validates every selected device belongs to the user's org.
   - For every device currently in a rack, validates the rack's
     building's site equals the target site. If any device fails,
     the entire batch is rejected with `reason = "device_in_rack_at_other_site"`
     and per-device error details listing which devices need to be
     unracked first.
   - On success, updates `device.site_id` for the batch and writes
     one activity-log row capturing user / source-site /
     target-site / device-ids JSON.
4. Modal surfaces the per-device errors (if any) so the operator
   knows which racks to deal with before retrying.

## Frontend updates

Core views to add or update. Component naming is illustrative; final
names land in the technical plan.

**New views:**

- **Sites admin page** at `/settings/sites`. Renders one section per
  site (site details card + power contract card + buildings card,
  per J3). When the topbar is on a specific site, only that site's
  section is shown. When "All Sites", every site is shown plus a
  "Create site" CTA and a "Reassign miners" bulk action.
- **Site edit modal** (site details + power contract).
- **Building edit modal** (name + capacity + layout + default rack
  settings).
- **Onboarding "Sites" step** — slotted between Security and Miners
  in the onboarding flow. Multi-row form, requires ≥1 site with
  name + location + timezone.
- **Topbar SitePicker** — replaces today's placeholder
  `LocationSelector`. Shows current site name (or "All Sites") and
  dropdown listing every site the user has access to plus an
  "All Sites" entry. Selection persists to localStorage keyed by
  username.
- **"Choose target site" modal step** — presented before the existing
  discovery UI in the Add Miners flow when "All Sites" is the active
  context.

**Updated views:**

- **Miner List** — new site column, new site filter chip, site-aware
  saved views. The active-site selection applies on top of any
  saved view's filters (intersection). The `zone` filter chip is
  renamed `building` once buildings ship.
- **Add Miners flow** — discovery scope read from the chosen site's
  network config; pair RPC sends the target `site_id`.
- **Page header / app shell** — SitePicker mounted; pages read
  active site from localStorage and scope reads accordingly.
- **Settings layout** — adds "Sites" entry to the settings nav.
- **Migration banner** — global banner component shown on first
  SUPER_ADMIN login post-upgrade; dismissed via the new
  `migration_banner_dismissed_at` field.

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

Goal: Block ops can name 3+ sites, organize them with buildings,
reassign existing miners, see the site column and filter on the miner
list, all from the settings page. No topbar, no onboarding rework
yet.

- Migrations: `site` (with location, timezone, network config,
  power-contract columns, no `connection_kind`); `building` (with
  layout columns); `device.site_id` NOT NULL FK with Default Site
  backfill; `device_set_rack.building_id` NOT NULL FK with
  zone-promotion backfill (each unique zone string per org becomes a
  building under the Default Site; null zones go to a "Default
  Building"); `user_organization.migration_banner_dismissed_at`.
- `SiteService` proto + handlers: list, create, update, delete (soft,
  with last-site and devices-present guards), reassign-devices.
- `BuildingService` proto + handlers: list (under a site), create,
  update, delete (soft, with racks-present guard).
- `site_ids` filter on miner-list query; `site_id` + `site_label` on
  `MinerStateSnapshot` with writer audit.
- Pairing handler reads `site_id` from request body; falls back to
  request-supplied IP ranges when site has no network config.
- Cross-collection enforcement: pair / reassign rejects if the
  device's target site doesn't equal its rack's building's site.
- Minimal `/settings/sites` page rendering per-site sections (details
  card + power contract card + buildings card) for both single-site
  and "All Sites" modes. Inline edit modals.
- Site column + site filter chip in Miner List. Bulk
  "Reassign miners" action.
- Activity-log rows on every site CRUD, building CRUD, and
  reassignment.

Acceptance: Block ops walks through the full reorganize-3+-sites
workflow in <30 minutes from `/settings/sites` alone, no engineer
help.

### Phase 2 — topbar, onboarding, upgrade banner

Goal: every page is site-aware, new orgs configure sites at first
run, existing orgs see a one-time prompt.

- Topbar SitePicker replaces the `LocationSelector` placeholder.
  localStorage-backed active-site selection.
- "All Sites" mode wired through every list/read page (miner list,
  errors, activity, dashboards, etc.). Reads aggregate; writes
  prompt for site when ambiguous.
- Onboarding "Name your sites" step + `SiteConfigured` gate (≥1
  site with name + location + timezone).
- Migration banner UI keyed off `migration_banner_dismissed_at`.
- "Choose target site" modal step in Add Miners when "All Sites"
  is active.
- Saved views: site filter included in the existing serialization;
  pre-existing saved views remain valid.
- Polish: multi-select on bulk reassign, undo, batch progress.

Acceptance: a new org completes onboarding with ≥1 named site
before pairing any miner; an existing org sees the banner exactly
once and can dismiss it.

### Phase 3 — site energy statistics

Goal: surface the energy data captured in the site config (power
capacity, contract terms, demand charges, etc.) as dashboards and
operational signals. Not blocking the multi-site basics, so deferred
until the foundation is in place.

Scope detailed in a follow-on plan; out of scope for this doc.

### Phase 4 — agent handoff / cleanup

Goal: align with the agent workstream and clean up deferred items.

- Coordinate with the agent team on whatever discriminator and
  agent-side schema they need; we add the columns/tables when they
  commit to a shape. Site `network_config` may be removed at this
  point if agents become the only data plane.
- Audit per-site config candidates (pools especially) and decide
  with mining ops whether any need to move from org-scoped to
  site-scoped before the cloud transition.
- Revisit the multi-install consolidation question (currently
  punted) if any customer asks.
- Drop the `device_set_rack.zone` column once writer audit confirms
  no callers remain.

## Open questions to resolve in the technical plan

These are intentionally not answered here — they need code-level review
before they're locked.

1. Final shape of the site `network_config` field: a multiline string
   of newline-separated CIDRs/IPs is the working assumption; confirm
   when wiring up the discovery handler.
2. "All Sites" scan mode that fans out across every site's IP ranges
   and segments findings by source range, auto-assigning at pair time.
   Captured here so we don't lose it; lower priority than the
   explicit-target flow.
3. Whether to drop `device_set_rack.zone` in the same migration that
   adds buildings, or in a Phase-4 follow-up after the writer audit.
4. Building deletion edge cases (racks present, last-building-in-site
   rules).
5. Power-contract enum coverage gaps as customers onboard — e.g.
   utility-list completeness for a region we haven't seen yet.

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
