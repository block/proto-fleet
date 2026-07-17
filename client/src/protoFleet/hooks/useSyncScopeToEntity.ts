import { useEffect } from "react";

import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";

// Sync the persisted header scope to the entity being viewed on the
// **headerless** detail routes (`/buildings/:id`, `/racks/:rackId`,
// `/sites/:id`). Those routes render outside SiteScopeLayout, so `useActiveSite`
// falls back to the last-persisted SitePicker selection, which can point at an
// unrelated site when the page is reached via deep link or bookmark (e.g.
// opening a North building while "South" is selected). That stale scope drives
// MinerSelectionList's toggle-on breadth and the Building/Rack facet options,
// producing the pre-existing miner picker bug (#764).
//
// Fixing it here — at the navigation layer — keeps every downstream consumer
// (modals, facets, feeds) agreeing with the opened entity, so no per-modal
// special-casing is needed.
//
// Behavior:
//   - "all-sites"       → left untouched. Viewing one entity shouldn't collapse
//                         an intentional org-wide view.
//   - matching "site"   → no-op, unless the stored slug is stale (renamed): we
//                         hold the entity's canonical slug here, so refresh it
//                         in place rather than leave a dead slug that later
//                         produces broken scoped paths.
//   - different "site"  → overwritten to the entity's own site.
//   - "unassigned"      → overwritten to the entity's own site.
//
// Only fires once the entity's site id + slug are resolved (both required to
// build a scoped ActiveSite). In-app navigation never mismatches, so this only
// changes behavior on deep links/bookmarks — exactly when switching context to
// the opened entity is desirable.
//
// Safe against useActiveSite's reconciliation: these routes carry no route
// scope, so the route-scope mirror effect early-returns and won't clobber this
// write, and the deleted-site guard passes for a real (loaded) site.
export const useSyncScopeToEntity = (siteId: string | undefined, slug: string | undefined): void => {
  const { activeSite, setActiveSite } = useActiveSite({});

  useEffect(() => {
    if (!siteId || !slug) return;
    // Never collapse an intentional org-wide view.
    if (activeSite.kind === "all") return;
    // Already scoped to this entity's site with an up-to-date slug — nothing to
    // do. A matching id but stale slug still falls through so the rename is
    // reconciled from the entity's canonical slug.
    if (activeSite.kind === "site" && activeSite.id === siteId && activeSite.slug === slug) return;
    setActiveSite({ kind: "site", id: siteId, slug });
  }, [activeSite, siteId, slug, setActiveSite]);
};
