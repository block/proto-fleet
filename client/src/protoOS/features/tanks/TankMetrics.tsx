import type { LevelRollup, MetricTelemetry, PlantTelemetry } from "@/protoOS/store/contract";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
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
  // CT1's numbered tanks subdivide two fluid volumes: A (left, 1–3) and B (right, 4–6).
  const side = [1, 2, 3].includes(tankSlot ?? 0) ? "Left" : [4, 5, 6].includes(tankSlot ?? 0) ? "Right" : undefined;
  const outlet = side === "Left" ? plant?.outletATemperature : side === "Right" ? plant?.outletBTemperature : undefined;
  const fluidLevel = side === "Left" ? plant?.tankALiquidLevel : side === "Right" ? plant?.tankBLiquidLevel : undefined;
  const coolingMetric = (label: string, metric: MetricTelemetry | undefined, units: string, note?: string) => {
    const value = latestValue(metric);
    const converted = value !== null && units === "°C" && unit === "F" ? convertCtoF(value) : value;
    return {
      label,
      text: converted === null ? "—" : `${converted.toFixed(1)} ${units === "°C" ? `°${unit}` : units}`,
      note: value === null ? "Reading unavailable." : note,
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
  const metrics: { label: string; text: string; note?: string }[] = [
    {
      label: "Hashrate",
      text: hashrate === null ? "—" : `${hashrate.toLocaleString(undefined, { maximumFractionDigits: 1 })} TH/s`,
    },
    {
      label: "Power",
      text: power === null ? "—" : `${(power / 1000).toFixed(1)} kW`,
      note:
        rollup?.powerCoverage?.kind === "hashboardOnly"
          ? "Hashboard power only; excludes cooling and other plant loads."
          : undefined,
    },
    { label: "Efficiency", text: formatEfficiencyOrDash(latestValue(rollup?.efficiency)) },
    { label: "Temperature", text: temperature },
    ...(showCooling
      ? [
          coolingMetric("Flow", plant?.flowRate, "L/min", "Shared container flow."),
          coolingMetric(
            `${side ? `${side} tank` : "Tank"} inlet temperature`,
            plant?.inletTemperature,
            "°C",
            "Shared tank inlet.",
          ),
          coolingMetric(`${side ? `${side} tank` : "Tank"} outlet temperature`, outlet, "°C"),
          coolingMetric("Fluid level", fluidLevel, "mm"),
        ]
      : []),
  ];
  return (
    <dl className={`grid grid-cols-2 gap-x-6 gap-y-8 ${showCooling ? "tablet:grid-cols-4" : ""}`}>
      {metrics.map(({ label, text, note }) => (
        <div key={label}>
          <dt className="mb-1 text-300 text-text-primary-70">{label}</dt>
          <dd className="text-emphasis-400 text-text-primary tabular-nums tablet:text-heading-200">
            {text}
            {note ? <span className="mt-1 block text-200 font-normal text-text-primary-70">{note}</span> : null}
          </dd>
        </div>
      ))}
    </dl>
  );
};

export default TankMetrics;
