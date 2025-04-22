import { Key, useEffect, useState } from "react";

// import DropDown from "@/shared/components/DropDown";
import clsx from "clsx";
import FilterItemComponent from "./FilterItem";
import { defaultListFilter } from "@/shared/components/List/constants";
import { type FilterItem } from "@/shared/components/List/Filters/types";

type FilterProps<ItemType, FilterType extends Key> = {
  className?: string;
  filterItems: FilterItem<FilterType>[];
  items: ItemType[];
  onFilter: (activeFilter: FilterType | typeof defaultListFilter) => void;
};

const Filters = <ItemType, FilterType extends Key>({
  className,
  filterItems,
  items,
  onFilter,
}: FilterProps<ItemType, FilterType>) => {
  type CompleteFilterType = FilterType | typeof defaultListFilter;
  const [activeFilter, setActiveFilter] =
    useState<CompleteFilterType>(defaultListFilter);

  useEffect(() => {
    onFilter(activeFilter);
  }, [activeFilter, items, onFilter]);

  return (
    <div className={clsx("flex flex-row flex-wrap", className)}>
      {filterItems.map((filter) => (
        <FilterItemComponent<CompleteFilterType>
          key={filter.value}
          status={filter.status}
          title={filter.title}
          count={filter.count}
          filter={filter.value}
          activeFilter={activeFilter}
          setActiveFilter={setActiveFilter}
        />
      ))}

      {/* <DropDown 
        title="Model"
        options={["Proto R1", "Proto R2"]}
        selectedValue=""
        onChange={(value) => console.log(value)}
      />

      <DropDown 
        title="Rack"
        options={["Rack 1", "Rack 2", "Rack 3"]}
        selectedValue=""
        onChange={(value) => console.log(value)}
      /> */}
    </div>
  );
};

export default Filters;
