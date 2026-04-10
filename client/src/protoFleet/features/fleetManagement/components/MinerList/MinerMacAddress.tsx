import { INACTIVE_PLACEHOLDER } from "./constants";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerMacAddressProps = {
  miner: MinerStateSnapshot;
};

const MinerMacAddress = ({ miner }: MinerMacAddressProps) => {
  return <span>{miner.macAddress || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerMacAddress;
