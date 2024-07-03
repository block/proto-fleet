import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { Duration } from "components/DurationSelector";

import { mockPowerData } from "./constants";
import {
  aggregatePowerValues,
  convertAggregatePowerValues,
  convertPowerValues,
} from "./utility";
import PowerUsageWidgetComponent from ".";

interface PowerUsageWidgetProps {
  duration: Duration;
  hasPowerUsage: boolean;
  loading: boolean;
}

export const PowerUsageWidget = ({
  duration,
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
          hasPowerUsage
            ? convertPowerValues(
                aggregatePowerValues(mockPowerData.data, duration)
              )
            : []
        }
        duration={duration}
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components/Info Widgets/Power Usage Widget",
  args: {
    duration: "12h",
    hasPowerUsage: true,
    loading: false,
  },
  argTypes: {
    duration: {
      control: "select",
      options: ["12h", "24h", "48h", "5d"],
    },
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
