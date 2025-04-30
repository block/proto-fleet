import { useOutletContext } from "react-router-dom";

import { useProcessedHashboardPowerUsages } from "../../hooks";
import { type OutletContext } from "../../types";
import KpiLineChart from "../KpiLineChart/KpiLineChart";
import { Aggregates } from "@/protoOS/api/types";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/features/kpis/components/Stats";

type StatsArgs = Aggregates & { lowestPerformer?: string };

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
    duration,
    hashboardSerials,
  } = useOutletContext<OutletContext>();

  const hbPowerUsages = useProcessedHashboardPowerUsages({
    serials: hashboardSerials,
    duration,
  });

  return (
    <>
      {aggregates && <Stats stats={getStats(aggregates)} />}
      <KpiLineChart
        series={hbPowerUsages}
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
