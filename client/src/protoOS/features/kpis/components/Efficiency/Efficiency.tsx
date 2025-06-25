import { useOutletContext } from "react-router-dom";

import { useProcessedHashboardEfficiencies } from "../../hooks";
import KpiLineChart from "../KpiLineChart";
import { KpiOutletContext } from "@/protoOS/features/kpis/types";
import { type StatProps } from "@/shared/components/Stat";
import { AggregateStats } from "@/shared/features/kpis";
import Stats from "@/shared/features/kpis/components/Stats";

type StatsArgs = AggregateStats & { lowestPerformer?: string };

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
  } = useOutletContext<KpiOutletContext>();

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
      <KpiLineChart
        series={hbEfficiencies}
        units="J/TH"
        aggregateSeries={{
          name: "Average Efficiency",
          data: totalEfficiency,
        }}
      />
    </>
  );
};

export default Efficiency;
