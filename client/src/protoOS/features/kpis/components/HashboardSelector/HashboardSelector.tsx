import ChartSeriesSelector from "@/protoOS/features/kpis/components/ChartSeriesSelector";
import { getHashboardColor } from "@/protoOS/features/kpis/utility";
import { useMinerStore } from "@/protoOS/store";

type HashboardSelectorProps = {
  chartLines: string[];
  setActiveChartLines: (serials: string[]) => void;
  activeChartLines: string[];
  aggregateKey: string;
  className?: string;
};

const HashboardSelector = ({
  chartLines,
  setActiveChartLines,
  activeChartLines,
  aggregateKey,
  className = "",
}: HashboardSelectorProps) => {
  const series = chartLines
    .filter((key) => key !== aggregateKey)
    .flatMap((key) => {
      const slot = useMinerStore.getState().hardware.getHashboard(key)?.slot;
      return slot === undefined ? [] : [{ key, label: String(slot), color: getHashboardColor(slot) }];
    });

  return (
    <ChartSeriesSelector
      activeKeys={activeChartLines}
      aggregateKey={aggregateKey}
      className={className}
      onChange={setActiveChartLines}
      segmentsLabel="Hashboards"
      series={series}
    />
  );
};

export default HashboardSelector;
