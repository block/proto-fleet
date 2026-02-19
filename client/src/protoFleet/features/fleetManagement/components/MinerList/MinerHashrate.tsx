import MinerMeasurement from "./MinerMeasurement";
import { useMinerHashrate } from "@/protoFleet/store";

type MinerHashrateProps = {
  deviceIdentifier: string;
};

const MinerHashrate = ({ deviceIdentifier }: MinerHashrateProps) => {
  const hashrate = useMinerHashrate(deviceIdentifier);

  return <MinerMeasurement measurement={hashrate} unit="TH/s" />;
};

export default MinerHashrate;
