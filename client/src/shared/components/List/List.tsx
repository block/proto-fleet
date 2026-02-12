import { ChangeEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Filters from "@/shared/components/List/Filters";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import ListActions from "@/shared/components/List/ListActions";
import { ColConfig, ColTitles, ListAction, SortDirection } from "@/shared/components/List/types";
import { PopoverProvider } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SortIndicator from "@/shared/components/SortIndicator";
import { Breakpoint, breakpoints } from "@/shared/constants/breakpoints";
import { useStickyState } from "@/shared/hooks/useStickyState";

type SelectionMode = "none" | "all" | "subset";

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
  customSelectedItems?: ItemKeyValueType[];
  customSetSelectedItems?: (selected: ItemKeyValueType[]) => void;
  disabled?: boolean;
  actions?: ListAction<ListItem>[];
  noDataElement?: ReactNode;
  renderActionBar?: (
    selectedItems: ItemKeyValueType[],
    clearSelection: () => void,
    selectionMode: SelectionMode,
    totalSelectable?: number,
  ) => ReactNode;
  containerClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
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
  /**
   * Callback when selection mode changes.
   * Called with "all" when Select All is clicked with no filters,
   * "subset" for individual selections or Select All with filters,
   * "none" when selection is cleared.
   */
  onSelectionModeChange?: (mode: SelectionMode) => void;
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
};

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
  initialActiveFilters,
  renderActionBar,
  containerClassName = "",
  paddingLeft,
  overflowContainer = true,
  stickyBgColor = "bg-surface-base",
  total,
  totalDisabled = 0,
  itemName = { singular: "item", plural: "items" },
  itemRef,
  hasActiveFilters = false,
  onFilterChange,
  onSelectionModeChange,
  onLoadMore,
  hasMore = false,
  isLoadingMore = false,
  isRowDisabled,
  columnsExemptFromDisabledStyling,
  sortableColumns,
  currentSort,
  onSort,
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
      customSetSelectedItems ? customSetSelectedItems(selection) : setSelectedItems(selection);
      // If we're selecting filtered items, it's a subset (unless all items match the filter)
      const allItemsMatchFilter = filteredItems.length === items.length;
      const newMode = hasActiveFilters || !allItemsMatchFilter ? "subset" : "all";
      setSelectionMode(newMode);
      onSelectionModeChange?.(newMode);
    } else {
      clearSelection();
    }
  };

  // Clear selection anchor when bulk selection changes (Select All or Clear Selection)
  useEffect(() => {
    if (selectionMode === "all" || selectionMode === "none") {
      lastClickedIndexRef.current = null;
    }
  }, [selectionMode]);

  // Reset selectionMode when customSelectedItems is externally changed from non-empty to empty
  // This handles "Select none" from external controls like ModalSelectAllFooter
  useEffect(() => {
    const prevLength = prevCustomSelectedLengthRef.current;
    const currentLength = customSelectedItems?.length;

    // Only reset when selection changed from non-empty to empty
    if (prevLength !== undefined && prevLength > 0 && currentLength === 0 && selectionMode !== "none") {
      setSelectionMode("none");
      onSelectionModeChange?.("none");
    }

    // Update ref for next render
    prevCustomSelectedLengthRef.current = currentLength;
  }, [customSelectedItems?.length, selectionMode, onSelectionModeChange]);

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
    const currentSelected = customSelectedItems ?? selectedItems;
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
    const newMode = newSelectedItems.length === 0 ? "none" : allItemsSelected && !hasActiveFilters ? "all" : "subset";
    setSelectionMode(newMode);
    onSelectionModeChange?.(newMode);
  };

  const allSelected = useMemo(() => {
    // Check if all filtered items are selected (not all items)
    const selectableItems = getSelectableItems(filteredItems);
    const selectableCount = selectableItems.length;
    if (selectableCount === 0) return false;
    const currentSelected = customSelectedItems ?? selectedItems;
    // Use Set for O(1) lookups instead of O(n) array.includes()
    const selectedSet = new Set<ItemKeyValueType>(currentSelected);
    return selectableItems.every((item) => selectedSet.has(item[itemKey] as ItemKeyValueType));
  }, [selectedItems, filteredItems, customSelectedItems, getSelectableItems, itemKey]);

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

  // Update filteredItems when items change (for server-side filtering)
  useEffect(() => {
    if (isServerSideFiltering) {
      setFilteredItems(items);
    }
  }, [items, isServerSideFiltering]);

  // Clear selection anchor when filtered items change to prevent invalid range selection
  useEffect(() => {
    lastClickedIndexRef.current = null;
  }, [filteredItems]);

  // Sync selected items when items list changes
  useEffect(() => {
    const selectableItems = getSelectableItems(items);
    const currentItemKeys = new Set(items.map((item) => item[itemKey] as ItemKeyValueType));
    const currentSelected = customSelectedItems ?? selectedItems;

    // In "all" mode, ensure all selectable current items are selected (handles Load More)
    if (selectionMode === "all") {
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

    // In "subset" or "none" mode, clean up stale selections (items that no longer exist)
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
  }, [items, itemKey, customSetSelectedItems, onSelectionModeChange, isRowDisabled]);

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
      style[`--list-padding-${breakpoint}`] = paddingLeft?.[breakpoint] || "0px"; // Fallback to 0 if not defined
    });
    return style;
  }, [paddingLeft]);

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

  return (
    <div style={paddingCssVariables}>
      <div className={clsx("relative z-3 flex")}>
        <Filters<ListItem>
          className={clsx("gap-4 py-10", paddingClasses)}
          filterItems={filters ?? []}
          filterSize={filterSize}
          items={items}
          onFilter={isServerSideFiltering ? handleServerFiltering : handleClientFiltering}
          isServerSide={isServerSideFiltering}
          headerControls={headerControls}
          initialActiveFilters={initialActiveFilters}
        />
      </div>

      {total !== undefined && (
        <div className="flex">
          <div className={clsx("sticky left-0 pb-4 text-emphasis-300 text-text-primary-70", paddingClasses)}>
            {total} {total === 1 ? itemName.singular : itemName.plural}
          </div>
        </div>
      )}
      <div className={clsx("flex flex-col", containerClassName)}>
        <div className={clsx({ "overflow-x-auto": overflowContainer })}>
          {!noDataElement || (items && items.length > 0) ? (
            <>
              <div ref={refs.vertical.start} />
              <div className="sticky top-0 flex justify-between">
                <div ref={refs.horizontal.start} />
                <div ref={refs.horizontal.end} />
              </div>
              <table className="mb-6 min-w-full table-fixed border-collapse">
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
                        <div className="w-9 truncate overflow-hidden">
                          <Checkbox
                            checked={allSelected}
                            partiallyChecked={
                              (selectedItems.length > 0 && selectedItems.length < filteredItems.length) ||
                              (customSelectedItems &&
                                customSelectedItems.length > 0 &&
                                customSelectedItems.length < filteredItems.length)
                            }
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

                      const handleHeaderClick = () => {
                        if (!isSortable || !onSort) return;

                        const newDirection: SortDirection = !isCurrentSort || sortDirection === "asc" ? "desc" : "asc";
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
                          )}
                          key={idx}
                          style={paddingCssVariables}
                          aria-sort={isCurrentSort ? (sortDirection === "asc" ? "ascending" : "descending") : undefined}
                        >
                          {isSortable ? (
                            <button
                              type="button"
                              className={clsx(
                                "inline-flex w-full cursor-pointer items-center truncate overflow-hidden text-left select-none",
                                colConfig[row]?.width,
                              )}
                              onClick={handleHeaderClick}
                              onMouseEnter={() => setHoveredHeader(row)}
                              onMouseLeave={() => setHoveredHeader(null)}
                            >
                              {colTitles[row]}
                              <SortIndicator direction={sortDirection} isHovering={isHovering} />
                            </button>
                          ) : (
                            <div
                              className={clsx(
                                "inline-flex items-center truncate overflow-hidden",
                                colConfig[row]?.width,
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
                  {filteredItems.map((item, i) => {
                    const rowDisabled = isRowDisabled?.(item) ?? false;
                    return (
                      <tr
                        key={i}
                        className={rowClassList}
                        ref={(el) => itemRef?.(item[itemKey] as ItemKeyValueType, el)}
                      >
                        {itemSelectable && (
                          <td
                            className={clsx(tdClassList, firstStickyClasses, "w-9", {
                              "opacity-50": rowDisabled,
                            })}
                            style={paddingCssVariables}
                            data-testid="checkbox"
                          >
                            <div className={clsx("w-9 truncate overflow-hidden", "py-4")}>
                              <Checkbox
                                checked={
                                  customSelectedItems?.includes(item[itemKey] as ItemKeyValueType) ||
                                  selectedItems.includes(item[itemKey] as ItemKeyValueType)
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
                          return (
                            <td
                              className={clsx(
                                tdClassList,
                                j === 0 && (itemSelectable ? secondStickyClasses : firstStickyClasses),
                                j === 0 && columnShadowBaseClassList,
                                j === 0 && stickyState.horizontal.isStuck && columnShadowVisibleClassList,
                                {
                                  "opacity-50": rowDisabled && !isExempt,
                                },
                              )}
                              key={j}
                              style={paddingCssVariables}
                              data-testid={row}
                            >
                              <div
                                className={clsx("truncate overflow-hidden", tdPaddingClassList, colConfig[row]?.width, {
                                  "text-core-primary-50": disabled,
                                })}
                              >
                                {colConfig[row]?.component
                                  ? colConfig[row].component(item, selectedItems)
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
                  })}
                </tbody>
              </table>
              {/* Infinite scroll trigger element */}
              {onLoadMore && hasMore && (
                <div ref={loadMoreTriggerRef} className="flex justify-center py-6">
                  {isLoadingMore && <ProgressCircular indeterminate />}
                </div>
              )}
              <div ref={refs.vertical.end} />
            </>
          ) : (
            noDataElement
          )}
        </div>
        {renderActionBar && (
          <div className="w-full">{renderActionBar(selectedItems, clearSelection, selectionMode, totalSelectable)}</div>
        )}
      </div>
    </div>
  );
};

export default List;
export type { SelectionMode };
