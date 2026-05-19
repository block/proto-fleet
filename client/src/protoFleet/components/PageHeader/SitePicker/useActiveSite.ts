import { useCallback, useEffect, useMemo } from "react";

import { useUsername } from "@/protoFleet/store";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

// Discriminated union for the picker's current selection. Site IDs come back
// from the proto as bigint, but bigint isn't JSON-serializable; we store the
// decimal string form and convert at the boundary.
export type ActiveSite = { kind: "all" } | { kind: "site"; id: string } | { kind: "unassigned" };

const DEFAULT_ACTIVE_SITE: ActiveSite = { kind: "all" };

const storageKey = (username: string) => `multiSite.activeSite:${username}`;

interface UseActiveSiteOptions {
  // Set of known site IDs from the latest ListSites response (as decimal
  // strings). When the stored selection points at an ID not in this set,
  // the hook falls back to { kind: "all" } and overwrites storage.
  knownSiteIds: Set<string>;
}

interface UseActiveSiteResult {
  activeSite: ActiveSite;
  setActiveSite: (next: ActiveSite) => void;
}

const useActiveSite = ({ knownSiteIds }: UseActiveSiteOptions): UseActiveSiteResult => {
  const username = useUsername();
  // Username may be empty during the first render before auth resolves. We
  // still call the storage hook unconditionally to keep hook order stable;
  // an empty username produces a transient key that never persists meaningful
  // state.
  const key = username ? storageKey(username) : "multiSite.activeSite:__anonymous";
  const [stored, setStored] = useReactiveLocalStorage<ActiveSite>(key, DEFAULT_ACTIVE_SITE);

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

  const setActiveSite = useCallback(
    (next: ActiveSite) => {
      setStored(next);
    },
    [setStored],
  );

  return { activeSite, setActiveSite };
};

export { useActiveSite };
