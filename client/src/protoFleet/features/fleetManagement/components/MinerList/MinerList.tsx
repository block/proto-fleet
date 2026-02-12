import { useCallback, useMemo } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import clsx from "clsx";
import { create } from "@bufbuild/protobuf";
import {
  componentIssues,
  deviceStatusFilterStates,
  minerCols,
  minerColTitles,
  type MinerColumn,
  minerTypes,
} from "./constants";
import minerColConfig from "./minerColConfig";
import { SORTABLE_COLUMNS } from "./sortConfig";
import { type DeviceListItem } from "./types";
import { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  MinerListFilterSchema,
  MinerType,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";
import {
  encodeFilterToURL,
  parseUrlToActiveFilters,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { useFleetStore } from "@/protoFleet/store";

import { LogoAlt } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import type { SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { Breakpoint } from "@/shared/constants/breakpoints";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type MinerListProps = {
  title: string;
  minerIds: string[];
  listClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  onAddMiners: () => void;
  totalMiners?: number;
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
   * Optional callback for infinite scroll. Called when the user scrolls
   * near the bottom of the list.
   */
  onLoadMore?: () => void;
  /**
   * Whether more items are available to load.
   */
  hasMore?: boolean;
  /**
   * Whether the list is currently loading more items.
   */
  isLoadingMore?: boolean;
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
};

// TODO: move this to state when we
// implement row customization
const activeCols: MinerColumn[] = [
  minerCols.name,
  minerCols.type,
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

const MinerList = ({
  title,
  minerIds = [],
  listClassName,
  paddingLeft,
  onAddMiners,
  totalMiners,
  totalDisabledMiners = 0,
  itemRef,
  loading = false,
  onLoadMore,
  hasMore = false,
  isLoadingMore = false,
  currentSort,
  onSort,
}: MinerListProps) => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");

  const showPhoneWidgets = isPhone && dismissedSetup;

  const deviceItems: DeviceListItem[] = useMemo(() => minerIds.map((id) => ({ deviceIdentifier: id })), [minerIds]);

  const miners = useFleetStore((state) => state.fleet.miners);

  const isRowDisabled = useCallback(
    (item: DeviceListItem) => {
      const miner = miners[item.deviceIdentifier];
      return miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
    },
    [miners],
  );

  const initialActiveFilters = useMemo(() => parseUrlToActiveFilters(searchParams), [searchParams]);
  const sortableColumnsSet = useMemo(() => new Set(SORTABLE_COLUMNS), []);

  const hasActiveFilters = useMemo(() => {
    return searchParams.has("status") || searchParams.has("issues") || searchParams.has("type");
  }, [searchParams]);

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
        title: "Type",
        value: "type",
        options: [
          { id: minerTypes.protoRig, label: "Proto Rig" },
          { id: minerTypes.bitmain, label: "Bitmain" },
        ],
        defaultOptionIds: [],
      },
    ] as FilterItem[];
  }, []);

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

      const typeFilters = filters.dropdownFilters.type;
      typeFilters?.forEach((filter) => {
        switch (filter) {
          case minerTypes.protoRig:
            minerFilter.types.push(MinerType.PROTO_RIG);
            break;
          case minerTypes.bitmain:
            minerFilter.types.push(MinerType.BITMAIN);
            break;
        }
      });
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

      // Navigate with URL params instead of calling parent callback
      const params = encodeFilterToURL(minerFilter);
      navigate(`?${params.toString()}`, { replace: true });
    },
    [navigate],
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
          <div className="flex h-full w-full items-center rounded-xl bg-landing-page p-6 sm:p-20 dark:bg-core-primary-5">
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

  return (
    <>
      <div className="sticky left-0 flex items-center justify-between text-heading-300 phone:px-6 tablet:px-6 laptop:px-10 desktop:px-10">
        <h2 className="text-heading-300">{title}</h2>
        <Button text="Add miners" variant={variants.secondary} size={sizes.compact} onClick={onAddMiners} />
      </div>

      {loading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <List<DeviceListItem, string, MinerColumn>
          activeCols={activeCols}
          colTitles={minerColTitles}
          colConfig={minerColConfig}
          filters={filters}
          onServerFilter={handleServerFilter}
          items={deviceItems}
          itemKey={"deviceIdentifier"}
          itemSelectable
          hasActiveFilters={hasActiveFilters}
          renderActionBar={(selectedItems, clearSelection, selectionMode, totalSelectable) => (
            <div className="flex w-full justify-center">
              <MinerListActionBar
                selectedMiners={selectedItems}
                onClearSelection={clearSelection}
                selectionMode={selectionMode}
                totalCount={totalSelectable}
              />
            </div>
          )}
          containerClassName={listClassName}
          paddingLeft={paddingLeft}
          overflowContainer={false}
          total={totalMiners}
          totalDisabled={totalDisabledMiners}
          itemName={{ singular: "miner", plural: "miners" }}
          itemRef={itemRef}
          initialActiveFilters={initialActiveFilters}
          onLoadMore={onLoadMore}
          hasMore={hasMore}
          isLoadingMore={isLoadingMore}
          isRowDisabled={isRowDisabled}
          columnsExemptFromDisabledStyling={new Set([minerCols.status, minerCols.issues])}
          sortableColumns={sortableColumnsSet}
          currentSort={currentSort}
          onSort={onSort}
        />
      )}
    </>
  );
};

export default MinerList;
