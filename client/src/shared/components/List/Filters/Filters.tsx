import { ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import ButtonFilter from "./ButtonFilter";
import DropdownFilter, { type DropdownOption } from "./DropdownFilter";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { defaultListFilter } from "@/shared/components/List/constants";
import { ActiveFilters, type DropdownFilterItem, type FilterItem } from "@/shared/components/List/Filters/types";

type FilterProps<ItemType> = {
  className?: string;
  filterItems: FilterItem[];
  /**
   * Filters that participate in the active-pill row but have no standalone trigger
   * in the bar (surfaced via the leadingControls slot, e.g. a meta-dropdown).
   */
  metaOnlyFilters?: DropdownFilterItem[];
  /**
   * Optional content rendered at the start of the filter bar, before the standalone
   * filterItems. Used to host a meta-dropdown that exposes the full filter set.
   */
  leadingControls?: ReactNode;
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
  metaOnlyFilters,
  leadingControls,
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

  // Derive active dropdown filter items from activeFilters - no need for separate state.
  // metaOnlyFilters participate in the pill row even though they have no standalone trigger.
  const activeDropdownFilterItems = useMemo(() => {
    const items: ActiveDropdownFilterItem[] = [];
    const dropdownSources: DropdownFilterItem[] = [
      ...filterItems.filter((f): f is DropdownFilterItem => f.type === "dropdown"),
      ...(metaOnlyFilters ?? []),
    ];

    dropdownSources.forEach((filter) => {
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
  }, [activeFilters.dropdownFilters, filterItems, metaOnlyFilters]);

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
    <div className={clsx("flex w-full flex-col gap-2", className)}>
      {/* Filter buttons row */}
      <div className="flex flex-row flex-wrap items-center gap-2">
        {leadingControls}
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
                  showSelectAll={filter.showSelectAll}
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
