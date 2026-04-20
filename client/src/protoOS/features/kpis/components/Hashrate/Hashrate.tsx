import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import {
  convertAndFormatMeasurement,
  useChartDataForMetric,
  useMiner,
  useMinerHashboards,
  useMinerStore,
} from "@/protoOS/store";
import { MetricTimeSeries } from "@/protoOS/store";
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
    {
      label: "Lowest Performer",
      value: lowestPerformer,
      size: "small",
    },
  ];
};

const Hashrate = () => {
  const { chartData, chartLines, xAxisDomain } = useChartDataForMetric("hashrate");
  const miner = useMiner();
  const hashboards = useMinerHashboards();
  const aggregates = miner?.hashrate?.timeSeries?.aggregates;

  const lowestPerformer = useMemo(() => {
    if (!hashboards) return undefined;

    let lowestSlot: number | undefined;
    let lowestAvg = Infinity;

    hashboards.forEach((hashboard) => {
      const hashboardAvg = hashboard.hashrate?.timeSeries?.aggregates?.avg?.value;
      if (!!hashboardAvg && hashboardAvg < lowestAvg) {
        lowestAvg = hashboardAvg;
        lowestSlot = useMinerStore.getState().hardware.getSlotByHbSn(hashboard.serial);
      }
    });

    return lowestSlot ? "Hashboard " + lowestSlot : undefined;
  }, [hashboards]);

  return (
    <>
      {aggregates && chartData.length > 0 ? (
        <ErrorBoundary>
          <Stats stats={getStats({ ...aggregates, lowestPerformer })} />
          <KpiLineChart chartData={chartData} chartLines={chartLines} units="TH/s" xAxisDomainOverride={xAxisDomain} />
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
