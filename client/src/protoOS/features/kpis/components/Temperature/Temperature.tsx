import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import { getHighestMaximumPerformer } from "@/protoOS/features/kpis/metricSeries";
import { convertAndFormatMeasurement, useChartDataForMetric, useMiner, useTemperatureUnit } from "@/protoOS/store";
import { MetricTimeSeries } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

type StatsArgs = MetricTimeSeries["aggregates"] & { lowestPerformer?: string };

const getStats = (stats: StatsArgs, temperatureUnit: "C" | "F", segmentsLabel: string): StatProps[] => {
  const { avg, max, min, lowestPerformer } = stats;

  const baseStats: StatProps[] = [
    {
      label: "Average",
      value: convertAndFormatMeasurement(avg, temperatureUnit, false),
      units: "°" + temperatureUnit,
      size: "small",
    },
    {
      label: "Highest",
      value: convertAndFormatMeasurement(max, temperatureUnit, false),
      units: "°" + temperatureUnit,
      size: "small",
    },
    {
      label: "Lowest",
      value: convertAndFormatMeasurement(min, temperatureUnit, false),
      units: "°" + temperatureUnit,
      size: "small",
    },
  ];

  if (lowestPerformer) {
    baseStats.push({
      label: `Hottest ${segmentsLabel.slice(0, -1)}`,
      value: lowestPerformer,
      size: "small",
    });
  }

  return baseStats;
};

const Temperature = () => {
  const { chartData, chartLines, segmentsLabel, series, xAxisDomain } = useChartDataForMetric("temperature");
  const miner = useMiner();
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
            chartLines={chartLines}
            series={series}
            segmentsLabel={segmentsLabel}
            units={"°" + temperatureUnit}
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
