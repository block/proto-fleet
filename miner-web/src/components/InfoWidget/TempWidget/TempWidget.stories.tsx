import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import TempWidgetComponent, { mockTemperatureData } from ".";

interface TempProps {
  hasTemp: boolean;
  hashboardNumber: number;
  loading: boolean;
}

export const TempWidget = ({
  hasTemp,
  hashboardNumber,
  loading,
}: TempProps) => {
  const hashboardSerials = ["1", "2", "3"];
  return (
    <div className="flex w-[294px]">
      <TempWidgetComponent
        temp={
          hasTemp
            ? mockTemperatureData.data[mockTemperatureData.data.length - 1]
                .value
            : undefined
        }
        highestTemp={
          hasTemp ? mockTemperatureData.aggregates?.max : undefined
        }
        hashboardSerials={
          hasTemp ? hashboardSerials.slice(-hashboardNumber) : []
        }
        duration="12h"
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components/Info Widgets/Temp Widget",
  args: {
    hasTemp: true,
    hashboardNumber: 3,
    loading: false,
  },
  argTypes: {
    hasTemp: {
      control: "boolean",
    },
    hashboardNumber: {
      control: "select",
      options: [1, 2, 3],
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
