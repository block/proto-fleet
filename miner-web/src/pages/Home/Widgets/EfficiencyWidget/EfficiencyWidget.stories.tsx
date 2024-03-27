import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import EfficiencyWidgetComponent from ".";

interface EfficiencyWidgetProps {
  efficiency: number;
  hasEfficiency: boolean;
  loading: boolean;
}

export const EfficiencyWidget = ({
  efficiency,
  hasEfficiency,
  loading,
}: EfficiencyWidgetProps) => {
  return (
    <div className="w-[277px]">
      <EfficiencyWidgetComponent
        efficiency={hasEfficiency ? efficiency : null}
        efficiencyValues={
          hasEfficiency && !loading
            ? [
                { value: 1 },
                { value: 3 },
                { value: 2 },
                { value: 9 },
                { value: 5 },
              ]
            : []
        }
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Pages/Home/Widgets/EfficiencyWidget",
  args: {
    efficiency: 15.50,
    hasEfficiency: true,
    loading: false,
  },
  argTypes: {
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
