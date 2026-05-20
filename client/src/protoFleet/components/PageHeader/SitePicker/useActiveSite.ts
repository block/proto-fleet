import { useEffect, useMemo } from "react";

import { type ActiveSite, DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

export type { ActiveSite } from "@/protoFleet/store/types/activeSite";

interface UseActiveSiteOptions {
  // Set of known site IDs from the latest ListSites response (as decimal
  // strings). When the stored selection points at an ID not in this set,
  // the hook falls back to { kind: "all" } and overwrites the store.
  knownSiteIds: Set<string>;
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

  // If the stored selection points at a site that no longer exists (deleted,
  // reassigned, or the user lost access), reset to "all" once the known set
  // is non-empty. Skipping while the set is empty avoids clobbering valid
  // selections during the brief window before ListSites returns.
  useEffect(() => {
    if (stored.kind !== "site" || knownSiteIds.size === 0) return;
    if (!knownSiteIds.has(stored.id)) {
      setStored(DEFAULT_ACTIVE_SITE);
    }
  }, [stored, knownSiteIds, setStored]);

  const activeSite = useMemo<ActiveSite>(() => {
    if (stored.kind === "site" && knownSiteIds.size > 0 && !knownSiteIds.has(stored.id)) {
      return DEFAULT_ACTIVE_SITE;
    }
    return stored;
  }, [stored, knownSiteIds]);

  return { activeSite, setActiveSite: setStored };
};

export { useActiveSite };
