import type { LevelRollup, MetricTelemetry } from "@/protoOS/store/contract";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import { convertCtoF, formatEfficiencyOrDash, formatTempRange } from "@/shared/utils/telemetryFormat";

const latestValue = (metric: MetricTelemetry | undefined) => {
  const number = metric?.latest?.value;
  return number !== undefined && number !== null && Number.isFinite(number) ? number : null;
};

const TankMetrics = ({ rollup, showCooling = false }: { rollup?: LevelRollup; showCooling?: boolean }) => {
  const unit = useTemperatureUnit();
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
  const metrics = [
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
      ? ["Flow", "Left tank inlet temperature", "Left tank outlet temperature", "Fluid level"].map((label) => ({
          label,
          text: "—",
          note: "Reading unavailable.",
        }))
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
