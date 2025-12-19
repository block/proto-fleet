import { INACTIVE_PLACEHOLDER } from "./constants";
import { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type MinerMeasurementProps = {
  measurement: Measurement[] | undefined | null;
  unit: string;
  className?: string;
};

const MinerMeasurement = ({ measurement, unit, className }: MinerMeasurementProps) => {
  // undefined = telemetry not loaded yet (show skeleton)
  if (measurement === undefined) {
    return <SkeletonBar className={className || "w-full pr-10"} />;
  }

  // null = miner is inactive/offline (show placeholder)
  if (measurement === null) {
    return <>{INACTIVE_PLACEHOLDER}</>;
  }

  const latestValue = getLatestMeasurementWithData(measurement)?.value;

  // Show value if available
  if (latestValue !== undefined) {
    return (
      <>
        {getDisplayValue(latestValue)} {unit}
      </>
    );
  }

  return <>{INACTIVE_PLACEHOLDER}</>;
};

export default MinerMeasurement;
