import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Code } from "@connectrpc/connect";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { SitesContext, type SitesContextValue } from "@/protoFleet/api/SitesContext";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { usePoll } from "@/shared/hooks/usePoll";

// Single owner of the org's `ListSites` response. Mounted once in the app
// shell (AppLayout) above PageHeader and every routed page so the picker and
// the page tables share one fetch + poll instead of each firing their own.
export const SitesProvider = ({ children }: { children: ReactNode }) => {
  // ListSites is server-gated on org-scoped site:read; skip the fetch entirely
  // for non-readers so they don't get permission-denied toasts just by loading
  // the shell.
  const canReadSites = useHasPermission("site:read");
  const { listSites } = useSites();
  // Bumped by the site create / rename / delete flows; re-runs the poll effect
  // so a just-mutated site shows up without waiting for the next tick.
  const sitesRevision = useFleetStore((state) => state.ui.sitesRevision);

  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(canReadSites ? undefined : []);
  const [sitesError, setSitesError] = useState<string | null>(null);
  const [sitesLoaded, setSitesLoaded] = useState(false);
  const [sitesSettled, setSitesSettled] = useState(!canReadSites);
  const [sitesPermissionDenied, setSitesPermissionDenied] = useState(false);

  // Tracks the in-flight ListSites request. A mutation fires both a direct
  // refetchSites() and a sitesRevision bump, and the 15s poll can overlap a
  // manual refetch — so without sequencing a slow older response could land
  // after a newer one and resurrect a deleted site or revert a rename.
  // Aborting the previous request before starting a new one (listSites skips
  // onSuccess/onError once its signal aborts) keeps state monotonic.
  const abortRef = useRef<AbortController | null>(null);

  const fetchSites = useCallback(() => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    return listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setSitesError(null);
        setSitesLoaded(true);
        setSitesSettled(true);
        setSitesPermissionDenied(false);
      },
      onError: (msg, code) => {
        setSitesError(msg);
        setSitesSettled(true);
        if (code === Code.PermissionDenied) {
          setSitesPermissionDenied(true);
          // The catalog is genuinely inaccessible now (e.g. a mid-session
          // server-side authz change), so drop the last-good list — otherwise
          // picker consumers, which only read `sites`/`sitesError`, keep
          // rendering stale site names and allow selecting them.
          setSites([]);
        } else {
          // Preserve last-good list across transient errors; only fall to []
          // on the initial-load failure path.
          setSites((prev) => prev ?? []);
        }
      },
    });
  }, [listSites]);

  // Abort any in-flight request on unmount to avoid setState-after-unmount.
  useEffect(() => () => abortRef.current?.abort(), []);

  usePoll({
    fetchData: fetchSites,
    params: sitesRevision,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: canReadSites,
  });

  const value = useMemo<SitesContextValue>(
    () => ({
      sites,
      sitesError,
      sitesLoaded,
      sitesSettled,
      sitesPermissionDenied,
      siteCatalogAccessGranted: canReadSites && sitesLoaded && !sitesPermissionDenied,
      refetchSites: fetchSites,
    }),
    [sites, sitesError, sitesLoaded, sitesSettled, sitesPermissionDenied, canReadSites, fetchSites],
  );

  return <SitesContext.Provider value={value}>{children}</SitesContext.Provider>;
};
