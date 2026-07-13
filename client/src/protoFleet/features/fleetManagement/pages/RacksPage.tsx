import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import clsx from "clsx";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import type { DeviceSetListItem } from "@/protoFleet/components/DeviceSetList";
import type { DeviceSetColumn } from "@/protoFleet/components/DeviceSetList";
import { DEFAULT_PAGE_SIZE, DeviceSetList, issueOptions, useIssueFilter } from "@/protoFleet/components/DeviceSetList";
import {
  getNextSortFromSelection,
  RACK_SORT_OPTIONS,
  SORTABLE_COLUMNS,
} from "@/protoFleet/components/DeviceSetList/sortConfig";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import NullState from "@/protoFleet/components/NullState";
import {
  intersectSiteFilters,
  siteFilterFromActive,
  useActiveSite,
} from "@/protoFleet/components/PageHeader/SitePicker";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetCreateFlow } from "@/protoFleet/features/fleetManagement/components/FleetCreateFlow/context";
import FleetGroupActionsMenu from "@/protoFleet/features/fleetManagement/components/FleetGroupActionsMenu";
import FleetGroupListActionBar from "@/protoFleet/features/fleetManagement/components/FleetGroupActionsMenu/FleetGroupListActionBar";
import { useOptionalFleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import { ManageRackModal, type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal";
import { RackCard } from "@/protoFleet/features/fleetManagement/components/RackCard";
import RackSettingsModal from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";
import { BUILDING_URL_PARAM } from "@/protoFleet/features/fleetManagement/utils/buildingFilterUrl";
import {
  FILTER_URL_PARAM_KEYS,
  fleetListTelemetryRangesFromURL,
  parseIdFilterValuesFromURL,
  parseUrlToActiveFilters,
  setTelemetryNumericFilterURLParams,
  UNASSIGNED_FILTER_OPTION,
  UNASSIGNED_URL_VALUE,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { mapRackToCardProps } from "@/protoFleet/features/fleetManagement/utils/rackCardMapper";
import {
  TELEMETRY_FILTER_BOUNDS,
  TELEMETRY_FILTER_KEYS,
  type TelemetryFilterKey,
} from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";
import { useDeviceSetListState } from "@/protoFleet/hooks/useDeviceSetListState";
import { isPathScopable } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

import { Alert, ArrowRight, ChevronDown, Edit, Plus, Racks } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import FilterChipsBar, { type FilterChipsBarNumericFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import { formatListCountLabel } from "@/shared/components/List/listCountLabel";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import useMeasure from "@/shared/hooks/useMeasure";
import { useNavigate } from "@/shared/hooks/useNavigate";
import type { NumericRangeValue } from "@/shared/utils/filterValidation";

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

// Subtitle copy for the "Move racks between sites?" building-clear
// confirm. Falls back to a generic message when no building resolved or
// the set is partial (unresolved) — naming a partial set would mislead.
const siteClearSubtitle = (labels: string[], rackCount: number, unresolved: boolean): string => {
  const rackNoun = rackCount === 1 ? "rack" : "racks";
  const isAre = rackCount === 1 ? "is" : "are";
  if (labels.length === 0 || unresolved) {
    return `${rackCount} of the selected ${rackNoun} ${isAre} in a building that may belong to a different site. Continuing will remove ${rackCount === 1 ? "it" : "them"} from ${rackCount === 1 ? "that building" : "their buildings"} before moving to the selected site.`;
  }
  const labelSummary = labels.slice(0, 3).join(", ");
  const more = labels.length > 3 ? ` and ${labels.length - 3} other building(s)` : "";
  return `${rackCount} of the selected ${rackNoun} ${isAre} currently in ${labelSummary}${more}, which belong${labels.length === 1 ? "s" : ""} to a different site. Continuing will clear the rack ${labels.length === 1 ? "from that building" : "from those buildings"} before moving to the selected site.`;
};

const TELEMETRY_FILTER_CHIPS: FilterChipsBarNumericFilter[] = TELEMETRY_FILTER_KEYS.map((key) => ({
  key,
  title: TELEMETRY_FILTER_BOUNDS[key].label,
  bounds: TELEMETRY_FILTER_BOUNDS[key],
}));

const RacksPage = () => {
  const navigate = useNavigate();
  const { listRacks, listRackZones, deleteGroup } = useDeviceSets();
  const { listAllBuildings, assignRacksToBuilding } = useBuildings();
  const canEditRack = useHasPermission("rack:manage");
  const canReadSiteCatalog = useHasPermission("site:read");
  // Both "Add to building" and "Add to site" reparent actions are gated
  // by site:manage (server enforces the same). One flag, two actions.
  const canManageSitePlacement = useHasPermission("site:manage");
  const [reparentTarget, setReparentTarget] = useState<{ rack: DeviceSet; kind: "building" | "site" } | null>(null);
  const { listSites, assignRacksToSite } = useSites();
  const [searchParams, setSearchParams] = useSearchParams();
  const { pathname } = useLocation();
  const insideFleetShell = isPathScopable(pathname);
  const [showRackSettingsModal, setShowRackSettingsModal] = useState(false);
  // Zones + issues are URL-driven so saved views can capture them. Values
  // are written via repeated keys (`?zone=A&zone=B`), so reading uses
  // `getAll` without comma-splitting — zone labels may legitimately contain
  // commas (e.g. "DC1, Row A").
  const selectedZones = useMemo(
    () =>
      Array.from(
        new Set(
          searchParams
            .getAll("zone")
            .map((v) => v.trim())
            .filter(Boolean),
        ),
      ),
    [searchParams],
  );
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
  const [allZones, setAllZones] = useState<{ id: string; label: string }[]>([]);
  const [allBuildings, setAllBuildings] = useState<{ id: string; label: string; siteId: string }[]>([]);
  const [allSites, setAllSites] = useState<{ id: string; label: string }[] | undefined>(undefined);
  const [selectedRackIds, setSelectedRackIds] = useState<string[]>([]);
  const [isBulkActionBusy, setIsBulkActionBusy] = useState(false);
  const [bulkReparentKind, setBulkReparentKind] = useState<"building" | "site" | null>(null);
  // Tracks the cross-site building-clear confirmation dialog. When a
  // site move would null `device_set_rack.building_id` for any rack in
  // the batch (because the rack's current building belongs to a
  // different site), we park the dispatch behind a confirm prompt that
  // mirrors the cross-site miner-reparent dialog in MinerReparentPicker.
  const [siteClearConfirmation, setSiteClearConfirmation] = useState<{
    affectedBuildingLabels: string[];
    affectedRackCount: number;
    // True when one or more affected racks couldn't be resolved to a
    // building (metadata unavailable); the dialog falls back to a
    // generic message instead of naming buildings.
    unresolved: boolean;
    onConfirm: () => Promise<void>;
  } | null>(null);
  const [siteClearInFlight, setSiteClearInFlight] = useState(false);

  // Path scope → server-side site_ids / include_unassigned. URL `?site=`
  // remains a list filter and composes with scope below.
  // allSites already holds the decimal-string site IDs, so derive the
  // known-id set directly rather than round-tripping through a partial
  // SiteWithCounts cast.
  const knownSiteIds = useMemo(() => (allSites ? new Set(allSites.map((s) => s.id)) : undefined), [allSites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  const activeSiteFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);
  // Prepopulate the new-rack Site dropdown when a single site is scoped in the
  // page header (undefined for "All sites" / "Unassigned").
  const scopedSiteId = useMemo(() => (activeSite.kind === "site" ? BigInt(activeSite.id) : undefined), [activeSite]);

  const selectedSiteFilter = useMemo(() => parseIdFilterValuesFromURL(searchParams, "site"), [searchParams]);
  const selectedSiteValues = selectedSiteFilter.values;
  const selectedSiteIds = useMemo(
    () => selectedSiteValues.filter((value) => value !== UNASSIGNED_URL_VALUE).map((value) => BigInt(value)),
    [selectedSiteValues],
  );
  const telemetryRanges = useMemo(() => fleetListTelemetryRangesFromURL(searchParams), [searchParams]);
  const selectedNumericValues = useMemo(() => parseUrlToActiveFilters(searchParams).numericFilters, [searchParams]);

  const selectedBuildingFilter = useMemo(() => parseIdFilterValuesFromURL(searchParams, "building"), [searchParams]);
  const selectedBuildingValues = selectedBuildingFilter.values;
  const selectedBuildingIds = useMemo(
    () => selectedBuildingValues.filter((value) => value !== UNASSIGNED_URL_VALUE).map((value) => BigInt(value)),
    [selectedBuildingValues],
  );
  const selectedBuildingIdStrings = selectedBuildingValues;
  const includeNoBuilding = selectedBuildingFilter.includeUnassigned;
  const effectiveBuildingIdsRef = useRef<bigint[]>(selectedBuildingIds);
  useEffect(() => {
    effectiveBuildingIdsRef.current = selectedBuildingIds;
  }, [selectedBuildingIds]);
  const getBuildingIds = useCallback(() => effectiveBuildingIdsRef.current, []);
  const includeNoBuildingRef = useRef(includeNoBuilding);
  useEffect(() => {
    includeNoBuildingRef.current = includeNoBuilding;
  }, [includeNoBuilding]);
  const getIncludeNoBuilding = useCallback(() => includeNoBuildingRef.current, []);
  const telemetryRangesRef = useRef(telemetryRanges);
  useEffect(() => {
    telemetryRangesRef.current = telemetryRanges;
  }, [telemetryRanges]);
  const getTelemetryRanges = useCallback(() => telemetryRangesRef.current, []);

  // Effective site filter: path scope ∩ `?site=` list filter. When neither
  // is set the filter is empty and the server returns every rack in the org.
  const effectiveSiteFilter = useMemo(() => {
    return intersectSiteFilters(activeSiteFilter, {
      siteIds: selectedSiteIds,
      includeUnassigned: selectedSiteFilter.includeUnassigned,
    });
  }, [activeSiteFilter, selectedSiteIds, selectedSiteFilter.includeUnassigned]);
  const effectiveSiteFilterRef = useRef(effectiveSiteFilter);
  useEffect(() => {
    effectiveSiteFilterRef.current = effectiveSiteFilter;
  }, [effectiveSiteFilter]);
  const getSiteFilter = useCallback(() => effectiveSiteFilterRef.current, []);
  const getSiteIds = useCallback(() => effectiveSiteFilterRef.current.siteIds, []);
  const getIncludeUnassigned = useCallback(() => effectiveSiteFilterRef.current.includeUnassigned, []);

  // ManageRackModal state
  const [manageRackFormData, setManageRackFormData] = useState<RackFormData | null>(null);
  const [manageRackId, setManageRackId] = useState<bigint | undefined>(undefined);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

  // Seed the refs with the URL-derived initial values so the first
  // useDeviceSetListState fetch (which runs in a child effect, before
  // RacksPage's own effects) picks up filters from a `?issues=` / `?zone=`
  // deep link or a restored saved view. Render-time writes are idempotent on
  // re-render and avoid a stale-ref window on mount.
  const selectedZonesRef = useRef<string[]>(selectedZones);
  // eslint-disable-next-line react-hooks/refs -- intentional render-time sync; initial mount + subsequent URL changes
  selectedZonesRef.current = selectedZones;
  const getZones = useCallback(() => selectedZonesRef.current, []);
  // eslint-disable-next-line react-hooks/refs -- intentional render-time sync; selectedIssuesRef comes from useIssueFilter so we can't seed it via useRef init
  selectedIssuesRef.current = selectedIssues;

  // Rack sort is URL-driven so saved views can capture and restore it (and so
  // deep-links carrying `?sort=&dir=` land on the right ordering). Grid mode
  // sets sort via the dropdown; list mode sets it via column headers; both
  // resolve to the same `?sort=field&dir=asc|desc` URL state.
  const urlRackSort = useMemo<{ field: DeviceSetColumn; direction: SortDirection } | undefined>(() => {
    const fieldRaw = searchParams.get("sort");
    if (!fieldRaw || !SORTABLE_COLUMNS.has(fieldRaw as DeviceSetColumn)) return undefined;
    const dirRaw = searchParams.get("dir");
    const direction: SortDirection = dirRaw === SORT_DESC ? SORT_DESC : SORT_ASC;
    return { field: fieldRaw as DeviceSetColumn, direction };
  }, [searchParams]);
  // Capture-once initializer for the hook — only the value at mount matters,
  // since subsequent URL changes flow through the sync effect below.
  const initialSortRef = useRef(urlRackSort);
  const getInitialRackSort = useCallback(() => initialSortRef.current ?? { field: "name", direction: SORT_ASC }, []);

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
  } = useDeviceSetListState(
    listRacks,
    DEFAULT_PAGE_SIZE,
    getErrorComponentTypes,
    getZones,
    getBuildingIds,
    getSiteFilter,
    getInitialRackSort,
    getSiteIds,
    getIncludeUnassigned,
    getIncludeNoBuilding,
    getTelemetryRanges,
  );

  // Propagate external URL sort changes (saved view activation, deep-link
  // nav) into the hook. When the page itself drives the change via
  // handleRackSort, currentSort and urlRackSort end up in sync on the same
  // render and this effect no-ops.
  useEffect(() => {
    const urlField = urlRackSort?.field ?? "name";
    const urlDirection = urlRackSort?.direction ?? SORT_ASC;
    if (urlField !== currentSort.field || urlDirection !== currentSort.direction) {
      handleSort(urlField, urlDirection);
    }
  }, [urlRackSort, currentSort, handleSort]);

  const storedRacksViewMode = useFleetStore((s) => s.ui.racksViewMode);
  const setStoredRacksViewMode = useFleetStore((s) => s.ui.setRacksViewMode);
  const temperatureUnit = useFleetStore((s) => s.ui.temperatureUnit);

  // URL is the source of truth for the segmented control so a saved view's
  // `display` param can dictate grid vs. list. Falls back to the persisted
  // Zustand preference when the param is absent so default sessions keep
  // the operator's last choice.
  //
  // We deliberately do NOT auto-write the stored mode into the URL: doing so
  // would re-add `display=` immediately after a user activates a view that
  // intentionally omits display via the "Include display mode" toggle, making
  // that view permanently dirty.
  const urlRacksViewMode: "grid" | "list" | undefined = (() => {
    const raw = searchParams.get("display");
    return raw === "grid" || raw === "list" ? raw : undefined;
  })();
  const racksViewMode = urlRacksViewMode ?? storedRacksViewMode;

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

  const createFlow = useFleetCreateFlow();
  const entitiesChangedAt = createFlow?.entitiesChangedAt ?? 0;

  // Org-scoped parent catalogs powering the Site/Building columns + filter
  // labels. Extracted into callbacks so the create-flow pulse can refresh
  // them — a building/site created from this tab must resolve to a name
  // immediately, not only after a remount.
  const fetchBuildingCatalog = useCallback(
    (signal?: AbortSignal) => {
      if (!canReadSiteCatalog) return;
      void listAllBuildings({
        signal,
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
        },
      });
    },
    [canReadSiteCatalog, listAllBuildings],
  );

  const fetchSiteCatalog = useCallback(
    (signal?: AbortSignal) => {
      if (!canReadSiteCatalog) return;
      void listSites({
        signal,
        onSuccess: (sites: SiteWithCounts[]) => {
          setAllSites(
            sites.filter((s) => s.site !== undefined).map((s) => ({ id: s.site!.id.toString(), label: s.site!.name })),
          );
        },
        onError: () => setAllSites((prev) => prev),
      });
    },
    [canReadSiteCatalog, listSites],
  );

  // Refetch when the hoisted create flow commits a new entity so this list
  // AND its parent-name catalogs reflect it without waiting for the next poll.
  // entitiesChangedAt starts at 0 and only bumps post-create, so the initial
  // mount is a no-op.
  useEffect(() => {
    if (entitiesChangedAt === 0) return;
    resetAndFetch();
    fetchZones();
    fetchBuildingCatalog();
    fetchSiteCatalog();
  }, [entitiesChangedAt, resetAndFetch, fetchZones, fetchBuildingCatalog, fetchSiteCatalog]);

  // One-shot loads on mount — org-scoped catalogs are small + stable.
  useEffect(() => {
    const controller = new AbortController();
    fetchBuildingCatalog(controller.signal);
    return () => controller.abort();
  }, [fetchBuildingCatalog]);

  useEffect(() => {
    const controller = new AbortController();
    fetchSiteCatalog(controller.signal);
    return () => controller.abort();
  }, [fetchSiteCatalog]);

  const siteNameById = useMemo(() => new Map((allSites ?? []).map((s) => [s.id, s.label])), [allSites]);
  const buildingNameById = useMemo(() => new Map(allBuildings.map((b) => [b.id, b.label])), [allBuildings]);
  const buildingById = useMemo(() => new Map(allBuildings.map((b) => [b.id, b])), [allBuildings]);

  // Detect racks whose `building_id` would be NULL'd by the upcoming
  // AssignRacksToSite write (server clears building_id whenever the
  // target site differs from the rack's current building's site).
  // Returns the set of building labels we'd be evicting from and the
  // number of distinct racks affected, so the confirm dialog can render
  // an actionable summary. Returns null when nothing would be cleared,
  // letting the caller skip the prompt and dispatch directly.
  //
  // `unresolved` flags racks that carry a building_id we can't classify
  // because the org-scoped building metadata isn't available (the
  // one-shot listAllBuildings is still loading, failed, or is stale).
  // We can't tell whether the move crosses sites, but the server WILL
  // clear building_id if it does — so these count toward the prompt
  // rather than being silently skipped, which would let the move bypass
  // the confirmation guard entirely.
  const summarizeBuildingClearance = useCallback(
    (
      rackIds: bigint[],
      targetSiteId: bigint,
    ): { buildingLabels: string[]; rackCount: number; unresolved: boolean } | null => {
      const wantedIds = new Set(rackIds.map((id) => id.toString()));
      const targetSiteIdStr = targetSiteId.toString();
      const labels = new Set<string>();
      let count = 0;
      let unresolved = false;
      for (const rack of racks) {
        if (!wantedIds.has(rack.id.toString())) continue;
        if (rack.typeDetails.case !== "rackInfo") continue;
        const buildingId = rack.typeDetails.value.buildingId;
        if (buildingId === undefined) continue;
        const building = buildingById.get(buildingId.toString());
        if (!building) {
          // Building metadata unavailable — can't classify this rack's
          // move as cross-site or not. Prompt conservatively.
          unresolved = true;
          count += 1;
          continue;
        }
        // Same site → server keeps building_id intact; nothing to warn about.
        if (building.siteId === targetSiteIdStr) continue;
        labels.add(building.label || "(unnamed)");
        count += 1;
      }
      if (count === 0) return null;
      return { buildingLabels: Array.from(labels), rackCount: count, unresolved };
    },
    [racks, buildingById],
  );

  // Outer picker promise resolver, parked here so the Dialog's
  // Cancel button can settle it without dispatching the RPC. Cleared
  // on every Continue/Cancel transition.
  const siteClearResolveRef = useRef<((ok: boolean) => void) | null>(null);

  // Wrap assignRacksToSite with the building-clearance confirm gate so
  // every rack→site path (single-row + bulk) prompts before silently
  // clearing rack.building_id on cross-site moves. Returns a promise
  // that resolves true on success, false on operator cancel, and
  // rejects on RPC failure so the picker's onConfirm chain stays
  // identical to the no-confirm shape.
  const dispatchRackSiteAssign = useCallback(
    (rackIds: bigint[], targetSiteId: bigint, subjectLabel: string): Promise<boolean> => {
      const performAssign = () =>
        new Promise<boolean>((resolve, reject) => {
          void assignRacksToSite({
            rackIds,
            targetSiteId,
            onSuccess: () => {
              pushToast({ message: `Moved ${subjectLabel} to selected site.`, status: STATUSES.success });
              resetAndFetch();
              resolve(true);
            },
            onError: (msg) => {
              pushToast({ message: `Couldn't move ${subjectLabel}: ${msg}`, status: STATUSES.error });
              reject(new Error(msg));
            },
          });
        });

      const clearance = summarizeBuildingClearance(rackIds, targetSiteId);
      if (clearance === null) {
        return performAssign();
      }
      return new Promise<boolean>((resolve, reject) => {
        siteClearResolveRef.current = resolve;
        setSiteClearConfirmation({
          affectedBuildingLabels: clearance.buildingLabels,
          affectedRackCount: clearance.rackCount,
          unresolved: clearance.unresolved,
          // The Dialog's Continue button awaits this; on success it
          // resolves(true), on RPC failure rejects the outer picker
          // promise so the modal surfaces an error toast and stays open.
          onConfirm: async () => {
            setSiteClearInFlight(true);
            try {
              const ok = await performAssign();
              siteClearResolveRef.current = null;
              setSiteClearInFlight(false);
              setSiteClearConfirmation(null);
              resolve(ok);
            } catch (err) {
              siteClearResolveRef.current = null;
              setSiteClearInFlight(false);
              setSiteClearConfirmation(null);
              reject(err instanceof Error ? err : new Error(String(err)));
            }
          },
        });
      });
    },
    [assignRacksToSite, resetAndFetch, summarizeBuildingClearance],
  );

  // Building peer of dispatchRackSiteAssign, shared by the single-row and
  // bulk rack→building paths. No building-clearance gate: a building move
  // doesn't silently clear rack.building_id the way a cross-site move
  // does. Resolves true on success, rejects on RPC failure so each
  // caller's onConfirm chain stays identical to the site shape.
  const dispatchRackBuildingAssign = useCallback(
    (rackIds: bigint[], targetBuildingId: bigint, subjectLabel: string): Promise<boolean> =>
      new Promise<boolean>((resolve, reject) => {
        void assignRacksToBuilding({
          racks: rackIds.map((rackId) => ({ rackId })),
          targetBuildingId,
          onSuccess: () => {
            pushToast({ message: `Moved ${subjectLabel} to selected building.`, status: STATUSES.success });
            resetAndFetch();
            resolve(true);
          },
          onError: (msg) => {
            pushToast({ message: `Couldn't move ${subjectLabel}: ${msg}`, status: STATUSES.error });
            reject(new Error(msg));
          },
        });
      }),
    [assignRacksToBuilding, resetAndFetch],
  );

  // Wired to the Cancel button on the building-clear dialog. Resolves
  // the outer picker promise as a no-op so ParentPickerModal closes
  // without dispatching the RPC.
  const cancelSiteClearConfirmation = useCallback(() => {
    const resolve = siteClearResolveRef.current;
    siteClearResolveRef.current = null;
    setSiteClearConfirmation(null);
    resolve?.(false);
  }, []);

  // Surface buildings + sites to FleetLayout so the saved-view modal can
  // render human-readable labels when a view captures `building=` or
  // `site=` params. Guarded — RacksPage also mounts at standalone /racks
  // where there is no parent Outlet.
  const outletContext = useOptionalFleetOutletContext();
  const buildingSources = useMemo(() => allBuildings.map(({ id, label }) => ({ id, label })), [allBuildings]);
  useEffect(() => {
    outletContext?.publishViewFilterContext({
      availableBuildings: buildingSources,
      availableSites: allSites ?? [],
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
            if (trimmed === UNASSIGNED_URL_VALUE || (trimmed && /^\d+$/.test(trimmed))) {
              next.append(BUILDING_URL_PARAM, trimmed);
            }
          });
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Resolved building-filter key used by the combined URL-filter refetch below.
  // Site scoping moved to the server-side site_ids filter.
  const effectiveBuildingKey = useMemo(() => selectedBuildingIds.map(String).join(","), [selectedBuildingIds]);

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

  const effectiveSiteKey = useMemo(
    () =>
      `${effectiveSiteFilter.siteIds.map(String).join(",")}|${effectiveSiteFilter.includeUnassigned}|${effectiveSiteFilter.matchNone ?? false}`,
    [effectiveSiteFilter],
  );
  const prevSiteKey = useRef<string | null>(null);
  useEffect(() => {
    if (prevSiteKey.current !== null && prevSiteKey.current !== effectiveSiteKey) {
      // Drop the prior scope's selection so the bulk-action bar can't stay
      // active on racks that belong to a site the picker no longer shows.
      // (Out-of-order list responses are already dropped by
      // useDeviceSetListState's request-id guard, so rows can't go stale.)
      setSelectedRackIds([]);
    }
    prevSiteKey.current = effectiveSiteKey;
  }, [effectiveSiteKey]);

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
      if (key === "site") {
        writeMultiParam("site", values);
        return;
      }
      if (key === "building") {
        setBuildingFilter(values);
      }
    },
    [setBuildingFilter, writeMultiParam],
  );

  const handleNumericFilterChange = useCallback(
    (key: string, value: NumericRangeValue) => {
      setSelectedRackIds([]);
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

  // Refetch when any URL-derived filter input changes. Site, building, zone,
  // and issues are combined into a single effect so a navigation that updates
  // more than one of them (e.g. "Clear filters" or activating a saved view)
  // produces one fetch, not several.
  //
  // `JSON.stringify` (not a delimiter-joined string) is used to encode the
  // arrays so values containing the delimiter character — e.g. a zone label
  // like "DC1, Row A" — can't collide with a different selection that
  // happens to produce the same joined output.
  const filterFetchKey = useMemo(
    () =>
      JSON.stringify([
        effectiveSiteKey,
        effectiveBuildingKey,
        includeNoBuilding,
        selectedZones,
        selectedIssues,
        telemetryRanges,
      ]),
    [effectiveSiteKey, effectiveBuildingKey, includeNoBuilding, selectedZones, selectedIssues, telemetryRanges],
  );
  const prevFilterFetchKey = useRef<string | null>(null);
  useEffect(() => {
    if (prevFilterFetchKey.current !== null && prevFilterFetchKey.current !== filterFetchKey) {
      resetAndFetch();
    }
    prevFilterFetchKey.current = filterFetchKey;
  }, [filterFetchKey, resetAndFetch]);

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
        options: [...(allSites ?? []), UNASSIGNED_FILTER_OPTION],
        selectedValues: selectedSiteValues,
      },
      {
        key: "building",
        title: "Buildings",
        pluralTitle: "buildings",
        options: [...allBuildings, UNASSIGNED_FILTER_OPTION],
        selectedValues: selectedBuildingIdStrings,
      },
      {
        key: "zone",
        title: "Zones",
        pluralTitle: "zones",
        options: allZones,
        selectedValues: selectedZones,
        showGroupDivider: true,
      },
    ],
    [allSites, selectedSiteValues, allBuildings, selectedBuildingIdStrings, allZones, selectedZones, selectedIssues],
  );

  const hasActiveFilters =
    selectedSiteValues.length > 0 ||
    selectedBuildingIdStrings.length > 0 ||
    selectedZones.length > 0 ||
    selectedIssues.length > 0 ||
    telemetryRanges.length > 0;

  // Unfiltered rack count for the "X of Y racks" line. `totalCount` from the
  // list hook is the filtered total; this fetches the path-scope total (no
  // zone/building/issue/`?site=`/telemetry filters) so the count line can show
  // a denominator, mirroring the Miners tab. Only fetched while filters are
  // active — otherwise the displayed count already is the total.
  const [totalUnfilteredRacks, setTotalUnfilteredRacks] = useState<number | undefined>(undefined);
  useEffect(() => {
    // Only consumed while filters are active (the count line otherwise shows
    // `totalCount`). Skip the fetch when filters are off and leave the last
    // value — it's ignored, and a scope change re-runs this effect.
    if (!hasActiveFilters) return;
    let cancelled = false;
    // `racks` is in the deps as the list-refresh signal: its reference changes
    // whenever the rack list refetches (poll, resetAndFetch, create/delete/move),
    // so this cheap pageSize:1 count stays in sync with `totalCount` instead of
    // going stale until the filter/scope changes. The count itself is
    // page-independent, so re-running it on pagination is harmless.
    void listRacks({
      pageSize: 1,
      siteIds: activeSiteFilter.siteIds,
      includeUnassigned: activeSiteFilter.includeUnassigned,
      onSuccess: (_sets, _token, total) => {
        if (!cancelled) setTotalUnfilteredRacks(total);
      },
      onError: () => {
        if (!cancelled) setTotalUnfilteredRacks(undefined);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [hasActiveFilters, activeSiteFilter, listRacks, racks]);
  const visibleRackScopes = useMemo(
    () =>
      racks.flatMap((rack) => {
        if (rack.id === 0n) return [];
        // Carry the rack's current building so the bulk Add-to-building
        // dispatch can drop no-op moves (a same-building request without a
        // grid position is treated server-side as an explicit unplace).
        // siteId rides along for the "New site" conflict pre-warning.
        const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
        const buildingId = rackInfo?.buildingId ?? 0n;
        const siteId = rackInfo?.siteId ?? 0n;
        return [{ kind: "rack" as const, id: rack.id, name: rack.label || "(unnamed)", buildingId, siteId }];
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
      selectedSiteValues.length > 0 ||
      selectedBuildingIdStrings.length > 0 ||
      selectedZones.length > 0 ||
      selectedIssues.length > 0 ||
      telemetryRanges.length > 0;
    if (hadAny) {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          FILTER_URL_PARAM_KEYS.forEach((key) => next.delete(key));
          return next;
        },
        { replace: true },
      );
    } else {
      // No URL transition → manually kick the refetch (URL effects skip).
      resetAndFetch();
    }
  }, [
    resetAndFetch,
    selectedBuildingIdStrings,
    selectedIssues,
    selectedSiteValues,
    selectedZones,
    setSearchParams,
    telemetryRanges,
  ]);

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
  // Rack Settings "Continue" persists an existing rack's settings before the
  // final miner Save, so refresh the list in the background (without closing
  // the modal) to keep it consistent if the operator then dismisses.
  const handleRackSettingsPersisted = useCallback(() => {
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
      // Seed the rack's current placement (`|| undefined` collapses the proto
      // default 0 to unassigned) so the settings dropdowns and the miner
      // eligibility filter reflect where the rack lives.
      siteId: rack.placement?.site?.id || undefined,
      buildingId: rack.placement?.building?.id || undefined,
    });
    setManageRackId(rack.id);
  }, []);

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
        onClick: () => setReparentTarget({ rack, kind: "building" }),
        hidden: !canManageSitePlacement,
      },
      {
        label: "Add to site",
        icon: <Plus />,
        onClick: () => setReparentTarget({ rack, kind: "site" }),
        hidden: !canManageSitePlacement,
      },
    ],
    [navigate, handleEditRack, canEditRack, canManageSitePlacement],
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
      if (item.deviceSet.typeDetails.case !== "rackInfo") return <span />;
      const siteId = item.deviceSet.typeDetails.value.siteId;
      if (siteId === undefined) return <span />;
      return <span>{siteNameById.get(siteId.toString()) ?? ""}</span>;
    },
    [siteNameById],
  );

  const renderBuilding = useCallback(
    (item: DeviceSetListItem) => {
      if (item.deviceSet.typeDetails.case !== "rackInfo") return <span />;
      const buildingId = item.deviceSet.typeDetails.value.buildingId;
      if (buildingId === undefined) return <span />;
      return <span>{buildingNameById.get(buildingId.toString()) ?? ""}</span>;
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

  // Sort handler shared by the grid dropdown and the list column headers.
  // Writes the URL first; the sync effect above propagates to the hook.
  const handleRackSort: typeof handleSort = useCallback(
    (field, direction) => {
      setSelectedRackIds([]);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("sort", field);
          next.set("dir", direction);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
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
          className={clsx("sticky left-0", PAGE_SCROLL_CHROME_WIDTH)}
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
            defaultSiteId={scopedSiteId}
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
            scopedSiteId={scopedSiteId}
            onDismiss={handleManageRackDismiss}
            onSave={handleManageRackSave}
            onSettingsPersisted={handleRackSettingsPersisted}
            onDelete={manageRackId ? handleDeleteRack : undefined}
          />
        ) : null}
      </>
    );
  }

  return (
    <div>
      <div className={clsx("sticky left-0 z-3 px-6 pt-6 laptop:px-10 laptop:pt-10", PAGE_SCROLL_CHROME_WIDTH)}>
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
              numericFilters={TELEMETRY_FILTER_CHIPS}
              selectedNumericValues={selectedNumericValues}
              onNumericChange={handleNumericFilterChange}
              onClearAll={handleClearFilters}
            />
            {racksViewMode === "grid" ? (
              <DropdownFilter
                title="Sort"
                options={RACK_SORT_OPTIONS}
                selectedOptions={[currentSort.field]}
                onSelect={handleSortSelect}
                showSelectAll={false}
                closeOnSelect
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
              numericFilters={TELEMETRY_FILTER_CHIPS}
              selectedNumericValues={selectedNumericValues}
              onNumericChange={handleNumericFilterChange}
              onClearAll={handleClearFilters}
            />
            {racksViewMode === "grid" ? (
              <DropdownFilter
                title="Sort"
                options={RACK_SORT_OPTIONS}
                selectedOptions={[currentSort.field]}
                onSelect={handleSortSelect}
                showSelectAll={false}
                closeOnSelect
              />
            ) : null}
          </div>
        </div>
      </div>
      <div
        className={clsx(
          "sticky left-0 px-6 pb-4 text-emphasis-300 text-text-primary-70 laptop:px-10",
          PAGE_SCROLL_CHROME_WIDTH,
        )}
        data-testid="racks-count-label"
      >
        {formatListCountLabel(totalCount, {
          unfilteredTotal: totalUnfilteredRacks,
          hasActiveFilters,
          singular: "rack",
          plural: "racks",
        })}
      </div>
      {error ? (
        <Callout className="mx-6 mb-4 laptop:mx-10" intent="danger" prefixIcon={<Alert />} title={error} />
      ) : null}
      {racksViewMode === "list" ? (
        // No horizontal padding or overflow wrapper here: that inset the table
        // (white gaps beside the row rules) and added a second scroll
        // container. Row content is indented via DeviceSetList's paddingLeft
        // so the rules still span the full width, and the page is the single
        // scroll container.
        <div className="pb-6 laptop:pb-10">
          <DeviceSetList
            deviceSets={racks}
            statsMap={statsMap}
            renderName={renderName}
            renderMiners={renderMiners}
            renderSite={renderSite}
            renderBuilding={renderBuilding}
            columns={insideFleetShell ? RACK_COLUMNS_FLEET : RACK_COLUMNS_STANDALONE}
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
            paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
            overflowContainer={false}
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
          bulkExtraActions={[
            {
              label: "Add to building",
              icon: <Plus />,
              testId: "fleet-bulk-rack-actions-add-to-building",
              onClick: () => setBulkReparentKind("building"),
              hidden: !canManageSitePlacement,
            },
            {
              label: "Add to site",
              icon: <Plus />,
              testId: "fleet-bulk-rack-actions-add-to-site",
              onClick: () => setBulkReparentKind("site"),
              hidden: !canManageSitePlacement,
            },
          ]}
          onClearSelection={handleClearRackSelection}
          onSelectAllVisible={handleSelectAllVisibleRacks}
          onActionBusyChange={setIsBulkActionBusy}
        />
      ) : null}
      {bulkReparentKind ? (
        <ParentPickerModal
          kind={bulkReparentKind}
          show
          selectionMode="single"
          sourceLabel={
            selectedRackScopes.length === 1 ? selectedRackScopes[0]!.name : `${selectedRackScopes.length} racks`
          }
          createNewLaunchLabel={
            createFlow ? (bulkReparentKind === "building" ? "New building" : "New site") : undefined
          }
          onCreateNewLaunch={
            createFlow
              ? () => {
                  const rackIds = selectedRackScopes.map((scope) => scope.id);
                  setBulkReparentKind(null);
                  setSelectedRackIds([]);
                  if (bulkReparentKind === "building") {
                    // A rack in another building OR directly in another site
                    // will be moved into the new building's site.
                    const conflictCount = selectedRackScopes.filter(
                      (scope) => scope.buildingId !== 0n || scope.siteId !== 0n,
                    ).length;
                    createFlow.launchCreateBuilding({ rackIds, minerIds: [], conflictCount });
                  } else {
                    // A rack directly in another site OR in a building (whose
                    // building_id AssignRacksToSite clears on the cross-site
                    // move) is displaced — warn for both.
                    const conflictCount = selectedRackScopes.filter(
                      (scope) => scope.siteId !== 0n || scope.buildingId !== 0n,
                    ).length;
                    createFlow.launchCreateSite({ buildingIds: [], rackIds, minerIds: [], conflictCount });
                  }
                }
              : undefined
          }
          onDismiss={() => setBulkReparentKind(null)}
          onConfirm={(parentIds) =>
            new Promise<void>((resolve, reject) => {
              const parentId = parentIds[0];
              if (parentId === undefined) {
                resolve();
                return;
              }
              const buildingMode = bulkReparentKind === "building";
              // Drop no-op building moves: a same-building request without a
              // grid position is an explicit unplace server-side, so leaving
              // already-in-building racks in the batch would silently clear
              // their placement.
              const rackIds = buildingMode
                ? selectedRackScopes.filter((scope) => scope.buildingId !== parentId).map((scope) => scope.id)
                : selectedRackScopes.map((scope) => scope.id);
              if (buildingMode && rackIds.length === 0) {
                pushToast({ message: "Selected racks are already in that building.", status: STATUSES.queued });
                setBulkReparentKind(null);
                setSelectedRackIds([]);
                resolve();
                return;
              }
              const subjectLabel = `${rackIds.length} ${rackIds.length === 1 ? "rack" : "racks"}`;
              const dispatch = buildingMode
                ? dispatchRackBuildingAssign(rackIds, parentId, subjectLabel)
                : dispatchRackSiteAssign(rackIds, parentId, subjectLabel);
              dispatch
                .then((ok) => {
                  if (ok) {
                    setBulkReparentKind(null);
                    setSelectedRackIds([]);
                  }
                  resolve();
                })
                .catch((err) => reject(err instanceof Error ? err : new Error(String(err))));
            })
          }
        />
      ) : null}
      {showRackSettingsModal ? (
        <RackSettingsModal
          show={showRackSettingsModal}
          existingRacks={racks}
          defaultSiteId={scopedSiteId}
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
          scopedSiteId={scopedSiteId}
          onDismiss={handleManageRackDismiss}
          onSave={handleManageRackSave}
          onSettingsPersisted={handleRackSettingsPersisted}
        />
      ) : null}
      {reparentTarget ? (
        <ParentPickerModal
          kind={reparentTarget.kind}
          show
          selectionMode="single"
          sourceLabel={reparentTarget.rack.label || "rack"}
          description={
            reparentTarget.rack.deviceCount > 0
              ? `${reparentTarget.rack.deviceCount} ${reparentTarget.rack.deviceCount === 1 ? "miner" : "miners"} will move with this rack.`
              : undefined
          }
          currentParentId={
            reparentTarget.rack.typeDetails.case === "rackInfo"
              ? reparentTarget.kind === "building"
                ? reparentTarget.rack.typeDetails.value.buildingId
                : reparentTarget.rack.typeDetails.value.siteId
              : undefined
          }
          createNewLaunchLabel={
            createFlow ? (reparentTarget.kind === "building" ? "New building" : "New site") : undefined
          }
          onCreateNewLaunch={
            createFlow
              ? () => {
                  const rack = reparentTarget.rack;
                  const targetKind = reparentTarget.kind;
                  const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
                  setReparentTarget(null);
                  if (targetKind === "building") {
                    const buildingId = rackInfo?.buildingId ?? 0n;
                    const siteId = rackInfo?.siteId ?? 0n;
                    createFlow.launchCreateBuilding({
                      rackIds: [rack.id],
                      minerIds: [],
                      conflictCount: buildingId !== 0n || siteId !== 0n ? 1 : 0,
                    });
                  } else {
                    const siteId = rackInfo?.siteId ?? 0n;
                    const buildingId = rackInfo?.buildingId ?? 0n;
                    createFlow.launchCreateSite({
                      buildingIds: [],
                      rackIds: [rack.id],
                      minerIds: [],
                      // AssignRacksToSite clears building_id on the cross-site
                      // move, so a building-held rack is displaced too.
                      conflictCount: siteId !== 0n || buildingId !== 0n ? 1 : 0,
                    });
                  }
                }
              : undefined
          }
          onDismiss={() => setReparentTarget(null)}
          onConfirm={(parentIds) =>
            new Promise<void>((resolve, reject) => {
              const parentId = parentIds[0];
              if (parentId === undefined) {
                resolve();
                return;
              }
              const rackName = reparentTarget.rack.label || "rack";
              const currentBuildingId =
                reparentTarget.rack.typeDetails.case === "rackInfo"
                  ? reparentTarget.rack.typeDetails.value.buildingId
                  : 0n;
              // No-op building move: a same-building request without a grid
              // position is an explicit unplace server-side, so don't dispatch
              // it (it would silently clear this rack's placement).
              if (reparentTarget.kind === "building" && currentBuildingId === parentId) {
                pushToast({ message: `"${rackName}" is already in that building.`, status: STATUSES.queued });
                setReparentTarget(null);
                resolve();
                return;
              }
              const dispatch =
                reparentTarget.kind === "building"
                  ? dispatchRackBuildingAssign([reparentTarget.rack.id], parentId, `"${rackName}"`)
                  : dispatchRackSiteAssign([reparentTarget.rack.id], parentId, `"${rackName}"`);
              dispatch
                .then((ok) => {
                  if (ok) setReparentTarget(null);
                  resolve();
                })
                .catch((err) => reject(err instanceof Error ? err : new Error(String(err))));
            })
          }
        />
      ) : null}
      {siteClearConfirmation ? (
        <Dialog
          open
          title="Move racks between sites?"
          subtitle={siteClearSubtitle(
            siteClearConfirmation.affectedBuildingLabels,
            siteClearConfirmation.affectedRackCount,
            siteClearConfirmation.unresolved,
          )}
          onDismiss={() => {
            if (siteClearInFlight) return;
            cancelSiteClearConfirmation();
          }}
          buttons={[
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: cancelSiteClearConfirmation,
              disabled: siteClearInFlight,
            },
            {
              text: "Continue",
              variant: variants.primary,
              onClick: () => {
                void siteClearConfirmation.onConfirm();
              },
              loading: siteClearInFlight,
              disabled: siteClearInFlight,
            },
          ]}
        />
      ) : null}
    </div>
  );
};

export default RacksPage;
