---
title: KPI tab navigation must prefix minerRoot to stay inside the embedded miner view
date: 2026-07-02
category: docs/solutions/logic-errors
module: client/src/protoOS/features/kpis
problem_type: logic_error
component: navigation
symptoms:
  - "Clicking Hashrate/Efficiency/Power Usage/Temperature in the single-miner view jumps back to the fleet dashboard"
  - "KPI tab strip never shows an active tab when fleet-hosted"
  - "Absolute navigation like /hashrate escapes the /miners/:id/* embed"
root_cause: logic_error
resolution_type: code_fix
severity: medium
related_components:
  - client/src/shared/components/TabMenu
  - client/src/protoOS/contexts/MinerHostingContext
  - client/src/protoFleet/routing/siteScope
tags: [navigation, single-miner, embedded-view, fleet-hosted, minerRoot, base-path, protoOS, protoFleet]
---

# KPI tab navigation must prefix minerRoot to stay inside the embedded miner view

## Problem

ProtoOS runs two ways: standalone (routes at `/hashrate`, `/efficiency`, …) and
embedded inside ProtoFleet's single-miner view at `/miners/:id/*`. In-view
navigation therefore cannot use absolute paths — the correct target depends on
which shell is hosting.

The KPI tab strip (the Hashrate / Efficiency / Power Usage / Temperature stat
cards on the miner Home page) navigated to absolute paths. `KpiLayout` rendered
`<TabMenu />` with no `basePath`, so the shared `TabMenu` defaulted it to `""`
and called `navigate("/hashrate")`.

- **Standalone ProtoOS:** `/hashrate` is a real route → worked.
- **Fleet-embedded:** the target should be `/miners/:id/hashrate`, but it
  navigated to `/hashrate`. ProtoFleet's router matches that against its
  `/:siteScope` route (treating `"hashrate"` as a site slug), fails to resolve
  it, and `SiteScopeLayout` renders `<Navigate to="/" replace />` →
  `appEntryLoader` → the **dashboard**.

A second, quieter symptom: the active-tab check
(`basePath + items[key].path === location.pathname`) never matched in fleet
mode, so no KPI tab ever appeared selected.

## Symptoms

- Clicking any KPI stat card in `/miners/:id/*` redirects to the fleet dashboard.
- No KPI tab is highlighted as active when fleet-hosted.

## Solution

Derive the base path from `minerRoot` (provided by `MinerHostingContext`) instead
of leaving it empty. `TabMenuWrapper` already had a dead `basePath` prop that was
never passed; wire it to the hosting context:

```tsx
const TabMenuWrapper = memo(() => {
  const temperatureUnit = useTemperatureUnit();
  const miner = useMiner();
  // Prefix minerRoot so KPI tab navigation stays inside the embedded miner view
  // when fleet-hosted (minerRoot is "" in standalone protoOS). Without it the
  // absolute paths (e.g. "/hashrate") escape the embed and ProtoFleet's
  // "/:siteScope" route treats the tab segment as an unknown site, redirecting
  // back to the dashboard.
  const { minerRoot } = useMinerHosting();
  // ...tabItems...
  return <TabMenu items={tabItems} basePath={minerRoot} />;
});
```

`minerRoot` is `/miners/:id` when fleet-hosted and `""` in standalone ProtoOS —
both apps wrap the tree in `MinerHostingProvider` — so standalone behavior is
unchanged. Coverage: `TabMenuWrapper.test.tsx` asserts `/hashrate` in standalone
and `/miners/miner-1/efficiency` when hosted.

## Why This Works

`minerRoot` is the single source of truth for "where does this ProtoOS instance
live in the URL tree." Prefixing it makes every navigation relative to the host
shell without the component needing to know which shell that is. Sibling
in-view navigations already followed this idiom:

- `NavigationMenu/Navigation.tsx` — `navigate(`${minerRoot}/${navigationItem}`)`
- `NoPoolsCallout/NoPoolsCallout.tsx` — `navigate(`${minerRoot}/${navigationItems.miningPools}`)`
- `PageHeader/PoolStatus/PoolStatusWrapper.tsx` — same pattern

The KPI tab strip was the one place that missed it.

## Prevention

- **Any navigation inside ProtoOS must be relative to `minerRoot`.** Never call
  `navigate("/some-route")` or build a `<Link to="/some-route">` with a leading
  slash in ProtoOS code — prefix `minerRoot` from `useMinerHosting()`.
- The shared `TabMenu` defaulting `basePath` to `""` (i.e. absolute navigation)
  is the trap. When adding a new embedded consumer of `TabMenu`, always pass an
  explicit `basePath`; treat an unset `basePath` as a bug in fleet-hosted code.
- When a route "silently redirects to the dashboard," suspect an absolute path
  escaping the embed and being swallowed by ProtoFleet's `/:siteScope`
  catch-all. That route treats any unknown top-level segment as a site slug.

## Related

Introduced by the embedded single-miner view: PR #581
(`feat(fleet): single-miner view with embedded ProtoOS proxy`). The
`minerRoot` prefixing convention comes from `MinerHostingContext`.
