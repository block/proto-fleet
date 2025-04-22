import { Key, ReactNode, useCallback, useState } from "react";
import clsx from "clsx";

import { Ellipsis } from "@/shared/assets/icons";
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
    activeFilter: FilterType | typeof defaultListFilter,
  ) => boolean;
  items: ListItem[];
  itemKey: keyof ListItem;
  itemSelectable?: boolean;
  disabled?: boolean;
  actions?: ListAction<ListItem>[];
  noDataElement?: ReactNode;
  renderActionBar?: (selectedItems: ItemKeyValueType[]) => ReactNode;
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
  items,
  itemKey,
  itemSelectable = false,
  disabled = false,
  actions = [],
  noDataElement,
  renderActionBar,
}: ListProps<ListItem, ItemKeyValueType, FilterType>) => {
  const [selectedItems, setSelectedItems] = useState<ItemKeyValueType[]>([]);
  const [filteredItems, setFilteredItems] = useState<ListItem[]>(items);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedItems(items.map((item) => item[itemKey] as ItemKeyValueType));
    } else {
      setSelectedItems([]);
    }
  };

  const handleSelectItem = (itemKey: ItemKeyValueType, checked: boolean) => {
    setSelectedItems((prev) => {
      if (checked && !prev.includes(itemKey)) {
        return [...prev, itemKey];
      } else if (!checked) {
        return prev.filter((addr) => addr !== itemKey);
      }
      return prev;
    });
  };

  const allSelected = items.length > 0 && selectedItems.length === items.length;

  const handleFiltering = useCallback(
    (activeFilter: FilterType | typeof defaultListFilter) => {
      setFilteredItems(
        items.filter(
          (item) => filterItem === undefined || filterItem(item, activeFilter),
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

                <th className={thClassList}>
                  <div className="w-12 truncate overflow-hidden">
                    <button className="align-middle text-text-primary-30 hover:cursor-pointer hover:text-text-primary-50">
                      <Ellipsis />
                    </button>
                  </div>
                </th>
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
                          checked={selectedItems.includes(
                            item[itemKey] as ItemKeyValueType,
                          )}
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

                  <td className={tdClassList}>
                    <div className="w-12">
                      <PopoverProvider>
                        <ListActions<ListItem> item={item} actions={actions} />
                      </PopoverProvider>
                    </div>
                  </td>
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
