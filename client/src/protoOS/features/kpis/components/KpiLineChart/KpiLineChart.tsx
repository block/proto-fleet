import { useEffect, useMemo, useRef } from "react";
import ChartSeriesSelector from "@/protoOS/features/kpis/components/ChartSeriesSelector";
import { aggregateColor, aggregateKey } from "@/protoOS/features/kpis/constants";
import { getHashboardColor } from "@/protoOS/features/kpis/utility";
import {
  type KpiChartSeries,
  useActiveChartLines,
  useBayCount,
  useHashboardHardware,
  useMinerStore,
  useSetActiveChartLines,
} from "@/protoOS/store";
import { HashboardIndicator } from "@/shared/assets/icons";
import LineChart, { type LineChartProps } from "@/shared/components/LineChart";

interface ToolTipItemIconProps {
  itemKey: string;
  series: KpiChartSeries[];
  segmentsLabel: string;
}

const ToolTipItemIcon = ({ itemKey, series, segmentsLabel }: ToolTipItemIconProps) => {
  const hashboard = useHashboardHardware(itemKey);
  const totalHashboards = useBayCount() * 3; // TODO: assume 3 slots per bay for now

  if (itemKey === aggregateKey) return null;
  const item = series.find(({ key }) => key === itemKey);
  if (!item) return null;

  if (segmentsLabel === "hashboards") {
    const slot = hashboard?.slot;
    return (
      <div className="inline-flex items-center gap-2">
        <div className="flex h-5 w-5 items-center justify-center rounded-3xl bg-core-primary-5 text-emphasis-200 text-text-primary">
          {slot ?? ""}
        </div>
        <HashboardIndicator activeHashboardSlot={slot} totalHashboards={totalHashboards} />
      </div>
    );
  }

  return (
    <div className="inline-flex items-center gap-2">
      <div className="flex h-5 w-5 items-center justify-center rounded-3xl bg-core-primary-5 text-emphasis-200 text-text-primary">
        {item.label}
      </div>
    </div>
  );
};

// Wrapper component for ProtoOS that uses the shared KpiLineChart component
const KpiLineChart = ({
  chartData,
  seriesTimestamps,
  chartLines,
  series,
  units,
  segmentsLabel = "hashboards",
  xAxisDomainOverride,
}: Omit<LineChartProps, "aggregateKey" | "getSeriesColorMap"> & {
  chartLines: string[];
  series?: KpiChartSeries[];
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

  const resolvedSeries = useMemo<KpiChartSeries[]>(() => {
    if (series) return series;
    return chartLines
      .filter((key) => key !== aggregateKey)
      .map((key, index) => {
        const slot = useMinerStore.getState().hardware.getHashboard(key)?.slot ?? index + 1;
        return { key, label: String(slot), name: `Hashboard ${slot}`, colorIndex: slot };
      });
  }, [chartLines, series]);

  const colorMap = useMemo(() => {
    return resolvedSeries.reduce(
      (acc, key) => {
        acc[key.key] = getHashboardColor(key.colorIndex);
        return acc;
      },
      { [aggregateKey]: aggregateColor } as { [key: string]: string },
    );
  }, [resolvedSeries]);

  const tooltipItemIcon = useMemo(
    () => (props: { itemKey: string }) => (
      <ToolTipItemIcon {...props} series={resolvedSeries} segmentsLabel={segmentsLabel} />
    ),
    [resolvedSeries, segmentsLabel],
  );

  return (
    <>
      <div className="scrollbar-hide w-[calc(100%+theme(space.12))] -translate-x-6 overflow-x-auto tablet:w-[calc(100%+theme(space.20))] tablet:-translate-x-10 laptop:w-[calc(100%+theme(space.28))] laptop:-translate-x-14">
        <ChartSeriesSelector
          series={resolvedSeries.map(({ key, label, colorIndex }) => ({
            key,
            label,
            color: getHashboardColor(colorIndex),
          }))}
          onChange={setActiveChartLines}
          activeKeys={activeChartLines}
          aggregateKey={aggregateKey}
          segmentsLabel={segmentsLabel}
          className={"px-6 tablet:px-10 laptop:px-14"}
        />
      </div>
      <LineChart
        chartData={chartData}
        seriesTimestamps={seriesTimestamps}
        aggregateKey={aggregateKey}
        activeKeys={activeChartLines.length === 0 ? chartLines : activeChartLines}
        colorMap={colorMap}
        toolTipItemIcon={tooltipItemIcon}
        units={units}
        segmentsLabel={segmentsLabel.charAt(0).toUpperCase() + segmentsLabel.slice(1)}
        tooltipXOffset={60}
        xAxisDomainOverride={xAxisDomainOverride}
      />
    </>
  );
};

export default KpiLineChart;
