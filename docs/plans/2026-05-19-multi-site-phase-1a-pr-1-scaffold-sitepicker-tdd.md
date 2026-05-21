---
title: "Multi-site Phase 1a PR 1: scaffold + SitePicker"
date: 2026-05-19
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/198
---

# Multi-site Phase 1a PR 1: scaffold + SitePicker

## Context

The multi-site backend foundation is in place: `SiteService` and
`BuildingService` Connect-RPC shipped in #196; the schema, snapshots,
and rack-cascade invariants shipped in #195 / #197. Generated proto
clients exist under
`client/src/protoFleet/api/generated/sites/v1/sites_pb.ts` and
`client/src/protoFleet/api/generated/buildings/v1/buildings_pb.ts`,
but no UI consumes them yet.

This PR delivers the **navigation scaffold** for the multi-site
frontend per the scaffold-first phasing in
[`docs/plans/2026-05-05-multi-site-support-plan.md`](./2026-05-05-multi-site-support-plan.md).
It registers three new routes (`/sites`, `/settings/sites`,
`/buildings/:id`), mounts a global topbar SitePicker that replaces
today's `LocationSelector` placeholder, gates the new sidenav +
settings subnav entries behind a Vite-time feature flag, and ships
read-only API hooks (`useSites` / `useBuildings`). All page bodies
are placeholder blocks; site and building CRUD modals land in #261
and #262.

The picker mounts globally even though existing pages (`/miners`,
`/racks`, dashboards) do **not** yet consume the active-site
selection — that wiring lives in #202 and #265. The PR description
will note this transitional state explicitly so reviewers and
operators understand the picker is informational on those pages
until 1b lands.

The scaffold is the foundation everything else in Phase 1 layers on:
- #261 fills in the CRUD modals reached from this scaffold's CTAs.
- #262 fills in building CRUD inside those modals.
- #263 / #264 replace the placeholder blocks on `/sites` and
  `/buildings/:id` with real metrics + diagnostics.
- #202 wires the existing miner list + rack list to consume the
  active site selection (and depends on #265 for the rack-list BE
  filter).

## Goals

- Three new routes registered and reachable: `/sites`,
  `/settings/sites`, `/buildings/:id`. Each renders the shell
  structure described in master plan J3 / J8 / J9 with FPO
  placeholder content where Phase 1b enrichment will land.
- Topbar `SitePicker` component replaces today's `LocationSelector`
  in `PageHeader`. Mounts globally — visible on every page. The
  three new routes consume the selection in this PR; existing
  routes (`/miners`, `/racks`, dashboards) continue to ignore it
  until #202.
- Active-site selection persists in `localStorage`, keyed by
  username, with three discriminated states: `all`, a specific
  `site` (by ID), or `unassigned`.
- Vite-time feature flag (`VITE_MULTI_SITE_ENABLED`) hides the
  new sidenav + settings subnav entries **and** the SitePicker
  itself when off. Routes themselves remain reachable by direct
  URL for QA/dogfood; in that direct-URL state the picker stays
  hidden and the new pages render in default ("All Sites") mode.
- `useSites` and `useBuildings` read-only API hooks wrap the
  generated `SiteService` / `BuildingService` clients in the same
  callback shape used by `useDeviceSets`.
- `/settings/sites` All Sites flat table renders accurate
  attachment counts from `ListSites` response (device, building,
  rack counts).
- `/sites` BuildingCards render real building label + rack
  count + miner count placeholders (FPO implementation noted in
  source), link to `/buildings/:id`.
