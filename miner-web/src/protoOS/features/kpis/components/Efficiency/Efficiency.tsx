import { useOutletContext } from "react-router-dom";

import { useProcessedHashboardEfficiencies } from "../../hooks";
import { type OutletContext } from "../../types";
import KpiLineChart from "../KpiLineChart";
import { Aggregates } from "@/protoOS/api/types";
import Stats from "@/protoOS/features/kpis/components/Stats";
import { type StatProps } from "@/shared/components/Stat";

type StatsArgs = Aggregates & { lowestPerformer?: string };

const getStats = (stats: StatsArgs = {}): StatProps[] => {
  const { avg, max, min, lowestPerformer } = stats;

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
    {
      label: "Lowest Performer",
      value: lowestPerformer,
      size: "small",
    },
  ];
};

const Efficiency = () => {
  const {
    minerEfficiency: { efficiency: totalEfficiency, aggregates },
    duration,
    hashboardSerials,
  } = useOutletContext<OutletContext>();

  const { efficiencies: hbEfficiencies, lowestPerformer } =
    useProcessedHashboardEfficiencies({
      serials: hashboardSerials,
      duration,
    });

  return (
    <>
      {aggregates && (
        <Stats stats={getStats({ ...aggregates, lowestPerformer })} />
      )}
      <div className="h-[486px]">
        <KpiLineChart
          duration={duration}
          series={hbEfficiencies}
          units="J/TH"
          aggregateSeries={{
            name: "Total Efficiency",
            data: totalEfficiency,
          }}
        />
      </div>
    </>
  );
};

export default Efficiency;
