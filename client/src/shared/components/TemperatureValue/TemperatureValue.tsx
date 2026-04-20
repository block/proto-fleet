import useMinerStore from "@/protoOS/store/useMinerStore";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { convertCtoF } from "@/shared/utils/utility";

interface TemperatureValueProps {
  value: number | undefined | null;
}

function TemperatureValue({ value }: TemperatureValueProps) {
  const temperatureUnit = useMinerStore((state) => state.ui.temperatureUnit);

  if (value === null) {
    return <>N/A</>;
  }

  if (value === undefined) {
    return <SkeletonBar />;
  }

  const displayValue = temperatureUnit === "F" ? convertCtoF(value) : value;

  return (
    <>
      {displayValue.toFixed(1)} °{temperatureUnit}
    </>
  );
}

export default TemperatureValue;
