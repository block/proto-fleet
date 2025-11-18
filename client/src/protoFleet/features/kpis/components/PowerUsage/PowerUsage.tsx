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
      value: avg === null ? "N/A" : avg,
      units: "kW",
      size: "small",
    },
    {
      label: "Highest",
      value: max === null ? "N/A" : max,
      units: "kW",
      size: "small",
    },
    {
      label: "Lowest",
      value: min === null ? "N/A" : min,
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
        chartData={totalPowerUsage}
        units="kW"
        aggregateKey="value"
      />
    </>
  );
};

export default PowerUsage;
