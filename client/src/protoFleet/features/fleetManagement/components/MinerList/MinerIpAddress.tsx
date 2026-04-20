import { INACTIVE_PLACEHOLDER } from "./constants";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerIpAddressProps = {
  miner: MinerStateSnapshot;
};

const MinerIpAddress = ({ miner }: MinerIpAddressProps) => {
  if (!miner.ipAddress) {
    return <span>{INACTIVE_PLACEHOLDER}</span>;
  }

  if (!miner.url) {
    return <span>{miner.ipAddress}</span>;
  }

  return (
    <a href={miner.url} target="_blank" rel="noopener noreferrer">
      {miner.ipAddress}
    </a>
  );
};

export default MinerIpAddress;
