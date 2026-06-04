import { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";

import { type FleetOutletContext } from "./outletContext";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

type FleetTabId = "sites" | "buildings" | "racks" | "miners";

const TAB_ORDER: FleetTabId[] = ["sites", "buildings", "racks", "miners"];
const DEFAULT_TAB: FleetTabId = "sites";
const DEFAULT_TAB_NO_SITES: FleetTabId = "buildings";
const LAST_TAB_KEY = "fleet:lastActiveTab";

const tabLabel: Record<FleetTabId, string> = {
  miners: "Miners",
  racks: "Racks",
  buildings: "Buildings",
  sites: "Sites",
};

const isFleetTabId = (s: string): s is FleetTabId => (TAB_ORDER as string[]).includes(s);

const tabFromPath = (pathname: string): FleetTabId | undefined => {
  const m = pathname.match(/^\/fleet\/([^/]+)/);
  if (!m) return undefined;
  return isFleetTabId(m[1]) ? m[1] : undefined;
};

const FleetLayout = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [lastTab, setLastTab] = useReactiveLocalStorage<FleetTabId | undefined>(LAST_TAB_KEY, undefined);

  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [sitesError, setSitesError] = useState<string | null>(null);

  const fetchSites = useCallback(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setSitesError(null);
      },
      onError: (msg) => {
        setSitesError(msg);
        // Preserve last-good list across transient errors; only fall to []
        // on the initial-load failure path.
        setSites((prev) => prev ?? []);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  useEffect(() => fetchSites(), [fetchSites]);

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  const sitesTabHidden = activeSite.kind === "site";

  const currentTab = tabFromPath(location.pathname);

  // Defer redirect until the initial sites load resolves: a stale single-site
  // picker selection pointing at a now-deleted site would otherwise briefly
  // hide the Sites tab and bounce the operator before useActiveSite's
  // known-id validation resets it to "all".
  useEffect(() => {
    if (sites === undefined) return;
    // Guard against corrupted or older-schema localStorage values so a stale
    // lastTab cannot navigate to /fleet/<garbage>.
    const safeLastTab = lastTab && isFleetTabId(lastTab) ? lastTab : undefined;
    const fallback = sitesTabHidden ? DEFAULT_TAB_NO_SITES : DEFAULT_TAB;
    if (location.pathname === "/fleet" || location.pathname === "/fleet/") {
      const target = safeLastTab && (safeLastTab !== "sites" || !sitesTabHidden) ? safeLastTab : fallback;
      navigate(`/fleet/${target}`, { replace: true });
      return;
    }
    if (currentTab === "sites" && sitesTabHidden) {
      const target = safeLastTab && safeLastTab !== "sites" ? safeLastTab : fallback;
      navigate(`/fleet/${target}`, { replace: true });
    }
  }, [sites, location.pathname, currentTab, sitesTabHidden, lastTab, navigate]);

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

  const visibleTabs = TAB_ORDER.filter((t) => !(t === "sites" && sitesTabHidden));

  const outletContext: FleetOutletContext = useMemo(
    () => ({ sites, sitesError, refetchSites: fetchSites }),
    [sites, sitesError, fetchSites],
  );

  return (
    <div className="flex h-full flex-col" data-testid="fleet-layout">
      <div className="px-10 pt-6 phone:px-6">
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
