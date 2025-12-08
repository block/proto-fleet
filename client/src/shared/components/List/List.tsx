import { ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Filters from "@/shared/components/List/Filters";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import ListActions from "@/shared/components/List/ListActions";
import { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { PopoverProvider } from "@/shared/components/Popover";
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
  ) => ReactNode;
  containerClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  stickyBgColor?: string;
  total?: number;
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
   * Callback when selection mode changes.
   * Called with "all" when Select All is clicked with no filters,
   * "subset" for individual selections or Select All with filters,
   * "none" when selection is cleared.
   */
  onSelectionModeChange?: (mode: SelectionMode) => void;
};

const cellClassList = "text-left";
const rowClassList = "border-b border-border-5";
const thClassList = cellClassList + " py-3 text-emphasis-300 text-text-primary";
const baseStickyClassList = "sticky z-1";
const tdClassList = "text-left text-300";
const tdPaddingClassList = "px-2 py-4";
// use after element for shadow
const columnShadowClassList =
  "after:content-[''] after:absolute after:top-0 after:right-[-6px] after:bottom-[-1px] after:w-[9px] after:bg-[linear-gradient(90deg,rgba(0,0,0,0.06)0%,rgba(0,0,0,0)100%)]";

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
  itemName = { singular: "item", plural: "items" },
  itemRef,
  hasActiveFilters = false,
  onSelectionModeChange,
}: ListProps<ListItem, ItemKeyValueType, ColKey>) => {
  const { refs, stickyState } = useStickyState();

  const [selectedItems, setSelectedItems] = useState<ItemKeyValueType[]>(initialSelectedItems);
  const [filteredItems, setFilteredItems] = useState<ListItem[]>(items);
  const [selectionMode, setSelectionMode] = useState<SelectionMode>("none");
  const isServerSideFiltering = useMemo(() => onServerFilter !== undefined, [onServerFilter]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      const selection = items.map((item) => item[itemKey] as ItemKeyValueType);
      customSetSelectedItems ? customSetSelectedItems(selection) : setSelectedItems(selection);
      const newMode = hasActiveFilters ? "subset" : "all";
      setSelectionMode(newMode);
      onSelectionModeChange?.(newMode);
    } else {
      customSetSelectedItems ? customSetSelectedItems([]) : setSelectedItems([]);
      setSelectionMode("none");
      onSelectionModeChange?.("none");
    }
  };

  const handleSelectItem = (itemKey: ItemKeyValueType, checked: boolean) => {
    const cb = (prev: ItemKeyValueType[]) => {
      if (checked && !prev.includes(itemKey)) {
        return [...prev, itemKey];
      } else if (!checked) {
        return prev.filter((addr) => addr !== itemKey);
      }
      return prev;
    };

    let newSelectedItems: ItemKeyValueType[];
    if (customSetSelectedItems && customSelectedItems) {
      newSelectedItems = cb(customSelectedItems);
      customSetSelectedItems(newSelectedItems);
    } else {
      newSelectedItems = cb(selectedItems);
      setSelectedItems(newSelectedItems);
    }

    const newMode = newSelectedItems.length === 0 ? "none" : "subset";
    setSelectionMode(newMode);
    onSelectionModeChange?.(newMode);
  };

  const allSelected = useMemo(() => {
    return items.length > 0 && (customSelectedItems?.length === items.length || selectedItems.length === items.length);
  }, [selectedItems, items, customSelectedItems]);

  const handleServerFiltering = useCallback(
    (activeFilters: ActiveFilters) => {
      if (isServerSideFiltering) {
        onServerFilter!(activeFilters);
      }
    },
    [isServerSideFiltering, onServerFilter],
  );

  const handleClientFiltering = useCallback(
    (activeFilters: ActiveFilters) => {
      setFilteredItems(items.filter((item) => filterItem === undefined || filterItem(item, activeFilters)));
    },
    [filterItem, items],
  );

  // Update filteredItems when items change (for server-side filtering)
  useEffect(() => {
    if (isServerSideFiltering) {
      setFilteredItems(items);
    }
  }, [items, isServerSideFiltering]);

  // Sync selected items when items list changes
  useEffect(() => {
    const currentItemKeys = new Set(items.map((item) => item[itemKey] as ItemKeyValueType));
    const currentSelected = customSelectedItems ?? selectedItems;

    // In "all" mode, ensure all current items are selected (handles Load More)
    if (selectionMode === "all") {
      // Only update if there are items not yet selected
      const allSelected = items.every((item) => currentSelected.includes(item[itemKey] as ItemKeyValueType));
      if (!allSelected) {
        const allCurrentItems = items.map((item) => item[itemKey] as ItemKeyValueType);
        if (customSetSelectedItems) {
          customSetSelectedItems(allCurrentItems);
        } else {
          setSelectedItems(allCurrentItems);
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
  }, [
    items,
    itemKey,
    customSetSelectedItems,
    customSelectedItems,
    selectedItems,
    selectionMode,
    onSelectionModeChange,
  ]);

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

  const firstStickyClasses = clsx(baseStickyClassList, "left-0", stickyBgColor, paddingClasses);

  const secondStickyClasses = clsx(
    baseStickyClassList,
    stickyBgColor,
    "desktop:left-[calc(var(--list-padding-desktop)+theme(spacing.9))]",
    "laptop:left-[calc(var(--list-padding-laptop)+theme(spacing.9))]",
    "tablet:left-[calc(var(--list-padding-tablet)+theme(spacing.9))]",
    "phone:left-[calc(var(--list-padding-phone)+theme(spacing.9))]",
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
                {/* eslint-disable-next-line react-hooks/refs */}
                <div ref={refs.horizontal.start} />
                {/* eslint-disable-next-line react-hooks/refs */}
                <div ref={refs.horizontal.end} />
              </div>
              <table className="mb-6 min-w-full table-fixed border-collapse">
                <thead data-testid="list-header">
                  <tr
                    className={clsx("sticky top-0 z-2", stickyBgColor, {
                      "shadow-[0_0_6px_6px_rgba(0,0,0,0.06)]": stickyState.vertical.isStuck,
                    })}
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

                    {activeCols.map((row, idx) => (
                      <th
                        className={clsx(
                          "pl-2",
                          thClassList,
                          idx === 0 && (itemSelectable ? secondStickyClasses : firstStickyClasses),
                          idx === 0 && stickyState.horizontal.isStuck && columnShadowClassList,
                        )}
                        key={idx}
                        style={paddingCssVariables}
                      >
                        <div className={clsx("truncate overflow-hidden", colConfig[row]?.width)}>{colTitles[row]}</div>
                      </th>
                    ))}
                    {actions.length > 0 && (
                      <th className={thClassList}>
                        <div className="w-11 truncate overflow-hidden" />
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody data-testid="list-body">
                  {filteredItems.map((item, i) => (
                    <tr key={i} className={rowClassList} ref={(el) => itemRef?.(item[itemKey] as ItemKeyValueType, el)}>
                      {itemSelectable && (
                        <td
                          className={clsx(tdClassList, firstStickyClasses, "w-9")}
                          style={paddingCssVariables}
                          data-testid="checkbox"
                        >
                          <div className={clsx("w-9 truncate overflow-hidden", "py-4")}>
                            <Checkbox
                              checked={
                                customSelectedItems?.includes(item[itemKey] as ItemKeyValueType) ||
                                selectedItems.includes(item[itemKey] as ItemKeyValueType)
                              }
                              onChange={(e) => handleSelectItem(item[itemKey] as ItemKeyValueType, e.target.checked)}
                            />
                          </div>
                        </td>
                      )}

                      {activeCols.map((row, j) => (
                        <td
                          className={clsx(
                            tdClassList,
                            j === 0 && (itemSelectable ? secondStickyClasses : firstStickyClasses),
                            j === 0 && stickyState.horizontal.isStuck && columnShadowClassList,
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
                      ))}
                      {actions.length == 1 ? (
                        <td className={tdClassList} data-testid="action">
                          <div className={clsx("flex justify-end", tdPaddingClassList)}>
                            <Button
                              variant={variants.secondary}
                              size={sizes.compact}
                              text={actions[0].title}
                              onClick={() => actions[0].actionHandler(item)}
                            />
                          </div>
                        </td>
                      ) : actions.length > 1 ? (
                        <td className={tdClassList} data-testid="action">
                          <div className={clsx("w-11", tdPaddingClassList)}>
                            <PopoverProvider>
                              <ListActions<ListItem> item={item} actions={actions} />
                            </PopoverProvider>
                          </div>
                        </td>
                      ) : null}
                    </tr>
                  ))}
                </tbody>
              </table>
              {/* eslint-disable-next-line react-hooks/refs */}
              <div ref={refs.vertical.end} />
            </>
          ) : (
            noDataElement
          )}
        </div>
        {renderActionBar && (
          <div className="w-full">{renderActionBar(selectedItems, () => handleSelectAll(false), selectionMode)}</div>
        )}
      </div>
    </div>
  );
};

export default List;
export type { SelectionMode };
