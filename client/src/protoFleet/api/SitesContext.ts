import { createContext, useContext, useEffect } from "react";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

// Shared value published by SitesProvider — the single owner of the org's
// `ListSites` response, mounted once in the app shell above PageHeader and
// every routed page. Previously the always-mounted PageHeader, FleetLayout,
// and several pages each held their own `useSites()` fetch, so a navigation to
// a site-consuming page fired two parallel `ListSites` RPCs — each running the
// full server-side count + telemetry rollup. Sharing one fetch removes the
// duplicate aggregation and keeps the picker and page tables reading the same
// rows.
export interface SitesContextValue {
  // `undefined` while the first response is in flight (consumers render a
  // skeleton); `[]` once loaded with no sites, or for callers without
  // site:read (no fetch is issued).
  sites: SiteWithCounts[] | undefined;
  // Most recent `ListSites` error message, or null. Held alongside last-good
  // `sites` so a transient poll failure surfaces a retry without dropping the
  // table.
  sitesError: string | null;
  // True once any `ListSites` response has succeeded, even through later
  // failures. Distinguishes "never seen data" from "seen data, later poll
  // failed".
  sitesLoaded: boolean;
  // True once the first fetch attempt has resolved OR rejected. Lets route
  // guards stop waiting after an initial-load error (where `sitesLoaded`
  // stays false). Always true for callers without site:read.
  sitesSettled: boolean;
  // True only after a `ListSites` call returned PermissionDenied — a stale
  // session or server-side authz change that still denies the catalog RPC
  // after UI gating.
  sitesPermissionDenied: boolean;
  // True only after a successful catalog read with no permission denial.
  // FleetLayout's redirect waterfall keys off this to avoid catalog-backed
  // tabs for denied sessions.
  siteCatalogAccessGranted: boolean;
  refetchSites: () => void;
  // Register interest in *live* (polled) site data for as long as the caller
  // stays mounted; returns an unregister cleanup. The shared fetch is one-shot
  // by default (mount + sitesRevision) so header-only routes (e.g. settings)
  // don't run the ListSites count/telemetry rollups every 15s. Pages that
  // render live site tables/cards opt in, and the provider polls while at least
  // one is mounted.
  registerSitesPoll: () => () => void;
}

export const SitesContext = createContext<SitesContextValue | undefined>(undefined);

export const useSitesContext = (): SitesContextValue => {
  const ctx = useContext(SitesContext);
  if (!ctx) {
    throw new Error("useSitesContext must be used within a SitesProvider");
  }
  return ctx;
};

// Opt this route into polling the shared site catalog while mounted. Use from
// pages that show live site tables/cards (e.g. the Fleet shell); header-only
// routes should not call it so the catalog stays a one-shot fetch there.
export const useSitesPolling = (): void => {
  const { registerSitesPoll } = useSitesContext();
  useEffect(() => registerSitesPoll(), [registerSitesPoll]);
};
