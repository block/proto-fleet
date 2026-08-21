---
title: Site slugs for scoped routes
date: 2026-06-23
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/525
---

## Summary

PR #516 introduced site-scoped primary-section routes keyed on the numeric
site id (`/{siteId}/dashboard`, `/{siteId}/fleet`, …). Numeric ids are not
operator-friendly and leak the database identity into URLs. This plan adds a
stable, unique, server-generated **slug** to each site and switches the
user-visible scoped routes to use it (`/{siteSlug}/dashboard`, `/{siteSlug}/fleet`,
…). Numeric `site_id` remains the API identifier for every filter and
mutation — the slug is a URL-only alias that the client resolves back to a
numeric id at the route boundary.

For v1 slugs are **auto-generated and not user-editable**, but they **track the
site name**: renaming a site regenerates its slug (an edit that leaves the name
unchanged keeps the slug stable). There is no slug-change redirect/history
tracking. Multi-site is **not live yet**, so there are no production sites to
backfill and no live numeric-scoped URLs to preserve — the numeric route shape
from #516 is simply replaced, not redirected.

This is a sub-workstream of [Multi-site support](2026-05-05-multi-site-support-plan.md).

## Goals

- Every site (new and existing) has a unique, valid, URL-safe slug, unique
  within its org and guaranteed not to collide with any reserved root route
  segment.
- Scoped primary-section routes render slugs in user-visible URLs.
- The client resolves a slug route segment to a numeric site id for the
  existing additive `site_ids` / `include_unassigned` API filters — no API
  filter or mutation changes shape.
- `/unassigned/...` remains a reserved non-site scope.

## Non-goals

- User-editable slugs, slug history, or slug-change redirects. (Slugs are not
  user-editable and regenerate on rename; old slugs are not preserved.)
- Migrating mutations/filters off numeric `site_id` to slug.
- Scoped *detail* routes like `/{siteSlug}/sites/:slug`. The issue is explicit:
  do not nest site detail under a scope.
- Migrating site detail `/sites/:id` → `/sites/:slug`. Deferred to a follow-up;
  it is canonical/unscoped and changing it adds risk without unblocking the
  scoped-route goal.
- Numeric→slug compatibility redirects. Multi-site isn't live; no numeric scoped
  URLs exist in the wild to preserve.
- Per-site RBAC or any auth change.

## Key decisions (call out for review)

