import { useMemo } from "react";
import { useModuleHistory } from "../hooks/useModuleHistory";
import { emptyModulePoint, moduleHistoryChart } from "../moduleHistoryChart";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { useDuration, useSetDuration, useTemperatureUnit } from "@/protoFleet/store";
import Button from "@/shared/components/Button";
import DurationSelector, { fleetDurations, getFleetDurationMs } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

export default function ModulePerformance({ moduleId, enabled }: { moduleId: bigint; enabled: boolean }) {
  const duration = useDuration();
  const setDuration = useSetDuration();
  const unit = useTemperatureUnit();
  const { samples, endTime, error, retry } = useModuleHistory(moduleId, duration, enabled);
  const points = useMemo(() => {
    if (!samples?.length || endTime === undefined) return [];
    const data = moduleHistoryChart(samples, duration, unit);
    if (!data.length) return [];
    const end = endTime;
    const start = end - getFleetDurationMs(duration);
    return [...(data[0].datetime > start ? [emptyModulePoint(start)] : []), ...data, emptyModulePoint(end)];
  }, [samples, endTime, duration, unit]);
  const charts = [
    { key: "hashrate", label: "Hashrate", units: "TH/s" },
    { key: "temperature", label: "Temperature", units: `°${unit}` },
    { key: "power", label: "Equipment power", units: "kW" },
    { key: "efficiency", label: "Efficiency", units: "J/TH" },
  ];
  return (
    <section aria-label="Performance" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-heading-200">Performance</h2>
        <DurationSelector className="flex-wrap" duration={duration} durations={fleetDurations} onSelect={setDuration} />
      </div>
      {!enabled ? (
        <p role="status">History unavailable until module access can be refreshed.</p>
      ) : error ? (
        <div>
          <p role="alert">Could not load history: {error}</p>
          <Button variant="secondary" text="Retry history" onClick={retry} />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 tablet:grid-cols-2">
          {charts.map(({ key, label, units }) => {
            const latest = [...points].reverse().find((point) => point[key] !== null)?.[key];
            return (
              <ChartWidget
                surface="overlay"
                key={key}
                stats={{
                  label,
                  value:
                    samples === undefined
                      ? undefined
                      : latest === undefined || latest === null
                        ? "No data"
                        : latest.toFixed(1),
                  units: latest == null ? "" : units,
                }}
              >
                {samples === undefined ? (
                  <SkeletonBar className="h-60 w-full" />
                ) : latest == null ? null : (
                  <LineChart
                    chartData={points}
                    aggregateKey={key}
                    activeKeys={[key]}
                    tooltipKeys={[key]}
                    colorMap={{ [key]: "--color-core-primary-fill" }}
                    units={units}
                    duration={duration}
                    connectNulls={false}
                    heightClass="h-60"
                  />
                )}
              </ChartWidget>
            );
          })}
        </div>
      )}
    </section>
  );
}
