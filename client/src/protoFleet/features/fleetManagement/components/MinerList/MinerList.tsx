import { useMemo } from "react";

import {
  minerCols,
  minerColTitles,
  type MinerFilterState,
  minerFilterStates,
} from "./constants";
import minerColConfig from "./minerColConfig";

import { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

import List from "@/shared/components/List";
import { defaultListFilter } from "@/shared/components/List/constants";
import { FilterItem } from "@/shared/components/List/Filters/types";
import { statuses } from "@/shared/components/StatusCircle/constants";

type MinerListProps = {
  title: string;
  minerIds: string[];
  bodyClassName?: string;
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

const MinerList = ({ title, minerIds = [], bodyClassName }: MinerListProps) => {
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
    const countMiners = (status: MinerFilterState) => {
      // TODO: need to determine what properties need to be added to MinerStateSnapshot to support our filters
      void status;
      return minerItems.filter(
        () => true,
        // Add filtering logic based on deviceIdentifier when status filtering is implemented
      ).length;
    };

    return [
      {
        type: "button",
        title: "All miners",
        value: defaultListFilter,
        count: minerItems.length,
      },
      {
        type: "button",
        title: "Hashing",
        value: minerFilterStates.hashing,
        count: countMiners(minerFilterStates.hashing),
        status: statuses.normal,
      },
      {
        type: "button",
        title: "Broken",
        value: minerFilterStates.broken,
        count: countMiners(minerFilterStates.broken),
        status: statuses.error,
      },
      {
        type: "button",
        title: "Offline",
        value: minerFilterStates.offline,
        count: countMiners(minerFilterStates.offline),
        status: statuses.warning,
      },
      {
        type: "button",
        title: "Asleep",
        value: minerFilterStates.asleep,
        count: countMiners(minerFilterStates.asleep),
        status: statuses.inactive,
      },
    ] as FilterItem<MinerFilterState>[];
  }, [minerItems]);

  const filterMiner = (
    item: { deviceIdentifier: string },
    activeButtonFilters: (MinerFilterState | typeof defaultListFilter)[],
  ) => {
    if (activeButtonFilters.includes(defaultListFilter)) {
      return true;
    }

    // TODO: Implement filtering logic based on deviceIdentifier
    // We can get miner data from store using useMiner(item.deviceIdentifier) when needed
    void item;
    void activeButtonFilters;
    return true;
  };

  return (
    <div>
      <h2 className="text-heading-300">{title}</h2>
      <List<MinerStateSnapshot, string, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        filters={filters}
        filterItem={filterMiner}
        items={minerItems}
        itemKey={"deviceIdentifier"}
        itemSelectable
        renderActionBar={(selectedItems) => (
          <MinerListActionBar selectedMiners={selectedItems} />
        )}
        bodyClassName={bodyClassName}
      />
    </div>
  );
};

export default MinerList;
