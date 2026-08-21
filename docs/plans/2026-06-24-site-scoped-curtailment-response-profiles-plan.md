---
title: "Site-scoped curtailment response profiles"
date: 2026-06-24
status: draft
type: plan
---

# Site-scoped curtailment response profiles

## Summary

Add site-scoped curtailment by wiring the existing backend/API support into
response profile creation and editing. Backend proto, persistence, validation,
automation, and event scope translation already support `site_id`, so the
minimum footprint is frontend-only unless tests expose a gap.

Energy page event lists can remain org-level for now; event details already
distinguish `whole_org` vs `site`.

## Key Changes

- Add a site selector to the response profile modal for both create and edit
  flows.
- Default remains `Whole fleet`.
- Site options come from the existing sites API.
- Preserve an existing `Site <id>` fallback while editing if site options have
  not loaded.
- Preserve valid site scope in response profile create mode instead of coercing
  it to whole-org.
- Preserve site scope when selecting a site-scoped response profile for live
  curtailment.
- Keep response profiles scoped only to `Whole fleet` or `Site` in this change.

## Implementation Notes

- Use the existing `useSites` client API to build sorted site options from
  `SiteWithCounts.site.id/name`.
- Pass site options into `CurtailmentStartModal` from the settings curtailment
  page/content.
- In response profile variant, render a compact `Apply to`/scope section with
  `Whole fleet` and each available site.
- If site scope is not available to the current user, disable the `Site` option
  and keep `Whole fleet` selectable.
- On whole-fleet selection, clear `siteId`, `siteName`, device sets, and
  explicit miner identifiers.
- On site selection, set `scopeType: "site"`, `siteId`, `siteName`, and clear
  unsupported miner/device-set fields.
- Leave backend, proto, sqlc, migrations, and generated files unchanged unless
  verification shows a missing backend path.

## Test Plan

- Update `CurtailmentStartModal.test.tsx` so response profile create shows the
  scope selector, site selection submits `siteId/siteName`, and test-curtailment
  preview/confirmation uses the selected site.
- Update edit-mode tests so the modal preserves existing site scope and can
  switch to another site or whole fleet.
- Add an edit-mode test where the initial response profile has `siteId` but
  site options are still empty/loading, asserting the modal displays
  `Site <id>` and preserves that site on save.
- Update unavailable-site-scope tests so the modal disables `Site` and leaves
  `Whole fleet` selectable for the current user.
- Update live curtailment profile-selection tests so selecting a site-scoped
  response profile preserves `scopeType: "site"`.
- Update `CurtailmentSettingsPage.test.tsx` to mock sites and verify creating a
  response profile with a selected site persists the site fields.
- Keep existing API hook tests for create/update payloads with `site`; add only
  if coverage gaps appear.
- Run targeted client tests:

```sh
npm test -- CurtailmentStartModal.test.tsx CurtailmentSettingsPage.test.tsx useCurtailmentResponseProfiles.test.tsx
```

## Assumptions

- Site selector is required for both create and edit.
- Energy page active/history queries remain org-level in this pass.
- API persists the site by ID; site name is UI display metadata and may fall
  back to `Site <id>`.
- Existing backend permission checks for optional site context are sufficient
  for this change.

## Deferred / Open Questions

### From 2026-06-24 review

- **Site selector data source has an unresolved permission dependency** —
  Implementation Notes / Assumptions (P2, feasibility, confidence 75)

  Fresh `ADMIN` roles get both `site:read` and `curtailment:manage` by
  default, but custom roles or operator-revoked Admin permissions can still
  leave a curtailment manager without `site:read`. Decide whether the site
  selector should require `site:read`, degrade gracefully, or get its options
  from a curtailment-authorized source.

- **Plan does not require server-side verification of site authorization for
  submitted site IDs** — Implementation Notes / Test Plan / Assumptions (P2,
  security-lens, adversarial, confidence 100)

  The frontend change submits site IDs for response profile creation, editing,
  and live curtailment. Add an explicit verification gate for invalid, deleted,
  cross-org, and unauthorized site IDs across create, update, live curtailment,
  automation execution, and event translation so site scope cannot be silently
  dropped, broadened, or misapplied.

- **Plan commits to a UI path before stating the operator outcome** — Premise
  challenge (P2, product-lens, confidence 75)

  The plan adds a site selector to response profile create/edit flows, but does
  not state the operator pain, workflow failure, or success outcome this solves.
  Capture the intended operator outcome so implementation can be judged against
  the product need, not just the technical `site_id` path.

- **Org-level event lists may undercut trust in site-scoped actions** —
  Strategic consequences (P2, product-lens, adversarial, confidence 100)

  The plan leaves Energy page active/history queries org-level while adding
  site-scoped response profile selection. Decide what evidence would make that
  unacceptable, or where operators should verify scope without drilling into
  individual event details.
