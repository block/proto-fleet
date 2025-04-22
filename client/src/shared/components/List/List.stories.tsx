import { action } from "@storybook/addon-actions";
import ListComponent from ".";
import alertColConfig from "@/protoFleet/components/AlertsModal/alertColConfig";
import AlertListActionBar from "@/protoFleet/components/AlertsModal/AlertListActionBar";
import {
  alertCols,
  alertColTitles,
  AlertType,
  alertTypes,
} from "@/protoFleet/components/AlertsModal/constants";
import { alerts } from "@/protoFleet/components/AlertsModal/stories/mocks";
import { Alert } from "@/protoFleet/components/AlertsModal/types";
import { defaultListFilter } from "@/shared/components/List/constants";

interface ListArgs {
  numberOfItems: number;
  numberOfColumns: number;
  numberOfItemActions: number;
  itemSelectable: boolean;
  disabled: boolean;
}

const activeCols = [
  alertCols.name,
  alertCols.status,
  alertCols.error,
  alertCols.timestamp,
] as (keyof Alert)[];

const filters = [
  {
    title: "All alerts",
    value: defaultListFilter,
    count: alerts.length,
  },
  {
    title: "Control board",
    value: alertTypes.controlBoard,
    count: 1,
  },
  {
    title: "Fan",
    value: alertTypes.fan,
    count: 1,
  },
];

const actions = [
  {
    title: "Archive",
    actionHandler: action("Archive"),
  },
  {
    title: "View miner",
    actionHandler: action("View miner"),
  },
  {
    title: "Reboot miner",
    actionHandler: action("Reboot miner"),
  },
];

export const List = ({
  numberOfItems,
  numberOfColumns,
  numberOfItemActions,
  itemSelectable,
  disabled,
}: ListArgs) => {
  return (
    <ListComponent<Alert, Alert["minerMacAddress"], AlertType>
      activeCols={activeCols.slice(0, numberOfColumns)}
      colTitles={alertColTitles}
      colConfig={alertColConfig}
      filters={filters}
      items={alerts.slice(0, numberOfItems)}
      itemKey="minerMacAddress"
      actions={actions.slice(0, numberOfItemActions)}
      itemSelectable={itemSelectable}
      disabled={disabled}
      renderActionBar={(selectedItems) => (
        <AlertListActionBar selectedAlerts={selectedItems} />
      )}
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
