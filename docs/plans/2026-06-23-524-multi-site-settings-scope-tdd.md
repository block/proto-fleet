---
title: "Multi-site: audit and scope site-aware Settings pages"
date: 2026-06-23
status: implementing
type: tdd
tracker: https://github.com/block/proto-fleet/issues/524
---

# Multi-site: audit and scope site-aware Settings pages

## Context

PR [#516](https://github.com/block/proto-fleet/pull/516) added path-based
site-scope routing for the primary sections
(`/{siteScope}/{dashboard,fleet,groups,energy,activity}`) and deliberately
left **Settings** routes unscoped (`/settings/...`). Settings is unusual:
some subpages are org-wide by design (general, security, team, roles,
api-keys, firmware, pools, notifications, server-logs) while two
(schedules, curtailment) select or mutate site-local resources and were
built before multi-site landed.

Before the SitePicker flag (`VITE_MULTI_SITE_ENABLED`,
[`constants/featureFlags.ts`](../../client/src/protoFleet/constants/featureFlags.ts))
goes broad, every settings subpage needs an explicit decision: honor the
selected site as a default/filter, or stay deliberately all-sites without
implying the picker applies.

The reusable site-scope toolkit from #516 is the foundation here:

- `useActiveSite({ knownSiteIds })`
  ([`components/PageHeader/SitePicker/useActiveSite.ts`](../../client/src/protoFleet/components/PageHeader/SitePicker/useActiveSite.ts))
  — resolves the active site from URL ∪ store, healing stale selections.
- `siteFilterFromActive(activeSite)` → `{ siteIds, includeUnassigned, matchNone }`
  ([`components/PageHeader/SitePicker/siteFilter.ts`](../../client/src/protoFleet/components/PageHeader/SitePicker/siteFilter.ts))
  — the additive filter shape ListRacks/ListGroups/ListMiners already take.
- `scopedPath(to, activeSite)`
  ([`routing/siteScope.tsx`](../../client/src/protoFleet/routing/siteScope.tsx))
  — preserves scope on outbound navigation.
- `ActiveSite` store slice, persisted org-wide to `proto-fleet-multi-site`
  ([`store/types/activeSite.ts`](../../client/src/protoFleet/store/types/activeSite.ts),
  [`store/useFleetStore.ts`](../../client/src/protoFleet/store/useFleetStore.ts)).

Dependency: [#520](https://github.com/block/proto-fleet/issues/520) adds
`site_ids` / `include_unassigned` to `ListGroups` and `ListGroupMembers`
(client hook `useDeviceSets().listGroups({ siteIds, includeUnassigned })`).
The Schedules **group** modal is gated on #520 reaching `main`; everything
else in this plan builds on `main` today.

## The core design decision: store-driven, not URL-scoped

**Settings routes stay unscoped (`/settings/...`). Site-awareness is driven
by the store (`useActiveSite`), not a new URL segment.** Rejected
alternatives:

- **Per-child middle segment (`/settings/{siteId}/logs`)** — introduces a
  _second_ positional convention. The #516 toolkit (`scopedPath`,
  `unscopedScopablePath`, `segmentFromActiveSite`, `SCOPABLE_ROOT_SEGMENTS`)
  assumes site scope is the **leading** segment. A middle segment needs a
  parallel parser/guard/stale-heal stack for one feature. Rejected.
- **Settings as a scopable root (`/{siteId}/settings/...`)** — reuses the
  toolkit by adding `"settings"` to `SCOPABLE_ROOT_SEGMENTS`, but
  _over-applies_ scope: `/{siteId}/settings/team` implies team is
  site-filtered when it is org-wide. Settings mixes org-wide and site-aware
  children under one root, so a root-level scope is semantically wrong for
  the majority of the tree. Rejected.

The store is the canonical carrier of the selection and already persists
across navigation, so settings does not need scope in its own URL. This
matches the issue's framing ("use the stored SitePicker selection as a
default/filter") and its escape-hatch concern ("preserve selected site when
navigating from settings back to primary scoped pages") — handled by
`scopedPath` on outbound links, not by scoping settings itself.

**Filter vs. default (locked):** site-aware settings use the active site as
a **soft default filter**, not a hard lock. "All sites" (`{kind:"all"}`)
passes the empty filter and shows everything (today's behavior — no
regression). A single selected site pre-filters the lists/modals, but the
user is not prevented from acting across sites where the workflow legitimately
spans them (Curtailment already models this with its
`scopeType: "site" | "wholeOrg"` toggle). If later we want a shareable
site-filtered settings view, add a `?site=` query param (not a path
segment) and intersect via `intersectSiteFilters` — out of scope here.

## Per-page classification and decisions

| Route                     | Component                                 | Decision               | Work                                                                                         |
| ------------------------- | ----------------------------------------- | ---------------------- | -------------------------------------------------------------------------------------------- |
| `/settings/general`       | `General.tsx`                             | **Org-wide**           | Label only                                                                                   |
| `/settings/security`      | `Auth.tsx`                                | **Org-wide**           | Label only                                                                                   |
| `/settings/team`          | `Team.tsx`                                | **Org-wide**           | Label only                                                                                   |
| `/settings/roles`         | `Roles.tsx`                               | **Org-wide**           | Label only                                                                                   |
| `/settings/api-keys`      | `ApiKeys.tsx`                             | **Org-wide**           | Label only                                                                                   |
| `/settings/firmware`      | `Firmware.tsx`                            | **Org-wide**           | Label only                                                                                   |
| `/settings/mining-pools`  | `MiningPools.tsx`                         | **Org-wide**           | Label only (pool _defs_ are an org catalog; per-miner assignment happens on Fleet, not here) |
| `/settings/notifications` | `Notifications.tsx`                       | **Org-wide** (today)   | Label only; note maintenance-window/rule site-targeting as a possible follow-up              |
| `/settings/server-logs`   | `ServerLogsPage.tsx`                      | **Org-wide**           | Label only (see audit below — no site-attributable data)                                     |
| `/settings/schedules`     | `Schedules/SchedulesPage.tsx`             | **Site-aware**         | Enrich (§Schedules)                                                                          |
| `/settings/curtailment`   | `Curtailment/CurtailmentSettingsPage.tsx` | **Org-wide (for now)** | Deferred — see §Curtailment                                                                  |

### Server-logs audit (why org-wide)

Server logs carry **no site-attributable structured data**, so they stay
org-wide and the picker explicitly does not apply:

- `LogEntry` ([`proto/serverlog/v1/serverlog.proto`](../../proto/serverlog/v1/serverlog.proto))
  is `id / time / level / message / attrs / source`; `ListServerLogsRequest`
  is `min_level / search_text / since_id / limit`. No `site_id`/`device_id`.
- The buffer is an in-memory ring (`logging/buffer.go`), not persisted; the
  read permission is org-level (handler passes empty `ResourceContext{}`,
  `handlers/serverlog/handler.go`).
- Device/site context appears only as free-text `slog` attributes
  (e.g. `"device_id", deviceID`), substring-searchable but not a structured
  scope. Site filtering would need proto + write-path + permission + persist
  changes for an ephemeral debug tail — not worth it.
- If "what happened at site X" is the real need, that is **Activity**
  (`ListActivitiesRequest` already has `site_ids`, see #522), not server-logs.

## Goals

- Every settings subpage has an explicit, documented multi-site decision.
- Org-wide pages ignore the picker deliberately and **do not imply** they
  are filtered by it (consistent affordance).
- Schedules and Curtailment use the active site as a soft default filter for
  rack/group/miner selection and scope, with **no change to all-sites
  behavior**.
- Modals launched from settings (no URL state) read the active site from the
  store and pass the site filter into their list queries.
- Outbound "escape-hatch" links from settings preserve the active scope via
  `scopedPath`.
- Tests cover Schedules rack selection and Curtailment scope behavior
  (acceptance criteria), plus the all-sites regression.

## Non-goals

- No new URL scope segment for any settings route.
- No `?site=` deep-link param on settings (possible follow-up).
- No server-logs / notifications / pools site filtering (org-wide by decision).
- No backend schema or permission changes for the org-wide pages.
- No hard site-locking — selection is a default, not a constraint.

## Design

### Shared: org-wide affordance

Add a small, consistent indicator on org-wide settings pages so an operator
with a single site selected understands the picker does not filter here.
Options to settle in design review (pick one, apply uniformly):

- a header chip/subtext ("Org-wide · applies to all sites"), or
- visibly disabling/greying the SitePicker while on an org-wide settings
  route (driven off the route, leaving the stored selection intact).

Recommendation: header subtext — zero interaction risk, no change to the
shared `PageHeader` picker state. Implement once as a shared
`<OrgWideNotice />` (or a prop on the settings page header) and drop it on
the nine org-wide pages.

### Schedules (site-aware enrichment)

`SchedulesPage.tsx` + `ScheduleModal.tsx` and its three selection modals
were built pre-multi-site and fetch globally. Enrich:

1. **Read scope.** In `SchedulesPage` / `ScheduleModal`, obtain
   `knownSiteIds` (from a sites fetch or outlet context),
   `const { activeSite } = useActiveSite({ knownSiteIds })`, and
   `const scope = useMemo(() => siteFilterFromActive(activeSite), [activeSite])`.
2. **`RackSelectionModal`** ([`Schedules/RackSelectionModal.tsx`](../../client/src/protoFleet/features/settings/components/Schedules/RackSelectionModal.tsx))
   — pass `scope.siteIds` / `scope.includeUnassigned` into the existing
   `listRacks({ ... })` call (line ~28). `listRacks` already accepts these.
   Add `scope` to the effect deps.
3. **`MinerSelectionModal` / `MinerSelectionList`**
   ([`components/MinerSelectionList.tsx`](../../client/src/protoFleet/components/MinerSelectionList.tsx))
   — thread the filter into `useFleet()` and into its internal
   `listRacks({})` / `listGroups({})` filter-option queries so the facet
   options and the miner list both scope.
4. **`GroupSelectionModal`** ([`Schedules/GroupSelectionModal.tsx`](../../client/src/protoFleet/features/settings/components/Schedules/GroupSelectionModal.tsx))
   — **gated on #520.** Once `listGroups` accepts `{ siteIds,
includeUnassigned }`, pass `scope` through (line ~29). **UX note (from
   #520 proto):** site-filtering groups returns groups that _have a member
   at the site_, but group counts/rollups stay org-wide — if the modal shows
   a per-group count, either scope it via `listGroupMembers({ siteIds })` or
   label it org-wide. Decide explicitly.
5. **Schedule list itself.** `listSchedules()` takes no site param and
   schedules target rack/group/miner sets, not sites. Decision: **leave the
   schedule _list_ org-wide for now**; only the selection of targets is
   scoped. A schedule may legitimately span sites via its chosen sets, so a
   soft default on selection (not a hard filter on the list) is correct.
   Document this; revisit if product wants per-site schedule lists.

### Curtailment (deferred to #521)

> **Update (2026-06-23):** Curtailment site-scoping was attempted, then pulled
> from this work and **deferred to [#521](https://github.com/block/proto-fleet/issues/521)**
> (Energy + curtailment flows, which are intrinsically linked). It proved inert
> in the real UI: `CurtailmentStartModal.withResponseProfileScope` forces
> whole-org scope on newly-created response profiles (site is kept only in edit
> mode), and the modal has **no site-picker control** to set or show a site
> scope — that UI is tracked in [#425](https://github.com/block/proto-fleet/issues/425).
> The settings curtailment page therefore stays **org-wide** here (gets the
> shared OrgWideNotice like the other org-wide tabs). The original analysis
> below is retained for context.

`CurtailmentSettingsPage.tsx` already models site scope partially:
`ResponseProfileFormValues` carries `siteId` / `siteName`, and response
profiles derive `scopeType: "site" | "wholeOrg"` (lines ~155-157, ~362-364).
The gap is that the device/rack selection underneath doesn't enforce the
chosen site, and the scope isn't defaulted from the picker.

1. **Default the scope from the picker.** When opening the
   create/edit response-profile flow with a single site selected, default
   `scopeType: "site"` and prefill `siteId`/`siteName` from `activeSite`
   (still user-overridable to `wholeOrg`). With "all sites" selected, keep
   today's `wholeOrg` default.
2. **Scope device/rack/group selection** inside the curtailment targeting
   flow (`CurtailmentStartModal` and any rack/group/miner pickers it opens)
   to the profile's `siteId` — pass the site filter into the same
   `listRacks` / `listGroups` (#520) / `useFleet` queries.
3. **Site source.** `siteId` is currently a string on the form; confirm how
   the site is chosen (audit the form field during implementation) and feed
   the picker's selection in as the default. Reuse the `ListSites` lookup the
   SitePicker already uses for name resolution.
4. **Keep the existing `scopedPath("/energy", activeSite)` escape hatch**
   (line ~1668) — already correct; verify other outbound links match.

### Outbound navigation audit

Grep settings for bare `navigate("/fleet")` / `navigate("/energy")` /
`<Link to="/...">` to scopable roots and wrap with
`scopedPath(to, activeSite)`. Curtailment's energy link already does this;
confirm Schedules and any "view in fleet" affordances follow.

## Test plan

**Schedules (acceptance criterion):**

- `RackSelectionModal` with "all sites" → `listRacks` called with empty
  `siteIds`, `includeUnassigned=false` (regression: shows all racks).
- with a single site selected → `listRacks` called with that `siteId`;
  list shows only that site's racks; stale pre-selected rack ids pruned.
- `MinerSelectionList` facet options + miner list both scope to the site.
- `GroupSelectionModal` (post-#520) → `listGroups` receives the site filter;
  group-count UX behaves per the decided rule.

**Curtailment (acceptance criterion):**

- opening a new response profile with a site selected defaults
  `scopeType:"site"` + prefilled `siteId`; with "all sites" → `wholeOrg`.
- device/rack selection in the curtailment flow is constrained to the
  profile's site.
- editing an existing `wholeOrg` profile does not get silently re-scoped.

**Org-wide pages:**

- the org-wide affordance renders on all nine pages; the picker selection
  does not change their data (no site param sent — most send none today, so
  assert no regression).

**e2e** (`proto-fleet-playwright-e2e` skill):

- select a site → open `/settings/schedules` → rack modal lists only that
  site's racks; switch to "all sites" → all racks return.
- navigate from a settings escape-hatch link → lands on the scoped primary
  route (`/{siteId}/...`).

## Risks and mitigations

- **Hard-filter trap.** Over-scoping selection would block legitimate
  cross-site schedules/curtailments. Mitigation: soft default only; "all
  sites" always shows everything; covered by the all-sites regression tests.
- **#520 coupling.** Group modal depends on `ListGroups` site filtering.
  Mitigation: land rack + miner scoping independently; gate the group modal
  on #520 merging to `main`.
- **Group count vs. membership skew (#520 semantics).** Org-wide group
  counts under a site filter can mislead. Mitigation: explicit decision in
  the Schedules group-modal section; test it.
- **Picker implies filtering on org-wide pages.** The exact failure the
  issue warns against. Mitigation: shared org-wide affordance applied
  uniformly to all nine pages; reviewed in design review.
- **Curtailment partial model drift.** The form's string `siteId` and the
  derived `scopeType` must stay consistent when defaulted from the store.
  Mitigation: default through the existing normalize helpers (lines
  ~358-408), not by mutating form fields directly; unit-test the mapping.

## Open questions

1. Org-wide affordance: header subtext vs. disabled picker? (design review)
2. Schedules list: confirm product wants the schedule _list_ to stay
   org-wide while only target selection is scoped.
3. Curtailment site field: confirm the current site-selection UX so the
   picker default wires into the real input.
