import { ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import ButtonFilter from "./ButtonFilter";
import DropdownFilter from "./DropdownFilter";
import FilterChip from "./FilterChip";
import NestedDropdownFilter, { type FilterCategory } from "./NestedDropdownFilter";
import { sizes } from "@/shared/components/Button";
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

type ActiveDropdownFilterGroup = {
  filterValue: string;
  title: string;
  pluralTitle?: string;
  options: DropdownFilterItem["options"];
  selectedIds: string[];
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
  const defaultActiveFilters = useMemo<ActiveFilters>(
    () => ({
      buttonFilters: [defaultListFilter],
      dropdownFilters: {},
    }),
    [],
  );

  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(initialActiveFilters || defaultActiveFilters);
  // Tracking the open chip keeps it mounted while the user toggles its last selection off
  // — otherwise the chip unmounts mid-interaction and takes its popover with it.
  const [openChipFilterValue, setOpenChipFilterValue] = useState<string | null>(null);

  // Store onFilter in a ref to avoid re-running effects when the callback reference changes.
  // The callback changes when parent's items change (due to useCallback dependencies in List),
  // but we only want to call onFilter when activeFilters actually changes.
  const onFilterRef = useRef(onFilter);
  useLayoutEffect(() => {
    onFilterRef.current = onFilter;
  }, [onFilter]);

  // Sync internal state when initialActiveFilters changes (e.g., URL navigation from a
  // sibling component, or the parent clearing the prop). Uses the during-render derivation
  // pattern so React reschedules cleanly. Skips the resulting onFilter call so the URL
  // writer doesn't loop. When the prop transitions to undefined, fall back to defaults so
  // stale selections don't linger.
  const initialActiveFiltersKey = useMemo(() => JSON.stringify(initialActiveFilters ?? null), [initialActiveFilters]);
  const [prevSyncedKey, setPrevSyncedKey] = useState(initialActiveFiltersKey);
  const skipNextOnFilterRef = useRef(false);
  if (prevSyncedKey !== initialActiveFiltersKey) {
    setPrevSyncedKey(initialActiveFiltersKey);
    skipNextOnFilterRef.current = true;
    setActiveFilters(initialActiveFilters ?? defaultActiveFilters);
    // Drop any chip-edit state from before the resync so an external sync (back/forward,
    // sibling URL writer) doesn't leave a stale empty chip mounted.
    setOpenChipFilterValue(null);
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

  const activeDropdownFilterGroups = useMemo<ActiveDropdownFilterGroup[]>(() => {
    const groups: ActiveDropdownFilterGroup[] = [];
    dedupedDropdownSources.forEach((filter) => {
      const selectedIds = activeFilters.dropdownFilters[filter.value] || [];
      if (selectedIds.length === 0 && openChipFilterValue !== filter.value) return;
      groups.push({
        filterValue: filter.value,
        title: filter.title,
        pluralTitle: filter.pluralTitle,
        options: filter.options,
        selectedIds,
      });
    });
    return groups;
  }, [activeFilters.dropdownFilters, dedupedDropdownSources, openChipFilterValue]);

  const leadingFilters = useMemo(
    () => filterItems.filter((filter) => filter.type !== "nestedFilterDropdown"),
    [filterItems],
  );
  const nestedFilters = useMemo(
    () =>
      filterItems.filter(
        (filter): filter is Extract<FilterItem, { type: "nestedFilterDropdown" }> =>
          filter.type === "nestedFilterDropdown",
      ),
    [filterItems],
  );

  return (
    <div className={clsx("flex w-full flex-row flex-wrap items-center gap-2", className)}>
      {leadingFilters.map((filter) => {
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
                pluralTitle={filter.pluralTitle ?? `${filter.title}s`}
                options={filter.options}
                selectedOptions={selectedOptions || []}
                showSelectAll={filter.showSelectAll}
                onSelect={(items) => setDropdownSelection(filter.value, items)}
                withButtons={isServerSide}
              />
            </div>
          );
        }

        return null;
      })}

      {activeDropdownFilterGroups.map((group) => (
        <FilterChip
          key={group.filterValue}
          filterValue={group.filterValue}
          title={group.title}
          pluralTitle={group.pluralTitle}
          options={group.options}
          selectedIds={group.selectedIds}
          onChange={(ids) => setDropdownSelection(group.filterValue, ids)}
          onClear={() => {
            setDropdownSelection(group.filterValue, []);
            setOpenChipFilterValue((prev) => (prev === group.filterValue ? null : prev));
          }}
          onOpenChange={(open) =>
            setOpenChipFilterValue((prev) => {
              if (open) return group.filterValue;
              return prev === group.filterValue ? null : prev;
            })
          }
        />
      ))}

      {nestedFilters.map((filter) => {
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
            prefixIcon={filter.prefixIcon}
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
      })}

      {headerControls ? (
        <div className="ml-auto tablet:mr-(--list-padding-tablet) laptop:mr-(--list-padding-laptop) desktop:mr-(--list-padding-desktop) phone:mr-(--list-padding-phone)">
          {headerControls}
        </div>
      ) : null}
    </div>
  );
};

export default Filters;
