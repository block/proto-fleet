import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import { convertAndFormatMeasurement, useChartDataForMetric, useMiner } from "@/protoOS/store";
import { MetricTimeSeries } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

type StatsArgs = MetricTimeSeries["aggregates"] & { lowestPerformer?: string };

const getStats = (stats: StatsArgs): StatProps[] => {
  const { avg, max, min } = stats;

  return [
    {
      label: "Average",
      value: convertAndFormatMeasurement(avg, "kW", false),
      units: "kW",
      size: "small",
    },
    {
      label: "Highest",
      value: convertAndFormatMeasurement(max, "kW", false),
      units: "kW",
      size: "small",
    },
    {
      label: "Lowest",
      value: convertAndFormatMeasurement(min, "kW", false),
      units: "kW",
      size: "small",
    },
  ];
};

const PowerUsage = () => {
  const { chartData, chartLines, xAxisDomain } = useChartDataForMetric("power");
  const miner = useMiner();
  const aggregates = miner?.power?.timeSeries?.aggregates;

  return (
    <>
      {aggregates && chartData.length > 0 ? (
        <ErrorBoundary>
          <Stats stats={getStats(aggregates)} />
          <KpiLineChart chartData={chartData} chartLines={chartLines} units="W" xAxisDomainOverride={xAxisDomain} />
        </ErrorBoundary>
      ) : (
        <div className="flex h-full w-full items-center justify-center">
          <ProgressCircular indeterminate />
        </div>
      )}
    </>
  );
};

export default PowerUsage;
