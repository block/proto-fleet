import { useOutletContext } from "react-router-dom";

import { StatsArgs } from "../../types";
import KpiLineChart from "@/protoFleet/features/kpis/components/KpiLineChart/KpiLineChart";
import { KpiOutletContext } from "@/protoFleet/features/kpis/types";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

const getStats = (stats: StatsArgs = {}): StatProps[] => {
  const { avg, max, min } = stats;

  return [
    {
      label: "Average",
      value: avg == null ? "N/A" : avg,
      units: "J/TH",
      size: "small",
    },
    {
      label: "Highest",
      value: max == null ? "N/A" : max,
      units: "J/TH",
      size: "small",
    },
    {
      label: "Lowest",
      value: min == null ? "N/A" : min,
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
        units="J/TH"
        chartData={totalEfficiency}
        aggregateKey="totalEfficiency"
      />
    </>
  );
};

export default Efficiency;
