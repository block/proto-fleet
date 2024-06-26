import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { FanInfo } from "apiTypes";

import FanSpeedWidgetComponent from ".";

interface FanSpeedWidgetProps {
  fanSpeeds: FanInfo[];
  numberOfFans: number;
  loading: boolean;
}

export const FanSpeedWidget = ({
  fanSpeeds,
  numberOfFans,
  loading,
}: FanSpeedWidgetProps) => {
  return (
    <div className="flex w-[452px]">
      <FanSpeedWidgetComponent
        fanSpeeds={numberOfFans > 0 ? fanSpeeds.slice(0, numberOfFans) : undefined}
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components/Info Widgets/Fan Speed Widget",
  args: {
    fanSpeeds: [{ rpm: 3050 }, { rpm: 3049 }, { rpm: 6800 }, { rpm: 6730 }],
    numberOfFans: 4,
    loading: false,
  },
  argTypes: {
    fanSpeeds: {
      control: "object",
    },
    numberOfFans: {
      control: "select",
      options: [0, 1, 2, 3, 4],
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
