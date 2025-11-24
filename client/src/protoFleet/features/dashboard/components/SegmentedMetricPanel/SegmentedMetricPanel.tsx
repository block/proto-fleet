import { useMemo } from "react";
import { DEFAULT_CHART_HEIGHT } from "./constants";
import type { SegmentedMetricPanelProps } from "./types";
import {
  durationToHours,
  getCurrentBreakdown,
  processMultiDayChartData,
} from "./utils";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import Button, { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import SegmentedBarChart from "@/shared/components/SegmentedBarChart";

export const SegmentedMetricPanel = ({
  title,
  headline,
  headlineGenerator,
  chartData,
  segmentConfig,
  duration,
  className,
}: SegmentedMetricPanelProps) => {
  // Process the chart data - returns array of arrays for multi-day views
  const processedChartData = useMemo(
    () => processMultiDayChartData(chartData, duration, segmentConfig),
    [chartData, duration, segmentConfig],
  );

  // Calculate current breakdown from latest data
  const currentBreakdown = useMemo(
    () => getCurrentBreakdown(chartData, segmentConfig),
    [chartData, segmentConfig],
  );

  // Extract segment keys from config
  const segmentKeys = useMemo(
    () => Object.keys(segmentConfig),
    [segmentConfig],
  );

  // Build color map from config
  const colorMap = useMemo(
    () =>
      Object.entries(segmentConfig).reduce(
        (acc, [key, config]) => {
          acc[key] = config.color;
          return acc;
        },
        {} as Record<string, string>,
      ),
    [segmentConfig],
  );

  // Determine if we're showing multiple charts
  const hours = durationToHours(duration);
  const isMultiDay = hours > 24;

  // Calculate bar width for multi-chart layout
  const barWidth = useMemo(() => {
    if (!isMultiDay) return undefined;

    // Fixed 8px bar width for all multi-day charts
    return 8;
  }, [isMultiDay]);

  // Calculate width percentages for each chart based on number of bars and desired spacing
  // TODO: This isnt working quite right yet
  const chartWidths = useMemo(() => {
    if (!isMultiDay) return ["100%"];

    // We want 4px gaps between bars within each chart
    const targetGap = 4;
    const effectiveBarWidth = 8; // Fixed 8px for multi-day charts

    // Calculate the pixel width needed for each chart
    // Width = numBars * barWidth + (numBars - 1) * gap + padding
    const chartPixelWidths = processedChartData.map((dayData) => {
      const numBars = dayData.length;
      if (numBars === 0) return 0;

      // Calculate minimum width needed for this chart with 4px gaps
      const barsWidth = numBars * effectiveBarWidth;
      const gapsWidth = Math.max(0, (numBars - 1) * targetGap);
      const padding = 20; // Some padding on sides for visual breathing room

      return barsWidth + gapsWidth + padding;
    });

    // Calculate total width needed
    const totalPixelWidth = chartPixelWidths.reduce(
      (sum, width) => sum + width,
      0,
    );

    // Convert to percentages
    return chartPixelWidths.map((pixelWidth) => {
      if (totalPixelWidth === 0) return "0%";
      const widthPercent = (pixelWidth / totalPixelWidth) * 100;
      return `${widthPercent}%`;
    });
  }, [isMultiDay, processedChartData]);

  // Calculate X-axis padding for multi-day charts
  // Since we're using a linear time scale and controlling spacing through chart widths,
  // we just need minimal padding for visual breathing room
  const xAxisPadding = useMemo(() => {
    if (!isMultiDay) return undefined;

    // Small padding value for visual breathing room at chart edges
    // The actual spacing between bars is controlled by the chart width calculations above
    return 10;
  }, [isMultiDay]);

  // Generate headline using the generator function if provided, otherwise use static headline
  const computedHeadline = useMemo(() => {
    if (headlineGenerator && processedChartData.length > 0) {
      return headlineGenerator(processedChartData);
    }
    return headline || "";
  }, [headlineGenerator, processedChartData, headline]);

  const stat = {
    label: title,
    value: computedHeadline,
  };

  return (
    <div
      className={`flex w-full flex-row overflow-hidden rounded-xl bg-surface-base phone:flex-col phone:gap-6 ${className || ""}`}
    >
      {/* Left Panel: ChartWidget with SegmentedBarChart(s) */}
      <ChartWidget stats={stat} className="w-1/2 rounded-none! phone:w-full">
        <div className={`w-full ${isMultiDay ? "flex flex-row" : ""}`}>
          {processedChartData.map((dayData, index) => {
            // Use pre-calculated width for this chart
            const chartWidth = chartWidths[index];

            return (
              <div
                key={index}
                className={isMultiDay ? "flex flex-col" : ""}
                style={{ width: chartWidth, flexShrink: 0 }}
              >
                <SegmentedBarChart
                  chartData={dayData}
                  segmentKeys={segmentKeys}
                  colorMap={colorMap}
                  height={DEFAULT_CHART_HEIGHT}
                  showTooltip={true}
                  animate={false}
                  percentageDisplay={true}
                  xAxisTickInterval={isMultiDay ? (hours <= 48 ? 3 : 2) : 1}
                  yAxisTickCount={4}
                  toolTipKey={null}
                  barWidth={barWidth}
                  xAxisPadding={xAxisPadding}
                  showDateLabel={isMultiDay}
                  lastTickOverride={
                    !isMultiDay && hours < 24 ? "live" : undefined
                  }
                />
              </div>
            );
          })}
        </div>
      </ChartWidget>

      {/* Right Panel: Current Values Breakdown */}
      <div className="flex w-1/2 flex-col justify-between space-y-3 rounded-xl bg-surface-base p-10 phone:w-full phone:gap-4 phone:p-6 phone:pt-0">
        {currentBreakdown.map((segment, idx) => (
          <div
            key={segment.key}
            className="relative flex grow flex-row items-center"
          >
            {/* Icon or color indicator */}
            {segment.icon ? (
              <span
                className="mr-3 flex"
                style={{ color: `var(${segment.color})` }}
              >
                {segment.icon}
              </span>
            ) : (
              <div
                className="mr-3 h-3 w-3 rounded-full"
                style={{ backgroundColor: `var(${segment.color})` }}
              />
            )}

            {/* Label and percentage */}
            <div className="flex flex-1 flex-col">
              <span className="text-400 text-text-primary">
                {segment.label}
              </span>
              <span className="text-text-secondary text-300">
                {segment.percentageLabel}
              </span>
            </div>

            {/* Button with count */}
            <Button
              variant={variants[segment.buttonVariant] || variants.secondary}
              size="compact"
              onClick={() => {}}
              className="pointer-events-none"
            >
              {segment.count} {segment.count === 1 ? "miner" : "miners"}
            </Button>

            {/* Divider between segments (not on last item) */}
            {idx < currentBreakdown.length - 1 && (
              <Divider className="absolute -bottom-4 left-0 w-full" />
            )}
          </div>
        ))}
      </div>
    </div>
  );
};
