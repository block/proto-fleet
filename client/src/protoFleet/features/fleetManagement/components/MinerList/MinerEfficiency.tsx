import MinerMeasurement from "./MinerMeasurement";
import UnsupportedMetric from "./UnsupportedMetric";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerMeasurement } from "@/protoFleet/features/fleetManagement/utils/getMinerMeasurement";

type MinerEfficiencyProps = {
  miner: MinerStateSnapshot;
};

const MinerEfficiency = ({ miner }: MinerEfficiencyProps) => {
  const efficiency = getMinerMeasurement(miner, (m) => m.efficiency);

  // Check if miner doesn't support efficiency reporting
  const efficiencyReported = miner?.capabilities?.telemetry?.efficiencyReported;
  if (!efficiencyReported) {
    return <UnsupportedMetric message="This miner's firmware doesn't share this data." />;
  }

  return <MinerMeasurement measurement={efficiency} unit="J/TH" />;
};

export default MinerEfficiency;
