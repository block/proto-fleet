import { mockHashboardStats } from "../constants";
import AsicPopoverComponent from "./AsicPopover";
import { mockAsicHashrateData, mockAsicTemperatureData } from "./constants";

interface AsicTableProps {
  pendingAsicHashrateData: boolean;
  pendingAsicTemperatureData: boolean;
}

export const AsicPopover = ({
  pendingAsicHashrateData,
  pendingAsicTemperatureData,
}: AsicTableProps) => {
  return (
    <div className="relative mt-96 ml-40">
      <AsicPopoverComponent
        asic={mockHashboardStats.asics[0]}
        hashrateData={pendingAsicHashrateData ? [] : mockAsicHashrateData.data}
        pendingAsicHashrateData={pendingAsicHashrateData}
        pendingAsicTemperatureData={pendingAsicTemperatureData}
        temperatureData={
          pendingAsicTemperatureData ? [] : mockAsicTemperatureData.data
        }
      />
    </div>
  );
};

export default {
  title: "Pages/Hardware/Asic Popover",
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
