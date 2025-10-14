import useMinerStore from "@/protoOS/store/useMinerStore";
import { convertCtoF } from "@/shared/utils/utility";

interface TemperatureValueProps {
  value: number;
}

function TemperatureValue({ value }: TemperatureValueProps) {
  const temperatureUnit = useMinerStore((state) => state.ui.temperatureUnit);

  const displayValue = temperatureUnit === "F" ? convertCtoF(value) : value;

  return (
    <>
      {displayValue.toFixed(1)} °{temperatureUnit}
    </>
  );
}

export default TemperatureValue;
