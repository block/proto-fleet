import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerModel } from "@/protoFleet/store";

type MinerModelProps = {
  deviceIdentifier: string;
};

const MinerModel = ({ deviceIdentifier }: MinerModelProps) => {
  const model = useMinerModel(deviceIdentifier);
  return <span>{model || INACTIVE_PLACEHOLDER}</span>;
};

export default MinerModel;
