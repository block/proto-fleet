import MinerStatus from "./MinerStatus";
import type { DeviceListItem } from "./types";

type MinerStatusCellProps = {
  device: DeviceListItem;
  errorsLoaded: boolean;
  onOpenStatusFlow: (deviceIdentifier: string) => void;
};

const MinerStatusCell = ({ device, errorsLoaded, onOpenStatusFlow }: MinerStatusCellProps) => {
  return (
    <MinerStatus
      miner={device.miner}
      errors={device.errors}
      activeBatches={device.activeBatches}
      errorsLoaded={errorsLoaded}
      onClick={() => onOpenStatusFlow(device.deviceIdentifier)}
    />
  );
};

export default MinerStatusCell;
