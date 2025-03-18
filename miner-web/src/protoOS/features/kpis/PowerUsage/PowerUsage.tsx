import { useOutletContext } from "react-router-dom";

import { useProcessedHashboardPowerUsages } from "../hooks";
import KpiLineChart from "../KpiLineChart/KpiLineChart";
import { type OutletContext } from "../types";
import { Aggregates } from "@/protoOS/api/types";
import Stats from "@/protoOS/features/kpis/Stats";
import { type StatProps } from "@/shared/components/Stat";

type StatsArgs = Aggregates & { lowestPerformer?: string };

const getStats = (stats: StatsArgs = {}): StatProps[] => {
  const { avg, max, min } = stats;

  return [
    {
      label: "Average",
      value: avg,
      units: "kW/h",
      size: "small",
    },
    {
      label: "Highest",
      value: max,
      units: "kW/h",
      size: "small",
    },
    {
      label: "Lowest",
      value: min,
      units: "kW/h",
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
      <div className="h-[400px]">
        <KpiLineChart
          duration={duration}
          series={hbPowerUsages}
          units="kW"
          aggregateSeries={{
            name: "Total Power Usage",
            data: totalPowerUsage,
          }}
        />
      </div>
    </>
  );
};

export default PowerUsage;
