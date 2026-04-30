import { useMemo } from "react";
import { action } from "storybook/actions";
import ListComponent from ".";
import { defaultListFilter } from "@/shared/components/List/constants";
import { ActiveFilters, type DropdownFilterItem, type FilterItem } from "@/shared/components/List/Filters/types";
import testColConfig from "@/shared/components/List/mocks/colConfig";
import { testCols, testColTitles, testFilters, TestItem, testItems } from "@/shared/components/List/mocks/data";
import Switch from "@/shared/components/Switch";

interface ListArgs {
  numberOfItems: number;
  numberOfColumns: number;
  numberOfItemActions: number;
  itemSelectable: boolean;
  disabled: boolean;
}

const activeCols = [testCols.name, testCols.status, testCols.value, testCols.timestamp] as (keyof TestItem)[];

const actions = [
  {
    title: "Action 1",
    actionHandler: action("Action 1"),
  },
  {
    title: "Action 2",
    actionHandler: action("Action 2"),
  },
  {
    title: "Action 3",
    actionHandler: action("Action 3"),
  },
];

export const List = ({ numberOfItems, numberOfColumns, numberOfItemActions, itemSelectable, disabled }: ListArgs) => {
  // Filter function that supports both button and dropdown filters
  const filterItem = (item: TestItem, filters: ActiveFilters) => {
    // Check button filters first
    if (!filters.buttonFilters.includes(defaultListFilter)) {
      // If "all" isn't selected, item must match at least one active filter
      const matchesStatus = filters.buttonFilters.some((filter) => item.status === filter);

      if (!matchesStatus) {
        return false;
      }
    }

    // Then check dropdown filters
    if (filters.dropdownFilters && filters.dropdownFilters["valueRange"]) {
      const valueRange = filters.dropdownFilters["valueRange"];

      if (valueRange.includes("low") && item.value > 200) {
        return false;
      } else if (valueRange.includes("medium") && (item.value <= 200 || item.value > 400)) {
        return false;
      } else if (valueRange.includes("high") && item.value <= 400) {
        return false;
      }
    }

    return true;
  };

  // Demonstrate the meta-dropdown alongside the existing button + dropdown filters.
  // Wraps the value-range dropdown from `testFilters` plus an extra firmware category
  // that has no standalone trigger — selecting it shows up in the active-pill row.
  const filtersWithMeta = useMemo<FilterItem[]>(() => {
    const valueRangeFilter = testFilters.find(
      (f): f is DropdownFilterItem => f.type === "dropdown" && f.value === "valueRange",
    );
    const firmwareFilter: DropdownFilterItem = {
      type: "dropdown",
      title: "Firmware",
      value: "firmware",
      options: [
        { id: "v3.5.1", label: "v3.5.1" },
        { id: "v3.5.2", label: "v3.5.2" },
        { id: "v3.6.0", label: "v3.6.0" },
      ],
      defaultOptionIds: [],
    };
    const nestedChildren = valueRangeFilter ? [valueRangeFilter, firmwareFilter] : [firmwareFilter];
    return [
      {
        type: "nestedFilterDropdown",
        title: "Filters",
        value: "all-filters",
        children: nestedChildren,
      },
      ...testFilters,
    ];
  }, []);

  return (
    <ListComponent<TestItem, TestItem["id"]>
      activeCols={activeCols.slice(0, numberOfColumns)}
      colTitles={testColTitles}
      colConfig={testColConfig}
      filters={filtersWithMeta}
      filterItem={filterItem}
      headerControls={<Switch label="Show passwords" />}
      items={[...testItems, ...testItems, ...testItems, ...testItems].slice(0, numberOfItems)}
      itemKey="id"
      actions={actions.slice(0, numberOfItemActions)}
      itemSelectable={itemSelectable}
      disabled={disabled}
      noDataElement={
        <div className="flex h-64 w-full items-center justify-center rounded-2xl bg-core-primary-5">No data found</div>
      }
    />
  );
};

export default {
  title: "Shared/List",
  parameters: {
    docs: {
      description: {
        component:
          "A reusable and configurable list component for displaying tabular data with support for:\n" +
          " - Customizable columns\n" +
          "   - Define how to render the columns using object of type `ColConfig` and `ColTitles`\n" +
          "   - Define which columns should be visible at the moment with `activeCols` prop.\n" +
          " - Selectable items with a checkbox.\n" +
          "   - Can be turned on or off with `itemSelectable` prop\n" +
          " - Action buttons for each item.\n" +
          " - Filters for refining displayed data.\n" +
          "   - Use `filterItem` prop to use client side filtering. This predicate should decide whether list item should be displayed or not. \n" +
          "   - Use `onServerFilter` prop to use server side filtering. This callback should construct a filter message and request new data from the server. \n" +
          " - A customizable action bar for selected items.\n" +
          " - Disabled (readonly view)\n" +
          ' - A "no data" placeholder when the list is empty.\n' +
          " - Sticky header and first two columns on mobile and tablet viewports.\n" +
          "   - For this to work properly you need to specify max height of the table via `containerClassName` prop.",
      },
    },
  },
  args: {
    numberOfItems: 5,
    numberOfColumns: 4,
    numberOfItemActions: 3,
    itemSelectable: true,
    disabled: false,
  },
  argTypes: {
    numberOfItems: { control: { type: "range", min: 0, max: 20, step: 1 } },
    numberOfColumns: { control: { type: "range", min: 1, max: 4, step: 1 } },
    numberOfItemActions: {
      control: { type: "range", min: 0, max: 3, step: 1 },
    },
  },
  tags: ["autodocs"],
};
