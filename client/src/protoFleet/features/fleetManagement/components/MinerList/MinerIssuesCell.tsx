import MinerIssues from "./MinerIssues";
import type { DeviceListItem } from "./types";

type MinerIssuesCellProps = {
  device: DeviceListItem;
  errorsLoaded: boolean;
  onOpenStatusFlow: (deviceIdentifier: string) => void;
};

const MinerIssuesCell = ({ device, errorsLoaded, onOpenStatusFlow }: MinerIssuesCellProps) => {
  return (
    <MinerIssues
      miner={device.miner}
      errors={device.errors}
      errorsLoaded={errorsLoaded}
      onClick={() => onOpenStatusFlow(device.deviceIdentifier)}
    />
  );
};

export default MinerIssuesCell;
