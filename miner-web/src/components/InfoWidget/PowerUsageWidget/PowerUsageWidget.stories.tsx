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
    <div className="flex w-[294px]">
      <PowerUsageWidgetComponent
        powerUsage={hasPowerUsage ? powerUsage : null}
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
