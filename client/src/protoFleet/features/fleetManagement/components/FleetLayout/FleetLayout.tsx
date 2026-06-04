import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";

import { type FleetOutletContext } from "./outletContext";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { usePoll } from "@/shared/hooks/usePoll";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

type FleetTabId = "sites" | "buildings" | "racks" | "miners";

// Sites and Buildings tabs only ship under the multi-site flag. The routes
// remain mounted unconditionally (router.tsx + redirectLoaders) so direct-URL
// access still works during dogfood, but the tab nav hides them off-flag so
// half-implemented multi-site UX doesn't reach single-site installs.
const TAB_ORDER: FleetTabId[] = MULTI_SITE_ENABLED ? ["sites", "buildings", "racks", "miners"] : ["racks", "miners"];
// Default tab picks the leftmost visible in TAB_ORDER so the flag-off shell
// (no Sites/Buildings) lands on Racks, while flag-on lands on Sites.
const DEFAULT_TAB: FleetTabId = MULTI_SITE_ENABLED ? "sites" : "racks";
const DEFAULT_TAB_NO_SITES: FleetTabId = MULTI_SITE_ENABLED ? "buildings" : "racks";
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

// All four ids stay valid here so a localStorage `lastTab` value persisted
// while the flag was on doesn't get treated as garbage when the flag flips.
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

  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [sitesError, setSitesError] = useState<string | null>(null);
  // Tracks whether any listSites call has succeeded. Distinguishes a
  // zero-site org (loaded once, returned []) from a hard-blocked caller
  // (never loaded — likely permissions or transport). Without this, a
  // transient poll error on a zero-site org would flip sitesAccessBlocked
  // and hide the only Sites-tab CTA for creating the first site.
  const sitesLoadedRef = useRef(false);

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
          sitesLoadedRef.current = true;
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

  // Pick the default tab. When the layout has never successfully loaded sites
  // (initial listSites failed with a permission or transport error), fall
  // back to Miners so callers with reduced permissions land somewhere
  // usable. A zero-site org that loaded successfully then hit a poll error
  // still has Sites access — sitesLoadedRef distinguishes the two states.
  const sitesAccessBlocked =
    sitesError !== null && sites !== undefined && sites.length === 0 && !sitesLoadedRef.current;
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
    // lastTab cannot navigate to /fleet/<garbage>. Honor only tabs that are
    // currently visible (TAB_ORDER) — flag-off installs shouldn't replay a
    // Sites/Buildings choice persisted while the flag was on.
    const safeLastTab = lastTab && isVisibleFleetTabId(lastTab) ? lastTab : undefined;
    const usableLastTab =
      safeLastTab && (safeLastTab !== "sites" || (!sitesTabHidden && !sitesAccessBlocked)) ? safeLastTab : undefined;
    if (location.pathname === "/fleet" || location.pathname === "/fleet/") {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    // Active path points at a tab that's no longer visible — could be because
    // sites are hidden under a single-site picker, sites access is blocked, or
    // the multi-site flag was disabled while the operator was on a flagged tab.
    if (currentTab && !isVisibleFleetTabId(currentTab)) {
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
