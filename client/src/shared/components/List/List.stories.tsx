import { action } from "@storybook/addon-actions";
import ListComponent from ".";
import { defaultListFilter } from "@/shared/components/List/constants";
import testColConfig from "@/shared/components/List/mocks/colConfig";
import {
  testCols,
  testColTitles,
  testFilters,
  TestFilterState,
  TestItem,
  testItems,
} from "@/shared/components/List/mocks/data";

interface ListArgs {
  numberOfItems: number;
  numberOfColumns: number;
  numberOfItemActions: number;
  itemSelectable: boolean;
  disabled: boolean;
}

const activeCols = [
  testCols.name,
  testCols.status,
  testCols.value,
  testCols.timestamp,
] as (keyof TestItem)[];

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

export const List = ({
  numberOfItems,
  numberOfColumns,
  numberOfItemActions,
  itemSelectable,
  disabled,
}: ListArgs) => {
  // Filter function that supports both button and dropdown filters
  const filterItem = (
    item: TestItem,
    activeButtonFilters: (TestFilterState | typeof defaultListFilter)[],
    dropdownFilters?: Record<string, string>,
  ) => {
    // Check button filters first
    if (!activeButtonFilters.includes(defaultListFilter)) {
      // If "all" isn't selected, item must match at least one active filter
      const matchesStatus = activeButtonFilters.some(
        (filter) => item.status === filter,
      );

      if (!matchesStatus) {
        return false;
      }
    }

    // Then check dropdown filters
    if (dropdownFilters && dropdownFilters["valueRange"]) {
      const valueRange = dropdownFilters["valueRange"];

      if (valueRange === "low" && item.value > 200) {
        return false;
      } else if (
        valueRange === "medium" &&
        (item.value <= 200 || item.value > 400)
      ) {
        return false;
      } else if (valueRange === "high" && item.value <= 400) {
        return false;
      }
    }

    return true;
  };

  return (
    <ListComponent<TestItem, TestItem["id"], TestFilterState>
      activeCols={activeCols.slice(0, numberOfColumns)}
      colTitles={testColTitles}
      colConfig={testColConfig}
      filters={testFilters}
      filterItem={filterItem}
      items={testItems.slice(0, numberOfItems)}
      itemKey="id"
      actions={actions.slice(0, numberOfItemActions)}
      itemSelectable={itemSelectable}
      disabled={disabled}
      noDataElement={
        <div className="flex h-64 w-full items-center justify-center rounded-2xl bg-core-primary-5">
          No data found
        </div>
      }
    />
  );
};

export default {
  title: "Components (Shared)/List",
  parameters: {
    docs: {
      description: {
        component:
          "A reusable and configurable list component for displaying tabular data with support for:\n" +
          " - Customizable columns\n" +
          "   - Define how to render the columns using object of type ColConfig and ColTitles\n" +
          "   - Define which columns should be visible at the moment with activeCols prop.\n" +
          " - Selectable items with a checkbox.\n" +
          "   - Can be turned on or off with itemSelectable prop\n" +
          " - Action buttons for each item.\n" +
          " - Filters for refining displayed data.\n" +
          " - A customizable action bar for selected items.\n" +
          " - Disabled (readonly view)\n" +
          ' - A "no data" placeholder when the list is empty.',
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
    numberOfItems: { control: { type: "range", min: 0, max: 5, step: 1 } },
    numberOfColumns: { control: { type: "range", min: 1, max: 4, step: 1 } },
    numberOfItemActions: {
      control: { type: "range", min: 0, max: 3, step: 1 },
    },
  },
  tags: ["autodocs"],
};
