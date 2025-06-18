import { useOutletContext } from "react-router-dom";

import KpiLineChart from "@/protoFleet/features/kpis/components/KpiLineChart/KpiLineChartWrapper";
import { KpiOutletContext } from "@/protoFleet/features/kpis/types";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/features/kpis/components/Stats";
import { AggregateStats } from "@/shared/features/kpis/types";

type StatsArgs = AggregateStats & { lowestPerformer?: string };

const getStats = (stats: StatsArgs = {}): StatProps[] => {
  const { avg, max, min } = stats;

  return [
    {
      label: "Average",
      value: avg,
      units: "kW",
      size: "small",
    },
    {
      label: "Highest",
      value: max,
      units: "kW",
      size: "small",
    },
    {
      label: "Lowest",
      value: min,
      units: "kW",
      size: "small",
    },
  ];
};

const PowerUsage = () => {
  const {
    minerPowerUsage: { powerUsage: totalPowerUsage, aggregates },
  } = useOutletContext<KpiOutletContext>();

  return (
    <>
      {aggregates && <Stats stats={getStats(aggregates)} />}
      <KpiLineChart
        series={[]}
        units="kW"
        aggregateSeries={{
          name: "Total Power Usage",
          data: totalPowerUsage,
        }}
      />
    </>
  );
};

export default PowerUsage;
