import { useMemo } from "react";
import { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type MinerMeasurementProps = {
  measurement: Measurement[] | undefined;
  unit: string;
  className?: string;
};

const MinerMeasurement = ({
  measurement,
  unit,
  className,
}: MinerMeasurementProps) => {
  const latestMeasurement = useMemo(
    () => getLatestMeasurementWithData(measurement),
    [measurement],
  );

  const latestValue = latestMeasurement?.value;

  if (latestValue === undefined) return "N/A";
  return latestValue ? (
    <>
      {getDisplayValue(latestValue)} {unit}
    </>
  ) : (
    <SkeletonBar className={className || "w-full pr-10"} />
  );
};

export default MinerMeasurement;
