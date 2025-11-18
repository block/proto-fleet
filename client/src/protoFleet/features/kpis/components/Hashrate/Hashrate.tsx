import { useOutletContext } from "react-router-dom";

import { StatsArgs } from "../../types";
import KpiLineChart from "@/protoFleet/features/kpis/components/KpiLineChart/KpiLineChart";
import { KpiOutletContext } from "@/protoFleet/features/kpis/types";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

const getStats = (stats: StatsArgs = {}): StatProps[] => {
  const { avg, max, min } = stats;

  const avgFormatted = formatHashrateWithUnit(avg ?? 0);
  const maxFormatted = formatHashrateWithUnit(max ?? 0);
  const minFormatted = formatHashrateWithUnit(min ?? 0);

  // Use the unit from avg (they should all be the same after formatting)
  const units = avgFormatted.unit === "PH/S" ? "PH/s" : "TH/s";

  return [
    {
      label: "Average",
      value: avgFormatted.value,
      units: units,
      size: "small",
    },
    {
      label: "Highest",
      value: maxFormatted.value,
      units: units,
      size: "small",
    },
    {
      label: "Lowest",
      value: minFormatted.value,
      units: units,
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
        chartData={totalHashrate}
        aggregateKey="value"
        units="TH/s"
      />
    </>
  );
};

export default Hashrate;
