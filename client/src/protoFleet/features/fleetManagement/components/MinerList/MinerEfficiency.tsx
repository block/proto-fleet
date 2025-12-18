import MinerMeasurement from "./MinerMeasurement";
import UnsupportedMetric from "./UnsupportedMetric";
import { useMiner, useMinerEfficiency } from "@/protoFleet/store";

type MinerEfficiencyProps = {
  deviceIdentifier: string;
};

const MinerEfficiency = ({ deviceIdentifier }: MinerEfficiencyProps) => {
  const miner = useMiner(deviceIdentifier);
  const efficiency = useMinerEfficiency(deviceIdentifier);

  // Check if miner doesn't support efficiency reporting
  const efficiencyReported = miner?.capabilities?.telemetry?.efficiencyReported;
  if (!efficiencyReported) {
    return <UnsupportedMetric message="This miner's firmware doesn't share this data." />;
  }

  return <MinerMeasurement measurement={efficiency} unit="J/TH" />;
};

export default MinerEfficiency;
