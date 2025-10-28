import { useCallback, useMemo } from "react";

import { create } from "@bufbuild/protobuf";
import {
  componentIssues,
  deviceStatusFilterStates,
  minerCols,
  minerColTitles,
  minerTypes,
} from "./constants";
import minerColConfig from "./minerColConfig";
import {
  ComponentStatus,
  ComponentStatusFilterSchema,
  ComponentType,
  MinerListFilter,
  MinerListFilterSchema,
  MinerStateSnapshot,
  MinerType,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import {
  ActiveFilters,
  FilterItem,
} from "@/shared/components/List/Filters/types";
import { Breakpoint } from "@/shared/constants/breakpoints";

type MinerListProps = {
  title: string;
  minerIds: string[];
  listClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  onFilterChange: (filter: MinerListFilter) => void;
  onAddMiners: () => void;
  totalMiners?: number;
};

// TODO: move this to state when we
// implement row customization
const activeCols = [
  minerCols.name,
  minerCols.macAddress,
  minerCols.ipAddress,
  minerCols.status,
  minerCols.hashrate,
  minerCols.efficiency,
  minerCols.powerUsage,
  minerCols.temperature,
] as (keyof MinerStateSnapshot)[];

const MinerList = ({
  title,
  minerIds = [],
  listClassName,
  paddingLeft,
  overflowContainer,
  onFilterChange,
  onAddMiners,
  totalMiners,
}: MinerListProps) => {
  // Convert string array to objects for List component compatibility
  // List generally expects object with all items used in the ListItem to be passed to it as props
  // but because we just pass deviceIdentifier, and use that to look up the rest of the data in the store,
  // we can just create a simple object and cast it to MinerStateSnapshot type.
  const minerItems = useMemo(() => {
    return minerIds.map(
      (deviceIdentifier) =>
        ({
          deviceIdentifier,
        }) as MinerStateSnapshot,
    );
  }, [minerIds]);

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

      // Handle status dropdown filter
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

      // Handle type dropdown filter
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
      // Handle issues dropdown filter with component-specific filtering
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

      onFilterChange(minerFilter);
    },
    [onFilterChange],
  );
  return (
    <>
      <div className="sticky left-0 flex items-center justify-between text-heading-300 phone:px-6 tablet:px-6 laptop:px-10 desktop:px-10">
        <h2 className="text-heading-300">{title}</h2>
        <Button
          text="Add miners"
          variant={variants.secondary}
          size={sizes.compact}
          onClick={onAddMiners}
        />
      </div>

      <List<MinerStateSnapshot, string>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        filters={filters}
        onServerFilter={handleServerFilter}
        items={minerItems}
        itemKey={"deviceIdentifier"}
        itemSelectable
        renderActionBar={(selectedItems) => (
          <div className="flex w-full justify-center">
            <MinerListActionBar selectedMiners={selectedItems} />
          </div>
        )}
        containerClassName={listClassName}
        paddingLeft={paddingLeft}
        overflowContainer={overflowContainer}
        total={totalMiners}
        itemName={{ singular: "miner", plural: "miners" }}
      />
    </>
  );
};

export default MinerList;
