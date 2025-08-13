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
      units: "°C",
      size: "small",
    },
    {
      label: "Highest",
      value: max,
      units: "°C",
      size: "small",
    },
    {
      label: "Lowest",
      value: min,
      units: "°C",
      size: "small",
    },
  ];
};

const Temperature = () => {
  const {
    minerTemperature: { temperature: totalTemperature, aggregates },
  } = useOutletContext<KpiOutletContext>();

  return (
    <>
      {aggregates && <Stats stats={getStats(aggregates)} />}
      <KpiLineChart
        series={[]}
        units="°C"
        aggregateSeries={{
          name: "Average Temperature",
          data: totalTemperature,
        }}
      />
    </>
  );
};

export default Temperature;
