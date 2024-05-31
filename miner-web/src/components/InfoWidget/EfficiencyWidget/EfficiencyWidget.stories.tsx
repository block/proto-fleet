import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { getDisplayValue } from "common/utils/stringUtils";

import { convertEfficiencyValues } from "./utility";
import EfficiencyWidgetComponent, { mockEfficiencyData } from ".";

interface EfficiencyWidgetProps {
  hasEfficiency: boolean;
  loading: boolean;
}

export const EfficiencyWidget = ({
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
            ? convertEfficiencyValues(mockEfficiencyData.data)
            : []
        }
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components/Info Widgets/Efficiency Widget",
  args: {
    hasEfficiency: true,
    loading: false,
  },
  argTypes: {
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
