import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import BuildingList from "../components/BuildingList";
import FilterRow from "../components/FilterRow";
import FleetGroupListActionBar from "../components/FleetGroupActionsMenu/FleetGroupListActionBar";
import { useFleetOutletContext } from "../components/FleetLayout";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import {
  intersectSiteFilters,
  isMatchNoneSiteFilter,
  siteFilterFromActive,
  useActiveSite,
} from "@/protoFleet/components/PageHeader/SitePicker";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { usePoll } from "@/shared/hooks/usePoll";

const LIST_WRAPPER = "pt-6";

const FleetBuildingsPage = () => {
  const { sites, sitesError, sitesLoaded, refetchSites } = useFleetOutletContext();

  const { listBuildings } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<string | null>(null);
  const [selectedBuildingIds, setSelectedBuildingIds] = useState<string[]>([]);
  const [isBulkActionBusy, setIsBulkActionBusy] = useState(false);

  const knownSiteIds = useMemo(() => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
  const { activeSite } = useActiveSite({ knownSiteIds });

  // `?site=<id>` deep links filter the list without changing the path
  // scope. Scope and filter compose below.
  const [searchParams] = useSearchParams();
  const urlSiteIds = useMemo(
    () =>
      Array.from(
        new Set(
          searchParams
            .getAll("site")
            .map((value) => value.trim())
            .filter((value) => value !== "" && /^\d+$/.test(value)),
        ),
      ),
    [searchParams],
  );

  // Path scope ∩ `?site=` filter. Both empty + false → server returns every
  // building in the org (rendered straight through, no client filter).
  const requestSiteFilter = useMemo(() => {
    return intersectSiteFilters(siteFilterFromActive(activeSite), {
      siteIds: urlSiteIds.map((id) => BigInt(id)),
      includeUnassigned: false,
    });
  }, [urlSiteIds, activeSite]);
  const requestSiteFilterMatchesNoRows = isMatchNoneSiteFilter(requestSiteFilter);

  // Latest scope, read at response time. usePoll has no per-request
  // cancellation, so a slow ListBuildings for a previous scope can resolve
  // after a newer one; comparing the captured scope against this ref lets
  // the stale (out-of-order) response be ignored instead of clobbering the
  // current scope's rows.
  const requestSiteFilterRef = useRef(requestSiteFilter);
  useEffect(() => {
    requestSiteFilterRef.current = requestSiteFilter;
  }, [requestSiteFilter]);

  // Returning the promise lets usePoll schedule the next tick from response
  // completion (not from request start) so slow responses can't overlap.
  const fetchBuildings = useCallback(() => {
    const requestedFilter = requestSiteFilter; // captured for the staleness check
    if (isMatchNoneSiteFilter(requestedFilter)) {
      setBuildings([]);
      setBuildingsError(null);
      return Promise.resolve();
    }

    return listBuildings({
      siteIds: requestSiteFilter.siteIds,
      includeUnassigned: requestSiteFilter.includeUnassigned,
      onSuccess: (rows) => {
        if (requestSiteFilterRef.current !== requestedFilter) return; // scope changed mid-flight
        setBuildings(rows);
        setBuildingsError(null);
      },
      onError: (msg) => {
        if (requestSiteFilterRef.current !== requestedFilter) return; // scope changed mid-flight
        setBuildingsError(msg);
        // Preserve last-good list across transient errors; only fall to []
        // on the initial-load failure path.
        setBuildings((prev) => prev ?? []);
      },
    });
  }, [listBuildings, requestSiteFilter]);

  // Gate the poll on site:read — same gate FleetLayout uses to redirect.
  const canReadBuildings = useHasPermission("site:read");
  // usePoll keeps fetchData in a ref and doesn't re-run on its identity
  // change, so a site-filter switch wouldn't refetch until the next poll
  // tick. Feed the filter as `params` (a stable string key) so the poll
  // effect restarts immediately when the active site changes.
  const siteFilterKey = useMemo(
    () =>
      `${requestSiteFilter.siteIds.map(String).join(",")}|${requestSiteFilter.includeUnassigned}|${requestSiteFilter.matchNone ?? false}`,
    [requestSiteFilter],
  );
  usePoll({
    fetchData: fetchBuildings,
    params: siteFilterKey,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: canReadBuildings,
  });

  // Drop the previous scope's rows the moment the site filter changes so
  // the now-mismatched buildings can't render (or be selected/edited)
  // under the new scope during the in-flight refetch. Resetting to
  // `undefined` surfaces the Loading… state until the scoped response
  // lands; usePoll's params change fires that fetch immediately.
  const prevSiteFilterKey = useRef(siteFilterKey);
  useEffect(() => {
    if (prevSiteFilterKey.current !== siteFilterKey) {
      prevSiteFilterKey.current = siteFilterKey;
      // eslint-disable-next-line react-hooks/set-state-in-effect -- clearing stale cross-scope rows; external-sync pattern.
      setBuildings(requestSiteFilterMatchesNoRows ? [] : undefined);
      setSelectedBuildingIds([]);
    }
  }, [requestSiteFilterMatchesNoRows, siteFilterKey]);

  // Server-side filter already scoped the list to the active site /
  // URL deep-link; just pass through.
  const visibleBuildings = useMemo(() => buildings ?? [], [buildings]);
  const visibleBuildingScopes = useMemo(
    () =>
      visibleBuildings.flatMap((building) => {
        if (!building.building || building.building.id === 0n) return [];
        return [
          {
            kind: "building" as const,
            id: building.building.id,
            name: building.building.name,
          },
        ];
      }),
    [visibleBuildings],
  );
  const selectedBuildingScopes = useMemo(() => {
    const selected = new Set(selectedBuildingIds);
    return visibleBuildingScopes.filter((building) => selected.has(building.id.toString()));
  }, [selectedBuildingIds, visibleBuildingScopes]);
  useEffect(() => {
    const visible = new Set(visibleBuildingScopes.map((building) => building.id.toString()));
    // Keep selection scoped to the active site / URL filter even when the
    // filtered-empty branch below unmounts BuildingList.
    // eslint-disable-next-line react-hooks/set-state-in-effect -- selection mirrors externally controlled visible rows.
    setSelectedBuildingIds((prev) => {
      const next = prev.filter((id) => visible.has(id));
      return next.length === prev.length ? prev : next;
    });
  }, [visibleBuildingScopes]);
  const handleSelectAllVisibleBuildings = useCallback(
    () => setSelectedBuildingIds(visibleBuildingScopes.map((building) => building.id.toString())),
    [visibleBuildingScopes],
  );
  const handleClearBuildingSelection = useCallback(() => setSelectedBuildingIds([]), []);
  const handleSelectedBuildingIdsChange = useCallback(
    (ids: string[]) => {
      if (isBulkActionBusy) return;
      setSelectedBuildingIds(ids);
    },
    [isBulkActionBusy],
  );

  const buildingModals = useBuildingModals({ refetchBuildings: fetchBuildings });

  // Buildings-tab CTA opens the modal with no pre-filled site — the
  // Site dropdown inside BuildingSettingsModal collects the parent.
  // Site-context auto-fill belongs to /sites/:id, not this global tab.
  const handleAddBuilding = useCallback(() => {
    buildingModals.openDetailsCreate();
  }, [buildingModals]);

  const hasSites = (sites?.filter((s) => s.site !== undefined).length ?? 0) > 0;
  // CreateBuilding requires site:manage server-side.
  const canManageBuildings = useHasPermission("site:manage");

  // Resolve siteName from cache so the modal renders the parent label
  // without a follow-up fetch.
  const siteNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sites ?? []) {
      if (s.site) map.set(s.site.id.toString(), s.site.name);
    }
    return map;
  }, [sites]);
  const openEditBuilding = useCallback(
    (row: BuildingWithCounts) => {
      const siteId = row.building?.siteId;
      const siteName = siteId ? siteNameById.get(siteId.toString()) : undefined;
      buildingModals.openManage(row, siteName);
    },
    [buildingModals, siteNameById],
  );

  const { assignBuildingsToSite } = useSites();
  const [reparentTarget, setReparentTarget] = useState<BuildingWithCounts | null>(null);
  const handleAddBuildingToSite = useCallback((row: BuildingWithCounts) => setReparentTarget(row), []);

  if (buildings === undefined || sites === undefined) {
    return (
      <FilterRow>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </FilterRow>
    );
  }

  if (buildingsError && buildings.length === 0) {
    return (
      <FilterRow testId="fleet-buildings-error">
        <Header title="Couldn't load buildings" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{buildingsError}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={fetchBuildings}
          testId="fleet-buildings-retry"
        />
      </FilterRow>
    );
  }

  const addBuildingButton = canManageBuildings ? (
    <Button
      variant={variants.secondary}
      size={sizes.compact}
      text="Add building"
      onClick={handleAddBuilding}
      disabled={!hasSites}
      testId="fleet-buildings-add"
    />
  ) : null;

  const inlineErrors = (
    <>
      {sitesError ? (
        <Callout
          intent="danger"
          prefixIcon={<Alert />}
          title="Couldn't load sites for the Site column"
          subtitle={sitesError}
          buttonText="Retry"
          buttonOnClick={refetchSites}
          testId="fleet-buildings-sites-error"
        />
      ) : null}
      {buildingsError ? (
        <Callout
          intent="danger"
          prefixIcon={<Alert />}
          title="Couldn't refresh buildings"
          subtitle={buildingsError}
          buttonText="Retry"
          buttonOnClick={fetchBuildings}
          testId="fleet-buildings-inline-error"
        />
      ) : null}
    </>
  );

  const bulkActionBar =
    selectedBuildingScopes.length > 0 || isBulkActionBusy ? (
      <FleetGroupListActionBar
        selectedScopes={selectedBuildingScopes}
        kind="building"
        onClearSelection={handleClearBuildingSelection}
        onSelectAllVisible={handleSelectAllVisibleBuildings}
        onActionBusyChange={setIsBulkActionBusy}
      />
    ) : null;

  // When a site filter is active, the response is scoped — so an empty
  // response could mean "no buildings in this site" rather than "no
  // buildings at all in the org". Differentiate so we don't show the
  // first-time-user CTA inside a filtered scope.
  const hasSiteFilter =
    requestSiteFilter.siteIds.length > 0 || requestSiteFilter.includeUnassigned || requestSiteFilterMatchesNoRows;

  let pageContent: ReactNode;
  if (buildings.length === 0 && !hasSiteFilter) {
    pageContent = (
      <FilterRow testId="fleet-buildings-page">
        <div className="flex items-center justify-end">{addBuildingButton}</div>
        <div className="flex flex-col items-start gap-3 rounded-xl border border-dashed border-border-5 p-6">
          <Header title="No buildings yet" titleSize="text-heading-200" />
          <p className="text-300 text-text-primary-70">
            {!canManageBuildings
              ? "No buildings have been added to this fleet yet."
              : hasSites
                ? "Add a building to start organizing racks."
                : "Create a site first, then add buildings to organize racks."}
          </p>
        </div>
      </FilterRow>
    );
  } else if (visibleBuildings.length === 0) {
    const message =
      activeSite.kind === "unassigned"
        ? "No buildings without a site. Switch the picker to All Sites to see every building."
        : "No buildings in this site yet.";
    pageContent = (
      <FilterRow testId="fleet-buildings-page">
        <div className="flex items-center justify-end">{addBuildingButton}</div>
        <div
          className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
          data-testid="fleet-buildings-filter-empty"
        >
          {message}
        </div>
      </FilterRow>
    );
  } else {
    pageContent = (
      <>
        <FilterRow testId="fleet-buildings-page">
          {inlineErrors}
          <div className="flex items-center justify-end">{addBuildingButton}</div>
        </FilterRow>
        <div className={LIST_WRAPPER}>
          <BuildingList
            buildings={visibleBuildings}
            sites={sites}
            onEditBuilding={canManageBuildings ? openEditBuilding : undefined}
            onAddBuildingToSite={canManageBuildings ? handleAddBuildingToSite : undefined}
            selectedIds={selectedBuildingIds}
            onSelectedIdsChange={handleSelectedBuildingIdsChange}
            activeSite={activeSite}
          />
        </div>
      </>
    );
  }

  return (
    <>
      {pageContent}
      {bulkActionBar}
      <BuildingModals modals={buildingModals} sites={sites} />
      {reparentTarget?.building ? (
        <ParentPickerModal
          kind="site"
          show
          selectionMode="single"
          sourceLabel={reparentTarget.building.name || "building"}
          currentParentId={reparentTarget.building.siteId}
          onDismiss={() => setReparentTarget(null)}
          onConfirm={(siteIds) =>
            new Promise<void>((resolve, reject) => {
              const targetSiteId = siteIds[0];
              if (targetSiteId === undefined || !reparentTarget.building) {
                resolve();
                return;
              }
              const name = reparentTarget.building.name || "building";
              const buildingId = reparentTarget.building.id;
              void assignBuildingsToSite({
                buildingIds: [buildingId],
                targetSiteId,
                onSuccess: () => {
                  pushToast({ message: `Moved "${name}" to selected site.`, status: STATUSES.success });
                  fetchBuildings();
                  setReparentTarget(null);
                  resolve();
                },
                onError: (msg) => {
                  pushToast({ message: `Couldn't move building: ${msg}`, status: STATUSES.error });
                  reject(new Error(msg));
                },
              });
            })
          }
        />
      ) : null}
    </>
  );
};

export default FleetBuildingsPage;
