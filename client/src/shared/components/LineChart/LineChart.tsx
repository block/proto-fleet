import { ComponentType, type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart as RechartsLineChart,
  ReferenceLine,
  Tooltip,
  XAxis,
  YAxis,
  type YAxisTickContentProps,
} from "recharts";
import type { MouseHandlerDataParam } from "recharts/types/synchronisation/types";

import { lineProps } from "./constants";

import ChartTooltip from "./Tooltip";

import { type ChartData } from "./types";
import { ChartWrapper, LineCursor, TimeXAxisTick, xAxisProps } from "@/shared/components/Chart";
import useCssVariable from "@/shared/hooks/useCssVariable";
import useMeasure from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const TOOLTIP_WIDTH = 269;
const TOOLTIP_WIDTH_PHONE = 235;
const TOOLTIP_OFFSET = 24;
const Y_AXIS_TICK_WIDTH = 50;
const MIN_TIMESTAMP_X_POSITION = 70; // Padding to prevent timestamp label from being clipped on left edge
const MAX_TIMESTAMP_X_POSITION = 52; // Padding to prevent timestamp label from being clipped on right edge

// Static objects moved to module scope to avoid creating new references on every render
// This prevents Recharts from detecting "changes" and triggering infinite re-render loops
const X_AXIS_LEFT_PADDING = 40;
const X_AXIS_RIGHT_PADDING = 10;
const X_AXIS_RIGHT_PADDING_WITH_DATE = 15;
const X_AXIS_PADDING = { left: X_AXIS_LEFT_PADDING, right: X_AXIS_RIGHT_PADDING };
const X_AXIS_PADDING_WITH_DATE = { left: X_AXIS_LEFT_PADDING, right: X_AXIS_RIGHT_PADDING_WITH_DATE };

const TWENTY_FOUR_HOURS_MS = 24 * 60 * 60 * 1000;

// Responsive X-axis tick counts by screen size
const X_AXIS_TICK_COUNT_DESKTOP = 7;
const X_AXIS_TICK_COUNT_LAPTOP = 6;
const X_AXIS_TICK_COUNT_TABLET = 5;
const X_AXIS_TICK_COUNT_PHONE = 4;

// Max visible tick labels by screen size (used for data-index-based spacing)
const MAX_TICKS_DESKTOP = 13;
const MAX_TICKS_LAPTOP = 10;
const MAX_TICKS_TABLET = 8;
const MAX_TICKS_PHONE = 6;
const X_AXIS_LINE_STYLE = {
  stroke: "#000",
  strokeWidth: 1,
  strokeOpacity: 0, // hide the line because bottom tickline serves as axis line
};
const TOOLTIP_WRAPPER_STYLE: CSSProperties = { outline: "none", pointerEvents: "none" };
const LINE_CURSOR = <LineCursor />;

interface CustomYAxisTickProps extends YAxisTickContentProps {
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
  tooltipKeys?: string[];
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
  xAxisDomainOverride?: [number, number];
  connectNulls?: boolean;
  referenceLines?: { value: number; color: string; strokeDasharray?: string }[];
  hideAggregateContextWhenSingleSeries?: boolean;
}

