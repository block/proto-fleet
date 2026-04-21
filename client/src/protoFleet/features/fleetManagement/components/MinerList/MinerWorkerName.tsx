import { INACTIVE_PLACEHOLDER } from "./constants";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerWorkerNameProps = {
  miner: MinerStateSnapshot;
};

const MinerWorkerName = ({ miner }: MinerWorkerNameProps) => {
  const normalizedWorkerName = miner.workerName?.trim() ?? "";

  return <span>{normalizedWorkerName || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerWorkerName;
