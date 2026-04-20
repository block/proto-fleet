import MinerMeasurement from "./MinerMeasurement";
import UnsupportedMetric from "./UnsupportedMetric";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerMeasurement } from "@/protoFleet/features/fleetManagement/utils/getMinerMeasurement";

type MinerPowerUsageProps = {
  miner: MinerStateSnapshot;
};

const MinerPowerUsage = ({ miner }: MinerPowerUsageProps) => {
  const powerUsage = getMinerMeasurement(miner, (m) => m.powerUsage);

  // Check if miner doesn't support power usage reporting or capability is not available yet
  const powerUsageReported = miner?.capabilities?.telemetry?.powerUsageReported;
  if (!powerUsageReported) {
    return <UnsupportedMetric message="This miner's firmware doesn't share this data." />;
  }

  return <MinerMeasurement measurement={powerUsage} unit="kW" />;
};

export default MinerPowerUsage;
