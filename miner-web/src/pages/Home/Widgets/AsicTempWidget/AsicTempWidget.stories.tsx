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
    <div className="w-[277px]">
      <AsicTempWidgetComponent
        asicTemp={hasAsicTemp ? asicTemp : undefined}
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Pages/Home/Widgets/AsicTempWidget",
  args: {
    asicTemp: 1300,
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
