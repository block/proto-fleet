interface AxisTickProps {
  payload: { value: string };
  x: number;
  xOffset?: number;
  y: number;
}

const AxisTick = ({ payload, x, xOffset = 0, y }: AxisTickProps) => {
  return (
    <g transform={`translate(${x + xOffset},${y})`}>
      <text
        x={0}
        y={0}
        textAnchor="end"
        fillOpacity={0.5}
        className="text-emphasis-200 fill-text-primary"
      >
        {payload.value}
      </text>
    </g>
  );
};

export default AxisTick;
