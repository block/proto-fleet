import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useSearchParams } from "react-router-dom";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import type { DeviceSetListItem } from "@/protoFleet/components/DeviceSetList";
import type { DeviceSetColumn } from "@/protoFleet/components/DeviceSetList";
import { DEFAULT_PAGE_SIZE, DeviceSetList, issueOptions, useIssueFilter } from "@/protoFleet/components/DeviceSetList";
import { getNextSortFromSelection, RACK_SORT_OPTIONS } from "@/protoFleet/components/DeviceSetList/sortConfig";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import NullState from "@/protoFleet/components/NullState";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import FleetGroupActionsMenu from "@/protoFleet/features/fleetManagement/components/FleetGroupActionsMenu";
import {
  AssignMinersModal,
  type RackFormData,
} from "@/protoFleet/features/rackManagement/components/AssignMinersModal";
import { RackCard } from "@/protoFleet/features/rackManagement/components/RackCard";
import RackSettingsModal from "@/protoFleet/features/rackManagement/components/RackSettingsModal";
import {
  BUILDING_URL_PARAM,
  parseBuildingIdsFromParams,
} from "@/protoFleet/features/rackManagement/utils/buildingFilterUrl";
import { mapRackToCardProps } from "@/protoFleet/features/rackManagement/utils/rackCardMapper";
import { useDeviceSetListState } from "@/protoFleet/hooks/useDeviceSetListState";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

import { Alert, ArrowRight, ChevronDown, Edit, Plus, Racks } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import FilterChipsBar from "@/shared/components/List/Filters/FilterChipsBar";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import useMeasure from "@/shared/hooks/useMeasure";
import { useNavigate } from "@/shared/hooks/useNavigate";

