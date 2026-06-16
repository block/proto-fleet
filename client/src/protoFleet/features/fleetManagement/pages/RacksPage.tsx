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
import FleetGroupListActionBar from "@/protoFleet/features/fleetManagement/components/FleetGroupActionsMenu/FleetGroupListActionBar";
import { useOptionalFleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import { ManageRackModal, type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal";
import { RackCard } from "@/protoFleet/features/fleetManagement/components/RackCard";
import RackSettingsModal from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";
import {
  BUILDING_URL_PARAM,
  parseBuildingIdsFromParams,
} from "@/protoFleet/features/fleetManagement/utils/buildingFilterUrl";
import { mapRackToCardProps } from "@/protoFleet/features/fleetManagement/utils/rackCardMapper";
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
  const { listAllBuildings, assignRacksToBuilding } = useBuildings();
  const canEditRack = useHasPermission("rack:manage");
  const canAssignRacksToBuilding = useHasPermission("site:manage");
  const [reparentTarget, setReparentTarget] = useState<DeviceSet | null>(null);
  const { listSites } = useSites();
  const [searchParams, setSearchParams] = useSearchParams();
  const { pathname } = useLocation();
  const insideFleetShell = pathname.startsWith("/fleet/");
  const [showRackSettingsModal, setShowRackSettingsModal] = useState(false);
  // Zones + issues are URL-driven so saved views can capture them.
  const selectedZones = useMemo(() => {
    const values = searchParams.getAll("zone").flatMap((raw) => raw.split(","));
    return Array.from(new Set(values.map((v) => v.trim()).filter(Boolean)));
  }, [searchParams]);
  const selectedIssues = useMemo(() => {
    const values = searchParams.getAll("issues").flatMap((raw) => raw.split(","));
    return Array.from(new Set(values.map((v) => v.trim()).filter(Boolean)));
  }, [searchParams]);
  const [allZones, setAllZones] = useState<{ id: string; label: string }[]>([]);
  const [allBuildings, setAllBuildings] = useState<{ id: string; label: string; siteId: string }[]>([]);
  // Distinguishes "buildings still loading" from "site has zero buildings"
  // for the empty-filter sentinel below.
  const [allBuildingsLoaded, setAllBuildingsLoaded] = useState(false);
  const [allSites, setAllSites] = useState<{ id: string; label: string }[]>([]);
  const [selectedRackIds, setSelectedRackIds] = useState<string[]>([]);
  const [isBulkActionBusy, setIsBulkActionBusy] = useState(false);

  // listDeviceSets has no native siteIds filter, so we resolve
  // site → buildings client-side and pipe through buildingIds.
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
  // Explicit building filter wins; otherwise expand `?site=` into the
  // sites' buildings. `[0n]` is a sentinel for "no buildings match" —
  // server treats `[]` as no filter, so without it a site-scoped view
  // would briefly show every rack while buildings are still loading
  // (or permanently if the site has zero buildings). Building IDs are
  // positive autoincrement, so `WHERE building_id IN (0)` matches nothing.
  const effectiveBuildingIds = useMemo(() => {
    if (selectedBuildingIds.length > 0) return selectedBuildingIds;
    if (urlSiteIds.size === 0) return [] as bigint[];
    if (!allBuildingsLoaded) return [0n];
    const matched = allBuildings.filter((b) => urlSiteIds.has(b.siteId)).map((b) => BigInt(b.id));
    if (matched.length === 0) return [0n];
    return matched;
  }, [selectedBuildingIds, urlSiteIds, allBuildings, allBuildingsLoaded]);
  const effectiveBuildingIdsRef = useRef<bigint[]>(effectiveBuildingIds);
  useEffect(() => {
    effectiveBuildingIdsRef.current = effectiveBuildingIds;
  }, [effectiveBuildingIds]);
  const getBuildingIds = useCallback(() => effectiveBuildingIdsRef.current, []);

  // ManageRackModal state
  const [manageRackFormData, setManageRackFormData] = useState<RackFormData | null>(null);
  const [manageRackId, setManageRackId] = useState<bigint | undefined>(undefined);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

  // Seed the refs with the URL-derived initial values so the first
  // useDeviceSetListState fetch picks up filters from a `?issues=` /
  // `?zone=` deep link (or a saved view's restored URL). The useEffects
  // below keep them in sync on subsequent URL changes.
  const selectedZonesRef = useRef<string[]>(selectedZones);
  const getZones = useCallback(() => selectedZonesRef.current, []);
  // Mirror the same one-shot seed for the issue ref provided by
  // useIssueFilter — without this the initial fetch would miss any
  // `?issues=` URL state. Render-time write is intentional; the
  // companion useEffect below handles subsequent updates.
  // eslint-disable-next-line react-hooks/refs -- intentional render-time sync; initial mount only
  selectedIssuesRef.current = selectedIssues;

  useEffect(() => {
    selectedZonesRef.current = selectedZones;
  }, [selectedZones, selectedZonesRef]);
  useEffect(() => {
    selectedIssuesRef.current = selectedIssues;
  }, [selectedIssues, selectedIssuesRef]);

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

  const storedRacksViewMode = useFleetStore((s) => s.ui.racksViewMode);
  const setStoredRacksViewMode = useFleetStore((s) => s.ui.setRacksViewMode);
  const temperatureUnit = useFleetStore((s) => s.ui.temperatureUnit);

  // URL is the source of truth for the segmented control so a saved view's
  // `display` param can dictate grid vs. list. Falls back to the persisted
  // Zustand preference when the param is absent so default sessions keep
  // the operator's last choice.
  const urlRacksViewMode: "grid" | "list" | undefined = (() => {
    const raw = searchParams.get("display");
    return raw === "grid" || raw === "list" ? raw : undefined;
  })();
  const racksViewMode = urlRacksViewMode ?? storedRacksViewMode;

  // Sync the effective view mode into the URL when it isn't already there
  // so saved-view capture can see it. Runs once per absence — guarded by
  // `urlRacksViewMode === undefined` so it doesn't loop after the write.
  useEffect(() => {
    if (urlRacksViewMode !== undefined) return;
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("display", storedRacksViewMode);
        return next;
      },
      { replace: true },
    );
  }, [urlRacksViewMode, storedRacksViewMode, setSearchParams]);
  const setRacksViewMode = useCallback(
    (mode: "grid" | "list") => {
      // Mirror to both: URL for view-snapshot capture, Zustand so the
      // preference survives navigating away from a `?display=...` URL.
      setStoredRacksViewMode(mode);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("display", mode);
          return next;
        },
        { replace: true },
      );
    },
    [setStoredRacksViewMode, setSearchParams],
  );

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

  // One-shot load — org-scoped buildings are small + stable.
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
              siteId: (b.building!.siteId ?? 0n).toString(),
            })),
        );
        setAllBuildingsLoaded(true);
      },
    });
    return () => controller.abort();
  }, [listAllBuildings]);

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

  // Surface buildings + sites to FleetLayout so the saved-view modal can
  // render human-readable labels when a view captures `building=` or
  // `site=` params. Guarded — RacksPage also mounts at standalone /racks
  // where there is no parent Outlet.
  const outletContext = useOptionalFleetOutletContext();
  const buildingSources = useMemo(() => allBuildings.map(({ id, label }) => ({ id, label })), [allBuildings]);
  useEffect(() => {
    outletContext?.publishViewFilterContext({
      availableBuildings: buildingSources,
      availableSites: allSites,
    });
  }, [outletContext, buildingSources, allSites]);

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

  // Refetch on resolved building-filter change (explicit + site-expanded).
  // useDeviceSetListState reads the ref; this effect just kicks pagination.
  const effectiveBuildingKey = useMemo(() => effectiveBuildingIds.map(String).join(","), [effectiveBuildingIds]);
  const prevBuildingKey = useRef<string | null>(null);
  useEffect(() => {
    if (prevBuildingKey.current !== null && prevBuildingKey.current !== effectiveBuildingKey) {
      resetAndFetch();
    }
    prevBuildingKey.current = effectiveBuildingKey;
  }, [effectiveBuildingKey, resetAndFetch]);

  const writeMultiParam = useCallback(
    (key: string, values: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete(key);
          values.forEach((v) => {
            const trimmed = v.trim();
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
      setSelectedRackIds([]);
      if (key === "zone") {
        writeMultiParam("zone", values);
        return;
      }
      if (key === "issues") {
        writeMultiParam("issues", values);
        return;
      }
      if (key === "building") {
        setBuildingFilter(values);
      }
    },
    [setBuildingFilter, writeMultiParam],
  );

  // Zone + issues live in the URL now, so they're not tracked by the
  // building/site effect's prevBuildingKey path. Trigger refetch when
  // either of those URL keys changes.
  const zonesIssuesKey = useMemo(
    () => `${selectedZones.join(",")}|${selectedIssues.join(",")}`,
    [selectedZones, selectedIssues],
  );
  const prevZonesIssuesKey = useRef<string | null>(null);
  useEffect(() => {
    if (prevZonesIssuesKey.current !== null && prevZonesIssuesKey.current !== zonesIssuesKey) {
      resetAndFetch();
    }
    prevZonesIssuesKey.current = zonesIssuesKey;
  }, [zonesIssuesKey, resetAndFetch]);

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
    urlSiteIds.size > 0;
  const visibleRackScopes = useMemo(
    () =>
      racks.flatMap((rack) => {
        if (rack.id === 0n) return [];
        return [{ kind: "rack" as const, id: rack.id, name: rack.label || "(unnamed)" }];
      }),
    [racks],
  );
  const selectedRackScopes = useMemo(() => {
    const selected = new Set(selectedRackIds);
    return visibleRackScopes.filter((rack) => selected.has(rack.id.toString()));
  }, [selectedRackIds, visibleRackScopes]);
  const handleSelectAllVisibleRacks = useCallback(
    () => setSelectedRackIds(visibleRackScopes.map((rack) => rack.id.toString())),
    [visibleRackScopes],
  );
  const handleClearRackSelection = useCallback(() => setSelectedRackIds([]), []);
  const handleSelectedRackIdsChange = useCallback(
    (ids: string[]) => {
      if (isBulkActionBusy) return;
      setSelectedRackIds(ids);
    },
    [isBulkActionBusy],
  );

  const handleClearFilters = useCallback(() => {
    setSelectedRackIds([]);
    // All four URL-driven filter keys cleared in a single updater so they
    // batch into one history entry (react-router resolves the prev against
    // the current location, not a value set by an earlier call this render).
    const hadAny =
      selectedBuildingIdStrings.length > 0 ||
      urlSiteIds.size > 0 ||
      selectedZones.length > 0 ||
      selectedIssues.length > 0;
    if (hadAny) {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete("site");
          next.delete(BUILDING_URL_PARAM);
          next.delete("zone");
          next.delete("issues");
          return next;
        },
        { replace: true },
      );
    } else {
      // No URL transition → manually kick the refetch (URL effects skip).
      resetAndFetch();
    }
  }, [resetAndFetch, selectedBuildingIdStrings, selectedIssues, selectedZones, setSearchParams, urlSiteIds]);

  const emptyStateRow: ReactNode = useMemo(() => {
    if (isLoading || totalCount > 0) return undefined;
    return <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />;
  }, [hasActiveFilters, isLoading, totalCount, handleClearFilters]);

  const handleRackSettingsContinue = useCallback((formData: RackFormData) => {
    setShowRackSettingsModal(false);
    setManageRackFormData(formData);
    setManageRackId(undefined);
  }, []);

  const handleManageRackDismiss = useCallback(() => {
    setManageRackFormData(null);
    setManageRackId(undefined);
  }, []);

  const handleManageRackSave = useCallback(() => {
    setManageRackFormData(null);
    setManageRackId(undefined);
    resetAndFetch();
    fetchZones();
  }, [resetAndFetch, fetchZones]);

  const handleDeleteRack = useCallback(() => {
    if (!manageRackId) return Promise.resolve();
    return new Promise<void>((resolve, reject) => {
      deleteGroup({
        deviceSetId: manageRackId,
        onSuccess: () => {
          pushToast({ message: "Rack deleted", status: STATUSES.success });
          setManageRackFormData(null);
          setManageRackId(undefined);
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
  }, [manageRackId, deleteGroup, resetAndFetch, fetchZones]);

  // Mirrors Edit building → ManageBuildingModal: row-level Edit opens
  // the full-screen miners surface directly, with the small
  // RackSettingsModal reachable from inside it for label/zone/dim edits.
  const handleEditRack = useCallback((rack: DeviceSet) => {
    const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
    if (!rackInfo) return;
    setManageRackFormData({
      label: rack.label,
      zone: rackInfo.zone,
      rows: rackInfo.rows,
      columns: rackInfo.columns,
      orderIndex: rackInfo.orderIndex,
      coolingType: rackInfo.coolingType,
    });
    setManageRackId(rack.id);
  }, []);

  // Add-to-site stays deferred — no dedicated AssignRackToSite RPC,
  // and SaveRack is a heavyweight full-replace.
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
        hidden: !canAssignRacksToBuilding,
      },
    ],
    [navigate, handleEditRack, canEditRack, canAssignRacksToBuilding],
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
              scopes={[{ kind: "rack", id: rack.id, name: label }]}
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
  const isModalOpen = !!manageRackFormData || showRackSettingsModal;
  useEffect(() => {
    if (!hasCompletedInitialFetch || isModalOpen) return;
    const intervalId = setInterval(() => {
      refreshCurrentPage();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasCompletedInitialFetch, isModalOpen, refreshCurrentPage]);

  // Sort dropdown handler for grid view
  const handleRackSort: typeof handleSort = useCallback(
    (field, direction) => {
      setSelectedRackIds([]);
      handleSort(field, direction);
    },
    [handleSort],
  );

  const handleSortSelect = useCallback(
    (selected: string[]) => {
      const nextSort = getNextSortFromSelection(selected, currentSort);
      handleRackSort(nextSort.field, nextSort.direction);
    },
    [currentSort, handleRackSort],
  );

  const handleRacksViewModeSelect = useCallback(
    (key: string) => {
      const nextViewMode = key === "list" ? "list" : "grid";
      if (nextViewMode === "grid") setSelectedRackIds([]);
      setRacksViewMode(nextViewMode);
    },
    [setRacksViewMode],
  );

  const handleRackNextPage = useCallback(() => {
    setSelectedRackIds([]);
    handleNextPage();
  }, [handleNextPage]);

  const handleRackPrevPage = useCallback(() => {
    setSelectedRackIds([]);
    handlePrevPage();
  }, [handlePrevPage]);

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
        {manageRackFormData ? (
          <ManageRackModal
            show={!!manageRackFormData}
            rackSettings={manageRackFormData}
            existingRackId={manageRackId}
            existingRacks={racks}
            onDismiss={handleManageRackDismiss}
            onSave={handleManageRackSave}
            onDelete={manageRackId ? handleDeleteRack : undefined}
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
              onSelect={handleRacksViewModeSelect}
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
              onSelect={handleRacksViewModeSelect}
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
            onSort={handleRackSort}
            itemName={{ singular: "rack", plural: "racks" }}
            total={totalCount}
            loading={isLoading}
            pageSize={DEFAULT_PAGE_SIZE}
            currentPage={currentPage}
            hasPreviousPage={currentPage > 0}
            hasNextPage={hasNextPage}
            onNextPage={handleRackNextPage}
            onPrevPage={handleRackPrevPage}
            emptyStateRow={emptyStateRow}
            selectedIds={selectedRackIds}
            onSelectedIdsChange={handleSelectedRackIdsChange}
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
                  onClick={handleRackPrevPage}
                  disabled={currentPage === 0}
                />
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  ariaLabel="Next page"
                  prefixIcon={<ChevronDown className="rotate-270" />}
                  onClick={handleRackNextPage}
                  disabled={!hasNextPage}
                />
              </div>
            </div>
          ) : null}
        </div>
      )}
      {selectedRackScopes.length > 0 || isBulkActionBusy ? (
        <FleetGroupListActionBar
          selectedScopes={selectedRackScopes}
          kind="rack"
          onClearSelection={handleClearRackSelection}
          onSelectAllVisible={handleSelectAllVisibleRacks}
          onActionBusyChange={setIsBulkActionBusy}
        />
      ) : null}
      {showRackSettingsModal ? (
        <RackSettingsModal
          show={showRackSettingsModal}
          existingRacks={racks}
          onDismiss={() => setShowRackSettingsModal(false)}
          onContinue={handleRackSettingsContinue}
        />
      ) : null}
      {manageRackFormData ? (
        <ManageRackModal
          show={!!manageRackFormData}
          rackSettings={manageRackFormData}
          existingRackId={manageRackId}
          existingRacks={racks}
          onDismiss={handleManageRackDismiss}
          onSave={handleManageRackSave}
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
          onConfirm={(buildingIds) =>
            new Promise<void>((resolve, reject) => {
              const buildingId = buildingIds[0];
              if (buildingId === undefined) {
                resolve();
                return;
              }
              const rackName = reparentTarget.label || "rack";
              void assignRacksToBuilding({
                racks: [{ rackId: reparentTarget.id }],
                targetBuildingId: buildingId,
                onSuccess: () => {
                  pushToast({ message: `Moved "${rackName}" to selected building.`, status: STATUSES.success });
                  resetAndFetch();
                  setReparentTarget(null);
                  resolve();
                },
                onError: (msg) => {
                  pushToast({ message: `Couldn't move rack: ${msg}`, status: STATUSES.error });
                  reject(new Error(msg));
                },
              });
            })
          }
        />
      ) : null}
    </div>
  );
};

export default RacksPage;
