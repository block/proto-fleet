import { useEffect, useMemo } from "react";

import { useRouteSiteScope } from "@/protoFleet/routing/siteScope";
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
}

interface UseActiveSiteResult {
  activeSite: ActiveSite;
  setActiveSite: (next: ActiveSite) => void;
}

// Thin wrapper around the Zustand UI slice. Persistence (org-wide, matching
// `duration` and other UI prefs) is handled by useFleetStore's persist
// middleware — this hook only adds the "selection points at a deleted site"
// validation effect.
const useActiveSite = ({ knownSiteIds }: UseActiveSiteOptions): UseActiveSiteResult => {
  const stored = useFleetStore((state) => state.ui.activeSite);
  const setStored = useFleetStore((state) => state.ui.setActiveSite);
  const routeScope = useRouteSiteScope();
  const knownSiteIdsLoaded = knownSiteIds !== undefined;
  const routeScopeStale = routeScope?.kind === "site" && knownSiteIdsLoaded && !knownSiteIds.has(routeScope.id);

  useEffect(() => {
    if (!routeScope) return;
    if (routeScopeStale) {
      if (!activeSitesEqual(stored, DEFAULT_ACTIVE_SITE)) {
        setStored(DEFAULT_ACTIVE_SITE);
      }
      return;
    }
    if (activeSitesEqual(stored, routeScope)) return;
    setStored(routeScope);
  }, [routeScope, routeScopeStale, stored, setStored]);

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
  if (a.kind === "site" && b.kind === "site") return a.id === b.id;
  return true;
};

export { useActiveSite };
