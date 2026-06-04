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

  // Site access is gated client-side on the same catalog permission the
  // server enforces (`site:read`). Both ListSites and ListBuildings sit
  // behind PermSiteRead in `server/internal/handlers/middleware/
  // rpc_permissions.go`, so a caller without it can't load either surface.
  // Reading from the catalog (instead of inferring from a failed RPC) keeps
  // transient network / 5xx errors out of the access-blocked branch —
  // those still surface a retry banner on the Sites tab itself.
  const canReadSites = useHasPermission("site:read");

  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(canReadSites ? undefined : []);
  const [sitesError, setSitesError] = useState<string | null>(null);
  // Flips true on the first successful response. Stays true through any
  // later failure so tab consumers can tell "we have last-good data,
  // refresh just failed" from "we've never seen data" — the two states
  // need different UX (empty-state vs full-page error) when sites is [].
  const [sitesLoaded, setSitesLoaded] = useState(false);

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

  // Skip the call entirely for reduced-permission roles — the server would
  // return PermissionDenied anyway and we already know the answer client-side.
  usePoll({ fetchData: fetchSites, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: canReadSites });

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // Hide the Sites tab only when the picker resolves to an existing site.
  // A stale "single site" selection pointing at a now-deleted site (sites
  // list returned empty, useActiveSite couldn't reset because knownSiteIds
  // is empty) must keep the tab visible so the operator can create a new
  // one — see codex review thread on this file.
  const sitesTabHidden = activeSite.kind === "site" && knownSiteIds.has(activeSite.id);

  const currentTab = tabFromPath(location.pathname);

  // Pick the default tab. When the caller can't read sites at all, fall back
  // to Miners — site-gated tabs (Sites + Buildings) would both error on
  // mount.
  const sitesAccessBlocked = !canReadSites;
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
    // Sites/Buildings choice persisted while the flag was on. The access-
    // blocked case additionally rejects Sites and Buildings since neither
    // can load without site:read.
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
    // Active path points at a tab that's no longer visible — could be because
    // sites are hidden under a single-site picker, sites access is blocked, or
    // the multi-site flag was disabled while the operator was on a flagged tab.
    if (currentTab && !isVisibleFleetTabId(currentTab)) {
      navigate(`/fleet/${usableLastTab ?? fallback}`, { replace: true });
      return;
    }
    if (currentTab === "sites" && sitesTabHidden) {
      // Picker is pinned to an existing site. Treat `/fleet/sites` as a
      // shortcut to that site's management surface so the legacy "Manage
      // sites" affordance still has a meaningful landing page.
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

  // Site-gated tabs disappear under two conditions:
  //   - Sites only: picker pinned to an existing site (J2).
  //   - Sites AND Buildings: caller lacks site:read. Both ListSites and
  //     ListBuildings sit behind the same permission, so both pages would
  //     immediately error for these operators.
  const visibleTabs = TAB_ORDER.filter((t) => {
    if (t === "sites" && (sitesTabHidden || sitesAccessBlocked)) return false;
    if (t === "buildings" && sitesAccessBlocked) return false;
    return true;
  });

  const outletContext: FleetOutletContext = useMemo(
    () => ({ sites, sitesError, sitesLoaded, refetchSites: fetchSites }),
    [sites, sitesError, sitesLoaded, fetchSites],
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
