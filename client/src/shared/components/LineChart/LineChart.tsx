import { ComponentType, useCallback, useMemo, useState } from "react";
import { CartesianGrid, Line, LineChart as RechartsLineChart, Tooltip, XAxis, YAxis } from "recharts";

import { lineProps } from "./constants";

import ChartTooltip, { type TooltipData } from "./Tooltip";

import { type ChartData } from "./types";
import { ChartWrapper, LineCursor, TimeXAxisTick, xAxisProps } from "@/shared/components/Chart";
import useCssVariable from "@/shared/hooks/useCssVariable";
import useMeasure from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const TOOLTIP_WIDTH = 269;
const TOOLTIP_WIDTH_PHONE = 150;
const TOOLTIP_OFFSET = 24;
const Y_AXIS_TICK_WIDTH = 50;
const MIN_TIMESTAMP_X_POSITION = 70; // Padding to prevent timestamp label from being clipped on left edge
const MAX_TIMESTAMP_X_POSITION = 52; // Padding to prevent timestamp label from being clipped on right edge

// Static objects moved to module scope to avoid creating new references on every render
// This prevents Recharts from detecting "changes" and triggering infinite re-render loops
const X_AXIS_PADDING = { left: 25, right: 10 };
const X_AXIS_LINE_STYLE = {
  stroke: "#000",
  strokeWidth: 1,
  strokeOpacity: 0, // hide the line because bottom tickline serves as axis line
};
const TOOLTIP_WRAPPER_STYLE = { outline: "none" };
const LINE_CURSOR = <LineCursor />;

interface CustomYAxisTickProps {
  payload: {
    value: number;
    coordinate: number;
    offset: number;
    index: number;
  };
  x: number;
  y: number;
  yAxisTicks: number[];
  yOffset?: number;
  visibleTickIndices?: number[];
}

// Custom Y-axis tick that hides the first (lowest) tick
const CustomYAxisTick = ({ payload, x, y, yAxisTicks, yOffset = 20, visibleTickIndices }: CustomYAxisTickProps) => {
  // Find the index of this tick value in the yAxisTicks array
  const tickIndex = yAxisTicks.findIndex((tick) => tick === payload.value);

  // If visibleTickIndices is provided, only show ticks at those indices
  if (visibleTickIndices) {
    if (!visibleTickIndices.includes(tickIndex)) {
      return null;
    }
  } else {
    // Default behavior: hide the first (lowest) tick
    const isFirstTick = yAxisTicks.length > 0 && payload.value === yAxisTicks[0];
    if (isFirstTick) {
      return null;
    }
  }

  // Calculate text dimensions for background rectangle
  const text = String(payload.value);
  const textWidth = text.length * 7; // Approximate width based on font size
  const textHeight = 16; // Based on fontSize + padding
  const textPadding = 4;

  return (
    <g transform={`translate(${x},${y})`}>
      {/* Background rectangle */}
      <rect
        x={8 - textPadding / 2}
        y={yOffset - textHeight + textPadding / 2}
        width={textWidth + textPadding}
        height={textHeight}
        fill="var(--color-surface-base)"
        fillOpacity={0.8}
        rx={2}
      />
      {/* Text */}
      <text x={8} y={yOffset} textAnchor="start" fontSize={12} fill="var(--color-text-primary)" fillOpacity={0.5}>
        {payload.value}
      </text>
    </g>
  );
};

export interface LineChartProps {
  chartData: ChartData[] | null;
  units?: string;
  segmentsLabel?: string;
  aggregateKey: string;
  activeKeys?: string[];
  highestValue?: string | number;
  tickCount?: number;
  minTickInterval?: number;
  colorMap?: { [key: string]: string };
  toolTipItemIcon?: ComponentType<{ itemKey: string }>;
  yAxisTickYOffset?: number;
  visibleTickIndices?: number[];
  chartMarginTop?: number;
  xAxisLabelCount?: number;
  tooltipXOffset?: number;
}

