import { useMemo } from "react";

interface SegmentData {
  key: string;
  value: number;
  color: string;
}

interface CustomSegmentedBarProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  fill?: string;
  payload?: {
    datetime: number;
    segments: SegmentData[];
  };
  percentageDisplay?: boolean;
  index?: number;
  isHovered?: boolean;
  onMouseEnter?: (x: number, y: number) => void;
  onMouseLeave?: () => void;
}

const CustomSegmentedBar = ({
  x = 0,
  y = 0,
  width = 0,
  height = 0,
  payload,
  percentageDisplay = false,
  index = 0,
  isHovered = false,
  onMouseEnter,
  onMouseLeave,
}: CustomSegmentedBarProps) => {
  // Generate unique clip path ID for this bar
  const clipId = `rounded-clip-${index}-${payload?.datetime || 0}`;

  // Calculate segment positions and heights
  const segmentRects = useMemo(() => {
    if (!payload?.segments) return [];

    const segments = payload.segments;
    const total = segments.reduce((sum, seg) => sum + seg.value, 0);

    if (total === 0) return [];

    let currentY = y;
    const rects = [];

    // Process segments from bottom to top (reverse order for stacking)
    for (let i = segments.length - 1; i >= 0; i--) {
      const segment = segments[i];
      const segmentValue = segment.value;

      // Skip segments with 0 value
      if (segmentValue === 0) continue;

      let segmentHeight: number;
      if (percentageDisplay) {
        // For percentage display, calculate percentage of total and apply to full height
        const percentage = (segmentValue / total) * 100;
        segmentHeight = (percentage / 100) * height;
      } else {
        // For normal display, height is proportional to value
        segmentHeight = (segmentValue / total) * height;
      }

      // Round to nearest pixel to prevent sub-pixel rendering issues
      const roundedY = Math.round(currentY);
      const roundedHeight = Math.round(segmentHeight);

      // Extend height by 1px for all segments except the last (top) one to prevent gaps
      // The overlap ensures no white lines appear between segments
      const isLastSegment = i === 0; // Remember, we're iterating in reverse
      const adjustedHeight = isLastSegment ? roundedHeight : roundedHeight + 1;

      rects.push({
        x: Math.round(x),
        y: roundedY,
        width: Math.round(width),
        height: adjustedHeight,
        fill: segment.color.startsWith("--")
          ? `var(${segment.color})`
          : segment.color,
        key: segment.key,
      });

      currentY += segmentHeight;
    }

    return rects.reverse(); // Reverse back to match the original order
  }, [payload, x, y, width, height, percentageDisplay]);

  if (!payload || segmentRects.length === 0) {
    return null;
  }

  return (
    <g
      onMouseEnter={() => {
        onMouseEnter?.(x + width / 2, y);
      }}
      onMouseLeave={() => {
        onMouseLeave?.();
      }}
      style={{ cursor: "default" }}
    >
      <defs>
        <clipPath id={clipId}>
          <rect
            x={x}
            y={y}
            width={width}
            height={height}
            rx={4}
            ry={4}
            shapeRendering="crispEdges"
          />
        </clipPath>
      </defs>
      {/* Bar segments */}
      <g clipPath={`url(#${clipId})`}>
        {segmentRects.map((rect) => (
          <rect
            key={rect.key}
            x={rect.x}
            y={rect.y}
            width={rect.width}
            height={rect.height}
            fill={rect.fill}
            shapeRendering="crispEdges"
            stroke="none"
          />
        ))}
      </g>
      {/* Hover border - draw on top */}
      {isHovered && (
        <rect
          x={x - 2}
          y={y - 2}
          width={width + 4}
          height={height + 4}
          rx={6}
          ry={6}
          fill="none"
          stroke="var(--color-border-20)"
          strokeWidth={4}
          shapeRendering="crispEdges"
          pointerEvents="none"
        />
      )}
    </g>
  );
};

export default CustomSegmentedBar;
