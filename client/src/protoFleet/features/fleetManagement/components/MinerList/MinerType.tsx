import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerModel } from "@/protoFleet/store";

type MinerTypeProps = {
  deviceIdentifier: string;
};

const MinerType = ({ deviceIdentifier }: MinerTypeProps) => {
  const model = useMinerModel(deviceIdentifier);
  return <span>{model || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerType;
