import { useOutletContext } from "react-router-dom";
import { useProcessedHashboardHashrates } from "../hooks";
import KpiLineChart from "../KpiLineChart/KpiLineChart";
import { OutletContext } from "../types";
import { Aggregates } from "@/protoOS/api/types";
import Stats from "@/protoOS/features/kpis/Stats";
import { type StatProps } from "@/shared/components/Stat";

type StatsArgs = Aggregates & { lowestPerformer?: string };

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
  } = useOutletContext<OutletContext>();

  const { hashrates: hbHashrates, lowestPerformer } =
    useProcessedHashboardHashrates({
      serials: hashboardSerials,
      duration,
    });

  return (
    <>
      {aggregates && (
        <Stats stats={getStats({ ...aggregates, lowestPerformer })} />
      )}

      <div className="h-[400px]">
        <KpiLineChart
          duration={duration}
          series={hbHashrates}
          units="TH/s"
          aggregateSeries={{
            name: "Total Hashrate",
            data: totalHashrates,
          }}
        />
      </div>
    </>
  );
};

export default Hashrate;
