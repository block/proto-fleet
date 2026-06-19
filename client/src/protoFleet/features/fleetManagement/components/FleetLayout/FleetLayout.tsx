import { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";
import { Code } from "@connectrpc/connect";

import { type FleetOutletContext } from "./outletContext";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import FleetViewTabs from "@/protoFleet/features/fleetManagement/components/FleetViewTabs";
import { type FleetTabId } from "@/protoFleet/features/fleetManagement/views/savedViews";
import useFleetViews from "@/protoFleet/features/fleetManagement/views/useFleetViews";
import { type FilterLabelSource } from "@/protoFleet/features/fleetManagement/views/viewSummary";
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import { activeSiteFromScopablePath, scopedPath, unscopedScopablePath } from "@/protoFleet/routing/siteScope";
import { useHasPermission, useUsername } from "@/protoFleet/store";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { usePoll } from "@/shared/hooks/usePoll";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

const TAB_ORDER: FleetTabId[] = MULTI_SITE_ENABLED ? ["sites", "buildings", "racks", "miners"] : ["racks", "miners"];
// Absolute last-resort fallback when TAB_ORDER somehow contains no visible
// tabs. The real waterfall comes from `visibleTabs[0]` below.
const DEFAULT_TAB: FleetTabId = MULTI_SITE_ENABLED ? "sites" : "racks";
// When site access is blocked entirely, fall to Miners specifically — Racks
// can also be permission-gated (rack:read), so picking the first visible
// tab isn't safe enough for the access-blocked branch.
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

const tabFromPath = (pathname: string): FleetTabId | undefined => {
  const m = unscopedScopablePath(pathname).match(/^\/fleet\/([^/]+)/);
  if (!m) return undefined;
  return isFleetTabId(m[1]) ? m[1] : undefined;
};

const FleetLayout = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const username = useUsername();
  const viewsState = useFleetViews(username);
  const [lastTab, setLastTab] = useReactiveLocalStorage<FleetTabId | undefined>(LAST_TAB_KEY, undefined);

  // ListSites and ListBuildings both sit behind PermSiteRead server-side.
  // Reading from the catalog (instead of inferring from a failed RPC) keeps
  // transient transport errors out of the access-blocked branch.
  const canReadSites = useHasPermission("site:read");
  // CompleteSetup calls ListMinerStateSnapshots (gated on PermMinerRead) via
  // useAuthNeededMiners + usePoolNeededCount before deciding whether to show.
  // Skip the banner entirely for roles without miner:read so they don't get
  // permission-denied toasts just by opening a non-miner Fleet tab.
  const canReadMiners = useHasPermission("miner:read");

  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(canReadSites ? undefined : []);
  const [sitesError, setSitesError] = useState<string | null>(null);
  // Stays true once any listSites response succeeds, even through later
  // failures. Lets consumers tell "we have last-good data" from "we've
  // never seen data" when sites is [].
  const [sitesLoaded, setSitesLoaded] = useState(false);
  // useHasPermission is a flat union across scopes, so a user with only
  // site-scoped site:read passes `canReadSites` even though ListSites is
  // gated on org-scoped site:read. Flip this flag when the RPC returns
  // PermissionDenied so the redirect waterfall can fall back to Miners.
  const [sitesPermissionDenied, setSitesPermissionDenied] = useState(false);

  const fetchSites = useCallback(
    () =>
      listSites({
        onSuccess: (rows) => {
          setSites(rows);
          setSitesError(null);
          setSitesLoaded(true);
          setSitesPermissionDenied(false);
        },
        onError: (msg, code) => {
          setSitesError(msg);
          if (code === Code.PermissionDenied) {
            setSitesPermissionDenied(true);
          }
          // Preserve last-good list across transient errors; only fall to []
          // on the initial-load failure path.
          setSites((prev) => prev ?? []);
        },
      }),
    [listSites],
  );

  usePoll({ fetchData: fetchSites, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: canReadSites });

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const validatedKnownSiteIds = sitesLoaded ? knownSiteIds : undefined;
  const { activeSite } = useActiveSite({ knownSiteIds: validatedKnownSiteIds });
  // A stale "single site" selection pointing at a deleted site must keep the
  // tab visible so the operator can still create a new site.
  const sitesTabHidden = activeSite.kind === "site" && (validatedKnownSiteIds?.has(activeSite.id) ?? false);

  const currentTab = tabFromPath(location.pathname);
  const rawPathScope = useMemo(() => activeSiteFromScopablePath(location.pathname), [location.pathname]);
  const pathScope = useMemo(() => rawPathScope ?? activeSite, [rawPathScope, activeSite]);

  const sitesAccessBlocked = !canReadSites || sitesPermissionDenied;

  // Source of truth for "which tabs are reachable right now." Hide-rule
  // changes only need to touch this filter; the redirect effect, the tab
  // strip, and the lastTab guard all derive from it.
  const visibleTabs = useMemo(
    () =>
      TAB_ORDER.filter((t) => {
        if (t === "sites" && (sitesTabHidden || sitesAccessBlocked)) return false;
        if (t === "buildings" && sitesAccessBlocked) return false;
        return true;
      }),
    [sitesTabHidden, sitesAccessBlocked],
  );

  // Fallback waterfall mirrors the legacy DEFAULT_TAB_* ordering: prefer the
  // first visible tab, but force Miners when site access is blocked since
  // Racks may also be permission-gated (rack:read) for that role. Stored
  // lastTab is ignored in the access-blocked branch for the same reason —
  // a persisted "racks" pick must not override the Miners safe path.
  const fallbackTab = sitesAccessBlocked ? DEFAULT_TAB_NO_SITES_ACCESS : (visibleTabs[0] ?? DEFAULT_TAB);
  const usableLastTab = !sitesAccessBlocked && lastTab && visibleTabs.includes(lastTab) ? lastTab : undefined;
  const targetTab = usableLastTab ?? fallbackTab;

  // Defer redirect until the initial sites load resolves so a stale
  // single-site picker selection doesn't briefly hide the Sites tab before
  // useActiveSite's known-id validation can reset it.
  useEffect(() => {
    if (sites === undefined) return;

    if (
      rawPathScope?.kind === "site" &&
      validatedKnownSiteIds !== undefined &&
      !validatedKnownSiteIds.has(rawPathScope.id)
    ) {
      navigate(
        scopedPath(`${unscopedScopablePath(location.pathname)}${location.search}${location.hash}`, { kind: "all" }),
        {
          replace: true,
        },
      );
      return;
    }

    // Special shortcut: a pinned single-site picker on /fleet/sites lands on
    // that site's management detail page so legacy "Manage sites" entry
    // points stay useful.
    if (currentTab === "sites" && sitesTabHidden && activeSite.kind === "site") {
      navigate(`/sites/${activeSite.id}`, { replace: true });
      return;
    }

    const unscopedPath = unscopedScopablePath(location.pathname);
    const onBareFleet = unscopedPath === "/fleet" || unscopedPath === "/fleet/";
    const currentTabHidden = currentTab !== undefined && !visibleTabs.includes(currentTab);
    if (onBareFleet || currentTabHidden) {
      navigate(scopedPath(`/fleet/${targetTab}`, pathScope), { replace: true });
    }
  }, [
    sites,
    location.pathname,
    location.search,
    location.hash,
    currentTab,
    sitesTabHidden,
    activeSite,
    pathScope,
    rawPathScope,
    validatedKnownSiteIds,
    visibleTabs,
    targetTab,
    navigate,
  ]);

  useEffect(() => {
    if (currentTab && currentTab !== lastTab) {
      setLastTab(currentTab);
    }
  }, [currentTab, lastTab, setLastTab]);

  const onSelect = useCallback(
    (id: string) => {
      if (isFleetTabId(id)) navigate(scopedPath(`/fleet/${id}`, pathScope));
    },
    [navigate, pathScope],
  );

  const [viewFilterContext, setViewFilterContext] = useState<{
    availableGroups: DeviceSet[];
    availableRacks: DeviceSet[];
    availableBuildings: FilterLabelSource[];
    availableSites: FilterLabelSource[];
  }>({ availableGroups: [], availableRacks: [], availableBuildings: [], availableSites: [] });
  // Partial publish: a child tab only overwrites the keys it knows about,
  // so racks publishing buildings doesn't clobber miners' group/rack lists.
  const publishViewFilterContext = useCallback<FleetOutletContext["publishViewFilterContext"]>((ctx) => {
    setViewFilterContext((prev) => {
      const next = {
        availableGroups: ctx.availableGroups ?? prev.availableGroups,
        availableRacks: ctx.availableRacks ?? prev.availableRacks,
        availableBuildings: ctx.availableBuildings ?? prev.availableBuildings,
        availableSites: ctx.availableSites ?? prev.availableSites,
      };
      const unchanged =
        next.availableGroups === prev.availableGroups &&
        next.availableRacks === prev.availableRacks &&
        next.availableBuildings === prev.availableBuildings &&
        next.availableSites === prev.availableSites;
      return unchanged ? prev : next;
    });
  }, []);

  // Pairing/refetch coordination with the Miners tab. The chrome-level
  // CompleteSetup banner outlives any single tab, so the timestamp pulses
  // live here and surface to tab children via outlet context.
  const [lastPairingCompletedAt, setLastPairingCompletedAt] = useState(0);
  const [minersChangedAt, setMinersChangedAt] = useState(0);
  const notifyPairingCompleted = useCallback(() => setLastPairingCompletedAt(Date.now()), []);
  const notifyMinersChanged = useCallback(() => setMinersChangedAt(Date.now()), []);

  const outletContext: FleetOutletContext = useMemo(
    () => ({
      sites,
      sitesError,
      sitesLoaded,
      refetchSites: fetchSites,
      notifyPairingCompleted,
      minersChangedAt,
      publishViewFilterContext,
    }),
    [sites, sitesError, sitesLoaded, fetchSites, notifyPairingCompleted, minersChangedAt, publishViewFilterContext],
  );

  // Mobile docks the views selector beside the Fleet heading to keep the
  // tab nav uncluttered on narrow widths. Desktop lifts it into the
  // TabStrip's trailing slot so it sits right-aligned across from the
  // section tabs. Mounting twice (each gated by a `laptop:` visibility
  // class) keeps the DOM simple — only one is interactive at a time.
  const viewTabs = <FleetViewTabs viewsState={viewsState} currentTab={currentTab} filterContext={viewFilterContext} />;

  return (
    // w-max + min-w-full: the subtree grows to the widest tab content (a wide
    // table), which is what gives the sticky-left chrome below room to slide.
    // min-w-full keeps it at least viewport-wide when content is narrow.
    <div className="flex h-full w-max min-w-full flex-col" data-testid="fleet-layout">
      <div
        className={clsx(
          "sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-6 laptop:px-10",
          PAGE_SCROLL_CHROME_WIDTH,
        )}
      >
        <div className="flex items-baseline justify-between gap-4">
          <h1 className="text-heading-300 text-text-primary">Fleet</h1>
          <div className="laptop:hidden">{viewTabs}</div>
        </div>
        {canReadMiners ? (
          <CompleteSetup
            lastPairingCompletedAt={lastPairingCompletedAt}
            onPairingCompleted={notifyPairingCompleted}
            onRefetchMiners={notifyMinersChanged}
          />
        ) : null}
        <TabStrip
          activeId={currentTab}
          onSelect={onSelect}
          ariaLabel="Fleet sections"
          trailing={<div className="hidden pb-2 laptop:block">{viewTabs}</div>}
        >
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
