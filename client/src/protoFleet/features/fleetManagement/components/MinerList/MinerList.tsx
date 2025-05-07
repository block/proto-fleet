import { useMemo } from "react";

import {
  minerCols,
  minerColTitles,
  type MinerFilterState,
  minerFilterStates,
} from "./constants";
import minerColConfig from "./minerColConfig";
import { type Miner } from "./types";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";
import { type MinerStatusKey } from "@/protoFleet/features/fleetManagement/types";
import List from "@/shared/components/List";
import { defaultListFilter } from "@/shared/components/List/constants";
import { FilterItem } from "@/shared/components/List/Filters/types";
import { statuses } from "@/shared/components/StatusCircle/constants";

type MinerListProps = {
  title: string;
  miners: Miner[];
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
] as (keyof Miner)[];

const MinerList = ({ title, miners = [] }: MinerListProps) => {
  const filters = useMemo(() => {
    const countMiners = (status: MinerFilterState) => {
      return miners.filter(
        (m) => m.status && m.status[status as MinerStatusKey] === true,
      ).length;
    };

    return [
      {
        title: "All miners",
        value: defaultListFilter,
        count: miners.length,
      },
      {
        title: "Hashing",
        value: minerFilterStates.hashing,
        count: countMiners(minerFilterStates.hashing),
        status: statuses.normal,
      },
      {
        title: "Broken",
        value: minerFilterStates.broken,
        count: countMiners(minerFilterStates.broken),
        status: statuses.error,
      },
      {
        title: "Offline",
        value: minerFilterStates.offline,
        count: countMiners(minerFilterStates.offline),
        status: statuses.warning,
      },
      {
        title: "Asleep",
        value: minerFilterStates.asleep,
        count: countMiners(minerFilterStates.asleep),
        status: statuses.inactive,
      },
    ] as FilterItem<MinerFilterState>[];
  }, [miners]);

  const filterMiner = (item: Miner, activeFilter: MinerFilterState) => {
    return (
      item.status?.[activeFilter as keyof Miner["status"]] === true ||
      activeFilter === defaultListFilter
    );
  };

  return (
    <div>
      <h2 className="text-heading-300">{title}</h2>
      <List<Miner, Miner["deviceIdentifier"], MinerFilterState>
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