1. **Slug is not user-editable, but tracks the site name.** Auto-generated at
   create; regenerated from the name on a rename; stable across edits that don't
   change the name. Rationale: the issue says slugs "do not need to be editable
   by the user for v1", but a frozen slug would drift from a renamed site
   (rename "North DC" → slug stays `north-dc`), which is confusing. Regenerating
   on rename keeps the URL legible without exposing a slug field to the user. No
   old-slug redirect/history surface (multi-site isn't live; no links to break).
   A future issue can add slug-change 301 history if operators ask.
2. **Slug is a read-only proto field for v1.** `Site.slug` is populated by the
   server; `CreateSiteRequest`/`UpdateSiteRequest` do **not** accept a slug.
   This keeps the API contract minimal and avoids client-side validation/
   collision UX. (Issue scope lists slug on Create/Update "as needed" — for v1
   it is not needed.)
3. **Org-scoped uniqueness**, matching the existing `uk_site_org_name` partial
   unique index. Slug uniqueness is enforced among non-deleted sites only.
4. **Canonical slugify lives in Go** (one shared function), the single source of
   truth for all runtime slug creation. There is no production data to backfill;
   a one-time migration backfill exists only to satisfy `NOT NULL` for any
   pre-existing **test-server** rows, using `slugify(name) + '-' + <4 random
   hex>`. The always-present suffix sidesteps collisions and reserved words, so
   the SQL needs no collision loop and no reserved list — it doesn't have to
   match the Go algorithm.
5. **Reserved list is server-maintained**, documented inline (each word notes
   what route it protects), guarded by a parity test rather than build-time
   codegen. Codegen from the client route table is the "correct" abstraction but
   over-engineers the build system for a list that changes rarely.

## Slug format & generation

**Format (validation rule, shared constant):**
- lowercase `[a-z0-9-]+`
- no leading/trailing `-`, no consecutive `--`
- length 1–63
- must not equal any reserved segment (see below)
- must not match the numeric-id shape `^[1-9][0-9]*$` (so a slug can never be
  ambiguous with a legacy numeric segment during the redirect window)

**Generation (`slugify(name)`):**
1. lowercase, NFKD-normalize, strip accents
2. replace each run of non-`[a-z0-9]` with a single `-`
3. trim leading/trailing `-`, truncate to 63
4. if the result is empty **or** purely numeric **or** a reserved word →
   fall back to base `site`
5. resolve collisions within the org by appending `-2`, `-3`, … (the bare base
   first, then numeric suffixes) against the set of live slugs in that org

The reserved-word and numeric-shape rules in step 4 mean a site literally named
"Dashboard" or "123" still produces a safe slug (`dashboard-2`-style only if a
real collision, otherwise the reserved branch forces a suffix — see tests).

## Reserved segments (single source of truth)

The reserved set is defined **once on the server**, as a documented constant,
and validated at slug generation. It is the union of every first-segment route
the app owns. Each entry carries an inline comment noting what it protects:

```
dashboard, fleet, groups, energy, activity, settings, auth, welcome,
onboarding, miners, racks, buildings, sites, fleet-down, unassigned,
update-password
```

We deliberately **hand-maintain** this list rather than codegen it from the
client route table. Generating it (parse React Router config → emit a Go
constant) is the cleaner abstraction in theory, but threading that through the
build system is over-engineering for a list that changes a couple times a year.

The client already has a narrower `SCOPABLE_ROOT_SEGMENTS` set
(`client/src/protoFleet/routing/siteScope.tsx:9`) which stays as-is for routing
logic. The one drift guard is a **parity test** (see Testing) that fails CI if
the client registers a root route segment not present in the server reserved
set — catching the "new route shadows an existing slug" failure mode without any
build-system change.

## Backend / data model changes

### Migration `000095_add_slug_to_site`
`server/migrations/000095_add_slug_to_site.up.sql`:

There is **no production data** — this backfill exists only so the `NOT NULL`
add succeeds against any pre-existing test-server rows. It is intentionally
dumb: `slugify(name) + '-' + <4 random hex>`. The always-present random suffix
guarantees uniqueness and dodges reserved words, so no collision loop and no
reserved list in SQL — and it does **not** need to match the Go algorithm
(those test rows are throwaway; new sites get clean slugs at runtime).

1. `ALTER TABLE site ADD COLUMN slug VARCHAR(63);` (nullable initially)
2. Backfill every existing row in one `UPDATE`, clamping the base so the
   deterministic id suffix always fits within the 63-character slug column:
   ```sql
   WITH normalized AS (
     SELECT id,
     COALESCE(
       NULLIF(trim(both '-' from regexp_replace(lower(name), '[^a-z0-9]+', '-', 'g')), ''),
       'site'
     ) AS base
     FROM site
   )
   UPDATE site
   SET slug = left(normalized.base, 63 - length(site.id::text) - 1) || '-' || site.id::text
   FROM normalized
   WHERE normalized.id = site.id;
   ```
   (The id suffix is deterministic and unique per row. Soft-deleted rows are
   backfilled too — they need a non-null value but are excluded from the unique
   index.)
3. `ALTER TABLE site ALTER COLUMN slug SET NOT NULL;`
4. `CREATE UNIQUE INDEX uk_site_org_slug ON site(org_id, slug) WHERE deleted_at IS NULL;`

Down migration drops the index and the column. Follow the
`migration-immutability` skill: this is a new pair, never edit a shipped one.

### Proto (`proto/sites/v1/sites.proto`)
- `Site`: add `string slug = 17;` (read-only output).
- `CreateSiteRequest` / `UpdateSiteRequest`: **no change** for v1.
- Regenerate clients (`just regen` / proto-regen skill).

### sqlc (`server/sqlc/queries/site.sql`)
- `CreateSite`: add `slug` to the column list and args, `RETURNING *` already
  carries it.
- `ListSites` / `GetSite`: `SELECT *` / `s.*` already include the new column —
  verify the generated structs pick it up after `sqlc generate`.
- Add `GetSiteBySlug` (org-scoped, `deleted_at IS NULL`) — used only if we ever
  resolve server-side; the client resolves from `ListSites` so this may be
  unnecessary. Add only if a consumer needs it.

### Domain / service (`server/internal/domain/sites/service.go`)
- Add `Slug string` to `models.Site`.
- New shared `slugify` + reserved-word validation in a small package (e.g.
  `server/internal/domain/sites/slug.go`) with the canonical algorithm above.
- `CreateSite`: generate the slug from `name`, resolving collisions against the
  org's live slugs in the same transaction that holds `LockSiteForWrite`-style
  serialization; map the `uk_site_org_slug` unique-violation to a retry/suffix
  bump (belt-and-suspenders against a race).
- `UpdateSite`: read the current row; if the name is unchanged, carry the
  existing slug through untouched (no churn on unrelated edits). If the name
  changed, regenerate the slug from the new name — building the used-slug set
  from the org's live slugs **excluding this site's own current slug** (so a
  rename can re-derive the same/shorter base) — with the same
  `uk_site_org_slug` collision retry/suffix loop as `CreateSite`.

### Translate (`server/internal/handlers/sites/translate.go`)
- `toProtoSite`: copy `site.Slug` into the proto.
- `toCreateSiteParams`: no slug input (server generates).

## Client changes

The architectural heart: today `ActiveSite` of kind `site` carries a single
`id: string` that is used **both** as the URL segment and the API filter
(`siteScope.tsx:51` returns `id` for the URL; `siteFilter.ts:26` uses
`BigInt(id)` for the filter). Slugs split these two roles, so `ActiveSite` must
carry both, and the route layout must resolve slug → site using `ListSites`
data (it can no longer be a pure regex parse).

### Model (`store/types/activeSite.ts`)
- `{ kind: "site"; id: string; slug: string }`.
- `id` stays the decimal-string numeric id (API filter, persistence key).
- `slug` is the URL segment.
- Update `isActiveSite` guard + `sanitizeActiveSite`.
- Persisted Zustand selections from the old schema (no `slug`) fail the guard
  and reset to `{ kind: "all" }` — acceptable, self-healing on next pick.

### Route layout (`routing/siteScope.tsx`)
- `SITE_ID_SEGMENT_RE` → a slug-shaped regex `^[a-z0-9][a-z0-9-]*$` (still
  rejecting `unassigned`, which is matched first).
- `activeSiteFromSegment` can no longer build a complete `ActiveSite` from the
  string alone — it needs the slug→{id} map. **`SiteScopeLayout` becomes
  data-dependent:** it consumes `ListSites` (via `useSites` + `buildKnownSiteIds`
  equivalent keyed by slug), resolves the segment to `{ id, slug }`, and:
  - unknown slug + sites loaded → heal to `/` (extends the existing
    `routeScopeStale` healing in `useActiveSite.ts:35`)
  - sites not loaded yet → render nothing / spinner (mirror existing pre-fetch
    window handling)
- `segmentFromActiveSite` returns `activeSite.slug` (was `.id`).
- `scopedPath`, `scopeCurrentOrDashboardPath`, `appEntryPath`,
  `unscopedScopablePath`, `activeSiteFromScopablePath` keep working — they route
  through `segmentFromActiveSite`, so they emit slugs automatically once that
  returns the slug.

### Filter boundary (`components/PageHeader/SitePicker/siteFilter.ts`)
- `siteFilterFromActive` already reads `active.id` (numeric) — **no change**,
  because we keep `id` on the model. This is why carrying both fields (not
  replacing id with slug) is the right call.

### Known-sites resolution (`api/sites.ts`)
- Add `buildSiteSlugToId(sites)` (and/or `buildKnownSiteSlugs`) so the layout
  and `useActiveSite` staleness check can validate by slug as well as id.
- `useActiveSite` staleness check (`useActiveSite.ts`) keys on `id` today; the
  route now arrives as a slug, so resolution happens in the layout and
  `useActiveSite` keeps validating the resolved `id`. Confirm the healing path
  strips the slug segment correctly via `unscopedScopablePath`.

### SitePicker (`components/PageHeader/SitePicker/SitePicker.tsx`)
- When building an `ActiveSite` for a site option, include both `id` and `slug`
  from the `SiteWithCounts` row.
- Navigation via `scopeCurrentOrDashboardPath` is unchanged — it now emits slug
  URLs through `segmentFromActiveSite`.

### No numeric-route compatibility branch
Multi-site isn't live, so no numeric scoped URLs exist to preserve. The numeric
segment regex is simply replaced by the slug regex — a stray `/7/fleet` now
fails `activeSiteFromSegment` and heals to `/`, same as any other bad segment.

### Detail route
- `/sites/:id` (numeric) unchanged for v1 (`router.tsx:217`,
  `SiteDetailPage.tsx`). Migration to `/sites/:slug` is a deferred follow-up.

## Testing

**Server**
- `slugify` unit tests: spaces/punct → dashes; accents; empty/numeric/reserved
  name → fallback; collision suffixing `-2`,`-3`; length cap.
- Reserved-word rejection: every word in the reserved set forces a suffix.
- Service create: two sites with the same name in one org get distinct slugs;
  same name across orgs is fine.
- Migration test: seed a couple of test-server-style rows (duplicate names, a
  name = "dashboard", an all-emoji name, a soft-deleted row), run the up
  migration, assert every row gets a unique non-null slug and the `NOT NULL` +
  unique index hold. (Slug *quality* isn't asserted — the suffix makes the
  backfill dumb on purpose.)
- Reserved-parity test: assert the server reserved set contains every root route
  segment the client registers (the drift guard).

**Client**
- `siteScope` tests (`siteScope.test.ts`): update numeric-id expectations to
  slug expectations; `scopedPath("/fleet", {kind:"site",id:"7",slug:"north-dc"})`
  → `/north-dc/fleet`; unscope strips slug segment.
- `activeSiteFromSegment` slug regex accepts `north-dc`, rejects `Fleet`,
  `north_dc`, empty.
- `siteFilter` tests unchanged (still numeric) — assert they still pass to prove
  the API boundary didn't move.
- `SiteScopeLayout`: unknown slug heals to `/`; known slug provides scope;
  pre-load window renders nothing.
- SitePicker: selecting a site navigates to its slug URL.
- `useActiveSite`: stale (deleted) site still heals.

## Rollout / sequencing

1. Migration + proto + sqlc + Go slug generation + server tests. (Slug is
   populated and returned but unused by the client — safe to ship alone.)
2. Client model + route layout resolution + SitePicker + tests, behind the
   existing `MULTI_SITE_ENABLED` flag.

No bookmark/redirect cleanup phase — there's nothing live to migrate off.

## Resolved decisions (previously open)

- **Backfill:** no production data; the migration backfill is a dumb
  `slugify(name) + '-' + <4 hex>` purely to satisfy `NOT NULL` for stray
  test-server rows. No `unaccent` dependency, no Go/SQL parity requirement.
- **Reserved set:** server-maintained documented constant + a client→server
  parity test. No build-time codegen.
- **Numeric redirects:** not needed; multi-site isn't live.
- **Detail route:** defer `/sites/:id` → `/sites/:slug`.
