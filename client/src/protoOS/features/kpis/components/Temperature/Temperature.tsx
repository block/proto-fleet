import { useMemo } from "react";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/KpiLineChart";
import {
  convertAndFormatMeasurement,
  useChartDataForMetric,
  useMiner,
  useMinerHashboards,
  useMinerStore,
  useTemperatureUnit,
} from "@/protoOS/store";
import { MetricTimeSeries } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

type StatsArgs = MetricTimeSeries["aggregates"] & { lowestPerformer?: string };

const getStats = (stats: StatsArgs, temperatureUnit: "C" | "F"): StatProps[] => {
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
      label: "Hottest Hashboard",
      value: lowestPerformer,
      size: "small",
    });
  }

  return baseStats;
};

const Temperature = () => {
  const { chartData, chartLines, xAxisDomain } = useChartDataForMetric("temperature");
  const miner = useMiner();
  const hashboards = useMinerHashboards();
  const temperatureUnit = useTemperatureUnit();
  const aggregates = miner?.temperature?.timeSeries?.aggregates;

  const lowestPerformer = useMemo(() => {
    if (!hashboards) return undefined;

    let hottestSlot: number | undefined;
    let hottestTemp = -Infinity;

    hashboards.forEach((hashboard) => {
      const hashboardMax = hashboard.temperature?.timeSeries?.aggregates?.max?.value;
      if (!!hashboardMax && hashboardMax > hottestTemp) {
        hottestTemp = hashboardMax;
        hottestSlot = useMinerStore.getState().hardware.getSlotByHbSn(hashboard.serial);
      }
    });

    return hottestSlot ? "Hashboard " + hottestSlot : undefined;
  }, [hashboards]);

  return (
    <>
      {aggregates && chartData.length > 0 ? (
        <ErrorBoundary>
          <Stats stats={getStats({ ...aggregates, lowestPerformer }, temperatureUnit)} />
          <KpiLineChart
            chartData={chartData}
            chartLines={chartLines}
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
