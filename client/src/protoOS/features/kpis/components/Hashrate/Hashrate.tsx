import { useOutletContext } from "react-router-dom";
import KpiLineChart from "@/protoOS/features/kpis/components/KpiLineChart/index.ts";
import { useProcessedHashboardHashrates } from "@/protoOS/features/kpis/hooks/index.ts";
import { KpiOutletContext } from "@/protoOS/features/kpis/types";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
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
    {
      label: "Lowest Performer",
      value: lowestPerformer,
      size: "small",
    },
  ];
};

const Hashrate = () => {
  const {
    minerHashrate: { hashrate: totalHashrates, aggregates },
    duration,
    hashboardSerials,
  } = useOutletContext<KpiOutletContext>();

  const { hashrates: hbHashrates, lowestPerformer } =
    useProcessedHashboardHashrates({
      serials: hashboardSerials,
      duration,
    });

  return (
    <>
      {aggregates && (
        <ErrorBoundary>
          <Stats stats={getStats({ ...aggregates, lowestPerformer })} />
        </ErrorBoundary>
      )}
      <ErrorBoundary>
        <KpiLineChart
          series={hbHashrates}
          units="TH/s"
          aggregateSeries={{
            name: "Total Hashrate",
            data: totalHashrates,
          }}
        />
      </ErrorBoundary>
    </>
  );
};

export default Hashrate;
