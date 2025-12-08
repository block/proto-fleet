import { useMemo } from "react";
import clsx from "clsx";

import { DEFAULT_CHART_HEIGHT } from "./constants";
import type { SegmentedMetricPanelProps } from "./types";
import { durationToHours, getCurrentBreakdown, processMultiDayChartData } from "./utils";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import Button, { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import SegmentedBarChart from "@/shared/components/SegmentedBarChart";

// Constants for bar chart display
const MULTI_DAY_BAR_WIDTH = {
  desktop: 8,
  laptop: 6,
  tablet: 8,
  phone: 6,
};

const MULTI_DAY_BAR_GAP = {
  desktop: 4,
  laptop: 2,
  tablet: 2,
  phone: 2,
};

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
  const currentBreakdown = useMemo(() => getCurrentBreakdown(chartData, segmentConfig), [chartData, segmentConfig]);

  // Extract segment keys from config
  const segmentKeys = useMemo(() => Object.keys(segmentConfig), [segmentConfig]);

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
    return MULTI_DAY_BAR_WIDTH;
  }, [isMultiDay]);

  // Calculate equal chart widths for multi-day view
  const chartWidths = useMemo(() => {
    if (!isMultiDay) return ["100%"];

    const numberOfCharts = processedChartData.length;
    const chartWidthPercentage = `${100 / numberOfCharts}%`;
    return processedChartData.map(() => chartWidthPercentage);
  }, [isMultiDay, processedChartData]);

  // Generate headline using the generator function if provided, otherwise use static headline
  const computedHeadline = useMemo(() => {
    if (headlineGenerator && processedChartData.length > 0) {
      return headlineGenerator(processedChartData);
    }
    return headline || "";
  }, [headlineGenerator, processedChartData, headline]);

  // Check if we have no data
  const hasNoData = !chartData || chartData.length === 0;

  const stat = {
    label: title,
    value: hasNoData ? "No data" : computedHeadline,
  };

  // If no data, render just the ChartWidget without charts or breakdown
  if (hasNoData) {
    return <ChartWidget stats={stat}>{null}</ChartWidget>;
  }

  return (
    <div
      className={`flex w-full flex-row overflow-hidden rounded-xl bg-surface-base phone:flex-col phone:gap-6 tablet:flex-col tablet:gap-6 ${className || ""}`}
    >
      {/* Left Panel: ChartWidget with SegmentedBarChart(s) */}
      <ChartWidget stats={stat} className="w-1/2 rounded-none! phone:w-full tablet:w-full">
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
                  segmentConfig={segmentConfig}
                  units={{ singular: "miner", plural: "miners" }}
                  height={DEFAULT_CHART_HEIGHT}
                  percentageDisplay={true}
                  xAxisTickInterval={isMultiDay ? (hours <= 48 ? 3 : 2) : 1}
                  yAxisTickCount={4}
                  barWidth={barWidth}
                  barGap={isMultiDay ? MULTI_DAY_BAR_GAP : undefined}
                  showDateLabel={isMultiDay}
                  lastTickOverride={!isMultiDay && hours < 24 ? "live" : undefined}
                />
              </div>
            );
          })}
        </div>
      </ChartWidget>

      {/* Right Panel: Current Values Breakdown */}
      <div className="flex w-1/2 flex-col justify-between space-y-3 rounded-xl bg-surface-base p-10 phone:w-full phone:gap-4 phone:p-6 phone:pt-0 tablet:w-full tablet:gap-4 tablet:p-6 tablet:pt-0">
        {currentBreakdown.map((segment, idx) => (
          <div key={segment.key} className="relative flex grow flex-row items-center">
            {/* Icon or color indicator */}
            {segment.icon ? (
              <span className="mr-3 flex" style={{ color: `var(${segment.color})` }}>
                {segment.icon}
              </span>
            ) : (
              <div className="mr-3 h-3 w-3 rounded-full" style={{ backgroundColor: `var(${segment.color})` }} />
            )}

            {/* Label and percentage */}
            <div className="flex flex-1 flex-col">
              <span className="text-400 text-text-primary">{segment.label}</span>
              <span className="text-text-secondary text-300">{segment.percentageLabel}</span>
            </div>

            {/* Button with count - only show if showButton is true and count > 0 */}
            {segment.showButton && segment.count > 0 && (
              <Button
                variant={variants[segment.buttonVariant] || variants.secondary}
                size="compact"
                onClick={segment.onClick}
                className={clsx({ "pointer-events-none": !segment.onClick })}
              >
                {segment.count} {segment.count === 1 ? "miner" : "miners"}
              </Button>
            )}

            {/* Divider between segments (not on last item) */}
            {idx < currentBreakdown.length - 1 && <Divider className="absolute -bottom-4 left-0 w-full" />}
          </div>
        ))}
      </div>
    </div>
  );
};
