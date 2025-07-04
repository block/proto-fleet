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
  items: ListItem[];
  itemKey: keyof ListItem;
  itemSelectable?: boolean;
  initialSelectedItems?: ItemKeyValueType[];
  customSelectedItems?: ItemKeyValueType[];
  customSetSelectedItems?: (selected: ItemKeyValueType[]) => void;
  disabled?: boolean;
  actions?: ListAction<ListItem>[];
  noDataElement?: ReactNode;
  renderActionBar?: (selectedItems: ItemKeyValueType[]) => ReactNode;
  containerClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
};

const cellClassList = "text-left pl-2";
const rowClassList = "border-b border-border-5";
const thClassList = cellClassList + " py-3 text-emphasis-300 text-text-primary";
const baseStickyClassList = "sticky z-1 bg-surface-base";
const tdClassList = cellClassList + " py-4 text-300";
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
  containerClassName = "max-h-screen",
  paddingLeft,
  overflowContainer = true,
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

  const paddingCssVariables = useMemo(() => {
    const style: Record<string, string> = {};
    Object.entries(breakpoints).forEach(([, breakpoint]) => {
      style[`--list-padding-${breakpoint}`] =
        paddingLeft?.[breakpoint] || "0px"; // Fallback to 0 if not defined
    });
    return style;
  }, [paddingLeft]);

  const bodyClasses = useMemo(() => {
    const classes = [];
    if (overflowContainer) {
      classes.push("overflow-x-auto");
    }

    if (paddingLeft === undefined) return classes;

    // cannot create classes dynamically because Tailwind wouldn't include them in the bundle and they wouldn't work
    classes.push(
      ...[
        "phone:w-[calc(100%+var(--list-padding-phone)*2-theme(spacing.1))]",
        "phone:px-(--list-padding-phone)",
        "phone:-translate-x-(--list-padding-phone)",
        "tablet:w-[calc(100%+var(--list-padding-tablet)*2-theme(spacing.1))]",
        "tablet:px-(--list-padding-tablet)",
        "tablet:-translate-x-(--list-padding-tablet)",
        "laptop:w-[calc(100%+var(--list-padding-laptop)*2-theme(spacing.1))]",
        "laptop:px-(--list-padding-laptop)",
        "laptop:-translate-x-(--list-padding-laptop)",
        "desktop:w-[calc(100%+var(--list-padding-desktop)*2-theme(spacing.1))]",
        "desktop:px-(--list-padding-desktop)",
        "desktop:-translate-x-(--list-padding-desktop)",
      ],
    );
    return classes;
  }, [overflowContainer, paddingLeft]);

  const firstStickyClasses = clsx(
    baseStickyClassList,
    paddingLeft
      ? [
          "phone:-left-(--list-padding-phone)",
          "tablet:-left-(--list-padding-tablet)",
          "laptop:-left-(--list-padding-laptop)",
          "desktop:-left-(--list-padding-desktop)",
        ]
      : "left-0",
  );

  const secondStickyClasses = clsx(
    baseStickyClassList,
    paddingLeft
      ? [
          "phone:-left-[calc(theme(spacing.11)*-1+var(--list-padding-phone))]",
          "tablet:-left-[calc(theme(spacing.11)*-1+var(--list-padding-tablet))]",
          "laptop:-left-[calc(theme(spacing.11)*-1+var(--list-padding-laptop))]",
          "desktop:-left-[calc(theme(spacing.11)*-1+var(--list-padding-desktop))]",
        ]
      : "left-11",
  );

  return (
    <div className={clsx("flex flex-col", containerClassName)}>
      <Filters<ListItem>
        className="gap-4 py-4"
        filterItems={filters ?? []}
        filterSize={filterSize}
        items={items}
        onFilter={
          isServerSideFiltering ? handleServerFiltering : handleClientFiltering
        }
        isServerSide={isServerSideFiltering}
      />
      <div className={clsx(bodyClasses)} style={paddingCssVariables}>
        {!noDataElement || (items && items.length > 0) ? (
          <div className="relative min-w-fit">
            <div ref={refs.vertical.start} />
            <div className="sticky top-0 flex justify-between">
              <div ref={refs.horizontal.start} />
              <div ref={refs.horizontal.end} />
            </div>
            <table className="mb-6 min-w-full table-fixed border-collapse">
              <thead data-testid="list-header">
                <tr
                  className={clsx("sticky top-0 z-2 bg-surface-base", {
                    "shadow-[0_10px_8px_-6px_rgba(0,0,0,0.06)]":
                      stickyState.vertical.isStuck,
                  })}
                >
                  {itemSelectable && (
                    <th
                      className={clsx(thClassList, firstStickyClasses)}
                      style={paddingCssVariables}
                    >
                      <div className="w-9 truncate overflow-hidden">
                        <Checkbox
                          checked={allSelected}
                          onChange={(e) => handleSelectAll(e.target.checked)}
                        />
                      </div>
                    </th>
                  )}

                  {activeCols.map((row, idx) => (
                    <th
                      className={clsx(
                        thClassList,
                        idx === 0 &&
                          (itemSelectable
                            ? secondStickyClasses
                            : firstStickyClasses),
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
                        className={clsx(tdClassList, firstStickyClasses)}
                        style={paddingCssVariables}
                      >
                        <div className="w-9 truncate overflow-hidden">
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
                        <div className="flex justify-end">
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
                        <div className="w-11">
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
          </div>
        ) : (
          noDataElement
        )}
        {renderActionBar && (
          <div className="w-full">{renderActionBar(selectedItems)}</div>
        )}
      </div>
    </div>
  );
};

export default List;
