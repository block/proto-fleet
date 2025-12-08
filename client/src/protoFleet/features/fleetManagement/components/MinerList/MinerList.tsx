import { useCallback, useMemo } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

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
import { type DeviceListItem } from "./types";
import {
  ComponentStatus,
  ComponentStatusFilterSchema,
  ComponentType,
  MinerListFilterSchema,
  MinerType,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";
import {
  encodeFilterToURL,
  parseUrlToActiveFilters,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";

import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { Breakpoint } from "@/shared/constants/breakpoints";

type MinerListProps = {
  title: string;
  minerIds: string[];
  listClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  onAddMiners: () => void;
  totalMiners?: number;
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
};

// TODO: move this to state when we
// implement row customization
const activeCols: MinerColumn[] = [
  minerCols.name,
  minerCols.macAddress,
  minerCols.ipAddress,
  minerCols.status,
  minerCols.hashrate,
  minerCols.efficiency,
  minerCols.powerUsage,
  minerCols.temperature,
];

const MinerList = ({
  title,
  minerIds = [],
  listClassName,
  paddingLeft,
  overflowContainer,
  onAddMiners,
  totalMiners,
  itemRef,
  loading = false,
  onLoadMore,
  hasMore = false,
  isLoadingMore = false,
}: MinerListProps) => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const deviceItems: DeviceListItem[] = useMemo(() => minerIds.map((id) => ({ deviceIdentifier: id })), [minerIds]);

  const initialActiveFilters = useMemo(() => parseUrlToActiveFilters(searchParams), [searchParams]);

  // Determine if any filters are currently active from URL params
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
          { id: deviceStatusFilterStates.offline, label: "Offline" },
          { id: deviceStatusFilterStates.sleeping, label: "Sleeping" },
          {
            id: deviceStatusFilterStates.needsAttention,
            label: "Needs Attention",
          },
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
        componentFilters: [],
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
        const componentFilter = create(ComponentStatusFilterSchema, {
          statuses: [ComponentStatus.WARNING, ComponentStatus.ERROR],
        });

        switch (issue) {
          case componentIssues.controlBoard:
            componentFilter.component = ComponentType.CONTROL_BOARD;
            break;
          case componentIssues.fans:
            componentFilter.component = ComponentType.FANS;
            break;
          case componentIssues.hashBoards:
            componentFilter.component = ComponentType.HASH_BOARDS;
            break;
          case componentIssues.psu:
            componentFilter.component = ComponentType.PSU;
            break;
        }

        minerFilter.componentFilters.push(componentFilter);
      });

      // Navigate with URL params instead of calling parent callback
      const params = encodeFilterToURL(minerFilter);
      navigate(`?${params.toString()}`, { replace: true });
    },
    [navigate],
  );
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
          renderActionBar={(selectedItems, clearSelection, selectionMode) => (
            <div className="flex w-full justify-center">
              <MinerListActionBar
                selectedMiners={selectedItems}
                onClearSelection={clearSelection}
                selectionMode={selectionMode}
                totalCount={totalMiners}
              />
            </div>
          )}
          containerClassName={listClassName}
          paddingLeft={paddingLeft}
          overflowContainer={overflowContainer}
          total={totalMiners}
          itemName={{ singular: "miner", plural: "miners" }}
          itemRef={itemRef}
          initialActiveFilters={initialActiveFilters}
          onLoadMore={onLoadMore}
          hasMore={hasMore}
          isLoadingMore={isLoadingMore}
        />
      )}
    </>
  );
};

export default MinerList;
