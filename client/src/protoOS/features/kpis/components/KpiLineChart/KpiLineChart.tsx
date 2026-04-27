import { useEffect, useMemo, useRef } from "react";
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
  xAxisDomainOverride,
}: Omit<LineChartProps, "aggregateKey" | "getSeriesColorMap"> & {
  chartLines: string[];
}) => {
  const activeChartLines = useActiveChartLines() || [];
  const setActiveChartLines = useSetActiveChartLines();

  // Track previous chartLines to detect actual content changes (not just reference changes)
  const prevChartLinesRef = useRef<string[]>([]);

  // Clear active lines when chart lines becomes empty (e.g., when duration changes)
  // This resets to default state (show all) when data is reloading
  // Also filter out any stale serials that are no longer in chartLines
  useEffect(() => {
    // Compare array contents, not references, to avoid infinite loops
    const chartLinesChanged =
      chartLines.length !== prevChartLinesRef.current.length ||
      chartLines.some((line, i) => line !== prevChartLinesRef.current[i]);

    if (!chartLinesChanged) {
      return;
    }

    prevChartLinesRef.current = chartLines;

    const currentActiveLines = activeChartLines;

    if (chartLines.length === 0 && currentActiveLines.length > 0) {
      setActiveChartLines([]);
      return;
    }

    if (chartLines.length > 0 && currentActiveLines.length > 0) {
      // Remove any active lines that are no longer in chartLines (e.g., hashboard went offline)
      const validActiveLines = currentActiveLines.filter((serial) => chartLines.includes(serial));
      // Only update if there are stale serials to remove
      if (validActiveLines.length !== currentActiveLines.length) {
        setActiveChartLines(validActiveLines.length > 0 ? validActiveLines : []);
      }
    }
    // Note: We intentionally use activeChartLines.length instead of activeChartLines in dependencies
    // to avoid infinite loops caused by array reference changes. We capture the current array value
    // inside the effect and only re-run when the length changes or chartLines changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
      <div className="scrollbar-hide w-[calc(100%+theme(space.12))] -translate-x-6 overflow-x-auto tablet:w-[calc(100%+theme(space.20))] tablet:-translate-x-10 laptop:w-[calc(100%+theme(space.28))] laptop:-translate-x-14">
        <HashboardSelector
          chartLines={chartLines}
          setActiveChartLines={setActiveChartLines}
          activeChartLines={activeChartLines}
          aggregateKey={aggregateKey}
          className={"px-6 tablet:px-10 laptop:px-14"}
        />
      </div>
      <LineChart
        chartData={chartData}
        aggregateKey={aggregateKey}
        activeKeys={activeChartLines.length === 0 ? chartLines : activeChartLines}
        colorMap={colorMap}
        toolTipItemIcon={ToolTipItemIcon}
        units={units}
        segmentsLabel={segmentsLabel}
        tooltipXOffset={60}
        xAxisDomainOverride={xAxisDomainOverride}
      />
    </>
  );
};

export default KpiLineChart;
