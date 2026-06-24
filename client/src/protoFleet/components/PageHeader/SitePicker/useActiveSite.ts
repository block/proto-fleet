import { useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { unscopedScopablePath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { type ActiveSite, DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

export type { ActiveSite } from "@/protoFleet/store/types/activeSite";

interface UseActiveSiteOptions {
  // Set of known site IDs from the latest ListSites response (as decimal
  // strings). `undefined` means ListSites has not returned yet; an empty set
  // means it returned with no sites. When the stored selection points at an ID
  // not in a loaded set, the hook falls back to { kind: "all" } and overwrites
  // the store.
  knownSiteIds?: Set<string>;
  // id (decimal string) -> current slug from the same ListSites response.
  // When provided, the hook reconciles a stored selection whose slug went
  // stale (renamed in another tab/session: same id, new slug) so the persisted
  // entry path + picker don't emit a dead slug that ResolveSiteBySlug then
  // treats as missing. Optional — only callers with the full sites list pass
  // it (the header SitePicker), so the reconcile has a single owner.
  knownSiteSlugById?: Map<string, string>;
}

interface UseActiveSiteResult {
  activeSite: ActiveSite;
  setActiveSite: (next: ActiveSite) => void;
}

// Thin wrapper around the Zustand UI slice. Persistence (org-wide, matching
// `duration` and other UI prefs) is handled by useFleetStore's persist
// middleware — this hook only adds the "selection points at a deleted site"
// validation effect.
const useActiveSite = ({ knownSiteIds, knownSiteSlugById }: UseActiveSiteOptions): UseActiveSiteResult => {
  const stored = useFleetStore((state) => state.ui.activeSite);
  const setStored = useFleetStore((state) => state.ui.setActiveSite);
  const routeScope = useRouteSiteScope();
  const navigate = useNavigate();
  const { pathname, search, hash } = useLocation();
  const knownSiteIdsLoaded = knownSiteIds !== undefined;
  const routeScopeStale = routeScope?.kind === "site" && knownSiteIdsLoaded && !knownSiteIds.has(routeScope.id);

  useEffect(() => {
    if (!routeScope) return;
    if (routeScopeStale) {
      // The URL points at a site that no longer exists or is inaccessible.
      // Heal the URL by stripping the stale scope segment so every consumer
      // (header picker, page feeds, CSV exports) agrees on all-sites rather
      // than silently filtering to a non-existent site. Resetting the stored
      // selection keeps the picker correct through the redirect.
      if (!activeSitesEqual(stored, DEFAULT_ACTIVE_SITE)) {
        setStored(DEFAULT_ACTIVE_SITE);
      }
      const current = `${pathname}${search}${hash}`;
      const healed = `${unscopedScopablePath(pathname)}${search}${hash}`;
      if (healed !== current) {
        navigate(healed, { replace: true });
      }
      return;
    }
    if (activeSitesEqual(stored, routeScope)) return;
    setStored(routeScope);
  }, [routeScope, routeScopeStale, stored, setStored, navigate, pathname, search, hash]);

  // If the stored selection points at a site that no longer exists (deleted,
  // reassigned, or the user lost access), reset to "all" once ListSites has
  // returned. Skipping while the set is undefined avoids clobbering valid
  // selections during the brief pre-fetch window.
  useEffect(() => {
    if (routeScope) return;
    if (stored.kind !== "site" || !knownSiteIdsLoaded) return;
    if (!knownSiteIds.has(stored.id)) {
      setStored(DEFAULT_ACTIVE_SITE);
    }
  }, [routeScope, stored, knownSiteIds, knownSiteIdsLoaded, setStored]);

  // Reconcile a stale stored slug against the latest ListSites response. If the
  // selected site was renamed elsewhere (another tab/session), ListSites
  // returns the same id with a new slug while the store still holds the old
  // one — left alone, appEntryPath/picker navigation would emit a dead slug
  // that ResolveSiteBySlug treats as missing and clears the scope. Same id +
  // changed slug is a pure rename, so refresh the slug in place.
  useEffect(() => {
    if (stored.kind !== "site" || !knownSiteSlugById) return;
    const freshSlug = knownSiteSlugById.get(stored.id);
    if (freshSlug && freshSlug !== stored.slug) {
      setStored({ kind: "site", id: stored.id, slug: freshSlug });
    }
  }, [stored, knownSiteSlugById, setStored]);

  const activeSite = useMemo<ActiveSite>(() => {
    if (routeScopeStale) return DEFAULT_ACTIVE_SITE;
    if (routeScope) return routeScope;
    if (stored.kind === "site" && knownSiteIdsLoaded && !knownSiteIds.has(stored.id)) {
      return DEFAULT_ACTIVE_SITE;
    }
    return stored;
  }, [routeScope, routeScopeStale, stored, knownSiteIds, knownSiteIdsLoaded]);

  return { activeSite, setActiveSite: setStored };
};

const activeSitesEqual = (a: ActiveSite, b: ActiveSite): boolean => {
  if (a.kind !== b.kind) return false;
  if (a.kind === "site" && b.kind === "site") return a.id === b.id && a.slug === b.slug;
  return true;
};

export { useActiveSite };
