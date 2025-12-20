import AsicPopover from "./AsicPopover";
import { AsicStats } from "@/protoOS/api/generatedApi";
import { useMinerAsic } from "@/protoOS/store";
import { getAsicId } from "@/protoOS/store";

interface AsicPopoverWrapperProps {
  asic: AsicStats;
  hashboardSerial: string;
  closePopover: () => void;
  closeIgnoreSelectors?: string[];
}

const AsicPopoverWrapper = ({ asic, hashboardSerial, closePopover, closeIgnoreSelectors }: AsicPopoverWrapperProps) => {
  // Get integrated ASIC data using consistent ID format
  const asicId = getAsicId(hashboardSerial, asic?.index ?? 0);
  const asicData = useMinerAsic(asicId);

  // If no telemetry data is available, don't render the popover
  if (!asicData) {
    return null;
  }

  return <AsicPopover asic={asicData} closePopover={closePopover} closeIgnoreSelectors={closeIgnoreSelectors} />;
};

export default AsicPopoverWrapper;
