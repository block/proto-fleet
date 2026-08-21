import { useEffect } from "react";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildSiteSlugById } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";

// The scope an opened entity implies: either its own site (id + canonical slug)
// or the unassigned bucket when it has no site. Never "all" — an entity always
// lives somewhere specific, so viewing one can't imply an org-wide view.
export type ScopeSyncTarget = { kind: "site"; id: string; slug: string } | { kind: "unassigned" };

// Build the sync target for an entity that has finished loading. `siteId` is the
// entity's own site (bigint); undefined or 0n means the entity is unassigned.
// Returns `undefined` only while a scoped site's slug is still being resolved
// from the catalog, so the caller keeps waiting rather than sync a dead slug.
// Callers must pass `undefined` (not this) while the entity itself is still
// loading, so an unassigned target isn't confused with "not resolved yet".
export const entityScopeTarget = (
  siteId: bigint | undefined,
  sites: SiteWithCounts[] | undefined,
): ScopeSyncTarget | undefined => {
  const id = siteId?.toString();
  if (!id || id === "0") return { kind: "unassigned" };
  const slug = buildSiteSlugById(sites)?.get(id);
  return slug ? { kind: "site", id, slug } : undefined;
};

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
// `target` is undefined until the entity has loaded (and, for a scoped site,
// its slug resolved), so callers pass undefined while loading rather than let
// an unassigned entity look the same as "not resolved yet".
//
// Behavior:
//   - "all-sites"            → left untouched. Viewing one entity shouldn't
//                              collapse an intentional org-wide view.
//   - matching "site"        → no-op, unless the stored slug is stale (renamed):
//                              we hold the entity's canonical slug here, so
//                              refresh it in place rather than leave a dead slug
//                              that later produces broken scoped paths.
//   - different "site"       → overwritten to the entity's own site.
//   - unassigned entity      → overwritten to the unassigned bucket when the
//                              stored scope is a specific site (all-sites still
//                              left untouched).
//
// In-app navigation never mismatches, so this only changes behavior on deep
// links/bookmarks — exactly when switching context to the opened entity is
// desirable.
//
// Safe against useActiveSite's reconciliation: these routes carry no route
// scope, so the route-scope mirror effect early-returns and won't clobber this
// write, and the deleted-site guard passes for a real (loaded) site.
export const useSyncScopeToEntity = (target: ScopeSyncTarget | undefined): void => {
  const { activeSite, setActiveSite } = useActiveSite({});
  const kind = target?.kind;
  const siteId = target?.kind === "site" ? target.id : undefined;
  const slug = target?.kind === "site" ? target.slug : undefined;

  useEffect(() => {
    if (!kind) return;
    // Never collapse an intentional org-wide view.
    if (activeSite.kind === "all") return;
    if (kind === "unassigned") {
      if (activeSite.kind === "unassigned") return;
      setActiveSite({ kind: "unassigned" });
      return;
    }
    // Scoped site. A matching id but stale slug still falls through so the
    // rename is reconciled from the entity's canonical slug.
    if (!siteId || !slug) return;
    if (activeSite.kind === "site" && activeSite.id === siteId && activeSite.slug === slug) return;
    setActiveSite({ kind: "site", id: siteId, slug });
  }, [activeSite, kind, siteId, slug, setActiveSite]);
};
