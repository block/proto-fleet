import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import { clone, create } from "@bufbuild/protobuf";

import { useBuildings } from "@/protoFleet/api/buildings";
import {
  SortConfigSchema,
  SortDirection as SortDirectionProto,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot as ProtoMinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  IpRangeSchema,
  type MinerListFilter,
  MinerListFilterSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useFleet from "@/protoFleet/api/useFleet";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import { INACTIVE_PLACEHOLDER } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import {
  getMinerBuildingId,
  getMinerBuildingLabel,
  getMinerGroupLabels,
  getMinerRackId,
  getMinerRackLabel,
  getMinerSiteId,
  getMinerSiteLabel,
  isPlacementIneligible,
  type MinerEligibility,
} from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import { useHasPermission } from "@/protoFleet/store";

import { Alert, ChevronDown, Info, Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import List from "@/shared/components/List";
import type { ActiveFilters, FilterItem, NestedFilterChildItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles, SortDirection } from "@/shared/components/List/types";
import { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";
import { classifySubnetLine, normalizeSubnetLine, validateSubnetLine } from "@/shared/utils/filterValidation";

// --- Exported types ---

export type DeviceListItem = {
  deviceIdentifier: string;
  name: string;
  model: string;
  ipAddress: string;
  rackLabel: string;
  siteLabel: string;
  buildingLabel: string;
  groupLabels: string[];
  // Placement identity (id-based), undefined when the miner is unassigned at
  // that level. Drives eligibility checks that can't rely on labels (a
  // same-named rack in another building would otherwise slip past).
  rackId?: bigint;
  siteId?: bigint;
  buildingId?: bigint;
};

export type FilterConfig = {
  showTypeFilter?: boolean;
  showRackFilter?: boolean;
  showGroupFilter?: boolean;
  showSubnetFilter?: boolean;
  showSiteFilter?: boolean;
  showBuildingFilter?: boolean;
};

export type { MinerEligibility };

export interface MinerSelectionListHandle {
  getSelection: () => {
    selectedItems: string[];
    allSelected: boolean;
    totalMiners: number | undefined;
    filter: MinerListFilter;
    // Selected ids that are currently assigned to a different rack/building/site
    // (reassigning them moves them). Covers off-page selections — placement is
    // tracked for every item seen across pages, and a miner can only be selected
    // from a page it appeared on.
    reassignedItems: string[];
    // True when a placement facet conflicts with the target rack, so the list is
    // showing an empty "no results" state. The selection is preserved (clearing
    // the facet restores it), but callers must not act on it — committing would
    // save a selection the operator can't see. Restored to false once the facet
    // is cleared or changed.
    blockedByFilter: boolean;
  };
}

export interface MinerSelectionListProps {
  filterConfig?: FilterConfig;
  initialAllSelected?: boolean;
  initialSelectedItems?: string[];
  isMembersLoading?: boolean;
  isRowDisabled?: (item: DeviceListItem) => boolean;
  /** When true, renders radio buttons for single-item selection instead of checkboxes. */
  singleSelect?: boolean;
  disableFilteredSelectAll?: boolean;
  showSelectAllFooter?: boolean;
  // Soft default from the topbar SitePicker. A single selected site limits the
  // miner list and its rack facet options to that site; "all sites" passes the
  // empty filter and shows everything (no regression). Folded into the
  // MinerListFilter (AND with the user's model/rack/group facets) so applying a
  // facet never drops the site scope.
  scope?: SiteFilterFields;
  // Target rack placement. When set, renders a "Show assigned miners" toggle
  // (default off) that folds rack/building/site eligibility into the server
  // filter, so miners already assigned to another rack/building/site drop out.
  // Turning it on surfaces those miners — still selectable (reassigning moves
  // them, behind a caller confirm) and flagged in orange to show the existing
  // placement.
  eligibility?: MinerEligibility;
  // Label of the target rack, shown in the assignment-conflict dialog.
  targetRackLabel?: string;
  onSelectionChange?: (state: {
    selectedItems: string[];
    allSelected: boolean;
    totalMiners: number | undefined;
  }) => void;
}

// --- Constants ---

const modalCols = {
  name: "name",
  type: "type",
  ipAddress: "ipAddress",
  site: "site",
  building: "building",
  rack: "rack",
  group: "group",
} as const;

type ModalColumn = (typeof modalCols)[keyof typeof modalCols];

const modalColTitles: ColTitles<ModalColumn> = {
  name: "Name",
  type: "Model",
  ipAddress: "IP address",
  site: "Site",
  building: "Building",
  rack: "Rack",
  group: "Group",
};

const activeCols: ModalColumn[] = [
  modalCols.name,
  modalCols.type,
  modalCols.ipAddress,
  modalCols.site,
  modalCols.building,
  modalCols.rack,
  modalCols.group,
];

const modalColConfig: ColConfig<DeviceListItem, string, ModalColumn> = {
  [modalCols.name]: {
    component: (device: DeviceListItem) => <span>{device.name || device.deviceIdentifier}</span>,
    width: "min-w-28",
  },
  [modalCols.type]: {
    component: (device: DeviceListItem) => <span>{device.model || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-20",
  },
  [modalCols.ipAddress]: {
    component: (device: DeviceListItem) => <span>{device.ipAddress || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-24",
  },
  [modalCols.site]: {
    component: (device: DeviceListItem) => <span>{device.siteLabel || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-24",
  },
  [modalCols.building]: {
    component: (device: DeviceListItem) => <span>{device.buildingLabel || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-24",
  },
  [modalCols.rack]: {
    component: (device: DeviceListItem) => <span>{device.rackLabel || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-28",
  },
  [modalCols.group]: {
    component: (device: DeviceListItem) => {
      const label = device.groupLabels.length > 0 ? device.groupLabels.join(", ") : INACTIVE_PLACEHOLDER;
      return <span title={label}>{label}</span>;
    },
    width: "min-w-24 max-w-48",
  },
};

/** Columns that support server-side sorting, mapped to their proto SortField. */
const SORT_FIELD_BY_COLUMN: Partial<Record<ModalColumn, SortField>> = {
  [modalCols.name]: SortField.NAME,
  [modalCols.type]: SortField.MODEL,
  [modalCols.ipAddress]: SortField.IP_ADDRESS,
};

const ALL_SORTABLE_COLUMNS = new Set<ModalColumn>(Object.keys(SORT_FIELD_BY_COLUMN) as ModalColumn[]);

const PAGE_SIZE = 50;

const hasUnsupportedAllSelectionFilter = (filter: MinerListFilter): boolean =>
  filter.models.length > 0 ||
  filter.rackIds.length > 0 ||
  filter.groupIds.length > 0 ||
  filter.siteIds.length > 0 ||
  filter.buildingIds.length > 0 ||
  filter.ipCidrs.length > 0 ||
  filter.ipRanges.length > 0 ||
  filter.includeUnassigned;

const toDeviceListItem = (miner: ProtoMinerStateSnapshot): DeviceListItem => ({
  deviceIdentifier: miner.deviceIdentifier,
  name: miner.name,
  model: miner.model,
  ipAddress: miner.ipAddress,
  rackLabel: getMinerRackLabel(miner),
  siteLabel: getMinerSiteLabel(miner),
  buildingLabel: getMinerBuildingLabel(miner),
  groupLabels: getMinerGroupLabels(miner),
  rackId: getMinerRackId(miner),
  siteId: getMinerSiteId(miner),
  buildingId: getMinerBuildingId(miner),
});

/** Copy for the assignment-conflict dialog. Lists only the placement levels the
 *  miner currently occupies, joined naturally, so it reads correctly whether it
 *  has a site, a site + building, or a full site + building + rack placement. */
const describeReassignment = (item: DeviceListItem, targetRackLabel?: string): string => {
  const parts: string[] = [];
  if (item.siteLabel) parts.push(`site ${item.siteLabel}`);
  if (item.buildingLabel) parts.push(`building ${item.buildingLabel}`);
  if (item.rackLabel) parts.push(`rack ${item.rackLabel}`);
  const current =
    parts.length === 0
      ? "its current placement"
      : parts.length === 1
        ? parts[0]
        : `${parts.slice(0, -1).join(", ")}, and ${parts[parts.length - 1]}`;
  const name = item.name || item.deviceIdentifier;
  const target = targetRackLabel ? `"${targetRackLabel}"` : "this rack";
  return `Assigning ${name} to ${target} will unassign it from ${current}.`;
};

// --- Component ---

const MinerSelectionList = forwardRef<MinerSelectionListHandle, MinerSelectionListProps>(
  (
    {
      filterConfig,
      initialAllSelected = false,
      initialSelectedItems,
      isMembersLoading = false,
      isRowDisabled,
      singleSelect = false,
      disableFilteredSelectAll = false,
      showSelectAllFooter = true,
      scope,
      eligibility,
      targetRackLabel,
      onSelectionChange,
    },
    ref,
  ) => {
    const {
      showTypeFilter = true,
      showRackFilter: showRackFilterProp = true,
      showGroupFilter = true,
      showSubnetFilter = false,
      showSiteFilter: showSiteFilterProp = false,
      showBuildingFilter: showBuildingFilterProp = false,
    } = filterConfig ?? {};

    const canReadSiteCatalog = useHasPermission("site:read");

    const scopeSiteIds = useMemo(() => scope?.siteIds ?? [], [scope]);
    const scopeIncludeUnassigned = scope?.includeUnassigned ?? false;
    // Facet-option scope is decoupled from the miner-list scope. The list may
    // surface site-unassigned miners (scope.includeUnassigned), but the
    // Building/Rack *dropdown options* must stay strictly within the scoped
    // site — otherwise a scoped site would list unassigned buildings/racks,
    // offering facet choices outside the site (and, in assignable-only mode,
    // ones that immediately conflict → empty state). Only when no site is
    // scoped (e.g. the "unassigned" SitePicker mode) do the option fetches
    // honor includeUnassigned.
    const facetIncludeUnassigned = scopeSiteIds.length > 0 ? false : scopeIncludeUnassigned;
    // Serialized key so effects/callbacks only re-fire when the selection
    // actually changes (siteIds is a fresh bigint[] each render otherwise).
    const scopeKey = `${scopeSiteIds.map(String).join(",")}|${scopeIncludeUnassigned}`;

    // Eligibility ids destructured to primitives so the derived filter memo has
    // stable deps (the object prop is a fresh reference each render). Presence of
    // the prop — not whether any id is set — enables the toggle and the rack
    // exclusion, so a not-yet-placed new rack still filters out already-racked
    // miners.
    const eligibilityEnabled = eligibility !== undefined;
    const eligRackId = eligibility?.rackId;
    const eligSiteId = eligibility?.siteId;
    const eligBuildingId = eligibility?.buildingId;
    const [showAssignedInfo, setShowAssignedInfo] = useState(false);
    // The reassignment row whose conflict dialog is open, or null.
    const [conflictInfoItem, setConflictInfoItem] = useState<DeviceListItem | null>(null);
    // "Show assigned miners" — default off, so the list starts with only
    // assignable (unassigned + this-rack) miners. Turning it on also surfaces
    // miners currently assigned to another rack/building/site; they stay
    // selectable (reassigning moves them, behind a confirm) and render in orange
    // to flag the existing placement.
    const [showAssigned, setShowAssigned] = useState(false);

    // Site/Building facet options come from ListSites/ListBuildings (guarded by
    // site:read), so those two facets are hidden for rack-management roles
    // without that permission; the Site/Building *columns* still render since
    // their labels ride on the miner snapshot. The facets stay visible in both
    // toggle states — a facet that conflicts with the target rack's placement is
    // handled by the assignable-only empty state below, not by hiding.
    const showRackFilter = showRackFilterProp;
    const showSiteFilter = showSiteFilterProp && canReadSiteCatalog;
    const showBuildingFilter = showBuildingFilterProp && canReadSiteCatalog;

    const { listGroups, listRacks } = useDeviceSets();
    const { listSites } = useSites();
    const { listBuildings } = useBuildings();
    // The user's facet selections (model / subnet / site / building / rack /
    // group). Site scope and eligibility are layered on top in the derived
    // `filter` below so applying a facet never drops those constraints.
    const [userFilter, setUserFilter] = useState(() => create(MinerListFilterSchema, {}));
    const [selectedItems, setSelectedItems] = useState<string[]>(initialSelectedItems ?? []);
    const [allSelected, setAllSelected] = useState(initialAllSelected && !singleSelect);
    const [availableGroups, setAvailableGroups] = useState<DeviceSet[]>([]);
    const [availableRacks, setAvailableRacks] = useState<DeviceSet[]>([]);
    const [availableSites, setAvailableSites] = useState<{ id: string; label: string }[]>([]);
    const [availableBuildings, setAvailableBuildings] = useState<{ id: string; label: string }[]>([]);
    const [hasInitialSynced, setHasInitialSynced] = useState(!initialSelectedItems || initialSelectedItems.length > 0);
    const [currentSort, setCurrentSort] = useState<{ field: ModalColumn; direction: SortDirection } | undefined>(
      undefined,
    );

    // Build proto SortConfig from the current UI sort state
    const sortConfig = useMemo(() => {
      if (!currentSort) return undefined;
      const protoField = SORT_FIELD_BY_COLUMN[currentSort.field];
      if (!protoField) return undefined;
      return create(SortConfigSchema, {
        field: protoField,
        direction: currentSort.direction === "asc" ? SortDirectionProto.ASC : SortDirectionProto.DESC,
      });
    }, [currentSort]);

    // Effective server filter: the user's facets + the active site scope +,
    // when "assignable only" is on, the target rack's eligibility. Each
    // eligibility dimension is null-permissive (in-rack-or-none, in-building-or
    // -none, in-site-or-none), so the result excludes miners in a *different*
    // rack/building/site while keeping unplaced and current-rack miners.
    // useFleet dedupes by protobuf value equality, so a fresh object per render
    // only triggers a refetch when the contents actually change.
    const filter = useMemo(() => {
      const merged = clone(MinerListFilterSchema, userFilter);
      // Site scope is the soft baseline; a user-selected Site facet
      // (userFilter.siteIds) is more specific and takes precedence.
      if (merged.siteIds.length === 0) {
        merged.siteIds = scopeSiteIds;
        merged.includeUnassigned = scopeIncludeUnassigned;
      }
      if (!showAssigned && eligibilityEnabled) {
        // Each placement dimension is pinned to the target rack's value AND admits
        // miners unassigned at that level (the assignable set = this rack's
        // members + unplaced miners). When the operator has an explicit facet on a
        // dimension, that facet *defines* it: intersect with the eligible value
        // and drop the "include no ..." flag (they asked for specific
        // racks/buildings/sites, not "unassigned"). A facet that excludes the
        // target yields an empty intersection, surfaced as the
        // placementFacetConflict empty state — so it never broadens the request.
        if (userFilter.rackIds.length > 0) {
          merged.rackIds = eligRackId !== undefined ? userFilter.rackIds.filter((id) => id === eligRackId) : [];
          merged.includeNoRack = false;
        } else {
          merged.rackIds = eligRackId !== undefined ? [eligRackId] : [];
          merged.includeNoRack = true;
        }

        if (userFilter.buildingIds.length > 0) {
          merged.buildingIds =
            eligBuildingId !== undefined ? userFilter.buildingIds.filter((id) => id === eligBuildingId) : [];
          merged.includeNoBuilding = false;
        } else if (eligBuildingId !== undefined) {
          merged.buildingIds = [eligBuildingId];
          merged.includeNoBuilding = true;
        }

        if (userFilter.siteIds.length > 0) {
          merged.siteIds = eligSiteId !== undefined ? userFilter.siteIds.filter((id) => id === eligSiteId) : [];
          merged.includeUnassigned = false;
        } else if (eligSiteId !== undefined) {
          merged.siteIds = [eligSiteId];
          merged.includeUnassigned = true;
        }
      }
      return merged;
    }, [
      userFilter,
      scopeSiteIds,
      scopeIncludeUnassigned,
      showAssigned,
      eligibilityEnabled,
      eligRackId,
      eligBuildingId,
      eligSiteId,
    ]);

    const {
      minerIds,
      miners,
      totalMiners,
      isLoading,
      hasMore,
      currentPage,
      hasPreviousPage,
      goToNextPage,
      goToPrevPage,
      availableModels,
    } = useFleet({
      filter,
      sort: sortConfig,
      pageSize: PAGE_SIZE,
      pairingStatuses: [PairingStatus.PAIRED],
    });

    const currentPageItems = useMemo(() => {
      if (!miners) return [];
      return minerIds
        .map((id) => miners[id])
        .filter((snapshot): snapshot is ProtoMinerStateSnapshot => Boolean(snapshot))
        .map(toDeviceListItem);
    }, [minerIds, miners]);

    // Assignable-only + a conflicting placement facet = provably no results.
    //
    // In assignable-only mode (a target rack, "Show assigned miners" off), the
    // derived filter pins site/building/rack to the target rack's placement.
    // A user-selected Site/Building/Rack facet targets the same dimension, but
    // MinerListFilter carries one id-list per dimension with OR semantics, so it
    // *can't* express "building = A (eligibility) AND building = B (facet)" — the
    // request can only send one. Rather than silently override the facet (show
    // the rack's building instead of the picked one) or drop eligibility (leak
    // ineligible rows via the server's include_no_rack coupling — see #702), we
    // recognize the case client-side: if a placement facet doesn't include the
    // target's id — or the target is unplaced at that level (undefined), so any
    // facet there is unsatisfiable — nothing assignable can match, and we render
    // the empty state instead of the (misleading) pinned-dimension results the
    // server would still return. The facets stay visible and clearable, so this
    // is self-correcting once the operator changes or removes the facet.
    const placementFacetConflict = useMemo(() => {
      if (showAssigned || !eligibilityEnabled) return false;
      const conflicts = (facetIds: bigint[], targetId: bigint | undefined) =>
        facetIds.length > 0 && (targetId === undefined || !facetIds.includes(targetId));
      return (
        conflicts(userFilter.rackIds, eligRackId) ||
        conflicts(userFilter.buildingIds, eligBuildingId) ||
        conflicts(userFilter.siteIds, eligSiteId)
      );
    }, [showAssigned, eligibilityEnabled, userFilter, eligRackId, eligBuildingId, eligSiteId]);

    // Rows shown to the operator. A placement-facet conflict has no assignable
    // matches, so present it as empty even though the server (queried with the
    // pinned dimension) may return the target rack's miners.
    const displayItems = placementFacetConflict ? [] : currentPageItems;

    // Miners assigned to a different rack/building/site are still selectable
    // (reassigning moves them, behind a confirm), but are flagged in orange so
    // the operator sees the existing placement. Only meaningful when "Show
    // assigned miners" is on; otherwise they're filtered out server-side.
    const isReassignment = useCallback(
      (item: DeviceListItem) =>
        eligibilityEnabled &&
        isPlacementIneligible(item, { rackId: eligRackId, siteId: eligSiteId, buildingId: eligBuildingId }),
      [eligibilityEnabled, eligRackId, eligSiteId, eligBuildingId],
    );

    const currentSelectableItemIds = useMemo(
      () =>
        (isRowDisabled ? currentPageItems.filter((device) => !isRowDisabled(device)) : currentPageItems).map(
          (device) => device.deviceIdentifier,
        ),
      [currentPageItems, isRowDisabled],
    );

    // Wrap the base column renderers so a reassignment row's text is orange.
    // Wrapping (rather than editing each cell) keeps the placement flag in one
    // place and lets the inner cells inherit the color.
    // Flag reassignment rows with a right-aligned orange warning icon in the
    // Name cell (rows keep the normal text color); clicking it opens a dialog
    // explaining the placement collision. Only the Name column is overridden —
    // other columns render unchanged.
    const colConfig = useMemo<ColConfig<DeviceListItem, string, ModalColumn>>(() => {
      if (!eligibilityEnabled) return modalColConfig;
      return {
        ...modalColConfig,
        [modalCols.name]: {
          width: "min-w-28",
          component: (device: DeviceListItem) => (
            <div className="flex items-center justify-between gap-2">
              <span>{device.name || device.deviceIdentifier}</span>
              {isReassignment(device) ? (
                <Button
                  variant={variants.textOnly}
                  textOnlyUnderlineOnHover={false}
                  ariaLabel="Assignment conflict — view details"
                  prefixIcon={<Alert className="text-text-emphasis" />}
                  onClick={(e) => {
                    e.stopPropagation();
                    setConflictInfoItem(device);
                  }}
                />
              ) : null}
            </div>
          ),
        },
      };
    }, [eligibilityEnabled, isReassignment]);
    const displayedSelectedItems = allSelected && !singleSelect ? currentSelectableItemIds : selectedItems;
    // While "Show assigned miners" is on, the list mixes assignable and
    // assigned-elsewhere rows, so "select all" is ambiguous (and the assignable
    // resolver would silently drop the reassignment picks). Only offer it in the
    // assignable-only view.
    const canSelectAll =
      !singleSelect &&
      !(eligibilityEnabled && showAssigned) &&
      (!disableFilteredSelectAll || !hasUnsupportedAllSelectionFilter(filter));
    const shouldShowSelectionFooter =
      showSelectAllFooter &&
      totalMiners !== undefined &&
      totalMiners > 0 &&
      !singleSelect &&
      !placementFacetConflict &&
      (canSelectAll || allSelected || selectedItems.length > 0);

    const handleSort = useCallback((field: ModalColumn, direction: SortDirection) => {
      setCurrentSort({ field, direction });
    }, []);

    const scrollRef = useRef<HTMLDivElement>(null);
    const currentPageItemsRef = useRef(currentPageItems);
    // Accumulates every item seen across pages so reassignment detection covers
    // off-page selections. A miner can only be selected from a page it appeared
    // on, so this map holds placement for every selectable id — unlike the
    // parent's first-page-only snapshot cache.
    const seenItemsRef = useRef<Map<string, DeviceListItem>>(new Map());
    useEffect(() => {
      currentPageItemsRef.current = currentPageItems;
      for (const item of currentPageItems) seenItemsRef.current.set(item.deviceIdentifier, item);
    }, [currentPageItems]);

    const scrollToTop = useCallback(() => {
      scrollRef.current?.scrollTo({ top: 0, behavior: "smooth" });
    }, []);

    // Sync initialSelectedItems when they arrive asynchronously (edit mode).
    // Uses queueMicrotask to avoid synchronous setState inside effect body.
    useEffect(() => {
      if (hasInitialSynced) return;
      if (initialSelectedItems && initialSelectedItems.length > 0) {
        queueMicrotask(() => {
          setSelectedItems(initialSelectedItems);
          setHasInitialSynced(true);
        });
      }
    }, [initialSelectedItems, hasInitialSynced]);

    // Notify parent of selection changes. While a placement facet conflicts, the
    // list shows no results, so report an empty selection — this keeps callers
    // that gate on selection (e.g. Search's Assign button) consistent with the
    // empty view without discarding the underlying selection state.
    useEffect(() => {
      if (placementFacetConflict) {
        onSelectionChange?.({ selectedItems: [], allSelected: false, totalMiners });
        return;
      }
      onSelectionChange?.({ selectedItems, allSelected, totalMiners });
    }, [selectedItems, allSelected, totalMiners, onSelectionChange, placementFacetConflict]);

    useEffect(() => {
      if (!allSelected || canSelectAll) {
        return;
      }
      setAllSelected(false);
      setSelectedItems([]);
    }, [allSelected, canSelectAll]);

    // Expose selection state to parent via imperative handle
    useImperativeHandle(
      ref,
      () => ({
        getSelection: () => {
          const reassignedItems = selectedItems.filter((id) => {
            const item = seenItemsRef.current.get(id);
            return item !== undefined && isReassignment(item);
          });
          return {
            selectedItems,
            allSelected,
            totalMiners,
            filter,
            reassignedItems,
            blockedByFilter: placementFacetConflict,
          };
        },
      }),
      [selectedItems, allSelected, totalMiners, filter, isReassignment, placementFacetConflict],
    );

    const handleSetSelectedItems = useCallback(
      (newSelection: string[]) => {
        setAllSelected(false);
        if (singleSelect) {
          // In single-select mode, just keep the selected item (no off-page merging)
          setSelectedItems(newSelection.slice(0, 1));
        } else {
          setSelectedItems((prev) => {
            const currentPageKeys = new Set(currentPageItemsRef.current.map((d) => d.deviceIdentifier));
            const offPageSelections = prev.filter((id) => !currentPageKeys.has(id));
            return [...offPageSelections, ...newSelection.filter((id) => currentPageKeys.has(id))];
          });
        }
      },
      [singleSelect],
    );

    const handleNextPage = useCallback(() => {
      scrollToTop();
      goToNextPage();
    }, [scrollToTop, goToNextPage]);

    const handlePrevPage = useCallback(() => {
      scrollToTop();
      goToPrevPage();
    }, [scrollToTop, goToPrevPage]);

    // Fetch filter options only for enabled filters. Rack/building facet options
    // scope to the active site so the dropdowns list only the site's members
    // (facetIncludeUnassigned, not the list's includeUnassigned); group and site
    // options stay org-wide until ListGroups gains site filtering (issue #520).
    useEffect(() => {
      if (showGroupFilter) listGroups({ onSuccess: setAvailableGroups });
      if (showRackFilter)
        listRacks({ siteIds: scopeSiteIds, includeUnassigned: facetIncludeUnassigned, onSuccess: setAvailableRacks });
      if (showSiteFilter)
        listSites({
          onSuccess: (sites) =>
            setAvailableSites(
              sites.filter((s) => s.site !== undefined).map((s) => ({ id: String(s.site!.id), label: s.site!.name })),
            ),
        });
      if (showBuildingFilter)
        listBuildings({
          siteIds: scopeSiteIds,
          includeUnassigned: facetIncludeUnassigned,
          onSuccess: (buildings) =>
            setAvailableBuildings(
              buildings
                .filter((b) => b.building !== undefined)
                .map((b) => ({ id: String(b.building!.id), label: b.building!.name })),
            ),
        });
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [
      showGroupFilter,
      showRackFilter,
      showSiteFilter,
      showBuildingFilter,
      listGroups,
      listRacks,
      listSites,
      listBuildings,
      scopeKey,
    ]);

    // A single "Add filter" popover (matching the fleet miner list) whose
    // children mirror the displayed columns: Model, Subnet (IP), Site, Building,
    // Rack, Group. Only enabled facets are offered.
    // Order and grouping mirror the fleet miner list:
    // Model | Site, Building, Rack, Group | Subnet.
    const filters = useMemo((): FilterItem[] => {
      const children: NestedFilterChildItem[] = [];
      if (showTypeFilter) {
        children.push({
          type: "dropdown",
          title: "Model",
          // Keyed "model" to match the fleet miner list's nested filter (shared
          // testids / category key), not the legacy "type".
          value: "model",
          options: availableModels.map((model) => ({ id: model, label: model })),
          defaultOptionIds: [],
          showGroupDivider: true,
        });
      }
      if (showSiteFilter) {
        children.push({
          type: "dropdown",
          title: "Site",
          value: "site",
          options: availableSites,
          defaultOptionIds: [],
        });
      }
      if (showBuildingFilter) {
        children.push({
          type: "dropdown",
          title: "Building",
          value: "building",
          options: availableBuildings,
          defaultOptionIds: [],
        });
      }
      if (showRackFilter) {
        children.push({
          type: "dropdown",
          title: "Rack",
          value: "rack",
          options: availableRacks.map((rack) => ({ id: String(rack.id), label: rack.label })),
          defaultOptionIds: [],
        });
      }
      if (showGroupFilter) {
        children.push({
          type: "dropdown",
          title: "Group",
          value: "group",
          options: availableGroups.map((g) => ({ id: String(g.id), label: g.label })),
          defaultOptionIds: [],
          showGroupDivider: true,
        });
      }
      if (showSubnetFilter) {
        children.push({
          type: "textareaList",
          title: "Subnet",
          value: "subnet",
          validate: validateSubnetLine,
          normalize: normalizeSubnetLine,
          // Mirrors the onboarding discovery input, one example per accepted
          // form: explicit IP, IP range (short or full), and CIDR.
          placeholder: "10.0.0.42\n10.0.0.10-10.0.0.20\n192.168.1.0/24",
          noun: "subnet",
        });
      }
      if (children.length === 0) return [];
      return [
        {
          type: "nestedFilterDropdown",
          title: "Add filter",
          value: "filters-meta",
          prefixIcon: <Plus width="w-3" />,
          children,
        },
      ];
    }, [
      showTypeFilter,
      showSubnetFilter,
      showSiteFilter,
      showBuildingFilter,
      showRackFilter,
      showGroupFilter,
      availableModels,
      availableSites,
      availableBuildings,
      availableRacks,
      availableGroups,
    ]);

    // Build the user's facet filter (model / rack / group / subnet). Site scope
    // and eligibility are layered on in the derived `filter` memo, so they're
    // deliberately omitted here.
    const handleServerFilter = useCallback(
      async (activeFilters: ActiveFilters) => {
        const next = create(MinerListFilterSchema, { errorComponentTypes: [] });

        const typeFilters = activeFilters.dropdownFilters.model;
        if (typeFilters && typeFilters.length > 0) {
          next.models.push(...typeFilters);
        }

        if (showRackFilter) {
          const rackFilters = activeFilters.dropdownFilters.rack;
          if (rackFilters && rackFilters.length > 0) {
            next.rackIds.push(...rackFilters.map((id) => BigInt(id)));
          }
        }

        if (showGroupFilter) {
          const groupFilters = activeFilters.dropdownFilters.group;
          if (groupFilters && groupFilters.length > 0) {
            next.groupIds.push(...groupFilters.map((id) => BigInt(id)));
          }
        }

        if (showSiteFilter) {
          const siteFilters = activeFilters.dropdownFilters.site;
          if (siteFilters && siteFilters.length > 0) {
            next.siteIds.push(...siteFilters.map((id) => BigInt(id)));
          }
        }

        if (showBuildingFilter) {
          const buildingFilters = activeFilters.dropdownFilters.building;
          if (buildingFilters && buildingFilters.length > 0) {
            next.buildingIds.push(...buildingFilters.map((id) => BigInt(id)));
          }
        }

        if (showSubnetFilter) {
          const subnetFilters = activeFilters.textareaListFilters.subnet;
          if (subnetFilters && subnetFilters.length > 0) {
            // Ranges travel natively on ip_ranges; CIDRs/IPs on ip_cidrs. The
            // server ORs the two together and matches by containment/BETWEEN.
            subnetFilters.forEach((line) => {
              const entry = classifySubnetLine(line);
              if (!entry) return;
              if (entry.kind === "range") {
                next.ipRanges.push(create(IpRangeSchema, { startIp: entry.startIp, endIp: entry.endIp }));
              } else {
                next.ipCidrs.push(entry.cidr);
              }
            });
          }
        }

        setUserFilter(next);
      },
      [showRackFilter, showGroupFilter, showSiteFilter, showBuildingFilter, showSubnetFilter],
    );

    const showSpinner = (isLoading || isMembersLoading) && currentPageItems.length === 0;

    if (showSpinner) {
      return (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      );
    }

    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto pb-2">
          <List<DeviceListItem, string, ModalColumn>
            activeCols={activeCols}
            colTitles={modalColTitles}
            colConfig={colConfig}
            filters={filters}
            onServerFilter={handleServerFilter}
            headerControls={
              eligibilityEnabled ? (
                // px-1 gives the toggle's hover scale-up room so it doesn't
                // paint past the filter row's right edge and trigger horizontal
                // scroll in the modal.
                <div className="flex items-center gap-1 px-1">
                  <Button
                    variant={variants.textOnly}
                    textOnlyUnderlineOnHover={false}
                    ariaLabel="About “Show assigned miners”"
                    prefixIcon={<Info className="text-text-primary-70" />}
                    onClick={() => setShowAssignedInfo(true)}
                  />
                  <Switch
                    label="Show assigned miners"
                    ariaLabel="Show assigned miners"
                    checked={showAssigned}
                    setChecked={setShowAssigned}
                  />
                </div>
              ) : undefined
            }
            items={displayItems}
            itemKey="deviceIdentifier"
            itemSelectable
            selectionType={singleSelect ? "radio" : "checkbox"}
            sortableColumns={ALL_SORTABLE_COLUMNS}
            currentSort={currentSort}
            onSort={handleSort}
            customSelectedItems={displayedSelectedItems}
            customSetSelectedItems={handleSetSelectedItems}
            preserveOffPageSelection
            isRowDisabled={isRowDisabled}
            total={totalMiners}
            hideTotal
            itemName={{ singular: "miner", plural: "miners" }}
            containerClassName="min-h-0"
            overflowContainer
            stickyBgColor="bg-surface-elevated-base"
            emptyStateRow={
              <div className="py-10 text-center text-300 text-text-primary-70">No miners match these filters.</div>
            }
            footerContent={
              !placementFacetConflict && !isLoading && totalMiners !== undefined && totalMiners > 0 ? (
                <div className="flex flex-col items-center gap-4 py-6">
                  <span className="text-300 text-text-primary">
                    Showing {currentPage * PAGE_SIZE + 1}–{currentPage * PAGE_SIZE + currentPageItems.length} of{" "}
                    {totalMiners} miners
                  </span>
                  <div className="flex gap-3">
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      ariaLabel="Previous page"
                      prefixIcon={<ChevronDown className="rotate-90" />}
                      onClick={handlePrevPage}
                      disabled={!hasPreviousPage}
                    />
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      ariaLabel="Next page"
                      prefixIcon={<ChevronDown className="rotate-270" />}
                      onClick={handleNextPage}
                      disabled={!hasMore}
                    />
                  </div>
                </div>
              ) : null
            }
          />
        </div>
        {shouldShowSelectionFooter ? (
          <div className="shrink-0">
            <ModalSelectAllFooter
              label={
                allSelected && canSelectAll
                  ? `All ${totalMiners} miners selected`
                  : `${selectedItems.length} miners selected`
              }
              onSelectAll={
                canSelectAll
                  ? () => {
                      setAllSelected(true);
                      const selectableItems = isRowDisabled
                        ? currentPageItems.filter((d) => !isRowDisabled(d))
                        : currentPageItems;
                      setSelectedItems(selectableItems.map((d) => d.deviceIdentifier));
                    }
                  : undefined
              }
              onSelectNone={
                allSelected || selectedItems.length > 0
                  ? () => {
                      setAllSelected(false);
                      setSelectedItems([]);
                    }
                  : undefined
              }
            />
          </div>
        ) : null}
        {showAssignedInfo ? (
          <Dialog
            icon={<Info />}
            title="Show assigned miners"
            subtitle="Shows or hides miners that are already assigned to another rack, or to a building or site that this rack is not assigned to. Assigning these miners to this rack will unassign them from their current placement."
            onDismiss={() => setShowAssignedInfo(false)}
            buttons={[{ text: "Got it", variant: variants.primary, onClick: () => setShowAssignedInfo(false) }]}
          />
        ) : null}
        {conflictInfoItem ? (
          <Dialog
            icon={<Alert className="text-text-emphasis" />}
            title="Assignment conflict"
            subtitle={describeReassignment(conflictInfoItem, targetRackLabel)}
            onDismiss={() => setConflictInfoItem(null)}
            buttons={[{ text: "Got it", variant: variants.primary, onClick: () => setConflictInfoItem(null) }]}
          />
        ) : null}
      </div>
    );
  },
);

MinerSelectionList.displayName = "MinerSelectionList";

export default MinerSelectionList;
