import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { mockEfficiencyData } from "./constants";
import { aggregateEfficiencyValues, convertEfficiencyValues } from "./utility";
import EfficiencyWidgetComponent from ".";
import { Duration } from "@/shared/components/DurationSelector";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface EfficiencyWidgetProps {
  duration: Duration;
  hasEfficiency: boolean;
  loading: boolean;
}

export const EfficiencyWidget = ({
  duration,
  hasEfficiency,
  loading,
}: EfficiencyWidgetProps) => {
  return (
    <div className="flex w-[294px]">
      <EfficiencyWidgetComponent
        avgEfficiency={
          hasEfficiency
            ? getDisplayValue(mockEfficiencyData.aggregates.avg)
            : null
        }
        efficiencyValues={
          hasEfficiency && !loading
            ? convertEfficiencyValues(
                aggregateEfficiencyValues(mockEfficiencyData.data, duration)
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
  title: "Components (protoOS)/Info Widgets/Efficiency Widget",
  args: {
    duration: "12h",
    hasEfficiency: true,
    loading: false,
  },
  argTypes: {
    duration: {
      control: "select",
      options: ["12h", "24h", "48h", "5d"],
    },
    hasEfficiency: {
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
