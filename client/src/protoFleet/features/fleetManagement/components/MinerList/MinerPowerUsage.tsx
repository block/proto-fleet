import MinerMeasurement from "./MinerMeasurement";
import UnsupportedMetric from "./UnsupportedMetric";
import { useMiner, useMinerPowerUsage } from "@/protoFleet/store";

type MinerPowerUsageProps = {
  deviceIdentifier: string;
};

const MinerPowerUsage = ({ deviceIdentifier }: MinerPowerUsageProps) => {
  const miner = useMiner(deviceIdentifier);
  const powerUsage = useMinerPowerUsage(deviceIdentifier);

  // Check if miner doesn't support power usage reporting or capability is not available yet
  const powerUsageReported = miner?.capabilities?.telemetry?.powerUsageReported;
  if (!powerUsageReported) {
    return <UnsupportedMetric message="This miner's firmware doesn't share this data." />;
  }

  return <MinerMeasurement measurement={powerUsage} unit="kW" />;
};

export default MinerPowerUsage;
