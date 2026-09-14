import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import { getLowestAveragePerformer } from "@/protoOS/features/kpis/metricSeries";
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
      value: convertAndFormatMeasurement(avg, "TH/s", false),
      units: "TH/s",
      size: "small",
    },
    {
      label: "Highest",
      value: convertAndFormatMeasurement(max, "TH/s", false),
      units: "TH/s",
      size: "small",
    },
    {
      label: "Lowest",
      value: convertAndFormatMeasurement(min, "TH/s", false),
      units: "TH/s",
      size: "small",
    },
    { label: "Lowest Performer", value: lowestPerformer, size: "small" },
  ];
};

const Hashrate = () => {
  const { chartData, chartLines, miner, segmentsLabel, series, xAxisDomain } = useKpiMetricData("hashrate");
  const aggregates = miner?.hashrate?.timeSeries?.aggregates;

  const lowestPerformer = useMemo(() => getLowestAveragePerformer(series), [series]);

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
            units="TH/s"
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

export default Hashrate;
