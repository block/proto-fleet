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
      units: "J/TH",
      size: "small",
    },
    {
      label: "Highest",
      value: max,
      units: "J/TH",
      size: "small",
    },
    {
      label: "Lowest",
      value: min,
      units: "J/TH",
      size: "small",
    },
  ];
};

const Efficiency = () => {
  const {
    minerEfficiency: { efficiency: totalEfficiency, aggregates },
  } = useOutletContext<KpiOutletContext>();

  return (
    <>
      {aggregates && <Stats stats={getStats(aggregates)} />}
      <KpiLineChart
        series={[]}
        units="J/TH"
        aggregateSeries={{
          name: "Total Efficiency",
          data: totalEfficiency,
        }}
      />
    </>
  );
};

export default Efficiency;
