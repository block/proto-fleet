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
  ComponentStatus,
  MinerListFilter,
  MinerListFilterSchema,
  MinerStateSnapshot,
  MinerType,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

import {
  useMinerStateCounts,
  useTotalMiners,
} from "@/protoFleet/features/fleetManagement/store/useFleetStore";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
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
};

// TODO: move this to state when we
// implement row customization
const activeCols = [
  minerCols.name,
  minerCols.macAddress,
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
          // TODO is this mapping correct?
          switch (filter) {
            case minerFilterStates.hashing:
              minerFilter.status.push(ComponentStatus.OK);
              break;
            case minerFilterStates.broken:
              minerFilter.status.push(ComponentStatus.ERROR);
              break;
            case minerFilterStates.offline:
              minerFilter.status.push(ComponentStatus.OFFLINE);
              break;
            case minerFilterStates.asleep:
              minerFilter.status.push(ComponentStatus.UNSPECIFIED);
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
    <div>
      <div className="mb-10">
        <CompleteSetup />
      </div>
      <h2 className="text-heading-300">{title}</h2>
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
          <MinerListActionBar selectedMiners={selectedItems} />
        )}
        containerClassName={listClassName}
        paddingLeft={paddingLeft}
        overflowContainer={overflowContainer}
      />
    </div>
  );
};

export default MinerList;
