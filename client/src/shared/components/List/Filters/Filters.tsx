import { useEffect, useState } from "react";
import clsx from "clsx";

import { PopoverProvider } from "../../Popover";
import ButtonFilter from "./ButtonFilter";
import DropdownFilter from "./DropdownFilter";
import { sizes } from "@/shared/components/Button";
import { defaultListFilter } from "@/shared/components/List/constants";
import {
  ActiveFilters,
  type FilterItem,
} from "@/shared/components/List/Filters/types";

type FilterProps<ItemType> = {
  className?: string;
  filterItems: FilterItem[];
  filterSize?: keyof typeof sizes;
  items: ItemType[];
  onFilter: (activeFilters: ActiveFilters) => void | Promise<void>;
  isServerSide?: boolean;
};

const Filters = <ItemType,>({
  className,
  filterItems,
  filterSize = sizes.compact,
  items,
  onFilter,
  isServerSide = false,
}: FilterProps<ItemType>) => {
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>({
    buttonFilters: [defaultListFilter],
    dropdownFilters: {},
  });

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
  }, [activeFilters, onFilter]);

  // Ensure the client side filter is applied when items change
  useEffect(() => {
    if (!isServerSide) {
      onFilter(activeFilters);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items]);

  const handleButtonFilterChange = (filter: string) => {
    setActiveFilters((prev) => {
      if (filter === defaultListFilter) {
        return {
          ...prev,
          buttonFilters: [defaultListFilter],
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
          newButtonFilters = [defaultListFilter];
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
            <ButtonFilter
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
