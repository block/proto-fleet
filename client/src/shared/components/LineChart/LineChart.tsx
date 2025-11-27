import { ComponentType, useEffect, useMemo, useState } from "react";
import { CartesianGrid, Line, LineChart as RechartsLineChart, Tooltip, XAxis, YAxis } from "recharts";

import { lineProps } from "./constants";

import ChartTooltip, { type TooltipData } from "./Tooltip";

import { type ChartData } from "./types";
import { ChartWrapper, LineCursor, TimeXAxisTick, xAxisProps } from "@/shared/components/Chart";
import useCssVariable from "@/shared/hooks/useCssVariable";
import useMeasure from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const ANIMATION_DURATION = 1500;
const TOOLTIP_WIDTH = 269;
const TOOLTIP_WIDTH_PHONE = 150;
const TOOLTIP_OFFSET = 24;
const Y_AXIS_TICK_WIDTH = 50;

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
}

// Custom Y-axis tick that hides the first (lowest) tick
const CustomYAxisTick = ({ payload, x, y, yAxisTicks }: CustomYAxisTickProps) => {
  // Don't render if this is the first (lowest) tick value
  const isFirstTick = yAxisTicks.length > 0 && payload.value === yAxisTicks[0];

  if (isFirstTick) {
    return null;
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
        y={20 - textHeight + textPadding / 2}
        width={textWidth + textPadding}
        height={textHeight}
        fill="var(--color-surface-base)"
        fillOpacity={0.8}
        rx={2}
      />
      {/* Text */}
      <text x={8} y={20} textAnchor="start" fontSize={12} fill="var(--color-text-primary)" fillOpacity={0.5}>
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
}: LineChartProps) => {
  const [chartRef, _, chartBoundingRect] = useMeasure<HTMLDivElement>();
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const corePrimary10 = useCssVariable("--color-core-primary-10");

  const [shouldAnimate, setShouldAnimate] = useState(true);
  const { isDesktop, isTablet, isLaptop, isPhone } = useWindowDimensions();

  // initialize animation flags and chart data
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setShouldAnimate(true);

    const timeoutId = setTimeout(() => {
      setShouldAnimate(false);
    }, ANIMATION_DURATION);
    return () => clearTimeout(timeoutId);
  }, []);

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

    return {
      minDomain: ticks[0],
      maxDomain: ticks[ticks.length - 1],
      yAxisTicks: ticks,
    };
  }, [chartData, tickCount, minTickInterval, highestValue, aggregateKey, activeKeys]);

  const toolTipWidth = useMemo(() => {
    return isPhone ? TOOLTIP_WIDTH_PHONE : TOOLTIP_WIDTH;
  }, [isPhone]);

  const toolTipPositionX = useMemo(() => {
    const cursorIsLeftSide = tooltipData.x < Y_AXIS_TICK_WIDTH + chartBoundingRect.width / 2;

    if (cursorIsLeftSide) {
      // position tooltip on the right side
      return chartBoundingRect.width - TOOLTIP_OFFSET - toolTipWidth;
    } else {
      // position tooltip on the left side
      return TOOLTIP_OFFSET;
    }
  }, [tooltipData.x, chartBoundingRect.width, toolTipWidth]);

  return (
    <div ref={chartRef} className="min-h-100 flex-1">
      <ChartWrapper className="mb-10 h-full w-full">
        {chartData?.length ? (
          <RechartsLineChart
            data={chartData || []}
            margin={{
              top: 0,
              right: 0,
              left: -1 * Y_AXIS_TICK_WIDTH,
              bottom: 5,
            }}
          >
            <CartesianGrid vertical={false} stroke={corePrimary10} />

            <XAxis
              {...xAxisProps}
              tickMargin={28}
              axisLine={{
                stroke: "#000",
                strokeWidth: 1,
                strokeOpacity: 0, // hide the line because bottom tickline serves as axis line
              }}
              dataKey="datetime"
              scale="time"
              tick={
                <TimeXAxisTick
                  tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
                  dataPointCount={chartData?.length || 0}
                  maxTicksToShow={isDesktop ? 13 : isLaptop ? 10 : isTablet ? 8 : 6}
                  minXPosition={85}
                  maxXPosition={isPhone ? 303 : 871}
                />
              }
            />

            <Tooltip
              position={{
                y: TOOLTIP_OFFSET,
                x: toolTipPositionX,
              }}
              wrapperStyle={{ outline: "none" }}
              content={
                <ChartTooltip
                  aggregateKey={aggregateKey} // key of the aggregate value in the payload
                  aggregateLabel="Summary" // displayed name of the aggregate in the tooltip
                  onHover={setTooltipData}
                  tooltipData={tooltipData}
                  activeKeys={activeKeys}
                  units={units}
                  segmentsLabel={segmentsLabel}
                  colorMap={colorMap}
                  tooltipWidth={isPhone ? TOOLTIP_WIDTH_PHONE : TOOLTIP_WIDTH}
                  toolTipItemIcon={toolTipItemIcon}
                />
              }
              cursor={<LineCursor />}
              isAnimationActive={false}
            />

            {(activeKeys && activeKeys.length > 0 ? activeKeys : aggregateKey ? [aggregateKey] : []).map(
              (key, index) => {
                const strokeColor = colorMap?.[key] ? `var(${colorMap[key]})` : "var(--color-core-primary-fill)";

                return (
                  <Line
                    {...lineProps}
                    dataKey={key}
                    key={index}
                    isAnimationActive={key === aggregateKey && shouldAnimate}
                    stroke={strokeColor}
                  />
                );
              },
            )}

            <YAxis
              axisLine={{
                stroke: corePrimary10,
              }}
              tickLine={false}
              tick={(props) => <CustomYAxisTick {...props} yAxisTicks={yAxisTicks} />}
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
