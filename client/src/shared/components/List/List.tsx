import { Key, ReactNode, useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import { Ellipsis } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import { defaultListFilter } from "@/shared/components/List/constants";
import Filters from "@/shared/components/List/Filters";
import { FilterItem } from "@/shared/components/List/Filters/types";
import ListActions from "@/shared/components/List/ListActions";
import {
  ColConfig,
  ColTitles,
  ListAction,
} from "@/shared/components/List/types";
import { PopoverProvider } from "@/shared/components/Popover";

type ListProps<ListItem, ItemKeyValueType, FilterType extends Key> = {
  activeCols: (keyof ListItem)[];
  colTitles: ColTitles<keyof ListItem>;
  colConfig: ColConfig<ListItem, ItemKeyValueType>;
  filters?: FilterItem<FilterType>[];
  filterItem?: (
    item: ListItem,
    activeButtonFilters: (FilterType | typeof defaultListFilter)[],
    dropdownFilters?: Record<string, string>,
  ) => boolean;
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
  includeOptions?: boolean;
};

const cellClassList = "text-left";
const thClassList = cellClassList + " py-3 text-emphasis-300 text-text-primary";
const tdClassList = cellClassList + " py-4 text-300";
const rowClassList = "border-b border-border-5";

const List = <ListItem, ItemKeyValueType, FilterType extends Key>({
  activeCols,
  colTitles,
  colConfig,
  filters,
  filterItem,
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
  includeOptions,
}: ListProps<ListItem, ItemKeyValueType, FilterType>) => {
  const [selectedItems, setSelectedItems] =
    useState<ItemKeyValueType[]>(initialSelectedItems);
  const [filteredItems, setFilteredItems] = useState<ListItem[]>(items);

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

  const handleFiltering = useCallback(
    (activeFilters: {
      buttonFilters: (FilterType | typeof defaultListFilter)[];
      dropdownFilters: Record<string, string>;
    }) => {
      setFilteredItems(
        items.filter(
          (item) =>
            filterItem === undefined ||
            filterItem(
              item,
              activeFilters.buttonFilters,
              activeFilters.dropdownFilters,
            ),
        ),
      );
    },
    [filterItem, items],
  );

  return (
    <div className="flex flex-col overflow-hidden">
      <Filters<ListItem, FilterType>
        className="gap-4 py-4"
        filterItems={filters ?? []}
        filterSize={filterSize}
        items={items}
        onFilter={handleFiltering}
      />
      {!noDataElement || (items && items.length > 0) ? (
        <div className="overflow-y-auto">
          <table className="min-w-full table-fixed border-collapse">
            <thead data-testid="list-header">
              <tr className={rowClassList}>
                {itemSelectable && (
                  <th className={thClassList}>
                    <div className="w-12 truncate overflow-hidden">
                      <Checkbox
                        checked={allSelected}
                        onChange={(e) => handleSelectAll(e.target.checked)}
                      />
                    </div>
                  </th>
                )}

                {activeCols.map((row, idx) => (
                  <th className={thClassList} key={idx}>
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
                {includeOptions && (
                  <th className={thClassList}>
                    <div className="w-12 truncate overflow-hidden">
                      <button className="align-middle text-text-primary-30 hover:cursor-pointer hover:text-text-primary-50">
                        <Ellipsis />
                      </button>
                    </div>
                  </th>
                )}
              </tr>
            </thead>
            <tbody data-testid="list-body">
              {filteredItems.map((item, i) => (
                <tr
                  key={i}
                  className={clsx(rowClassList, "hover:cursor-pointer")}
                >
                  {itemSelectable && (
                    <td className={tdClassList}>
                      <div className="w-12 truncate overflow-hidden">
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
                    <td className={tdClassList} key={j}>
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
                      <div className="w-12">
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
        </div>
      ) : (
        noDataElement
      )}
      {renderActionBar && renderActionBar(selectedItems)}
    </div>
  );
};

export default List;
