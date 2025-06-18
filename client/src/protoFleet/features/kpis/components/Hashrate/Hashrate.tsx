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
      units: "TH/s",
      size: "small",
    },
    {
      label: "Highest",
      value: max,
      units: "TH/s",
      size: "small",
    },
    {
      label: "Lowest",
      value: min,
      units: "TH/s",
      size: "small",
    },
  ];
};

const Hashrate = () => {
  const {
    minerHashrate: { hashrate: totalHashrate, aggregates },
  } = useOutletContext<KpiOutletContext>();

  return (
    <>
      {aggregates && <Stats stats={getStats(aggregates)} />}
      <KpiLineChart
        series={[]}
        units="TH/s"
        aggregateSeries={{
          name: "Total Hashrate",
          data: totalHashrate,
        }}
      />
    </>
  );
};

export default Hashrate;
