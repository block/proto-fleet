import { ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import ButtonFilter from "./ButtonFilter";
import DropdownFilter, { type DropdownOption } from "./DropdownFilter";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
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
  headerControls?: ReactNode;
};

type ActiveDropdownFilterItem = DropdownOption & {
  filterValue: string;
};

const Filters = <ItemType,>({
  className,
  filterItems,
  filterSize = sizes.compact,
  items,
  onFilter,
  isServerSide = false,
  headerControls,
}: FilterProps<ItemType>) => {
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>({
    buttonFilters: [defaultListFilter],
    dropdownFilters: {},
  });

  // Initialize all dropdown filters with empty arrays (no selections)
  useEffect(() => {
    const initialDropdownValues: { [key: string]: string[] } = {};

    filterItems.forEach((filter) => {
      if (filter.type === "dropdown") {
        const filterKey = filter.value as string;
        // Start with empty selection (no filtering)
        initialDropdownValues[filterKey] = [];
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

  // Derive active dropdown filter items from activeFilters - no need for separate state
  const activeDropdownFilterItems = useMemo(() => {
    const items: ActiveDropdownFilterItem[] = [];

    filterItems.forEach((filter) => {
      if (filter.type === "dropdown") {
        const selectedIds = activeFilters.dropdownFilters[filter.value] || [];

        // Only add items if there are selections (empty means no filtering)
        if (selectedIds.length > 0) {
          filter.options.forEach((option) => {
            if (selectedIds.includes(option.id)) {
              items.push({
                ...option,
                filterValue: filter.value,
              });
            }
          });
        }
      }
    });

    return items;
  }, [activeFilters.dropdownFilters, filterItems]);

  const handleRemoveDropdownFilter = useCallback(
    (optionId: string, filterValue: string) => {
      const currentSelection = activeFilters.dropdownFilters[filterValue] || [];
      const newSelection = currentSelection.filter((id) => id !== optionId);

      setActiveFilters((prev) => ({
        ...prev,
        dropdownFilters: {
          ...prev.dropdownFilters,
          [filterValue]: newSelection,
        },
      }));
    },
    [activeFilters.dropdownFilters],
  );

  return (
    <div className={clsx("sticky left-0 flex flex-col gap-2", className)}>
      {/* Filter buttons row */}
      <div className="inline-flex flex-row flex-wrap items-center gap-2">
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
            const selectedOptions = activeFilters.dropdownFilters[filter.value];

            return (
              <div key={filter.value}>
                <DropdownFilter
                  title={filter.title}
                  pluralTitle={filter.title + "s"}
                  options={filter.options}
                  selectedOptions={selectedOptions || []}
                  onSelect={(items) => {
                    setActiveFilters((prev) => ({
                      ...prev,
                      dropdownFilters: {
                        ...prev.dropdownFilters,
                        [filter.value]: items,
                      },
                    }));
                  }}
                  withButtons={isServerSide}
                />
              </div>
            );
          }
          return null;
        })}
        {headerControls && <div className="mr-6 ml-auto">{headerControls}</div>}
      </div>

      {/* Active dropdown filters row */}
      {activeDropdownFilterItems.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {activeDropdownFilterItems.map((item) => (
            <Button
              size={sizes.compact}
              variant={variants.secondary}
              key={`${item.filterValue}-${item.id}`}
              prefixIcon={<DismissTiny />}
              onClick={() =>
                handleRemoveDropdownFilter(item.id, item.filterValue)
              }
            >
              {item.label}
            </Button>
          ))}
        </div>
      )}
    </div>
  );
};

export default Filters;
