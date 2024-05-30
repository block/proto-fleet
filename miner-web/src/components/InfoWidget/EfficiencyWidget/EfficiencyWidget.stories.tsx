import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { getTimeFromEpoch } from "common/utils/stringUtils";

import EfficiencyWidgetComponent, { mockEfficiencyData } from ".";

interface EfficiencyWidgetProps {
  avgEfficiency: number;
  efficiency: number;
  hasEfficiency: boolean;
  loading: boolean;
}

export const EfficiencyWidget = ({
  avgEfficiency,
  efficiency,
  hasEfficiency,
  loading,
}: EfficiencyWidgetProps) => {
  return (
    <div className="flex w-[294px]">
      <EfficiencyWidgetComponent
        efficiency={hasEfficiency ? efficiency : null}
        avgEfficiency={hasEfficiency ? avgEfficiency : null}
        efficiencyValues={
          hasEfficiency && !loading
            ? mockEfficiencyData.data.map((data) => ({
                time: getTimeFromEpoch(data.datetime),
                value: data.value || 0,
              }))
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
    avgEfficiency: 10.5,
    efficiency: 15.50,
    hasEfficiency: true,
    loading: false,
  },
  argTypes: {
    avgEfficiency: {
      control: "number",
    },
    efficiency: {
      control: "number",
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
