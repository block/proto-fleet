import { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";

import { type FleetOutletContext } from "./outletContext";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { INFRASTRUCTURE_DEVICES_ENABLED } from "@/protoFleet/constants/featureFlags";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import FleetCreateFlowProvider from "@/protoFleet/features/fleetManagement/components/FleetCreateFlow/FleetCreateFlowProvider";
import FleetViewTabs from "@/protoFleet/features/fleetManagement/components/FleetViewTabs";
import { type FleetTabId } from "@/protoFleet/features/fleetManagement/views/savedViews";
import useFleetViews from "@/protoFleet/features/fleetManagement/views/useFleetViews";
import { type FilterLabelSource } from "@/protoFleet/features/fleetManagement/views/viewSummary";
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import { activeSiteFromScopablePath, scopedPath, unscopedScopablePath } from "@/protoFleet/routing/siteScope";
import { useHasPermission, useUsername } from "@/protoFleet/store";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

const ROUTE_TAB_ORDER: FleetTabId[] = ["sites", "buildings", "racks", "miners", "infrastructure"];
const DISCOVERABLE_TAB_ORDER: FleetTabId[] = ROUTE_TAB_ORDER.filter(
  (tab) => tab !== "infrastructure" || INFRASTRUCTURE_DEVICES_ENABLED,
);
const LAST_TAB_KEY = "fleet:lastActiveTab";

const tabLabel: Record<FleetTabId, string> = {
  miners: "Miners",
  racks: "Racks",
  buildings: "Buildings",
  sites: "Sites",
  infrastructure: "Infrastructure",
};

// Recognize all tab ids regardless of flag so a persisted `lastTab` from a
// flag-on session isn't discarded as garbage when the flag flips.
const ALL_TAB_IDS = new Set<FleetTabId>(["sites", "buildings", "racks", "miners", "infrastructure"]);
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
  const canReadRacks = useHasPermission("rack:read");
  const canReadFleet = useHasPermission("fleet:read");

  // The site catalog (fetch + poll + last-good/permission-denied tracking) is
  // owned by the shell-level SitesProvider and shared with PageHeader and the
  // other routed pages, so FleetLayout just reads it and re-exposes it to its
  // tab children through the outlet context below.
  const { sites, sitesError, sitesLoaded, sitesPermissionDenied, siteCatalogAccessGranted, refetchSites } =
    useSitesContext();

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const validatedKnownSiteIds = sitesLoaded ? knownSiteIds : undefined;
  const { activeSite } = useActiveSite({ knownSiteIds: validatedKnownSiteIds });
  // A stale "single site" selection pointing at a deleted site must keep the
  // tab visible so the operator can still create a new site.
  const sitesTabHidden = activeSite.kind === "site" && (validatedKnownSiteIds?.has(activeSite.id) ?? false);

  const currentTab = tabFromPath(location.pathname);
  const unscopedPath = useMemo(() => unscopedScopablePath(location.pathname), [location.pathname]);
  const onBareFleet = unscopedPath === "/fleet" || unscopedPath === "/fleet/";
  const rawPathScope = useMemo(() => activeSiteFromScopablePath(location.pathname), [location.pathname]);
  const pathScope = useMemo(() => rawPathScope ?? activeSite, [rawPathScope, activeSite]);

  const sitesAccessBlocked = !canReadSites || sitesPermissionDenied;
  const canReadRacksTab = canReadRacks;
  const canReadMinersTab = canReadMiners && canReadRacks && canReadFleet;
  const canReadInfrastructureTab = !sitesAccessBlocked;

  // Permission source of truth for Fleet tabs. Feature flags can hide tab-strip
  // entries, but registered routes stay reachable for authorized deep links.
  const isTabReachable = useCallback(
    (t: FleetTabId) => {
      if (t === "sites" && (sitesTabHidden || sitesAccessBlocked)) return false;
      if (t === "buildings" && sitesAccessBlocked) return false;
      if (t === "racks" && !canReadRacksTab) return false;
      if (t === "miners" && !canReadMinersTab) return false;
      if (t === "infrastructure" && !canReadInfrastructureTab) return false;
      return true;
    },
    [sitesTabHidden, sitesAccessBlocked, canReadRacksTab, canReadMinersTab, canReadInfrastructureTab],
  );
  const reachableTabs = useMemo(() => ROUTE_TAB_ORDER.filter(isTabReachable), [isTabReachable]);
  const visibleTabs = useMemo(() => DISCOVERABLE_TAB_ORDER.filter(isTabReachable), [isTabReachable]);

  // Fallbacks must come from visibleTabs so roles don't get redirected into
  // tabs whose required RPCs they cannot call. Racks stays reachable without
  // site catalog access; its site/building metadata degrades separately.
  const fallbackTab = visibleTabs[0];
  const usableLastTab = lastTab && visibleTabs.includes(lastTab) ? lastTab : undefined;
  const targetTab = usableLastTab ?? fallbackTab;
  const currentTabAllowed = currentTab === undefined || reachableTabs.includes(currentTab);

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
      navigate(scopedPath(`${unscopedPath}${location.search}${location.hash}`, { kind: "all" }), {
        replace: true,
      });
      return;
    }

    // Special shortcut: a pinned single-site picker on /fleet/sites lands on
    // that site's management detail page so legacy "Manage sites" entry
    // points stay useful.
    if (currentTab === "sites" && sitesTabHidden && activeSite.kind === "site") {
      navigate(`/sites/${activeSite.id}`, { replace: true });
      return;
    }

    const currentTabHidden = currentTab !== undefined && !reachableTabs.includes(currentTab);
    if ((onBareFleet || currentTabHidden) && targetTab) {
      navigate(scopedPath(`/fleet/${targetTab}`, pathScope), { replace: true });
    }
  }, [
    sites,
    location.search,
    location.hash,
    currentTab,
    unscopedPath,
    onBareFleet,
    sitesTabHidden,
    activeSite,
    pathScope,
    rawPathScope,
    validatedKnownSiteIds,
    reachableTabs,
    targetTab,
    navigate,
  ]);

  useEffect(() => {
    if (currentTab && visibleTabs.includes(currentTab) && currentTab !== lastTab) {
      setLastTab(currentTab);
    }
  }, [currentTab, lastTab, setLastTab, visibleTabs]);

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
      siteCatalogAccessGranted,
      refetchSites,
      notifyPairingCompleted,
      minersChangedAt,
      publishViewFilterContext,
    }),
    [
      sites,
      sitesError,
      sitesLoaded,
      siteCatalogAccessGranted,
      refetchSites,
      notifyPairingCompleted,
      minersChangedAt,
      publishViewFilterContext,
    ],
  );

  // Mobile docks the views selector beside the Fleet heading to keep the
  // tab nav uncluttered on narrow widths. Desktop lifts it into the
  // TabStrip's trailing slot so it sits right-aligned across from the
  // section tabs. Mounting twice (each gated by a `laptop:` visibility
  // class) keeps the DOM simple — only one is interactive at a time.
  const viewTabs = <FleetViewTabs viewsState={viewsState} currentTab={currentTab} filterContext={viewFilterContext} />;

  const outlet =
    reachableTabs.length === 0 ? (
      <div className="p-6 text-300 text-text-primary-70 laptop:p-10">
        You do not have permission to view Fleet sections.
      </div>
    ) : (onBareFleet || !currentTabAllowed) && visibleTabs.length === 0 ? (
      <div className="p-6 text-300 text-text-primary-70 laptop:p-10">No Fleet sections are currently available.</div>
    ) : !currentTabAllowed ? (
      <div className="p-6 text-300 text-text-primary-70 laptop:p-10">Loading...</div>
    ) : (
      <Outlet context={outletContext} />
    );

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
        <FleetCreateFlowProvider
          sites={sites ?? []}
          refetchSites={refetchSites}
          notifyMinersChanged={notifyMinersChanged}
        >
          {outlet}
        </FleetCreateFlowProvider>
      </div>
    </div>
  );
};

export default FleetLayout;
