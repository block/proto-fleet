import { INACTIVE_PLACEHOLDER } from "./constants";
import { MinerMetricValue } from "./MinerMeasurement";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerMeasurement } from "@/protoFleet/features/fleetManagement/utils/getMinerMeasurement";
import { useTemperatureUnit } from "@/protoFleet/store";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

type MinerTemperatureProps = {
  miner: MinerStateSnapshot;
};

const MinerTemperature = ({ miner }: MinerTemperatureProps) => {
  const temperature = getMinerMeasurement(miner, (m) => m.temperature);
  const temperatureUnit = useTemperatureUnit();

  if (temperature === undefined) {
    return <SkeletonBar className="w-full pr-10" />;
  }

  if (temperature === null) {
    return <>{INACTIVE_PLACEHOLDER}</>;
  }

  // Empty array = empty cell for pool/auth required miners
  if (temperature.length === 0) {
    return null;
  }

  const latestValue = getLatestMeasurementWithData(temperature)?.value;

  if (latestValue === undefined) {
    return <>{INACTIVE_PLACEHOLDER}</>;
  }

  const displayValue = temperatureUnit === "F" ? convertCtoF(latestValue) : latestValue;

  return <MinerMetricValue value={displayValue} unit={`°${temperatureUnit}`} />;
};

export default MinerTemperature;
