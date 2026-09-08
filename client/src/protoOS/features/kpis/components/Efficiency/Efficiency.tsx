import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import { getHighestAveragePerformer } from "@/protoOS/features/kpis/metricSeries";
import { useKpiMetricData } from "@/protoOS/features/kpis/useKpiMetricData";
import { convertAndFormatMeasurement, type MetricTimeSeries } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

type StatsArgs = MetricTimeSeries["aggregates"] & { lowestPerformer?: string };

const getStats = (stats: StatsArgs): StatProps[] => {
  const { avg, max, min, lowestPerformer } = stats;

  return [
    {
      label: "Average",
      value: convertAndFormatMeasurement(avg, "J/TH", false),
      units: "J/TH",
      size: "small",
    },
    {
      label: "Highest",
      value: convertAndFormatMeasurement(max, "J/TH", false),
      units: "J/TH",
      size: "small",
    },
    {
      label: "Lowest",
      value: convertAndFormatMeasurement(min, "J/TH", false),
      units: "J/TH",
      size: "small",
    },
    {
      label: "Lowest performer",
      value: lowestPerformer,
      size: "small",
    },
  ];
};

const Efficiency = () => {
  const { chartData, chartLines, miner, segmentsLabel, series, xAxisDomain } = useKpiMetricData("efficiency");
  const aggregates = miner?.efficiency?.timeSeries?.aggregates;

  // Higher J/TH means less efficient, so the highest average is the lowest performer.
  const lowestPerformer = useMemo(() => getHighestAveragePerformer(series), [series]);

  return (
    <>
      {aggregates && chartData.length > 0 ? (
        <ErrorBoundary>
          <Stats stats={getStats({ ...aggregates, lowestPerformer })} />
          <KpiLineChart
            chartData={chartData}
            chartLines={chartLines}
            series={series}
            segmentsLabel={segmentsLabel}
            units="J/TH"
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

export default Efficiency;