const LineChart = ({
  chartData,
  aggregateKey,
  colorMap,
  activeKeys,
  tooltipKeys,
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
  xAxisDomainOverride,
  connectNulls = false,
  referenceLines,
  hideAggregateContextWhenSingleSeries = false,
}: LineChartProps) => {
  const [chartRef, _, chartBoundingRect] = useMeasure<HTMLDivElement>();
  const [tooltipDatetime, setTooltipDatetime] = useState<number | undefined>(undefined);
  const tooltipDatetimeRef = useRef<number | undefined>(undefined);
  const queuedTooltipDatetimeRef = useRef<number | undefined>(undefined);
  const tooltipAnimationFrameRef = useRef<number | null>(null);

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

    // Find the closest data point index for each target timestamp using binary search
    // (chartData is sorted chronologically)
    return targetTimestamps.map((targetTime) => {
      let left = 0;
      let right = chartData.length - 1;

      while (left < right) {
        const mid = (left + right) >>> 1;
        if (chartData[mid].datetime < targetTime) {
          left = mid + 1;
        } else {
          right = mid;
        }
      }

      // left is the first index >= targetTime; check if the previous index is closer
      const prev = left - 1;
      if (
        prev >= 0 &&
        Math.abs(chartData[prev].datetime - targetTime) <= Math.abs(chartData[left].datetime - targetTime)
      ) {
        return prev;
      }
      return left;
    });
  }, [chartData, xAxisLabelCount]);

  // Calculate explicit X-axis domain for consistent positioning
  // When xAxisDomainOverride is provided, use it to ensure the chart spans the
  // full requested time range even if the data points don't cover it all.
  const xAxisDomain = useMemo(() => {
    if (xAxisDomainOverride) return xAxisDomainOverride;
    if (!chartData?.length) return undefined;

    const firstTimestamp = chartData[0].datetime;
    const lastTimestamp = chartData[chartData.length - 1].datetime;

    return [firstTimestamp, lastTimestamp];
  }, [chartData, xAxisDomainOverride]);

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

  const tooltipKeysToShow = useMemo(() => {
    const preferredTooltipKeys = tooltipKeys ?? activeKeys ?? [];
    return preferredTooltipKeys.length > 0 ? preferredTooltipKeys : aggregateKey ? [aggregateKey] : [];
  }, [tooltipKeys, activeKeys, aggregateKey]);

  const scheduleTooltipDatetime = useCallback((nextTooltipDatetime: number | undefined) => {
    queuedTooltipDatetimeRef.current = nextTooltipDatetime;

    if (tooltipAnimationFrameRef.current !== null || tooltipDatetimeRef.current === nextTooltipDatetime) {
      return;
    }

    tooltipAnimationFrameRef.current = requestAnimationFrame(() => {
      tooltipAnimationFrameRef.current = null;
      const queuedTooltipDatetime = queuedTooltipDatetimeRef.current;
      tooltipDatetimeRef.current = queuedTooltipDatetime;
      setTooltipDatetime((currentTooltipDatetime) =>
        currentTooltipDatetime === queuedTooltipDatetime ? currentTooltipDatetime : queuedTooltipDatetime,
      );
    });
  }, []);

  useEffect(() => {
    tooltipDatetimeRef.current = tooltipDatetime;
  }, [tooltipDatetime]);

  useEffect(() => {
    return () => {
      if (tooltipAnimationFrameRef.current !== null) {
        cancelAnimationFrame(tooltipAnimationFrameRef.current);
      }
    };
  }, []);

  // Calculate max X position for timestamp labels dynamically based on chart width
  const maxXPosition = useMemo(() => {
    if (!chartBoundingRect.width) return undefined;
    return chartBoundingRect.width - MAX_TIMESTAMP_X_POSITION;
  }, [chartBoundingRect.width]);

  // Generate evenly-spaced tick timestamps across the full domain so labels
  // are distributed across the chart instead of clustering where data exists.
  // Uses actual container width (via useMeasure) so charts in multi-column
  // layouts get fewer ticks instead of overlapping labels.
  const xAxisTicks = useMemo(() => {
    if (!xAxisDomainOverride) return undefined;
    const [start, end] = xAxisDomainOverride;

    let count: number;
    const chartWidth = chartBoundingRect.width;
    if (chartWidth > 0) {
      const hasDateLabels = end - start >= TWENTY_FOUR_HOURS_MS;
      const minSlotWidth = hasDateLabels ? 100 : 60;
      count = Math.max(3, Math.min(X_AXIS_TICK_COUNT_DESKTOP, Math.floor(chartWidth / minSlotWidth)));
    } else {
      count = isDesktop
        ? X_AXIS_TICK_COUNT_DESKTOP
        : isLaptop
          ? X_AXIS_TICK_COUNT_LAPTOP
          : isTablet
            ? X_AXIS_TICK_COUNT_TABLET
            : X_AXIS_TICK_COUNT_PHONE;
    }

    return Array.from({ length: count }, (_, i) => Math.round(start + ((end - start) * i) / (count - 1)));
  }, [xAxisDomainOverride, chartBoundingRect.width, isDesktop, isLaptop, isTablet]);

  const tooltipTickValue = useMemo(() => {
    if (tooltipDatetime === undefined) return undefined;
    if (!xAxisTicks?.length) return tooltipDatetime;

    // Hover can land between synthetic ticks. Map it to the nearest tick so the
    // x-axis renders exactly one hover label at a deterministic tick position.
    return xAxisTicks.reduce((closestTick, tick) => {
      const tickDistance = Math.abs(tick - tooltipDatetime);
      const closestDistance = Math.abs(closestTick - tooltipDatetime);
      return tickDistance < closestDistance ? tick : closestTick;
    }, xAxisTicks[0]);
  }, [tooltipDatetime, xAxisTicks]);

  const showDateOnXAxis = useMemo(() => {
    if (!xAxisDomain) return false;
    return xAxisDomain[1] - xAxisDomain[0] >= TWENTY_FOUR_HOURS_MS;
  }, [xAxisDomain]);

  // Memoize tick components to prevent infinite re-render loops.
  // Depends on tooltipDatetime/tooltipTickValue (primitives extracted from tooltipData)
  // rather than the full tooltipData object, so the memo only invalidates when the
  // hovered data point actually changes — not on every mouse-move event.
  const maxTicksToShow = isDesktop
    ? MAX_TICKS_DESKTOP
    : isLaptop
      ? MAX_TICKS_LAPTOP
      : isTablet
        ? MAX_TICKS_TABLET
        : MAX_TICKS_PHONE;

  // When explicit ticks are generated from xAxisDomainOverride, use the tick count
  // as dataPointCount so all evenly-spaced ticks show labels. Also bypass
  // labelCount/timeBasedIndices which are data-index-based and conflict with
  // the synthetic tick indices.
  const explicitTickCount = xAxisTicks?.length;

  const xAxisTick = useMemo(
    () => (
      <TimeXAxisTick
        tooltipDatetime={tooltipDatetime}
        tooltipTickValue={tooltipTickValue}
        hideNonTooltipTicks={tooltipTickValue !== undefined}
        dataPointCount={explicitTickCount ?? (chartData?.length || 0)}
        maxTicksToShow={maxTicksToShow}
        showDate={showDateOnXAxis}
        minXPosition={MIN_TIMESTAMP_X_POSITION}
        maxXPosition={maxXPosition}
        labelCount={explicitTickCount ? undefined : xAxisLabelCount}
        timeBasedIndices={explicitTickCount ? undefined : timeBasedLabelIndices}
      />
    ),
    [
      tooltipDatetime,
      tooltipTickValue,
      explicitTickCount,
      chartData?.length,
      maxTicksToShow,
      showDateOnXAxis,
      maxXPosition,
      xAxisLabelCount,
      timeBasedLabelIndices,
    ],
  );

  const yAxisTick = useCallback(
    (props: YAxisTickContentProps) => (
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

  const displayableRange = useMemo(() => {
    if (!connectNulls || !chartData?.length) return undefined;
    let first: number | undefined;
    let last: number | undefined;
    for (const datum of chartData) {
      if (tooltipKeysToShow.some((key) => datum[key] !== null && datum[key] !== undefined)) {
        if (first === undefined) first = datum.datetime;
        last = datum.datetime;
      }
    }
    return first !== undefined && last !== undefined ? { first, last } : undefined;
  }, [chartData, connectNulls, tooltipKeysToShow]);

  const getTooltipDatetimeFromState = useCallback(
    (state: MouseHandlerDataParam) => {
      const rawTooltipIndex = state.activeTooltipIndex;
      const tooltipIndex =
        typeof rawTooltipIndex === "number"
          ? rawTooltipIndex
          : typeof rawTooltipIndex === "string" && rawTooltipIndex !== ""
            ? Number(rawTooltipIndex)
            : NaN;

      if (!state.isTooltipActive || !Number.isInteger(tooltipIndex) || tooltipIndex < 0) {
        return undefined;
      }

      const hoveredDatum = chartData?.[tooltipIndex];
      if (!hoveredDatum) {
        return undefined;
      }

      if (connectNulls) {
        if (
          !displayableRange ||
          hoveredDatum.datetime < displayableRange.first ||
          hoveredDatum.datetime > displayableRange.last
        ) {
          return undefined;
        }
        return hoveredDatum.datetime;
      }

      const hasDisplayableTooltipValue = tooltipKeysToShow.some((key) => {
        const value = hoveredDatum[key];
        return value !== null && value !== undefined;
      });
      return hasDisplayableTooltipValue ? hoveredDatum.datetime : undefined;
    },
    [chartData, connectNulls, displayableRange, tooltipKeysToShow],
  );

  const handleChartTooltipMove = useCallback(
    (state: MouseHandlerDataParam) => {
      scheduleTooltipDatetime(getTooltipDatetimeFromState(state));
    },
    [getTooltipDatetimeFromState, scheduleTooltipDatetime],
  );

  const clearTooltipDatetime = useCallback(() => {
    scheduleTooltipDatetime(undefined);
  }, [scheduleTooltipDatetime]);

  const tooltipChartData = connectNulls ? chartData : undefined;

  const tooltipContent = useMemo(
    () => (
      <ChartTooltip
        aggregateKey={aggregateKey}
        aggregateLabel="Summary"
        activeKeys={tooltipKeysToShow}
        chartData={tooltipChartData}
        chartWidth={chartBoundingRect.width}
        connectNulls={connectNulls}
        units={units}
        segmentsLabel={segmentsLabel}
        colorMap={colorMap}
        tooltipWidth={toolTipWidth}
        tooltipXOffset={tooltipXOffset}
        tooltipYOffset={TOOLTIP_OFFSET}
        toolTipItemIcon={toolTipItemIcon}
        hideAggregateContextWhenSingleSeries={hideAggregateContextWhenSingleSeries}
      />
    ),
    [
      aggregateKey,
      tooltipKeysToShow,
      tooltipChartData,
      chartBoundingRect.width,
      connectNulls,
      units,
      segmentsLabel,
      colorMap,
      toolTipWidth,
      tooltipXOffset,
      toolTipItemIcon,
      hideAggregateContextWhenSingleSeries,
    ],
  );

  return (
    <div ref={chartRef} className="min-h-100 flex-1 [&_*]:!outline-none" data-testid="line-chart">
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
            onMouseMove={handleChartTooltipMove}
            onMouseLeave={clearTooltipDatetime}
            onTouchMove={handleChartTooltipMove}
            onTouchEnd={clearTooltipDatetime}
          >
            <CartesianGrid vertical={false} stroke={corePrimary10} syncWithTicks={true} />

            <XAxis
              {...xAxisProps}
              tickMargin={28}
              padding={showDateOnXAxis ? X_AXIS_PADDING_WITH_DATE : X_AXIS_PADDING}
              domain={xAxisDomain}
              type="number"
              axisLine={X_AXIS_LINE_STYLE}
              dataKey="datetime"
              scale="time"
              tick={xAxisTick}
              ticks={xAxisTicks}
            />

            <Tooltip
              offset={0}
              allowEscapeViewBox={{ x: true, y: true }}
              wrapperStyle={TOOLTIP_WRAPPER_STYLE}
              content={tooltipContent}
              cursor={LINE_CURSOR}
              isAnimationActive={false}
              filterNull={connectNulls ? false : undefined}
            />

            {(activeKeys && activeKeys.length > 0 ? activeKeys : aggregateKey ? [aggregateKey] : []).map(
              (key, index) => {
                const strokeColor = colorMap?.[key] ? `var(${colorMap[key]})` : "var(--color-core-primary-fill)";

                return (
                  <Line {...lineProps} connectNulls={connectNulls} dataKey={key} key={index} stroke={strokeColor} />
                );
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

            {referenceLines?.map((line, i) => (
              <ReferenceLine
                key={i}
                y={line.value}
                stroke={`var(${line.color})`}
                strokeWidth={3}
                strokeDasharray={line.strokeDasharray ?? "1 6"}
                strokeOpacity={0.5}
                strokeLinecap="round"
                ifOverflow="extendDomain"
              />
            ))}
          </RechartsLineChart>
        ) : (
          <></>
        )}
      </ChartWrapper>
    </div>
  );
};

export default LineChart;
