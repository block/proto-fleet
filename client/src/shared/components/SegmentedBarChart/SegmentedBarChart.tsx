import { MouseEvent, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import type { SegmentedBarChartProps } from "./types";
import { formatDate, formatTime, getResponsiveValue } from "./utils";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const SegmentedBarChart = ({
  chartData,
  segmentKeys,
  colorMap,
  units = "",
  percentageDisplay = false,
  yAxisTickCount = 3,
  xAxisTickInterval = 1,
  className,
  height = 200,
  barWidth = 12,
  barGap,
  showDateLabel = false,
  toolTipKey = "total",
  lastTickOverride,
}: SegmentedBarChartProps) => {
  const [hoveredBar, setHoveredBar] = useState<number | null>(null);
  const [tooltipPosition, setTooltipPosition] = useState<{ x: number; y: number } | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const viewport = useWindowDimensions();

  // Get responsive values and ensure they're integers to avoid subpixel rendering
  const actualBarWidth = Math.round(getResponsiveValue(barWidth, 12, viewport));
  const actualBarGap = getResponsiveValue(barGap, undefined as number | undefined, viewport);
  const actualBarGapRounded = actualBarGap !== undefined ? Math.round(actualBarGap) : undefined;

  // Transform data for rendering
  const transformedData = useMemo(() => {
    if (!chartData) return null;

    // Default colors if not provided
    const defaultColors = [
      "var(--color-extended-navy-fill)",
      "var(--color-surface-5)",
      "var(--color-intent-warning-fill)",
      "var(--color-intent-critical-fill)",
      "var(--color-extended-pink-fill)",
      "var(--color-extended-purple-fill)",
      "var(--color-extended-forest-fill)",
      "var(--color-extended-teal-fill)",
    ];

    const getColorForSegment = (key: string, index: number) => {
      if (colorMap && colorMap[key]) {
        return colorMap[key];
      }
      return defaultColors[index] || defaultColors[0];
    };

    return chartData.map((item) => {
      const actualTotal = segmentKeys.reduce((sum, key) => sum + item[key], 0);

      if (percentageDisplay) {
        return {
          datetime: item.datetime,
          total: 100,
          segments: segmentKeys.map((key, index) => {
            const val = item[key];
            const percentageValue = actualTotal > 0 ? (val / actualTotal) * 100 : 0;
            return {
              key,
              value: percentageValue,
              color: getColorForSegment(key, index),
            };
          }),
        };
      } else {
        return {
          datetime: item.datetime,
          total: actualTotal,
          segments: segmentKeys.map((key, index) => ({
            key,
            value: item[key],
            color: getColorForSegment(key, index),
          })),
        };
      }
    });
  }, [chartData, segmentKeys, colorMap, percentageDisplay]);

  // Calculate max value for Y-axis scaling
  const maxValue = useMemo(() => {
    if (percentageDisplay) return 100;
    if (!transformedData) return 0;
    return Math.max(...transformedData.map((d) => d.total));
  }, [transformedData, percentageDisplay]);

  // Y-axis tick values
  const yAxisTicks = useMemo(() => {
    const ticks = [];
    const step = maxValue / yAxisTickCount;
    for (let i = 0; i <= yAxisTickCount; i++) {
      ticks.push(Math.round(step * i));
    }
    return ticks.reverse();
  }, [maxValue, yAxisTickCount]);

  // Calculate exact container width when using fixed barGap to avoid subpixel rounding
  const containerWidth = useMemo(() => {
    if (actualBarGapRounded === undefined || !transformedData) return undefined;

    const barCount = transformedData.length;
    if (barCount === 0) return undefined;

    // Total width = (barWidth * barCount) + (gap * (barCount - 1))
    // Use rounded values to ensure integer pixel widths
    const totalBarsWidth = actualBarWidth * barCount;
    const totalGapsWidth = actualBarGapRounded * (barCount - 1);
    return totalBarsWidth + totalGapsWidth;
  }, [actualBarGapRounded, actualBarWidth, transformedData]);

  const handleBarHover = (index: number, event: MouseEvent<HTMLDivElement>) => {
    if (toolTipKey === null) return;

    const rect = event.currentTarget.firstElementChild?.getBoundingClientRect();
    const containerRect = containerRef.current?.getBoundingClientRect();

    if (containerRect) {
      setHoveredBar(index);
      setTooltipPosition({
        x: rect ? rect.left - containerRect.left + rect.width / 2 : 0,
        y: rect ? rect.top - containerRect.top - 8 : 0,
      });
    }
  };

  const tooltipText = useMemo(() => {
    if (hoveredBar === null || !transformedData) return null;
    const dataPoint = transformedData[hoveredBar];
    const key = toolTipKey;

    const value =
      key === "total" ? dataPoint.total : dataPoint.segments.find((segment) => segment.key === key)?.value || null;

    if (!value) return null;

    const formattedValue = getDisplayValue(value);
    return formattedValue ? `${formattedValue}${percentageDisplay ? "%" : units}` : null;
  }, [hoveredBar, transformedData, toolTipKey, percentageDisplay, units]);

  const handleBarLeave = () => {
    setHoveredBar(null);
    setTooltipPosition(null);
  };

  if (!transformedData || transformedData.length === 0) {
    return (
      <div className={clsx("flex items-center justify-center", className)} style={{ height }}>
        <span className="text-text-tertiary">No data available</span>
      </div>
    );
  }

  return (
    <div ref={containerRef} className={clsx("relative flex flex-col pb-8", className)} style={{ height }}>
      {/* Chart area with Y-axis grid lines */}
      <div className="relative flex flex-1 items-end">
        {/* Y-axis grid lines */}
        <div className="pointer-events-none absolute inset-0 flex flex-col justify-between border-b border-core-primary-5">
          {yAxisTicks.map((tick) => (
            <div
              key={tick}
              className="flex items-center border-t border-core-primary-5"
              style={{ height: `${100 / yAxisTicks.length}%` }}
            ></div>
          ))}
        </div>

        {/* Bars container */}
        <div
          className={clsx("h-full", {
            "flex w-full items-end justify-between": actualBarGapRounded === undefined,
            "relative mx-auto justify-center": actualBarGapRounded !== undefined,
          })}
          style={{
            width: containerWidth !== undefined ? `${containerWidth}px` : undefined,
          }}
        >
          {transformedData.map((data, index) => {
            const barHeight = maxValue > 0 ? (data.total / maxValue) * 100 : 0;
            const isLast = index === transformedData.length - 1;
            const showTick = !showDateLabel && index % xAxisTickInterval === 0;

            // Calculate exact position when using fixed gap
            const barPosition =
              actualBarGapRounded !== undefined ? index * (actualBarWidth + actualBarGapRounded) : undefined;

            return (
              <div
                key={index}
                className={clsx("group cursor-pointer", {
                  "relative flex grow flex-col items-center justify-end": actualBarGapRounded === undefined,
                  "absolute bottom-0 flex flex-col items-center justify-end": actualBarGapRounded !== undefined,
                })}
                style={{
                  height: "100%",
                  width: actualBarGapRounded !== undefined ? `${actualBarWidth}px` : "auto",
                  left: barPosition !== undefined ? `${barPosition}px` : undefined,
                }}
                onMouseEnter={(e) => handleBarHover(index, e)}
                onMouseLeave={handleBarLeave}
              >
                {/* Segmented bar */}
                <div
                  className="box-border flex flex-col-reverse overflow-hidden rounded-sm transition-shadow group-hover:shadow-[0_0_0_4px_theme(--color-core-primary-20)]"
                  style={{
                    height: `${barHeight}%`,
                    width: `${actualBarWidth}px`,
                    minWidth: `${actualBarWidth}px`,
                    maxWidth: `${actualBarWidth}px`,
                  }}
                >
                  {data.segments.map((segment, segIndex) => {
                    const segmentHeight = data.total > 0 ? (segment.value / data.total) * 100 : 0;

                    return (
                      <div
                        key={segIndex}
                        className="w-full"
                        style={{
                          height: `${segmentHeight}%`,
                          backgroundColor: `${segment.color}`,
                        }}
                      />
                    );
                  })}
                </div>
                {showTick && (
                  <div className="absolute top-full left-1/2 mt-3 -translate-x-1/2 text-center text-200 text-text-primary-50">
                    {isLast && lastTickOverride ? lastTickOverride : formatTime(data.datetime)}
                  </div>
                )}
              </div>
            );
          })}
          {/* Centered date label when showDateLabel is true */}
          {showDateLabel && transformedData.length > 0 && (
            <div className="absolute top-full left-1/2 mt-3 -translate-x-1/2 text-center text-200 text-text-primary-50">
              {formatDate(transformedData[Math.floor(transformedData.length / 2)].datetime)}
            </div>
          )}
        </div>
      </div>

      {/* Tooltip */}
      {tooltipText !== null && hoveredBar !== null && tooltipPosition && (
        <div
          className="pointer-events-none absolute z-10 rounded-lg bg-surface-base p-3 text-nowrap shadow-100"
          style={{
            left: `${tooltipPosition.x}px`,
            top: `${tooltipPosition.y}px`,
            transform: "translateX(-50%) translateY(-100%)",
          }}
        >
          <div className="space-y-1 text-xs">
            <div className="text-text-secondary">{tooltipText}</div>
          </div>
        </div>
      )}
    </div>
  );
};

export default SegmentedBarChart;
