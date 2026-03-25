import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerWorkerName } from "@/protoFleet/store";

type MinerWorkerNameProps = {
  deviceIdentifier: string;
};

const MinerWorkerName = ({ deviceIdentifier }: MinerWorkerNameProps) => {
  const workerName = useMinerWorkerName(deviceIdentifier);
  const normalizedWorkerName = workerName?.trim() ?? "";

  return <span>{normalizedWorkerName || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerWorkerName;
