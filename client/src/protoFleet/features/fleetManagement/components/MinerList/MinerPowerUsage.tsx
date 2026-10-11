import MinerMeasurement from "./MinerMeasurement";
import UnsupportedMetric from "./UnsupportedMetric";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerMeasurement } from "@/protoFleet/features/fleetManagement/utils/getMinerMeasurement";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { formatPowerKW } from "@/shared/utils/stringUtils";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";
import SkeletonBar from "@/shared/components/SkeletonBar";

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

  if (powerUsage === undefined) return <SkeletonBar className="w-full pr-10" />;
  if (powerUsage === null) return <>{INACTIVE_PLACEHOLDER}</>;
  if (powerUsage.length === 0) return null;

  const latestValue = getLatestMeasurementWithData(powerUsage)?.value;
  if (latestValue === undefined) return <>{INACTIVE_PLACEHOLDER}</>;

  const { value, unit } = formatPowerKW(latestValue);
  return <>{value} {unit}</>;
};

export default MinerPowerUsage;
