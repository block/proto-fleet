import { useEffect, useMemo, useRef, useState } from "react";
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";

import {
  BAR_ANIMATION_DURATION,
  barProps,
  cartesianGridProps,
  DEFAULT_BAR_WIDTH,
  DEFAULT_CHART_HEIGHT,
  DEFAULT_Y_AXIS_TICK_COUNT,
  defaultColors,
  xAxisProps,
  yAxisProps,
} from "./constants";
import CustomSegmentedBar from "./CustomSegmentedBar";
import SegmentedXAxisTick from "./SegmentedXAxisTick";
import SegmentedBarTooltip from "./Tooltip/SegmentedBarTooltip";
import type { SegmentedBarChartProps } from "./types";
import ChartWrapper from "@/shared/components/Chart/ChartWrapper";
import useMeasure from "@/shared/hooks/useMeasure";

const SegmentedBarChart = ({
  chartData,
  segmentKeys,
  colorMap,
  units = "",
  percentageDisplay = false,
  showTooltip = true,
  animate = false,
  className,
  height = DEFAULT_CHART_HEIGHT,
  barWidth = DEFAULT_BAR_WIDTH,
  xAxisPadding: customXAxisPadding,
  yAxisPadding = 0,
  yAxisTickCount = DEFAULT_Y_AXIS_TICK_COUNT,
  xAxisTickInterval = 1,
  showDateLabel = false,
  lastTickOverride,
  toolTipKey,
}: SegmentedBarChartProps) => {
  const [shouldAnimate, setShouldAnimate] = useState(animate);
  const [hoveredBar, setHoveredBar] = useState<{
    x: number;
    y: number;
    index: number;
  } | null>(null);
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  const chartRef = useRef<HTMLDivElement>(null);

  // Measure the chart container to get actual width
  const [measureRef, contentRect] = useMeasure<HTMLDivElement>();

  // Transform data for custom bar rendering
  const transformedData = useMemo(() => {
    if (!chartData) return null;

    // Use default colors if colorMap not provided
    const getColorForSegment = (key: string, index: number) => {
      if (colorMap && colorMap[key]) {
        return colorMap[key];
      }
      // Use color from array by index, fallback to first color if index is out of bounds
      return defaultColors[index] || defaultColors[0];
    };

    return chartData.map((item) => {
      // Calculate actual total for this data point
      const actualTotal = segmentKeys.reduce((sum, key) => {
        return sum + item[key];
      }, 0);

      if (percentageDisplay) {
        // For percentage display, convert values to percentages
        return {
          datetime: item.datetime,
          total: 100, // Always 100 for percentage display
          segments: segmentKeys.map((key, index) => {
            const val = item[key];
            // Convert to percentage of total
            const percentageValue =
              actualTotal > 0 ? (val / actualTotal) * 100 : 0;
            return {
              key,
              value: percentageValue,
              color: getColorForSegment(key, index),
            };
          }),
        };
      } else {
        // For normal display, use actual values
        return {
          datetime: item.datetime,
          total: actualTotal,
          segments: segmentKeys.map((key, index) => {
            const val = item[key];
            return {
              key,
              value: val,
              color: getColorForSegment(key, index),
            };
          }),
        };
      }
    });
  }, [chartData, segmentKeys, colorMap, percentageDisplay]);

  // Calculate x-axis padding based on chart width and bar dimensions
  // Use custom padding if provided, otherwise calculate automatically
  const xAxisPadding = useMemo(() => {
    // Use custom padding if provided
    if (customXAxisPadding !== undefined) {
      return customXAxisPadding;
    }

    // Otherwise calculate automatically
    if (!transformedData || transformedData.length === 0) return 0;

    const chartWidth = contentRect.width;
    if (chartWidth === 0) return 0; // No width measured yet

    const numBars = transformedData.length;
    const totalBarWidth = barWidth * numBars;

    // Calculate the padding needed on each side
    // This centers the bars with equal spacing on left and right
    const padding = Math.max(0, (chartWidth - totalBarWidth) / numBars) / 2;

    return padding;
  }, [customXAxisPadding, contentRect.width, transformedData, barWidth]);

  // Calculate Y-axis domain with optional padding
  const yAxisDomain = useMemo(() => {
    if (percentageDisplay) {
      return [0, 100]; // Use 100 for percentage scale
    }

    // Calculate max value from data
    const maxValue =
      transformedData?.reduce((max, item) => Math.max(max, item.total), 0) || 0;

    // If no data or all zeros, use a default scale to prevent tick overlap
    if (maxValue === 0) {
      return [0, 100]; // Default scale when no data
    }

    if (yAxisPadding > 0) {
      return [0, maxValue * (1 + yAxisPadding)];
    }

    // Default: scale to data max
    return [0, maxValue];
  }, [percentageDisplay, yAxisPadding, transformedData]);

  // Calculate tick values for evenly spaced grid lines
  const yAxisTicks = useMemo(() => {
    if (percentageDisplay) {
      // For percentage display, create evenly spaced ticks from 0 to 100
      const ticks = [];
      for (let i = 0; i <= yAxisTickCount; i++) {
        ticks.push((i * 100) / yAxisTickCount);
      }
      return ticks;
    }

    // For normal display, calculate based on domain
    const maxValue =
      typeof yAxisDomain[1] === "number"
        ? yAxisDomain[1]
        : transformedData?.reduce(
            (max, item) => Math.max(max, item.total),
            0,
          ) || 0;

    const ticks = [];
    for (let i = 0; i <= yAxisTickCount; i++) {
      ticks.push((maxValue * i) / yAxisTickCount);
    }
    return ticks;
  }, [percentageDisplay, yAxisTickCount, yAxisDomain, transformedData]);

  // Handle animation lifecycle
  useEffect(() => {
    if (animate) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setShouldAnimate(true);
      const timeoutId = setTimeout(() => {
        setShouldAnimate(false);
      }, BAR_ANIMATION_DURATION);

      return () => clearTimeout(timeoutId);
    }
  }, [animate]);

  if (!transformedData || transformedData.length === 0) {
    return (
      <div
        className={clsx(
          "flex h-full w-full items-center justify-center text-text-primary-50",
          className,
        )}
      >
        <span>No data available</span>
      </div>
    );
  }

  return (
    <div
      ref={(el) => {
        chartRef.current = el;
        measureRef(el);
      }}
      className={clsx("outline-none", className)}
      style={{ height }}
      onMouseMove={(e) => {
        // Get the element under the mouse
        const element = e.target as Element;

        // Check if we're hovering over any element that's part of a bar group
        // This includes the <g> element or any of its children
        let currentElement: Element | null = element;
        let isOverBar = false;

        // Walk up the DOM to check if we're in a bar group
        while (currentElement && currentElement !== e.currentTarget) {
          if (
            currentElement.tagName === "g" &&
            (currentElement as HTMLElement).style.cursor === "default"
          ) {
            isOverBar = true;
            break;
          }
          currentElement = currentElement.parentElement;
        }

        // If not over a bar clear the state
        if (!isOverBar && hoveredIndex !== null) {
          setHoveredIndex(null);
          setHoveredBar(null);
        }
      }}
      onMouseLeave={() => {
        setHoveredIndex(null);
        setHoveredBar(null);
      }}
    >
      <ChartWrapper className="h-full w-full [&_*:focus]:outline-none [&_svg]:outline-none">
        <BarChart
          data={transformedData}
          margin={{ top: 5, right: 0, bottom: 5, left: 0 }}
        >
          <CartesianGrid {...cartesianGridProps} />

          <XAxis
            {...xAxisProps}
            dataKey="datetime"
            scale="linear"
            type="number"
            domain={["dataMin", "dataMax"]}
            padding={{ left: xAxisPadding, right: xAxisPadding }}
            tickCount={showDateLabel ? 1 : transformedData.length}
            ticks={
              showDateLabel && transformedData && transformedData.length > 0
                ? [
                    // Calculate middle timestamp
                    transformedData[Math.floor(transformedData.length / 2)]
                      .datetime,
                  ]
                : undefined
            }
            interval={showDateLabel ? 0 : xAxisTickInterval - 1}
            tick={(props: any) => {
              const lastTickValue =
                transformedData && transformedData.length > 0
                  ? transformedData[transformedData.length - 1].datetime
                  : null;
              return (
                <SegmentedXAxisTick
                  {...props}
                  showDateLabel={showDateLabel}
                  lastTickOverride={lastTickOverride}
                  isLastTick={props.payload?.value === lastTickValue}
                />
              );
            }}
          />

          <YAxis {...yAxisProps} domain={yAxisDomain} ticks={yAxisTicks} />

          {showTooltip &&
            toolTipKey !== null &&
            hoveredIndex !== null &&
            hoveredBar &&
            transformedData && (
              <Tooltip
                cursor={false}
                position={{ x: hoveredBar.x, y: hoveredBar.y - 8 }}
                isAnimationActive={false}
                content={
                  <SegmentedBarTooltip
                    active={true}
                    units={units}
                    percentageDisplay={percentageDisplay}
                    barPosition={hoveredBar}
                    toolTipKey={toolTipKey}
                    customPayload={transformedData[hoveredBar.index]}
                  />
                }
              />
            )}

          <Bar
            dataKey="total"
            fill="transparent"
            barSize={barWidth}
            {...barProps}
            isAnimationActive={shouldAnimate}
            shape={(props: any) => (
              <CustomSegmentedBar
                {...props}
                percentageDisplay={percentageDisplay}
                isHovered={hoveredIndex === props.index}
                onMouseEnter={(x: number, y: number) => {
                  setHoveredIndex(props.index);
                  setHoveredBar({ x, y, index: props.index });
                }}
                onMouseLeave={() => {
                  setHoveredIndex(null);
                  setHoveredBar(null);
                }}
              />
            )}
          />
        </BarChart>
      </ChartWrapper>
    </div>
  );
};

export default SegmentedBarChart;
