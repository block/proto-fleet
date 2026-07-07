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

import { ChevronDown, Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ActiveFilters, FilterItem, NestedFilterChildItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles, SortDirection } from "@/shared/components/List/types";
import { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";
import { expandSubnetLineToCidrs, normalizeSubnetLine, validateSubnetLine } from "@/shared/utils/filterValidation";

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
  // Target rack placement. When set, renders a "Show assignable only" toggle
  // (default on) that folds rack/building/site eligibility into the server
  // filter so miners in another rack/building/site drop out of the list
  // entirely. When the toggle is off, ineligible miners are shown but disabled
  // (id-based), so a cross-site pick can't happen silently.
  eligibility?: MinerEligibility;
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
      onSelectionChange,
    },
    ref,
  ) => {
    const {
      showTypeFilter = true,
      showRackFilter = true,
      showGroupFilter = true,
      showSubnetFilter = false,
      showSiteFilter = false,
      showBuildingFilter = false,
    } = filterConfig ?? {};

    const scopeSiteIds = useMemo(() => scope?.siteIds ?? [], [scope]);
    const scopeIncludeUnassigned = scope?.includeUnassigned ?? false;
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
    // "Show assignable only" — default on when a target rack is provided.
    const [assignableOnly, setAssignableOnly] = useState(true);

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
      if (assignableOnly && eligibilityEnabled) {
        // Rack: always admit unracked miners; also admit the target rack's own
        // members when it exists. Excludes miners in any other rack.
        merged.includeNoRack = true;
        if (eligRackId !== undefined && !merged.rackIds.includes(eligRackId)) {
          merged.rackIds.push(eligRackId);
        }
        if (eligBuildingId !== undefined) {
          merged.buildingIds = [eligBuildingId];
          merged.includeNoBuilding = true;
        }
        if (eligSiteId !== undefined) {
          merged.siteIds = [eligSiteId];
          merged.includeUnassigned = true;
        }
      }
      return merged;
    }, [
      userFilter,
      scopeSiteIds,
      scopeIncludeUnassigned,
      assignableOnly,
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

    // A caller-supplied predicate wins; otherwise, when a target rack is set,
    // disable ineligible rows. With "assignable only" on they're already
    // filtered out server-side; with it off they render disabled so a
    // cross-rack/site pick can't happen silently.
    const effectiveIsRowDisabled = useMemo(() => {
      if (isRowDisabled) return isRowDisabled;
      if (!eligibilityEnabled) return undefined;
      const target: MinerEligibility = { rackId: eligRackId, siteId: eligSiteId, buildingId: eligBuildingId };
      return (item: DeviceListItem) => isPlacementIneligible(item, target);
    }, [isRowDisabled, eligibilityEnabled, eligRackId, eligSiteId, eligBuildingId]);

    const currentSelectableItemIds = useMemo(
      () =>
        (effectiveIsRowDisabled
          ? currentPageItems.filter((device) => !effectiveIsRowDisabled(device))
          : currentPageItems
        ).map((device) => device.deviceIdentifier),
      [currentPageItems, effectiveIsRowDisabled],
    );
    const displayedSelectedItems = allSelected && !singleSelect ? currentSelectableItemIds : selectedItems;
    const canSelectAll = !singleSelect && (!disableFilteredSelectAll || !hasUnsupportedAllSelectionFilter(filter));
    const shouldShowSelectionFooter =
      showSelectAllFooter &&
      totalMiners !== undefined &&
      totalMiners > 0 &&
      !singleSelect &&
      (canSelectAll || allSelected || selectedItems.length > 0);

    const handleSort = useCallback((field: ModalColumn, direction: SortDirection) => {
      setCurrentSort({ field, direction });
    }, []);

    const scrollRef = useRef<HTMLDivElement>(null);
    const currentPageItemsRef = useRef(currentPageItems);
    useEffect(() => {
      currentPageItemsRef.current = currentPageItems;
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

    // Notify parent of selection changes
    useEffect(() => {
      onSelectionChange?.({ selectedItems, allSelected, totalMiners });
    }, [selectedItems, allSelected, totalMiners, onSelectionChange]);

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
        getSelection: () => ({ selectedItems, allSelected, totalMiners, filter }),
      }),
      [selectedItems, allSelected, totalMiners, filter],
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
    // scope to the active site so the dropdowns list only the site's members;
    // group and site options stay org-wide until ListGroups gains site filtering
    // (issue #520).
    useEffect(() => {
      if (showGroupFilter) listGroups({ onSuccess: setAvailableGroups });
      if (showRackFilter)
        listRacks({ siteIds: scopeSiteIds, includeUnassigned: scopeIncludeUnassigned, onSuccess: setAvailableRacks });
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
          includeUnassigned: scopeIncludeUnassigned,
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
    const filters = useMemo((): FilterItem[] => {
      const children: NestedFilterChildItem[] = [];
      if (showTypeFilter) {
        children.push({
          type: "dropdown",
          title: "Model",
          value: "type",
          options: availableModels.map((model) => ({ id: model, label: model })),
          defaultOptionIds: [],
        });
      }
      if (showSubnetFilter) {
        children.push({
          type: "textareaList",
          title: "Subnet",
          value: "subnet",
          validate: validateSubnetLine,
          normalize: normalizeSubnetLine,
          // Mirrors the onboarding discovery input: CIDR, bare IP, or IP range
          // (short or full form).
          placeholder: "192.168.1.0/24\n10.0.0.10-10.0.0.20\n10.0.0.42",
          noun: "subnet",
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

        const typeFilters = activeFilters.dropdownFilters.type;
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
            // Ranges expand to their covering CIDRs; CIDRs/IPs pass through. The
            // server ORs all ip_cidrs and matches by containment.
            next.ipCidrs.push(...subnetFilters.flatMap(expandSubnetLineToCidrs));
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
            colConfig={modalColConfig}
            filters={filters}
            onServerFilter={handleServerFilter}
            headerControls={
              eligibilityEnabled ? (
                // px-1 gives the toggle's hover scale-up room so it doesn't
                // paint past the filter row's right edge and trigger horizontal
                // scroll in the modal.
                <div className="px-1">
                  <Switch
                    label="Show assignable only"
                    ariaLabel="Show assignable only"
                    checked={assignableOnly}
                    setChecked={setAssignableOnly}
                  />
                </div>
              ) : undefined
            }
            items={currentPageItems}
            itemKey="deviceIdentifier"
            itemSelectable
            selectionType={singleSelect ? "radio" : "checkbox"}
            sortableColumns={ALL_SORTABLE_COLUMNS}
            currentSort={currentSort}
            onSort={handleSort}
            customSelectedItems={displayedSelectedItems}
            customSetSelectedItems={handleSetSelectedItems}
            preserveOffPageSelection
            isRowDisabled={effectiveIsRowDisabled}
            total={totalMiners}
            hideTotal
            itemName={{ singular: "miner", plural: "miners" }}
            containerClassName="min-h-0"
            overflowContainer
            stickyBgColor="bg-surface-elevated-base"
            footerContent={
              !isLoading && totalMiners !== undefined && totalMiners > 0 ? (
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
                      const selectableItems = effectiveIsRowDisabled
                        ? currentPageItems.filter((d) => !effectiveIsRowDisabled(d))
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
      </div>
    );
  },
);

MinerSelectionList.displayName = "MinerSelectionList";

export default MinerSelectionList;
