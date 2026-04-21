import { useMemo } from "react";

import { DEFAULT_CHART_HEIGHT } from "./constants";
import type { SegmentedMetricPanelProps } from "./types";
import { durationToHours, getCurrentBreakdown, processMultiDayChartData } from "./utils";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { StatusBreakdownPanel } from "@/protoFleet/features/dashboard/components/StatusBreakdownPanel";
import SegmentedBarChart from "@/shared/components/SegmentedBarChart";

// Constants for bar chart display
const MULTI_DAY_BAR_WIDTH = {
  desktop: 8,
  laptop: 6,
  tablet: 8,
  phone: 6,
};

// Duration thresholds (in hours)
const ONE_DAY_IN_HOURS = 24;
const TWO_DAYS_IN_HOURS = 48;
const TEN_DAYS_IN_HOURS = 240;

// X-axis tick intervals based on duration
const TICK_INTERVAL_SINGLE_DAY = 1;
const TICK_INTERVAL_SHORT_MULTI_DAY = 3;
const TICK_INTERVAL_MEDIUM_MULTI_DAY = 2;
const TICK_INTERVAL_LONG_MULTI_DAY = 4;

/**
 * Determines the x-axis tick interval based on duration.
 * Shorter durations show more ticks, longer durations show fewer to prevent overlap.
 */
const getXAxisTickInterval = (hours: number, isMultiDay: boolean): number => {
  if (!isMultiDay) {
    return TICK_INTERVAL_SINGLE_DAY;
  }
  if (hours <= TWO_DAYS_IN_HOURS) {
    return TICK_INTERVAL_SHORT_MULTI_DAY;
  }
  if (hours <= TEN_DAYS_IN_HOURS) {
    return TICK_INTERVAL_MEDIUM_MULTI_DAY;
  }
  return TICK_INTERVAL_LONG_MULTI_DAY;
};

/**
 * Determines whether to use date format (e.g., "1/15") for x-axis ticks.
 * Use date format for multi-day durations where bars represent multiple hours.
 */
const shouldUseDateFormat = (hours: number): boolean => {
  return hours > ONE_DAY_IN_HOURS;
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

  // Calculate current breakdown from processed chart data (shares logic with chart)
  const currentBreakdown = useMemo(
    () => getCurrentBreakdown(processedChartData, segmentConfig),
    [processedChartData, segmentConfig],
  );

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
      className={`flex w-full flex-row overflow-hidden rounded-xl bg-surface-base dark:bg-core-primary-5 phone:flex-col phone:gap-6 tablet:flex-col tablet:gap-6 ${className || ""}`}
    >
      {/* Left Panel: ChartWidget with SegmentedBarChart(s) */}
      <ChartWidget
        stats={stat}
        className="w-1/2 rounded-none! bg-transparent dark:bg-transparent phone:w-full tablet:w-full"
      >
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
                  xAxisTickInterval={getXAxisTickInterval(hours, isMultiDay)}
                  yAxisTickCount={4}
                  barWidth={barWidth}
                  showDateLabel={isMultiDay && processedChartData.length > 1}
                  useDateFormat={shouldUseDateFormat(hours)}
                  lastTickOverride={!isMultiDay && hours < 24 ? "live" : undefined}
                />
              </div>
            );
          })}
        </div>
      </ChartWidget>

      {/* Right Panel: Current Values Breakdown */}
      <StatusBreakdownPanel items={currentBreakdown} className="w-1/2 phone:w-full tablet:w-full" />
    </div>
  );
};
