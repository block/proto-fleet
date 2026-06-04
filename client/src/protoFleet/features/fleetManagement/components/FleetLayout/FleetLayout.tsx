import { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";

import { type FleetOutletContext } from "./outletContext";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { usePoll } from "@/shared/hooks/usePoll";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

type FleetTabId = "sites" | "buildings" | "racks" | "miners";

const TAB_ORDER: FleetTabId[] = ["sites", "buildings", "racks", "miners"];
const DEFAULT_TAB: FleetTabId = "sites";
const DEFAULT_TAB_NO_SITES: FleetTabId = "buildings";
// Used when the operator's role can't load sites (listSites returned an error
// on initial load). Miners is the most universally accessible fleet view, so
// landing there avoids dumping the operator into a permission-error page.
const DEFAULT_TAB_NO_SITES_ACCESS: FleetTabId = "miners";
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

  // Poll listSites on the same cadence as the legacy /sites overview so site
  // renames / deletes / count changes from another session surface here
  // without remounting the route. Returning the promise lets usePoll schedule
  // the next tick from response completion.
  const fetchSites = useCallback(
    () =>
      listSites({
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
      }),
    [listSites],
  );

  usePoll({ fetchData: fetchSites, poll: true, pollIntervalMs: POLL_INTERVAL_MS });

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // Hide the Sites tab only when the picker resolves to an existing site.
  // A stale "single site" selection pointing at a now-deleted site (sites
  // list returned empty, useActiveSite couldn't reset because knownSiteIds
  // is empty) must keep the tab visible so the operator can create a new
  // one — see codex review thread on this file.
  const sitesTabHidden = activeSite.kind === "site" && knownSiteIds.has(activeSite.id);

  const currentTab = tabFromPath(location.pathname);

  // Pick the default tab. When the layout can't load sites at all (initial
  // listSites failed with a permission or transport error and we have no
  // last-good data), fall back to Miners so callers with reduced permissions
  // land somewhere usable. Otherwise prefer the leftmost visible tab.
  const sitesAccessBlocked = sitesError !== null && sites !== undefined && sites.length === 0;
  const fallback = sitesAccessBlocked
    ? DEFAULT_TAB_NO_SITES_ACCESS
    : sitesTabHidden
      ? DEFAULT_TAB_NO_SITES
      : DEFAULT_TAB;

  // Defer redirect until the initial sites load resolves: a stale single-site
  // picker selection pointing at a now-deleted site would otherwise briefly
  // hide the Sites tab and bounce the operator before useActiveSite's
  // known-id validation resets it to "all".
  useEffect(() => {
    if (sites === undefined) return;
    // Guard against corrupted or older-schema localStorage values so a stale
    // lastTab cannot navigate to /fleet/<garbage>.
    const safeLastTab = lastTab && isFleetTabId(lastTab) ? lastTab : undefined;
    // If sites access is blocked, do not honor lastTab="sites" — that route
    // would immediately error.
    const usableLastTab =
      safeLastTab && (safeLastTab !== "sites" || (!sitesTabHidden && !sitesAccessBlocked)) ? safeLastTab : undefined;
    if (location.pathname === "/fleet" || location.pathname === "/fleet/") {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    if (currentTab === "sites" && (sitesTabHidden || sitesAccessBlocked)) {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
    }
  }, [sites, location.pathname, currentTab, sitesTabHidden, sitesAccessBlocked, lastTab, navigate, fallback]);

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

  // Sites tab is suppressed when the picker resolves to an existing site OR
  // when the operator can't load sites at all — both states make the Sites
  // surface unusable.
  const visibleTabs = TAB_ORDER.filter((t) => !(t === "sites" && (sitesTabHidden || sitesAccessBlocked)));

  const outletContext: FleetOutletContext = useMemo(
    () => ({ sites, sitesError, refetchSites: fetchSites }),
    [sites, sitesError, fetchSites],
  );

  // Chrome bands (heading + tab strip) stay pinned during any residual
  // horizontal scroll. Each tab's list controls its own overflow (List
  // defaults to `overflowContainer` = true), but a sticky-left chrome row
  // is the standing convention across all four tabs so any new band added
  // later inherits the right behavior for free.
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
