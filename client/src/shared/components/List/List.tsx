import { ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import { Ellipsis } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Filters from "@/shared/components/List/Filters";
import {
  ActiveFilters,
  FilterItem,
} from "@/shared/components/List/Filters/types";
import ListActions from "@/shared/components/List/ListActions";
import {
  ColConfig,
  ColTitles,
  ListAction,
} from "@/shared/components/List/types";
import { PopoverProvider } from "@/shared/components/Popover";
import { Breakpoint, breakpoints } from "@/shared/constants/breakpoints";
import { useStickyState } from "@/shared/hooks/useStickyState";

type ListProps<ListItem, ItemKeyValueType> = {
  activeCols: (keyof ListItem)[];
  colTitles: ColTitles<keyof ListItem>;
  colConfig: ColConfig<ListItem, ItemKeyValueType>;
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

const List = <ListItem, ItemKeyValueType>({
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
  renderActionBar,
  containerClassName = "",
  paddingLeft,
  overflowContainer = true,
  stickyBgColor = "bg-surface-base",
  total,
  itemName = { singular: "item", plural: "items" },
}: ListProps<ListItem, ItemKeyValueType>) => {
  const { refs, stickyState } = useStickyState();

  const [selectedItems, setSelectedItems] =
    useState<ItemKeyValueType[]>(initialSelectedItems);
  const [filteredItems, setFilteredItems] = useState<ListItem[]>(items);
  const isServerSideFiltering = useMemo(
    () => onServerFilter !== undefined,
    [onServerFilter],
  );

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      const selection = items.map((item) => item[itemKey] as ItemKeyValueType);
      customSetSelectedItems
        ? customSetSelectedItems(selection)
        : setSelectedItems(selection);
    } else {
      customSetSelectedItems
        ? customSetSelectedItems([])
        : setSelectedItems([]);
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

    if (customSetSelectedItems && customSelectedItems) {
      const newSelectedItems = cb(customSelectedItems);
      customSetSelectedItems(newSelectedItems);
    } else {
      setSelectedItems(cb);
    }
  };

  const allSelected = useMemo(() => {
    return (
      items.length > 0 &&
      (customSelectedItems?.length === items.length ||
        selectedItems.length === items.length)
    );
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
      setFilteredItems(
        items.filter(
          (item) => filterItem === undefined || filterItem(item, activeFilters),
        ),
      );
    },
    [filterItem, items],
  );

  // Update filteredItems when items change (for server-side filtering)
  useEffect(() => {
    if (isServerSideFiltering) {
      setFilteredItems(items);
    }
  }, [items, isServerSideFiltering]);

  // Clear selected items that are no longer in the current items list
  useEffect(() => {
    const currentItemKeys = new Set(
      items.map((item) => item[itemKey] as ItemKeyValueType),
    );

    if (customSetSelectedItems && customSelectedItems) {
      const newSelectedItems = customSelectedItems.filter((selectedKey) =>
        currentItemKeys.has(selectedKey),
      );
      if (newSelectedItems.length !== customSelectedItems.length) {
        customSetSelectedItems(newSelectedItems);
      }
    } else {
      setSelectedItems((prevSelected) => {
        const newSelectedItems = prevSelected.filter((selectedKey) =>
          currentItemKeys.has(selectedKey),
        );
        return newSelectedItems.length !== prevSelected.length
          ? newSelectedItems
          : prevSelected;
      });
    }
  }, [items, itemKey, customSetSelectedItems, customSelectedItems]);

  const paddingCssVariables = useMemo(() => {
    const style: Record<string, string> = {};
    Object.entries(breakpoints).forEach(([, breakpoint]) => {
      style[`--list-padding-${breakpoint}`] =
        paddingLeft?.[breakpoint] || "0px"; // Fallback to 0 if not defined
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
    "left-0",
    stickyBgColor,
    paddingClasses,
  );

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
          onFilter={
            isServerSideFiltering
              ? handleServerFiltering
              : handleClientFiltering
          }
          isServerSide={isServerSideFiltering}
          headerControls={headerControls}
        />
      </div>

      {total !== undefined && (
        <div className="flex">
          <div
            className={clsx(
              "sticky left-0 pb-4 text-emphasis-300 text-text-primary-70",
              paddingClasses,
            )}
          >
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
                    className={clsx("sticky top-0 z-2", stickyBgColor, {
                      "shadow-[0_0_6px_6px_rgba(0,0,0,0.06)]":
                        stickyState.vertical.isStuck,
                    })}
                  >
                    {itemSelectable && (
                      <th
                        className={clsx(thClassList, firstStickyClasses, "w-9")}
                        style={paddingCssVariables}
                      >
                        <div className="w-9 truncate overflow-hidden">
                          <Checkbox
                            checked={allSelected}
                            partiallyChecked={
                              (selectedItems.length > 0 &&
                                selectedItems.length < filteredItems.length) ||
                              (customSelectedItems &&
                                customSelectedItems.length > 0 &&
                                customSelectedItems.length <
                                  filteredItems.length)
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
                          idx === 0 &&
                            (itemSelectable
                              ? secondStickyClasses
                              : firstStickyClasses),
                          idx === 0 &&
                            stickyState.horizontal.isStuck &&
                            columnShadowClassList,
                        )}
                        key={idx}
                        style={paddingCssVariables}
                      >
                        <div
                          className={clsx(
                            "truncate overflow-hidden",
                            colConfig[row]?.width,
                          )}
                        >
                          {colTitles[row]}
                        </div>
                      </th>
                    ))}
                    {actions.length > 0 && (
                      <th className={thClassList}>
                        <div className="w-11 truncate overflow-hidden">
                          {actions.length > 1 && (
                            <button className="align-middle text-text-primary-30 hover:cursor-pointer hover:text-text-primary-50">
                              <Ellipsis />
                            </button>
                          )}
                        </div>
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody data-testid="list-body">
                  {filteredItems.map((item, i) => (
                    <tr key={i} className={rowClassList}>
                      {itemSelectable && (
                        <td
                          className={clsx(
                            tdClassList,
                            firstStickyClasses,
                            "w-9",
                          )}
                          style={paddingCssVariables}
                        >
                          <div
                            className={clsx(
                              "w-9 truncate overflow-hidden",
                              "py-4",
                            )}
                          >
                            <Checkbox
                              checked={
                                customSelectedItems?.includes(
                                  item[itemKey] as ItemKeyValueType,
                                ) ||
                                selectedItems.includes(
                                  item[itemKey] as ItemKeyValueType,
                                )
                              }
                              onChange={(e) =>
                                handleSelectItem(
                                  item[itemKey] as ItemKeyValueType,
                                  e.target.checked,
                                )
                              }
                            />
                          </div>
                        </td>
                      )}

                      {activeCols.map((row, j) => (
                        <td
                          className={clsx(
                            tdClassList,
                            j === 0 &&
                              (itemSelectable
                                ? secondStickyClasses
                                : firstStickyClasses),
                            j === 0 &&
                              stickyState.horizontal.isStuck &&
                              columnShadowClassList,
                          )}
                          key={j}
                          style={paddingCssVariables}
                        >
                          <div
                            className={clsx(
                              "truncate overflow-hidden",
                              tdPaddingClassList,
                              colConfig[row]?.width,
                              {
                                "text-core-primary-50": disabled,
                              },
                            )}
                          >
                            {colConfig[row]?.component
                              ? colConfig[row].component(item, selectedItems)
                              : (item[row] as ReactNode)}
                          </div>
                        </td>
                      ))}
                      {actions.length == 1 ? (
                        <td className={tdClassList}>
                          <div
                            className={clsx(
                              "flex justify-end",
                              tdPaddingClassList,
                            )}
                          >
                            <Button
                              variant={variants.secondary}
                              size={sizes.compact}
                              text={actions[0].title}
                              onClick={() => actions[0].actionHandler(item)}
                            />
                          </div>
                        </td>
                      ) : actions.length > 1 ? (
                        <td className={tdClassList}>
                          <div className={clsx("w-11", tdPaddingClassList)}>
                            <PopoverProvider>
                              <ListActions<ListItem>
                                item={item}
                                actions={actions}
                              />
                            </PopoverProvider>
                          </div>
                        </td>
                      ) : null}
                    </tr>
                  ))}
                </tbody>
              </table>
              <div ref={refs.vertical.end} />
            </>
          ) : (
            noDataElement
          )}
        </div>
        {renderActionBar && (
          <div className="w-full">
            {renderActionBar(selectedItems, () => handleSelectAll(false))}
          </div>
        )}
      </div>
    </div>
  );
};

export default List;
