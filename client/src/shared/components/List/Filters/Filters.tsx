import { ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import ButtonFilter from "./ButtonFilter";
import DropdownFilter from "./DropdownFilter";
import FilterChip from "./FilterChip";
import ModalFilterChip from "./ModalFilterChip";
import NestedDropdownFilter from "./NestedDropdownFilter";
import NumericRangeModal from "./NumericRangeModal";
import TextareaListModal from "./TextareaListModal";
import { sizes } from "@/shared/components/Button";
import { defaultListFilter } from "@/shared/components/List/constants";
import {
  ActiveFilters,
  type DropdownFilterItem,
  type FilterCategory,
  type FilterItem,
  type NestedFilterChildItem,
  type NumericRangeFilterItem,
  type TextareaListFilterItem,
} from "@/shared/components/List/Filters/types";
import { formatNumericRangeCondition, formatTextareaListCondition } from "@/shared/utils/filterChipFormatting";
import type { NumericRangeValue } from "@/shared/utils/filterValidation";

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
      numericFilters: {},
      textareaListFilters: {},
    }),
    [],
  );

  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(initialActiveFilters || defaultActiveFilters);
  // Tracking the open chip keeps it mounted while the user toggles its last selection off
  // — otherwise the chip unmounts mid-interaction and takes its popover with it.
  const [openChipFilterValue, setOpenChipFilterValue] = useState<string | null>(null);

  // Tracks which numeric/textareaList category the user is currently editing
  // in a modal. The modal is parent-rendered (not inside the dropdown popover)
  // because users want a focused surface and explicit Apply, not on-input
  // propagation. `null` = no modal open.
  const [editingFilterKey, setEditingFilterKey] = useState<string | null>(null);

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

  const setNumericFilter = useCallback((key: string, value: NumericRangeValue) => {
    setActiveFilters((prev) => {
      const next = { ...prev.numericFilters };
      if (value.min === undefined && value.max === undefined) {
        delete next[key];
      } else {
        next[key] = value;
      }
      return { ...prev, numericFilters: next };
    });
  }, []);

  const setTextareaListFilter = useCallback((key: string, values: string[]) => {
    setActiveFilters((prev) => {
      const next = { ...prev.textareaListFilters };
      if (values.length === 0) {
        delete next[key];
      } else {
        next[key] = values;
      }
      return { ...prev, textareaListFilters: next };
    });
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
          // Only checkbox children expose option lists; numeric/textareaList
          // categories live in their own ActiveFilters buckets and don't
          // surface in the active-pill row.
          if (child.type === "dropdown" && !map.has(child.value)) {
            map.set(child.value, child);
          }
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

  // Look up numeric/textareaList children once so the chip render below can resolve
  // a key (e.g. "hashrate") back to the originating FilterItem (label, bounds, etc.)
  // without re-walking every nested dropdown's children on every render.
  const modalChildByKey = useMemo(() => {
    const map = new Map<string, NumericRangeFilterItem | TextareaListFilterItem>();
    nestedFilters.forEach((filter) => {
      filter.children.forEach((child) => {
        if (child.type === "numericRange" || child.type === "textareaList") {
          if (!map.has(child.value)) map.set(child.value, child);
        }
      });
    });
    return map;
  }, [nestedFilters]);

  // Active chips for numeric / textareaList filters. We build them as a flat list
  // so they slot into the chip row alongside FilterChip instances and respect the
  // parent declaration order (numeric keys first, then textareaList keys).
  const activeModalChips = useMemo(() => {
    const chips: { key: string; child: NumericRangeFilterItem | TextareaListFilterItem; condition: string }[] = [];
    Object.entries(activeFilters.numericFilters).forEach(([key, value]) => {
      const child = modalChildByKey.get(key);
      if (!child || child.type !== "numericRange") return;
      const condition = formatNumericRangeCondition(value, child.bounds.unit);
      if (!condition) return;
      chips.push({ key, child, condition });
    });
    Object.entries(activeFilters.textareaListFilters).forEach(([key, values]) => {
      const child = modalChildByKey.get(key);
      if (!child || child.type !== "textareaList") return;
      const condition = formatTextareaListCondition(values, { noun: child.noun });
      if (!condition) return;
      chips.push({ key, child, condition });
    });
    return chips;
  }, [activeFilters.numericFilters, activeFilters.textareaListFilters, modalChildByKey]);

  return (
    <div className={clsx("flex w-full flex-row items-center justify-start", className)}>
      <div className="flex min-w-0 grow flex-wrap items-center gap-2">
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

        {activeModalChips.map(({ key, child, condition }) => (
          <ModalFilterChip
            key={key}
            filterValue={key}
            typeLabel={child.title}
            condition={condition}
            onEdit={() => setEditingFilterKey(key)}
            onClear={() => {
              if (child.type === "numericRange") {
                setNumericFilter(key, {});
              } else {
                setTextareaListFilter(key, []);
              }
            }}
          />
        ))}

        {nestedFilters.map((filter) => {
          const childByKey = new Map(filter.children.map((c) => [c.value, c] as const));
          const categories: FilterCategory[] = filter.children.map((child): FilterCategory => {
            if (child.type === "numericRange") {
              return {
                kind: "numericRange",
                key: child.value,
                label: child.title,
                bounds: child.bounds,
                value: activeFilters.numericFilters[child.value] ?? {},
                showGroupDivider: child.showGroupDivider,
              };
            }
            if (child.type === "textareaList") {
              return {
                kind: "textareaList",
                key: child.value,
                label: child.title,
                validate: child.validate,
                normalize: child.normalize,
                placeholder: child.placeholder,
                maxLines: child.maxLines,
                value: activeFilters.textareaListFilters[child.value] ?? [],
                showGroupDivider: child.showGroupDivider,
              };
            }
            return {
              kind: "checkbox",
              key: child.value,
              label: child.title,
              options: child.options,
              selectedValues: activeFilters.dropdownFilters[child.value] ?? [],
              showGroupDivider: child.showGroupDivider,
            };
          });
          const editingChild = editingFilterKey ? childByKey.get(editingFilterKey) : undefined;
          const editingNumeric =
            editingChild?.type === "numericRange" ? (editingChild as NumericRangeFilterItem) : undefined;
          const editingTextareaList =
            editingChild?.type === "textareaList" ? (editingChild as TextareaListFilterItem) : undefined;
          return (
            <div key={filter.value}>
              <NestedDropdownFilter
                testId={`filter-nested-${filter.value}`}
                label={filter.title}
                prefixIcon={filter.prefixIcon}
                categories={categories}
                onCheckboxChange={setDropdownSelection}
                onRequestEdit={setEditingFilterKey}
                onClearAll={() =>
                  setActiveFilters((prev) => {
                    const nextDropdown = { ...prev.dropdownFilters };
                    const nextNumeric = { ...prev.numericFilters };
                    const nextTextareaList = { ...prev.textareaListFilters };
                    filter.children.forEach((child: NestedFilterChildItem) => {
                      delete nextDropdown[child.value];
                      delete nextNumeric[child.value];
                      delete nextTextareaList[child.value];
                    });
                    return {
                      ...prev,
                      dropdownFilters: nextDropdown,
                      numericFilters: nextNumeric,
                      textareaListFilters: nextTextareaList,
                    };
                  })
                }
              />
              {editingNumeric ? (
                <NumericRangeModal
                  open
                  categoryKey={editingNumeric.value}
                  label={editingNumeric.title}
                  bounds={editingNumeric.bounds}
                  initialValue={activeFilters.numericFilters[editingNumeric.value] ?? {}}
                  onApply={(value) => setNumericFilter(editingNumeric.value, value)}
                  onClose={() => setEditingFilterKey(null)}
                />
              ) : null}
              {editingTextareaList ? (
                <TextareaListModal
                  open
                  categoryKey={editingTextareaList.value}
                  label={editingTextareaList.title}
                  validate={editingTextareaList.validate}
                  normalize={editingTextareaList.normalize}
                  placeholder={editingTextareaList.placeholder}
                  maxLines={editingTextareaList.maxLines}
                  initialValue={activeFilters.textareaListFilters[editingTextareaList.value] ?? []}
                  onApply={(value) => setTextareaListFilter(editingTextareaList.value, value)}
                  onClose={() => setEditingFilterKey(null)}
                />
              ) : null}
            </div>
          );
        })}
      </div>

      {headerControls ? (
        <div className="shrink-0 tablet:mr-(--list-padding-tablet) laptop:mr-(--list-padding-laptop) desktop:mr-(--list-padding-desktop) phone:mr-(--list-padding-phone)">
          {headerControls}
        </div>
      ) : null}
    </div>
  );
};

export default Filters;
