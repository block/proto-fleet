import { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";

import { type FleetOutletContext } from "./outletContext";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useHasPermission } from "@/protoFleet/store";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { usePoll } from "@/shared/hooks/usePoll";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

type FleetTabId = "sites" | "buildings" | "racks" | "miners";

const TAB_ORDER: FleetTabId[] = MULTI_SITE_ENABLED ? ["sites", "buildings", "racks", "miners"] : ["racks", "miners"];
const DEFAULT_TAB: FleetTabId = MULTI_SITE_ENABLED ? "sites" : "racks";
const DEFAULT_TAB_NO_SITES: FleetTabId = MULTI_SITE_ENABLED ? "buildings" : "racks";
const DEFAULT_TAB_NO_SITES_ACCESS: FleetTabId = "miners";
const LAST_TAB_KEY = "fleet:lastActiveTab";

const tabLabel: Record<FleetTabId, string> = {
  miners: "Miners",
  racks: "Racks",
  buildings: "Buildings",
  sites: "Sites",
};

// Recognize all four ids regardless of flag so a persisted `lastTab` from a
// flag-on session isn't discarded as garbage when the flag flips.
const ALL_TAB_IDS = new Set<FleetTabId>(["sites", "buildings", "racks", "miners"]);
const isFleetTabId = (s: string): s is FleetTabId => ALL_TAB_IDS.has(s as FleetTabId);
const isVisibleFleetTabId = (s: string): s is FleetTabId => (TAB_ORDER as string[]).includes(s);

const tabFromPath = (pathname: string): FleetTabId | undefined => {
  const m = pathname.match(/^\/fleet\/([^/]+)/);
  if (!m) return undefined;
  return isFleetTabId(m[1]) ? m[1] : undefined;
};

const FleetLayout = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [lastTab, setLastTab] = useReactiveLocalStorage<FleetTabId | undefined>(LAST_TAB_KEY, undefined);

  // ListSites and ListBuildings both sit behind PermSiteRead server-side.
  // Reading from the catalog (instead of inferring from a failed RPC) keeps
  // transient transport errors out of the access-blocked branch.
  const canReadSites = useHasPermission("site:read");

  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(canReadSites ? undefined : []);
  const [sitesError, setSitesError] = useState<string | null>(null);
  // Stays true once any listSites response succeeds, even through later
  // failures. Lets consumers tell "we have last-good data" from "we've
  // never seen data" when sites is [].
  const [sitesLoaded, setSitesLoaded] = useState(false);

  const fetchSites = useCallback(
    () =>
      listSites({
        onSuccess: (rows) => {
          setSites(rows);
          setSitesError(null);
          setSitesLoaded(true);
        },
        onError: (msg) => {
          setSitesError(msg);
          // Preserve last-good list across transient errors; only fall to []
          // on the initial-load failure path.
          setSites((prev) => prev ?? []);
        },
      }),
    [listSites],
  );

  usePoll({ fetchData: fetchSites, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: canReadSites });

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // A stale "single site" selection pointing at a deleted site (knownSiteIds
  // is empty and useActiveSite can't reset) must keep the tab visible so the
  // operator can still create a new site.
  const sitesTabHidden = activeSite.kind === "site" && knownSiteIds.has(activeSite.id);

  const currentTab = tabFromPath(location.pathname);

  const sitesAccessBlocked = !canReadSites;
  const fallback = sitesAccessBlocked
    ? DEFAULT_TAB_NO_SITES_ACCESS
    : sitesTabHidden
      ? DEFAULT_TAB_NO_SITES
      : DEFAULT_TAB;

  // Defer redirect until the initial sites load resolves so a stale
  // single-site picker selection doesn't briefly hide the Sites tab before
  // useActiveSite's known-id validation can reset it.
  useEffect(() => {
    if (sites === undefined) return;
    const safeLastTab = lastTab && isVisibleFleetTabId(lastTab) ? lastTab : undefined;
    const lastTabUsable =
      safeLastTab &&
      (safeLastTab === "sites" ? !sitesTabHidden && !sitesAccessBlocked : true) &&
      (safeLastTab === "buildings" ? !sitesAccessBlocked : true);
    const usableLastTab = lastTabUsable ? safeLastTab : undefined;
    if (location.pathname === "/fleet" || location.pathname === "/fleet/") {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    if (currentTab && !isVisibleFleetTabId(currentTab)) {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    if (currentTab === "sites" && sitesTabHidden) {
      // Single-site picker — treat /fleet/sites as a shortcut to that site's
      // management surface so legacy "Manage sites" entry points still land
      // somewhere useful.
      if (activeSite.kind === "site") {
        navigate(`/sites/${activeSite.id}`, { replace: true });
        return;
      }
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    if (currentTab === "sites" && sitesAccessBlocked) {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    if (currentTab === "buildings" && sitesAccessBlocked) {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
    }
  }, [
    sites,
    location.pathname,
    currentTab,
    sitesTabHidden,
    sitesAccessBlocked,
    activeSite,
    lastTab,
    navigate,
    fallback,
  ]);

  useEffect(() => {
    if (currentTab && currentTab !== lastTab) {
      setLastTab(currentTab);
    }
  }, [currentTab, lastTab, setLastTab]);

  const onSelect = useCallback(
    (id: string) => {
      if (isFleetTabId(id)) navigate(`/fleet/${id}`);
    },
    [navigate],
  );

  const visibleTabs = TAB_ORDER.filter((t) => {
    if (t === "sites" && (sitesTabHidden || sitesAccessBlocked)) return false;
    if (t === "buildings" && sitesAccessBlocked) return false;
    return true;
  });

  const outletContext: FleetOutletContext = useMemo(
    () => ({ sites, sitesError, sitesLoaded, refetchSites: fetchSites }),
    [sites, sitesError, sitesLoaded, fetchSites],
  );

  return (
    <div className="flex h-full flex-col" data-testid="fleet-layout">
      <div className="sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-6 laptop:px-10">
        <h1 className="text-heading-300 text-text-primary">Fleet</h1>
        <TabStrip activeId={currentTab} onSelect={onSelect} ariaLabel="Fleet sections">
          {visibleTabs.map((tab) => (
            <TabStripItem key={tab} id={tab} label={tabLabel[tab]} testId={`fleet-tab-${tab}`} />
          ))}
        </TabStrip>
      </div>
      <div className="min-h-0 flex-1">
        <Outlet context={outletContext} />
      </div>
    </div>
  );
};

export default FleetLayout;
