import { tankSide } from "./tankSide";
import type { LevelRollup, MetricTelemetry, PlantTelemetry } from "@/protoOS/store/contract";
import { useFlowUnit, useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import { formatFlowRate } from "@/shared/utils/flowRate";
import { convertCtoF, formatEfficiencyOrDash, formatTempRange } from "@/shared/utils/telemetryFormat";

const latestValue = (metric: MetricTelemetry | undefined) => {
  const number = metric?.latest?.value;
  return number !== undefined && number !== null && Number.isFinite(number) ? number : null;
};

const TankMetrics = ({
  rollup,
  showCooling = false,
  plant,
  tankSlot,
}: {
  rollup?: LevelRollup;
  showCooling?: boolean;
  plant?: PlantTelemetry;
  tankSlot?: number;
}) => {
  const unit = useTemperatureUnit();
  const flowUnit = useFlowUnit();
  const side = tankSide(tankSlot);
  const outlet = side === "Left" ? plant?.outletATemperature : side === "Right" ? plant?.outletBTemperature : undefined;
  const fluidLevel = side === "Left" ? plant?.tankALiquidLevel : side === "Right" ? plant?.tankBLiquidLevel : undefined;
  const coolingMetric = (label: string, metric: MetricTelemetry | undefined, units: string) => {
    const value = latestValue(metric);
    const converted = value !== null && units === "°C" && unit === "F" ? convertCtoF(value) : value;
    return {
      label,
      text: converted === null ? "—" : `${converted.toFixed(1)} ${units === "°C" ? `°${unit}` : units}`,
    };
  };
  const hashrate = latestValue(rollup?.hashrate);
  const power = latestValue(rollup?.power);
  const min = latestValue(rollup?.minimumTemperature);
  const max = latestValue(rollup?.maximumTemperature);
  const average = latestValue(rollup?.temperature);
  let temperature = "—";
  if (min !== null && max !== null) {
    temperature = formatTempRange(min, max, unit);
  } else if (average !== null) {
    const converted = unit === "F" ? convertCtoF(average) : average;
    temperature = `${converted.toFixed(1)} °${unit}`;
  }
  const metrics: { label: string; text: string }[] = [
    {
      label: "Hashrate",
      text: hashrate === null ? "—" : `${hashrate.toLocaleString(undefined, { maximumFractionDigits: 1 })} TH/s`,
    },
    {
      label: "Power",
      text: power === null ? "—" : `${(power / 1000).toFixed(1)} kW`,
    },
    { label: "Efficiency", text: formatEfficiencyOrDash(latestValue(rollup?.efficiency)) },
    { label: "Temperature", text: temperature },
  ];
  if (showCooling) {
    const tankLabel = side ? `${side} tank` : "Tank";
    metrics.push(
      { label: "Flow", text: formatFlowRate(latestValue(plant?.flowRate), flowUnit) },
      coolingMetric(`${tankLabel} inlet temperature`, plant?.inletTemperature, "°C"),
      coolingMetric(`${tankLabel} outlet temperature`, outlet, "°C"),
      coolingMetric("Fluid level", fluidLevel, "mm"),
    );
  }
  return (
    <dl className={`grid grid-cols-2 gap-x-6 gap-y-8 ${showCooling ? "tablet:grid-cols-4" : ""}`}>
      {metrics.map(({ label, text }) => (
        <div key={label}>
          <dt className="mb-1 text-300 text-text-primary-70">{label}</dt>
          <dd className="text-emphasis-400 text-text-primary tabular-nums tablet:text-heading-200">{text}</dd>
        </div>
      ))}
    </dl>
  );
};

export default TankMetrics;
