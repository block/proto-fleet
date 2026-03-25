import { ChangeEvent, ReactNode, Ref, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Filters from "@/shared/components/List/Filters";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import ListActions from "@/shared/components/List/ListActions";
import { ColConfig, ColTitles, ListAction, SORT_ASC, SORT_DESC, SortDirection } from "@/shared/components/List/types";
import { PopoverProvider } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SortIndicator from "@/shared/components/SortIndicator";
import { Breakpoint, breakpoints } from "@/shared/constants/breakpoints";
import { useStickyState } from "@/shared/hooks/useStickyState";

type SelectionMode = "none" | "all" | "subset";

type ControlledSelectionModeProps<ItemKeyValueType> = {
  customSelectedItems: ItemKeyValueType[];
  customSetSelectedItems: (selected: ItemKeyValueType[]) => void;
  /**
   * Callback when selection mode changes.
   * Called with "all" when Select All is clicked with no filters,
   * "subset" for individual selections or Select All with filters,
   * "none" when selection is cleared.
   */
  onSelectionModeChange: (mode: SelectionMode) => void;
  /**
   * Controlled selection mode value.
   * Use with customSelectedItems/customSetSelectedItems when the parent owns
   * selection state and needs to keep the list's derived mode in sync.
   */
  customSelectionMode: SelectionMode;
};

type UncontrolledSelectionModeProps<ItemKeyValueType> = {
  customSelectedItems?: ItemKeyValueType[];
  customSetSelectedItems?: (selected: ItemKeyValueType[]) => void;
  /**
   * Callback when selection mode changes.
   * Called with "all" when Select All is clicked with no filters,
   * "subset" for individual selections or Select All with filters,
   * "none" when selection is cleared.
   */
  onSelectionModeChange?: (mode: SelectionMode) => void;
  customSelectionMode?: undefined;
};

type ListProps<ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string> = {
  activeCols: ColKey[];
  colTitles: ColTitles<ColKey>;
  colConfig: ColConfig<ListItem, ItemKeyValueType, ColKey>;
  filters?: FilterItem[];
  filterItem?: (item: ListItem, filters: ActiveFilters) => boolean;
  onServerFilter?: (filters: ActiveFilters) => Promise<void>;
  filterSize?: keyof typeof sizes;
  headerControls?: ReactNode;
  items: ListItem[];
  itemKey: keyof ListItem;
  itemSelectable?: boolean;
  initialSelectedItems?: ItemKeyValueType[];
  disabled?: boolean;
  actions?: ListAction<ListItem>[];
  noDataElement?: ReactNode;
  emptyStateRow?: ReactNode;
  renderActionBar?: (
    selectedItems: ItemKeyValueType[],
    clearSelection: () => void,
    selectionMode: SelectionMode,
    totalSelectable?: number,
  ) => ReactNode;
  containerClassName?: string;
  tableClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  paddingRight?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  stickyBgColor?: string;
  total?: number;
  /**
   * Total number of disabled items across all pages.
   * Used with total to calculate selectable count: total - totalDisabled
   */
  totalDisabled?: number;
  itemName?: {
    singular: string;
    plural: string;
  };
  initialActiveFilters?: ActiveFilters;
  /**
   * When true, suppresses the built-in item count display below the filter bar.
   * Use when the parent component renders its own count (e.g., MinerList subtitle).
   */
  hideTotal?: boolean;
  /**
   * Optional callback to attach refs to list row elements.
   * Useful for viewport visibility tracking (Intersection Observer).
   * @param itemKey - The key value of the item
   * @param element - The tr element for the row (null on unmount)
   */
  itemRef?: (itemKey: ItemKeyValueType, element: HTMLTableRowElement | null) => void;
  /**
   * Whether server-side filters are currently active.
   * Used to determine selection mode: "all" (no filters) vs "subset" (with filters).
   */
  hasActiveFilters?: boolean;
  /**
   * Callback when filters change.
   * Called with the current active filters whenever the user modifies filters.
   */
  onFilterChange?: (filters: ActiveFilters) => void;
  /*
   * Optional callback for infinite scroll. Called when the user scrolls
   * near the bottom of the list.
   */
  onLoadMore?: () => void;
  /**
   * Whether more items are available to load. When false, onLoadMore
   * will not be triggered.
   */
  hasMore?: boolean;
  /**
   * Whether the list is currently loading more items.
   */
  isLoadingMore?: boolean;
  /**
   * Optional callback to determine if a specific row should be disabled.
   * Disabled rows are greyed out and cannot be selected.
   * @param item - The list item
   * @returns true if the row should be disabled
   */
  isRowDisabled?: (item: ListItem) => boolean;
  /**
   * Optional set of column keys that should NOT be affected by disabled row styling.
   * These columns will maintain full opacity even when the row is disabled.
   */
  columnsExemptFromDisabledStyling?: Set<ColKey>;
  /**
   * Set of column keys that support sorting.
   * When provided, these columns will have clickable headers with sort indicators.
   */
  sortableColumns?: Set<ColKey>;
  /**
   * Current sort state. When provided, shows sort indicator on the sorted column.
   */
  currentSort?: { field: ColKey; direction: SortDirection };
  /**
   * Callback fired when a sortable column header is clicked.
   * The direction passed is the NEW direction to sort by.
   */
  onSort?: (field: ColKey, direction: SortDirection) => void;
  /**
   * Optional callback to determine the default sort direction for a column.
   * Called when clicking on a column that isn't currently sorted.
   * Defaults to "desc" if not provided.
   */
  getDefaultSortDirection?: (field: ColKey) => SortDirection;
  /**
   * Optional content to render at the bottom of the scroll container.
   * Useful for pagination controls that should scroll with the list content.
   */
  footerContent?: ReactNode;
  /**
   * When true, apply the configured column widths to the <th>/<td> cells
   * instead of the inner content wrapper. Use this when the configured width
   * should represent the total cell width including inner padding.
   */
  applyColumnWidthsToCells?: boolean;
  /**
   * When true, renders filters outside/above the scroll container so they remain
   * visible while scrolling. Default is false (filters scroll with content).
   */
  /**
   * Ref forwarded to the scrollable container element.
   * Useful for programmatic scroll control (e.g., scroll-to-top on pagination).
   */
  scrollRef?: Ref<HTMLDivElement>;
  /**
   * When true, skips automatic cleanup of customSelectedItems that are not
   * in the current items list. Use for paginated lists where the parent
   * manages selections across pages.
   */
  preserveOffPageSelection?: boolean;
  /**
   * When true, header and row checkbox behavior stays scoped to the current
   * page instead of promoting selection into a dataset-wide "all" state.
   */
  pageScopedSelection?: boolean;
} & (ControlledSelectionModeProps<ItemKeyValueType> | UncontrolledSelectionModeProps<ItemKeyValueType>);

const cellClassList = "text-left";
const rowClassList = "border-b border-border-5";
const thClassList = cellClassList + " py-3 text-emphasis-300 text-text-primary";
const baseStickyClassList = "tablet:sticky laptop:sticky desktop:sticky z-1";
const tdClassList = "text-left text-300";
const tdPaddingClassList = "px-2 py-4";
// use after element for shadow (hidden on phone since column isn't sticky)
// after pseudo-element is always present with opacity-0, transitions to visible when stuck
const columnShadowBaseClassList =
  "after:content-[''] after:absolute after:top-0 after:right-[-6px] after:bottom-[-1px] after:w-[9px] after:bg-[linear-gradient(90deg,rgba(0,0,0,0.06)0%,rgba(0,0,0,0)100%)] after:opacity-0 after:transition-opacity after:duration-500 phone:after:content-none";
const columnShadowVisibleClassList = "after:opacity-100";

const List = <ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string>({
  activeCols,
  colTitles,
  colConfig,
  filters,
  filterItem,
  onServerFilter,
  filterSize = sizes.compact,
  headerControls,
  initialSelectedItems = [],
  customSetSelectedItems,
  customSelectedItems,
  items,
  itemKey,
  itemSelectable = false,
  disabled = false,
  actions = [],
  noDataElement,
  emptyStateRow,
  initialActiveFilters,
  hideTotal = false,
  renderActionBar,
  containerClassName = "",
  tableClassName,
  paddingLeft,
  paddingRight,
  overflowContainer = true,
  stickyBgColor = "bg-surface-base",
  total,
  totalDisabled = 0,
  itemName = { singular: "item", plural: "items" },
  itemRef,
  hasActiveFilters = false,
  onFilterChange,
  onSelectionModeChange,
  customSelectionMode,
  onLoadMore,
  hasMore = false,
  isLoadingMore = false,
  isRowDisabled,
  columnsExemptFromDisabledStyling,
  sortableColumns,
  currentSort,
  onSort,
  getDefaultSortDirection,
  footerContent,
  applyColumnWidthsToCells = false,
  scrollRef,
  preserveOffPageSelection = false,
  pageScopedSelection = false,
}: ListProps<ListItem, ItemKeyValueType, ColKey>) => {
  const { refs, stickyState } = useStickyState();
  const loadMoreTriggerRef = useRef<HTMLDivElement>(null);
  const lastClickedIndexRef = useRef<number | null>(null);

  const [selectedItems, setSelectedItems] = useState<ItemKeyValueType[]>(initialSelectedItems);
  const [filteredItems, setFilteredItems] = useState<ListItem[]>(items);
  const [selectionMode, setSelectionMode] = useState<SelectionMode>("none");
  const [hoveredHeader, setHoveredHeader] = useState<ColKey | null>(null);
  const isServerSideFiltering = useMemo(() => onServerFilter !== undefined, [onServerFilter]);
  const prevCustomSelectedLengthRef = useRef<number | undefined>(undefined);
  const currentSelectionMode = customSelectionMode ?? selectionMode;
  const currentSelectedItems = customSelectedItems ?? selectedItems;

  // Helper to get selectable items (excludes disabled rows)
  const getSelectableItems = useCallback(
    (itemList: ListItem[]) => {
      if (!isRowDisabled) return itemList;
      return itemList.filter((item) => !isRowDisabled(item));
    },
    [isRowDisabled],
  );

  // Calculate total selectable count (total - disabled)
  const totalSelectable = useMemo(() => {
    if (total === undefined) return undefined;
    return total - totalDisabled;
  }, [total, totalDisabled]);

  // Memoized callback for action bar - defined first so handleSelectAll can use it
  const clearSelection = useCallback(() => {
    customSetSelectedItems ? customSetSelectedItems([]) : setSelectedItems([]);
    setSelectionMode("none");
    onSelectionModeChange?.("none");
  }, [customSetSelectedItems, onSelectionModeChange]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      // Select only filtered items (respects both client-side and server-side filters)
      const selectableItems = getSelectableItems(filteredItems);
      const selection = selectableItems.map((item) => item[itemKey] as ItemKeyValueType);
      if (selection.length === 0) {
        clearSelection();
        return;
      }
      customSetSelectedItems ? customSetSelectedItems(selection) : setSelectedItems(selection);
      // If we're selecting filtered items, it's a subset (unless all items match the filter)
      const allItemsMatchFilter = filteredItems.length === items.length;
      const newMode = pageScopedSelection || hasActiveFilters || !allItemsMatchFilter ? "subset" : "all";
      setSelectionMode(newMode);
      onSelectionModeChange?.(newMode);
    } else {
      clearSelection();
    }
  };

  // Clear selection anchor when bulk selection changes (Select All or Clear Selection)
  useEffect(() => {
    if (currentSelectionMode === "all" || currentSelectionMode === "none") {
      lastClickedIndexRef.current = null;
    }
  }, [currentSelectionMode]);

  // Reset selectionMode when customSelectedItems is externally changed from non-empty to empty
  // This handles "Select none" from external controls like ModalSelectAllFooter
  useEffect(() => {
    const prevLength = prevCustomSelectedLengthRef.current;
    const currentLength = customSelectedItems?.length;

    // Only reset when selection changed from non-empty to empty
    if (prevLength !== undefined && prevLength > 0 && currentLength === 0 && currentSelectionMode !== "none") {
      setSelectionMode("none");
      onSelectionModeChange?.("none");
    }

    // Update ref for next render
    prevCustomSelectedLengthRef.current = currentLength;
  }, [customSelectedItems?.length, currentSelectionMode, onSelectionModeChange]);

  const selectRange = (anchorIndex: number, targetIndex: number, currentSelected: ItemKeyValueType[]) => {
    const start = Math.min(anchorIndex, targetIndex);
    const end = Math.max(anchorIndex, targetIndex);
    const rangeItems = filteredItems.slice(start, end + 1);
    const selectableRangeItems = getSelectableItems(rangeItems);
    const rangeKeys = selectableRangeItems.map((item) => item[itemKey] as ItemKeyValueType);

    const selectedSet = new Set(currentSelected);
    rangeKeys.forEach((key) => selectedSet.add(key));
    return Array.from(selectedSet);
  };

  const toggleSingleItem = (itemKeyValue: ItemKeyValueType, checked: boolean, currentSelected: ItemKeyValueType[]) => {
    if (checked && !currentSelected.includes(itemKeyValue)) {
      return [...currentSelected, itemKeyValue];
    } else if (!checked) {
      return currentSelected.filter((key) => key !== itemKeyValue);
    }
    return currentSelected;
  };

  const handleSelectItem = (
    itemKeyValue: ItemKeyValueType,
    checked: boolean,
    index: number,
    event: ChangeEvent<HTMLInputElement>,
  ) => {
    const currentSelected =
      pageScopedSelection && currentSelectionMode === "all"
        ? getSelectableItems(filteredItems).map((item) => item[itemKey] as ItemKeyValueType)
        : currentSelectedItems;
    const isShiftClick = event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey;
    const canRangeSelect = isShiftClick && lastClickedIndexRef.current !== null && checked;

    let newSelectedItems: ItemKeyValueType[];

    if (canRangeSelect) {
      newSelectedItems = selectRange(lastClickedIndexRef.current!, index, currentSelected);
      lastClickedIndexRef.current = null;
    } else {
      newSelectedItems = toggleSingleItem(itemKeyValue, checked, currentSelected);
      lastClickedIndexRef.current = checked ? index : null;
    }

    if (customSetSelectedItems) {
      customSetSelectedItems(newSelectedItems);
    } else {
      setSelectedItems(newSelectedItems);
    }

    const selectableItems = getSelectableItems(items);
    const allItemsSelected = newSelectedItems.length === selectableItems.length && selectableItems.length > 0;
    const newMode =
      newSelectedItems.length === 0
        ? "none"
        : pageScopedSelection
          ? "subset"
          : allItemsSelected && !hasActiveFilters
            ? "all"
            : "subset";
    setSelectionMode(newMode);
    onSelectionModeChange?.(newMode);
  };

  const allSelected = useMemo(() => {
    // Check if all filtered items are selected (not all items)
    const selectableItems = getSelectableItems(filteredItems);
    const selectableCount = selectableItems.length;
    if (selectableCount === 0) return false;
    if (pageScopedSelection && currentSelectionMode === "all") {
      return selectableCount > 0;
    }

    const currentSelected = currentSelectedItems;
    // Use Set for O(1) lookups instead of O(n) array.includes()
    const selectedSet = new Set<ItemKeyValueType>(currentSelected);
    return selectableItems.every((item) => selectedSet.has(item[itemKey] as ItemKeyValueType));
  }, [currentSelectedItems, currentSelectionMode, filteredItems, getSelectableItems, itemKey, pageScopedSelection]);

  const visibleSelectedCount = useMemo(() => {
    const selectableItems = getSelectableItems(filteredItems);
    if (selectableItems.length === 0) return 0;

    const selectedSet = new Set<ItemKeyValueType>(currentSelectedItems);
    return selectableItems.filter((item) => selectedSet.has(item[itemKey] as ItemKeyValueType)).length;
  }, [currentSelectedItems, filteredItems, getSelectableItems, itemKey]);

  const handleServerFiltering = useCallback(
    (activeFilters: ActiveFilters) => {
      if (isServerSideFiltering) {
        onServerFilter!(activeFilters);
      }
      onFilterChange?.(activeFilters);
    },
    [isServerSideFiltering, onServerFilter, onFilterChange],
  );

  const handleClientFiltering = useCallback(
    (activeFilters: ActiveFilters) => {
      setFilteredItems(items.filter((item) => filterItem === undefined || filterItem(item, activeFilters)));
      onFilterChange?.(activeFilters);
    },
    [filterItem, items, onFilterChange],
  );

  // Determine if Filters component will render (and handle filtering)
  const shouldRenderFilters = !!(filters?.length || headerControls);

  // Update filteredItems when items change
  useEffect(() => {
    if (isServerSideFiltering) {
      // Server-side filtering: items are already filtered by server
      setFilteredItems(items);
    } else if (!shouldRenderFilters && filterItem) {
      // Client-side filtering without Filters component: apply filterItem directly
      setFilteredItems(items.filter((item) => filterItem(item, { buttonFilters: [], dropdownFilters: {} })));
    } else if (!shouldRenderFilters) {
      // No filtering at all: use items directly
      setFilteredItems(items);
    }
    // When shouldRenderFilters is true, Filters component handles filtering via onFilter callback
  }, [items, isServerSideFiltering, shouldRenderFilters, filterItem]);

  // Clear selection anchor when filtered items change to prevent invalid range selection
  useEffect(() => {
    lastClickedIndexRef.current = null;
  }, [filteredItems]);

  // Sync selected items when items list changes
  useEffect(() => {
    const selectableItems = getSelectableItems(items);
    const currentItemKeys = new Set(items.map((item) => item[itemKey] as ItemKeyValueType));
    const currentSelected = currentSelectedItems;

    // In "all" mode, ensure all selectable current items are selected (handles Load More)
    if (currentSelectionMode === "all") {
      const allSelectableItemKeys = selectableItems.map((item) => item[itemKey] as ItemKeyValueType);
      const currentSelectedSet = new Set(currentSelected);
      const needsUpdate = allSelectableItemKeys.some((key) => !currentSelectedSet.has(key));

      if (needsUpdate) {
        if (customSetSelectedItems) {
          customSetSelectedItems(allSelectableItemKeys);
        } else {
          setSelectedItems(allSelectableItemKeys);
        }
      }
      return;
    }

    // When preserveOffPageSelection is enabled, skip cleanup so the parent
    // can manage selections across pages.
    if (preserveOffPageSelection) {
      return;
    }

    if (customSetSelectedItems && customSelectedItems) {
      const newSelectedItems = customSelectedItems.filter((selectedKey) => currentItemKeys.has(selectedKey));
      if (newSelectedItems.length !== customSelectedItems.length) {
        customSetSelectedItems(newSelectedItems);
        const newMode = newSelectedItems.length === 0 ? "none" : "subset";
        setSelectionMode(newMode);
        onSelectionModeChange?.(newMode);
      }
    } else {
      const newSelectedItems = selectedItems.filter((selectedKey) => currentItemKeys.has(selectedKey));
      if (newSelectedItems.length !== selectedItems.length) {
        setSelectedItems(newSelectedItems);
        const newMode = newSelectedItems.length === 0 ? "none" : "subset";
        setSelectionMode(newMode);
        onSelectionModeChange?.(newMode);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    items,
    itemKey,
    customSetSelectedItems,
    onSelectionModeChange,
    isRowDisabled,
    currentSelectedItems,
    currentSelectionMode,
  ]);

  // Infinite scroll: trigger loadMore when scroll reaches near bottom
  useEffect(() => {
    if (!onLoadMore || !hasMore || isLoadingMore) return;

    const trigger = loadMoreTriggerRef.current;
    if (!trigger) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const [entry] = entries;
        if (entry.isIntersecting && hasMore && !isLoadingMore) {
          onLoadMore();
        }
      },
      {
        rootMargin: "200px", // Start loading 200px before reaching the bottom
        threshold: 0,
      },
    );

    observer.observe(trigger);

    return () => {
      observer.disconnect();
    };
  }, [onLoadMore, hasMore, isLoadingMore]);

  const paddingCssVariables = useMemo(() => {
    const style: Record<string, string> = {};
    Object.entries(breakpoints).forEach(([, breakpoint]) => {
      style[`--list-padding-${breakpoint}`] = paddingLeft?.[breakpoint] || "0px";
      style[`--list-padding-right-${breakpoint}`] = paddingRight?.[breakpoint] || "0px";
    });
    return style;
  }, [paddingLeft, paddingRight]);

  const paddingClasses = clsx(
    paddingLeft
      ? [
          "phone:pl-(--list-padding-phone)",
          "tablet:pl-(--list-padding-tablet)",
          "laptop:pl-(--list-padding-laptop)",
          "desktop:pl-(--list-padding-desktop)",
        ]
      : "",
  );

  const rightPaddingClasses = clsx(
    paddingRight
      ? [
          "phone:pr-(--list-padding-right-phone)",
          "tablet:pr-(--list-padding-right-tablet)",
          "laptop:pr-(--list-padding-right-laptop)",
          "desktop:pr-(--list-padding-right-desktop)",
        ]
      : "",
  );

  const firstStickyClasses = clsx(
    baseStickyClassList,
    "tablet:left-0 laptop:left-0 desktop:left-0",
    stickyBgColor,
    paddingClasses,
  );

  const secondStickyClasses = clsx(
    baseStickyClassList,
    stickyBgColor,
    "desktop:left-[calc(var(--list-padding-desktop)+theme(spacing.9))]",
    "laptop:left-[calc(var(--list-padding-laptop)+theme(spacing.9))]",
    "tablet:left-[calc(var(--list-padding-tablet)+theme(spacing.9))]",
  );

  const filtersElement = (filters?.length || headerControls) && (
    <div className={clsx("relative sticky left-0 z-3 flex w-full")}>
      <Filters<ListItem>
        key={JSON.stringify(initialActiveFilters ?? null)}
        className={clsx("gap-4 py-6", paddingClasses)}
        filterItems={filters ?? []}
        filterSize={filterSize}
        items={items}
        onFilter={isServerSideFiltering ? handleServerFiltering : handleClientFiltering}
        isServerSide={isServerSideFiltering}
        headerControls={headerControls}
        initialActiveFilters={initialActiveFilters}
      />
    </div>
  );

  return (
    <div style={paddingCssVariables}>
      {filtersElement}

      {!hideTotal && total !== undefined && (
        <div className="sticky left-0 flex">
          <div className={clsx("sticky left-0 pb-4 text-emphasis-300 text-text-primary-70", paddingClasses)}>
            {total} {total === 1 ? itemName.singular : itemName.plural}
          </div>
        </div>
      )}
      <div className={clsx("flex flex-col", containerClassName)}>
        <div ref={scrollRef} className={clsx({ "overflow-x-auto": overflowContainer })}>
          {!noDataElement || (items && items.length > 0) ? (
            <>
              <div ref={refs.vertical.start} />
              <div className="sticky top-0 flex justify-between">
                <div ref={refs.horizontal.start} />
                <div ref={refs.horizontal.end} />
              </div>
              <table className={clsx("min-w-full table-fixed border-collapse", tableClassName ?? "mb-6")}>
                <thead data-testid="list-header">
                  <tr
                    className={clsx(
                      "sticky top-0 z-2 transition-shadow duration-500",
                      stickyBgColor,
                      stickyState.vertical.isStuck
                        ? "shadow-[0_4px_6px_0_rgba(0,0,0,0.06)]"
                        : "shadow-[0_4px_6px_0_rgba(0,0,0,0)]",
                    )}
                  >
                    {itemSelectable && (
                      <th className={clsx(thClassList, firstStickyClasses, "w-9")} style={paddingCssVariables}>
                        <div className="w-9 truncate overflow-hidden" data-testid="select-all-checkbox">
                          <Checkbox
                            checked={allSelected}
                            partiallyChecked={visibleSelectedCount > 0 && !allSelected}
                            onChange={(e) => handleSelectAll(e.target.checked)}
                          />
                        </div>
                      </th>
                    )}

                    {activeCols.map((row, idx) => {
                      const isSortable = sortableColumns?.has(row);
                      const isCurrentSort = currentSort?.field === row;
                      const sortDirection = isCurrentSort ? currentSort.direction : undefined;
                      const isHovering = hoveredHeader === row;
                      const columnWidthClass = colConfig[row]?.width;

                      const handleHeaderClick = () => {
                        if (!isSortable || !onSort) return;

                        let newDirection: SortDirection;
                        if (isCurrentSort) {
                          // Toggle direction when clicking currently sorted column
                          newDirection = sortDirection === SORT_ASC ? SORT_DESC : SORT_ASC;
                        } else {
                          // Use callback or default to ASC for new columns (matches server default)
                          newDirection = getDefaultSortDirection?.(row) ?? SORT_ASC;
                        }
                        onSort(row, newDirection);
                      };

                      return (
                        <th
                          className={clsx(
                            "pl-2",
                            thClassList,
                            idx === 0 && (itemSelectable ? secondStickyClasses : firstStickyClasses),
                            idx === 0 && columnShadowBaseClassList,
                            idx === 0 && stickyState.horizontal.isStuck && columnShadowVisibleClassList,
                            idx === activeCols.length - 1 && rightPaddingClasses,
                            applyColumnWidthsToCells && columnWidthClass,
                          )}
                          key={idx}
                          style={paddingCssVariables}
                          aria-sort={
                            isCurrentSort ? (sortDirection === SORT_ASC ? "ascending" : "descending") : undefined
                          }
                        >
                          {isSortable ? (
                            <button
                              type="button"
                              className={clsx(
                                "inline-flex w-full cursor-pointer items-center truncate overflow-hidden text-left select-none",
                                applyColumnWidthsToCells ? "box-border" : columnWidthClass,
                              )}
                              onClick={handleHeaderClick}
                              onMouseEnter={() => setHoveredHeader(row)}
                              onMouseLeave={() => setHoveredHeader(null)}
                            >
                              {colTitles[row]}
                              <SortIndicator
                                direction={sortDirection}
                                defaultDirection={getDefaultSortDirection?.(row)}
                                isHovering={isHovering}
                              />
                            </button>
                          ) : (
                            <div
                              className={clsx(
                                "inline-flex w-full items-center truncate overflow-hidden",
                                applyColumnWidthsToCells ? "box-border" : columnWidthClass,
                              )}
                            >
                              {colTitles[row]}
                            </div>
                          )}
                        </th>
                      );
                    })}
                    {actions.length > 0 && (
                      <th className={thClassList}>
                        <div className="w-11 truncate overflow-hidden" />
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody data-testid="list-body">
                  {filteredItems.length > 0
                    ? filteredItems.map((item, i) => {
                        const rowDisabled = isRowDisabled?.(item) ?? false;
                        return (
                          <tr
                            key={item[itemKey] as string | number}
                            className={rowClassList}
                            ref={(el) => itemRef?.(item[itemKey] as ItemKeyValueType, el)}
                            data-testid="list-row"
                          >
                            {itemSelectable && (
                              <td
                                className={clsx(tdClassList, firstStickyClasses, "w-9")}
                                style={paddingCssVariables}
                                data-testid="checkbox"
                              >
                                <div
                                  className={clsx("w-9 truncate overflow-hidden py-4", {
                                    "opacity-50": rowDisabled,
                                  })}
                                >
                                  <Checkbox
                                    checked={
                                      pageScopedSelection && currentSelectionMode === "all"
                                        ? !rowDisabled
                                        : currentSelectedItems.includes(item[itemKey] as ItemKeyValueType)
                                    }
                                    onChange={(e) =>
                                      handleSelectItem(item[itemKey] as ItemKeyValueType, e.target.checked, i, e)
                                    }
                                    disabled={rowDisabled}
                                  />
                                </div>
                              </td>
                            )}

                            {activeCols.map((row, j) => {
                              const isExempt = columnsExemptFromDisabledStyling?.has(row) ?? false;
                              const columnWidthClass = colConfig[row]?.width;
                              return (
                                <td
                                  className={clsx(
                                    tdClassList,
                                    j === 0 && (itemSelectable ? secondStickyClasses : firstStickyClasses),
                                    j === 0 && columnShadowBaseClassList,
                                    j === 0 && stickyState.horizontal.isStuck && columnShadowVisibleClassList,
                                    applyColumnWidthsToCells && columnWidthClass,
                                    j === activeCols.length - 1 && rightPaddingClasses,
                                  )}
                                  key={j}
                                  style={paddingCssVariables}
                                  data-testid={row}
                                >
                                  <div
                                    className={clsx(
                                      "truncate overflow-hidden",
                                      tdPaddingClassList,
                                      applyColumnWidthsToCells ? "box-border w-full" : columnWidthClass,
                                      {
                                        "opacity-50": rowDisabled && !isExempt,
                                      },
                                      {
                                        "text-core-primary-50": disabled,
                                      },
                                    )}
                                  >
                                    {colConfig[row]?.component
                                      ? colConfig[row].component(item, currentSelectedItems)
                                      : typeof item === "object" && item !== null && row in item
                                        ? ((item as Record<string, unknown>)[row as string] as ReactNode)
                                        : null}
                                  </div>
                                </td>
                              );
                            })}
                            {actions.length == 1 ? (
                              <td
                                className={clsx(tdClassList, {
                                  "opacity-50": rowDisabled,
                                })}
                                data-testid="action"
                              >
                                <div className={clsx("flex justify-end", tdPaddingClassList)}>
                                  <Button
                                    variant={variants.secondary}
                                    size={sizes.compact}
                                    text={actions[0].title}
                                    onClick={() => actions[0].actionHandler(item)}
                                    disabled={rowDisabled}
                                  />
                                </div>
                              </td>
                            ) : actions.length > 1 ? (
                              <td
                                className={clsx(tdClassList, {
                                  "opacity-50": rowDisabled,
                                })}
                                data-testid="action"
                              >
                                <div className={clsx("w-11", tdPaddingClassList)}>
                                  <PopoverProvider>
                                    <ListActions<ListItem> item={item} actions={actions} disabled={rowDisabled} />
                                  </PopoverProvider>
                                </div>
                              </td>
                            ) : null}
                          </tr>
                        );
                      })
                    : emptyStateRow && (
                        <tr data-testid="list-empty-row">
                          <td colSpan={activeCols.length + (itemSelectable ? 1 : 0) + (actions.length > 0 ? 1 : 0)}>
                            {emptyStateRow}
                          </td>
                        </tr>
                      )}
                </tbody>
              </table>
              {/* Infinite scroll trigger element */}
              {onLoadMore && hasMore && (
                <div ref={loadMoreTriggerRef} className="flex justify-center py-6">
                  {isLoadingMore && <ProgressCircular indeterminate />}
                </div>
              )}
              {footerContent}
              <div ref={refs.vertical.end} />
            </>
          ) : (
            noDataElement
          )}
        </div>
        {renderActionBar && (
          <div className="w-full">
            {renderActionBar(currentSelectedItems, clearSelection, currentSelectionMode, totalSelectable)}
          </div>
        )}
      </div>
    </div>
  );
};

export default List;
export type { SelectionMode };
