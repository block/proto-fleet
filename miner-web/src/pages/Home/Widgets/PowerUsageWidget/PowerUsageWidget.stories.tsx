import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import PowerUsageWidgetComponent from ".";

interface PowerUsageWidgetProps {
  hasPowerUsage: boolean;
  loading: boolean;
  powerUsage: string;
}

export const PowerUsageWidget = ({
  hasPowerUsage,
  loading,
  powerUsage,
}: PowerUsageWidgetProps) => {
  return (
    <div className="w-[277px]">
      <PowerUsageWidgetComponent
        powerUsage={hasPowerUsage ? powerUsage : null}
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Home/Widgets/PowerUsageWidget",
  args: {
    hasPowerUsage: true,
    loading: false,
    powerUsage: 3.1,
  },
  argTypes: {
    hasPowerUsage: {
      control: "boolean",
    },
    loading: {
      control: "boolean",
    },
    powerUsage: {
      control: "number",
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
