import { useMinerModel } from "@/protoFleet/store";

type MinerTypeProps = {
  deviceIdentifier: string;
};

const MinerType = ({ deviceIdentifier }: MinerTypeProps) => {
  const model = useMinerModel(deviceIdentifier);
  return <span>{model || "—"}</span>;
};

export default MinerType;
