import { Key, useEffect, useState } from "react";
import clsx from "clsx";

import { PopoverProvider } from "../../Popover";
import ButtonFilter from "./ButtonFilter";
import DropdownFilter from "./DropdownFilter";
import { sizes } from "@/shared/components/Button";
import { defaultListFilter } from "@/shared/components/List/constants";
import { type FilterItem } from "@/shared/components/List/Filters/types";

type ActiveFilters<FilterType extends Key> = {
  buttonFilters: (FilterType | typeof defaultListFilter)[];
  dropdownFilters: Record<string, string>;
};

type FilterProps<ItemType, FilterType extends Key> = {
  className?: string;
  filterItems: FilterItem<FilterType>[];
  filterSize?: keyof typeof sizes;
  items: ItemType[];
  onFilter: (activeFilters: ActiveFilters<FilterType>) => void;
};

const Filters = <ItemType, FilterType extends Key>({
  className,
  filterItems,
  filterSize = sizes.compact,
  items,
  onFilter,
}: FilterProps<ItemType, FilterType>) => {
  type CompleteFilterType = FilterType | typeof defaultListFilter;

  const [activeFilters, setActiveFilters] = useState<ActiveFilters<FilterType>>(
    {
      buttonFilters: [defaultListFilter as CompleteFilterType],
      dropdownFilters: {},
    },
  );

  useEffect(() => {
    const initialDropdownValues: { [key: string]: string } = {};

    filterItems.forEach((filter) => {
      if (filter.type === "dropdown" && filter.defaultOptionId) {
        const filterKey = filter.value as string;
        initialDropdownValues[filterKey] = filter.defaultOptionId;
      }
    });

    if (Object.keys(initialDropdownValues).length > 0) {
      setActiveFilters((prev) => {
        const newDropdownFilters = { ...prev.dropdownFilters };
        Object.entries(initialDropdownValues).forEach(([key, value]) => {
          if (!prev.dropdownFilters[key]) {
            newDropdownFilters[key] = value;
          }
        });

        return {
          ...prev,
          dropdownFilters: newDropdownFilters,
        };
      });
    }
  }, [filterItems]);

  useEffect(() => {
    onFilter(activeFilters);
  }, [activeFilters, items, onFilter]);

  const handleButtonFilterChange = (filter: CompleteFilterType) => {
    setActiveFilters((prev) => {
      if (filter === defaultListFilter) {
        return {
          ...prev,
          buttonFilters: [defaultListFilter as CompleteFilterType],
          dropdownFilters: { ...prev.dropdownFilters },
        };
      }

      let newButtonFilters = [...prev.buttonFilters];

      // Remove "all" filter if it exists and we're adding a different filter
      if (newButtonFilters.includes(defaultListFilter)) {
        newButtonFilters = newButtonFilters.filter(
          (f) => f !== defaultListFilter,
        );
      }

      // Toggle the filter
      if (newButtonFilters.includes(filter)) {
        newButtonFilters = newButtonFilters.filter((f) => f !== filter);

        // If no filters remain, add back the "all" filter
        if (newButtonFilters.length === 0) {
          newButtonFilters = [defaultListFilter as CompleteFilterType];
        }
      } else {
        newButtonFilters.push(filter);
      }

      return {
        ...prev,
        buttonFilters: newButtonFilters,
        dropdownFilters: { ...prev.dropdownFilters },
      };
    });
  };

  // Handle dropdown filter change
  const handleDropdownFilterChange = (filterKey: string, value: string) => {
    setActiveFilters((prev) => ({
      ...prev,
      dropdownFilters: {
        ...prev.dropdownFilters,
        [filterKey]: value,
      },
    }));
  };

  return (
    <div className={clsx("flex flex-row flex-wrap items-center", className)}>
      {filterItems.map((filter) => {
        if (filter.type === "button") {
          return (
            <ButtonFilter<CompleteFilterType>
              key={filter.value}
              status={filter.status}
              title={filter.title}
              count={filter.count}
              filter={filter.value}
              activeFilters={activeFilters.buttonFilters}
              setActiveFilter={handleButtonFilterChange}
              size={filterSize}
            />
          );
        } else if (filter.type === "dropdown") {
          const selectedOption =
            activeFilters.dropdownFilters[filter.value as string];
          return (
            <div key={filter.value}>
              <PopoverProvider>
                <DropdownFilter
                  title={filter.title}
                  options={filter.options}
                  selectedOption={selectedOption}
                  size={filterSize}
                  onSelect={(value) =>
                    handleDropdownFilterChange(filter.value as string, value)
                  }
                />
              </PopoverProvider>
            </div>
          );
        }
        return null;
      })}
    </div>
  );
};

export default Filters;