const LineChart = ({
  chartData,
  aggregateKey,
  colorMap,
  activeKeys,
  highestValue,
  units,
  segmentsLabel = "Hashboards",
  tickCount = 10,
  minTickInterval = 0.5,
  toolTipItemIcon,
  yAxisTickYOffset = 20,
  visibleTickIndices,
  chartMarginTop = 0,
  xAxisLabelCount,
  tooltipXOffset = TOOLTIP_OFFSET,
}: LineChartProps) => {
  const [chartRef, _, chartBoundingRect] = useMeasure<HTMLDivElement>();
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const corePrimary10 = useCssVariable("--color-core-primary-10");

  // Calculate time-based label indices when using xAxisLabelCount
  const timeBasedLabelIndices = useMemo(() => {
    if (!xAxisLabelCount || !chartData?.length) return undefined;

    // Handle edge case: if only 1 label requested, return first index
    if (xAxisLabelCount <= 1) return [0];

    const firstTimestamp = chartData[0].datetime;
    const lastTimestamp = chartData[chartData.length - 1].datetime;
    const timeRange = lastTimestamp - firstTimestamp;

    // Calculate target timestamps evenly spaced in time
    const targetTimestamps = Array.from({ length: xAxisLabelCount }, (_, i) => {
      return firstTimestamp + (timeRange * i) / (xAxisLabelCount - 1);
    });

    // Find the closest data point index for each target timestamp
    return targetTimestamps.map((targetTime) => {
      let closestIndex = 0;
      let minDiff = Math.abs(chartData[0].datetime - targetTime);

      for (let i = 1; i < chartData.length; i++) {
        const diff = Math.abs(chartData[i].datetime - targetTime);
        if (diff < minDiff) {
          minDiff = diff;
          closestIndex = i;
        }
      }

      return closestIndex;
    });
  }, [chartData, xAxisLabelCount]);

  // Calculate explicit X-axis domain for consistent positioning
  const xAxisDomain = useMemo(() => {
    if (!chartData?.length) return undefined;

    const firstTimestamp = chartData[0].datetime;
    const lastTimestamp = chartData[chartData.length - 1].datetime;

    return [firstTimestamp, lastTimestamp];
  }, [chartData]);

  const { isDesktop, isTablet, isLaptop, isPhone } = useWindowDimensions();

  // Calculate Y-axis domain and ticks from chart data
  const { minDomain, maxDomain, yAxisTicks } = useMemo(() => {
    if (!chartData?.length) {
      return { minDomain: 0, maxDomain: 0, yAxisTicks: [] };
    }

    // iterate over all data points and find the
    // highest and loweset values for all active keys
    const max =
      +(highestValue || 0) ||
      Math.max(
        ...chartData.map((data) => {
          return Math.max(
            ...Object.entries(data)
              .filter(
                ([key, _]) =>
                  activeKeys?.includes(key) ||
                  // if all series are inactive and show aggregate is false set
                  // max according to the aggregate
                  (!activeKeys?.length && key === aggregateKey),
              )
              .map(([_, value]) => value)
              .filter((v) => typeof v === "number"),
          );
        }),
      );

    const min =
      +(highestValue || 0) ||
      Math.min(
        ...chartData.map((data) => {
          return Math.min(
            ...Object.entries(data)
              .filter(
                ([key, _]) =>
                  activeKeys?.includes(key) ||
                  // if all series are inactive and show aggregate is false set
                  // max according to the aggregate
                  (!activeKeys?.length && key === aggregateKey),
              )
              .map(([_, value]) => value)
              .filter((v) => typeof v === "number"),
          );
        }),
      );

    // Guard against empty data: if filtering returned no values,
    // Math.max() returns -Infinity and Math.min() returns Infinity,
    // which would cause NaN ticks downstream
    if (!isFinite(max) || !isFinite(min)) {
      return { minDomain: 0, maxDomain: 0, yAxisTicks: [] };
    }

    const range = max - min;
    const paddedMin = min - range * 0.2;
    const paddedMax = max + range * 0.2;

    const tickInterval = Math.max(
      Math.round(((paddedMax - paddedMin) / (tickCount - 1)) * (1 / minTickInterval)) / (1 / minTickInterval),
      minTickInterval,
    );
    const middleTick = Math.round(((paddedMin + paddedMax) / 2) * (1 / minTickInterval)) / (1 / minTickInterval);

    let ticks = Array.from({ length: tickCount }, (_, i) => middleTick - (tickCount / 2 - 1 - i) * tickInterval).sort(
      (a, b) => a - b,
    );

    // If any ticks are negative, shift the whole array so first tick is 0
    if (ticks[0] < 0) {
      const shift = -ticks[0];
      ticks = ticks.map((tick) => tick + shift);
    }

    // Extend domain below first tick and above last tick to prevent line clipping
    const domainPadding = tickInterval * 0.5;

    return {
      minDomain: Math.max(0, ticks[0] - domainPadding),
      maxDomain: ticks[ticks.length - 1] + domainPadding,
      yAxisTicks: ticks,
    };
  }, [chartData, tickCount, minTickInterval, highestValue, aggregateKey, activeKeys]);

  const toolTipWidth = useMemo(() => {
    return isPhone ? TOOLTIP_WIDTH_PHONE : TOOLTIP_WIDTH;
  }, [isPhone]);

  const toolTipPositionX = useMemo(() => {
    // Position tooltip to the right of cursor by default
    const tooltipRightPosition = tooltipData.x + tooltipXOffset;

    // Check if tooltip would overflow the right edge
    const wouldOverflowRight = tooltipRightPosition + toolTipWidth > chartBoundingRect.width;

    if (wouldOverflowRight) {
      // Position tooltip to the left of cursor
      return Math.max(tooltipXOffset, tooltipData.x - toolTipWidth - tooltipXOffset);
    } else {
      // Position tooltip to the right of cursor
      return tooltipRightPosition;
    }
  }, [tooltipData.x, chartBoundingRect.width, toolTipWidth, tooltipXOffset]);

  // Calculate max X position for timestamp labels dynamically based on chart width
  const maxXPosition = useMemo(() => {
    if (!chartBoundingRect.width) return undefined;
    return chartBoundingRect.width - MAX_TIMESTAMP_X_POSITION;
  }, [chartBoundingRect.width]);

  // Memoize tick components to prevent infinite re-render loops
  // These must NOT depend on tooltipData to avoid the cycle:
  // hover → tooltipData updates → tick re-renders → triggers more updates
  const maxTicksToShow = isDesktop ? 13 : isLaptop ? 10 : isTablet ? 8 : 6;

  const xAxisTick = useMemo(
    () => (
      <TimeXAxisTick
        tooltipDatetime={undefined} // Don't pass tooltipData to break the render cycle
        dataPointCount={chartData?.length || 0}
        maxTicksToShow={maxTicksToShow}
        minXPosition={MIN_TIMESTAMP_X_POSITION}
        maxXPosition={maxXPosition}
        labelCount={xAxisLabelCount}
        timeBasedIndices={timeBasedLabelIndices}
      />
    ),
    [chartData?.length, maxTicksToShow, maxXPosition, xAxisLabelCount, timeBasedLabelIndices],
  );

  const yAxisTick = useCallback(
    (props: CustomYAxisTickProps) => (
      <CustomYAxisTick
        {...props}
        yAxisTicks={yAxisTicks}
        yOffset={yAxisTickYOffset}
        visibleTickIndices={visibleTickIndices}
      />
    ),
    [yAxisTicks, yAxisTickYOffset, visibleTickIndices],
  );

  const yAxisLineStyle = useMemo(() => ({ stroke: corePrimary10 }), [corePrimary10]);

  const tooltipPosition = useMemo(() => ({ y: TOOLTIP_OFFSET, x: toolTipPositionX }), [toolTipPositionX]);

  const tooltipContent = useMemo(
    () => (
      <ChartTooltip
        aggregateKey={aggregateKey}
        aggregateLabel="Summary"
        onHover={setTooltipData}
        tooltipData={tooltipData}
        activeKeys={activeKeys}
        units={units}
        segmentsLabel={segmentsLabel}
        colorMap={colorMap}
        tooltipWidth={isPhone ? TOOLTIP_WIDTH_PHONE : TOOLTIP_WIDTH}
        toolTipItemIcon={toolTipItemIcon}
      />
    ),
    [aggregateKey, tooltipData, activeKeys, units, segmentsLabel, colorMap, isPhone, toolTipItemIcon],
  );

  return (
    <div ref={chartRef} className="min-h-100 flex-1 [&_*]:!outline-none">
      <ChartWrapper className="mb-10 h-full w-full">
        {chartData?.length ? (
          <RechartsLineChart
            data={chartData || []}
            margin={{
              top: chartMarginTop,
              right: 0,
              left: -1 * Y_AXIS_TICK_WIDTH,
              bottom: 5,
            }}
          >
            <CartesianGrid vertical={false} stroke={corePrimary10} syncWithTicks={true} />

            <XAxis
              {...xAxisProps}
              tickMargin={28}
              padding={X_AXIS_PADDING}
              domain={xAxisDomain}
              type="number"
              axisLine={X_AXIS_LINE_STYLE}
              dataKey="datetime"
              scale="time"
              tick={xAxisTick}
            />

            <Tooltip
              position={tooltipPosition}
              wrapperStyle={TOOLTIP_WRAPPER_STYLE}
              content={tooltipContent}
              cursor={LINE_CURSOR}
              isAnimationActive={false}
            />

            {(activeKeys && activeKeys.length > 0 ? activeKeys : aggregateKey ? [aggregateKey] : []).map(
              (key, index) => {
                const strokeColor = colorMap?.[key] ? `var(${colorMap[key]})` : "var(--color-core-primary-fill)";

                return <Line {...lineProps} dataKey={key} key={index} stroke={strokeColor} />;
              },
            )}

            <YAxis
              axisLine={yAxisLineStyle}
              tickLine={false}
              tick={yAxisTick}
              tickSize={0}
              width={Y_AXIS_TICK_WIDTH}
              interval={0}
              tickMargin={0}
              domain={[minDomain, maxDomain]}
              ticks={yAxisTicks}
              allowDecimals={false}
              allowDataOverflow
            />
          </RechartsLineChart>
        ) : (
          <></>
        )}
      </ChartWrapper>
    </div>
  );
};

export default LineChart;
