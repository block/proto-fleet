import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import AsicTempWidgetComponent from ".";

interface AsicTempProps {
  asicTemp: number;
  hasAsicTemp: boolean;
  loading: boolean;
}

export const AsicTempWidget = ({
  asicTemp,
  hasAsicTemp,
  loading,
}: AsicTempProps) => {
  return (
    <div className="flex w-[294px]">
      <AsicTempWidgetComponent
        asicTemp={hasAsicTemp ? asicTemp : undefined}
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components/Info Widgets/Asic Temp Widget",
  args: {
    asicTemp: 61,
    hasAsicTemp: true,
    loading: false,
  },
  argTypes: {
    asicTemp: {
      control: "number",
    },
    hasAsicTemp: {
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
