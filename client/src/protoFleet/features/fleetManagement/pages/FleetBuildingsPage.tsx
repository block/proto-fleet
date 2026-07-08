import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import clsx from "clsx";

import BuildingList from "../components/BuildingList";
import FilterRow from "../components/FilterRow";
import FleetGroupListActionBar from "../components/FleetGroupActionsMenu/FleetGroupListActionBar";
import { useFleetOutletContext } from "../components/FleetLayout";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { issueOptions } from "@/protoFleet/components/DeviceSetList";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import NullState from "@/protoFleet/components/NullState";
import {
  intersectSiteFilters,
  isMatchNoneSiteFilter,
  siteFilterFromActive,
  useActiveSite,
} from "@/protoFleet/components/PageHeader/SitePicker";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { useFleetCreateFlow } from "@/protoFleet/features/fleetManagement/components/FleetCreateFlow/context";
import {
  FILTER_URL_PARAM_KEYS,
  fleetListTelemetryRangesFromURL,
  issueComponentTypesFromURL,
  parseIdFilterValuesFromURL,
  parseUrlToActiveFilters,
  setTelemetryNumericFilterURLParams,
  UNASSIGNED_FILTER_OPTION,
  UNASSIGNED_URL_VALUE,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import {
  TELEMETRY_FILTER_BOUNDS,
  TELEMETRY_FILTER_KEYS,
  type TelemetryFilterKey,
} from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";
import { useHasPermission } from "@/protoFleet/store";
import { Alert, Building, Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import FilterChipsBar, { type FilterChipsBarNumericFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { usePoll } from "@/shared/hooks/usePoll";
import type { NumericRangeValue } from "@/shared/utils/filterValidation";

const LIST_WRAPPER = "pt-6";

const TELEMETRY_FILTER_CHIPS: FilterChipsBarNumericFilter[] = TELEMETRY_FILTER_KEYS.map((key) => ({
  key,
  title: TELEMETRY_FILTER_BOUNDS[key].label,
  bounds: TELEMETRY_FILTER_BOUNDS[key],
}));

const FleetBuildingsPage = () => {
  const { sites, sitesError, siteCatalogAccessGranted, refetchSites } = useFleetOutletContext();

  const { listBuildings } = useBuildings();
  const [searchParams, setSearchParams] = useSearchParams();
  const errorComponentTypes = useMemo(() => issueComponentTypesFromURL(searchParams), [searchParams]);
  const telemetryRanges = useMemo(() => fleetListTelemetryRangesFromURL(searchParams), [searchParams]);
  const selectedNumericValues = useMemo(() => parseUrlToActiveFilters(searchParams).numericFilters, [searchParams]);
  const selectedIssues = useMemo(
    () =>
      Array.from(
        new Set(
          searchParams
            .getAll("issues")
            .map((v) => v.trim())
            .filter(Boolean),
        ),
      ),
    [searchParams],
  );
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<string | null>(null);
  const [selectedBuildingIds, setSelectedBuildingIds] = useState<string[]>([]);
  const [isBulkActionBusy, setIsBulkActionBusy] = useState(false);

  // Validate scope against catalog access (authoritative now), not sitesLoaded:
  // a mid-session PermissionDenied clears `sites` to [] with sitesLoaded still
  // true, which would otherwise strip a reachable scoped route.
  const knownSiteIds = useMemo(
    () => (siteCatalogAccessGranted ? buildKnownSiteIds(sites) : undefined),
    [siteCatalogAccessGranted, sites],
  );
  const { activeSite } = useActiveSite({ knownSiteIds });

  // `?site=<id>` deep links filter the list without changing the path
  // scope. Scope and filter compose below.
  const urlSiteFilter = useMemo(() => parseIdFilterValuesFromURL(searchParams, "site"), [searchParams]);
  const urlSiteIds = useMemo(
    () => urlSiteFilter.values.filter((value) => value !== UNASSIGNED_URL_VALUE),
    [urlSiteFilter],
  );
  const hasActiveFilters = urlSiteFilter.values.length > 0 || selectedIssues.length > 0 || telemetryRanges.length > 0;

  // Path scope ∩ `?site=` filter. Both empty + false → server returns every
  // building in the org (rendered straight through, no client filter).
  const requestSiteFilter = useMemo(() => {
    return intersectSiteFilters(siteFilterFromActive(activeSite), {
      siteIds: urlSiteIds.map((id) => BigInt(id)),
      includeUnassigned: urlSiteFilter.includeUnassigned,
    });
  }, [urlSiteFilter.includeUnassigned, urlSiteIds, activeSite]);
  const requestSiteFilterMatchesNoRows = isMatchNoneSiteFilter(requestSiteFilter);

  // usePoll keeps fetchData in a ref and doesn't re-run on its identity
  // change, so a site/filter switch wouldn't refetch until the next poll
  // tick. Feed the full request filter as `params` (a stable string key) so
  // the poll effect restarts immediately when the active site or filter URL
  // changes.
  const listFilterKey = useMemo(() => {
    const telemetryKey = telemetryRanges
      .map(
        (range) =>
          `${range.field}:${range.min ?? ""}:${range.max ?? ""}:${range.minInclusive ? "1" : "0"}:${range.maxInclusive ? "1" : "0"}`,
      )
      .join(",");
    return [
      requestSiteFilter.siteIds.map(String).join(","),
      requestSiteFilter.includeUnassigned ? "1" : "0",
      requestSiteFilter.matchNone ? "1" : "0",
      errorComponentTypes.join(","),
      telemetryKey,
    ].join("|");
  }, [requestSiteFilter, errorComponentTypes, telemetryRanges]);

  // Latest request key/id, read at response time. usePoll has no
  // per-request cancellation, and manual modal refreshes can overlap an
  // in-flight poll, so a slow ListBuildings response can resolve after a
  // newer one. The key rejects old filters; the request id rejects older
  // same-filter refreshes.
  const listFilterKeyRef = useRef(listFilterKey);
  const listRequestIdRef = useRef(0);
  useEffect(() => {
    listFilterKeyRef.current = listFilterKey;
  }, [listFilterKey]);

  // Unfiltered building count for the "X of Y buildings" line — the path/site
  // scope alone, no issue/telemetry/`?site=` filters. Fetched alongside the
  // filtered list inside fetchBuildings (below) so the denominator refreshes on
  // exactly the same triggers as the rows it's compared against: poll ticks,
  // filter/scope changes, create-flow pulses, and modal mutations. Its own
  // request-id guard rejects out-of-order count responses.
  const scopeOnlyFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);
  const [totalUnfilteredBuildings, setTotalUnfilteredBuildings] = useState<number | undefined>(undefined);
  const unfilteredCountRequestIdRef = useRef(0);

  // Returning the promise lets usePoll schedule the next tick from response
  // completion (not from request start) so slow responses can't overlap.
  const fetchBuildings = useCallback(() => {
    const requestedFilterKey = listFilterKey; // captured for the staleness check
    const requestId = ++listRequestIdRef.current;
    if (isMatchNoneSiteFilter(requestSiteFilter)) {
      setBuildings([]);
      setBuildingsError(null);
      return Promise.resolve();
    }

    // Refresh the unfiltered denominator on the same beat as the filtered list.
    // Only needed while filters are active (otherwise the displayed count is
    // already the total).
    if (hasActiveFilters && !isMatchNoneSiteFilter(scopeOnlyFilter)) {
      const countRequestId = ++unfilteredCountRequestIdRef.current;
      void listBuildings({
        siteIds: scopeOnlyFilter.siteIds,
        includeUnassigned: scopeOnlyFilter.includeUnassigned,
        onSuccess: (rows) => {
          if (countRequestId === unfilteredCountRequestIdRef.current) setTotalUnfilteredBuildings(rows.length);
        },
        onError: () => {
          if (countRequestId === unfilteredCountRequestIdRef.current) setTotalUnfilteredBuildings(undefined);
        },
      });
    }

    return listBuildings({
      siteIds: requestSiteFilter.siteIds,
      includeUnassigned: requestSiteFilter.includeUnassigned,
      errorComponentTypes,
      telemetryRanges,
      onSuccess: (rows) => {
        if (requestId !== listRequestIdRef.current || listFilterKeyRef.current !== requestedFilterKey) return;
        setBuildings(rows);
        setBuildingsError(null);
      },
      onError: (msg) => {
        if (requestId !== listRequestIdRef.current || listFilterKeyRef.current !== requestedFilterKey) return;
        setBuildingsError(msg);
        // Preserve last-good list across transient errors; only fall to []
        // on the initial-load failure path.
        setBuildings((prev) => prev ?? []);
      },
    });
  }, [
    listBuildings,
    requestSiteFilter,
    errorComponentTypes,
    telemetryRanges,
    listFilterKey,
    hasActiveFilters,
    scopeOnlyFilter,
  ]);

  // Gate the poll on site:read — same gate FleetLayout uses to redirect.
  const canReadBuildings = useHasPermission("site:read");
  usePoll({
    fetchData: fetchBuildings,
    params: listFilterKey,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: canReadBuildings,
  });

  // Drop the previous scope's rows the moment the site filter changes so
  // the now-mismatched buildings can't render (or be selected/edited)
  // under the new scope during the in-flight refetch. Resetting to
  // `undefined` surfaces the Loading… state until the scoped response
  // lands; usePoll's params change fires that fetch immediately.
  const prevListFilterKey = useRef(listFilterKey);
  useEffect(() => {
    if (prevListFilterKey.current !== listFilterKey) {
      prevListFilterKey.current = listFilterKey;
      // eslint-disable-next-line react-hooks/set-state-in-effect -- clearing stale cross-scope rows; external-sync pattern.
      setBuildings(requestSiteFilterMatchesNoRows ? [] : undefined);
      setSelectedBuildingIds([]);
    }
  }, [listFilterKey, requestSiteFilterMatchesNoRows]);

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
            // Rides along for the "New site" conflict pre-warning.
            siteId: building.building.siteId ?? 0n,
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
  const createFlow = useFleetCreateFlow();

  // Buildings-tab CTA opens the modal with no pre-filled site — the
  // Site dropdown inside BuildingSettingsModal collects the parent.
  // Site-context auto-fill belongs to /sites/:id, not this global tab.
  //
  // Route through the create flow (empty seed) so a freshly created building
  // advances to ManageBuildingModal for rack/miner positioning. The plain
  // openDetailsCreate path closes to "none" on save with no manage step, so
  // it's only the fallback for standalone mounts without the flow provider.
  const handleAddBuilding = useCallback(() => {
    if (createFlow) {
      createFlow.launchCreateBuilding({ rackIds: [], minerIds: [], conflictCount: 0 });
      return;
    }
    // Pre-fill (and lock to) the page-header site scope so a new building
    // belongs to the site the operator is viewing. Unscoped → editable, and
    // the operator must pick a site.
    const scopedSiteId = activeSite.kind === "site" ? BigInt(activeSite.id) : undefined;
    buildingModals.openDetailsCreate(scopedSiteId);
  }, [createFlow, buildingModals, activeSite]);

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
  const [bulkAddToSite, setBulkAddToSite] = useState(false);
  const handleAddBuildingToSite = useCallback((row: BuildingWithCounts) => setReparentTarget(row), []);

  // Open the hoisted create flow in place when it commits a new entity.
  const entitiesChangedAt = createFlow?.entitiesChangedAt ?? 0;
  useEffect(() => {
    if (entitiesChangedAt === 0) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- refetch on a cross-component create signal; external-sync pattern.
    fetchBuildings();
  }, [entitiesChangedAt, fetchBuildings]);

  const siteFilterOptions = useMemo(
    () => [
      ...(sites ?? [])
        .filter((site) => site.site !== undefined)
        .map((site) => ({ id: site.site!.id.toString(), label: site.site!.name })),
      UNASSIGNED_FILTER_OPTION,
    ],
    [sites],
  );

  const filterChipsBarFilters = useMemo(
    () => [
      {
        key: "issues",
        title: "Issues",
        pluralTitle: "issues",
        options: issueOptions,
        selectedValues: selectedIssues,
        showGroupDivider: true,
      },
      {
        key: "site",
        title: "Sites",
        pluralTitle: "sites",
        options: siteFilterOptions,
        selectedValues: urlSiteFilter.values,
        showGroupDivider: true,
      },
    ],
    [selectedIssues, siteFilterOptions, urlSiteFilter.values],
  );

  const writeMultiParam = useCallback(
    (key: string, values: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete(key);
          values.forEach((value) => {
            const trimmed = value.trim();
            if (trimmed) next.append(key, trimmed);
          });
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const handleFilterChange = useCallback(
    (key: string, values: string[]) => {
      if (key === "site") {
        writeMultiParam("site", values);
        return;
      }
      if (key === "issues") {
        writeMultiParam("issues", values);
      }
    },
    [writeMultiParam],
  );

  const handleNumericFilterChange = useCallback(
    (key: string, value: NumericRangeValue) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          setTelemetryNumericFilterURLParams(next, key as TelemetryFilterKey, value);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const handleClearFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        FILTER_URL_PARAM_KEYS.forEach((key) => next.delete(key));
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

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

  const filterControls = (
    <div className="flex flex-row flex-wrap items-center gap-2">
      <FilterChipsBar
        filters={filterChipsBarFilters}
        onChange={handleFilterChange}
        numericFilters={TELEMETRY_FILTER_CHIPS}
        selectedNumericValues={selectedNumericValues}
        onNumericChange={handleNumericFilterChange}
        onClearAll={handleClearFilters}
      />
      <div className="ml-auto">{addBuildingButton}</div>
    </div>
  );

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
        bulkExtraActions={[
          {
            label: "Add to site",
            icon: <Plus />,
            testId: "fleet-bulk-building-actions-add-to-site",
            onClick: () => setBulkAddToSite(true),
            hidden: !canManageBuildings,
          },
        ]}
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
  if (buildings.length === 0) {
    pageContent = hasActiveFilters ? (
      <FilterRow testId="fleet-buildings-page">
        {inlineErrors}
        {filterControls}
        <NoFilterResultsEmptyState hasActiveFilters onClearFilters={handleClearFilters} />
      </FilterRow>
    ) : hasSiteFilter ? (
      <FilterRow testId="fleet-buildings-page">
        {filterControls}
        <div
          className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
          data-testid="fleet-buildings-filter-empty"
        >
          {activeSite.kind === "unassigned"
            ? "No buildings without a site. Switch the picker to All Sites to see every building."
            : "No buildings in this site yet."}
        </div>
      </FilterRow>
    ) : (
      <>
        {inlineErrors}
        <NullState
          className={clsx("sticky left-0", PAGE_SCROLL_CHROME_WIDTH)}
          icon={<Building width="w-5" />}
          title="No buildings yet"
          description={
            !canManageBuildings
              ? "No buildings have been added to this fleet yet."
              : hasSites
                ? "Add a building to start organizing racks."
                : "Create a site first, then add buildings to organize racks."
          }
          action={
            canManageBuildings ? (
              <Button variant={variants.primary} onClick={handleAddBuilding} disabled={!hasSites} text="Add building" />
            ) : undefined
          }
          testId="fleet-buildings-page"
        />
      </>
    );
  } else if (visibleBuildings.length === 0) {
    pageContent = (
      <FilterRow testId="fleet-buildings-page">
        {filterControls}
        {hasActiveFilters ? (
          <NoFilterResultsEmptyState hasActiveFilters onClearFilters={handleClearFilters} />
        ) : (
          <div
            className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
            data-testid="fleet-buildings-filter-empty"
          >
            {activeSite.kind === "unassigned"
              ? "No buildings without a site. Switch the picker to All Sites to see every building."
              : "No buildings in this site yet."}
          </div>
        )}
      </FilterRow>
    );
  } else {
    pageContent = (
      <>
        <FilterRow testId="fleet-buildings-page">
          {inlineErrors}
          {filterControls}
        </FilterRow>
        <div className={LIST_WRAPPER}>
          <BuildingList
            buildings={visibleBuildings}
            sites={sites}
            totalUnfiltered={totalUnfilteredBuildings}
            hasActiveFilters={hasActiveFilters}
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
          createNewLaunchLabel={createFlow ? "New site" : undefined}
          onCreateNewLaunch={
            createFlow
              ? () => {
                  const building = reparentTarget.building;
                  if (!building) return;
                  const conflictCount = building.siteId !== undefined && building.siteId !== 0n ? 1 : 0;
                  setReparentTarget(null);
                  createFlow.launchCreateSite({ buildingIds: [building.id], rackIds: [], minerIds: [], conflictCount });
                }
              : undefined
          }
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
      {bulkAddToSite ? (
        <ParentPickerModal
          kind="site"
          show
          selectionMode="single"
          sourceLabel={
            selectedBuildingScopes.length === 1
              ? selectedBuildingScopes[0]!.name
              : `${selectedBuildingScopes.length} buildings`
          }
          createNewLaunchLabel={createFlow ? "New site" : undefined}
          onCreateNewLaunch={
            createFlow
              ? () => {
                  const buildingIds = selectedBuildingScopes.map((scope) => scope.id);
                  const conflictCount = selectedBuildingScopes.filter((scope) => scope.siteId !== 0n).length;
                  setBulkAddToSite(false);
                  setSelectedBuildingIds([]);
                  createFlow.launchCreateSite({ buildingIds, rackIds: [], minerIds: [], conflictCount });
                }
              : undefined
          }
          onDismiss={() => setBulkAddToSite(false)}
          onConfirm={(siteIds) =>
            new Promise<void>((resolve, reject) => {
              const targetSiteId = siteIds[0];
              if (targetSiteId === undefined) {
                resolve();
                return;
              }
              const buildingIds = selectedBuildingScopes.map((scope) => scope.id);
              const count = buildingIds.length;
              void assignBuildingsToSite({
                buildingIds,
                targetSiteId,
                onSuccess: () => {
                  pushToast({
                    message: `Moved ${count} ${count === 1 ? "building" : "buildings"} to selected site.`,
                    status: STATUSES.success,
                  });
                  fetchBuildings();
                  setBulkAddToSite(false);
                  setSelectedBuildingIds([]);
                  resolve();
                },
                onError: (msg) => {
                  pushToast({ message: `Couldn't move buildings: ${msg}`, status: STATUSES.error });
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
