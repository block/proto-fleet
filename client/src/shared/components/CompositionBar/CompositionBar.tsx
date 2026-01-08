import { useMemo } from "react";
import clsx from "clsx";
import {
  DEFAULT_BAR_HEIGHT,
  DEFAULT_GAP,
  GAP_CLASS_MAP,
  MIN_SEGMENT_WIDTH_PERCENTAGE,
  STATUS_COLORS,
} from "./constants";
import type { CompositionBarProps, Segment } from "./types";

/**
 * Individual segment component within the composition bar
 */
interface SegmentBarProps {
  segment: Segment & {
    percentage: number;
    displayPercentage: number;
    displayValue: string;
  };
  colorMap: Record<string, string>;
}

const SegmentBar = ({ segment, colorMap }: SegmentBarProps) => {
  const segmentClasses = clsx(colorMap[segment.status], "transition-all duration-300 ease-in-out", "rounded-full");

  return (
    <div
      className={segmentClasses}
      style={{ width: `${segment.displayPercentage}%` }}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={segment.percentage}
      aria-label={`${segment.name}: ${segment.displayValue}%`}
    />
  );
};

/**
 * CompositionBar - A horizontal bar chart showing data composition with colored segments
 *
 * @example
 * ```tsx
 * <CompositionBar
 *   segments={[
 *     { name: "Healthy", status: "OK", count: 45 },
 *     { name: "Warning", status: "WARNING", count: 10 },
 *     { name: "Critical", status: "CRITICAL", count: 5 }
 *   ]}
 * />
 * ```
 */
const CompositionBar = ({
  segments,
  className,
  height = DEFAULT_BAR_HEIGHT,
  gap = DEFAULT_GAP,
  colorMap,
}: CompositionBarProps) => {
  // Check if this is a loading state (all counts are undefined)
  const isLoading = useMemo(() => {
    return segments.length > 0 && segments.every((segment) => segment.count === undefined);
  }, [segments]);

  // Calculate percentages for each segment
  const segmentData = useMemo(() => {
    // Filter out segments with zero, negative, or undefined counts
    const validSegments = segments.filter((segment) => segment.count !== undefined && segment.count > 0);
    const totalCount = validSegments.reduce((sum, segment) => sum + (segment.count || 0), 0);

    if (totalCount === 0) {
      return [];
    }

    return validSegments.map((segment) => {
      const percentage = ((segment.count || 0) / totalCount) * 100;
      // Ensure minimum width for visibility
      const displayPercentage = Math.max(percentage, MIN_SEGMENT_WIDTH_PERCENTAGE);

      return {
        ...segment,
        count: segment.count || 0,
        percentage: percentage,
        displayPercentage: displayPercentage,
        displayValue: percentage.toFixed(1),
      };
    });
  }, [segments]);

  // Merge custom color mappings with defaults
  const effectiveColorMap = useMemo(
    () => ({
      ...STATUS_COLORS,
      ...colorMap,
    }),
    [colorMap],
  );

  // Handle loading state
  if (isLoading) {
    return (
      <div className={clsx("w-full", className)} data-testid="composition-bar-skeleton">
        <div
          className="relative isolate overflow-hidden rounded-full before:absolute before:inset-0 before:animate-[shimmer_2s_ease-in-out_infinite] before:bg-[linear-gradient(90deg,transparent_0%,var(--color-core-primary-5)_30%,var(--color-core-primary-5)_70%,transparent_100%)]"
          style={{ height: `${height}px` }}
        >
          <div className="h-full rounded-full bg-core-primary-10" />
        </div>
      </div>
    );
  }

  // Handle empty state
  if (segmentData.length === 0) {
    return (
      <div
        className={clsx("w-full rounded-full bg-grayscale-gray-20", className)}
        style={{ height: `${height}px` }}
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={0}
        aria-label="No data available"
      />
    );
  }

  return (
    <div className={clsx("w-full", className)} role="group" aria-label="Composition bar chart">
      <div className={clsx("flex w-full", GAP_CLASS_MAP[gap])} style={{ height: `${height}px` }}>
        {segmentData.map((segment, index) => (
          <SegmentBar
            key={`${segment.name}-${segment.status}-${index}`}
            segment={segment}
            colorMap={effectiveColorMap}
          />
        ))}
      </div>
    </div>
  );
};

export default CompositionBar;
