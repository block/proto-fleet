import { MouseEvent, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import type { SegmentedBarChartProps } from "./types";
import { formatDate, formatTime, formatTimeRange, getResponsiveValue } from "./utils";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { debounce } from "@/shared/utils/utility";

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
  useDateFormat = false,
  lastTickOverride,
  segmentConfig,
}: SegmentedBarChartProps) => {
  const [hoveredBar, setHoveredBar] = useState<number | null>(null);
  const [tooltipPosition, setTooltipPosition] = useState<{ x: number; y: number } | null>(null);
  const [ticksOverlapping, setTicksOverlapping] = useState<boolean>(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const ticksContainerRef = useRef<HTMLDivElement>(null);
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

  // Check if any tick times have minutes (for consistent formatting)
  const hasMinutesInTicks = useMemo(() => {
    if (!transformedData) return false;

    return transformedData.some((data) => {
      const date = new Date(data.datetime);
      return date.getMinutes() !== 0;
    });
  }, [transformedData]);

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
    // Get the actual segmented bar element (the visual bar with colors)
    const barElement = event.currentTarget.querySelector(".segmented-bar");
    const containerRect = containerRef.current?.getBoundingClientRect();

    if (containerRect && barElement) {
      const barRect = barElement.getBoundingClientRect();
      const barX = barRect.left - containerRect.left;
      const barY = barRect.top - containerRect.top;
      const barCenterY = barY + barRect.height / 2;

      setHoveredBar(index);
      // Initial position (will be adjusted after tooltip renders)
      setTooltipPosition({
        x: barX + barRect.width + 12, // Default to right side
        y: barCenterY,
      });
    }
  };

  // Compute tooltip data: time range and segments with non-zero values
  const tooltipData = useMemo(() => {
    if (hoveredBar === null || !transformedData || !chartData) return null;

    const dataPoint = transformedData[hoveredBar];
    const originalData = chartData[hoveredBar];
    const isLastBar = hoveredBar === transformedData.length - 1;

    // Calculate time range from adjacent data points
    const startTime = dataPoint.datetime;
    let timeRange: string;
    if (isLastBar) {
      timeRange = `${formatTime(startTime)} - live`;
    } else if (hoveredBar < transformedData.length - 1) {
      // Use the next data point to determine the end time
      const nextTime = transformedData[hoveredBar + 1].datetime;
      timeRange = formatTimeRange(startTime, nextTime);
    } else {
      // Fallback to just showing the start time
      timeRange = formatTime(startTime);
    }

    // Get segments with non-zero values
    const nonZeroSegments = dataPoint.segments
      .filter((segment) => {
        const originalValue = originalData[segment.key];
        return originalValue > 0;
      })
      .map((segment) => {
        const originalValue = originalData[segment.key];
        // Always use original value for tooltip, regardless of percentageDisplay
        // Round to integer for miner counts
        const roundedValue = Math.round(originalValue);

        return {
          key: segment.key,
          label: segmentConfig?.[segment.key]?.label || segment.key,
          color: segment.color,
          value: roundedValue,
        };
      });

    return {
      timeRange,
      segments: nonZeroSegments,
    };
  }, [hoveredBar, transformedData, chartData, segmentConfig]);

  const handleBarLeave = () => {
    setHoveredBar(null);
    setTooltipPosition(null);
  };

  // Check for overlapping tick labels with debounce
  useEffect(() => {
    if (showDateLabel || !ticksContainerRef.current || !transformedData) return;

    // Create the debounced function inside the effect to avoid ref access during render
    const checkOverlap = () => {
      if (!ticksContainerRef.current) return;

      const ticks = ticksContainerRef.current.querySelectorAll(".x-axis-tick");
      if (!ticks || ticks.length < 2) {
        setTicksOverlapping(false);
        return;
      }

      // Get ticks that would be visible based on xAxisTickInterval
      const visibleTicks: Element[] = [];
      ticks.forEach((tick, index) => {
        if (index % xAxisTickInterval === 0) {
          visibleTicks.push(tick);
        }
      });

      if (visibleTicks.length < 2) {
        setTicksOverlapping(false);
        return;
      }

      // Check if any adjacent visible ticks have less than 4px spacing
      let hasOverlap = false;
      for (let i = 0; i < visibleTicks.length - 1; i++) {
        const currentRect = visibleTicks[i].getBoundingClientRect();
        const nextRect = visibleTicks[i + 1].getBoundingClientRect();

        // Require minimum 4px gap between ticks
        const gap = nextRect.left - currentRect.right;
        if (gap < 4) {
          hasOverlap = true;
          break;
        }
      }

      setTicksOverlapping(hasOverlap);
    };

    const debouncedCheck = debounce(checkOverlap, 300);
    debouncedCheck();

    // Cleanup: cancel any pending debounced calls
    return () => {
      debouncedCheck.cancel();
    };
  }, [transformedData, xAxisTickInterval, showDateLabel, actualBarWidth, actualBarGapRounded, viewport.width]);

  // Adjust tooltip position after it renders to ensure it fits in the container
  useEffect(() => {
    if (!tooltipRef.current || !containerRef.current || !tooltipPosition || hoveredBar === null) return;

    const tooltip = tooltipRef.current;
    const container = containerRef.current;
    const tooltipRect = tooltip.getBoundingClientRect();
    const containerRect = container.getBoundingClientRect();

    // Get the actual segmented bar element (not the wrapper)
    const barWrappers = container.querySelectorAll("[data-bar-index]");
    const barWrapper = barWrappers[hoveredBar];
    if (!barWrapper) return;

    const barElement = barWrapper.querySelector(".segmented-bar");
    if (!barElement) return;

    const barRect = barElement.getBoundingClientRect();
    const barX = barRect.left - containerRect.left;
    const barCenterY = barRect.top - containerRect.top + barRect.height / 2;

    let newX = barX + barRect.width + 12; // Default to right side

    // Check if tooltip fits on the right
    if (newX + tooltipRect.width > containerRect.width) {
      // Position on the left side instead
      newX = barX - tooltipRect.width - 12;
    }

    // Center vertically relative to bar
    const newY = barCenterY;

    // Update position if it changed
    if (newX !== tooltipPosition.x || newY !== tooltipPosition.y) {
      setTooltipPosition({ x: newX, y: newY });
    }
  }, [hoveredBar, tooltipPosition]);

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
          ref={ticksContainerRef}
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
            const isFirst = index === 0;
            const isLast = index === transformedData.length - 1;

            // Determine if tick should be shown
            let showTick = false;
            if (!showDateLabel) {
              if (ticksOverlapping) {
                // When overlapping, only show first and last ticks (plus hovered)
                showTick = isFirst || isLast || hoveredBar === index;
              } else {
                // Normal behavior: show based on interval or when hovered
                showTick = index % xAxisTickInterval === 0 || hoveredBar === index;
              }
            }

            // Calculate exact position when using fixed gap
            const barPosition =
              actualBarGapRounded !== undefined ? index * (actualBarWidth + actualBarGapRounded) : undefined;

            return (
              <div
                key={index}
                data-bar-index={index}
                className={clsx("group", {
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
                {/* Segmented bar container with hover stroke */}
                <div
                  className="relative"
                  style={{
                    height: `${barHeight}%`,
                    width: `${actualBarWidth}px`,
                    minWidth: `${actualBarWidth}px`,
                    maxWidth: `${actualBarWidth}px`,
                  }}
                >
                  {/* Hover stroke outline: 2px stroke with 2px offset outside the bar */}
                  <div className="pointer-events-none absolute -inset-0.5 rounded-sm opacity-0 ring-2 ring-core-primary-20 transition-opacity group-hover:opacity-100" />

                  {/* Actual segmented bar */}
                  <div className="segmented-bar relative box-border flex h-full w-full flex-col-reverse overflow-hidden rounded-sm">
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
                </div>
                {/* Always render tick but control visibility (only for time ticks, not date labels) */}
                {!showDateLabel ? (
                  <div
                    className={clsx(
                      "x-axis-tick absolute top-full left-1/2 mt-3 -translate-x-1/2 text-center text-200 text-text-primary-50 transition-opacity",
                      {
                        "pointer-events-none opacity-0": !showTick,
                        "opacity-0": showTick && hoveredBar !== null && hoveredBar !== index,
                      },
                    )}
                  >
                    {isLast && lastTickOverride
                      ? lastTickOverride
                      : useDateFormat
                        ? formatDate(data.datetime)
                        : formatTime(data.datetime, hasMinutesInTicks)}
                  </div>
                ) : null}
              </div>
            );
          })}
          {/* Centered date label when showDateLabel is true */}
          {showDateLabel && transformedData.length > 0 ? (
            <div className="absolute top-full left-1/2 mt-3 -translate-x-1/2 text-center text-200 text-text-primary-50">
              {formatDate(transformedData[Math.floor(transformedData.length / 2)].datetime)}
              {hoveredBar !== null ? (
                <span className="ml-1">at {formatTime(transformedData[hoveredBar].datetime, hasMinutesInTicks)}</span>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      {/* Tooltip */}
      {tooltipData && hoveredBar !== null && tooltipPosition ? (
        <div
          ref={tooltipRef}
          className="pointer-events-none absolute z-10 rounded-xl bg-surface-elevated-base/70 pt-6 pb-4 shadow-200 backdrop-blur-[7px]"
          style={{
            left: `${tooltipPosition.x}px`,
            top: `${tooltipPosition.y}px`,
            transform: "translateY(-50%)",
          }}
        >
          <div className="px-6" style={{ minWidth: "200px" }}>
            {/* Time Range Title */}
            <div className="text-200 text-text-primary-70">{tooltipData.timeRange}</div>

            {/* Segments */}
            {tooltipData.segments.length > 0 ? (
              <div>
                {tooltipData.segments.map((segment) => (
                  <div key={segment.key} className="flex items-center gap-2 py-1">
                    {/* Custom icon or color indicator */}
                    {segmentConfig?.[segment.key]?.icon ? (
                      <span className="flex" style={{ color: segment.color }}>
                        {segmentConfig[segment.key].icon}
                      </span>
                    ) : (
                      <div className="h-2 w-2 rounded-full" style={{ backgroundColor: segment.color }} />
                    )}
                    {/* Combined text: value + units + label */}
                    <span className="text-300 text-text-primary">
                      {segment.value}
                      {units
                        ? typeof units === "string"
                          ? ` ${units}`
                          : ` ${segment.value === 1 ? units.singular : units.plural}`
                        : null}{" "}
                      {segment.label.toLowerCase()}
                    </span>
                  </div>
                ))}
              </div>
            ) : null}

            {/* No data message if no segments */}
            {tooltipData.segments.length === 0 ? <div className="text-200 text-text-primary-50">No miners</div> : null}
          </div>
        </div>
      ) : null}
    </div>
  );
};

export default SegmentedBarChart;
