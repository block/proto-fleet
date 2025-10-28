import { useCallback, useMemo } from "react";

import { create } from "@bufbuild/protobuf";
import {
  minerCols,
  minerColTitles,
  minerFilterStates,
  minerTypes,
} from "./constants";
import minerColConfig from "./minerColConfig";
import {
  MinerListFilter,
  MinerListFilterSchema,
  MinerStateSnapshot,
  MinerType,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

import { useMinerStateCounts, useTotalMiners } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { defaultListFilter } from "@/shared/components/List/constants";
import {
  ActiveFilters,
  FilterItem,
} from "@/shared/components/List/Filters/types";
import { statuses } from "@/shared/components/StatusCircle/constants";
import { Breakpoint } from "@/shared/constants/breakpoints";

type MinerListProps = {
  title: string;
  minerIds: string[];
  listClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  onFilterChange: (filter: MinerListFilter) => void;
  onAddMiners: () => void;
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
}: MinerListProps) => {
  const totalMiners = useTotalMiners();
  const minerStateCounts = useMinerStateCounts();

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
        type: "button",
        title: "All miners",
        value: defaultListFilter,
        count: totalMiners,
      },
      {
        type: "button",
        title: "Hashing",
        value: minerFilterStates.hashing,
        count: minerStateCounts.hashingCount,
        status: statuses.normal,
      },
      {
        type: "button",
        title: "Broken",
        value: minerFilterStates.broken,
        count: minerStateCounts.brokenCount,
        status: statuses.error,
      },
      {
        type: "button",
        title: "Offline",
        value: minerFilterStates.offline,
        count: minerStateCounts.offlineCount,
        status: statuses.warning,
      },
      {
        type: "button",
        title: "Asleep",
        value: minerFilterStates.asleep,
        count: minerStateCounts.sleepingCount,
        status: statuses.inactive,
      },
      {
        type: "dropdown",
        title: "Type",
        value: "type",
        options: [
          { id: "all", label: "All Types" },
          { id: minerTypes.protoRig, label: "Proto Rig" },
          { id: minerTypes.bitmain, label: "Bitmain" },
        ],
        defaultOptionId: "all",
      },
    ] as FilterItem[];
  }, [totalMiners, minerStateCounts]);

  const handleServerFilter = useCallback(
    async (filters: ActiveFilters) => {
      const minerFilter = create(MinerListFilterSchema, { status: [] });

      if (!filters.buttonFilters.includes(defaultListFilter)) {
        filters.buttonFilters.forEach((filter) => {
          switch (filter) {
            case minerFilterStates.hashing:
              minerFilter.deviceStatus.push(DeviceStatus.ONLINE);
              break;
            case minerFilterStates.broken:
              minerFilter.deviceStatus.push(DeviceStatus.ERROR);
              break;
            case minerFilterStates.offline:
              minerFilter.deviceStatus.push(DeviceStatus.OFFLINE);
              break;
            case minerFilterStates.asleep:
              minerFilter.deviceStatus.push(DeviceStatus.INACTIVE);
              break;
          }
        });
      }

      // TODO: Add support for multiple types in dropdown
      if (filters.dropdownFilters.type) {
        if (filters.dropdownFilters.type.includes(minerTypes.protoRig)) {
          minerFilter.type = MinerType.PROTO_RIG;
        }
        if (filters.dropdownFilters.type.includes(minerTypes.bitmain)) {
          minerFilter.type = MinerType.BITMAIN;
        }
      }
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
      />
    </>
  );
};

export default MinerList;
