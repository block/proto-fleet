import { INACTIVE_PLACEHOLDER } from "./constants";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerWebUiUrl } from "@/protoFleet/features/fleetManagement/utils/minerWebUiUrl";

type MinerIpAddressProps = {
  miner: MinerStateSnapshot;
};

const MinerIpAddress = ({ miner }: MinerIpAddressProps) => {
  if (!miner.ipAddress) {
    return <span>{INACTIVE_PLACEHOLDER}</span>;
  }

  const webUiUrl = getMinerWebUiUrl({ ipAddress: miner.ipAddress, url: miner.url });

  if (!webUiUrl) {
    return <span>{miner.ipAddress}</span>;
  }

  return (
    <a href={webUiUrl} target="_blank" rel="noopener noreferrer">
      {miner.ipAddress}
    </a>
  );
};

export default MinerIpAddress;
