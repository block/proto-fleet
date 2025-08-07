import { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import SkeletonBar from "@/shared/components/SkeletonBar";
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
  return (
    <>
      {measurement && measurement.length > 0 ? (
        <>
          {getDisplayValue(measurement[measurement.length - 1]?.value)} {unit}
        </>
      ) : (
        <SkeletonBar className={className || "w-full pr-10"} />
      )}
    </>
  );
};

export default MinerMeasurement;
