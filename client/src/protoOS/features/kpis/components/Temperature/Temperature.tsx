import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import { getHighestMaximumPerformer } from "@/protoOS/features/kpis/metricSeries";
import { useKpiMetricData } from "@/protoOS/features/kpis/useKpiMetricData";
import { convertAndFormatMeasurement, temperatureDisplayUnit, useTemperatureUnit } from "@/protoOS/store";
import type { MetricTimeSeries } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

type StatsArgs = MetricTimeSeries["aggregates"] & { lowestPerformer?: string };

const singularTitleCase = (segmentsLabel: string) => {
  const singular = segmentsLabel.endsWith("s") ? segmentsLabel.slice(0, -1) : segmentsLabel;
  return singular.charAt(0).toUpperCase() + singular.slice(1);
};

const getStats = (stats: StatsArgs, temperatureUnit: "C" | "F", segmentsLabel: string): StatProps[] => {
  const { avg, max, min, lowestPerformer } = stats;
  const displayUnit = temperatureDisplayUnit(temperatureUnit);

  const baseStats: StatProps[] = [
    {
      label: "Average",
      value: convertAndFormatMeasurement(avg, displayUnit, false),
      units: displayUnit,
      size: "small",
    },
    {
      label: "Highest",
      value: convertAndFormatMeasurement(max, displayUnit, false),
      units: displayUnit,
      size: "small",
    },
    {
      label: "Lowest",
      value: convertAndFormatMeasurement(min, displayUnit, false),
      units: displayUnit,
      size: "small",
    },
  ];

  if (lowestPerformer) {
    baseStats.push({
      label: `Hottest ${singularTitleCase(segmentsLabel)}`,
      value: lowestPerformer,
      size: "small",
    });
  }

  return baseStats;
};

const Temperature = () => {
  const { chartData, chartLines, seriesTimestamps, miner, segmentsLabel, series, xAxisDomain } =
    useKpiMetricData("temperature");
  const temperatureUnit = useTemperatureUnit();
  const aggregates = miner?.temperature?.timeSeries?.aggregates;

  const lowestPerformer = useMemo(() => getHighestMaximumPerformer(series), [series]);

  return (
    <>
      {aggregates && chartData.length > 0 ? (
        <ErrorBoundary>
          <Stats stats={getStats({ ...aggregates, lowestPerformer }, temperatureUnit, segmentsLabel)} />
          <KpiLineChart
            chartData={chartData}
            seriesTimestamps={seriesTimestamps}
            chartLines={chartLines}
            series={series}
            segmentsLabel={segmentsLabel}
            units={temperatureDisplayUnit(temperatureUnit)}
            xAxisDomainOverride={xAxisDomain}
          />
        </ErrorBoundary>
      ) : (
        <div className="flex h-full w-full items-center justify-center">
          <ProgressCircular indeterminate />
        </div>
      )}
    </>
  );
};

export default Temperature;
