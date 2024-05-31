import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { convertAggregatePowerValues, convertPowerValues } from "./utility";
import PowerUsageWidgetComponent, { mockPowerData } from ".";

interface PowerUsageWidgetProps {
  hasPowerUsage: boolean;
  loading: boolean;
}

export const PowerUsageWidget = ({
  hasPowerUsage,
  loading,
}: PowerUsageWidgetProps) => {
  return (
    <div className="flex w-[294px]">
      <PowerUsageWidgetComponent
        powerAggregates={
          hasPowerUsage
            ? convertAggregatePowerValues(mockPowerData.aggregates)
            : {}
        }
        powerValues={
          hasPowerUsage ? convertPowerValues(mockPowerData.data) : []
        }
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components/Info Widgets/Power Usage Widget",
  args: {
    hasPowerUsage: true,
    loading: false,
  },
  argTypes: {
    hasPowerUsage: {
      control: "boolean",
    },
    loading: {
      control: "boolean",
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
