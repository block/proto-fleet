interface DotProps {
  chartDataLength: number;
  cx?: number;
  cy?: number;
  index?: number;
  stroke?: string;
}

// only show dot on the last data point
const Dot = ({ cx = 0, cy = 0, chartDataLength, index, stroke }: DotProps) => {
  if (index === chartDataLength - 1) {
    return (
      <svg
        x={cx - 8}
        y={cy - 8}
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <circle
          cx="8.11938"
          cy="8.32684"
          r="6"
          fill="white"
          stroke={stroke}
          strokeWidth="3"
          strokeLinecap="round"
        />
      </svg>
    );
  }
  return null;
};

export default Dot;
