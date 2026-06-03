import { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";

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

const useFleetSites = (): SiteWithCounts[] | undefined => {
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);

  // One-shot load is fine here — the Sites tab visibility only depends on
  // whether the org has sites and whether the picker resolves to a real one.
  // Mutations from elsewhere in the app are infrequent and refresh on next
  // navigation; we don't poll here to avoid a competing fetch with whichever
  // tab is mounted.
  useEffect(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => setSites(rows),
      onError: () => {},
    });
    return () => controller.abort();
  }, [listSites]);

  return sites;
};

const FleetLayout = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [lastTab, setLastTab] = useReactiveLocalStorage<FleetTabId | undefined>(LAST_TAB_KEY, undefined);

  const sites = useFleetSites();
  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // Hide the Sites tab once a specific site is picked — J2. "All Sites" and
  // "Unassigned" both keep the tab visible since both modes treat the list
  // as more than one row.
  const sitesTabHidden = activeSite.kind === "site";

  const currentTab = tabFromPath(location.pathname);

  // Bare /fleet → redirect to last active tab (or default). Sites tab hidden
  // under a single-site picker → redirect to another tab.
  useEffect(() => {
    const fallback = sitesTabHidden ? DEFAULT_TAB_NO_SITES : DEFAULT_TAB;
    if (location.pathname === "/fleet" || location.pathname === "/fleet/") {
      const target = lastTab && (lastTab !== "sites" || !sitesTabHidden) ? lastTab : fallback;
      navigate(`/fleet/${target}`, { replace: true });
      return;
    }
    if (currentTab === "sites" && sitesTabHidden) {
      const target = lastTab && lastTab !== "sites" ? lastTab : fallback;
      navigate(`/fleet/${target}`, { replace: true });
    }
  }, [location.pathname, currentTab, sitesTabHidden, lastTab, navigate]);

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
        <Outlet />
      </div>
    </div>
  );
};

export default FleetLayout;
