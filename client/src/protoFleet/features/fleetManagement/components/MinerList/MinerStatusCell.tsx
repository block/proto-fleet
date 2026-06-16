import MinerStatus from "./MinerStatus";
import type { DeviceListItem } from "./types";

type MinerStatusCellProps = {
  device: DeviceListItem;
  errorsLoaded: boolean;
  onOpenStatusFlow: (deviceIdentifier: string) => void;
  isRefreshing?: boolean;
};

const MinerStatusCell = ({ device, errorsLoaded, onOpenStatusFlow, isRefreshing }: MinerStatusCellProps) => {
  return (
    <MinerStatus
      miner={device.miner}
      errors={device.errors}
      activeBatches={device.activeBatches}
      errorsLoaded={errorsLoaded}
      isRefreshing={isRefreshing}
      onClick={() => onOpenStatusFlow(device.deviceIdentifier)}
    />
  );
};

export default MinerStatusCell;
