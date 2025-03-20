import { ElementType } from "react";
import { mockHashboardStats } from "../constants";
import AsicPopoverComponent from "./AsicPopover";
import { mockAsicHashrateData, mockAsicTemperatureData } from "./constants";
import { convertHashrateValues, convertTemperatureValues } from "./utility";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";

interface AsicTableProps {
  pendingAsicHashrateData: boolean;
  pendingAsicTemperatureData: boolean;
}

export const AsicPopover = ({
  pendingAsicHashrateData,
  pendingAsicTemperatureData,
}: AsicTableProps) => {
  const { triggerRef } = usePopover();

  return (
    <div ref={triggerRef} className="relative mt-96 ml-40">
      <AsicPopoverComponent
        asic={mockHashboardStats.asics[0]}
        hashrateData={
          pendingAsicHashrateData
            ? []
            : convertHashrateValues(mockAsicHashrateData.data)
        }
        pendingAsicHashrateData={pendingAsicHashrateData}
        pendingAsicTemperatureData={pendingAsicTemperatureData}
        temperatureData={
          pendingAsicTemperatureData
            ? []
            : convertTemperatureValues(mockAsicTemperatureData.data)
        }
      />
    </div>
  );
};

export default {
  title: "Components/Asic Temperature/Asic Popover",
  decorators: [
    (Story: ElementType) => (
      <PopoverProvider>
        <Story />
      </PopoverProvider>
    ),
  ],
  args: {
    pendingAsicHashrateData: false,
    pendingAsicTemperatureData: false,
  },
  argTypes: {
    pendingAsicHashrateData: {
      control: {
        type: "boolean",
      },
    },
    pendingAsicTemperatureData: {
      control: {
        type: "boolean",
      },
    },
  },
};
