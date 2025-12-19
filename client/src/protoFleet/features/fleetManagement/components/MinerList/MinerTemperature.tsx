import { INACTIVE_PLACEHOLDER } from "./constants";
import { useMinerTemperature, useTemperatureUnit } from "@/protoFleet/store";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF } from "@/shared/utils/utility";

type MinerTemperatureProps = {
  deviceIdentifier: string;
};

const MinerTemperature = ({ deviceIdentifier }: MinerTemperatureProps) => {
  const temperature = useMinerTemperature(deviceIdentifier);
  const temperatureUnit = useTemperatureUnit();

  if (temperature === undefined) {
    return <SkeletonBar className="w-full pr-10" />;
  }

  if (temperature === null) {
    return <>{INACTIVE_PLACEHOLDER}</>;
  }

  const latestValue = getLatestMeasurementWithData(temperature)?.value;

  if (latestValue === undefined) {
    return <>{INACTIVE_PLACEHOLDER}</>;
  }

  const displayValue = temperatureUnit === "F" ? convertCtoF(latestValue) : latestValue;

  return (
    <>
      {getDisplayValue(displayValue)} °{temperatureUnit}
    </>
  );
};

export default MinerTemperature;
