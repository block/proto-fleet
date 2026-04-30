import { ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import ButtonFilter from "./ButtonFilter";
import DropdownFilter, { type DropdownOption } from "./DropdownFilter";
import NestedDropdownFilter, { type FilterCategory } from "./NestedDropdownFilter";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { defaultListFilter } from "@/shared/components/List/constants";
import { ActiveFilters, type DropdownFilterItem, type FilterItem } from "@/shared/components/List/Filters/types";

type FilterProps<ItemType> = {
  className?: string;
  filterItems: FilterItem[];
  filterSize?: keyof typeof sizes;
  items: ItemType[];
  onFilter: (activeFilters: ActiveFilters) => void | Promise<void>;
  isServerSide?: boolean;
  headerControls?: ReactNode;
  initialActiveFilters?: ActiveFilters;
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
  initialActiveFilters,
}: FilterProps<ItemType>) => {
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(
    initialActiveFilters || {
      buttonFilters: [defaultListFilter],
      dropdownFilters: {},
    },
  );

  // Store onFilter in a ref to avoid re-running effects when the callback reference changes.
  // The callback changes when parent's items change (due to useCallback dependencies in List),
  // but we only want to call onFilter when activeFilters actually changes.
  const onFilterRef = useRef(onFilter);
  useLayoutEffect(() => {
    onFilterRef.current = onFilter;
  }, [onFilter]);

  // Sync internal state when initialActiveFilters changes (e.g., URL navigation from a
  // sibling component). Uses the during-render derivation pattern so React reschedules cleanly.
  // Skips the resulting onFilter call so the URL writer doesn't loop.
  const initialActiveFiltersKey = useMemo(() => JSON.stringify(initialActiveFilters ?? null), [initialActiveFilters]);
  const [prevSyncedKey, setPrevSyncedKey] = useState(initialActiveFiltersKey);
  const skipNextOnFilterRef = useRef(false);
  if (prevSyncedKey !== initialActiveFiltersKey) {
    setPrevSyncedKey(initialActiveFiltersKey);
    if (initialActiveFilters) {
      skipNextOnFilterRef.current = true;
      setActiveFilters(initialActiveFilters);
    }
  }

  useEffect(() => {
    if (skipNextOnFilterRef.current) {
      skipNextOnFilterRef.current = false;
      return;
    }
    onFilterRef.current(activeFilters);
  }, [activeFilters]);

  // Ensure the client side filter is applied when items change
  useEffect(() => {
    if (!isServerSide) {
      onFilterRef.current(activeFilters);
    }
  }, [items, isServerSide, activeFilters]);

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
        newButtonFilters = newButtonFilters.filter((f) => f !== defaultListFilter);
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

  const setDropdownSelection = useCallback((value: string, selectedIds: string[]) => {
    setActiveFilters((prev) => ({
      ...prev,
      dropdownFilters: {
        ...prev.dropdownFilters,
        [value]: selectedIds,
      },
    }));
  }, []);

  // Walk every dropdown source (top-level + every nestedFilterDropdown.children) and
  // dedup by `value`. First-seen wins so callers control which surface "owns" the option
  // labels in the active-pill row when the same key is exposed in multiple places.
  const dedupedDropdownSources = useMemo(() => {
    const map = new Map<string, DropdownFilterItem>();
    filterItems.forEach((filter) => {
      if (filter.type === "dropdown") {
        if (!map.has(filter.value)) map.set(filter.value, filter);
      } else if (filter.type === "nestedFilterDropdown") {
        filter.children.forEach((child) => {
          if (!map.has(child.value)) map.set(child.value, child);
        });
      }
    });
    return Array.from(map.values());
  }, [filterItems]);

  const activeDropdownFilterItems = useMemo(() => {
    const items: ActiveDropdownFilterItem[] = [];
    dedupedDropdownSources.forEach((filter) => {
      const selectedIds = activeFilters.dropdownFilters[filter.value] || [];
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
    });
    return items;
  }, [activeFilters.dropdownFilters, dedupedDropdownSources]);

  const handleRemoveDropdownFilter = useCallback(
    (optionId: string, filterValue: string) => {
      const currentSelection = activeFilters.dropdownFilters[filterValue] || [];
      const newSelection = currentSelection.filter((id) => id !== optionId);
      setDropdownSelection(filterValue, newSelection);
    },
    [activeFilters.dropdownFilters, setDropdownSelection],
  );

  return (
    <div className={clsx("flex w-full flex-col gap-2", className)}>
      {/* Filter buttons row */}
      <div className="flex flex-row flex-wrap items-center gap-2">
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
          }

          if (filter.type === "dropdown") {
            const selectedOptions = activeFilters.dropdownFilters[filter.value];
            return (
              <div key={filter.value}>
                <DropdownFilter
                  title={filter.title}
                  pluralTitle={filter.title + "s"}
                  options={filter.options}
                  selectedOptions={selectedOptions || []}
                  showSelectAll={filter.showSelectAll}
                  onSelect={(items) => setDropdownSelection(filter.value, items)}
                  withButtons={isServerSide}
                />
              </div>
            );
          }

          if (filter.type === "nestedFilterDropdown") {
            const categories: FilterCategory[] = filter.children.map((child) => ({
              key: child.value,
              label: child.title,
              options: child.options,
              selectedValues: activeFilters.dropdownFilters[child.value] ?? [],
            }));
            return (
              <NestedDropdownFilter
                key={filter.value}
                testId={`filter-nested-${filter.value}`}
                label={filter.title}
                categories={categories}
                onChange={setDropdownSelection}
                onClearAll={() =>
                  setActiveFilters((prev) => {
                    const next = { ...prev.dropdownFilters };
                    filter.children.forEach((child) => {
                      delete next[child.value];
                    });
                    return { ...prev, dropdownFilters: next };
                  })
                }
              />
            );
          }

          return null;
        })}
        {headerControls ? (
          <div className="ml-auto tablet:mr-(--list-padding-tablet) laptop:mr-(--list-padding-laptop) desktop:mr-(--list-padding-desktop) phone:mr-(--list-padding-phone)">
            {headerControls}
          </div>
        ) : null}
      </div>

      {/* Active dropdown filters row */}
      {activeDropdownFilterItems.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {activeDropdownFilterItems.map((item) => (
            <Button
              size={sizes.compact}
              variant={variants.accent}
              key={`${item.filterValue}-${item.id}`}
              prefixIcon={<DismissTiny />}
              onClick={() => handleRemoveDropdownFilter(item.id, item.filterValue)}
              testId={`active-filter-${item.filterValue}-${item.id}`}
            >
              {item.label}
            </Button>
          ))}
        </div>
      ) : null}
    </div>
  );
};

export default Filters;
