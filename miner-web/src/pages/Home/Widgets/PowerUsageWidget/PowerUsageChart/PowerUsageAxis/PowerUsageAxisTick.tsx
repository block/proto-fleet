interface PowerUsageAxisTickProps {
  value: string;
  x: number;
  xOffset?: number;
  y: number;
}

const PowerUsageAxisTick = ({
  value,
  x,
  xOffset = 0,
  y,
}: PowerUsageAxisTickProps) => {
  return (
    <g transform={`translate(${x + xOffset},${y})`}>
      <text
        x={0}
        y={0}
        dy={16}
        textAnchor="end"
        fill="#000"
        fillOpacity={0.5}
        className="text-emphasis-200"
      >
        {value}
      </text>
    </g>
  );
};

export default PowerUsageAxisTick;
