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
  miners: MinerStateSnapshot[];
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

const MinerList = ({ title, miners = [] }: MinerListProps) => {
  const filters = useMemo(() => {
    const countMiners = (status: MinerFilterState) => {
      // TODO: need to determine what properties need to be added to MinerStateSnapshot to support our filters
      void status;
      return miners.filter(
        () => true,
        // (m) => m.status && m.status[status as MinerStatusKey] === true,
      ).length;
    };

    return [
      {
        type: "button",
        title: "All miners",
        value: defaultListFilter,
        count: miners.length,
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
  }, [miners]);

  const filterMiner = (
    item: MinerStateSnapshot,
    activeButtonFilters: (MinerFilterState | typeof defaultListFilter)[],
  ) => {
    if (activeButtonFilters.includes(defaultListFilter)) {
      return true;
    }

    for (const filter of activeButtonFilters) {
      if (
        item.status?.[filter as keyof MinerStateSnapshot["status"]] === true
      ) {
        return true;
      }
    }

    return false;
  };

  return (
    <div>
      <h2 className="text-heading-300">{title}</h2>
      <List<
        MinerStateSnapshot,
        MinerStateSnapshot["deviceIdentifier"],
        MinerFilterState
      >
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        filters={filters}
        filterItem={filterMiner}
        items={miners}
        itemKey="deviceIdentifier"
        itemSelectable
        renderActionBar={(selectedItems) => (
          <MinerListActionBar selectedMiners={selectedItems} />
        )}
      />
    </div>
  );
};

export default MinerList;