const RACK_COLUMNS_FLEET: DeviceSetColumn[] = [
  "name",
  "site",
  "building",
  "zone",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

const RACK_COLUMNS_STANDALONE: DeviceSetColumn[] = [
  "name",
  "zone",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

const RacksPage = () => {
  const navigate = useNavigate();
  const { listRacks, listRackZones, deleteGroup } = useDeviceSets();
  const { listAllBuildings, assignRackToBuilding } = useBuildings();
  // Rack edit + AssignRackToBuilding are gated server-side; the UI gates
  // mirror the BuildingList / SiteList pattern so 403-bound actions never
  // surface in the row menu.
  const canEditRack = useHasPermission("rack:manage");
  const canAssignRackToBuilding = useHasPermission("site:manage");
  // Re-parent picker target. When set, ParentPickerModal opens with the
  // rack as the source; on confirm we dispatch AssignRackToBuilding.
  const [reparentTarget, setReparentTarget] = useState<DeviceSet | null>(null);
  const { listSites } = useSites();
  const [searchParams, setSearchParams] = useSearchParams();
  const { pathname } = useLocation();
  const insideFleetShell = pathname.startsWith("/fleet/");
  const [showRackSettingsModal, setShowRackSettingsModal] = useState(false);
  // When set, RackSettingsModal opens in edit mode against this row.
  // Cleared on dismiss alongside the show flag so the next "Add rack"
  // CTA starts from a clean create-mode slate.
  const [editingRack, setEditingRack] = useState<DeviceSet | null>(null);
  const [selectedZones, setSelectedZones] = useState<string[]>([]);
  const [selectedIssues, setSelectedIssues] = useState<string[]>([]);
  const [allZones, setAllZones] = useState<{ id: string; label: string }[]>([]);
  const [allBuildings, setAllBuildings] = useState<{ id: string; label: string; siteId: string }[]>([]);
  // Flips on the first successful ListBuildings response. Needed so the
  // empty-result branch for `?site=<id>` with zero matching buildings
  // doesn't fire while buildings are still loading (which would briefly
  // mask all racks). Default false; toggles true regardless of result
  // length so a real zero-building site is distinguishable from "still
  // loading".
  const [allBuildingsLoaded, setAllBuildingsLoaded] = useState(false);
  const [allSites, setAllSites] = useState<{ id: string; label: string }[]>([]);

  // `?site=<id>` lands here via SiteList's "View racks" deep link. The
  // rack list RPC has no native `siteIds` filter, so we resolve site →
  // buildings client-side and feed those building ids through the
  // existing buildingIds filter. Without this, the deep link no-ops
  // (operator sees the full racks list instead of the site's racks).
  const urlSiteIds = useMemo(
    () =>
      new Set(
        searchParams
          .getAll("site")
          .flatMap((raw) => raw.split(","))
          .map((value) => value.trim())
          .filter((value) => value !== "" && /^\d+$/.test(value)),
      ),
    [searchParams],
  );

  const selectedBuildingIds = useMemo(() => parseBuildingIdsFromParams(searchParams), [searchParams]);
  const selectedBuildingIdStrings = useMemo(() => selectedBuildingIds.map(String), [selectedBuildingIds]);
  // Building IDs the rack list should be filtered to. If the operator
  // picked buildings explicitly, that wins. Otherwise, when a site
  // filter is present, expand it into the buildings that belong to
  // those sites once `allBuildings` has loaded.
  //
  // Sentinel: when the site filter is set but resolves to zero
  // buildings (loaded list confirms there are none), we send `[0n]`
  // rather than `[]`. Server treats `buildingIds: []` as "no filter",
  // so without the sentinel a valid site that simply has no buildings
  // would show every rack in the fleet. Building IDs are positive
  // auto-incrementing in the DB, so `WHERE building_id IN (0)` matches
  // nothing — exactly the empty result the operator expects.
  const effectiveBuildingIds = useMemo(() => {
    if (selectedBuildingIds.length > 0) return selectedBuildingIds;
    if (urlSiteIds.size === 0) return [] as bigint[];
    const matched = allBuildings.filter((b) => urlSiteIds.has(b.siteId)).map((b) => BigInt(b.id));
    if (matched.length === 0 && allBuildingsLoaded) return [0n];
    return matched;
  }, [selectedBuildingIds, urlSiteIds, allBuildings, allBuildingsLoaded]);
  const effectiveBuildingIdsRef = useRef<bigint[]>(effectiveBuildingIds);
  useEffect(() => {
    effectiveBuildingIdsRef.current = effectiveBuildingIds;
  }, [effectiveBuildingIds]);
  const getBuildingIds = useCallback(() => effectiveBuildingIdsRef.current, []);

  // AssignMinersModal state
  const [assignMinersFormData, setAssignMinersFormData] = useState<RackFormData | null>(null);
  const [assignMinersRackId, setAssignMinersRackId] = useState<bigint | undefined>(undefined);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

  const selectedZonesRef = useRef<string[]>([]);
  const getZones = useCallback(() => selectedZonesRef.current, []);

  const {
    deviceSets: racks,
    statsMap,
    isLoading,
    hasEverLoaded,
    hasCompletedInitialFetch,
    error,
    currentSort,
    currentPage,
    hasNextPage,
    totalCount,
    handleSort,
    handleNextPage,
    handlePrevPage,
    resetAndFetch,
    refreshCurrentPage,
  } = useDeviceSetListState(listRacks, DEFAULT_PAGE_SIZE, getErrorComponentTypes, getZones, getBuildingIds);

  const racksViewMode = useFleetStore((s) => s.ui.racksViewMode);
  const setRacksViewMode = useFleetStore((s) => s.ui.setRacksViewMode);
  const temperatureUnit = useFleetStore((s) => s.ui.temperatureUnit);

  // Fetch all rack zones once on mount
  const zonesRequestId = useRef(0);
  const fetchZones = useCallback(() => {
    const requestId = ++zonesRequestId.current;
    listRackZones({
      onSuccess: (zones) => {
        if (requestId !== zonesRequestId.current) return;
        setAllZones(zones.map((z) => ({ id: z, label: z })));
      },
    });
  }, [listRackZones]);

  useEffect(() => {
    fetchZones();
  }, [fetchZones]);

  // Fetch all buildings once for the building filter dropdown. The list
  // is small (org-scoped) and stable enough that a single load on mount
  // is fine; the rack list re-renders independently when filters change.
  useEffect(() => {
    const controller = new AbortController();
    void listAllBuildings({
      signal: controller.signal,
      onSuccess: (buildings: BuildingWithCounts[]) => {
        setAllBuildings(
          buildings
            .filter((b) => b.building !== undefined)
            .map((b) => ({
              id: b.building!.id.toString(),
              label: b.building!.name,
              // Carry siteId through so `?site=<id>` deep links can
              // resolve to the building set without a second lookup.
              siteId: (b.building!.siteId ?? 0n).toString(),
            })),
        );
        setAllBuildingsLoaded(true);
      },
    });
    return () => controller.abort();
  }, [listAllBuildings]);

  // Sites lookup powers the new Site column on the rack list. Same one-shot
  // load shape as buildings; renames are infrequent enough that polling here
  // would be wasted bandwidth.
  useEffect(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (sites: SiteWithCounts[]) => {
        setAllSites(
          sites.filter((s) => s.site !== undefined).map((s) => ({ id: s.site!.id.toString(), label: s.site!.name })),
        );
      },
      onError: () => setAllSites([]),
    });
    return () => controller.abort();
  }, [listSites]);

  const siteNameById = useMemo(() => new Map(allSites.map((s) => [s.id, s.label])), [allSites]);
  const buildingNameById = useMemo(() => new Map(allBuildings.map((b) => [b.id, b.label])), [allBuildings]);

  const setBuildingFilter = useCallback(
    (ids: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete(BUILDING_URL_PARAM);
          ids.forEach((id) => {
            const trimmed = id.trim();
            if (trimmed && /^\d+$/.test(trimmed)) next.append(BUILDING_URL_PARAM, trimmed);
          });
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Refetch when the resolved building filter changes. Covers both the
  // explicit building filter and the site → buildings expansion that
  // lands once `allBuildings` finishes loading. The ref is read by
  // useDeviceSetListState's fetchPage so this effect just kicks the
  // pagination reset; the filter value itself flows through the ref.
  const effectiveBuildingKey = useMemo(() => effectiveBuildingIds.map(String).join(","), [effectiveBuildingIds]);
  const prevBuildingKey = useRef<string | null>(null);
  useEffect(() => {
    if (prevBuildingKey.current !== null && prevBuildingKey.current !== effectiveBuildingKey) {
      resetAndFetch();
    }
    prevBuildingKey.current = effectiveBuildingKey;
  }, [effectiveBuildingKey, resetAndFetch]);

  const handleFilterChange = useCallback(
    (key: string, values: string[]) => {
      if (key === "zone") {
        setSelectedZones(values);
        selectedZonesRef.current = values;
        resetAndFetch();
        return;
      }
      if (key === "issues") {
        setSelectedIssues(values);
        selectedIssuesRef.current = values;
        resetAndFetch();
        return;
      }
      if (key === "building") {
        setBuildingFilter(values);
      }
    },
    [resetAndFetch, selectedIssuesRef, selectedZonesRef, setBuildingFilter],
  );

  const filterChipsBarFilters = useMemo(
    () => [
      {
        key: "building",
        title: "Building",
        pluralTitle: "buildings",
        options: allBuildings,
        selectedValues: selectedBuildingIdStrings,
      },
      {
        key: "zone",
        title: "Zone",
        pluralTitle: "zones",
        options: allZones,
        selectedValues: selectedZones,
      },
      {
        key: "issues",
        title: "Issues",
        pluralTitle: "issues",
        options: issueOptions,
        selectedValues: selectedIssues,
      },
    ],
    [allBuildings, selectedBuildingIdStrings, allZones, selectedZones, selectedIssues],
  );

  const hasActiveFilters =
    selectedBuildingIdStrings.length > 0 ||
    selectedZones.length > 0 ||
    selectedIssues.length > 0 ||
    // URL-driven site filter counts as an active filter even when the
    // operator didn't pick buildings explicitly, so a site-scoped result
    // of zero falls into the "no matches for this filter" empty state
    // rather than the global "you haven't set up any racks" null state.
    urlSiteIds.size > 0;

  const handleClearFilters = useCallback(() => {
    // Snapshot before any state changes — drives whether the URL-change
    // effect will refetch for us.
    const hadBuildingFilter = selectedBuildingIdStrings.length > 0;
    setSelectedZones([]);
    selectedZonesRef.current = [];
    setSelectedIssues([]);
    selectedIssuesRef.current = [];
    setBuildingFilter([]);
    // When the building filter was active, clearing the URL fires the
    // `prevBuildingKey` effect which calls resetAndFetch with the cleared
    // ref. Calling it here too would double-fetch (the first call would
    // read stale building ids from the ref before the URL change settles).
    if (!hadBuildingFilter) {
      resetAndFetch();
    }
  }, [resetAndFetch, selectedBuildingIdStrings, selectedIssuesRef, selectedZonesRef, setBuildingFilter]);

  const emptyStateRow: ReactNode = useMemo(() => {
    if (isLoading || totalCount > 0) return undefined;
    return <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />;
  }, [hasActiveFilters, isLoading, totalCount, handleClearFilters]);

  const handleRackSettingsContinue = useCallback((formData: RackFormData) => {
    setShowRackSettingsModal(false);
    setAssignMinersFormData(formData);
    setAssignMinersRackId(undefined);
  }, []);

  const handleAssignMinersDismiss = useCallback(() => {
    setAssignMinersFormData(null);
    setAssignMinersRackId(undefined);
  }, []);

  const handleAssignMinersSave = useCallback(() => {
    setAssignMinersFormData(null);
    setAssignMinersRackId(undefined);
    resetAndFetch();
    fetchZones();
  }, [resetAndFetch, fetchZones]);

  const handleDeleteRack = useCallback(() => {
    if (!assignMinersRackId) return Promise.resolve();
    return new Promise<void>((resolve, reject) => {
      deleteGroup({
        deviceSetId: assignMinersRackId,
        onSuccess: () => {
          pushToast({ message: "Rack deleted", status: STATUSES.success });
          setAssignMinersFormData(null);
          setAssignMinersRackId(undefined);
          resetAndFetch();
          fetchZones();
          resolve();
        },
        onError: (msg) => {
          pushToast({ message: msg, status: STATUSES.error });
          reject(new Error(msg));
        },
      });
    });
  }, [assignMinersRackId, deleteGroup, resetAndFetch, fetchZones]);

  const handleEditRack = useCallback((rack: DeviceSet) => {
    setEditingRack(rack);
    setShowRackSettingsModal(true);
  }, []);

  // Extras appended below FleetGroupActionsMenu's wired bulk cluster.
  // Mirrors the Sites / Buildings menus: navigate, then edit, then
  // re-parent (Add to building). Add to site stays deferred — no
  // dedicated AssignRackToSite RPC exists today; SaveRack is a heavy
  // full-replace, which we don't want to wire from a row action.
  const buildRackExtraActions = useCallback(
    (rack: DeviceSet) => [
      {
        label: "View rack",
        icon: <ArrowRight />,
        onClick: () => navigate(`/racks/${rack.id}`),
      },
      {
        label: "View miners",
        icon: <ArrowRight />,
        onClick: () => navigate(`/miners?rack=${rack.id}`),
        showGroupDivider: true,
      },
      {
        label: "Edit rack",
        icon: <Edit />,
        onClick: () => handleEditRack(rack),
        hidden: !canEditRack,
      },
      {
        label: "Add to building",
        icon: <Plus />,
        onClick: () => setReparentTarget(rack),
        hidden: !canAssignRackToBuilding,
      },
    ],
    [navigate, handleEditRack, canEditRack, canAssignRackToBuilding],
  );

  const renderName = useCallback(
    (item: DeviceSetListItem) => {
      const rack = item.deviceSet;
      const label = rack.label || "(unnamed)";
      return (
        <div className="grid w-full grid-cols-[1fr_auto] items-center gap-2">
          <button
            type="button"
            className="truncate text-left hover:underline"
            onClick={() => navigate(`/racks/${rack.id}`)}
          >
            {label}
          </button>
          {rack.id !== undefined && rack.id !== 0n ? (
            <FleetGroupActionsMenu
              scope={{ kind: "rack", id: rack.id, name: label }}
              ariaLabel={`Actions for ${label}`}
              testIdPrefix={`rack-list-row-${rack.id.toString()}-actions`}
              extraActions={buildRackExtraActions(rack)}
            />
          ) : null}
        </div>
      );
    },
    [navigate, buildRackExtraActions],
  );

  const renderMiners = useCallback((item: DeviceSetListItem) => <span>{item.deviceSet.deviceCount}</span>, []);

  const renderSite = useCallback(
    (item: DeviceSetListItem) => {
      if (item.deviceSet.typeDetails.case !== "rackInfo") return <span>—</span>;
      const siteId = item.deviceSet.typeDetails.value.siteId;
      if (siteId === undefined) return <span>—</span>;
      return <span>{siteNameById.get(siteId.toString()) ?? "—"}</span>;
    },
    [siteNameById],
  );

  const renderBuilding = useCallback(
    (item: DeviceSetListItem) => {
      if (item.deviceSet.typeDetails.case !== "rackInfo") return <span>—</span>;
      const buildingId = item.deviceSet.typeDetails.value.buildingId;
      if (buildingId === undefined) return <span>—</span>;
      return <span>{buildingNameById.get(buildingId.toString()) ?? "—"}</span>;
    },
    [buildingNameById],
  );

  // Responsive grid measurement
  const [measureRef, contentRect] = useMeasure<HTMLDivElement>();
  const RACK_CARD_MIN_WIDTH_PX = 300;
  const numColumns = Math.max(1, Math.floor((contentRect.width || RACK_CARD_MIN_WIDTH_PX) / RACK_CARD_MIN_WIDTH_PX));

  // Polling — refresh current page every 60s, paused while modals are open
  const isModalOpen = !!assignMinersFormData || showRackSettingsModal;
  useEffect(() => {
    if (!hasCompletedInitialFetch || isModalOpen) return;
    const intervalId = setInterval(() => {
      refreshCurrentPage();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasCompletedInitialFetch, isModalOpen, refreshCurrentPage]);

  // Sort dropdown handler for grid view
  const handleSortSelect = useCallback(
    (selected: string[]) => {
      const nextSort = getNextSortFromSelection(selected, currentSort);
      handleSort(nextSort.field, nextSort.direction);
    },
    [currentSort, handleSort],
  );

  // Grid pagination
  const firstItemIndex = currentPage * DEFAULT_PAGE_SIZE + 1;
  const lastItemIndex = currentPage * DEFAULT_PAGE_SIZE + racks.length;
  const shouldRenderGridPagination = !isLoading && totalCount > 0;

  if (isLoading && !hasEverLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (error && !hasEverLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-300 text-text-primary-50">{error}</p>
      </div>
    );
  }

  // `hasActiveFilters` short-circuits the null state when the user is
  // filtering. `hasEverLoaded` only flips when an unfiltered fetch returns
  // a non-empty page (useDeviceSetListState), so deep-linking to a building
  // that has no racks would otherwise render "You haven't set up any racks"
  // instead of the filtered-empty state with the chip showing.
  const hasRacks = hasEverLoaded || totalCount > 0 || racks.length > 0 || hasActiveFilters;

  if (!hasRacks) {
    return (
      <>
        <NullState
          icon={<Racks width="w-5" />}
          title="You haven't set up any racks"
          description="Add a rack and assign miners to rack positions to get started."
          action={
            <Button variant="primary" onClick={() => setShowRackSettingsModal(true)}>
              Add rack
            </Button>
          }
        />
        {showRackSettingsModal ? (
          <RackSettingsModal
            show={showRackSettingsModal}
            existingRacks={racks}
            onDismiss={() => setShowRackSettingsModal(false)}
            onContinue={handleRackSettingsContinue}
          />
        ) : null}
        {assignMinersFormData ? (
          <AssignMinersModal
            show={!!assignMinersFormData}
            rackSettings={assignMinersFormData}
            existingRackId={assignMinersRackId}
            existingRacks={racks}
            onDismiss={handleAssignMinersDismiss}
            onSave={handleAssignMinersSave}
            onDelete={assignMinersRackId ? handleDeleteRack : undefined}
          />
        ) : null}
      </>
    );
  }

  return (
    <div>
      <div className="sticky left-0 z-3 px-6 pt-6 laptop:px-10 laptop:pt-10">
        {insideFleetShell ? null : <h1 className="pb-4 text-heading-300 text-text-primary">Racks</h1>}
        <div className="flex flex-col gap-2 pb-6">
          {/* Action button — full-width on tablet/phone */}
          <div className="block laptop:hidden">
            <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowRackSettingsModal(true)}>
              Add rack
            </Button>
          </div>
          {/* View toggle — full width on tablet/phone */}
          <div className="block laptop:hidden">
            <SegmentedControl
              key={`mobile-${racksViewMode}`}
              className="!w-full whitespace-nowrap [&>button]:flex-1"
              segmentClassName="text-center"
              segments={[
                { key: "grid", title: "View grid" },
                { key: "list", title: "View list" },
              ]}
              initialSegmentKey={racksViewMode}
              onSelect={(key) => setRacksViewMode(key as "grid" | "list")}
            />
          </div>
          {/* Desktop layout — single row with toggle + filters left, buttons right */}
          <div className="hidden flex-row flex-wrap items-center gap-2 laptop:flex">
            <SegmentedControl
              key={`desktop-${racksViewMode}`}
              className="shrink-0 whitespace-nowrap"
              segments={[
                { key: "grid", title: "View grid" },
                { key: "list", title: "View list" },
              ]}
              initialSegmentKey={racksViewMode}
              onSelect={(key) => setRacksViewMode(key as "grid" | "list")}
            />
            <FilterChipsBar
              filters={filterChipsBarFilters}
              onChange={handleFilterChange}
              onClearAll={handleClearFilters}
            />
            {racksViewMode === "grid" ? (
              <DropdownFilter
                title="Sort"
                options={RACK_SORT_OPTIONS}
                selectedOptions={[currentSort.field]}
                onSelect={handleSortSelect}
                showSelectAll={false}
              />
            ) : null}
            <Button
              className="ml-auto"
              variant={variants.secondary}
              size={sizes.compact}
              onClick={() => setShowRackSettingsModal(true)}
            >
              Add rack
            </Button>
          </div>
          {/* Filters — shown separately on tablet/phone */}
          <div className="flex flex-row flex-wrap items-center gap-2 laptop:hidden">
            <FilterChipsBar
              filters={filterChipsBarFilters}
              onChange={handleFilterChange}
              onClearAll={handleClearFilters}
            />
            {racksViewMode === "grid" ? (
              <DropdownFilter
                title="Sort"
                options={RACK_SORT_OPTIONS}
                selectedOptions={[currentSort.field]}
                onSelect={handleSortSelect}
                showSelectAll={false}
              />
            ) : null}
          </div>
        </div>
      </div>
      {error ? (
        <Callout className="mx-6 mb-4 laptop:mx-10" intent="danger" prefixIcon={<Alert />} title={error} />
      ) : null}
      {racksViewMode === "list" ? (
        <div className="overflow-x-auto p-6 pt-0 laptop:p-10 laptop:pt-0">
          <DeviceSetList
            deviceSets={racks}
            statsMap={statsMap}
            renderName={renderName}
            renderMiners={renderMiners}
            renderSite={renderSite}
            renderBuilding={renderBuilding}
            columns={insideFleetShell && MULTI_SITE_ENABLED ? RACK_COLUMNS_FLEET : RACK_COLUMNS_STANDALONE}
            currentSort={currentSort}
            onSort={handleSort}
            itemName={{ singular: "rack", plural: "racks" }}
            total={totalCount}
            loading={isLoading}
            pageSize={DEFAULT_PAGE_SIZE}
            currentPage={currentPage}
            hasPreviousPage={currentPage > 0}
            hasNextPage={hasNextPage}
            onNextPage={handleNextPage}
            onPrevPage={handlePrevPage}
            emptyStateRow={emptyStateRow}
          />
        </div>
      ) : (
        <div className="px-6 laptop:px-10">
          {isLoading && racks.length === 0 ? (
            <div className="flex items-center justify-center py-20">
              <ProgressCircular indeterminate />
            </div>
          ) : racks.length === 0 ? (
            <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />
          ) : (
            <div ref={measureRef}>
              <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(${numColumns}, 1fr)` }}>
                {racks.map((rack) => {
                  const stats = statsMap.get(rack.id);
                  const { zone, rows, cols, loading, statusSegments, slots, hashrate, efficiency, power, temperature } =
                    mapRackToCardProps(rack, stats, temperatureUnit);
                  return (
                    <RackCard
                      key={rack.id.toString()}
                      label={rack.label}
                      zone={zone}
                      cols={cols}
                      rows={rows}
                      slots={slots}
                      loading={loading}
                      statusSegments={statusSegments}
                      hashrate={hashrate}
                      efficiency={efficiency}
                      power={power}
                      temperature={temperature}
                      onClick={() => navigate(`/racks/${rack.id}`)}
                    />
                  );
                })}
              </div>
            </div>
          )}
          {shouldRenderGridPagination || (currentPage > 0 && racks.length === 0) ? (
            <div className="sticky left-0 flex flex-col items-center gap-4 py-6">
              <span className="text-300 text-text-primary">
                Showing {firstItemIndex}–{lastItemIndex} of {totalCount} racks
              </span>
              <div className="flex gap-3">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  ariaLabel="Previous page"
                  prefixIcon={<ChevronDown className="rotate-90" />}
                  onClick={handlePrevPage}
                  disabled={currentPage === 0}
                />
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  ariaLabel="Next page"
                  prefixIcon={<ChevronDown className="rotate-270" />}
                  onClick={handleNextPage}
                  disabled={!hasNextPage}
                />
              </div>
            </div>
          ) : null}
        </div>
      )}
      {showRackSettingsModal ? (
        <RackSettingsModal
          show={showRackSettingsModal}
          existingRacks={racks}
          rack={editingRack ?? undefined}
          onDismiss={() => {
            setShowRackSettingsModal(false);
            setEditingRack(null);
          }}
          onContinue={handleRackSettingsContinue}
        />
      ) : null}
      {assignMinersFormData ? (
        <AssignMinersModal
          show={!!assignMinersFormData}
          rackSettings={assignMinersFormData}
          existingRackId={assignMinersRackId}
          existingRacks={racks}
          onDismiss={handleAssignMinersDismiss}
          onSave={handleAssignMinersSave}
        />
      ) : null}
      {reparentTarget ? (
        <ParentPickerModal
          kind="building"
          show
          selectionMode="single"
          sourceLabel={reparentTarget.label || "rack"}
          description={
            reparentTarget.deviceCount > 0
              ? `${reparentTarget.deviceCount} ${reparentTarget.deviceCount === 1 ? "miner" : "miners"} will move with this rack.`
              : undefined
          }
          currentParentId={
            reparentTarget.typeDetails.case === "rackInfo" ? reparentTarget.typeDetails.value.buildingId : undefined
          }
          onDismiss={() => setReparentTarget(null)}
          onConfirm={(buildingIds) => {
            const buildingId = buildingIds[0];
            if (buildingId === undefined) return;
            const rackName = reparentTarget.label || "rack";
            setReparentTarget(null);
            void assignRackToBuilding({
              rackId: reparentTarget.id,
              buildingId,
              onSuccess: () => {
                pushToast({ message: `Moved "${rackName}" to selected building.`, status: STATUSES.success });
                resetAndFetch();
              },
              onError: (msg) => pushToast({ message: `Couldn't move rack: ${msg}`, status: STATUSES.error }),
            });
          }}
        />
      ) : null}
    </div>
  );
};

export default RacksPage;
