import { getGradientBarValues } from "./utility";

interface GradientBarProps {
  chartHeight: number;
  intensity: number;
  height?: number;
  width?: number;
  x?: number;
}

const GradientBar = ({
  chartHeight,
  intensity,
  height,
  width,
  x,
}: GradientBarProps) => {
  const { bgColor, gradientColor, gradientId } =
    getGradientBarValues(intensity);
  return (
    <g>
      {/* gradient definitions for the filled area */}
      <defs>
        <linearGradient id={gradientId} x1="0" x2="0" y2="100%">
          <stop stopColor={gradientColor} stopOpacity="0.2" />
          <stop stopColor={gradientColor} stopOpacity="0" offset="1" />
        </linearGradient>
      </defs>
      {/* flat bg of the bar */}
      <rect
        x={x}
        rx={4}
        width={width}
        height={`${chartHeight}px`}
        stroke="none"
        fill={bgColor}
        fillOpacity={0.1}
      />
      {/* border for top of filled area */}
      <rect
        x={x}
        y={`calc(${chartHeight}px - ${height}px)`}
        width={width}
        height={1}
        stroke="none"
        fill={bgColor}
      />
      {/* filled area */}
      <rect
        x={x}
        y={`calc(${chartHeight}px - ${height}px)`}
        width={width}
        height={height}
        stroke="none"
        fill={`url(#${gradientId})`}
      />
    </g>
  );
};

export default GradientBar;
