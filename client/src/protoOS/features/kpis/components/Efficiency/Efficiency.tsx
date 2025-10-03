import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart";
import {
  convertAndFormatMeasurement,
  useChartDataForMetric,
  useMiner,
  useMinerHashboards,
  useMinerStore,
} from "@/protoOS/store";
import { MetricTimeSeries } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
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
      label: "Lowest Performer",
      value: lowestPerformer,
      size: "small",
    },
  ];
};

const Efficiency = () => {
  const { chartData, chartLines } = useChartDataForMetric("efficiency");
  const miner = useMiner();
  const hashboards = useMinerHashboards();
  const aggregates = miner?.efficiency?.aggregates;

  const lowestPerformer = useMemo(() => {
    if (!hashboards) return undefined;

    let lowestSlot: number | undefined;
    let lowestAvg = -Infinity; // For efficiency, lower is worse, so we want highest value (worst efficiency)

    hashboards.forEach((hashboard) => {
      const hashboardAvg = hashboard.efficiency?.aggregates?.avg?.value;
      if (!!hashboardAvg && hashboardAvg > lowestAvg) {
        lowestAvg = hashboardAvg;
        lowestSlot = useMinerStore
          .getState()
          .hardware.getSlotByHbSn(hashboard.serial);
      }
    });

    return lowestSlot ? "Hashboard " + lowestSlot : undefined;
  }, [hashboards]);

  return (
    <>
      {aggregates && (
        <ErrorBoundary>
          <Stats stats={getStats({ ...aggregates, lowestPerformer })} />
        </ErrorBoundary>
      )}
      <ErrorBoundary>
        <KpiLineChart
          chartData={chartData}
          chartLines={chartLines}
          units="J/TH"
        />
      </ErrorBoundary>
    </>
  );
};

export default Efficiency;
