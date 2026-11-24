import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

const ModuleTemperature = ({ value }: { value: number | null }) => {
  const unit = useTemperatureUnit();
  const displayValue = value === null ? null : unit === "F" ? convertCtoF(value) : value;
  return <span className="tabular-nums">{displayValue === null ? "—" : `${displayValue.toFixed(1)} °${unit}`}</span>;
};

export default ModuleTemperature;
