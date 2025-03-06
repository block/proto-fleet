interface PowerUsageBarProps {
  active?: boolean;
  height?: number;
  width?: number;
  x?: number;
}

const PowerUsageBar = ({ active, height, width, x }: PowerUsageBarProps) => {
  return (
    <g>
      {/* gradient definitions for the filled area */}
      <defs>
        <linearGradient id="gradient" x1="0" x2="0" y2="100%">
          <stop
            className="text-core-primary-fill"
            stopColor="currentColor"
            stopOpacity="0.1"
          />
          <stop offset="1" stopOpacity="0" />
        </linearGradient>
        <linearGradient id="activeGradient" x1="0" x2="0" y2="100%">
          <stop
            className="text-core-accent-fill"
            stopColor="currentColor"
            stopOpacity="0.2"
          />
          <stop offset="1" stopOpacity="0" />
        </linearGradient>
      </defs>
      {/* flat bg of the bar */}
      <rect
        x={x}
        rx={4}
        width={width}
        height="120px"
        stroke="none"
        className={active ? "fill-core-accent-fill" : "fill-core-primary-5"}
        fillOpacity={active ? 0.1 : 1}
      />
      {/* border for top of filled area */}
      <rect
        x={x}
        y={`calc(120px - ${height}px)`}
        width={width}
        height={1}
        stroke="none"
        className={active ? "fill-core-accent-fill" : "fill-core-primary-80"}
      />
      {/* filled area */}
      <rect
        x={x}
        y={`calc(120px - ${height}px)`}
        width={width}
        height={height}
        stroke="none"
        fill={active ? "url(#activeGradient)" : "url(#gradient)"}
      />
    </g>
  );
};

export default PowerUsageBar;
