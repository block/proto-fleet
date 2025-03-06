import { Rectangle } from "recharts";

interface LineCursorProps {
  points?: { x: number }[];
  height?: number;
}

const LineCursor = (props: LineCursorProps) => {
  const { points, height } = props;
  return (
    <g>
      <defs>
        <linearGradient
          id="gradient"
          x1="0.5"
          y1="0"
          x2="0.5"
          y2={height}
          gradientUnits="userSpaceOnUse"
        >
          <stop stopOpacity="0" />
          <stop offset="0.5" stopOpacity="0.5" />
          <stop offset="1" stopOpacity="0" />
        </linearGradient>
      </defs>
      <Rectangle
        fill="url(#gradient)"
        x={points?.[0].x}
        width={1}
        height={height}
      />
    </g>
  );
};

export default LineCursor;
