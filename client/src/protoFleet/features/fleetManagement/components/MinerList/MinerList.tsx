import { useCallback, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import clsx from "clsx";
import { create } from "@bufbuild/protobuf";
import { useShallow } from "zustand/react/shallow";
import {
  componentIssues,
  deviceStatusFilterStates,
  minerCols,
  minerColTitles,
  type MinerColumn,
  MINERS_PAGE_SIZE,
} from "./constants";
import createMinerColConfig from "./minerColConfig";
import { getDefaultSortDirection, SORTABLE_COLUMNS } from "./sortConfig";
import { type DeviceListItem } from "./types";
import type { DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  type MinerListFilter,
  MinerListFilterSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { ProtoFleetStatusModal } from "@/protoFleet/components/StatusModal";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";

import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";
import {
  encodeFilterToURL,
  parseUrlToActiveFilters,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { useFleetStore } from "@/protoFleet/store";

import { ChevronDown, LogoAlt } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { type SelectionMode } from "@/shared/components/List";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import { type SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { Breakpoint } from "@/shared/constants/breakpoints";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type FleetCredentials = { username: string; password: string };

type MinerModalFlow =
  | { kind: "closed" }
  | { kind: "authenticate-miners"; deviceIdentifier: string }
  | { kind: "authenticate-fleet"; deviceIdentifier: string; deviceStatus?: DeviceStatus }
  | {
      kind: "pool-selection";
      deviceIdentifier: string;
      deviceStatus?: DeviceStatus;
      credentials: FleetCredentials;
    }
  | { kind: "status-modal"; deviceIdentifier: string };

type MinerListProps = {
  title: string;
  minerIds: string[];
  listClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  onAddMiners: () => void;
  totalMiners?: number;
  /**
   * Total unfiltered miner count for the "X of Y miners" subtitle display.
   */
  totalUnfilteredMiners?: number;
  /**
   * Total number of disabled miners (requiring authentication).
   * Used to calculate selectable count: totalMiners - totalDisabledMiners
   */
  totalDisabledMiners?: number;
  /**
   * Optional callback to attach refs to list row elements.
   * Used for viewport visibility tracking.
   */
  itemRef?: (itemKey: string, element: HTMLTableRowElement | null) => void;
  /**
   * Whether the list is loading. Shows a spinner in place of list items.
   */
  loading?: boolean;
  /**
   * Number of items per page. Used to compute the displayed item range (e.g., "Showing 1–100").
   * Must match the pageSize passed to useFleet.
   */
  pageSize?: number;
  /**
   * Current page index (0-based) for pagination display.
   */
  currentPage?: number;
  /**
   * Whether there is a previous page to navigate to.
   */
  hasPreviousPage?: boolean;
  /**
   * Whether there is a next page to navigate to.
   */
  hasNextPage?: boolean;
  /**
   * Callback to navigate to the next page.
   */
  onNextPage?: () => void;
  /**
   * Callback to navigate to the previous page.
   */
  onPrevPage?: () => void;
  /**
   * Current sort configuration from URL/store.
   * Passed down from parent to enable controlled sorting.
   */
  currentSort?: { field: MinerColumn; direction: SortDirection };
  /**
   * Callback when user clicks a sortable column header.
   * Parent handles URL update and API request.
   */
  onSort?: (field: MinerColumn, direction: SortDirection) => void;
  /**
   * Available model names for the model filter dropdown.
   * Comes from the API response.
   */
  availableModels?: string[];
  /**
   * Available groups for the group filter dropdown.
   */
  availableGroups?: DeviceCollection[];
  /**
   * Exports the full paired miner list as CSV.
   */
  onExportCsv?: () => void | Promise<void>;
  /**
   * Whether a CSV export is currently in progress.
   */
  exportCsvLoading?: boolean;
  /** Active server-side filter — forwarded for "all" mode delete */
  currentFilter?: MinerListFilter;
  /** Current server-side sort — forwarded for bulk actions that depend on table order. */
  currentSortConfig?: SortConfig;
};

// TODO: move this to state when we
// implement row customization
const activeCols: MinerColumn[] = [
  minerCols.name,
  minerCols.groups,
  minerCols.model,
  minerCols.macAddress,
  minerCols.ipAddress,
  minerCols.status,
  minerCols.issues,
  minerCols.hashrate,
  minerCols.efficiency,
  minerCols.powerUsage,
  minerCols.temperature,
  minerCols.firmware,
];

type ScopedMinerListBodyProps = {
  deviceItems: DeviceListItem[];
  minerColConfig: ReturnType<typeof createMinerColConfig>;
  filters: FilterItem[];
  handleServerFilter: (filters: ActiveFilters) => Promise<void>;
  initialActiveFilters: ActiveFilters;
  listClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer: boolean;
  totalMiners?: number;
  totalDisabledMiners: number;
  itemRef?: (itemKey: string, element: HTMLTableRowElement | null) => void;
  hasActiveFilters: boolean;
  onAddMiners: () => void;
  onExportCsv?: () => void | Promise<void>;
  exportCsvLoading?: boolean;
  handleClearFilters: () => void;
  isRowDisabled: (item: DeviceListItem) => boolean;
  currentFilter?: MinerListFilter;
  currentSortConfig?: SortConfig;
  currentSort?: { field: MinerColumn; direction: SortDirection };
  onSort?: (field: MinerColumn, direction: SortDirection) => void;
  firstItemIndex: number;
  lastItemIndex: number;
  shouldRenderPagination: boolean;
  hasPreviousPage: boolean;
  hasNextPage: boolean;
  handlePrevPage: () => void;
  handleNextPage: () => void;
};

const ScopedMinerListBody = ({
  deviceItems,
  minerColConfig,
  filters,
  handleServerFilter,
  initialActiveFilters,
  listClassName,
  paddingLeft,
  overflowContainer,
  totalMiners,
  totalDisabledMiners,
  itemRef,
  hasActiveFilters,
  onAddMiners,
  onExportCsv,
  exportCsvLoading = false,
  handleClearFilters,
  isRowDisabled,
  currentFilter,
  currentSortConfig,
  currentSort,
  onSort,
  firstItemIndex,
  lastItemIndex,
  shouldRenderPagination,
  hasPreviousPage,
  hasNextPage,
  handlePrevPage,
  handleNextPage,
}: ScopedMinerListBodyProps) => {
  const [selectedMinerIds, setSelectedMinerIds] = useState<string[]>([]);
  const [selectionMode, setSelectionMode] = useState<SelectionMode>("none");
  const sortableColumnsSet = useMemo(() => new Set(SORTABLE_COLUMNS), []);

  const currentPageSelectableMinerIds = deviceItems
    .filter((item) => !isRowDisabled(item))
    .map((item) => item.deviceIdentifier);

  const handleSelectAllMiners = useCallback(() => {
    setSelectedMinerIds(currentPageSelectableMinerIds);
    setSelectionMode("all");
  }, [currentPageSelectableMinerIds]);

  const handleSelectNoneMiners = useCallback(() => {
    setSelectedMinerIds([]);
    setSelectionMode("none");
  }, []);

  return (
    <>
      <List<DeviceListItem, string, MinerColumn>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        filters={filters}
        onServerFilter={handleServerFilter}
        items={deviceItems}
        itemKey={"deviceIdentifier"}
        customSelectedItems={selectedMinerIds}
        customSetSelectedItems={setSelectedMinerIds}
        customSelectionMode={selectionMode}
        itemSelectable
        pageScopedSelection
        hasActiveFilters={hasActiveFilters}
        headerControls={
          <div className="flex items-center gap-2">
            <Button
              text="Export CSV"
              variant={variants.secondary}
              size={sizes.compact}
              onClick={onExportCsv}
              loading={exportCsvLoading}
              disabled={totalMiners === 0}
            />
            <Button text="Add miners" variant={variants.secondary} size={sizes.compact} onClick={onAddMiners} />
          </div>
        }
        renderActionBar={(selectedItems, clearSelection, currentSelectionMode, totalSelectable) => (
          <div className="flex w-full justify-center">
            <MinerListActionBar
              selectedMiners={selectedItems}
              onClearSelection={clearSelection}
              onSelectAll={hasActiveFilters ? undefined : handleSelectAllMiners}
              onSelectNone={hasActiveFilters ? undefined : handleSelectNoneMiners}
              selectionMode={currentSelectionMode}
              totalCount={totalSelectable}
              currentFilter={currentFilter}
              currentSort={currentSortConfig}
            />
          </div>
        )}
        containerClassName={listClassName}
        tableClassName="mb-4"
        paddingLeft={paddingLeft}
        overflowContainer={overflowContainer}
        total={totalMiners}
        totalDisabled={totalDisabledMiners}
        hideTotal
        itemName={{ singular: "miner", plural: "miners" }}
        itemRef={itemRef}
        initialActiveFilters={initialActiveFilters}
        onSelectionModeChange={setSelectionMode}
        isRowDisabled={isRowDisabled}
        columnsExemptFromDisabledStyling={new Set([minerCols.status, minerCols.issues])}
        sortableColumns={sortableColumnsSet}
        currentSort={currentSort}
        onSort={onSort}
        getDefaultSortDirection={getDefaultSortDirection}
        emptyStateRow={
          hasActiveFilters && totalMiners === 0 ? (
            <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
              <div className="text-heading-200 text-text-primary">No results</div>
              <p className="mt-1 text-400 text-text-primary-70">Try adjusting or clearing your filters.</p>
              <Button
                className="mt-6"
                variant={variants.secondary}
                size={sizes.base}
                testId="clear-all-filters-button"
                onClick={handleClearFilters}
              >
                Clear all filters
              </Button>
            </div>
          ) : undefined
        }
      />

      {shouldRenderPagination && (
        <div
          className={clsx("flex flex-col items-center gap-4 pt-6", {
            "pb-24": selectionMode !== "none",
            "pb-6": selectionMode === "none",
          })}
          data-testid="miners-pagination"
        >
          <span className="text-300 text-text-primary">
            Showing {firstItemIndex}–{lastItemIndex} of {totalMiners} miners
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
              disabled={!hasNextPage}
            />
          </div>
        </div>
      )}
    </>
  );
};

const MinerList = ({
  title,
  minerIds = [],
  listClassName,
  paddingLeft,
  overflowContainer = true,
  onAddMiners,
  totalMiners,
  totalUnfilteredMiners,
  totalDisabledMiners = 0,
  itemRef,
  loading = false,
  pageSize = MINERS_PAGE_SIZE,
  currentPage = 0,
  hasPreviousPage = false,
  hasNextPage = false,
  onNextPage,
  onPrevPage,
  currentSort,
  onSort,
  availableModels = [],
  availableGroups = [],
  onExportCsv,
  exportCsvLoading = false,
  currentFilter,
  currentSortConfig,
}: MinerListProps) => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");

  const showPhoneWidgets = isPhone && dismissedSetup;
  const [modalFlow, setModalFlow] = useState<MinerModalFlow>({ kind: "closed" });

  const topRef = useRef<HTMLDivElement>(null);

  const scrollToTop = useCallback(() => {
    topRef.current?.scrollIntoView?.({ behavior: "smooth", block: "start" });
  }, []);

  const handleNextPage = useCallback(() => {
    scrollToTop();
    onNextPage?.();
  }, [scrollToTop, onNextPage]);

  const handlePrevPage = useCallback(() => {
    scrollToTop();
    onPrevPage?.();
  }, [scrollToTop, onPrevPage]);

  const deviceItems: DeviceListItem[] = useMemo(() => minerIds.map((id) => ({ deviceIdentifier: id })), [minerIds]);

  const disabledMinerIds = useFleetStore(
    useShallow((state) =>
      minerIds.filter((deviceIdentifier) => {
        const miner = state.fleet.miners[deviceIdentifier];
        return miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
      }),
    ),
  );
  const disabledMinerIdSet = useMemo(() => new Set(disabledMinerIds), [disabledMinerIds]);
  const isRowDisabled = useCallback(
    (item: DeviceListItem) => disabledMinerIdSet.has(item.deviceIdentifier),
    [disabledMinerIdSet],
  );

  const initialActiveFilters = useMemo(() => parseUrlToActiveFilters(searchParams), [searchParams]);

  const closeModalFlow = useCallback(() => {
    setModalFlow({ kind: "closed" });
  }, []);

  const handleOpenStatusFlow = useCallback((deviceIdentifier: string) => {
    const miner = useFleetStore.getState().fleet.miners[deviceIdentifier];
    if (!miner) return;

    const needsAuthentication = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
    const needsMiningPool = miner.deviceStatus === DeviceStatus.NEEDS_MINING_POOL;

    if (needsAuthentication) {
      setModalFlow({ kind: "authenticate-miners", deviceIdentifier });
      return;
    }

    if (needsMiningPool) {
      setModalFlow({
        kind: "authenticate-fleet",
        deviceIdentifier,
        deviceStatus: miner.deviceStatus,
      });
      return;
    }

    setModalFlow({ kind: "status-modal", deviceIdentifier });
  }, []);

  const handleFleetAuthenticated = useCallback((username: string, password: string) => {
    setModalFlow((current) => {
      if (current.kind !== "authenticate-fleet") {
        return current;
      }

      return {
        kind: "pool-selection",
        deviceIdentifier: current.deviceIdentifier,
        deviceStatus: current.deviceStatus,
        credentials: { username, password },
      };
    });
  }, []);

  const minerColConfig = useMemo(
    () => createMinerColConfig({ onOpenStatusFlow: handleOpenStatusFlow, availableGroups }),
    [handleOpenStatusFlow, availableGroups],
  );

  const hasActiveFilters = useMemo(() => {
    return (
      searchParams.has("status") || searchParams.has("issues") || searchParams.has("model") || searchParams.has("group")
    );
  }, [searchParams]);
  const selectionFilterKey = useMemo(() => {
    const params = new URLSearchParams();
    ["status", "issues", "model"].forEach((key) => {
      searchParams
        .getAll(key)
        .sort()
        .forEach((value) => params.append(key, value));
    });
    return params.toString();
  }, [searchParams]);
  const selectionScopeKey = useMemo(() => `${selectionFilterKey}:${currentPage}`, [currentPage, selectionFilterKey]);

  const handleClearFilters = useCallback(() => {
    const nextSearchParams = new URLSearchParams(searchParams);
    nextSearchParams.delete("status");
    nextSearchParams.delete("issues");
    nextSearchParams.delete("model");
    nextSearchParams.delete("group");

    const nextSearch = nextSearchParams.toString();
    navigate({ search: nextSearch ? `?${nextSearch}` : "" }, { replace: true });
  }, [navigate, searchParams]);

  const filters = useMemo(() => {
    return [
      {
        type: "dropdown",
        title: "Status",
        value: "status",
        options: [
          { id: deviceStatusFilterStates.hashing, label: "Hashing" },
          {
            id: deviceStatusFilterStates.needsAttention,
            label: "Needs Attention",
          },
          { id: deviceStatusFilterStates.offline, label: "Offline" },
          { id: deviceStatusFilterStates.sleeping, label: "Sleeping" },
        ],
        defaultOptionIds: [],
      },
      {
        type: "dropdown",
        title: "Issues",
        value: "issues",
        options: [
          { id: componentIssues.controlBoard, label: "Control board issue" },
          { id: componentIssues.fans, label: "Fan issue" },
          { id: componentIssues.hashBoards, label: "Hash board issue" },
          { id: componentIssues.psu, label: "PSU issue" },
        ],
        defaultOptionIds: [],
      },
      {
        type: "dropdown",
        title: "Model",
        value: "model",
        options: availableModels.map((model) => ({ id: model, label: model })),
        defaultOptionIds: [],
      },
      {
        type: "dropdown",
        title: "Groups",
        value: "group",
        options: availableGroups.map((g) => ({ id: String(g.id), label: g.label })),
        defaultOptionIds: [],
      },
    ] as FilterItem[];
  }, [availableModels, availableGroups]);

  const handleServerFilter = useCallback(
    async (filters: ActiveFilters) => {
      const minerFilter = create(MinerListFilterSchema, {
        errorComponentTypes: [],
      });

      const statusFilters = filters.dropdownFilters.status;
      if (statusFilters !== undefined && statusFilters.length > 0) {
        // Only apply status filtering if specific statuses are selected
        statusFilters.forEach((filter) => {
          switch (filter) {
            case deviceStatusFilterStates.hashing:
              minerFilter.deviceStatus.push(DeviceStatus.ONLINE);
              break;
            case deviceStatusFilterStates.needsAttention:
              minerFilter.deviceStatus.push(DeviceStatus.ERROR);
              minerFilter.deviceStatus.push(DeviceStatus.NEEDS_MINING_POOL);
              break;
            case deviceStatusFilterStates.offline:
              minerFilter.deviceStatus.push(DeviceStatus.OFFLINE);
              break;
            case deviceStatusFilterStates.sleeping:
              minerFilter.deviceStatus.push(DeviceStatus.INACTIVE);
              break;
          }
        });
      }
      // If statusFilters is undefined or empty, don't add any status filter (show all)

      const modelFilters = filters.dropdownFilters.model;
      if (modelFilters && modelFilters.length > 0) {
        minerFilter.models.push(...modelFilters);
      }
      const issueFilters = filters.dropdownFilters.issues;
      issueFilters?.forEach((issue) => {
        switch (issue) {
          case componentIssues.controlBoard:
            minerFilter.errorComponentTypes.push(ComponentType.CONTROL_BOARD);
            break;
          case componentIssues.fans:
            minerFilter.errorComponentTypes.push(ComponentType.FAN);
            break;
          case componentIssues.hashBoards:
            minerFilter.errorComponentTypes.push(ComponentType.HASH_BOARD);
            break;
          case componentIssues.psu:
            minerFilter.errorComponentTypes.push(ComponentType.PSU);
            break;
        }
      });

      const groupFilters = filters.dropdownFilters.group;
      if (groupFilters && groupFilters.length > 0) {
        groupFilters.forEach((id) => {
          minerFilter.groupIds.push(BigInt(id));
        });
      }

      // Navigate with URL params instead of calling parent callback
      // Start fresh with filter params, then preserve existing sort params
      const params = encodeFilterToURL(minerFilter);
      const sortParam = searchParams.get("sort");
      const dirParam = searchParams.get("dir");
      if (sortParam) params.set("sort", sortParam);
      if (dirParam) params.set("dir", dirParam);
      navigate(`?${params.toString()}`, { replace: true });
    },
    [navigate, searchParams],
  );
  // Show null state when no miners are paired and not loading
  const showNullState = !loading && totalMiners === 0 && !hasActiveFilters;

  if (showNullState) {
    return (
      <div
        className={clsx(
          "fixed top-[calc(theme(spacing.1)*15)] right-0 bottom-0 left-16 z-20 overflow-auto bg-surface-base",
          "phone:left-0 tablet:top-[calc(theme(spacing.1)*12)] tablet:left-0",
          showPhoneWidgets ? "phone:top-[calc(theme(spacing.1)*12+57px)]" : "phone:top-[calc(theme(spacing.1)*12)]",
        )}
      >
        <div className="h-[calc(100vh-theme(spacing.1)*15)] p-6 sm:p-10">
          <div className="flex h-full w-full items-center rounded-xl bg-landing-page p-6 sm:p-20">
            <div className="flex flex-col gap-12">
              <div className="flex flex-col gap-4">
                <LogoAlt width="w-[48px]" />
                <Header
                  title="You haven't paired any miners"
                  titleSize="text-display-200"
                  description="Add miners to your fleet to get started."
                />
              </div>
              <div>
                <Button variant="primary" onClick={onAddMiners}>
                  Get started
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  const firstItemIndex = currentPage * pageSize + 1;
  const lastItemIndex = currentPage * pageSize + minerIds.length;
  const shouldRenderPagination = !loading && totalMiners !== undefined && totalMiners > 0;

  return (
    <>
      <div
        ref={topRef}
        className="sticky left-0 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6 laptop:px-10 desktop:px-10"
      >
        <h2 className="text-heading-300">{title}</h2>
      </div>

      <div className="sticky left-0 text-300 text-text-primary-70 phone:px-6 tablet:px-6 laptop:px-10 desktop:px-10">
        {hasActiveFilters && totalUnfilteredMiners !== undefined && totalMiners !== totalUnfilteredMiners
          ? `${totalMiners} of ${totalUnfilteredMiners} miners`
          : `${totalMiners ?? 0} miners`}
      </div>

      {loading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <ScopedMinerListBody
          key={selectionScopeKey}
          deviceItems={deviceItems}
          minerColConfig={minerColConfig}
          filters={filters}
          handleServerFilter={handleServerFilter}
          initialActiveFilters={initialActiveFilters}
          listClassName={listClassName}
          paddingLeft={paddingLeft}
          overflowContainer={overflowContainer}
          totalMiners={totalMiners}
          totalDisabledMiners={totalDisabledMiners}
          itemRef={itemRef}
          hasActiveFilters={hasActiveFilters}
          onAddMiners={onAddMiners}
          onExportCsv={onExportCsv}
          exportCsvLoading={exportCsvLoading}
          handleClearFilters={handleClearFilters}
          isRowDisabled={isRowDisabled}
          currentFilter={currentFilter}
          currentSortConfig={currentSortConfig}
          currentSort={currentSort}
          onSort={onSort}
          firstItemIndex={firstItemIndex}
          lastItemIndex={lastItemIndex}
          shouldRenderPagination={shouldRenderPagination}
          hasPreviousPage={hasPreviousPage}
          hasNextPage={hasNextPage}
          handlePrevPage={handlePrevPage}
          handleNextPage={handleNextPage}
        />
      )}

      {modalFlow.kind === "authenticate-miners" && <AuthenticateMiners open onClose={closeModalFlow} />}

      {modalFlow.kind === "authenticate-fleet" && (
        <AuthenticateFleetModal
          open
          purpose="pool"
          onAuthenticated={handleFleetAuthenticated}
          onDismiss={closeModalFlow}
        />
      )}

      {modalFlow.kind === "pool-selection" && (
        <PoolSelectionPageWrapper
          open
          selectedMiners={[
            {
              deviceIdentifier: modalFlow.deviceIdentifier,
              deviceStatus: modalFlow.deviceStatus,
            },
          ]}
          selectionMode="subset"
          userUsername={modalFlow.credentials.username}
          userPassword={modalFlow.credentials.password}
          onSuccess={closeModalFlow}
          onError={closeModalFlow}
          onDismiss={closeModalFlow}
        />
      )}

      {modalFlow.kind === "status-modal" && (
        <ProtoFleetStatusModal open onClose={closeModalFlow} deviceId={modalFlow.deviceIdentifier} />
      )}
    </>
  );
};

export default MinerList;