- `/buildings/:id` header + button row functional (View racks
  links to `/racks?building_id=...`, View miners links to
  `/miners?building_id=...`, Edit building is a stub for #262/#264).

## Non-goals

- Site / building **create, edit, delete** flows — all CRUD lands in
  #261 (sites) and #262 (buildings).
- Real metric components, BuildingCard component, diagnostics, or
  performance sections — Phase 1b (#263, #264).
- Active-site **consumption** on existing pages — the picker is
  mounted globally in this PR, but `/miners`, `/racks`, errors,
  activity, telemetry, and dashboards continue to render org-wide
  results regardless of the selection. Wiring those reads to the
  active site lives in #202 (miner+rack) and Phase 2 (history
  pages).
- Rack-list site filter on `ListDeviceSetsRequest` — #265.
- Power-contract UI — deferred per master plan.
- Unassigned-buildings UI — non-goal per master plan.
- Single-site rendering mode driven by URL deep-link
  (`/sites/${id}`) — Phase 1b enhancement; in this PR the
  single-site view is reached purely via the SitePicker selection.
- Address / type / IP-range form inputs whose backing BE columns
  are deferred (#266, #267, #268). The single-site Details table
  hides those rows when their fields are absent.

## Design

### File map

New files:

- `client/src/protoFleet/api/sites.ts` — `sitesClient` Connect client + `useSites` hook.
- `client/src/protoFleet/api/buildings.ts` — `buildingsClient` Connect client + `useBuildings` hook.
- `client/src/protoFleet/features/sites/`
  - `pages/SitesPage.tsx` — `/sites` overview shell.
  - `pages/SettingsSitesPage.tsx` — `/settings/sites` config shell.
  - `components/SitesAllTable.tsx` — flat all-sites table.
  - `components/SiteOverviewSection.tsx` — single per-site section (used by `SitesPage`).
  - `components/SiteSettingsSingleView.tsx` — single-site `/settings/sites` body.
  - `components/SitesEmptyState.tsx` — shared empty-state CTA.
  - `components/PlaceholderBlock.tsx` — shared FPO grey-box primitive (this PR only — Phase 1b replaces individual usages with real components).
  - `components/SitesPageHeader.tsx` — page header w/ headline, sub, "Add a site" CTA.
- `client/src/protoFleet/features/buildings/`
  - `pages/BuildingPage.tsx` — `/buildings/:id` shell.
  - `components/BuildingPageHeader.tsx` — header + button row.
  - `components/BuildingCard.tsx` — the BuildingCard component used on `/sites`. Phase 1a ships an FPO implementation (grey box + label + rack/miner counts + link to `/buildings/:id`); the file lives at its final path so #263 replaces the body in-place rather than swapping imports across the tree. A top-of-file `// FPO implementation — real visuals + metrics arrive in #263.` comment makes the state explicit.
- `client/src/protoFleet/components/PageHeader/SitePicker/`
  - `SitePicker.tsx` — popover with All Sites / each site / Unassigned.
  - `SitePicker.test.tsx`.
  - `useActiveSite.ts` — localStorage-backed state hook returning the active selection and a setter.
  - `useActiveSite.test.ts`.
  - `index.ts` — re-exports.
- `client/src/protoFleet/constants/featureFlags.ts` — central exports for `MULTI_SITE_ENABLED`, future flags.

Touched files:

- `client/src/protoFleet/api/clients.ts` — add `sitesClient`, `buildingsClient`.
- `client/src/protoFleet/components/PageHeader/PageHeader.tsx` — replace `LocationSelector` with `SitePicker` when the feature flag is on. When the flag is off, keep `LocationSelector` for now (it stays the placeholder until the feature ships to operators).
- `client/src/protoFleet/config/navItems.ts` — add `/sites` to `primaryNavItems` and `/settings/sites` to `secondaryNavItems`, each filtered out at module export time when the flag is off.
- `client/src/protoFleet/router.tsx` — register the three new routes (`/sites`, `/settings/sites`, `/buildings/:id`). Routes are unconditional — the feature flag only hides the nav buttons.
- `client/src/protoFleet/routePrefetch.ts` — add lazy-load entries for the new pages so the existing prefetch behavior covers them.

### Feature flag

Single Vite env var, parsed once:

```ts
// constants/featureFlags.ts
export const MULTI_SITE_ENABLED = import.meta.env.VITE_MULTI_SITE_ENABLED === "true";
```

Conventions:

- Default is **off** — any value other than the literal string `"true"` resolves to `false`. Production builds without the env var stay safe.
- Consumed only at:
  - `navItems.ts` (sidenav + subnav filtering at module export).
  - `PageHeader.tsx` (deciding whether to render `SitePicker` or fall back to today's `LocationSelector`).
- Routes themselves are **not** flag-guarded. An operator who knows the URL can navigate; this preserves QA + dogfood paths. The flag is purely a UI-discovery gate (and a safety net for the SitePicker, which the new pages tolerate being absent — they fall back to `{ kind: "all" }`).
- The flag is intentionally read once at module load (no React hook). React re-renders don't change the env. This matches `VITE_POLL_INTERVAL_MS` already in the repo.

### Active-site state

State shape — discriminated union, serialized as JSON:

```ts
type ActiveSite =
  | { kind: "all" }
  | { kind: "site"; id: string } // string, not bigint — see "BigInt handling" risk
  | { kind: "unassigned" };
```

Storage:

- Hook: `useActiveSite()` wraps the existing `useReactiveLocalStorage<ActiveSite>` (`client/src/shared/hooks/useReactiveLocalStorage.ts`). Same pattern as `completeSetupDismissed` already in `PageHeader`.
- Key shape: `multiSite.activeSite:${username}`. Username comes from the existing auth store (`@/protoFleet/store`); when auth has not resolved, the hook returns `{ kind: "all" }` as a transient default and does not persist.
- Default-after-first-login rule (from master plan J2):
  - User has access to **multiple** sites → `{ kind: "all" }`.
  - User has access to **exactly one** site → `{ kind: "site", id }` for that site.
  - User has access to **zero** sites → SitePicker hidden; selection is irrelevant.
- This default is applied **once**, on first read when no stored value exists. After the first selection, the stored value wins.

Validation on read:

- If the stored selection is `{ kind: "site", id }` and that ID is not present in the latest `ListSites` response (deleted, reassigned, or user lost access), the hook **falls back** to `{ kind: "all" }` and overwrites the stored value. This prevents the picker from getting stuck pointing at a tombstoned site.

### SitePicker component

API:

```ts
interface SitePickerProps {
  // Sites known to the caller. When undefined the picker shows a skeleton.
  sites: Site[] | undefined;
  loading: boolean;
}
```

The component is dumb-ish — it reads `useActiveSite()` internally and renders the popover. Sites are passed in from `PageHeader` (which holds the `ListSites` query), so the picker doesn't double-fire the request when multiple routes mount it.

Rendering rules:

- `sites` empty + not loading → picker hidden (returns `null`).
- `sites` non-empty + not loading → button labeled with current selection:
  - `{ kind: "all" }` → "All Sites".
  - `{ kind: "site", id }` → site label (lookup in `sites`); when the lookup fails the hook will have already reset to `all`.
  - `{ kind: "unassigned" }` → "Unassigned".
- Click opens a popover with: "All Sites" → each site (sorted by `site.name` ascending) → "Unassigned" at the bottom.
- Selection writes through `useActiveSite()`; popover closes.

This PR does **not** wire the SitePicker as a data filter on any list query — only the new routes read it (and they re-fetch `ListSites` on every render anyway for the placeholder content). #202 brings in the broader reactivity.

### PageHeader integration

`PageHeader.tsx` already uses `useReactiveLocalStorage`. Add:

```ts
const { listSites, sites, isLoading } = useSites();
useEffect(() => {
  if (MULTI_SITE_ENABLED) listSites();
}, [listSites]);

return (
  // ...
  <div className="flex grow items-center">
    {/* existing menu button */}
    {MULTI_SITE_ENABLED
      ? <SitePicker sites={sites} loading={isLoading} />
      : <LocationSelector />}
  </div>
  // ...
);
```

The picker fires `listSites` once on mount of `PageHeader` (which
is mounted at the app-shell level for every protoFleet route). The
response is cached in component state inside `useSites`; no
polling. When the flag is off, the existing `LocationSelector`
placeholder continues to render unchanged.

Pages outside the multi-site set (`/miners`, `/racks`, etc.)
see the picker, but their data queries do not yet consume the
selection — see Risk R3 and the PR-description note.

### useSites / useBuildings hooks

Mirror the existing `useDeviceSets` shape (`client/src/protoFleet/api/useDeviceSets.ts`). They are imperative callback hooks, not TanStack Query wrappers — that is the project pattern.

```ts
// api/sites.ts (sketch)
export const useSites = () => {
  const { setAuthErrors } = useAuthErrors();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>();
  const [isLoading, setLoading] = useState(false);

  const listSites = useCallback(async (props?: {
    onSuccess?: (sites: SiteWithCounts[]) => void;
    onError?: (message: string) => void;
    onFinally?: () => void;
  }) => {
    setLoading(true);
    try {
      const response = await sitesClient.listSites({});
      setSites(response.sites);
      props?.onSuccess?.(response.sites);
    } catch (err) {
      const message = getErrorMessage(err);
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        setAuthErrors(err);
      }
      props?.onError?.(message);
    } finally {
      setLoading(false);
      props?.onFinally?.();
    }
  }, [setAuthErrors]);

  return { listSites, sites, isLoading };
};
```

`useBuildings` is structurally identical, wrapping `buildingsClient.listBuildings({ siteId })`.

In this PR neither hook exposes any mutation — only `listSites` and
`listBuildings`. CRUD methods land alongside the modals in #261 / #262.

### Page shells

Each page renders header + placeholder body content. No mocks, no
made-up data — placeholders are literal grey boxes labeled with what
the real content will be.

**`/sites` — `SitesPage`**

```
<SitesPageHeader headline="Sites" sub="..." onAddSite={openSiteModal /* TODO #261 */} />
{state.kind === "empty"           ? <SitesEmptyState /> :
 state.kind === "single"          ? <SiteOverviewSection site={...} /> :
                                    sites.map(s => <SiteOverviewSection site={s} key={s.id} />)}
```

`SiteOverviewSection` renders a metrics-row `PlaceholderBlock` and a row of `BuildingCard`s linking to `/buildings/:id`. The cards ship as the FPO implementation in this PR (grey box + label + counts); the component file lives at its final path so #263 replaces the body without import churn. Real metrics arrive in #263.

**`/settings/sites` — `SettingsSitesPage`**

Wrapped in `SettingsLayout` for the existing subnav.

```
{state.kind === "empty"  ? <SitesEmptyState onAddSite={...} /> :
 state.kind === "single" ? <SiteSettingsSingleView site={...} /> :
                           <SitesAllTable sites={...} />}
```

`SitesAllTable` renders three columns (Site / Infrastructure /
Power+Efficiency) with `site.name` / city,state / building+miner
counts / power+efficiency placeholders. Rows ordered by
`site.name` ascending; not user-sortable. Row click navigates to
the single-site view via the SitePicker setter.

`SiteSettingsSingleView` renders the button row (`< All sites` +
`Manage site` stub), header (label + address subline), a Details
table (Power, PUE, Timezone, Gateway, Notes — rows hidden when
backing field absent), and a Buildings table populated by
`ListBuildings({ siteId })`.

**`/buildings/:id` — `BuildingPage`**

```
<BuildingPageHeader
  building={...}
  onEditBuilding={openManageBuildingModalStub /* TODO #262 / #264 */}
/>
<PlaceholderBlock label="Metrics row" />
<PlaceholderBlock label="Diagnostics — rack grid" />
<PlaceholderBlock label="Performance" />
```

The "View racks" and "View miners" buttons link to
`/racks?building_id={id}` and `/miners?building_id={id}` — those
existing pages already accept a building filter via #229, so the
links work today.

### Routing

In `router.tsx`, add (alongside the existing `/settings/*` block):

```tsx
createRoute("/sites", <SitesPage />),
createRoute("/buildings/:id", <BuildingPage />),
createRoute(
  "/settings/sites",
  <SettingsLayout>
    <SettingsSitesPage />
  </SettingsLayout>,
),
```

Lazy-load entries are added to `routePrefetch.ts` so the existing
prefetch-at-idle behavior covers the new pages without a separate
warming pass.

### Navigation entries

In `navItems.ts`:

```ts
const sitesNavItem: NavItem = { path: "/sites", label: "Sites", icon: Location /* or similar */ };
const settingsSitesNavItem: SecondaryNavItem = { path: "/settings/sites", label: "Sites", parent: "/settings" };

export const primaryNavItems: NavItem[] = [
  ...baseItems,
  ...(MULTI_SITE_ENABLED ? [sitesNavItem] : []),
];

export const secondaryNavItems: SecondaryNavItem[] = [
  ...baseSettingsItems,
  ...(MULTI_SITE_ENABLED ? [settingsSitesNavItem] : []),
];
```

Picking an icon: the existing icon registry under
`@/shared/assets/icons` will need a "site" or "location" icon —
borrow from an existing pin/location glyph if one exists; otherwise
add a minimal SVG in this PR. Inventory the icon set as part of
implementation; this TDD doesn't lock in the visual.

Placement in the primary nav order: between "Racks" and "Groups"
keeps the spatial → logical hierarchy. Final placement is a small
visual review during PR.

## Alternatives considered

**A1. Mount SitePicker only on the three new routes.**
Considered (and adopted as "Option D" in the master plan
discussion), then reverted. The route-scoped approach avoids the
appearance of a half-wired filter on the miner list / rack list,
but it adds matching logic to `PageHeader`, splits selection
behavior across the app, and means the picker disappears when
the operator clicks "View miners" from `/buildings/:id` —
exactly the moment they'd want context to follow them. We
preferred the simpler global mount + a clear transitional-state
note in the PR description over the route-scoping complexity.

**A2. URL params (`?site=12`) instead of localStorage.**
Rejected because the user expects the picked site to survive
cross-route navigation. URL params would force every nav element to
forward the param explicitly, and a freshly-opened tab would start
in "All Sites". localStorage matches the existing saved-views and
`completeSetupDismissed` patterns.

**A3. Wrap the API hooks in TanStack Query.**
Rejected for scope. The project does not use TanStack Query
anywhere today; introducing it in a scaffold PR creates a much
bigger pattern question. The imperative `useDeviceSets`-style hook
already covers the read-only needs, and CRUD wiring in #261 can
follow the same shape. If we later move to TanStack Query, this is
a localized refactor.

**A4. Defer SitePicker entirely to 1b.**
Rejected. The picker drives the scaffold demo — without it, an
operator on `/sites` can't switch between sites except by
navigating away. Building modals (#261) also benefit from a
working picker for "single site selected → defaults populated"
behavior.

**A5. Feature-flag the routes themselves (return 404 when off).**
Rejected. QA + dogfood need direct-URL access; the nav-button-only
gate is enough to prevent accidental operator discovery in
production builds. Route-level guards would also complicate the
React Router lazy chunks.

## Risks

**R1. BigInt serialization in localStorage.**
Generated proto site IDs are `bigint` (`Site.id: bigint`). JSON
cannot serialize bigint natively. **Mitigation:** the
`ActiveSite.site` variant stores `id` as a **string** (decimal
representation), and the picker converts to `bigint` only when
comparing against a `Site.id`. The conversion is centralized in
`useActiveSite`; downstream consumers never see the string form.

**R2. Active site pointing at a deleted site.**
If a site is deleted in another tab or by another operator while
the user has it selected, the next render will not find it in
`ListSites`. **Mitigation:** `useActiveSite` validates the stored
ID against the latest `sites` array on every read and falls back
to `{ kind: "all" }` (overwriting storage) when the ID is missing.
A toast / soft notice is out of scope for this PR; #202 may add
one as part of the global rollout.

**R3. Username unavailable on first render.**
Auth resolves asynchronously. **Mitigation:** the SitePicker is
rendered by `PageHeader`, which already lives inside the
auth-resolved app shell — so by the time the picker mounts, the
username is available. If a race surfaces during integration,
the hook returns `{ kind: "all" }` and skips writes until the
username is non-empty.

**R4. Picker mounted globally but only the new pages consume it.**
On `/miners`, `/racks`, errors, activity, telemetry, and
dashboards, the picker is interactive but selection has no effect
on the rendered data. **Mitigation:** the PR description calls
this out explicitly, and the picker tooltip / accessible label
includes a "Phase 1: filters only the new Sites pages" note.
#202 closes the gap for miner+rack lists; history-page consumption
is Phase 2. We accept the transitional UX because the alternative
(route-scoping the picker) adds matching logic and breaks
selection continuity when the operator navigates from
`/buildings/:id` to `/racks?building_id=...`.

**R5. Feature-flag drift between environments.**
Vite env vars bake into the build. **Mitigation:** the flag is
read once and exported from `constants/featureFlags.ts`; CI build
configuration owns the value (we will document the env var in the
build README as part of this PR). The default-off behavior means
forgetting the flag is the safer failure mode.

**R6. `ListSites` request cost.**
`PageHeader` is mounted at the app-shell level for every protoFleet
route, so the SitePicker fires `listSites` once on every full app
load (and once on every PageHeader remount). **Mitigation:** the
response is cached in component state inside `useSites` and only
re-fetched on explicit `listSites()` calls. There is no polling.
The orgs we're targeting in Phase 1 have at most low double-digit
site counts, so a single response is cheap. If staleness becomes
visible (e.g. a sibling tab creates a site and this tab doesn't
notice), #202 is the right place to introduce
stale-while-revalidate.

**R7. Icon for `/sites` not yet in registry.**
**Mitigation:** add a minimal location-pin SVG to
`@/shared/assets/icons` as part of this PR. Visual polish is
handled in PR review.

**R8. Direct-URL access to `/buildings/:id` with an unknown ID.**
**Mitigation:** `BuildingPage` calls `buildingsClient.getBuilding`
on mount; a not-found response renders an empty-state shell ("Building
not found — was it deleted?") with a link to `/sites`. No new
proto needed — `GetBuilding` already exists.

## Test plan

**Unit / component**

- `useActiveSite.test.ts`
  - Returns `{ kind: "all" }` when storage is empty.
  - Persists writes through `useReactiveLocalStorage`.
  - Falls back to `{ kind: "all" }` and clears storage when stored ID is not in current `sites`.
  - Default-after-first-login: single accessible site → `{ kind: "site", id }`; multiple → `{ kind: "all" }`.
  - Username-namespaced key isolates two users on the same browser.
- `SitePicker.test.tsx`
  - Hidden when `sites` is `[]`.
  - Renders label for each selection variant.
  - Click opens popover; clicking an item writes the active-site selection.
  - Sorts entries by site name ascending; "All Sites" first, "Unassigned" last.
- `navItems.test.ts`
  - With `MULTI_SITE_ENABLED === true`, `primaryNavItems` contains `/sites`.
  - With it `false`, `primaryNavItems` excludes `/sites`. (Mock the module.)
- `PageHeader.test.tsx`
  - Renders `SitePicker` when flag is on (regardless of route).
  - Renders `LocationSelector` when flag is off (regardless of route).
  - Calls `listSites` once on mount when flag is on.

**Smoke / integration**

- Render the router with a memory history at each of `/sites`,
  `/settings/sites`, `/buildings/:id` and confirm the corresponding
  page component mounts.
- Seed `ListSites` (via mocked client) with 0, 1, and 3 sites and
  confirm:
  - 0 sites → empty-state CTA renders on both `/sites` and `/settings/sites`.
  - 1 site → single-site view renders; SitePicker reads `{ kind: "site", id }` as default.
  - 3 sites → `/sites` stacks 3 sections; `/settings/sites` All Sites table has 3 rows sorted by name; SitePicker reads `{ kind: "all" }` as default.

**Acceptance / manual**

Run a local server with `VITE_MULTI_SITE_ENABLED=true`, hand-seed
the DB with 3 sites + 4 buildings + a few miners via direct SQL
or the existing migration test fixtures. Verify:

1. Sidenav shows "Sites" entry; clicking it navigates to `/sites`.
2. SitePicker appears in the topbar on **every** route (`/sites`,
   `/settings/sites`, `/buildings/:id`, `/miners`, `/racks`,
   `/`, etc.).
3. Toggling the picker between sites updates the rendered page
   content immediately on `/sites`, `/settings/sites`, and
   `/buildings/:id` (no full reload). On `/miners`, `/racks`,
   etc. the picker selection updates but the rendered list does
   not change — this is the documented transitional state until
   #202 lands.
4. Selection persists across navigation between routes and
   across browser refresh.
5. Settings subnav shows "Sites" entry; the settings page renders
   the All Sites table.
6. Clicking a row in the All Sites table moves the picker into
   `{ kind: "site" }` mode and renders the single-site config
   view.
7. Clicking a BuildingCard on `/sites` navigates to
   `/buildings/:id`.
8. With `VITE_MULTI_SITE_ENABLED=false`, both sidenav and
   settings subnav entries are hidden, the SitePicker does not
   appear (existing `LocationSelector` placeholder renders
   instead), but navigating directly to `/sites` /
   `/settings/sites` / `/buildings/:id` still renders each page
   (no 404). The new pages render in default ("All Sites") mode.

**Out of scope for this PR's tests**

- E2E tests of the create / edit / delete flows land alongside
  #261 / #262.
- E2E tests of the global picker rollout land with #202.
- Visual-regression / Storybook coverage of placeholder content is
  not added — Phase 1b replaces it.
