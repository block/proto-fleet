import MinerMeasurement from "./MinerMeasurement";
import { useMinerPowerUsage } from "@/protoFleet/store";

type MinerPowerUsageProps = {
  deviceIdentifier: string;
};

const MinerPowerUsage = ({ deviceIdentifier }: MinerPowerUsageProps) => {
  const powerUsage = useMinerPowerUsage(deviceIdentifier);
  return <MinerMeasurement measurement={powerUsage} unit="kW" />;
};

export default MinerPowerUsage;
