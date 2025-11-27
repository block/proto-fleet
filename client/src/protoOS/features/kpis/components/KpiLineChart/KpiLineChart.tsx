import { useEffect, useMemo } from "react";
import HashboardSelector from "@/protoOS/features/kpis/components/HashboardSelector";
import { aggregateColor, aggregateKey } from "@/protoOS/features/kpis/constants";
import { getHashboardColor } from "@/protoOS/features/kpis/utility";
import {
  useActiveChartLines,
  useBayCount,
  useHashboardHardware,
  useMinerStore,
  useSetActiveChartLines,
} from "@/protoOS/store";
import { HashboardIndicator } from "@/shared/assets/icons";
import LineChart, { type LineChartProps } from "@/shared/components/LineChart";

const ToolTipItemIcon = ({ itemKey }: { itemKey: string }) => {
  const hashboard = useHashboardHardware(itemKey);
  const slot = hashboard?.slot;
  const totalBays = useBayCount();
  const totalSlots = totalBays * 3; // TODO: assume 3 slots per bay for now

  // Don't render icon for aggregate key
  if (itemKey === aggregateKey) {
    return null;
  }

  return (
    <div className="inline-flex items-center gap-2">
      <div className="flex h-5 w-5 items-center justify-center rounded-3xl bg-core-primary-5 text-emphasis-200 text-text-primary">
        {slot ?? ""}
      </div>

      <HashboardIndicator activeHashboardSlot={slot} totalHashboards={totalSlots} />
    </div>
  );
};

// Wrapper component for ProtoOS that uses the shared KpiLineChart component
const KpiLineChart = ({
  chartData,
  chartLines,
  units,
  segmentsLabel = "Hashboards",
}: Omit<LineChartProps, "aggregateKey" | "getSeriesColorMap"> & {
  chartLines: string[];
}) => {
  const activeChartLines = useActiveChartLines() || [];
  const setActiveChartLines = useSetActiveChartLines();

  // Initialize active lines when chart lines change (only if not already set)
  // Also clear active lines when chart lines becomes empty (e.g., when duration changes)
  useEffect(() => {
    if (chartLines.length > 0 && activeChartLines.length === 0) {
      setActiveChartLines(chartLines);
    } else if (chartLines.length === 0 && activeChartLines.length > 0) {
      setActiveChartLines([]);
    }
  }, [chartLines, activeChartLines.length, setActiveChartLines]);

  const colorMap = useMemo(() => {
    return chartLines.reduce(
      (acc, key) => {
        if (key === aggregateKey) {
          acc[key] = aggregateColor;
          return acc;
        }

        const slot = useMinerStore.getState().hardware.getHashboard(key)?.slot;
        if (slot !== undefined) {
          acc[key] = getHashboardColor(slot);
        }

        return acc;
      },
      {} as { [key: string]: string },
    );
  }, [chartLines]);

  return (
    <>
      <div className="scrollbar-hide w-[calc(100%+theme(space.28))] -translate-x-14 overflow-x-auto phone:w-[calc(100%+theme(space.12))] phone:-translate-x-6 tablet:w-[calc(100%+theme(space.20))] tablet:-translate-x-10">
        <HashboardSelector
          chartLines={chartLines}
          setActiveChartLines={setActiveChartLines}
          activeChartLines={activeChartLines}
          aggregateKey={aggregateKey}
          className={"px-14 phone:px-6 tablet:px-10"}
        />
      </div>
      <LineChart
        chartData={chartData}
        aggregateKey={aggregateKey}
        activeKeys={activeChartLines}
        colorMap={colorMap}
        toolTipItemIcon={ToolTipItemIcon}
        units={units}
        segmentsLabel={segmentsLabel}
      />
    </>
  );
};

export default KpiLineChart;
