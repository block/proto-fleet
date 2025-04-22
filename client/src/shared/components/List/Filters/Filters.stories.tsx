import { useMemo } from "react";
import FiltersComponent from "./index";
import {
  MinerFilterState,
  minerFilterStates,
} from "@/protoFleet/components/MinerList/constants";
import { miners } from "@/protoFleet/components/MinerList/stories/mocks";
import { MinerStatusKey } from "@/protoFleet/components/MinerList/types";
import { defaultListFilter } from "@/shared/components/List/constants";
import { FilterItem } from "@/shared/components/List/Filters/types";
import { statuses } from "@/shared/components/StatusCircle";

interface FiltersArgs {
  numberOfFilters: number;
}

export const Filters = ({ numberOfFilters }: FiltersArgs) => {
  const filters = useMemo(() => {
    const countMiners = (status: MinerFilterState) => {
      return miners.filter((m) => m.status[status as MinerStatusKey] === true)
        .length;
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
  }, []);

  return (
    <FiltersComponent
      className="gap-4 py-4"
      filterItems={filters.slice(0, numberOfFilters)}
      items={miners}
      onFilter={() => {}}
    />
  );
};

export default {
  title: "Components (Shared)/List/Filters",
  parameters: {
    docs: {
      description: {
        component:
          "A reusable and configurable filter component for refining displayed data in a list. \n" +
          "It supports:\n " +
          " - Customizable filter items with title, count, and status.\n " +
          " - Dynamic filtering logic through the `onFilter` callback.\n " +
          " - Integration with list components for seamless data filtering.",
      },
    },
  },
  args: {
    numberOfFilters: 5,
  },
  argTypes: {
    numberOfFilters: { control: { type: "range", min: 1, max: 5, step: 1 } },
  },
  tags: ["autodocs"],
};
