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
// Default lands on the leftmost tab — Sites — to mirror the fleet
// hierarchy (site → building → rack → device). Falls back to the next
// visible tab when Sites is hidden under a single-site picker.
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

// Extracts the tab segment from a /fleet/* path. Returns undefined for bare
// /fleet so the caller can apply the redirect rule.
const tabFromPath = (pathname: string): FleetTabId | undefined => {
  const m = pathname.match(/^\/fleet\/([^/]+)/);
  if (!m) return undefined;
  return isFleetTabId(m[1]) ? m[1] : undefined;
};

const FleetLayout = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [lastTab, setLastTab] = useReactiveLocalStorage<FleetTabId | undefined>(LAST_TAB_KEY, undefined);

  // Sites list lives at the layout level so the Sites-tab visibility check
  // and the tab pages share one fetch. Previously each tab page issued its
  // own listSites RPC alongside the layout's, producing 2-3 identical
  // in-flight requests on /fleet/sites and /fleet/buildings.
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
        // Preserve last-good list on transient errors; only clear it on the
        // initial-load failure path so consumers can distinguish "no sites"
        // from "fetch failed and we have nothing to show".
        setSites((prev) => prev ?? []);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  useEffect(() => fetchSites(), [fetchSites]);

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // Hide the Sites tab once a specific site is picked — J2. "All Sites" and
  // "Unassigned" both keep the tab visible since both modes treat the list
  // as more than one row.
  const sitesTabHidden = activeSite.kind === "site";

  const currentTab = tabFromPath(location.pathname);

  // Bare /fleet → redirect to last active tab (or default). Sites tab hidden
  // under a single-site picker → redirect to another tab. Wait for the
  // initial sites load so a stale "single site" picker selection pointing at
  // a now-deleted site doesn't briefly hide the Sites tab and redirect away
  // before useActiveSite's known-id validation effect can reset it to "all".
  useEffect(() => {
    if (sites === undefined) return;
    // Validate the persisted lastTab — corrupted or older-schema localStorage
    // values must not navigate into /fleet/<garbage>.
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

  // Persist the active tab so /fleet bare-route and cross-session reopens
  // land on the operator's last view.
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

  // Tab strip sits in its own flush band with horizontal padding matching
  // the rest of the layout (`p-10` / `p-6`). Children render flush; each
  // tab page owns its own internal padding (the existing Miners + Racks
  // pages already do this, and the new Buildings/Sites stubs match).
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
