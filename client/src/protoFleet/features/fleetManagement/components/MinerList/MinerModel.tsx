import { INACTIVE_PLACEHOLDER } from "./constants";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerModelProps = {
  miner: MinerStateSnapshot;
};

const MinerModel = ({ miner }: MinerModelProps) => {
  return <span>{miner.model || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerModel;
